package inference

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"local-lfm-dlp-proxy/internal/dlp"
)

// allEmpty is the model's "nothing sensitive" output: every category present and
// empty, exactly as the dataset specifies.
const allEmpty = `{"address": [], "company_name": [], "email_address": [], "human_name": [], "phone_number": [], "account_identifier": [], "network_identifier": [], "system_config": [], "project_info": [], "financial_info": [], "transaction_id": []}`

func TestJPExtractionParseEmptyAllows(t *testing.T) {
	out, err := parseJPConfidentialExtraction(allEmpty)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if out.NG {
		t.Fatalf("all-empty extraction should ALLOW, got %+v", out)
	}
	if out.ShortReason != "" {
		t.Fatalf("expected empty reason, got %q", out.ShortReason)
	}
}

// A populated category blocks, and the reason names the CATEGORY but must never
// leak the extracted value (it flows into the audit log + block message).
func TestJPExtractionParseOneCategoryBlocksWithoutValue(t *testing.T) {
	out, err := parseJPConfidentialExtraction(`{"human_name": ["山田太郎"], "address": []}`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if !out.NG {
		t.Fatalf("populated category should BLOCK, got %+v", out)
	}
	if !strings.Contains(out.ShortReason, "human_name") {
		t.Fatalf("reason should name the category, got %q", out.ShortReason)
	}
	if strings.Contains(out.ShortReason, "山田太郎") {
		t.Fatalf("reason must NOT contain the extracted value, got %q", out.ShortReason)
	}
}

// The reason follows the canonical category order regardless of key order in the
// model output, so it is deterministic.
func TestJPExtractionParseMultipleCategoriesDeterministicOrder(t *testing.T) {
	out, err := parseJPConfidentialExtraction(`{"company_name": ["Acme"], "address": ["Tokyo"]}`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if !out.NG {
		t.Fatalf("expected BLOCK, got %+v", out)
	}
	const want = "confidential entities: address, company_name"
	if out.ShortReason != want {
		t.Fatalf("reason = %q, want %q", out.ShortReason, want)
	}
}

func TestJPExtractionParseMissingKeysIgnored(t *testing.T) {
	out, err := parseJPConfidentialExtraction(`{"email_address": ["a@b.com"]}`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if !out.NG || !strings.Contains(out.ShortReason, "email_address") {
		t.Fatalf("expected BLOCK on email_address, got %+v", out)
	}
}

func TestJPExtractionParseEmptyObjectAllows(t *testing.T) {
	out, err := parseJPConfidentialExtraction(`{}`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if out.NG {
		t.Fatalf("empty object should ALLOW, got %+v", out)
	}
}

// llama.cpp may not enforce the grammar; the model can wrap JSON in prose or a
// fenced block. extractJSON must recover it.
func TestJPExtractionParseProseWrapped(t *testing.T) {
	out, err := parseJPConfidentialExtraction("Here you go:\n```json\n{\"phone_number\": [\"090-1234-5678\"]}\n```\n")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if !out.NG || !strings.Contains(out.ShortReason, "phone_number") {
		t.Fatalf("prose-wrapped JSON should be tolerated and BLOCK, got %+v", out)
	}
}

func TestJPExtractionParseNullTreatedAsEmpty(t *testing.T) {
	out, err := parseJPConfidentialExtraction(`{"human_name": null, "address": ["X"]}`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if !out.NG {
		t.Fatalf("expected BLOCK on address, got %+v", out)
	}
	if strings.Contains(out.ShortReason, "human_name") {
		t.Fatalf("null category must not fire, got %q", out.ShortReason)
	}
}

// A complete unmarshal failure (truncated / non-JSON / wrong value type) must
// return an error so the detector fails closed (blocks) rather than allowing.
func TestJPExtractionParseMalformedFailsClosed(t *testing.T) {
	cases := []string{
		`{"human_name": ["X"`,  // truncated object (e.g. max_tokens cutoff)
		`not json at all`,      // no JSON present
		`{"address": "Tokyo"}`, // scalar where an array is required
	}
	for _, c := range cases {
		if _, err := parseJPConfidentialExtraction(c); err == nil {
			t.Fatalf("input %q should error so the caller fails closed", c)
		}
	}
}

func TestJPExtractionProfileRegistered(t *testing.T) {
	p, ok := LookupProfile("jp_confidential_extraction")
	if !ok {
		t.Fatal("jp_confidential_extraction not registered")
	}
	if p.Name != "jp_confidential_extraction" {
		t.Fatalf("name = %q", p.Name)
	}
	if p.System == "" || len(p.Schema) == 0 || p.BuildUser == nil || p.Parse == nil {
		t.Fatal("profile fields incomplete")
	}
	if p.MaxTokens != 384 {
		t.Fatalf("MaxTokens = %d, want 384", p.MaxTokens)
	}
	// The load-bearing format guarantee: raw text passes through with no wrapper
	// and no segment_type hint (matching the model's training distribution).
	got := p.BuildUser(dlp.ClassifyInput{Text: "hello world", SegmentType: "user_text"})
	if got != "hello world" {
		t.Fatalf("BuildUser should pass text through verbatim, got %q", got)
	}
	if strings.Contains(got, "DATA") || strings.Contains(got, "segment_type") {
		t.Fatalf("BuildUser must not add wrapper/metadata, got %q", got)
	}
}

func TestJPExtractionSchemaShape(t *testing.T) {
	if !json.Valid(jpConfidentialExtractionSchema) {
		t.Fatal("schema is not valid JSON")
	}
	var s struct {
		Properties           map[string]json.RawMessage `json:"properties"`
		Required             []string                   `json:"required"`
		AdditionalProperties bool                       `json:"additionalProperties"`
	}
	if err := json.Unmarshal(jpConfidentialExtractionSchema, &s); err != nil {
		t.Fatalf("schema decode: %v", err)
	}
	if len(s.Properties) != len(jpExtractionSensitiveCategories) {
		t.Fatalf("schema has %d properties, want %d", len(s.Properties), len(jpExtractionSensitiveCategories))
	}
	for _, cat := range jpExtractionSensitiveCategories {
		if _, ok := s.Properties[cat]; !ok {
			t.Fatalf("schema missing property %q", cat)
		}
	}
	if len(s.Required) != len(jpExtractionSensitiveCategories) {
		t.Fatalf("schema requires %d keys, want %d", len(s.Required), len(jpExtractionSensitiveCategories))
	}
	if s.AdditionalProperties {
		t.Fatal("schema should set additionalProperties: false")
	}
}

// End-to-end through the client with the extraction profile: a real-shaped model
// reply blocks, and the outgoing request carries the profile's larger max_tokens.
func TestJPExtractionClassifyEndToEndAndMaxTokens(t *testing.T) {
	var gotMaxTokens int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			MaxTokens int `json:"max_tokens"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		gotMaxTokens = req.MaxTokens
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{
				{"message": map[string]any{"content": `{"company_name": ["Acme Corp"], "address": []}`}},
			},
		})
	}))
	defer srv.Close()

	c := NewLlamaClient(srv.URL, "ft-model", 2000, 500)
	c.SetProfile(jpConfidentialExtractionProfile())
	out, err := c.Classify(context.Background(), dlp.ClassifyInput{SegmentType: "user_text", Text: "Acme Corp"})
	if err != nil {
		t.Fatalf("classify: %v", err)
	}
	if !out.NG || !strings.Contains(out.ShortReason, "company_name") {
		t.Fatalf("expected BLOCK naming company_name, got %+v", out)
	}
	if gotMaxTokens != 384 {
		t.Fatalf("max_tokens = %d, want 384 for extraction profile", gotMaxTokens)
	}
}

// The classifier profiles must keep the historical 128-token cap (MaxTokens unset).
func TestDefaultProfileMaxTokensUnchanged(t *testing.T) {
	var gotMaxTokens int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			MaxTokens int `json:"max_tokens"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		gotMaxTokens = req.MaxTokens
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{
				{"message": map[string]any{"content": `{"reason":"ok","decision":"ALLOW"}`}},
			},
		})
	}))
	defer srv.Close()

	c := NewLlamaClient(srv.URL, "m", 2000, 500) // default profile (reason_decision)
	if _, err := c.Classify(context.Background(), dlp.ClassifyInput{Text: "hi"}); err != nil {
		t.Fatalf("classify: %v", err)
	}
	if gotMaxTokens != defaultMaxTokens {
		t.Fatalf("default profile max_tokens = %d, want %d", gotMaxTokens, defaultMaxTokens)
	}
}
