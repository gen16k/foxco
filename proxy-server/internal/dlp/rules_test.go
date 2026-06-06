package dlp

import "testing"

func TestRuleEngineMatches(t *testing.T) {
	e := NewRuleEngine()
	cases := []struct {
		text string
		want string
	}{
		{"AWS_ACCESS_KEY_ID=AKIAIOSFODNN7EXAMPLE", "aws_access_key"},
		{"-----BEGIN OPENSSH PRIVATE KEY-----\nabc", "private_key_block"},
		// The credential test vectors are split with + so the source file does
		// not contain a contiguous match (GitHub secret-scanning push
		// protection), while the runtime string the regex sees is unchanged.
		{"key: AIza" + "SyDdI0hCZtE6vySjMm-WEfRq3CPzqKqqsHI", "google_api_key"},
		{"token ghp" + "_0123456789012345678901234567890123456789", "github_token"},
		{"ANTHROPIC_API_KEY=sk-ant" + "-api03-abcdefghij1234567890", "anthropic_key"},
	}
	for _, c := range cases {
		name, ok := e.Match(c.text)
		if !ok || name != c.want {
			t.Errorf("Match(%q) = (%q,%v), want (%q,true)", c.text, name, ok, c.want)
		}
	}
}

func TestRuleEngineMatchSpanReturnsExactValue(t *testing.T) {
	e := NewRuleEngine()
	const key = "AKIAIOSFODNN7EXAMPLE"
	name, span, ok := e.MatchSpan("AWS_ACCESS_KEY_ID=" + key + " in the config")
	if !ok || name != "aws_access_key" {
		t.Fatalf("MatchSpan = (%q,%q,%v), want aws_access_key", name, span, ok)
	}
	if span != key {
		t.Errorf("span = %q, want exactly %q (no surrounding text)", span, key)
	}
}

func TestRuleEngineMatchSpanEmptyOnNoMatch(t *testing.T) {
	e := NewRuleEngine()
	if name, span, ok := e.MatchSpan("just some ordinary prose"); ok || name != "" || span != "" {
		t.Errorf("MatchSpan on benign text = (%q,%q,%v), want empty/false", name, span, ok)
	}
}

func TestRuleEngineNoFalsePositiveOnPlainCode(t *testing.T) {
	e := NewRuleEngine()
	benign := []string{
		"func main() { fmt.Println(\"hello\") }",
		"const id = uuid.New().String()",
		"SELECT * FROM users WHERE id = 42",
		"base64 of small token: aGVsbG8=",
	}
	for _, b := range benign {
		if name, ok := e.Match(b); ok {
			t.Errorf("Match(%q) unexpectedly fired rule %q", b, name)
		}
	}
}
