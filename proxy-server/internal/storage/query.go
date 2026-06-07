package storage

import (
	"database/sql"
	"encoding/json"
	"sort"
	"strings"
	"time"
)

// EventFilter selects and pages audit events for the admin API. All string
// values are bound as SQL parameters; only the WHERE *shape* is dynamic.
type EventFilter struct {
	From     string // RFC3339 lower bound (inclusive); empty = unbounded
	To       string // RFC3339 upper bound (inclusive); empty = unbounded
	Decision string // "ALLOW" | "BLOCK" | ""
	Source   string // rule|lfm|classifier_unavailable|proxy|sanitizer | ""
	Q        string // case-insensitive substring over reason / prompt_text / matched_snippet
	Limit    int    // clamped to 1..500 (default 50)
	Offset   int    // clamped to >= 0
}

// EventRow is one audit event as exposed by the admin API. JSON tags are the
// admin API contract (camelCase) consumed by the Next.js BFF. promptText /
// matchedSnippet are null unless store_raw_text was enabled when recorded.
type EventRow struct {
	EventID        string  `json:"eventId"`
	CreatedAt      string  `json:"createdAt"`
	Decision       string  `json:"decision"`
	Source         string  `json:"source"`
	Reason         string  `json:"reason"`
	LatencyMS      int64   `json:"latencyMs"`
	ModelName      string  `json:"modelName"`
	Backend        string  `json:"backend"`
	UpstreamCalled bool    `json:"upstreamCalled"`
	Path           string  `json:"path"`
	PromptText     *string `json:"promptText"`
	MatchedSnippet *string `json:"matchedSnippet"`
}

// EventPage is a filtered, paginated slice plus the unpaginated total.
type EventPage struct {
	Total  int        `json:"total"`
	Events []EventRow `json:"events"`
}

// ReasonCount is one entry in the top-block-reasons breakdown.
type ReasonCount struct {
	Reason string `json:"reason"`
	Count  int    `json:"count"`
}

// Bucket is one time-series point (allow vs block counts in a time bucket).
type Bucket struct {
	Ts    string `json:"ts"` // RFC3339 bucket start (UTC)
	Allow int    `json:"allow"`
	Block int    `json:"block"`
}

// Stats is the aggregate dashboard payload for a time range.
type Stats struct {
	Total          int            `json:"total"`
	Blocked        int            `json:"blocked"`
	Allowed        int            `json:"allowed"`
	BlockRate      float64        `json:"blockRate"` // 0..1
	UpstreamCalled int            `json:"upstreamCalled"`
	BySource       map[string]int `json:"bySource"`   // block sources
	TopReasons     []ReasonCount  `json:"topReasons"` // block reasons, desc
	AvgLatencyMS   float64        `json:"avgLatencyMs"`
	P95LatencyMS   int64          `json:"p95LatencyMs"`
	Series         []Bucket       `json:"series"`
}

// detailsJSON is the safe metadata blob stored per event.
type detailsJSON struct {
	Reason         string `json:"reason"`
	Source         string `json:"source"`
	UpstreamStatus int    `json:"upstream_status"`
}

// Query returns a filtered, newest-first page plus the total match count.
func (s *Store) Query(f EventFilter) (EventPage, error) {
	where, args := buildWhere(f)

	page := EventPage{Events: []EventRow{}}
	countQ := `SELECT COUNT(*) FROM audit_events` + where
	if err := s.db.QueryRow(countQ, args...).Scan(&page.Total); err != nil {
		return page, err
	}

	limit := f.Limit
	if limit <= 0 {
		limit = 50
	}
	if limit > 500 {
		limit = 500
	}
	offset := f.Offset
	if offset < 0 {
		offset = 0
	}

	listQ := `SELECT event_id, created_at, decision, latency_ms, model_name, backend, upstream_called, details_json, path, prompt_text, matched_snippet
	          FROM audit_events` + where + `
	          ORDER BY created_at DESC, event_id DESC LIMIT ? OFFSET ?`
	rows, err := s.db.Query(listQ, append(append([]any{}, args...), limit, offset)...)
	if err != nil {
		return page, err
	}
	defer rows.Close()
	for rows.Next() {
		row, err := scanRow(rows)
		if err != nil {
			return page, err
		}
		page.Events = append(page.Events, row)
	}
	return page, rows.Err()
}

