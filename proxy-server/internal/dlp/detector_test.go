package dlp

import (
	"context"
	"errors"
	"testing"
)

// stubClassifier flags any segment whose text contains a marker substring.
type stubClassifier struct {
	flag  string
	err   error
	calls int
}

func (s *stubClassifier) Classify(_ context.Context, in ClassifyInput) (ClassifyOutput, error) {
	s.calls++
	if s.err != nil {
		return ClassifyOutput{}, s.err
	}
	if s.flag != "" && contains(in.Text, s.flag) {
		return ClassifyOutput{NG: true, ShortReason: "looks sensitive"}, nil
	}
	return ClassifyOutput{NG: false}, nil
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func newDet(c Classifier, failClosed bool) *Detector {
	return NewDetector(NewRuleEngine(), true, c, NewCache(16), failClosed)
}

func TestDetectorRuleShortCircuitsWithoutLFM(t *testing.T) {
	stub := &stubClassifier{}
	d := newDet(stub, true)
	r := d.Classify(context.Background(), Segment{Type: SegToolResult, Text: "AKIAIOSFODNN7EXAMPLE"})
	if r.Decision != Block || r.Source != "rule" {
		t.Fatalf("want rule block, got %+v", r)
	}
	if stub.calls != 0 {
		t.Fatalf("LFM should not be called when a rule fires, calls=%d", stub.calls)
	}
}

func TestDetectorCachesAndSkipsSecondCall(t *testing.T) {
	stub := &stubClassifier{flag: "topsecret"}
	d := newDet(stub, true)
	seg := Segment{Type: SegUserText, Text: "this is topsecret data"}
	_ = d.Classify(context.Background(), seg)
	_ = d.Classify(context.Background(), seg)
	if stub.calls != 1 {
		t.Fatalf("expected 1 LFM call due to cache, got %d", stub.calls)
	}
}

func TestDetectorFailClosed(t *testing.T) {
	d := newDet(&stubClassifier{err: errors.New("down")}, true)
	r := d.Classify(context.Background(), Segment{Type: SegUserText, Text: "harmless"})
	if r.Decision != Block || r.Source != "classifier_unavailable" {
		t.Fatalf("fail-closed should block, got %+v", r)
	}
}

func TestDetectorFailOpen(t *testing.T) {
	d := newDet(&stubClassifier{err: errors.New("down")}, false)
	r := d.Classify(context.Background(), Segment{Type: SegUserText, Text: "harmless"})
	if r.Decision != Allow {
		t.Fatalf("fail-open should allow, got %+v", r)
	}
}

func TestEvaluateLiveBlock(t *testing.T) {
	d := newDet(&stubClassifier{flag: "secretz"}, true)
	segs := []Segment{
		{Type: SegUserText, Text: "old safe message", MsgIndex: 0},
		{Type: SegUserText, Text: "now sending secretz", MsgIndex: 2},
	}
	ev := d.Evaluate(context.Background(), segs, 2)
	if !ev.Block {
		t.Fatalf("live secret should block, got %+v", ev)
	}
}

func TestEvaluateHistoryNG(t *testing.T) {
	d := newDet(&stubClassifier{flag: "secretz"}, true)
	segs := []Segment{
		{Type: SegToolResult, Text: "earlier secretz leak", MsgIndex: 0, ToolUseID: "tu_1"},
		{Type: SegUserText, Text: "a safe new question", MsgIndex: 4},
	}
	ev := d.Evaluate(context.Background(), segs, 4)
	if ev.Block {
		t.Fatalf("live turn is safe, should not block: %+v", ev)
	}
	if len(ev.HistoryNG) != 1 || ev.HistoryNG[0].ToolUseID != "tu_1" {
		t.Fatalf("expected 1 history NG (tu_1), got %+v", ev.HistoryNG)
	}
}
