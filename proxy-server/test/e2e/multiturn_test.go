//go:build e2e

// End-to-end multi-turn verification of the DLP proxy by driving a REAL `claude`
// child process across three turns: allow -> block -> sanitize.
//
// Why this exists: the proxy is normally exercised only by in-process unit tests.
// This harness instead spawns the actual `claude` binary (headless, `-p`) pointed
// at the proxy via ANTHROPIC_BASE_URL, and reuses ONE conversation across separate
// process invocations via `--session-id` / `--resume`. That makes a real client
// produce the accumulated history the sanitizer needs, so we verify the product
// the way it is really used.
//
// The proxy itself is the real code path (real constructors from cmd/proxy/main.go,
// real LFM classifier, real forwarder); it is hosted in-process on an httptest
// server purely so we can attach an in-memory audit recorder and a buffered logger
// for assertions.
//
// Two upstream modes (one env toggle):
//   - default (mock): an in-test fake Anthropic endpoint that records request
//     bodies and emits real Anthropic SSE. Free, deterministic, and lets us prove
//     byte-for-byte that sensitive content never egressed.
//   - FOXCO_E2E_REAL=1: forward to https://api.anthropic.com using the ambient
//     Claude subscription (or CLAUDE_CODE_OAUTH_TOKEN). Real model answers; no per-
//     call charge. No-egress is proven by upstream_called=false on block + the
//     sanitize log on turn 3.
//
// Run:
//   go test -tags e2e -run MultiTurn ./test/e2e/ -v          # mock dry-run
//   $env:FOXCO_E2E_REAL=1; go test -tags e2e -run MultiTurn ./test/e2e/ -v
//
// Skips cleanly if `claude` is absent (both tests) or the LFM sidecar is down
// (LFM test only). The rule-anchor test needs neither the sidecar nor real auth.

package e2e

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"local-lfm-dlp-proxy/internal/anthropic"
	"local-lfm-dlp-proxy/internal/dlp"
	"local-lfm-dlp-proxy/internal/inference"
	"local-lfm-dlp-proxy/internal/proxy"
	"local-lfm-dlp-proxy/internal/storage"
)

// memoMarker is a unique token embedded in the sensitive turn so we can grep any
// upstream-received body for it. It is deliberately NOT a credential shape, so the
// rule guardrail does not fire and the BLOCK must come from the LFM (source=lfm).
const memoMarker = "AURORA-OPLAN-7F3KQ"

// sensitiveMemo is fictional business-confidential content (unpublished financials,
// layoffs, undisclosed M&A) with no regex-matchable secret — the kind of thing the
// LFM should block on semantics alone.
const sensitiveMemo = "Summarize this CONFIDENTIAL internal board memo for me: " +
	"Project Aurora (ref " + memoMarker + "): FY2026 Q2 revenue missed plan by 31 percent " +
	"(842M actual versus 1.22B target). The board approved closing the Sendai office and " +
	"cutting 58 roles in August. Undisclosed acquisition talks with competitor Meridian KK " +
	"(offer 9.4B) are ongoing and must not leak to the market."

