package dlp

import (
	"context"
	"log/slog"
)

// Detector orchestrates the per-segment decision: deterministic rule guardrail
// first (cheap, high-precision, short-circuits to BLOCK), then the LFM
// classifier, with a fingerprint cache in front of both. On classifier failure
// it fails closed (BLOCK) when configured to.
type Detector struct {
	rules       *RuleEngine
	ruleEnabled bool
	lfm         Classifier
	cache       *Cache
	failClosed  bool
	logger      *slog.Logger
}

// SetLogger attaches a logger so classifier errors are surfaced (otherwise they
// are silently treated as fail-closed BLOCKs).
func (d *Detector) SetLogger(l *slog.Logger) { d.logger = l }

// NewDetector wires the guardrail, classifier, and cache together.
func NewDetector(rules *RuleEngine, ruleEnabled bool, lfm Classifier, cache *Cache, failClosed bool) *Detector {
	return &Detector{rules: rules, ruleEnabled: ruleEnabled, lfm: lfm, cache: cache, failClosed: failClosed}
}

// Classify returns the decision for one segment, consulting the cache first.
// A "classifier_unavailable" result is never cached (the condition is transient).
func (d *Detector) Classify(ctx context.Context, seg Segment) Result {
	key := Fingerprint(seg.Text)
	if r, ok := d.cache.Get(key); ok {
		return r
	}

	if d.ruleEnabled && d.rules != nil {
		if name, span, hit := d.rules.MatchSpan(seg.Text); hit {
			r := Result{Decision: Block, Reason: "secret detected (" + name + ")", Source: "rule", Match: span}
			d.cache.Put(key, r)
			return r
		}
	}

	out, err := d.lfm.Classify(ctx, ClassifyInput{SegmentType: string(seg.Type), Text: seg.Text})
	if err != nil {
		if d.logger != nil {
			d.logger.Warn("classify_error", "err", err.Error(), "seg_type", string(seg.Type), "text_len", len(seg.Text))
		}
		if d.failClosed {
			// transient: do not cache.
			return Result{Decision: Block, Reason: "classifier unavailable", Source: "classifier_unavailable"}
		}
		return Result{Decision: Allow, Source: "classifier_unavailable"}
	}

	r := Result{Decision: Allow, Source: "lfm"}
	if out.NG {
		r.Decision = Block
		r.Reason = out.ShortReason
		// The LFM gives a verdict, not a span, so the whole flagged segment is
		// the best available "offending text" for highlighting.
		r.Match = seg.Text
	}
	d.cache.Put(key, r)
	return r
}

// Evaluation is the request-level outcome.
type Evaluation struct {
	Block       bool      // the live (latest) turn must be blocked
	BlockReason string    // safe-to-surface reason for the block
	BlockSource string    // "rule" | "lfm" | "classifier_unavailable"
	BlockMatch  string    // offending text of the live block (raw; opt-in storage only)
	HistoryNG   []Segment // sensitive history segments to sanitize before forwarding
}

// Evaluate classifies the live turn first (the segments belonging to the last
// message); if it is sensitive the request is blocked. Otherwise it classifies
// the history and reports which segments must be removed before forwarding.
func (d *Detector) Evaluate(ctx context.Context, segs []Segment, lastMsgIndex int) Evaluation {
	var live, hist []Segment
	for _, s := range segs {
		if s.MsgIndex == lastMsgIndex {
			live = append(live, s)
		} else {
			hist = append(hist, s)
		}
	}

	for _, s := range live {
		if r := d.Classify(ctx, s); r.Decision == Block {
			return Evaluation{Block: true, BlockReason: r.Reason, BlockSource: r.Source, BlockMatch: r.Match}
		}
	}

	var ng []Segment
	for _, s := range hist {
		if r := d.Classify(ctx, s); r.Decision == Block {
			ng = append(ng, s)
		}
	}
	return Evaluation{HistoryNG: ng}
}
