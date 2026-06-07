package inference

import "testing"

// These tests pin the reason_decision parser's contract: the verdict (NG) is
// driven ONLY by the JSON `decision` field, and the human-readable reason is
// passed through verbatim from the JSON `reason` field. This is the evidence that
// a BLOCK whose reason reads as benign (e.g. "inert data with no sensitive
// information") is the MODEL contradicting itself — not the parser misreading the
// decision. If the model had emitted decision:"ALLOW", the parser would allow.
func TestParseReasonDecisionIsFaithfulToDecisionField(t *testing.T) {
	cases := []struct {
		name    string
		content string
		wantNG  bool
		wantRsn string
	}{
		{
			// Exactly the shape observed in the field: benign reason, BLOCK verdict.
			// Proves the block came from decision=="BLOCK", and the displayed reason
			// is the model's own `reason` (i.e. JSON parsed successfully).
			name:    "benign reason but decision BLOCK -> NG (model self-contradiction)",
			content: `{"reason":"inert data with no sensitive information","decision":"BLOCK"}`,
			wantNG:  true,
			wantRsn: "inert data with no sensitive information",
		},
		{
			// Same reason, ALLOW verdict -> ALLOW. So the parser is NOT forcing BLOCK;
			// had the model said ALLOW, the turn would have been allowed.
			name:    "benign reason with decision ALLOW -> allowed",
			content: `{"reason":"inert data with no sensitive information","decision":"ALLOW"}`,
			wantNG:  false,
			wantRsn: "inert data with no sensitive information",
		},
		{
			name:    "real secret -> NG",
			content: `{"reason":"contains a real password","decision":"BLOCK"}`,
			wantNG:  true,
			wantRsn: "contains a real password",
		},
		{
			name:    "lowercase/whitespace decision is normalized",
			content: "{\"reason\":\"general question\",\"decision\":\" allow \"}",
			wantNG:  false,
			wantRsn: "general question",
		},
		{
			name:    "prose around the JSON is tolerated",
			content: "Sure, here is the verdict:\n{\"reason\":\"ordinary code task\",\"decision\":\"ALLOW\"}\nDone.",
			wantNG:  false,
			wantRsn: "ordinary code task",
		},
		{
			name:    "bare ALLOW token (schema not enforced) -> allowed",
			content: `ALLOW`,
			wantNG:  false,
		},
		{
			name:    "bare BLOCK token (schema not enforced) -> NG",
			content: `BLOCK`,
			wantNG:  true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, err := parseReasonDecision(tc.content)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if out.NG != tc.wantNG {
				t.Errorf("NG = %v, want %v (content=%q)", out.NG, tc.wantNG, tc.content)
			}
			if tc.wantRsn != "" && out.ShortReason != tc.wantRsn {
				t.Errorf("ShortReason = %q, want %q", out.ShortReason, tc.wantRsn)
			}
		})
	}
}

// An ambiguous (both tokens) or empty verdict must be an error so the caller
// fails closed — and crucially yields a GENERIC reason, never a benign-looking
// one. So a benign-looking reason in a block could only have come from a cleanly
// parsed JSON object (above), reinforcing that such blocks are the model's doing.
func TestParseReasonDecisionAmbiguousIsError(t *testing.T) {
	for _, c := range []string{``, `maybe`, `ALLOW or BLOCK?`, `hmm not sure`} {
		if _, err := parseReasonDecision(c); err == nil {
			t.Errorf("content %q: expected error (fail closed), got nil", c)
		}
	}
}