// TestMultiTurnAllowBlockSanitize drives a real claude child through three turns
// against the real LFM classifier, asserting the proxy's allow/block/sanitize
// invariants from its own authoritative audit trail and the child's output.
func TestMultiTurnAllowBlockSanitize(t *testing.T) {
	claudePath := requireClaude(t)
	lfm := requireHealthyLFM(t)

	// Mock upstream: free, deterministic, and lets us prove byte-for-byte that
	// nothing sensitive egressed. The clean allow/block/sanitize sequence runs in
	// --bare mode so the request carries exactly our content. (Real-API forwarding
	// + auth and real-LFM blocking of the real egress channel are covered by the
	// TestRealUpstream* tests. A clean *benign* turn cannot pass against the real
	// API, because non-bare Claude Code — the mode subscription auth requires —
	// injects account context (email, device id, billing) that the DLP correctly
	// flags. See README "Why the real-upstream tests are split".)
	mock := newMockUpstream()
	ms := httptest.NewServer(mock)
	defer ms.Close()

	h, rec, logBuf := buildProxy(t, lfm, ms.URL, "LFM2.5-1.2B", "vulkan")
	proxySrv := httptest.NewServer(h)
	defer proxySrv.Close()

	child := childConfig{
		claudePath: claudePath,
		env:        childEnv(proxySrv.URL, false),
		cwd:        t.TempDir(),
		sessionID:  newUUID(t),
		real:       false,
		model:      e2eModel(),
	}

	t.Logf("mode=mock-upstream upstream=%s proxy=%s session=%s model=%s",
		ms.URL, proxySrv.URL, child.sessionID, child.model)

	turns := []struct {
		name        string
		prompt      string
		expectBlock bool
	}{
		{"allow1-benign", "What is 2+2? Reply with just the number.", false},
		{"block2-sensitive", sensitiveMemo, true},
		{"allow3-followup", "What is the capital of France? Reply with one word.", false},
	}

	for i, tn := range turns {
		before := rec.snapshot()
		mockBefore := mockCount(mock, "/v1/messages")

		res := child.run(t, i == 0, tn.prompt)
		newEvents := rec.since(before)
		blocked := isBlockNotice(res.Result)

		t.Logf("turn=%s blocked=%v is_error=%v decisions=%s result=%q",
			tn.name, blocked, res.IsError, summarize(newEvents), truncate(res.Result, 80))

		if res.IsError {
			t.Fatalf("turn %s: claude reported is_error=true; stderr=%q result=%q", tn.name, res.Stderr, res.Result)
		}

		if tn.expectBlock {
			if !blocked {
				t.Fatalf("turn %s: expected a DLP block notice but child got a normal answer: %q", tn.name, res.Result)
			}
			requireBlock(t, tn.name, newEvents, "lfm")
			requireNoEgress(t, tn.name, newEvents)
			if got := mockCount(mock, "/v1/messages") - mockBefore; got != 0 {
				t.Errorf("turn %s: %d message(s) reached upstream on a blocked turn (want 0)", tn.name, got)
			}
		} else {
			if blocked {
				t.Fatalf("turn %s: benign prompt was unexpectedly blocked: %q", tn.name, res.Result)
			}
			requireAllowEgress(t, tn.name, newEvents)
		}
	}

	// Turn 3 must have triggered structure-aware sanitization of the blocked turn.
	if m := sanitizeRe.FindStringSubmatch(logBuf.String()); m == nil {
		t.Errorf("expected a 'sanitize removed_units>=1' log on the post-block turn; proxy log:\n%s", logBuf.String())
	} else {
		t.Logf("sanitize removed_units=%s confirmed", m[1])
	}

	// Strong no-egress proof (mock mode): the sensitive memo never left the proxy,
	// neither blocked (turn 2) nor sanitized-away (turn 3), on any egress channel.
	if mock != nil {
		assertMarkerNeverEgressed(t, mock, "/v1/messages")
		assertMarkerNeverEgressed(t, mock, "/v1/messages/count_tokens")
		t.Logf("no-egress verified: marker %q absent from all %d forwarded message bodies",
			memoMarker, len(mock.bodiesFor("/v1/messages")))
	}
}

// TestRuleAnchorBlock is a fast, deterministic anchor that needs neither the LFM
// sidecar nor real auth: a synthetic AWS key trips the always-on rule guardrail,
// so the harness itself is proven sound even if the model is unavailable or flaky.
func TestRuleAnchorBlock(t *testing.T) {
	claudePath := requireClaude(t)

	mock := newMockUpstream()
	ms := httptest.NewServer(mock)
	defer ms.Close()

	// Keyword classifier so this never needs a model; the rule guardrail runs first.
	h, rec, _ := buildProxy(t, inference.NewKeywordClassifier(), ms.URL, "keyword", "keyword")
	proxySrv := httptest.NewServer(h)
	defer proxySrv.Close()

	child := childConfig{
		claudePath: claudePath,
		env:        childEnv(proxySrv.URL, false),
		cwd:        t.TempDir(),
		sessionID:  newUUID(t),
		real:       false,
		model:      e2eModel(),
	}

	// AWS doc example key: matches \b(?:AKIA|ASIA)[0-9A-Z]{16}\b in rules.go.
	res := child.run(t, true, "Please store this AWS key for later: AKIAIOSFODNN7EXAMPLE thanks.")
	if res.IsError {
		t.Fatalf("claude reported is_error=true; stderr=%q", res.Stderr)
	}
	if !isBlockNotice(res.Result) {
		t.Fatalf("expected a DLP block notice, got: %q", res.Result)
	}
	requireBlock(t, "rule-anchor", rec.since(0), "rule")
	requireNoEgress(t, "rule-anchor", rec.since(0))
	if n := mockCount(mock, "/v1/messages"); n != 0 {
		t.Errorf("rule anchor: %d message(s) reached upstream (want 0)", n)
	}
}

