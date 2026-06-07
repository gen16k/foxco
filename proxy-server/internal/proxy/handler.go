// Package proxy contains the HTTP handler that ties the DLP pipeline together:
// parse -> segment -> evaluate (live BLOCK / history sanitize) -> forward, with
// audit recording. It is separated from main so it can be integration-tested
// against a mock upstream.
package proxy

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"time"
	"unicode/utf8"

	"promptgate/internal/anthropic"
	"promptgate/internal/dlp"
	"promptgate/internal/sanitizer"
	"promptgate/internal/storage"
)

const defaultMaxBody = 32 << 20 // 32 MiB

// maxPromptBytes caps how much live-turn text is persisted when store_raw_text
// is enabled, so one giant prompt can't bloat the audit DB.
const maxPromptBytes = 16 << 10 // 16 KiB

// BypassConfig is the explicit user-override marker policy. When Enabled and the
// latest user message contains Marker, that turn is forwarded without DLP
// blocking and audited as a distinct BYPASS decision (see bypass.go).
type BypassConfig struct {
	Enabled bool
	Marker  string
}

// Handler implements the proxy request flow.
type Handler struct {
	detector   *dlp.Detector
	forwarder  *anthropic.Forwarder
	audit      storage.Recorder
	log        *slog.Logger
	failClosed bool
	modelName  string
	backend    string
	storeRaw   bool         // persist the live-turn prompt (opt-in; off by default)
	bypass     BypassConfig // explicit user override marker
	maxBody    int64
}

// New builds a Handler. storeRaw enables persisting the live user-turn prompt to
// the audit DB (store_raw_text); it is off by default and intentionally relaxes
// the "never persist raw content" invariant for the local admin UI. bypass
// configures the explicit user override marker.
func New(d *dlp.Detector, f *anthropic.Forwarder, audit storage.Recorder, log *slog.Logger, failClosed bool, modelName, backend string, storeRaw bool, bypass BypassConfig) *Handler {
	if audit == nil {
		audit = storage.NopRecorder{}
	}
	return &Handler{
		detector: d, forwarder: f, audit: audit, log: log,
		failClosed: failClosed, modelName: modelName, backend: backend,
		storeRaw: storeRaw, bypass: bypass, maxBody: defaultMaxBody,
	}
}

// classifierWarmingMessage is shown when the request is fail-closed because the
// LFM classifier is unavailable/warming (vs. an actual sensitive-content block),
// so a transient startup state is not mistaken for a content block.
const classifierWarmingMessage = "⏳ DLP分類器が起動中です（モデルのウォームアップ中、または分類器が応答しません）。数秒待ってから再実行してください。\n" +
	"⏳ The DLP classifier is still starting up (model warming up, or the classifier is not responding). Please wait a few seconds and retry."

// Register wires the handler onto the standard /v1/messages and count_tokens
// routes (the DLP-inspected egress channels). count_tokens is treated as a
// second egress channel and goes through the same DLP path. Under transparent
// interception the proxy also receives every other path to the intercepted host;
// those are transparently passed through (and audited) by the catch-all so that
// non-message endpoints (e.g. GET /v1/models) keep working.
func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("/v1/messages", func(w http.ResponseWriter, r *http.Request) { h.process(w, r, false) })
	mux.HandleFunc("/v1/messages/count_tokens", func(w http.ResponseWriter, r *http.Request) { h.process(w, r, true) })
	mux.HandleFunc("/", h.passthrough)
}

