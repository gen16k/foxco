package inference

import (
	"context"
	"regexp"

	"promptgate/internal/dlp"
)

// KeywordClassifier is a deterministic stand-in for the LFM, used when no model
// is available (CI, local dev, demos without a GGUF downloaded). It is NOT the
// product's classifier — the real build uses LlamaClient — but it lets the full
// proxy pipeline run and be tested end-to-end. It implements dlp.Classifier.
type KeywordClassifier struct {
	patterns []namedPattern
}

type namedPattern struct {
	reason string
	re     *regexp.Regexp
}

// NewKeywordClassifier returns a fallback classifier with coarse heuristics.
func NewKeywordClassifier() *KeywordClassifier {
	return &KeywordClassifier{patterns: []namedPattern{
		{"password or credential keyword", regexp.MustCompile(`(?i)\b(pass(word|wd)|secret|credential|api[_-]?key|access[_-]?token)\b`)},
		{"private key material", regexp.MustCompile(`(?i)-----BEGIN .*PRIVATE KEY-----`)},
		{"environment/config file content", regexp.MustCompile(`(?i)(^|\n)\s*[A-Z][A-Z0-9_]{2,}\s*=\s*\S`)},
		{"email address", regexp.MustCompile(`[A-Za-z0-9._%+\-]+@[A-Za-z0-9.\-]+\.[A-Za-z]{2,}`)},
		{"internal/private IP address", regexp.MustCompile(`\b(10|127)\.\d{1,3}\.\d{1,3}\.\d{1,3}\b|\b192\.168\.\d{1,3}\.\d{1,3}\b|\b172\.(1[6-9]|2\d|3[01])\.\d{1,3}\.\d{1,3}\b`)},
	}}
}

// Classify flags the text if any heuristic matches.
func (k *KeywordClassifier) Classify(_ context.Context, in dlp.ClassifyInput) (dlp.ClassifyOutput, error) {
	for _, p := range k.patterns {
		if p.re.MatchString(in.Text) {
			return dlp.ClassifyOutput{NG: true, ShortReason: p.reason}, nil
		}
	}
	return dlp.ClassifyOutput{NG: false}, nil
}
