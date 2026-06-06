package storage

import (
	"fmt"
	"path/filepath"
	"testing"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	st, err := Open(filepath.Join(t.TempDir(), "dlp.db"), 0)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

func ptr(s string) *string { return &s }

// seed inserts a deterministic mix of events for query/stats assertions.
func seed(t *testing.T, st *Store) {
	t.Helper()
	rows := []AuditEvent{
		{EventID: "a1", CreatedAt: "2026-06-06T10:00:00Z", EventType: "request", Decision: "ALLOW", LatencyMS: 100, ModelName: "LFM2.5-1.2B", Backend: "keyword", UpstreamCalled: true, Details: `{"upstream_status":200}`, Path: "/v1/messages", PromptText: ptr("please refactor my code")},
		{EventID: "b1", CreatedAt: "2026-06-06T10:05:00Z", EventType: "request", Decision: "BLOCK", LatencyMS: 200, ModelName: "LFM2.5-1.2B", Backend: "keyword", UpstreamCalled: false, Details: `{"reason":"secret detected (aws_access_key)","source":"rule"}`, Path: "/v1/messages", PromptText: ptr("here is AKIAIOSFODNN7EXAMPLE")},
		{EventID: "b2", CreatedAt: "2026-06-06T10:06:00Z", EventType: "request", Decision: "BLOCK", LatencyMS: 300, ModelName: "LFM2.5-1.2B", Backend: "lfm", UpstreamCalled: false, Details: `{"reason":"contains a real password","source":"lfm"}`, Path: "/v1/messages"},
		{EventID: "b3", CreatedAt: "2026-06-06T10:07:00Z", EventType: "request", Decision: "BLOCK", LatencyMS: 50, ModelName: "LFM2.5-1.2B", Backend: "keyword", UpstreamCalled: false, Details: `{"reason":"secret detected (aws_access_key)","source":"rule"}`, Path: "/v1/messages"},
		{EventID: "a2", CreatedAt: "2026-06-06T10:08:00Z", EventType: "request", Decision: "ALLOW", LatencyMS: 400, ModelName: "LFM2.5-1.2B", Backend: "keyword", UpstreamCalled: true, Details: `{"upstream_status":200}`, Path: "/v1/messages/count_tokens"},
	}
	for _, r := range rows {
		if err := st.Record(r); err != nil {
			t.Fatalf("seed %s: %v", r.EventID, err)
		}
	}
}

func TestQueryNewestFirstAndPagination(t *testing.T) {
	st := newTestStore(t)
	seed(t, st)

	page, err := st.Query(EventFilter{Limit: 2, Offset: 0})
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if page.Total != 5 {
		t.Fatalf("total = %d, want 5", page.Total)
	}
	if len(page.Events) != 2 {
		t.Fatalf("len = %d, want 2", len(page.Events))
	}
	if page.Events[0].EventID != "a2" || page.Events[1].EventID != "b3" {
		t.Fatalf("newest-first wrong: %s,%s", page.Events[0].EventID, page.Events[1].EventID)
	}
	p2, _ := st.Query(EventFilter{Limit: 2, Offset: 2})
	if p2.Events[0].EventID != "b2" || p2.Events[1].EventID != "b1" {
		t.Fatalf("page2 wrong: %s,%s", p2.Events[0].EventID, p2.Events[1].EventID)
	}
}

func TestQueryDecisionAndSourceFilters(t *testing.T) {
	st := newTestStore(t)
	seed(t, st)

	blocks, _ := st.Query(EventFilter{Decision: "BLOCK"})
	if blocks.Total != 3 {
		t.Fatalf("blocks total = %d, want 3", blocks.Total)
	}
	rule, _ := st.Query(EventFilter{Source: "rule"})
	if rule.Total != 2 {
		t.Fatalf("rule total = %d, want 2", rule.Total)
	}
	lfm, _ := st.Query(EventFilter{Source: "lfm"})
	if lfm.Total != 1 || lfm.Events[0].EventID != "b2" {
		t.Fatalf("lfm filter wrong: total=%d", lfm.Total)
	}
}

func TestQueryFreeTextOverReasonAndPrompt(t *testing.T) {
	st := newTestStore(t)
	seed(t, st)

	// Matches a reason ("password").
	pw, _ := st.Query(EventFilter{Q: "password"})
	if pw.Total != 1 || pw.Events[0].EventID != "b2" {
		t.Fatalf("q=password wrong: total=%d", pw.Total)
	}
	// Matches prompt_text only.
	ak, _ := st.Query(EventFilter{Q: "AKIAIOSFODNN7EXAMPLE"})
	if ak.Total != 1 || ak.Events[0].EventID != "b1" {
		t.Fatalf("q=AKIA wrong: total=%d", ak.Total)
	}
}

func TestQueryDateRange(t *testing.T) {
	st := newTestStore(t)
	seed(t, st)
	page, _ := st.Query(EventFilter{From: "2026-06-06T10:06:00Z", To: "2026-06-06T10:07:30Z"})
	if page.Total != 2 {
		t.Fatalf("range total = %d, want 2 (b2,b3)", page.Total)
	}
}

func TestQueryInjectionSafe(t *testing.T) {
	st := newTestStore(t)
	seed(t, st)
	// A malicious q must be treated as a literal substring, not SQL.
	page, err := st.Query(EventFilter{Q: "'; DROP TABLE audit_events; --"})
	if err != nil {
		t.Fatalf("query errored on injection input: %v", err)
	}
	if page.Total != 0 {
		t.Fatalf("injection q matched %d rows, want 0", page.Total)
	}
	// Table must still exist / be queryable.
	if all, err := st.Query(EventFilter{}); err != nil || all.Total != 5 {
		t.Fatalf("table damaged after injection attempt: total=%d err=%v", all.Total, err)
	}
}

func TestGetFoundAndNotFound(t *testing.T) {
	st := newTestStore(t)
	seed(t, st)
	row, ok, err := st.Get("b1")
	if err != nil || !ok {
		t.Fatalf("get b1: ok=%v err=%v", ok, err)
	}
	if row.Source != "rule" || row.PromptText == nil {
		t.Fatalf("b1 row wrong: %+v", row)
	}
	if _, ok, _ := st.Get("nope"); ok {
		t.Fatal("expected not found")
	}
}

func TestStatsAggregates(t *testing.T) {
	st := newTestStore(t)
	seed(t, st)
	s, err := st.Stats("", "")
	if err != nil {
		t.Fatalf("stats: %v", err)
	}
	if s.Total != 5 || s.Blocked != 3 || s.Allowed != 2 {
		t.Fatalf("counts wrong: %+v", s)
	}
	if s.UpstreamCalled != 2 {
		t.Fatalf("upstreamCalled = %d, want 2", s.UpstreamCalled)
	}
	if s.BlockRate < 0.59 || s.BlockRate > 0.61 {
		t.Fatalf("blockRate = %f, want ~0.6", s.BlockRate)
	}
	if s.BySource["rule"] != 2 || s.BySource["lfm"] != 1 {
		t.Fatalf("bySource wrong: %+v", s.BySource)
	}
	// latencies: 100,200,300,50,400 -> avg 210, p95 = sorted[4] = 400.
	if s.AvgLatencyMS < 209 || s.AvgLatencyMS > 211 {
		t.Fatalf("avg = %f, want 210", s.AvgLatencyMS)
	}
	if s.P95LatencyMS != 400 {
		t.Fatalf("p95 = %d, want 400", s.P95LatencyMS)
	}
	if len(s.TopReasons) == 0 || s.TopReasons[0].Reason != "secret detected (aws_access_key)" || s.TopReasons[0].Count != 2 {
		t.Fatalf("topReasons wrong: %+v", s.TopReasons)
	}
	if len(s.Series) == 0 {
		t.Fatal("series empty")
	}
	var allow, block int
	for _, b := range s.Series {
		allow += b.Allow
		block += b.Block
	}
	if allow != 2 || block != 3 {
		t.Fatalf("series totals wrong: allow=%d block=%d", allow, block)
	}
}

func TestStatsEmptyDB(t *testing.T) {
	st := newTestStore(t)
	s, err := st.Stats("", "")
	if err != nil {
		t.Fatalf("stats: %v", err)
	}
	if s.Total != 0 || s.BlockRate != 0 || s.P95LatencyMS != 0 {
		t.Fatalf("empty stats not zeroed: %+v", s)
	}
	if s.BySource == nil || s.TopReasons == nil || s.Series == nil {
		t.Fatal("empty stats must use non-nil maps/slices for clean JSON")
	}
	page, _ := st.Query(EventFilter{})
	if page.Total != 0 || page.Events == nil {
		t.Fatalf("empty query must return non-nil empty events: %+v", page)
	}
}

func TestQueryLimitClamp(t *testing.T) {
	st := newTestStore(t)
	for i := 0; i < 12; i++ {
		_ = st.Record(AuditEvent{
			EventID: fmt.Sprintf("e%02d", i), CreatedAt: fmt.Sprintf("2026-06-06T10:%02d:00Z", i),
			EventType: "request", Decision: "ALLOW", Details: `{"upstream_status":200}`,
		})
	}
	// Default limit is 50; all 12 returned.
	page, _ := st.Query(EventFilter{})
	if len(page.Events) != 12 {
		t.Fatalf("default limit returned %d, want 12", len(page.Events))
	}
}
