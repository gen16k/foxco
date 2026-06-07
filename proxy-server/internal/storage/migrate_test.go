package storage

import (
	"database/sql"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

// oldSchema is the original audit_events table before the opt-in raw columns
// were added. Tests use it to simulate a database created by an older proxy.
const oldSchema = `
CREATE TABLE IF NOT EXISTS audit_events (
    event_id TEXT PRIMARY KEY,
    created_at TEXT NOT NULL,
    event_type TEXT NOT NULL,
    decision TEXT,
    latency_ms INTEGER,
    model_name TEXT,
    backend TEXT,
    upstream_called INTEGER NOT NULL,
    details_json TEXT
);`

func columns(t *testing.T, path string) map[string]bool {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()
	rows, err := db.Query(`PRAGMA table_info(audit_events)`)
	if err != nil {
		t.Fatalf("pragma: %v", err)
	}
	defer rows.Close()
	have := map[string]bool{}
	for rows.Next() {
		var cid, notnull, pk int
		var name, ctype string
		var dflt sql.NullString
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
			t.Fatalf("scan: %v", err)
		}
		have[name] = true
	}
	return have
}

func createOldDB(t *testing.T, path string) {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open old: %v", err)
	}
	defer db.Close()
	if _, err := db.Exec(oldSchema); err != nil {
		t.Fatalf("old schema: %v", err)
	}
	if _, err := db.Exec(
		`INSERT INTO audit_events (event_id, created_at, event_type, decision, latency_ms, model_name, backend, upstream_called, details_json)
		 VALUES (?,?,?,?,?,?,?,?,?)`,
		"old1", "2026-06-06T00:00:00Z", "request", "BLOCK", 10, "LFM2.5-1.2B", "keyword", 0, `{"reason":"secret detected (aws_access_key)","source":"rule"}`,
	); err != nil {
		t.Fatalf("insert old row: %v", err)
	}
}

func TestMigrateAddsColumns(t *testing.T) {
	path := filepath.Join(t.TempDir(), "dlp.db")
	createOldDB(t, path)
	if have := columns(t, path); have["prompt_text"] {
		t.Fatal("precondition: old DB should not have prompt_text")
	}

	st, err := Open(path, 0)
	if err != nil {
		t.Fatalf("open (migrate): %v", err)
	}
	defer st.Close()

	have := columns(t, path)
	for _, c := range []string{"path", "prompt_text", "matched_snippet"} {
		if !have[c] {
			t.Fatalf("migration did not add column %q", c)
		}
	}
}

func TestMigratePreservesRowsAndRetention(t *testing.T) {
	path := filepath.Join(t.TempDir(), "dlp.db")
	createOldDB(t, path)

	// retention 0 => disabled, so the 2026 row survives.
	st, err := Open(path, 0)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer st.Close()

	got, ok, err := st.Get("old1")
	if err != nil || !ok {
		t.Fatalf("old row lost after migration: ok=%v err=%v", ok, err)
	}
	if got.Source != "rule" || got.Reason == "" {
		t.Fatalf("old row details not parsed: %+v", got)
	}
	if got.PromptText != nil {
		t.Fatal("migrated old row should have NULL prompt_text")
	}

	// New writes can use the added columns.
	prompt := "please send me AKIAIOSFODNN7EXAMPLE"
	if err := st.Record(AuditEvent{
		EventID: "new1", CreatedAt: "2026-06-06T01:00:00Z", EventType: "request",
		Decision: "BLOCK", Path: "/v1/messages", Details: `{"reason":"x","source":"rule"}`,
		PromptText: &prompt,
	}); err != nil {
		t.Fatalf("record new: %v", err)
	}
	n1, ok, err := st.Get("new1")
	if err != nil || !ok {
		t.Fatalf("get new1: ok=%v err=%v", ok, err)
	}
	if n1.PromptText == nil || *n1.PromptText != prompt {
		t.Fatalf("prompt_text not persisted: %v", n1.PromptText)
	}
}

func TestMigrateIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "dlp.db")
	createOldDB(t, path)
	for i := 0; i < 3; i++ {
		st, err := Open(path, 0)
		if err != nil {
			t.Fatalf("open #%d: %v", i, err)
		}
		st.Close()
	}
	// A fresh DB (new schema) should also migrate cleanly (no-op).
	fresh := filepath.Join(t.TempDir(), "fresh.db")
	st, err := Open(fresh, 0)
	if err != nil {
		t.Fatalf("open fresh: %v", err)
	}
	defer st.Close()
	have := columns(t, fresh)
	for _, c := range []string{"path", "prompt_text", "matched_snippet"} {
		if !have[c] {
			t.Fatalf("fresh DB missing column %q", c)
		}
	}
}