// TestRealUpstreamForwarding confirms the real-API path end to end: a real `claude`
// child, via the subscription, talks through the proxy to https://api.anthropic.com
// and a REAL model answer comes back. It uses an allow-all classifier so this test
// isolates auth + forwarding from DLP (the allow/block/sanitize invariants are
// covered by the mock multi-turn test). Runs only with FOXCO_E2E_REAL=1.
func TestRealUpstreamForwarding(t *testing.T) {
	requireRealMode(t)
	claudePath := requireClaude(t)

	h, rec, _ := buildProxy(t, allowAllClassifier{}, "https://api.anthropic.com", "real", "real")
	proxySrv := httptest.NewServer(h)
	defer proxySrv.Close()

	child := childConfig{
		claudePath: claudePath,
		env:        childEnv(proxySrv.URL, true),
		cwd:        t.TempDir(),
		sessionID:  newUUID(t),
		real:       true,
		model:      e2eModel(),
	}
	res := child.run(t, true, "Reply with exactly the single token PINGOK and nothing else.")
	t.Logf("real round-trip: is_error=%v result=%q decisions=%s", res.IsError, truncate(res.Result, 120), summarize(rec.since(0)))
	if res.IsError {
		t.Fatalf("claude is_error=true talking to the real API through the proxy; stderr=%q", res.Stderr)
	}
	if isBlockNotice(res.Result) {
		t.Fatalf("unexpected DLP block in the forwarding test: %q", res.Result)
	}
	if strings.Contains(res.Result, "MOCK_OK") {
		t.Fatalf("got MOCK_OK — the request did not reach the real upstream")
	}
	if !upstreamStatusSeen(rec.since(0), 200) {
		t.Fatalf("no ALLOW with upstream_status=200; subscription auth/forwarding failed: %s", summarize(rec.since(0)))
	}
}

// TestRealUpstreamBlocksSensitive confirms the proxy protects the REAL egress
// channel: with the real LFM and the real upstream, the sensitive memo is blocked
// (source=lfm) and never reaches https://api.anthropic.com. Runs only with
// FOXCO_E2E_REAL=1 and a healthy sidecar.
func TestRealUpstreamBlocksSensitive(t *testing.T) {
	requireRealMode(t)
	claudePath := requireClaude(t)
	lfm := requireHealthyLFM(t)

	h, rec, _ := buildProxy(t, lfm, "https://api.anthropic.com", "LFM2.5-1.2B", "vulkan")
	proxySrv := httptest.NewServer(h)
	defer proxySrv.Close()

	child := childConfig{
		claudePath: claudePath,
		env:        childEnv(proxySrv.URL, true),
		cwd:        t.TempDir(),
		sessionID:  newUUID(t),
		real:       true,
		model:      e2eModel(),
	}
	res := child.run(t, true, sensitiveMemo)
	t.Logf("real block: blocked=%v decisions=%s", isBlockNotice(res.Result), summarize(rec.since(0)))
	if !isBlockNotice(res.Result) {
		t.Fatalf("sensitive memo was NOT blocked against the real upstream: %q", res.Result)
	}
	requireBlock(t, "real-sensitive", rec.since(0), "lfm")
	requireNoEgress(t, "real-sensitive", rec.since(0)) // nothing reached api.anthropic.com
}

// allowAllClassifier never blocks; used by the diagnostic so the request reaches
// the mock regardless of content (the rule guardrail still runs).
type allowAllClassifier struct{}

func (allowAllClassifier) Classify(context.Context, dlp.ClassifyInput) (dlp.ClassifyOutput, error) {
	return dlp.ClassifyOutput{NG: false}, nil
}

