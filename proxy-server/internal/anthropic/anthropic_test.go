package anthropic

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRequestRoundTripPreservesUnknownFields(t *testing.T) {
	body := `{"model":"claude","future_field":{"x":1},"messages":[{"role":"user","content":"hi","weird":true}]}`
	req, err := ParseRequest([]byte(body))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	out, err := req.Marshal()
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var m map[string]json.RawMessage
	_ = json.Unmarshal(out, &m)
	if _, ok := m["future_field"]; !ok {
		t.Error("unknown top-level field not preserved")
	}
	// the unknown per-message field must survive too
	if !strings.Contains(string(out), "weird") {
		t.Error("unknown message field not preserved")
	}
}

func TestSetMessagesReplaces(t *testing.T) {
	req, _ := ParseRequest([]byte(`{"messages":[{"role":"user","content":"a"},{"role":"user","content":"b"}]}`))
	msgs, _ := req.Messages()
	if err := req.SetMessages(msgs[:1]); err != nil {
		t.Fatalf("set: %v", err)
	}
	got, _ := req.Messages()
	if len(got) != 1 || got[0].StringContent() != "a" {
		t.Fatalf("SetMessages did not replace correctly: %+v", got)
	}
}

func TestBuildBlockResponse(t *testing.T) {
	raw, err := BuildBlockResponse("secret detected (aws_access_key)")
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("invalid json: %v", err)
	}
	if m["role"] != "assistant" || m["type"] != "message" {
		t.Errorf("unexpected envelope: %v", m)
	}
	if !strings.Contains(string(raw), BlockNoticeSentinel) {
		t.Error("block response missing sentinel")
	}
}

func TestWriteBlockSSE(t *testing.T) {
	rec := httptest.NewRecorder()
	if err := WriteBlockSSE(rec, "contains api key"); err != nil {
		t.Fatalf("sse: %v", err)
	}
	body := rec.Body.String()
	for _, ev := range []string{"message_start", "content_block_start", "content_block_delta", "content_block_stop", "message_delta", "message_stop"} {
		if !strings.Contains(body, "event: "+ev) {
			t.Errorf("missing event %q", ev)
		}
	}
	if rec.Header().Get("Content-Type") != "text/event-stream" {
		t.Errorf("wrong content-type: %s", rec.Header().Get("Content-Type"))
	}
}

func TestForwarderForwardsHeadersAndBody(t *testing.T) {
	var gotKey, gotPath string
	var gotBody []byte
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotKey = r.Header.Get("x-api-key")
		gotPath = r.URL.Path
		gotBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer upstream.Close()

	f := NewForwarder(upstream.URL, 5000)
	rec := httptest.NewRecorder()
	in := http.Header{}
	in.Set("x-api-key", "sk-test")
	in.Set("content-type", "application/json")
	status, err := f.Forward(context.Background(), "/v1/messages", in, []byte(`{"hello":"world"}`), rec)
	if err != nil {
		t.Fatalf("forward: %v", err)
	}
	if status != http.StatusOK {
		t.Errorf("status = %d", status)
	}
	if gotKey != "sk-test" {
		t.Errorf("api key not forwarded, got %q", gotKey)
	}
	if gotPath != "/v1/messages" {
		t.Errorf("path = %q", gotPath)
	}
	if string(gotBody) != `{"hello":"world"}` {
		t.Errorf("body = %q", gotBody)
	}
	if rec.Body.String() != `{"ok":true}` {
		t.Errorf("client body = %q", rec.Body.String())
	}
}
