package dlp

import (
	"testing"

	"local-lfm-dlp-proxy/internal/anthropic"
)

func mustMessages(t *testing.T, body string) []anthropic.Message {
	t.Helper()
	req, err := anthropic.ParseRequest([]byte(body))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	msgs, err := req.Messages()
	if err != nil {
		t.Fatalf("messages: %v", err)
	}
	return msgs
}

func TestSegmentStringAndBlockContent(t *testing.T) {
	body := `{
		"model": "claude",
		"messages": [
			{"role": "user", "content": "hello world"},
			{"role": "assistant", "content": [{"type": "text", "text": "ignored assistant"}]},
			{"role": "user", "content": [
				{"type": "text", "text": "look at this"},
				{"type": "tool_result", "tool_use_id": "tu_1", "content": "FILE CONTENTS"}
			]}
		]
	}`
	segs, err := Segmentize(mustMessages(t, body))
	if err != nil {
		t.Fatalf("segment: %v", err)
	}
	if len(segs) != 3 {
		t.Fatalf("got %d segments, want 3: %+v", len(segs), segs)
	}
	if segs[0].Type != SegUserText || segs[0].Text != "hello world" || segs[0].MsgIndex != 0 || segs[0].BlockIndex != -1 {
		t.Errorf("seg0 unexpected: %+v", segs[0])
	}
	if segs[2].Type != SegToolResult || segs[2].ToolUseID != "tu_1" || segs[2].MsgIndex != 2 || segs[2].BlockIndex != 1 {
		t.Errorf("seg2 unexpected: %+v", segs[2])
	}
}

func TestSegmentSkipsAssistant(t *testing.T) {
	body := `{"messages":[{"role":"assistant","content":"model output with sk-secret"}]}`
	segs, err := Segmentize(mustMessages(t, body))
	if err != nil {
		t.Fatalf("segment: %v", err)
	}
	if len(segs) != 0 {
		t.Fatalf("assistant content should not be segmented, got %+v", segs)
	}
}
