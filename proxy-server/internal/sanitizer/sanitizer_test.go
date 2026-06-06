package sanitizer

import (
	"strings"
	"testing"

	"local-lfm-dlp-proxy/internal/anthropic"
	"local-lfm-dlp-proxy/internal/dlp"
)

func parse(t *testing.T, body string) []anthropic.Message {
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

func roles(msgs []anthropic.Message) []string {
	out := make([]string, len(msgs))
	for i, m := range msgs {
		out[i] = m.Role()
	}
	return out
}

// hasToolBlock reports whether any message contains a block of the given type.
func hasToolBlock(t *testing.T, msgs []anthropic.Message, typ string) bool {
	for _, m := range msgs {
		blocks, err := m.Blocks()
		if err != nil {
			t.Fatalf("blocks: %v", err)
		}
		for _, b := range blocks {
			if b.Type() == typ {
				return true
			}
		}
	}
	return false
}

func TestSanitizeRemovesToolPairNoOrphan(t *testing.T) {
	body := `{"messages":[
		{"role":"user","content":"find the bug"},
		{"role":"assistant","content":[{"type":"text","text":"let me read"},{"type":"tool_use","id":"tu_1","name":"Read","input":{}}]},
		{"role":"user","content":[{"type":"tool_result","tool_use_id":"tu_1","content":"AKIAIOSFODNN7EXAMPLE in .env"}]},
		{"role":"assistant","content":"blocked <!-- LOCAL_DLP_NOTE -->"},
		{"role":"user","content":"now a safe question"}
	]}`
	msgs := parse(t, body)
	ng := []dlp.Segment{{Type: dlp.SegToolResult, MsgIndex: 2, BlockIndex: 0, ToolUseID: "tu_1"}}

	out, err := Sanitize(msgs, ng)
	if err != nil {
		t.Fatalf("sanitize: %v", err)
	}
	if hasToolBlock(t, out, "tool_use") {
		t.Error("tool_use should have been removed with its tool_result")
	}
	if hasToolBlock(t, out, "tool_result") {
		t.Error("tool_result (secret) should have been removed")
	}
	gotRoles := strings.Join(roles(out), ",")
	if gotRoles != "user,assistant,user" {
		t.Errorf("unexpected roles after sanitize: %s", gotRoles)
	}
	// The remaining assistant text must survive; the secret must be gone.
	for _, m := range out {
		if strings.Contains(m.FlatText(), "AKIA") {
			t.Error("secret leaked into sanitized output")
		}
	}
}

func TestSanitizeRemovesUserTurnAndBlockNotice(t *testing.T) {
	body := `{"messages":[
		{"role":"user","content":"earlier safe"},
		{"role":"assistant","content":"sure"},
		{"role":"user","content":"DB_PASSWORD=hunter2 in .env"},
		{"role":"assistant","content":"blocked <!-- LOCAL_DLP_NOTE -->"},
		{"role":"user","content":"safe followup"}
	]}`
	msgs := parse(t, body)
	ng := []dlp.Segment{{Type: dlp.SegUserText, MsgIndex: 2, BlockIndex: -1}}

	out, err := Sanitize(msgs, ng)
	if err != nil {
		t.Fatalf("sanitize: %v", err)
	}
	if strings.Join(roles(out), ",") != "user,assistant,user" {
		t.Errorf("unexpected roles: %v", roles(out))
	}
	for _, m := range out {
		if strings.Contains(m.FlatText(), "hunter2") {
			t.Error("secret leaked")
		}
		if strings.Contains(m.FlatText(), "LOCAL_DLP_NOTE") {
			t.Error("block notice should have been removed with its turn")
		}
	}
}

func TestSanitizeInvalidWhenLeadingUserRemoved(t *testing.T) {
	// First user turn is sensitive and the following message is a real assistant
	// reply (no sentinel), so removal leaves an assistant-first array -> invalid.
	body := `{"messages":[
		{"role":"user","content":"secret leading turn"},
		{"role":"assistant","content":"real model response"},
		{"role":"user","content":"safe"}
	]}`
	msgs := parse(t, body)
	ng := []dlp.Segment{{Type: dlp.SegUserText, MsgIndex: 0, BlockIndex: -1}}

	if _, err := Sanitize(msgs, ng); err != ErrInvalidStructure {
		t.Fatalf("want ErrInvalidStructure, got %v", err)
	}
}

func TestSanitizeNoopWhenNoNG(t *testing.T) {
	body := `{"messages":[{"role":"user","content":"hi"}]}`
	msgs := parse(t, body)
	out, err := Sanitize(msgs, nil)
	if err != nil {
		t.Fatalf("sanitize: %v", err)
	}
	if len(out) != 1 {
		t.Fatalf("expected passthrough, got %d msgs", len(out))
	}
}

func TestSanitizeRemovesSingleTextBlockKeepsMessage(t *testing.T) {
	body := `{"messages":[
		{"role":"user","content":[{"type":"text","text":"secret part"},{"type":"text","text":"keep this part"}]}
	]}`
	msgs := parse(t, body)
	ng := []dlp.Segment{{Type: dlp.SegUserText, MsgIndex: 0, BlockIndex: 0}}

	out, err := Sanitize(msgs, ng)
	if err != nil {
		t.Fatalf("sanitize: %v", err)
	}
	if len(out) != 1 {
		t.Fatalf("message should be kept, got %d", len(out))
	}
	if strings.Contains(out[0].FlatText(), "secret part") {
		t.Error("secret block not removed")
	}
	if !strings.Contains(out[0].FlatText(), "keep this part") {
		t.Error("non-sensitive block should be kept")
	}
}