// TestDiagnoseNonBareContext is a DIAGNOSTIC (not an invariant check). It runs ONE
// non-bare claude turn — the mode subscription auth requires — against the mock and
// reports (a) what context claude injects into the request body and (b) whether
// subscription auth headers (Authorization: Bearer + anthropic-beta) reach upstream.
// It hits only the mock: no real API, no token spend.
func TestDiagnoseNonBareContext(t *testing.T) {
	claudePath := requireClaude(t)

	mock := newMockUpstream()
	ms := httptest.NewServer(mock)
	defer ms.Close()

	h, _, _ := buildProxy(t, allowAllClassifier{}, ms.URL, "diag", "diag")
	proxySrv := httptest.NewServer(h)
	defer proxySrv.Close()

	child := childConfig{
		claudePath: claudePath,
		env:        childEnv(proxySrv.URL, true), // real=true: ambient subscription, no dummy key
		cwd:        t.TempDir(),
		sessionID:  newUUID(t),
		real:       true, // no --bare
		model:      e2eModel(),
	}
	res := child.run(t, true, "What is 2+2? Reply with just the number.")
	t.Logf("child: is_error=%v result=%q", res.IsError, truncate(res.Result, 160))
	if res.Stderr != "" {
		t.Logf("child stderr: %s", truncate(res.Stderr, 600))
	}

	reqs := mock.requests()
	t.Logf("mock received %d upstream request(s)", len(reqs))
	for i, rq := range reqs {
		auth := rq.header.Get("Authorization")
		authKind := "none"
		switch {
		case strings.HasPrefix(auth, "Bearer "):
			authKind = fmt.Sprintf("Bearer(len=%d)", len(auth)-7)
		case auth != "":
			authKind = "present(non-bearer)"
		}
		t.Logf("[req %d] %s | auth=%s x-api-key=%v anthropic-beta=%q anthropic-version=%q",
			i, rq.path, authKind, rq.header.Get("x-api-key") != "",
			rq.header.Get("anthropic-beta"), rq.header.Get("anthropic-version"))
		t.Logf("[req %d] body=%s", i, truncate(string(rq.body), 1400))
	}
}

// --- proxy wiring (mirrors cmd/proxy/main.go) ---------------------------------

func buildProxy(t *testing.T, classifier dlp.Classifier, upstreamBase, model, backend string) (http.Handler, *captureRecorder, *syncBuffer) {
	t.Helper()
	logBuf := &syncBuffer{}
	logger := slog.New(slog.NewTextHandler(logBuf, &slog.HandlerOptions{Level: slog.LevelInfo}))

	det := dlp.NewDetector(dlp.NewRuleEngine(), true /*ruleEnabled*/, classifier, dlp.NewCache(4096), true /*failClosed*/)
	det.SetLogger(logger)
	fwd := anthropic.NewForwarder(upstreamBase, 60000)
	rec := &captureRecorder{}

	h := proxy.New(det, fwd, rec, logger, true /*failClosed*/, model, backend)
	mux := http.NewServeMux()
	h.Register(mux)
	return mux, rec, logBuf
}

// --- claude child driver ------------------------------------------------------

type childConfig struct {
	claudePath string
	env        []string
	cwd        string
	sessionID  string
	real       bool
	model      string
}

type claudeResult struct {
	Result    string `json:"result"`
	IsError   bool   `json:"is_error"`
	SessionID string `json:"session_id"`
	Subtype   string `json:"subtype"`
	Stderr    string `json:"-"`
}

// run invokes the claude binary for one turn and returns its parsed JSON result.
// Turn 1 opens the session (--session-id); later turns resume it (--resume), so
// the proxy sees the accumulated history.
func (c childConfig) run(t *testing.T, first bool, prompt string) claudeResult {
	t.Helper()
	args := []string{
		"-p", prompt,
		"--output-format", "json",
		"--tools", "", // disable all tools -> one predictable /v1/messages per turn
		"--permission-mode", "dontAsk",
		"--system-prompt", "You are a helpful assistant. Answer concisely.",
		"--strict-mcp-config", // ignore the user's MCP servers (no --mcp-config given)
		"--model", c.model,
	}
	if first {
		args = append(args, "--session-id", c.sessionID)
	} else {
		args = append(args, "--resume", c.sessionID)
	}
	if !c.real {
		// Mock mode: --bare forces ANTHROPIC_API_KEY auth (ignores subscription/
		// keychain) and strips hooks/CLAUDE.md/plugins for a clean, isolated child.
		args = append(args, "--bare")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, c.claudePath, args...)
	cmd.Dir = c.cwd
	cmd.Env = c.env

	var out, errb bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errb
	runErr := cmd.Run()

	var r claudeResult
	if err := json.Unmarshal(bytes.TrimSpace(out.Bytes()), &r); err != nil {
		t.Fatalf("turn (first=%v): could not parse claude JSON output (runErr=%v)\nstdout=%q\nstderr=%q",
			first, runErr, out.String(), errb.String())
	}
	r.Stderr = errb.String()
	return r
}