func (h *Handler) process(w http.ResponseWriter, r *http.Request, isCount bool) {
	start := time.Now()
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, h.maxBody))
	if err != nil {
		http.Error(w, "read error", http.StatusBadRequest)
		return
	}

	req, perr := anthropic.ParseRequest(body)
	var msgs []anthropic.Message
	if perr == nil {
		msgs, perr = req.Messages()
	}
	var segs []dlp.Segment
	if perr == nil {
		segs, perr = dlp.Segmentize(msgs)
	}
	if perr != nil {
		// Un-inspectable request: fail closed (block) rather than risk egress.
		if h.failClosed {
			h.recordBlock(start, "request_unparseable", "proxy", r.URL.Path, "", "")
			h.writeBlock(w, req, isCount, "リクエストを解析できなかったため送信をブロックしました。\n"+
				"The request could not be parsed, so it was blocked from being sent.")
			return
		}
		h.forwardRaw(w, r, body, start)
		return
	}

	// The live turn (the newest user message) is the prompt being sent now. We
	// capture only this turn, not the whole history, which Claude Code resends
	// every request. It is persisted only when store_raw_text is enabled.
	liveText := h.liveTurnText(msgs)

	lastIdx := dlp.LastMessageIndex(msgs)

	// Explicit user bypass: when the latest user turn carries the override
	// marker, forward it without DLP blocking (both the rule guardrail and the
	// classifier are skipped for this turn), but still sanitize previously
	// sensitive history. Detection is a deterministic substring check on
	// user-authored text only (never tool_result), so it cannot be triggered by
	// inspected data — preserving the inert-data invariant. This deliberately
	// relaxes "BLOCK means no egress" for the marked turn; it is advisory under
	// the honest-mistake threat model and recorded as a distinct BYPASS decision.
	if h.bypass.Enabled && lastIdx >= 0 && msgs[lastIdx].Role() == "user" &&
		containsBypassMarker(msgs[lastIdx], h.bypass.Marker) {
		h.bypassForward(w, r, req, msgs, segs, lastIdx, isCount, body, start)
		return
	}

	eval := h.detector.Evaluate(r.Context(), segs, lastIdx)
	if eval.Block {
		h.recordBlock(start, eval.BlockReason, eval.BlockSource, r.URL.Path, liveText, eval.BlockMatch)
		// Fail-closed because the classifier could not vet the content (warming /
		// sidecar down): no egress, but surface a distinct "starting up" message
		// rather than a sensitive-content block so the user knows to just retry.
		// This now also covers a history segment the classifier could not vet (see
		// dlp.Evaluate): such a request fails closed here instead of being routed to
		// the misleading "history has secrets" sanitize path below.
		if eval.BlockSource == dlp.SourceClassifierUnavailable {
			h.log.Info("decision", "result", "BLOCK", "reason", "classifier_unavailable",
				"latency_ms", since(start))
			h.writeBlock(w, req, isCount, classifierWarmingMessage)
			return
		}
		h.log.Info("decision", "result", "BLOCK", "reason", eval.BlockReason,
			"source", eval.BlockSource, "latency_ms", since(start))
		h.writeBlock(w, req, isCount, eval.BlockReason)
		return
	}

	if len(eval.HistoryNG) > 0 {
		sanitized, serr := sanitizer.Sanitize(msgs, eval.HistoryNG)
		if serr != nil {
			h.recordBlock(start, "sanitize_failed", "sanitizer", r.URL.Path, liveText, "")
			h.log.Warn("decision", "result", "BLOCK", "reason", "sanitize_failed", "latency_ms", since(start))
			h.writeBlock(w, req, isCount,
				"過去の履歴に機密情報が残っています。/clear で会話をリセットしてから再開してください。\n"+
					"Sensitive content remains in the conversation history. Run /clear to reset the conversation, then start over.")
			return
		}
		if err := req.SetMessages(sanitized); err == nil {
			if nb, err := req.Marshal(); err == nil {
				body = nb
			}
		}
		h.log.Info("sanitize", "removed_units", len(eval.HistoryNG), "latency_ms", since(start))
	}

	status, ferr := h.forwarder.Forward(r.Context(), r.URL.Path, r.Header, body, w)
	if ferr != nil {
		http.Error(w, "upstream error", http.StatusBadGateway)
		h.recordAllow(start, 0, false, r.URL.Path, liveText)
		h.log.Warn("upstream_error", "err", ferr.Error())
		return
	}
	h.recordAllow(start, status, true, r.URL.Path, liveText)
	h.log.Info("decision", "result", "ALLOW", "upstream_status", status, "latency_ms", since(start))
}

func (h *Handler) forwardRaw(w http.ResponseWriter, r *http.Request, body []byte, start time.Time) {
	status, ferr := h.forwarder.Forward(r.Context(), r.URL.Path, r.Header, body, w)
	if ferr != nil {
		http.Error(w, "upstream error", http.StatusBadGateway)
		h.recordAllow(start, 0, false, r.URL.Path, "")
		return
	}
	h.recordAllow(start, status, true, r.URL.Path, "")
}

