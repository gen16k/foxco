package proxy

import (
	"bytes"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"local-lfm-dlp-proxy/internal/anthropic"
	"local-lfm-dlp-proxy/internal/dlp"
	"local-lfm-dlp-proxy/internal/inference"
	"local-lfm-dlp-proxy/internal/storage"
)

// mockUpstream records every request body it receives so tests can assert that
// a secret never egresses.
type mockUpstream struct {
	mu     sync.Mutex
	bodies [][]byte
	calls  int
	srv    *httptest.Server
}

func newMockUpstream() *mockUpstream {
	m := &mockUpstream{}
	m.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		m.mu.Lock()
		m.bodies = append(m.bodies, b)
		m.calls++
		m.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"msg_real","type":"message","role":"assistant"}`))
	}))
	return m
}

func (m *mockUpstream) sawSecret(s string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, b := range m.bodies {
		if bytes.Contains(b, []byte(s)) {
			return true
		}
	}
	return false
}

func newTestHandler(upstreamURL string) *Handler {
	det := dlp.NewDetector(dlp.NewRuleEngine(), true, inference.NewKeywordClassifier(), dlp.NewCache(64), true)
	fwd := anthropic.NewForwarder(upstreamURL, 5000)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	return New(det, fwd, nil, log, true, "test", "keyword", false)
}

// newCaptureHandler wires a handler to a real temp audit store so tests can
// assert exactly what was (or was not) persisted. storeRaw toggles store_raw_text.
func newCaptureHandler(t *testing.T, upstreamURL string, storeRaw bool) (*Handler, *storage.Store) {
	t.Helper()
	st, err := storage.Open(filepath.Join(t.TempDir(), "dlp.db"), 0)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	det := dlp.NewDetector(dlp.NewRuleEngine(), true, inference.NewKeywordClassifier(), dlp.NewCache(64), true)
	fwd := anthropic.NewForwarder(upstreamURL, 5000)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	return New(det, fwd, st, log, true, "test", "keyword", storeRaw), st
}

func onlyEvent(t *testing.T, st *storage.Store) storage.EventRow {
	t.Helper()
	page, err := st.Query(storage.EventFilter{})
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if page.Total != 1 {
		t.Fatalf("expected 1 event, got %d", page.Total)
	}
	return page.Events[0]
}

func do(t *testing.T, h *Handler, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.Header.Set("x-api-key", "sk-test")
	rec := httptest.NewRecorder()
	mux := http.NewServeMux()
	h.Register(mux)
	mux.ServeHTTP(rec, req)
	return rec
}

func TestAllowForwardsToUpstream(t *testing.T) {
	up := newMockUpstream()
	defer up.srv.Close()
	h := newTestHandler(up.srv.URL)

	rec := do(t, h, "/v1/messages", `{"model":"claude","messages":[{"role":"user","content":"please refactor my function"}]}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if up.calls != 1 {
		t.Fatalf("expected upstream call, got %d", up.calls)
	}
	if !strings.Contains(rec.Body.String(), "msg_real") {
		t.Errorf("client did not receive upstream response: %s", rec.Body.String())
	}
}

