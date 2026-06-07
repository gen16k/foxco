package admin

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"promptgate/internal/storage"
)

func seededStore(t *testing.T) *storage.Store {
	t.Helper()
	st, err := storage.Open(filepath.Join(t.TempDir(), "dlp.db"), 0)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	prompt := "here is AKIAIOSFODNN7EXAMPLE"
	rows := []storage.AuditEvent{
		{EventID: "a1", CreatedAt: "2026-06-06T10:00:00Z", EventType: "request", Decision: "ALLOW", LatencyMS: 100, ModelName: "LFM2.5-1.2B", Backend: "keyword", UpstreamCalled: true, Details: `{"upstream_status":200}`, Path: "/v1/messages"},
		{EventID: "b1", CreatedAt: "2026-06-06T10:05:00Z", EventType: "request", Decision: "BLOCK", LatencyMS: 200, ModelName: "LFM2.5-1.2B", Backend: "keyword", UpstreamCalled: false, Details: `{"reason":"secret detected (aws_access_key)","source":"rule"}`, Path: "/v1/messages", PromptText: &prompt},
	}
	for _, r := range rows {
		if err := st.Record(r); err != nil {
			t.Fatalf("seed %s: %v", r.EventID, err)
		}
	}
	return st
}

func serve(t *testing.T, h *Handler, method, target string) *httptest.ResponseRecorder {
	t.Helper()
	mux := http.NewServeMux()
	h.Register(mux)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(method, target, nil))
	return rec
}

func TestStatsRoute(t *testing.T) {
	h := New(seededStore(t), Meta{}, "", nil)
	rec := serve(t, h, http.MethodGet, "/admin/stats")
	if rec.Code != 200 {
		t.Fatalf("status = %d", rec.Code)
	}
	var s storage.Stats
	if err := json.Unmarshal(rec.Body.Bytes(), &s); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if s.Total != 2 || s.Blocked != 1 || s.Allowed != 1 {
		t.Fatalf("stats wrong: %+v", s)
	}
}

func TestEventsRouteAndFilter(t *testing.T) {
	h := New(seededStore(t), Meta{}, "", nil)

	all := serve(t, h, http.MethodGet, "/admin/events")
	var page storage.EventPage
	_ = json.Unmarshal(all.Body.Bytes(), &page)
	if page.Total != 2 {
		t.Fatalf("events total = %d", page.Total)
	}

	blocked := serve(t, h, http.MethodGet, "/admin/events?decision=BLOCK")
	var bp storage.EventPage
	_ = json.Unmarshal(blocked.Body.Bytes(), &bp)
	if bp.Total != 1 || bp.Events[0].EventID != "b1" {
		t.Fatalf("filter wrong: %+v", bp)
	}
}

func TestEventDetail(t *testing.T) {
	h := New(seededStore(t), Meta{}, "", nil)

	found := serve(t, h, http.MethodGet, "/admin/events/b1")
	if found.Code != 200 {
		t.Fatalf("found status = %d", found.Code)
	}
	var row storage.EventRow
	_ = json.Unmarshal(found.Body.Bytes(), &row)
	if row.PromptText == nil {
		t.Fatal("detail should include prompt text")
	}

	missing := serve(t, h, http.MethodGet, "/admin/events/nope")
	if missing.Code != 404 {
		t.Fatalf("missing status = %d, want 404", missing.Code)
	}
}

func TestMetaRoute(t *testing.T) {
	meta := Meta{StoreRawText: true, RetentionDays: 30, Model: "LFM2.5-1.2B", Backend: "keyword", ListenAddr: "127.0.0.1:8787"}
	h := New(seededStore(t), meta, "", nil)
	rec := serve(t, h, http.MethodGet, "/admin/meta")
	var got Meta
	_ = json.Unmarshal(rec.Body.Bytes(), &got)
	if !got.StoreRawText || got.Model != "LFM2.5-1.2B" {
		t.Fatalf("meta wrong: %+v", got)
	}
}

func TestMethodNotAllowed(t *testing.T) {
	h := New(seededStore(t), Meta{}, "", nil)
	rec := serve(t, h, http.MethodPost, "/admin/events")
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST status = %d, want 405", rec.Code)
	}
}

func TestAuthToken(t *testing.T) {
	h := New(seededStore(t), Meta{}, "s3cret", nil)
	mux := http.NewServeMux()
	h.Register(mux)

	// No token => 401.
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/admin/stats", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("no-token status = %d, want 401", rec.Code)
	}

	// Wrong token => 401.
	rec = httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/admin/stats", nil)
	req.Header.Set("Authorization", "Bearer wrong")
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("wrong-token status = %d, want 401", rec.Code)
	}

	// Correct token => 200.
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/admin/stats", nil)
	req.Header.Set("Authorization", "Bearer s3cret")
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("correct-token status = %d, want 200", rec.Code)
	}
}

func TestEmptyDB(t *testing.T) {
	st, err := storage.Open(filepath.Join(t.TempDir(), "dlp.db"), 0)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer st.Close()
	h := New(st, Meta{}, "", nil)

	stats := serve(t, h, http.MethodGet, "/admin/stats")
	if stats.Code != 200 {
		t.Fatalf("empty stats status = %d", stats.Code)
	}
	events := serve(t, h, http.MethodGet, "/admin/events")
	var page storage.EventPage
	if err := json.Unmarshal(events.Body.Bytes(), &page); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if page.Total != 0 || page.Events == nil {
		t.Fatalf("empty events must be non-nil empty: %+v", page)
	}
}