// bypassForward handles a request whose latest user turn carries the override
// marker: it strips the marker, still sanitizes prior sensitive history, and
// forwards upstream (including the count_tokens egress channel), recording a
// BYPASS audit event. The live turn itself is never classified.
func (h *Handler) bypassForward(w http.ResponseWriter, r *http.Request, req *anthropic.Request,
	msgs []anthropic.Message, segs []dlp.Segment, lastIdx int, isCount bool, body []byte, start time.Time) {

	// Remove the marker so Claude never sees it; keep it only if stripping would
	// leave the message empty (rare: the message was just the marker).
	stripBypassMarker(&msgs[lastIdx], h.bypass.Marker)
	// Recompute the live-turn text after stripping so a persisted prompt (when
	// store_raw_text is on) does not contain the marker.
	liveText := msgs[lastIdx].FlatText()

	out := msgs
	if ng := h.detector.EvaluateHistoryOnly(r.Context(), segs, lastIdx); len(ng) > 0 {
		sanitized, serr := sanitizer.Sanitize(msgs, ng)
		if serr != nil {
			// Cannot produce a structurally valid history: fail closed even on a
			// bypass, rather than risk leaking previously blocked secrets.
			h.recordBlock(start, "sanitize_failed", "sanitizer", r.URL.Path, liveText, "")
			h.log.Warn("decision", "result", "BLOCK", "reason", "sanitize_failed", "latency_ms", since(start))
			h.writeBlock(w, req, isCount,
				"過去の履歴に機密情報が残っています。/clear で会話をリセットしてから再開してください。\n"+
					"Sensitive content remains in the conversation history. Run /clear to reset the conversation, then start over.")
			return
		}
		out = sanitized
	}

	// Best-effort rewrite (matches the normal sanitize path): on the practically
	// impossible marshal failure we forward the original bytes unchanged.
	if err := req.SetMessages(out); err == nil {
		if nb, err := req.Marshal(); err == nil {
			body = nb
		}
	}

	status, ferr := h.forwarder.Forward(r.Context(), r.URL.Path, r.Header, body, w)
	if ferr != nil {
		http.Error(w, "upstream error", http.StatusBadGateway)
		h.recordBypass(start, 0, false, r.URL.Path, liveText)
		h.log.Warn("upstream_error", "err", ferr.Error())
		return
	}
	h.recordBypass(start, status, true, r.URL.Path, liveText)
	h.log.Warn("decision", "result", "BYPASS", "reason", "user_bypass_marker",
		"count_tokens", isCount, "upstream_status", status, "latency_ms", since(start))
}

// passthrough transparently relays any non-message path/method to the upstream.
// Under transparent interception the proxy receives every request to the
// intercepted host; only /v1/messages and count_tokens carry prompt payloads and
// are DLP-inspected. Everything else (e.g. GET /v1/models) is forwarded untouched
// so clients keep working. This is a deliberate, AUDITED DLP coverage gap — the
// event records method + path (no body), so the bypass is visible, not silent.
func (h *Handler) passthrough(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	body, err := io.ReadAll(io.LimitReader(r.Body, h.maxBody))
	if err != nil {
		http.Error(w, "read error", http.StatusBadRequest)
		return
	}
	status, ferr := h.forwarder.ForwardRaw(r.Context(), r.Method, r.URL.RequestURI(), r.Header, body, w)
	if ferr != nil {
		http.Error(w, "upstream error", http.StatusBadGateway)
		h.recordPassthrough(start, r.Method, r.URL.Path, 0, false)
		h.log.Warn("passthrough_upstream_error", "method", r.Method, "path", r.URL.Path, "err", ferr.Error())
		return
	}
	h.recordPassthrough(start, r.Method, r.URL.Path, status, true)
	h.log.Info("decision", "result", "PASSTHROUGH", "method", r.Method, "path", r.URL.Path,
		"upstream_status", status, "latency_ms", since(start))
}

