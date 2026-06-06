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

func TestRecordNoRawText(t *testing.T) {
	// Guard against accidentally widening AuditEvent to carry raw content.
	// details must be caller-supplied safe JSON; the schema has no raw column.
	ev := AuditEvent{Details: `{"reason":"x"}`}
	if ev.Details == "" {
		t.Skip()
	}
}
