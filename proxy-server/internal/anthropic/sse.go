package anthropic

import (
	"fmt"
	"net/http"
)

// WriteBlockSSE emits an Anthropic-compatible SSE stream carrying the block
// notice, for clients that requested stream=true (spec §9.3). It sets the SSE
// headers, writes the message_start..message_stop event sequence, and flushes
// after each event.
func WriteBlockSSE(w http.ResponseWriter, reason string) error {
	flusher, ok := w.(http.Flusher)
	if !ok {
		return fmt.Errorf("response writer does not support streaming")
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)

	id := "msg_local_dlp_" + NewBlockID()
	text := BlockText(reason)

	events := []struct {
		name string
		data map[string]any
	}{
		{"message_start", map[string]any{
			"type": "message_start",
			"message": map[string]any{
				"id": id, "type": "message", "role": "assistant", "model": blockModel,
				"content": []any{}, "stop_reason": nil, "stop_sequence": nil,
				"usage": map[string]any{"input_tokens": 0, "output_tokens": 0},
			},
		}},
		{"content_block_start", map[string]any{
			"type": "content_block_start", "index": 0,
			"content_block": map[string]any{"type": "text", "text": ""},
		}},
		{"content_block_delta", map[string]any{
			"type": "content_block_delta", "index": 0,
			"delta": map[string]any{"type": "text_delta", "text": text},
		}},
		{"content_block_stop", map[string]any{"type": "content_block_stop", "index": 0}},
		{"message_delta", map[string]any{
			"type":  "message_delta",
			"delta": map[string]any{"stop_reason": "end_turn", "stop_sequence": nil},
			"usage": map[string]any{"output_tokens": 0},
		}},
		{"message_stop", map[string]any{"type": "message_stop"}},
	}

	for _, e := range events {
		if err := writeEvent(w, e.name, e.data); err != nil {
			return err
		}
		flusher.Flush()
	}
	return nil
}

func writeEvent(w http.ResponseWriter, name string, data map[string]any) error {
	raw, err := marshalNoEscape(data)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", name, raw)
	return err
}