// Get returns a single event by id. ok is false when not found.
func (s *Store) Get(id string) (EventRow, bool, error) {
	q := `SELECT event_id, created_at, decision, latency_ms, model_name, backend, upstream_called, details_json, path, prompt_text, matched_snippet
	      FROM audit_events WHERE event_id = ?`
	row, err := scanRow(s.db.QueryRow(q, id))
	if err == sql.ErrNoRows {
		return EventRow{}, false, nil
	}
	if err != nil {
		return EventRow{}, false, err
	}
	return row, true, nil
}

// Stats computes the aggregate dashboard payload over an optional [from,to]
// range. Aggregation (by_source, top_reasons, p95, time buckets) is done in Go:
// the dataset is local-scale and this avoids depending on SQLite extensions.
func (s *Store) Stats(from, to string) (Stats, error) {
	st := Stats{BySource: map[string]int{}, TopReasons: []ReasonCount{}, Series: []Bucket{}}

	var where string
	var args []any
	if from != "" {
		where += ` AND created_at >= ?`
		args = append(args, from)
	}
	if to != "" {
		where += ` AND created_at <= ?`
		args = append(args, to)
	}
	if where != "" {
		where = ` WHERE` + strings.TrimPrefix(where, ` AND`)
	}

	q := `SELECT created_at, decision, latency_ms, upstream_called, details_json FROM audit_events` + where
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return st, err
	}
	defer rows.Close()

	var latencies []int64
	reasonCounts := map[string]int{}
	var pts []seriesPoint

	for rows.Next() {
		var createdAt, decision, details string
		var latency int64
		var upstream int
		if err := rows.Scan(&createdAt, &decision, &latency, &upstream, &details); err != nil {
			return st, err
		}
		st.Total++
		if upstream != 0 {
			st.UpstreamCalled++
		}
		latencies = append(latencies, latency)

		var d detailsJSON
		_ = json.Unmarshal([]byte(details), &d)

		block := decision == "BLOCK"
		if block {
			st.Blocked++
			src := d.Source
			if src == "" {
				src = "other"
			}
			st.BySource[src]++
			if d.Reason != "" {
				reasonCounts[d.Reason]++
			}
		} else {
			st.Allowed++
		}

		if t, perr := time.Parse(time.RFC3339, createdAt); perr == nil {
			pts = append(pts, seriesPoint{t: t.UTC(), block: block})
		}
	}
	if err := rows.Err(); err != nil {
		return st, err
	}

	if st.Total > 0 {
		st.BlockRate = float64(st.Blocked) / float64(st.Total)
	}
	st.AvgLatencyMS = avg(latencies)
	st.P95LatencyMS = percentile(latencies, 0.95)

	// Top reasons, descending by count then reason for stable ordering.
	for r, c := range reasonCounts {
		st.TopReasons = append(st.TopReasons, ReasonCount{Reason: r, Count: c})
	}
	sort.Slice(st.TopReasons, func(i, j int) bool {
		if st.TopReasons[i].Count != st.TopReasons[j].Count {
			return st.TopReasons[i].Count > st.TopReasons[j].Count
		}
		return st.TopReasons[i].Reason < st.TopReasons[j].Reason
	})
	if len(st.TopReasons) > 15 {
		st.TopReasons = st.TopReasons[:15]
	}

	st.Series = buildSeries(pts, from, to)
	return st, nil
}

// buildWhere assembles a parameterized WHERE clause from the filter. Every user
// value is bound (?), so the dynamic shape is injection-safe.
func buildWhere(f EventFilter) (string, []any) {
	var conds []string
	var args []any
	if f.From != "" {
		conds = append(conds, "created_at >= ?")
		args = append(args, f.From)
	}
	if f.To != "" {
		conds = append(conds, "created_at <= ?")
		args = append(args, f.To)
	}
	if f.Decision != "" {
		conds = append(conds, "decision = ?")
		args = append(args, f.Decision)
	}
	if f.Source != "" {
		conds = append(conds, `details_json LIKE ? ESCAPE '\'`)
		args = append(args, `%"source":"`+escapeLike(f.Source)+`"%`)
	}
	if f.Q != "" {
		conds = append(conds, `(details_json LIKE ? ESCAPE '\' OR prompt_text LIKE ? ESCAPE '\' OR matched_snippet LIKE ? ESCAPE '\')`)
		like := "%" + escapeLike(f.Q) + "%"
		args = append(args, like, like, like)
	}
	if len(conds) == 0 {
		return "", nil
	}
	return " WHERE " + strings.Join(conds, " AND "), args
}

