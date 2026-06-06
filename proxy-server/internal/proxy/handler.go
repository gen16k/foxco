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

	"local-lfm-dlp-proxy/internal/anthropic"
	"local-lfm-dlp-proxy/internal/dlp"
	"local-lfm-dlp-proxy/internal/sanitizer"
	"local-lfm-dlp-proxy/internal/storage"
)

const defaultMaxBody = 32 << 20 // 32 MiB

// maxPromptBytes caps how much live-turn text is persisted when store_raw_text
// is enabled, so one giant prompt can't bloat the audit DB.
const maxPromptBytes = 16 << 10 // 16 KiB

// Handler implements the proxy request flow.
type Handler struct {
	detector   *dlp.Detector
	forwarder  *anthropic.Forwarder
	audit      storage.Recorder
	log        *slog.Logger
	failClosed bool
	modelName  string
	backend    string
	storeRaw   bool // persist the live-turn prompt (opt-in; off by default)
	maxBody    int64
}

// New builds a Handler. storeRaw enables persisting the live user-turn prompt to
// the audit DB (store_raw_text); it is off by default and intentionally relaxes
// the "never persist raw content" invariant for the local admin UI.
func New(d *dlp.Detector, f *anthropic.Forwarder, audit storage.Recorder, log *slog.Logger, failClosed bool, modelName, backend string, storeRaw bool) *Handler {
	if audit == nil {
		audit = storage.NopRecorder{}
	}
	return &Handler{
		detector: d, forwarder: f, audit: audit, log: log,
		failClosed: failClosed, modelName: modelName, backend: backend,
		storeRaw: storeRaw, maxBody: defaultMaxBody,
	}
}

// Register wires the handler onto the standard /v1/messages and count_tokens
// routes. count_tokens is treated as a second egress channel and goes through
// the same DLP path.
func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("/v1/messages", func(w http.ResponseWriter, r *http.Request) { h.process(w, r, false) })
	mux.HandleFunc("/v1/messages/count_tokens", func(w http.ResponseWriter, r *http.Request) { h.process(w, r, true) })
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
			h.recordBlock(start, "request_unparseable", "proxy", r.URL.Path, "")
			h.writeBlock(w, req, isCount, "リクエストを解析できなかったため送信をブロックしました。")
			return
		}
		h.forwardRaw(w, r, body, start)
		return
	}

	// The live turn (the newest user message) is the prompt being sent now. We
	// capture only this turn, not the whole history, which Claude Code resends
	// every request. It is persisted only when store_raw_text is enabled.
	liveText := h.liveTurnText(msgs)

	eval := h.detector.Evaluate(r.Context(), segs, dlp.LastMessageIndex(msgs))
	if eval.Block {
		h.recordBlock(start, eval.BlockReason, eval.BlockSource, r.URL.Path, liveText)
		h.log.Info("decision", "result", "BLOCK", "reason", eval.BlockReason,
			"source", eval.BlockSource, "latency_ms", since(start))
		h.writeBlock(w, req, isCount, eval.BlockReason)
		return
	}

	if len(eval.HistoryNG) > 0 {
		sanitized, serr := sanitizer.Sanitize(msgs, eval.HistoryNG)
		if serr != nil {
			h.recordBlock(start, "sanitize_failed", "sanitizer", r.URL.Path, liveText)
			h.log.Warn("decision", "result", "BLOCK", "reason", "sanitize_failed", "latency_ms", since(start))
			h.writeBlock(w, req, isCount,
				"過去の履歴に機密情報が残っています。/clear で会話をリセットしてから再開してください。")
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

func (h *Handler) recordBlock(start time.Time, reason, source, path, liveText string) {
	_ = h.audit.Record(storage.AuditEvent{
		EventID: anthropic.NewBlockID(), CreatedAt: now(), EventType: "request",
		Decision: "BLOCK", LatencyMS: since(start), ModelName: h.modelName, Backend: h.backend,
		UpstreamCalled: false, Details: safeDetails(reason, source), Path: path,
		PromptText: h.promptPtr(liveText),
		// MatchedSnippet is left unset: the evaluation does not isolate the exact
		// offending span, so a precise snippet is deferred (see docs/todo.md).
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
