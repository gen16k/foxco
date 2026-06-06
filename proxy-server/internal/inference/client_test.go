package inference

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"local-lfm-dlp-proxy/internal/dlp"
)

func TestLlamaClientClassify(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		// Echo back a constrained JSON verdict.
		resp := map[string]any{
			"choices": []map[string]any{
				{"message": map[string]any{"content": `{"reason": "contains api key", "decision": "BLOCK"}`}},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	c := NewLlamaClient(srv.URL, "LFM2-1.2B", 2000, 500)
	out, err := c.Classify(context.Background(), dlp.ClassifyInput{SegmentType: "tool_result", Text: "x"})
	if err != nil {
		t.Fatalf("classify: %v", err)
	}
	if !out.NG || out.ShortReason != "contains api key" {
		t.Fatalf("unexpected verdict: %+v", out)
	}
}

func TestLlamaClientHandlesProseWrappedJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		resp := map[string]any{
			"choices": []map[string]any{
				{"message": map[string]any{"content": "Sure! {\"reason\": \"ok\", \"decision\": \"ALLOW\"} done"}},
			},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	c := NewLlamaClient(srv.URL, "m", 2000, 500)
	out, err := c.Classify(context.Background(), dlp.ClassifyInput{Text: "hi"})
	if err != nil {
		t.Fatalf("classify: %v", err)
	}
	if out.NG {
		t.Fatalf("expected ok, got %+v", out)
	}
}

// The llama.cpp json_schema constraint is not always enforced; the model can
// emit a bare label. The tolerant parser must still classify it.
func TestLlamaClientTolerantBareLabel(t *testing.T) {
	mk := func(content string) *LlamaClient {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"choices": []map[string]any{{"message": map[string]any{"content": content}}},
			})
		}))
		t.Cleanup(srv.Close)
		return NewLlamaClient(srv.URL, "m", 2000, 500)
	}
	out, err := mk("BLOCK").Classify(context.Background(), dlp.ClassifyInput{Text: "x"})
	if err != nil || !out.NG {
		t.Fatalf("bare BLOCK should map to NG, got %+v err=%v", out, err)
	}
	out, err = mk("ALLOW").Classify(context.Background(), dlp.ClassifyInput{Text: "x"})
	if err != nil || out.NG {
		t.Fatalf("bare ALLOW should map to OK, got %+v err=%v", out, err)
	}
	if _, err := mk("42").Classify(context.Background(), dlp.ClassifyInput{Text: "x"}); err == nil {
		t.Fatal("unparseable output should error (caller fails closed)")
	}
}

func TestLlamaClientErrorOnStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()
	c := NewLlamaClient(srv.URL, "m", 2000, 500)
	if _, err := c.Classify(context.Background(), dlp.ClassifyInput{Text: "x"}); err == nil {
		t.Fatal("expected error on 503")
	}
}

func TestKeywordClassifier(t *testing.T) {
	k := NewKeywordClassifier()
	ng, _ := k.Classify(context.Background(), dlp.ClassifyInput{Text: "DB_PASSWORD=hunter2"})
	if !ng.NG {
		t.Fatal("expected NG for password assignment")
	}
	ok, _ := k.Classify(context.Background(), dlp.ClassifyInput{Text: "please refactor this function"})
	if ok.NG {
		t.Fatal("expected OK for benign text")
	}
}
