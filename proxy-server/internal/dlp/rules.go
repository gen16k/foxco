package dlp

import "regexp"

// rule is one deterministic secret pattern. Names are surfaced in the block
// reason (e.g. "rule:aws_access_key") but never the matched value.
type rule struct {
	name string
	re   *regexp.Regexp
}

// RuleEngine is the pluggable deterministic guardrail. It is intentionally
// high-precision (only unambiguous credential shapes) so it is safe to BLOCK on
// a match without the LFM. It is the concrete first implementation of the
// "rule layer" the design leaves room for; the LFM remains the primary
// classifier for everything else.
type RuleEngine struct {
	rules []rule
}

// NewRuleEngine returns the default guardrail covering high-confidence secrets.
func NewRuleEngine() *RuleEngine {
	return &RuleEngine{rules: []rule{
		{"private_key_block", regexp.MustCompile(`-----BEGIN (?:RSA |EC |OPENSSH |PGP |DSA )?PRIVATE KEY-----`)},
		{"aws_access_key", regexp.MustCompile(`\b(?:AKIA|ASIA)[0-9A-Z]{16}\b`)},
		{"anthropic_key", regexp.MustCompile(`\bsk-ant-[A-Za-z0-9_\-]{20,}`)},
		{"openai_key", regexp.MustCompile(`\bsk-(?:proj-)?[A-Za-z0-9_\-]{20,}`)},
		{"google_api_key", regexp.MustCompile(`\bAIza[0-9A-Za-z_\-]{35}\b`)},
		{"github_token", regexp.MustCompile(`\bgh[pousr]_[A-Za-z0-9]{36,}\b`)},
		{"slack_token", regexp.MustCompile(`\bxox[baprs]-[A-Za-z0-9\-]{10,}`)},
		{"gcp_sa_key", regexp.MustCompile(`"type"\s*:\s*"service_account"`)},
		{"jwt", regexp.MustCompile(`\beyJ[A-Za-z0-9_\-]{10,}\.[A-Za-z0-9_\-]{10,}\.[A-Za-z0-9_\-]{10,}`)},
	}}
}

// Match reports whether any rule fires, returning the first rule's name.
func (e *RuleEngine) Match(text string) (string, bool) {
	name, _, hit := e.MatchSpan(text)
	return name, hit
}

// MatchSpan is like Match but also returns the exact matched substring (the
// offending value). The span is the precise text the highlight UI marks; it is
// the secret itself, so callers must persist it ONLY under the opt-in raw-text
// gate and must never place it in a block reason (the reason carries the rule
// name only).
func (e *RuleEngine) MatchSpan(text string) (name, span string, hit bool) {
	for _, r := range e.rules {
		if loc := r.re.FindStringIndex(text); loc != nil {
			return r.name, text[loc[0]:loc[1]], true
		}
	}
	return "", "", false
}
