package upstreamdial

import (
	"context"
	"encoding/binary"
	"net"
	"testing"
	"time"
)

func TestEncodeNameRejectsBadLabels(t *testing.T) {
	if _, err := encodeName("api.anthropic.com"); err != nil {
		t.Errorf("encodeName(valid) = %v", err)
	}
	if _, err := encodeName("a..b"); err == nil {
		t.Error("encodeName(empty label) should error")
	}
}

func TestQueryResponseRoundTrip(t *testing.T) {
	query, err := buildAQuery(0x1234, "api.anthropic.com")
	if err != nil {
		t.Fatal(err)
	}
	resp := buildAResponse(query, []net.IP{
		net.IPv4(160, 79, 104, 10).To4(),
		net.IPv4(160, 79, 104, 11).To4(),
	}, 120)

	ips, ttl, err := parseAResponse(resp, 0x1234)
	if err != nil {
		t.Fatalf("parseAResponse: %v", err)
	}
	if len(ips) != 2 {
		t.Fatalf("got %d IPs, want 2", len(ips))
	}
	if ips[0].String() != "160.79.104.10" || ips[1].String() != "160.79.104.11" {
		t.Errorf("IPs = %v, want [160.79.104.10 160.79.104.11]", ips)
	}
	if ttl != 120 {
		t.Errorf("ttl = %d, want 120", ttl)
	}
}

func TestParseAResponseIDMismatch(t *testing.T) {
	query, _ := buildAQuery(1, "x.anthropic.com")
	resp := buildAResponse(query, []net.IP{net.IPv4(1, 2, 3, 4).To4()}, 60)
	if _, _, err := parseAResponse(resp, 999); err == nil {
		t.Error("parseAResponse should reject an ID mismatch")
	}
}

func TestIsInterceptedCaseAndTrailingDot(t *testing.T) {
	r := &resolver{intercept: toSet([]string{"api.anthropic.com"})}
	for _, h := range []string{"api.anthropic.com", "API.Anthropic.COM", "api.anthropic.com."} {
		if !r.isIntercepted(h) {
			t.Errorf("isIntercepted(%q) = false, want true", h)
		}
	}
	if r.isIntercepted("example.com") {
		t.Error("isIntercepted(example.com) = true, want false")
	}
}

func TestClampTTL(t *testing.T) {
	if got := clampTTL(1); got != minTTL {
		t.Errorf("clampTTL(1) = %v, want %v", got, minTTL)
	}
	if got := clampTTL(99999); got != maxTTL {
		t.Errorf("clampTTL(99999) = %v, want %v", got, maxTTL)
	}
	if got := clampTTL(60); got != 60*time.Second {
		t.Errorf("clampTTL(60) = %v, want 60s", got)
	}
}

// TestDialContextResolvesViaMockDNS is the hermetic end-to-end: a mock UDP DNS
// server resolves an intercepted name to 127.0.0.1, and DialContext must reach a
// local TCP listener at that address. No real DNS, no hosts file, no port 443.
func TestDialContextResolvesViaMockDNS(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		c, e := ln.Accept()
		if e != nil {
			return
		}
		_, _ = c.Write([]byte("ok"))
		_ = c.Close()
	}()

	dnsAddr := startMockDNS(t, net.IPv4(127, 0, 0, 1).To4())
	port := ln.Addr().(*net.TCPAddr).Port

	r := &resolver{
		dnsServer: dnsAddr,
		intercept: toSet([]string{"test.intercept"}),
		dialer:    &net.Dialer{Timeout: 2 * time.Second},
		cache:     map[string]cacheEntry{},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	conn, err := r.DialContext(ctx, "tcp", net.JoinHostPort("test.intercept", itoa(port)))
	if err != nil {
		t.Fatalf("DialContext via mock DNS: %v", err)
	}
	defer conn.Close()
	b := make([]byte, 2)
	if _, err := conn.Read(b); err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(b) != "ok" {
		t.Errorf("read %q, want ok", b)
	}
}

// --- test helpers ---

// buildAResponse crafts a DNS response for the given query, answering with A
// records pointing at ips (using a name-compression pointer to the question).
func buildAResponse(query []byte, ips []net.IP, ttl uint32) []byte {
	out := make([]byte, 12)
	copy(out[0:2], query[0:2])                            // echo ID
	binary.BigEndian.PutUint16(out[2:], 0x8180)           // QR=1 RD=1 RA=1 RCODE=0
	binary.BigEndian.PutUint16(out[4:], 1)                // QDCOUNT
	binary.BigEndian.PutUint16(out[6:], uint16(len(ips))) // ANCOUNT
	out = append(out, query[12:]...)                      // copy the single question verbatim
	for _, ip := range ips {
		var rr [12]byte
		binary.BigEndian.PutUint16(rr[0:], 0xC00C) // pointer to question name (offset 12)
		binary.BigEndian.PutUint16(rr[2:], 1)      // TYPE A
		binary.BigEndian.PutUint16(rr[4:], 1)      // CLASS IN
		binary.BigEndian.PutUint32(rr[6:], ttl)
		binary.BigEndian.PutUint16(rr[10:], 4) // RDLENGTH
		out = append(out, rr[:]...)
		out = append(out, ip.To4()...)
	}
	return out
}

func startMockDNS(t *testing.T, answer net.IP) string {
	t.Helper()
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = pc.Close() })
	go func() {
		buf := make([]byte, 1500)
		for {
			n, addr, e := pc.ReadFrom(buf)
			if e != nil {
				return
			}
			resp := buildAResponse(buf[:n], []net.IP{answer}, 60)
			_, _ = pc.WriteTo(resp, addr)
		}
	}()
	return pc.LocalAddr().String()
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [12]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
