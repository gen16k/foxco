package anthropic

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
)

// marshalNoEscape marshals v without HTML-escaping <, >, & so the sentinel and
// notice text appear literally on the wire (functionally equivalent to the
// escaped form, but cleaner and easier to verify).
func marshalNoEscape(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimRight(buf.Bytes(), "\n"), nil
}

// blockModel is the synthetic model name reported on block responses so they
// are visibly distinguishable from real Claude output.
const blockModel = "local-lfm-dlp-blocker"

// NewBlockID returns a unique id for a block response/notice.
func NewBlockID() string {
	var b [12]byte
	_, _ = rand.Read(b[:])
	return "blk_" + hex.EncodeToString(b[:])
}

// BlockText renders the fixed block-notice body. It never quotes the input or a
// secret value; it states only the (safe) reason, that nothing was sent
// externally, and embeds the self-identifying sentinel. See spec §9.4.
func BlockText(reason string) string {
	if reason == "" {
		reason = "機密情報の可能性 / possible sensitive data"
	}
	return "⚠️ ローカルDLPにより、この入力は外部Claude APIへの送信をブロックしました。\n" +
		"⚠️ Local DLP blocked this input from being sent to the external Claude API.\n\n" +
		"理由 / Reason: " + reason + "\n\n" +
		"機密情報を含む可能性があるため、内容は外部送信されていません。該当箇所を削除またはマスクしてから再度実行してください。\n" +
		"Nothing was sent externally. Remove or mask the flagged content, then try again.\n\n" +
		BlockNoticeSentinel
}

// BuildBlockResponse returns a non-streaming Anthropic-compatible assistant
// message carrying the block notice (spec §9.2).
func BuildBlockResponse(reason string) ([]byte, error) {
	msg := map[string]any{
		"id":            "msg_local_dlp_" + NewBlockID(),
		"type":          "message",
		"role":          "assistant",
		"model":         blockModel,
		"content":       []map[string]any{{"type": "text", "text": BlockText(reason)}},
		"stop_reason":   "end_turn",
		"stop_sequence": nil,
		"usage":         map[string]any{"input_tokens": 0, "output_tokens": 0},
	}
	return marshalNoEscape(msg)
}
