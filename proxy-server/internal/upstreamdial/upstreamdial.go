// Package upstreamdial builds an *http.Transport for reaching the REAL upstream
// API even though the proxy installs a hosts-file entry mapping that hostname to
// 127.0.0.1 (itself). It cannot use Go's net.Resolver, because the pure-Go
// resolver consults the OS hosts file first and would return the loopback entry.
// Instead it performs its own minimal DNS query against a configured external
// resolver (e.g. 1.1.1.1:53), dials the resolved IP directly, and leaves the
// request's URL host as the TLS SNI so the real upstream certificate validates.
package upstreamdial

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	minTTL    = 30 * time.Second
	maxTTL    = 5 * time.Minute
	udpReadTO = 5 * time.Second
)

// New returns an *http.Transport whose DialContext resolves interceptHosts via
// resolverDNS (host:port), bypassing the OS hosts file, and dials the resolved
// IP. Hosts not in interceptHosts use the standard system dialer. TLS is left to
// the transport (SNI from the request URL host), so real cert verification is
// unchanged.
func New(resolverDNS string, interceptHosts []string, dialTimeout time.Duration) *http.Transport {
	r := &resolver{
		dnsServer: resolverDNS,
		intercept: toSet(interceptHosts),
		dialer:    &net.Dialer{Timeout: dialTimeout},
		cache:     map[string]cacheEntry{},
	}
	return &http.Transport{
		DialContext:           r.DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          100,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}
}

type cacheEntry struct {
	ips    []net.IP
	expiry time.Time
}

type resolver struct {
	dnsServer string
	intercept map[string]struct{}
	dialer    *net.Dialer

	mu    sync.Mutex
	cache map[string]cacheEntry

	queryID atomic.Uint32
}

// DialContext dials addr, resolving intercepted hostnames via the external DNS
// server so the proxy's own hosts-file redirect does not loop it back to itself.
func (r *resolver) DialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, err
	}
	// IP literals and non-intercepted names: nothing to bypass.
	if net.ParseIP(host) != nil || !r.isIntercepted(host) {
		return r.dialer.DialContext(ctx, network, addr)
	}

	ips, err := r.resolve(ctx, host)
	if err != nil {
		return nil, fmt.Errorf("upstreamdial: resolve %q via %s: %w", host, r.dnsServer, err)
	}
	var firstErr error
	for _, ip := range ips {
		conn, derr := r.dialer.DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
		if derr == nil {
			return conn, nil
		}
		if firstErr == nil {
			firstErr = derr
		}
	}
	if firstErr == nil {
		firstErr = errors.New("no addresses resolved")
	}
	return nil, fmt.Errorf("upstreamdial: dial %q: %w", host, firstErr)
}

func (r *resolver) isIntercepted(host string) bool {
	_, ok := r.intercept[strings.ToLower(strings.TrimSuffix(host, "."))]
	return ok
}

func (r *resolver) resolve(ctx context.Context, host string) ([]net.IP, error) {
	key := strings.ToLower(host)
	r.mu.Lock()
	if e, ok := r.cache[key]; ok && time.Now().Before(e.expiry) {
		ips := e.ips
		r.mu.Unlock()
		return ips, nil
	}
	r.mu.Unlock()

	ips, ttl, err := r.queryA(ctx, host)
	if err != nil {
		return nil, err
	}
	if len(ips) == 0 {
		return nil, errors.New("no A records")
	}
	r.mu.Lock()
	r.cache[key] = cacheEntry{ips: ips, expiry: time.Now().Add(clampTTL(ttl))}
	r.mu.Unlock()
	return ips, nil
}

// queryA performs a single A-record DNS query over UDP against r.dnsServer.
func (r *resolver) queryA(ctx context.Context, host string) ([]net.IP, uint32, error) {
	id := uint16(r.queryID.Add(1))
	query, err := buildAQuery(id, host)
	if err != nil {
		return nil, 0, err
	}
	conn, err := r.dialer.DialContext(ctx, "udp", r.dnsServer)
	if err != nil {
		return nil, 0, err
	}
	defer conn.Close()

	deadline := time.Now().Add(udpReadTO)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	_ = conn.SetDeadline(deadline)

	if _, err := conn.Write(query); err != nil {
		return nil, 0, err
	}
	buf := make([]byte, 1232) // typical EDNS-less UDP payload ceiling
	n, err := conn.Read(buf)
	if err != nil {
		return nil, 0, err
	}
	return parseAResponse(buf[:n], id)
}

func toSet(hosts []string) map[string]struct{} {
	m := make(map[string]struct{}, len(hosts))
	for _, h := range hosts {
		m[strings.ToLower(strings.TrimSuffix(h, "."))] = struct{}{}
	}
	return m
}