// writeBlock produces the client-facing block response. For count_tokens it
// returns a local token estimate (so Claude Code's token accounting keeps
// working without egress); otherwise an Anthropic-compatible assistant message
// or SSE stream.
func (h *Handler) writeBlock(w http.ResponseWriter, req *anthropic.Request, isCount bool, reason string) {
	if isCount {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]int{"input_tokens": estimateTokens(req)})
		return
	}
	if req != nil && req.Stream() {
		if err := anthropic.WriteBlockSSE(w, reason); err != nil {
			h.log.Warn("sse_error", "err", err.Error())
		}
		return
	}
	raw, err := anthropic.BuildBlockResponse(reason)
	if err != nil {
		http.Error(w, "block build error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(raw)
}

func (h *Handler) recordBlock(start time.Time, reason, source, path, liveText, match string) {
	_ = h.audit.Record(storage.AuditEvent{
		EventID: anthropic.NewBlockID(), CreatedAt: now(), EventType: "request",
		Decision: "BLOCK", LatencyMS: since(start), ModelName: h.modelName, Backend: h.backend,
		UpstreamCalled: false, Details: safeDetails(reason, source), Path: path,
		PromptText: h.promptPtr(liveText),
		// match is the offending text the admin UI highlights (rule: the exact
		// secret span; lfm: the whole flagged segment). It is the secret value, so
		// it is gated by the same opt-in as PromptText and never enters Details.
		MatchedSnippet: h.snippetPtr(match),
	})
}

func (h *Handler) recordBypass(start time.Time, status int, upstreamCalled bool, path, liveText string) {
	_ = h.audit.Record(storage.AuditEvent{
		EventID: anthropic.NewBlockID(), CreatedAt: now(), EventType: "request",
		Decision: "BYPASS", LatencyMS: since(start), ModelName: h.modelName, Backend: h.backend,
		UpstreamCalled: upstreamCalled, Details: safeBypassDetails(status), Path: path,
		PromptText: h.promptPtr(liveText),
	})
}

func (h *Handler) recordAllow(start time.Time, status int, upstreamCalled bool, path, liveText string) {
	_ = h.audit.Record(storage.AuditEvent{
		EventID: anthropic.NewBlockID(), CreatedAt: now(), EventType: "request",
		Decision: "ALLOW", LatencyMS: since(start), ModelName: h.modelName, Backend: h.backend,
		UpstreamCalled: upstreamCalled, Details: safeStatus(status), Path: path,
		PromptText: h.promptPtr(liveText),
	})
}

// recordPassthrough audits a non-DLP transparent forward. Records method + path
// (no body, no query) so the coverage gap is visible in the audit log.
func (h *Handler) recordPassthrough(start time.Time, method, path string, status int, upstreamCalled bool) {
	_ = h.audit.Record(storage.AuditEvent{
		EventID: anthropic.NewBlockID(), CreatedAt: now(), EventType: "request",
		Decision: "PASSTHROUGH", LatencyMS: since(start), ModelName: h.modelName, Backend: h.backend,
		UpstreamCalled: upstreamCalled, Details: safePassthrough(method, path, status), Path: path,
	})
}

// liveTurnText returns the flattened text of the latest (live) user turn, or ""
// if there is none.
func (h *Handler) liveTurnText(msgs []anthropic.Message) string {
	li := dlp.LastMessageIndex(msgs)
	if li < 0 || li >= len(msgs) {
		return ""
	}
	return msgs[li].FlatText()
}

// promptPtr returns the prompt text to persist, or nil when raw storage is off
// or there is nothing to store. Long prompts are truncated (rune-safe).
func (h *Handler) promptPtr(text string) *string {
	if !h.storeRaw || text == "" {
		return nil
	}
	t := truncate(text, maxPromptBytes)
	return &t
}

// snippetPtr returns the offending span to persist (matched_snippet), gated by
// the same store_raw_text opt-in as promptPtr because it carries the secret
// value. nil when raw storage is off or there is no match.
func (h *Handler) snippetPtr(match string) *string {
	if !h.storeRaw || match == "" {
		return nil
	}
	t := truncate(match, maxPromptBytes)
	return &t
}

// estimateTokens gives a rough local token count (~4 chars/token) so a blocked
// count_tokens request still returns a usable number.
func estimateTokens(req *anthropic.Request) int {
	if req == nil {
		return 0
	}
	msgs, err := req.Messages()
	if err != nil {
		return 0
	}
	total := len(req.System())
	for _, m := range msgs {
		total += len(m.FlatText())
	}
	return total / 4
}

func safeDetails(reason, source string) string {
	b, _ := json.Marshal(map[string]string{"reason": reason, "source": source})
	return string(b)
}

func safeStatus(status int) string {
	b, _ := json.Marshal(map[string]int{"upstream_status": status})
	return string(b)
}

func safePassthrough(method, path string, status int) string {
	b, _ := json.Marshal(map[string]any{"method": method, "path": path, "upstream_status": status})
	return string(b)
}

func safeBypassDetails(status int) string {
	b, _ := json.Marshal(map[string]any{
		"reason": "user_bypass_marker", "source": "user", "upstream_status": status,
	})
	return string(b)
}

// truncate cuts s to at most max bytes without splitting a UTF-8 rune, appending
// a marker when it shortens the string.
func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	cut := max
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + "…(truncated)"
}

func now() string             { return time.Now().UTC().Format(time.RFC3339) }
func since(t time.Time) int64 { return time.Since(t).Milliseconds() }
