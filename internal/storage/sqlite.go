// Package storage persists DLP audit events. It deliberately stores no raw
// request text, secret values, or auth headers (spec §19.2/§19.3) — only
// metadata about each decision. It uses the pure-Go modernc.org/sqlite driver
// so the proxy builds on Windows without cgo.
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
// user content or secrets — only safe metadata (reason, source, path).
type AuditEvent struct {
	EventID        string
	CreatedAt      string
	EventType      string // "request"
	Decision       string // "ALLOW" | "BLOCK"
	LatencyMS      int64
	ModelName      string
	Backend        string
	UpstreamCalled bool
	Details        string // safe JSON: {"reason":...,"source":...,"path":...}
}

// Recorder is the minimal audit sink the proxy depends on, so handlers can run
// with a real Store or a Nop in tests.
type Recorder interface {
	Record(ev AuditEvent) error
}

// Store is a SQLite-backed Recorder.
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
    details_json TEXT
);`

// Open opens (creating if needed) the audit database and applies retention.
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
	s := &Store{db: db}
	if retentionDays > 0 {
		s.applyRetention(retentionDays)
	}
	return s, nil
}

// Record inserts an audit event.
func (s *Store) Record(ev AuditEvent) error {
	_, err := s.db.Exec(
		`INSERT INTO audit_events (event_id, created_at, event_type, decision, latency_ms, model_name, backend, upstream_called, details_json)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		ev.EventID, ev.CreatedAt, ev.EventType, ev.Decision, ev.LatencyMS,
		ev.ModelName, ev.Backend, boolToInt(ev.UpstreamCalled), ev.Details,
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

// NopRecorder discards events (used when storage is disabled or fails to open).
type NopRecorder struct{}

func (NopRecorder) Record(AuditEvent) error { return nil }
