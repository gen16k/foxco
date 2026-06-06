package anthropic

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"strings"
	"time"
)

// forwardHeaders are the request headers passed through to the upstream
// Anthropic API. The api key (x-api-key / authorization) is forwarded but never
// stored by the proxy. Hop-by-hop and proxy-internal headers are dropped.
var forwardHeaders = []string{
	"x-api-key",
	"authorization",
	"anthropic-version",
	"anthropic-beta",
	"anthropic-dangerous-direct-browser-access",
	"content-type",
	"user-agent",
	"accept",
}

// Forwarder relays an approved (and sanitized) request to the upstream API and
// streams the response back to the client unbuffered, so SSE responses are not
// delayed (no TTFT penalty — the proxy inspects requests only, not responses).
type Forwarder struct {
	base  string
	httpc *http.Client
}

// NewForwarder builds a forwarder targeting base (e.g. https://api.anthropic.com).
func NewForwarder(base string, timeoutMS int) *Forwarder {
	return &Forwarder{
		base:  strings.TrimRight(base, "/"),
		httpc: &http.Client{Timeout: time.Duration(timeoutMS) * time.Millisecond},
	}
}

// Forward sends body to base+path with the allow-listed headers from in, then
// copies the upstream status, headers, and body to w. Returns the upstream
// status code (or 0 on transport error).
func (f *Forwarder) Forward(ctx context.Context, path string, in http.Header, body []byte, w http.ResponseWriter) (int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, f.base+path, bytes.NewReader(body))
	if err != nil {
		return 0, err
	}
	for _, h := range forwardHeaders {
		if v := in.Get(h); v != "" {
			req.Header.Set(h, v)
		}
	}

	resp, err := f.httpc.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	for k, vs := range resp.Header {
		if isHopByHop(k) {
			continue
		}
		for _, v := range vs {
			w.Header().Add(k, v)
		}
	}
	w.WriteHeader(resp.StatusCode)
	copyFlushing(w, resp.Body)
	return resp.StatusCode, nil
}

// copyFlushing streams src to w, flushing after each chunk so SSE deltas reach
// the client immediately.
func copyFlushing(w http.ResponseWriter, src io.Reader) {
	flusher, _ := w.(http.Flusher)
	buf := make([]byte, 16*1024)
	for {
		n, err := src.Read(buf)
		if n > 0 {
			if _, werr := w.Write(buf[:n]); werr != nil {
				return
			}
			if flusher != nil {
				flusher.Flush()
			}
		}
		if err != nil {
			return
		}
	}
}

func isHopByHop(h string) bool {
	switch strings.ToLower(h) {
	case "connection", "keep-alive", "proxy-authenticate", "proxy-authorization",
		"te", "trailer", "transfer-encoding", "upgrade":
		return true
	}
	return false
}