// childEnv builds the child's environment: point it at the proxy, and (mock mode)
// give it a dummy API key. Stale ANTHROPIC_* are stripped so they cannot leak in.
// Real mode keeps the ambient subscription env (e.g. CLAUDE_CODE_OAUTH_TOKEN).
func childEnv(proxyURL string, real bool) []string {
	var env []string
	for _, kv := range os.Environ() {
		up := strings.ToUpper(kv)
		switch {
		case strings.HasPrefix(up, "ANTHROPIC_BASE_URL="),
			strings.HasPrefix(up, "ANTHROPIC_API_KEY="),
			strings.HasPrefix(up, "ANTHROPIC_AUTH_TOKEN="):
			continue
		}
		env = append(env, kv)
	}
	env = append(env, "ANTHROPIC_BASE_URL="+proxyURL)
	if !real {
		env = append(env, "ANTHROPIC_API_KEY=sk-ant-e2e-mock-key-do-not-use-0000000000")
	}
	return env
}

// --- mock upstream ------------------------------------------------------------

// mockUpstream is a fake Anthropic API: it records every request body (so we can
// prove what did and did not egress) and answers /v1/messages with real Anthropic
// SSE and /v1/messages/count_tokens with a token count.
type capturedReq struct {
	path   string
	body   []byte
	header http.Header
}

type mockUpstream struct {
	mu   sync.Mutex
	reqs []capturedReq
}

func newMockUpstream() *mockUpstream { return &mockUpstream{} }

func (m *mockUpstream) record(path string, body []byte, header http.Header) {
	m.mu.Lock()
	defer m.mu.Unlock()
	b := make([]byte, len(body))
	copy(b, body)
	m.reqs = append(m.reqs, capturedReq{path: path, body: b, header: header.Clone()})
}

func (m *mockUpstream) bodiesFor(path string) [][]byte {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out [][]byte
	for _, r := range m.reqs {
		if r.path == path {
			out = append(out, r.body)
		}
	}
	return out
}

func (m *mockUpstream) requests() []capturedReq {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]capturedReq, len(m.reqs))
	copy(out, m.reqs)
	return out
}

