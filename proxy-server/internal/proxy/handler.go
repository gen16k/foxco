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

	"local-lfm-dlp-proxy/internal/anthropic"
	"local-lfm-dlp-proxy/internal/dlp"
	"local-lfm-dlp-proxy/internal/sanitizer"
	"local-lfm-dlp-proxy/internal/storage"
)

const defaultMaxBody = 32 << 20 // 32 MiB

// Handler implements the proxy request flow.
type Handler struct {
	detector   *dlp.Detector
	forwarder  *anthropic.Forwarder
	audit      storage.Recorder
	log        *slog.Logger
	failClosed bool
	modelName  string
	backend    string
	maxBody    int64
}

// New builds a Handler.
func New(d *dlp.Detector, f *anthropic.Forwarder, audit storage.Recorder, log *slog.Logger, failClosed bool, modelName, backend string) *Handler {
	if audit == nil {
		audit = storage.NopRecorder{}
	}
	return &Handler{
		detector: d, forwarder: f, audit: audit, log: log,
		failClosed: failClosed, modelName: modelName, backend: backend, maxBody: defaultMaxBody,
	}
}

// classifierWarmingMessage is shown when the request is fail-closed because the
// LFM classifier is unavailable/warming (vs. an actual sensitive-content block),
// so a transient startup state is not mistaken for a content block.
const classifierWarmingMessage = "⏳ DLP分類器が起動中です（モデルのウォームアップ中、または分類器が応答しません）。数秒待ってから再実行してください。"

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
			h.recordBlock(start, "request_unparseable", "proxy")
			h.writeBlock(w, req, isCount, "リクエストを解析できなかったため送信をブロックしました。")
			return
		}
		h.forwardRaw(w, r, body, start)
		return
	}

	eval := h.detector.Evaluate(r.Context(), segs, dlp.LastMessageIndex(msgs))
	if eval.Block {
		h.recordBlock(start, eval.BlockReason, eval.BlockSource)
		// Fail-closed because the classifier could not vet the content (warming /
		// sidecar down): no egress, but surface a distinct "starting up" message
		// rather than a sensitive-content block so the user knows to just retry.
		if eval.BlockSource == "classifier_unavailable" {
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
			h.recordBlock(start, "sanitize_failed", "sanitizer")
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
		h.recordAllow(start, 0, false)
		h.log.Warn("upstream_error", "err", ferr.Error())
		return
	}
	h.recordAllow(start, status, true)
	h.log.Info("decision", "result", "ALLOW", "upstream_status", status, "latency_ms", since(start))
}

func (h *Handler) forwardRaw(w http.ResponseWriter, r *http.Request, body []byte, start time.Time) {
	status, ferr := h.forwarder.Forward(r.Context(), r.URL.Path, r.Header, body, w)
	if ferr != nil {
		http.Error(w, "upstream error", http.StatusBadGateway)
		h.recordAllow(start, 0, false)
		return
	}
	h.recordAllow(start, status, true)
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

func (h *Handler) recordBlock(start time.Time, reason, source string) {
	_ = h.audit.Record(storage.AuditEvent{
		EventID: anthropic.NewBlockID(), CreatedAt: now(), EventType: "request",
		Decision: "BLOCK", LatencyMS: since(start), ModelName: h.modelName, Backend: h.backend,
		UpstreamCalled: false, Details: safeDetails(reason, source),
	})
}

func (h *Handler) recordAllow(start time.Time, status int, upstreamCalled bool) {
	_ = h.audit.Record(storage.AuditEvent{
		EventID: anthropic.NewBlockID(), CreatedAt: now(), EventType: "request",
		Decision: "ALLOW", LatencyMS: since(start), ModelName: h.modelName, Backend: h.backend,
		UpstreamCalled: upstreamCalled, Details: safeStatus(status),
	})
}

// recordPassthrough audits a non-DLP transparent forward. Records method + path
// (no body, no query) so the coverage gap is visible in the audit log.
func (h *Handler) recordPassthrough(start time.Time, method, path string, status int, upstreamCalled bool) {
	_ = h.audit.Record(storage.AuditEvent{
		EventID: anthropic.NewBlockID(), CreatedAt: now(), EventType: "request",
		Decision: "PASSTHROUGH", LatencyMS: since(start), ModelName: h.modelName, Backend: h.backend,
		UpstreamCalled: upstreamCalled, Details: safePassthrough(method, path, status),
	})
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

func now() string             { return time.Now().UTC().Format(time.RFC3339) }
func since(t time.Time) int64 { return time.Since(t).Milliseconds() }
