// Package storage persists DLP audit events. By default it stores no raw request
// text, secret values, or auth headers (spec §19.2/§19.3) — only metadata about
// each decision. It can ALSO persist the live user-turn prompt (and a detection
// snippet) when the proxy is started with storage.store_raw_text=true; that is a
// deliberate, user-confirmed opt-in for the local admin UI (off by default, and
// it then stores secrets — see docs/decisions.md). It uses the pure-Go
// modernc.org/sqlite driver so the proxy builds on Windows without cgo.
package storage

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

// AuditEvent is one recorded DLP decision. details_json must never contain raw
// user content or secrets — only safe metadata (reason, source). PromptText and
// MatchedSnippet are nil unless store_raw_text is enabled; when set they DO carry
// raw user content and are persisted only by deliberate opt-in.
type AuditEvent struct {
	EventID        string
	CreatedAt      string
	EventType      string // "request"
	Decision       string // "ALLOW" | "BLOCK" | "BYPASS" | "PASSTHROUGH"
	LatencyMS      int64
	ModelName      string
	Backend        string
	UpstreamCalled bool
	Details        string // safe JSON: {"reason":...,"source":...}
	Path           string // request channel, e.g. "/v1/messages" or "/v1/messages/count_tokens"

	// Opt-in raw fields (nil => SQL NULL). Only populated when store_raw_text=true.
	PromptText     *string // the live user turn (the new prompt) — may contain secrets
	MatchedSnippet *string // detection detail; on BLOCK may include the offending text
}

// Recorder is the minimal audit sink the proxy depends on, so handlers can run
// with a real Store or a Nop in tests.
type Recorder interface {
	Record(ev AuditEvent) error
}

// Store is a SQLite-backed Recorder and read source for the admin API.
type Store struct {
	db *sql.DB
}

const schema = `
CREATE TABLE IF NOT EXISTS audit_events (
    event_id TEXT PRIMARY KEY,
    created_at TEXT NOT NULL,
    event_type TEXT NOT NULL,
    decision TEXT,
    latency_ms INTEGER,
    model_name TEXT,
    backend TEXT,
    upstream_called INTEGER NOT NULL,
    details_json TEXT,
    path TEXT,
    prompt_text TEXT,
    matched_snippet TEXT
);`

// addColumns are the opt-in columns added after the original schema shipped.
// migrate() adds any that a pre-existing database is missing.
var addColumns = []struct{ name, ddl string }{
	{"path", `ALTER TABLE audit_events ADD COLUMN path TEXT`},
	{"prompt_text", `ALTER TABLE audit_events ADD COLUMN prompt_text TEXT`},
	{"matched_snippet", `ALTER TABLE audit_events ADD COLUMN matched_snippet TEXT`},
}

// Open opens (creating if needed) the audit database, applies migrations, and
// applies retention.
func Open(path string, retentionDays int) (*Store, error) {
	if dir := filepath.Dir(path); dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("create state dir: %w", err)
		}
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("create schema: %w", err)
	}
	if err := migrate(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}
	s := &Store{db: db}
	if retentionDays > 0 {
		s.applyRetention(retentionDays)
	}
	return s, nil
}

// migrate idempotently brings an existing audit_events table up to the current
// column set. CREATE TABLE IF NOT EXISTS never alters an existing table, so a DB
// created by an older proxy is missing the opt-in columns; add only what's
// absent. Re-running is a no-op.
func migrate(db *sql.DB) error {
	rows, err := db.Query(`PRAGMA table_info(audit_events)`)
	if err != nil {
		return err
	}
	have := map[string]bool{}
	for rows.Next() {
		var cid, notnull, pk int
		var name, ctype string
		var dflt sql.NullString
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
			rows.Close()
			return err
		}
		have[name] = true
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	for _, c := range addColumns {
		if !have[c.name] {
			if _, err := db.Exec(c.ddl); err != nil {
				return fmt.Errorf("add column %s: %w", c.name, err)
			}
		}
	}
	return nil
}

// Record inserts an audit event. PromptText/MatchedSnippet bind as SQL NULL when
// nil, so a metadata-only event is indistinguishable from an empty string.
func (s *Store) Record(ev AuditEvent) error {
	_, err := s.db.Exec(
		`INSERT INTO audit_events (event_id, created_at, event_type, decision, latency_ms, model_name, backend, upstream_called, details_json, path, prompt_text, matched_snippet)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		ev.EventID, ev.CreatedAt, ev.EventType, ev.Decision, ev.LatencyMS,
		ev.ModelName, ev.Backend, boolToInt(ev.UpstreamCalled), ev.Details,
		nullStr(ev.Path), nullPtr(ev.PromptText), nullPtr(ev.MatchedSnippet),
	)
	return err
}

// Close closes the database.
func (s *Store) Close() error { return s.db.Close() }

func (s *Store) applyRetention(days int) {
	cutoff := time.Now().AddDate(0, 0, -days).UTC().Format(time.RFC3339)
	_, _ = s.db.Exec(`DELETE FROM audit_events WHERE created_at < ?`, cutoff)
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// nullPtr binds a *string as NULL when nil, else its value.
func nullPtr(s *string) any {
	if s == nil {
		return nil
	}
	return *s
}

// nullStr binds an empty string as NULL.
func nullStr(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// NopRecorder discards events (used when storage is disabled or fails to open).
type NopRecorder struct{}

func (NopRecorder) Record(AuditEvent) error { return nil }