func (m *mockUpstream) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(io.LimitReader(r.Body, 32<<20))
	m.record(r.URL.Path, body, r.Header)

	if strings.HasSuffix(r.URL.Path, "/count_tokens") {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"input_tokens":12}`)
		return
	}
	if strings.Contains(r.Header.Get("Accept"), "event-stream") || bytes.Contains(body, []byte(`"stream":true`)) {
		writeMockSSE(w, "MOCK_OK")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	io.WriteString(w, `{"id":"msg_mock","type":"message","role":"assistant","model":"claude-mock",`+
		`"content":[{"type":"text","text":"MOCK_OK"}],"stop_reason":"end_turn","stop_sequence":null,`+
		`"usage":{"input_tokens":1,"output_tokens":1}}`)
}

// writeMockSSE emits the standard Anthropic event sequence (modeled on the proxy's
// own internal/anthropic/sse.go), flushing each event so the client streams it.
func writeMockSSE(w http.ResponseWriter, text string) {
	fl, _ := w.(http.Flusher)
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)
	events := []string{
		"event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_mock\",\"type\":\"message\",\"role\":\"assistant\",\"model\":\"claude-mock\",\"content\":[],\"stop_reason\":null,\"stop_sequence\":null,\"usage\":{\"input_tokens\":1,\"output_tokens\":1}}}\n\n",
		"event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\n",
		"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"" + text + "\"}}\n\n",
		"event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":0}\n\n",
		"event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\",\"stop_sequence\":null},\"usage\":{\"output_tokens\":2}}\n\n",
		"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n",
	}
	for _, e := range events {
		io.WriteString(w, e)
		if fl != nil {
			fl.Flush()
		}
	}
}

// --- audit capture + log buffer ----------------------------------------------

type captureRecorder struct {
	mu     sync.Mutex
	events []storage.AuditEvent
}

func (c *captureRecorder) Record(ev storage.AuditEvent) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.events = append(c.events, ev)
	return nil
}

func (c *captureRecorder) snapshot() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.events)
}

func (c *captureRecorder) since(n int) []storage.AuditEvent {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]storage.AuditEvent, len(c.events)-n)
	copy(out, c.events[n:])
	return out
}

type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

// --- assertions ---------------------------------------------------------------

type detailFields struct {
	Reason string `json:"reason"`
	Source string `json:"source"`
}

var sanitizeRe = regexp.MustCompile(`msg=sanitize\b[^\n]*removed_units=([1-9]\d*)`)

func requireBlock(t *testing.T, turn string, events []storage.AuditEvent, wantSource string) {
	t.Helper()
	for _, e := range events {
		if e.Decision != "BLOCK" || e.UpstreamCalled {
			continue
		}
		var d detailFields
		_ = json.Unmarshal([]byte(e.Details), &d)
		if d.Source == wantSource {
			return
		}
	}
	t.Fatalf("turn %s: no BLOCK with source=%s and upstream_called=false; got %s", turn, wantSource, summarize(events))
}

func requireNoEgress(t *testing.T, turn string, events []storage.AuditEvent) {
	t.Helper()
	for _, e := range events {
		if e.UpstreamCalled {
			t.Fatalf("turn %s: upstream_called=true on a blocked turn (%s)", turn, summarize(events))
		}
	}
}

func requireAllowEgress(t *testing.T, turn string, events []storage.AuditEvent) {
	t.Helper()
	for _, e := range events {
		if e.Decision == "ALLOW" && e.UpstreamCalled {
			return
		}
	}
	t.Fatalf("turn %s: expected an ALLOW with upstream_called=true; got %s", turn, summarize(events))
}

func assertMarkerNeverEgressed(t *testing.T, m *mockUpstream, path string) {
	t.Helper()
	for i, b := range m.bodiesFor(path) {
		if bytes.Contains(b, []byte(memoMarker)) {
			t.Fatalf("SECURITY: marker %q egressed to upstream %s (body #%d)", memoMarker, path, i)
		}
	}
}

// --- small helpers ------------------------------------------------------------

func requireClaude(t *testing.T) string {
	t.Helper()
	p, err := exec.LookPath("claude")
	if err != nil {
		t.Skip("claude not on PATH; skipping e2e (set up Claude Code to run this)")
	}
	return p
}

func isBlockNotice(result string) bool {
	return strings.Contains(result, anthropic.BlockNoticeSentinel) || strings.Contains(result, "ローカルDLP")
}

func lfmEndpoint() string {
	if v := os.Getenv("FOXCO_E2E_LFM_ENDPOINT"); v != "" {
		return v
	}
	return "http://127.0.0.1:8791"
}

func e2eModel() string {
	if v := os.Getenv("FOXCO_E2E_MODEL"); v != "" {
		return v
	}
	return "claude-haiku-4-5-20251001"
}

func requireRealMode(t *testing.T) {
	t.Helper()
	if os.Getenv("FOXCO_E2E_REAL") != "1" {
		t.Skip("set FOXCO_E2E_REAL=1 to run the real-upstream pass")
	}
}

// requireHealthyLFM builds the real LFM client and skips the test if the sidecar
// is not up (so we never silently run degraded).
func requireHealthyLFM(t *testing.T) *inference.LlamaClient {
	t.Helper()
	lfm := inference.NewLlamaClient(lfmEndpoint(), "LFM2.5-1.2B", 8000, 2000)
	lfm.SetProfile(inference.DefaultProfile())
	hctx, hcancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer hcancel()
	if err := lfm.Health(hctx); err != nil {
		t.Skipf("LFM sidecar not healthy at %s: %v (start it with start.ps1 or llama-server)", lfmEndpoint(), err)
	}
	wctx, wcancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer wcancel()
	if err := lfm.Warmup(wctx); err != nil {
		t.Logf("LFM warmup returned: %v (continuing)", err)
	}
	return lfm
}

// upstreamStatusSeen reports whether any ALLOW event recorded a forwarded request
// with the given upstream HTTP status.
func upstreamStatusSeen(events []storage.AuditEvent, status int) bool {
	for _, e := range events {
		if e.Decision != "ALLOW" || !e.UpstreamCalled {
			continue
		}
		var d struct {
			UpstreamStatus int `json:"upstream_status"`
		}
		_ = json.Unmarshal([]byte(e.Details), &d)
		if d.UpstreamStatus == status {
			return true
		}
	}
	return false
}

func mockCount(m *mockUpstream, path string) int {
	if m == nil {
		return 0
	}
	return len(m.bodiesFor(path))
}

func newUUID(t *testing.T) string {
	t.Helper()
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		t.Fatalf("uuid: %v", err)
	}
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // variant 10
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

func summarize(events []storage.AuditEvent) string {
	if len(events) == 0 {
		return "[]"
	}
	parts := make([]string, 0, len(events))
	for _, e := range events {
		var d detailFields
		_ = json.Unmarshal([]byte(e.Details), &d)
		src := d.Source
		if src == "" {
			src = "-"
		}
		parts = append(parts, fmt.Sprintf("%s(src=%s,up=%v)", e.Decision, src, e.UpstreamCalled))
	}
	return "[" + strings.Join(parts, " ") + "]"
}

func truncate(s string, n int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
