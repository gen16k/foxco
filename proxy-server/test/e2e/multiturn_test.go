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
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"promptgate/internal/anthropic"
	"promptgate/internal/dlp"
	"promptgate/internal/inference"
	"promptgate/internal/proxy"
	"promptgate/internal/storage"
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

// TestMultiTurnAllowBlockSanitize drives a real claude child through three turns —
// allow -> block -> sanitize — asserting the proxy's invariants from its own audit
// trail and the child's output. The sequence is made deterministic with the
// always-on rule guardrail (a synthetic AWS key in turn 2): benign turns reliably
// allow and the secret reliably blocks, so it does not depend on LFM judgment
// (covered separately by TestLFMBlocksSensitive). Free; needs no sidecar.
func TestMultiTurnAllowBlockSanitize(t *testing.T) {
	claudePath := requireClaude(t)

	// Mock upstream lets us prove byte-for-byte that the secret never egressed. The
	// child runs in --bare mode so the request carries exactly our content.
	mock := newMockUpstream()
	ms := httptest.NewServer(mock)
	defer ms.Close()

	// allow-all classifier => only the deterministic rule guardrail can block.
	h, rec, logBuf := buildProxy(t, allowAllClassifier{}, ms.URL, "rule-multiturn", "rule")
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

	const awsKey = "AKIAIOSFODNN7EXAMPLE" // matches the aws_access_key rule
	turns := []struct {
		name        string
		prompt      string
		expectBlock bool
	}{
		{"allow1-benign", "What is 2+2? Reply with just the number.", false},
		{"block2-secret", "Save this AWS access key to the deploy config: " + awsKey, true},
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
			requireBlock(t, tn.name, newEvents, "rule")
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

	// Byte-level no-egress: the secret never left the proxy — neither blocked
	// (turn 2) nor sanitized-away (turn 3) — on any channel.
	assertSecretAbsent(t, mock, awsKey)
	t.Logf("no-egress verified: %q absent from all %d forwarded message bodies",
		awsKey, len(mock.bodiesFor("/v1/messages")))
}

// TestLFMBlocksSensitive is the real-LFM showcase: the LFM (on the iGPU) blocks an
// unambiguously sensitive memo (source=lfm) with no egress. It asserts only the
// reliable direction — for sensitive content, both a true positive and the tolerant
// fail-closed parse yield BLOCK — so it is immune to the LFM's benign-turn
// nondeterminism (a 1.2B model occasionally fail-closes on benign input; that is
// why the multi-turn sequence above uses the deterministic rule guardrail). Skips
// if the sidecar is down.
func TestLFMBlocksSensitive(t *testing.T) {
	claudePath := requireClaude(t)
	lfm := requireHealthyLFM(t)

	mock := newMockUpstream()
	ms := httptest.NewServer(mock)
	defer ms.Close()

	h, rec, _ := buildProxy(t, lfm, ms.URL, "LFM2.5-1.2B", "vulkan")
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

	res := child.run(t, true, sensitiveMemo)
	t.Logf("blocked=%v decisions=%s", isBlockNotice(res.Result), summarize(rec.since(0)))
	if res.IsError {
		t.Fatalf("claude reported is_error=true; stderr=%q", res.Stderr)
	}
	if !isBlockNotice(res.Result) {
		t.Fatalf("LFM did not block the clearly-sensitive memo: %q", res.Result)
	}
	requireBlock(t, "lfm-sensitive", rec.since(0), "lfm")
	requireNoEgress(t, "lfm-sensitive", rec.since(0))
	assertMarkerNeverEgressed(t, mock, "/v1/messages")
}

// TestToolResultSecretBlockedAndSanitized covers the TOOL-CALL egress channel,
// which the other tests skip (they pass --tools ""). A real claude child actually
// executes a Read tool whose file contents include a synthetic secret; that secret
// comes back as a tool_result on the next request, which the DLP must block. Then a
// benign follow-up turn forces structure-aware sanitization of the tool_use /
// tool_result pair from history. Free and deterministic: the mock scripts the
// tool_use and the always-on rule guardrail (AWS key) does the blocking, so no LFM
// and no real API are needed.
func TestToolResultSecretBlockedAndSanitized(t *testing.T) {
	claudePath := requireClaude(t)

	cwd := t.TempDir()
	// File name avoids keyword triggers (e.g. "secret") so only the tool_result —
	// not the prompt — is what gets flagged.
	secretFile := filepath.Join(cwd, "deploy-notes.txt")
	const awsKey = "AKIAIOSFODNN7EXAMPLE" // matches the aws_access_key rule
	if err := os.WriteFile(secretFile, []byte("Deployment notes.\naws_access_key = "+awsKey+"\n"), 0o600); err != nil {
		t.Fatalf("write secret file: %v", err)
	}

	mock := newMockUpstream()
	// First model turn: tell the child to Read the file, so its contents return as a
	// tool_result on the NEXT request (the one the DLP inspects).
	inputJSON, _ := json.Marshal(map[string]string{"file_path": secretFile})
	mock.firstToolUse = &toolUsePlan{id: "toolu_e2e01", name: "Read", inputJSON: string(inputJSON)}
	ms := httptest.NewServer(mock)
	defer ms.Close()

	// allow-all classifier so ONLY the deterministic rule guardrail blocks (on the
	// AWS key inside the tool_result) — model-independent and free.
	h, rec, logBuf := buildProxy(t, allowAllClassifier{}, ms.URL, "rule-tools", "tools")
	proxySrv := httptest.NewServer(h)
	defer proxySrv.Close()

	child := childConfig{
		claudePath:   claudePath,
		env:          childEnv(proxySrv.URL, false),
		cwd:          cwd,
		sessionID:    newUUID(t),
		real:         false,
		model:        e2eModel(),
		tools:        "Read",
		allowedTools: "Read",
	}

	// Turn 1: child Reads the file -> tool_result carries the key -> DLP blocks it.
	before := rec.snapshot()
	res := child.run(t, true, "Read the file deploy-notes.txt in the current directory and tell me the access key value.")
	ev1 := rec.since(before)
	t.Logf("turn1 (tool_result) blocked=%v is_error=%v decisions=%s", isBlockNotice(res.Result), res.IsError, summarize(ev1))
	if !isBlockNotice(res.Result) {
		t.Fatalf("tool_result secret was NOT blocked; child result=%q stderr=%q", truncate(res.Result, 200), truncate(res.Stderr, 400))
	}
	// The tool_result request must be BLOCKed (source=rule, no egress). Note the
	// prompt request *does* forward — that's how the child receives the tool_use —
	// so the invariant is "the secret never egressed", not "nothing forwarded".
	requireBlock(t, "tool-result", ev1, "rule")
	assertSecretAbsent(t, mock, awsKey)

	// Turn 2: benign follow-up. History now carries the tool_use/tool_result pair
	// (with the key) plus the block notice; the proxy must sanitize them out before
	// forwarding. Either outcome keeps the key off the wire: ALLOW after sanitizing
	// (expected), or fail-closed BLOCK if a valid structure can't be produced.
	mockMsgBefore := len(mock.bodiesFor("/v1/messages"))
	before2 := rec.snapshot()
	res2 := child.run(t, false, "Thanks. Now what is the capital of France? One word.")
	ev2 := rec.since(before2)
	t.Logf("turn2 (sanitize) blocked=%v is_error=%v decisions=%s", isBlockNotice(res2.Result), res2.IsError, summarize(ev2))
	assertSecretAbsent(t, mock, awsKey)
	if isBlockNotice(res2.Result) {
		t.Logf("turn2 fail-closed BLOCK (still zero egress) — acceptable")
	} else {
		if !sanitizeRe.MatchString(logBuf.String()) {
			t.Errorf("turn2 was allowed but no 'sanitize removed_units>=1' log was emitted:\n%s", logBuf.String())
		}
		if got := len(mock.bodiesFor("/v1/messages")) - mockMsgBefore; got < 1 {
			t.Errorf("turn2 was allowed but no new forwarded request reached the mock")
		}
		t.Logf("turn2 sanitized and forwarded with the tool_result removed")
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

	h := proxy.New(det, fwd, rec, logger, true /*failClosed*/, model, backend, false /*storeRaw*/, proxy.BypassConfig{})
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
	// tools is the value for --tools ("" disables all tools -> one predictable
	// /v1/messages per turn; e.g. "Read" enables the Read tool). allowedTools, when
	// set, is passed to --allowedTools so the tool runs without a permission prompt.
	tools        string
	allowedTools string
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
		"--tools", c.tools, // "" disables all tools; e.g. "Read" enables just Read
		"--permission-mode", "dontAsk",
		"--system-prompt", "You are a helpful assistant. Answer concisely.",
		"--strict-mcp-config", // ignore the user's MCP servers (no --mcp-config given)
		"--model", c.model,
	}
	if c.allowedTools != "" {
		args = append(args, "--allowedTools", c.allowedTools) // pre-approve -> no prompt
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

// toolUsePlan scripts a single tool_use response (used to make the child actually
// execute a tool, so its result flows back as a tool_result the proxy inspects).
type toolUsePlan struct {
	id        string
	name      string
	inputJSON string // the tool input, already JSON-encoded, e.g. {"file_path":"..."}
}

type mockUpstream struct {
	mu sync.Mutex
	// firstToolUse, if set, makes the first real (non-title-generation) /v1/messages
	// call return this tool_use instead of text; it is served at most once.
	firstToolUse *toolUsePlan
	servedTool   bool
	reqs         []capturedReq
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

	// Serve the scripted tool_use to the MAIN conversation request only — not the
	// background session-title request, and not a follow-up that already carries a
	// tool_result — and only once.
	m.mu.Lock()
	plan := m.firstToolUse
	serve := plan != nil && !m.servedTool &&
		!bytes.Contains(body, []byte("Generate a concise")) &&
		!bytes.Contains(body, []byte("tool_result"))
	if serve {
		m.servedTool = true
	}
	m.mu.Unlock()
	if serve {
		writeMockToolUseSSE(w, plan)
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

// writeMockToolUseSSE emits an Anthropic tool_use streaming response so the child
// executes the tool. Events are built via json.Marshal so the tool input (a path
// with backslashes) is escaped correctly.
func writeMockToolUseSSE(w http.ResponseWriter, plan *toolUsePlan) {
	fl, _ := w.(http.Flusher)
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)
	emit := func(name string, obj any) {
		data, _ := json.Marshal(obj)
		fmt.Fprintf(w, "event: %s\ndata: %s\n\n", name, data)
		if fl != nil {
			fl.Flush()
		}
	}
	emit("message_start", map[string]any{"type": "message_start", "message": map[string]any{
		"id": "msg_mock_tool", "type": "message", "role": "assistant", "model": "claude-mock",
		"content": []any{}, "stop_reason": nil, "stop_sequence": nil,
		"usage": map[string]any{"input_tokens": 1, "output_tokens": 1}}})
	emit("content_block_start", map[string]any{"type": "content_block_start", "index": 0,
		"content_block": map[string]any{"type": "tool_use", "id": plan.id, "name": plan.name, "input": map[string]any{}}})
	emit("content_block_delta", map[string]any{"type": "content_block_delta", "index": 0,
		"delta": map[string]any{"type": "input_json_delta", "partial_json": plan.inputJSON}})
	emit("content_block_stop", map[string]any{"type": "content_block_stop", "index": 0})
	emit("message_delta", map[string]any{"type": "message_delta",
		"delta": map[string]any{"stop_reason": "tool_use", "stop_sequence": nil},
		"usage": map[string]any{"output_tokens": 5}})
	emit("message_stop", map[string]any{"type": "message_stop"})
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

// assertSecretAbsent fails if the secret appears in ANY recorded upstream body, on
// any path — the byte-level no-egress guarantee for the tool-call test.
func assertSecretAbsent(t *testing.T, m *mockUpstream, secret string) {
	t.Helper()
	for _, rq := range m.requests() {
		if bytes.Contains(rq.body, []byte(secret)) {
			t.Fatalf("SECURITY: secret reached upstream on %s", rq.path)
		}
	}
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