// rowScanner is satisfied by *sql.Row and *sql.Rows.
type rowScanner interface {
	Scan(dest ...any) error
}

func scanRow(sc rowScanner) (EventRow, error) {
	var (
		row             EventRow
		details         string
		path            sql.NullString
		prompt, snippet sql.NullString
		upstream        int
	)
	if err := sc.Scan(&row.EventID, &row.CreatedAt, &row.Decision, &row.LatencyMS,
		&row.ModelName, &row.Backend, &upstream, &details, &path, &prompt, &snippet); err != nil {
		return EventRow{}, err
	}
	row.UpstreamCalled = upstream != 0
	row.Path = path.String
	var d detailsJSON
	_ = json.Unmarshal([]byte(details), &d)
	row.Reason = d.Reason
	row.Source = d.Source
	if prompt.Valid {
		v := prompt.String
		row.PromptText = &v
	}
	if snippet.Valid {
		v := snippet.String
		row.MatchedSnippet = &v
	}
	return row, nil
}

func escapeLike(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r == '\\' || r == '%' || r == '_' {
			b.WriteByte('\\')
		}
		b.WriteRune(r)
	}
	return b.String()
}

func avg(xs []int64) float64 {
	if len(xs) == 0 {
		return 0
	}
	var sum int64
	for _, x := range xs {
		sum += x
	}
	return float64(sum) / float64(len(xs))
}

func percentile(xs []int64, p float64) int64 {
	if len(xs) == 0 {
		return 0
	}
	sorted := append([]int64{}, xs...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	idx := int(float64(len(sorted))*p+0.999999) - 1 // ceil(p*n)-1
	if idx < 0 {
		idx = 0
	}
	if idx >= len(sorted) {
		idx = len(sorted) - 1
	}
	return sorted[idx]
}

// seriesPoint is one event reduced to (time, was-blocked) for bucketing.
type seriesPoint struct {
	t     time.Time
	block bool
}

// buildSeries buckets events into an ordered allow/block time series. Bucket
// width scales with the span (hour / day / week). Empty buckets within the
// window are emitted as zeros so charts render a continuous axis.
func buildSeries(pts []seriesPoint, from, to string) []Bucket {
	if len(pts) == 0 {
		return []Bucket{}
	}
	minT, maxT := pts[0].t, pts[0].t
	for _, p := range pts {
		if p.t.Before(minT) {
			minT = p.t
		}
		if p.t.After(maxT) {
			maxT = p.t
		}
	}
	lo, hi := minT, maxT
	if t, err := time.Parse(time.RFC3339, from); err == nil {
		lo = t.UTC()
	}
	if t, err := time.Parse(time.RFC3339, to); err == nil {
		hi = t.UTC()
	}
	if hi.Before(lo) {
		lo, hi = hi, lo
	}

	bucket := bucketDuration(hi.Sub(lo))
	start := lo.Truncate(bucket)
	end := hi.Truncate(bucket)

	counts := map[int64]*Bucket{}
	for _, p := range pts {
		key := p.t.Truncate(bucket).Unix()
		b := counts[key]
		if b == nil {
			b = &Bucket{Ts: time.Unix(key, 0).UTC().Format(time.RFC3339)}
			counts[key] = b
		}
		if p.block {
			b.Block++
		} else {
			b.Allow++
		}
	}

	var out []Bucket
	const maxBuckets = 2000
	n := 0
	for t := start; !t.After(end) && n < maxBuckets; t = t.Add(bucket) {
		key := t.Unix()
		if b, ok := counts[key]; ok {
			out = append(out, *b)
		} else {
			out = append(out, Bucket{Ts: t.UTC().Format(time.RFC3339)})
		}
		n++
	}
	if out == nil {
		out = []Bucket{}
	}
	return out
}

func bucketDuration(span time.Duration) time.Duration {
	switch {
	case span <= 48*time.Hour:
		return time.Hour
	case span <= 60*24*time.Hour:
		return 24 * time.Hour
	default:
		return 7 * 24 * time.Hour
	}
}
