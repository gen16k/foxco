package storage

import (
	"path/filepath"
	"testing"
)

func TestStoreRecordAndReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "dlp.db")
	st, err := Open(path, 30)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	ev := AuditEvent{
		EventID: "e1", CreatedAt: "2026-06-06T00:00:00Z", EventType: "request",
		Decision: "BLOCK", LatencyMS: 12, ModelName: "LFM2-1.2B", Backend: "llama",
		UpstreamCalled: false, Details: `{"reason":"secret detected","source":"rule"}`,
	}
	if err := st.Record(ev); err != nil {
		t.Fatalf("record: %v", err)
	}
	st.Close()

	// Reopen and count rows.
	st2, err := Open(path, 30)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer st2.Close()
	var n int
	if err := st2.db.QueryRow(`SELECT COUNT(*) FROM audit_events`).Scan(&n); err != nil {
		t.Fatalf("query: %v", err)
	}
	if n != 1 {
		t.Fatalf("expected 1 row, got %d", n)
	}
}

func TestDefaultEventStoresNoRawText(t *testing.T) {
	// The opt-in raw columns (prompt_text/matched_snippet) must stay nil unless a
	// caller deliberately sets them (store_raw_text=true path). A metadata-only
	// event leaves them NULL, preserving the default "no raw content" posture.
	ev := AuditEvent{Details: `{"reason":"x","source":"rule"}`}
	if ev.PromptText != nil || ev.MatchedSnippet != nil {
		t.Fatal("zero-value AuditEvent must not carry raw prompt/snippet")
	}

	path := filepath.Join(t.TempDir(), "dlp.db")
	st, err := Open(path, 30)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer st.Close()
	ev.EventID = "e1"
	ev.CreatedAt = "2026-06-06T00:00:00Z"
	ev.EventType = "request"
	ev.Decision = "BLOCK"
	if err := st.Record(ev); err != nil {
		t.Fatalf("record: %v", err)
	}
	got, ok, err := st.Get("e1")
	if err != nil || !ok {
		t.Fatalf("get: ok=%v err=%v", ok, err)
	}
	if got.PromptText != nil || got.MatchedSnippet != nil {
		t.Fatalf("persisted row carried raw fields: prompt=%v snippet=%v", got.PromptText, got.MatchedSnippet)
	}
}