func TestBlockDoesNotCallUpstreamAndHidesSecret(t *testing.T) {
	up := newMockUpstream()
	defer up.srv.Close()
	h := newTestHandler(up.srv.URL)

	secret := "AKIAIOSFODNN7EXAMPLE"
	rec := do(t, h, "/v1/messages", `{"model":"claude","messages":[{"role":"user","content":"check this key `+secret+`"}]}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if up.calls != 0 {
		t.Fatalf("upstream MUST NOT be called on block, got %d calls", up.calls)
	}
	if up.sawSecret(secret) {
		t.Fatal("secret reached upstream")
	}
	if !strings.Contains(rec.Body.String(), anthropic.BlockNoticeSentinel) {
		t.Errorf("block response missing sentinel: %s", rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), secret) {
		t.Error("block response echoed the secret")
	}
}

func TestNextTurnSanitizesBlockedHistory(t *testing.T) {
	up := newMockUpstream()
	defer up.srv.Close()
	h := newTestHandler(up.srv.URL)

	secret := "AKIAIOSFODNN7EXAMPLE"
	body := `{"model":"claude","messages":[
		{"role":"user","content":"find the bug"},
		{"role":"assistant","content":[{"type":"text","text":"reading env"},{"type":"tool_use","id":"tu_1","name":"Read","input":{}}]},
		{"role":"user","content":[{"type":"tool_result","tool_use_id":"tu_1","content":"` + secret + ` found in .env"}]},
		{"role":"assistant","content":"blocked <!-- LOCAL_DLP_NOTE -->"},
		{"role":"user","content":"now explain in general terms how to debug env issues"}
	]}`
	rec := do(t, h, "/v1/messages", body)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if up.calls != 1 {
		t.Fatalf("safe live turn should forward, upstream calls = %d", up.calls)
	}
	if up.sawSecret(secret) {
		t.Fatal("blocked history secret leaked to upstream on a later turn")
	}
	if up.sawSecret("tu_1") {
		t.Error("orphaned tool_use id forwarded (pairing not cleaned)")
	}
}

func TestCountTokensBlockReturnsEstimateNoEgress(t *testing.T) {
	up := newMockUpstream()
	defer up.srv.Close()
	h := newTestHandler(up.srv.URL)

	secret := "AKIAIOSFODNN7EXAMPLE"
	rec := do(t, h, "/v1/messages/count_tokens", `{"model":"claude","messages":[{"role":"user","content":"`+secret+`"}]}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if up.calls != 0 {
		t.Fatalf("count_tokens with secret must not egress, calls = %d", up.calls)
	}
	if !strings.Contains(rec.Body.String(), "input_tokens") {
		t.Errorf("count_tokens block should return a token estimate, got %s", rec.Body.String())
	}
}

func TestStreamBlockReturnsSSE(t *testing.T) {
	up := newMockUpstream()
	defer up.srv.Close()
	h := newTestHandler(up.srv.URL)

	rec := do(t, h, "/v1/messages", `{"model":"claude","stream":true,"messages":[{"role":"user","content":"DB_PASSWORD=hunter2"}]}`)
	if up.calls != 0 {
		t.Fatalf("upstream must not be called, calls = %d", up.calls)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "text/event-stream" {
		t.Errorf("expected SSE content-type, got %q", ct)
	}
	if !strings.Contains(rec.Body.String(), "event: message_stop") {
		t.Errorf("missing SSE terminal event: %s", rec.Body.String())
	}
}

func TestCaptureOnAllowRawOn(t *testing.T) {
	up := newMockUpstream()
	defer up.srv.Close()
	h, st := newCaptureHandler(t, up.srv.URL, true)

	prompt := "please refactor my function"
	do(t, h, "/v1/messages", `{"model":"claude","messages":[{"role":"user","content":"`+prompt+`"}]}`)

	e := onlyEvent(t, st)
	if e.Decision != "ALLOW" {
		t.Fatalf("decision = %s", e.Decision)
	}
	if e.PromptText == nil || *e.PromptText != prompt {
		t.Fatalf("prompt not captured: %v", e.PromptText)
	}
	if e.Path != "/v1/messages" {
		t.Fatalf("path = %q", e.Path)
	}
}

func TestCaptureOnBlockRawOn(t *testing.T) {
	up := newMockUpstream()
	defer up.srv.Close()
	h, st := newCaptureHandler(t, up.srv.URL, true)

	secret := "AKIAIOSFODNN7EXAMPLE"
	do(t, h, "/v1/messages", `{"model":"claude","messages":[{"role":"user","content":"here is `+secret+`"}]}`)

	e := onlyEvent(t, st)
	if e.Decision != "BLOCK" || e.Source != "rule" {
		t.Fatalf("decision/source = %s/%s", e.Decision, e.Source)
	}
	if e.PromptText == nil || !strings.Contains(*e.PromptText, secret) {
		t.Fatalf("blocked prompt not captured: %v", e.PromptText)
	}
	if e.UpstreamCalled {
		t.Fatal("block must not mark upstream called")
	}
}

func TestCaptureOffStoresNoPrompt(t *testing.T) {
	up := newMockUpstream()
	defer up.srv.Close()

	// ALLOW with raw off.
	hA, stA := newCaptureHandler(t, up.srv.URL, false)
	do(t, hA, "/v1/messages", `{"model":"claude","messages":[{"role":"user","content":"please refactor"}]}`)
	if e := onlyEvent(t, stA); e.PromptText != nil {
		t.Fatalf("ALLOW raw-off stored prompt: %v", *e.PromptText)
	}

	// BLOCK with raw off.
	hB, stB := newCaptureHandler(t, up.srv.URL, false)
	do(t, hB, "/v1/messages", `{"model":"claude","messages":[{"role":"user","content":"AKIAIOSFODNN7EXAMPLE"}]}`)
	if e := onlyEvent(t, stB); e.PromptText != nil {
		t.Fatalf("BLOCK raw-off stored prompt: %v", *e.PromptText)
	}
}

func TestCaptureLiveTurnOnly(t *testing.T) {
	up := newMockUpstream()
	defer up.srv.Close()
	h, st := newCaptureHandler(t, up.srv.URL, true)

	body := `{"model":"claude","messages":[
		{"role":"user","content":"first question about apples"},
		{"role":"assistant","content":"sure"},
		{"role":"user","content":"second question about bananas"}
	]}`
	do(t, h, "/v1/messages", body)

	e := onlyEvent(t, st)
	if e.PromptText == nil || *e.PromptText != "second question about bananas" {
		t.Fatalf("live turn not captured exactly: %v", e.PromptText)
	}
	if strings.Contains(*e.PromptText, "apples") {
		t.Fatal("captured prompt leaked earlier history turns")
	}
}

func TestCaptureUnparseableStoresNoPrompt(t *testing.T) {
	up := newMockUpstream()
	defer up.srv.Close()
	h, st := newCaptureHandler(t, up.srv.URL, true)

	do(t, h, "/v1/messages", `{not valid json`)

	e := onlyEvent(t, st)
	if e.Decision != "BLOCK" || e.Source != "proxy" {
		t.Fatalf("unparseable decision/source = %s/%s", e.Decision, e.Source)
	}
	if e.PromptText != nil {
		t.Fatalf("unparseable request must not store a prompt: %v", *e.PromptText)
	}
}

func TestCaptureTruncatesLargePrompt(t *testing.T) {
	up := newMockUpstream()
	defer up.srv.Close()
	h, st := newCaptureHandler(t, up.srv.URL, true)

	big := strings.Repeat("a", 20000) // > maxPromptBytes (16 KiB)
	do(t, h, "/v1/messages", `{"model":"claude","messages":[{"role":"user","content":"`+big+`"}]}`)

	e := onlyEvent(t, st)
	if e.PromptText == nil {
		t.Fatal("large prompt not captured")
	}
	if len(*e.PromptText) >= len(big) {
		t.Fatalf("prompt not truncated: len=%d", len(*e.PromptText))
	}
	if !strings.HasSuffix(*e.PromptText, "…(truncated)") {
		t.Fatalf("missing truncation marker: ...%q", (*e.PromptText)[len(*e.PromptText)-20:])
	}
}
