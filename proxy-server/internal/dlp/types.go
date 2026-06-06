// Package dlp implements the data-loss-prevention pipeline: normalization,
// segmentation, a deterministic rule guardrail, the LFM classifier contract,
// a fingerprint cache, and the policy that turns detections into ALLOW/BLOCK.
package dlp

import "context"

// Decision is the live action for a request or the cached verdict of a segment.
type Decision string

const (
	Allow Decision = "ALLOW"
	Block Decision = "BLOCK"
)

// SegmentType labels what kind of content a segment carries. It is passed to
// the LFM as a light metadata hint (we never send the whole conversation).
type SegmentType string

const (
	SegUserText   SegmentType = "user_text"
	SegToolResult SegmentType = "tool_result"
)

// Segment is one logically atomic unit of text to classify, plus enough
// location information for the sanitizer to remove the unit it belongs to.
type Segment struct {
	Type       SegmentType
	Text       string // normalized text used for classification + fingerprint
	MsgIndex   int    // index into the messages array
	BlockIndex int    // index into the message's content blocks, -1 if string content
	ToolUseID  string // tool_use_id, set for tool_result segments
	Role       string // role of the containing message
}

// Result is the outcome of classifying a single segment.
type Result struct {
	Decision Decision
	Reason   string // human-readable, safe to surface (never contains the secret)
	Source   string // "rule", "lfm", or "classifier_unavailable"
}

// ClassifyInput is what the proxy hands the LFM classifier.
type ClassifyInput struct {
	SegmentType string
	Text        string
}

// ClassifyOutput is the binary verdict the LFM returns.
type ClassifyOutput struct {
	NG          bool   `json:"ng"`
	ShortReason string `json:"short_reason"`
}

// Classifier is the LFM (or mock) binary NG/OK classifier. Implementations live
// in internal/inference (llama.cpp sidecar) and in this package (mock).
type Classifier interface {
	Classify(ctx context.Context, in ClassifyInput) (ClassifyOutput, error)
}