func clampTTL(ttl uint32) time.Duration {
	d := time.Duration(ttl) * time.Second
	if d < minTTL {
		return minTTL
	}
	if d > maxTTL {
		return maxTTL
	}
	return d
}

// --- minimal DNS wire format (RFC 1035, A records only) ---

func buildAQuery(id uint16, host string) ([]byte, error) {
	name, err := encodeName(host)
	if err != nil {
		return nil, err
	}
	msg := make([]byte, 0, 12+len(name)+4)
	var hdr [12]byte
	binary.BigEndian.PutUint16(hdr[0:], id)
	binary.BigEndian.PutUint16(hdr[2:], 0x0100) // RD (recursion desired)
	binary.BigEndian.PutUint16(hdr[4:], 1)      // QDCOUNT
	msg = append(msg, hdr[:]...)
	msg = append(msg, name...)
	var qt [4]byte
	binary.BigEndian.PutUint16(qt[0:], 1) // QTYPE = A
	binary.BigEndian.PutUint16(qt[2:], 1) // QCLASS = IN
	msg = append(msg, qt[:]...)
	return msg, nil
}

func encodeName(host string) ([]byte, error) {
	host = strings.TrimSuffix(host, ".")
	var out []byte
	for _, label := range strings.Split(host, ".") {
		if len(label) == 0 || len(label) > 63 {
			return nil, fmt.Errorf("upstreamdial: invalid DNS label %q", label)
		}
		out = append(out, byte(len(label)))
		out = append(out, label...)
	}
	out = append(out, 0x00)
	return out, nil
}

// parseAResponse extracts IPv4 addresses and the minimum answer TTL from a DNS
// response. It ignores non-A answers and tolerates name compression.
func parseAResponse(buf []byte, wantID uint16) ([]net.IP, uint32, error) {
	if len(buf) < 12 {
		return nil, 0, errors.New("short DNS response")
	}
	if binary.BigEndian.Uint16(buf[0:]) != wantID {
		return nil, 0, errors.New("DNS response ID mismatch")
	}
	flags := binary.BigEndian.Uint16(buf[2:])
	if flags&0x000F != 0 { // RCODE != 0
		return nil, 0, fmt.Errorf("DNS error rcode %d", flags&0x000F)
	}
	qd := int(binary.BigEndian.Uint16(buf[4:]))
	an := int(binary.BigEndian.Uint16(buf[6:]))

	off := 12
	for i := 0; i < qd; i++ { // skip questions
		var err error
		if off, err = skipName(buf, off); err != nil {
			return nil, 0, err
		}
		off += 4 // QTYPE + QCLASS
	}
	if off > len(buf) {
		return nil, 0, errors.New("malformed DNS question section")
	}

	var ips []net.IP
	var minTTLVal uint32 = ^uint32(0)
	for i := 0; i < an && off < len(buf); i++ {
		var err error
		if off, err = skipName(buf, off); err != nil {
			return nil, 0, err
		}
		if off+10 > len(buf) {
			return nil, 0, errors.New("malformed DNS RR header")
		}
		typ := binary.BigEndian.Uint16(buf[off:])
		ttl := binary.BigEndian.Uint32(buf[off+4:])
		rdlen := int(binary.BigEndian.Uint16(buf[off+8:]))
		off += 10
		if off+rdlen > len(buf) {
			return nil, 0, errors.New("malformed DNS RDATA")
		}
		if typ == 1 && rdlen == 4 { // A record
			ip := net.IPv4(buf[off], buf[off+1], buf[off+2], buf[off+3])
			ips = append(ips, ip)
			if ttl < minTTLVal {
				minTTLVal = ttl
			}
		}
		off += rdlen
	}
	if len(ips) == 0 {
		minTTLVal = 0
	}
	return ips, minTTLVal, nil
}

// skipName advances past a (possibly compressed) DNS name and returns the new
// offset. A compression pointer terminates the name immediately.
func skipName(buf []byte, off int) (int, error) {
	for {
		if off >= len(buf) {
			return 0, errors.New("DNS name runs past buffer")
		}
		b := buf[off]
		switch {
		case b == 0x00:
			return off + 1, nil
		case b&0xC0 == 0xC0: // 2-byte compression pointer
			if off+2 > len(buf) {
				return 0, errors.New("truncated DNS name pointer")
			}
			return off + 2, nil
		case b&0xC0 == 0x00: // label of length b
			off += int(b) + 1
		default:
			return 0, errors.New("unknown DNS label type")
		}
	}
}
