//go:build evalbench

// Eval/benchmark harness: measure DLP classification accuracy and latency against
// a LIVE LFM sidecar (any backend) so the AMD NPU can be compared with Vulkan/CPU
// before flipping the default. Report-only — it never fails on accuracy and skips
// cleanly when no live endpoint is configured. Build-tagged so normal `go test`
// (which has no sidecar) ignores it.
//
// Run (point at whichever sidecar is up; repeat per backend and compare):
//
//	# llama.cpp Vulkan (or CPU) sidecar on :8791
//	$env:FOXCO_EVAL_ENDPOINT="http://127.0.0.1:8791"; $env:FOXCO_EVAL_BACKEND="vulkan"
//	go test -tags evalbench -run EvalBench ./internal/inference/ -v
//
//	# AMD NPU via the Ryzen AI ONNX shim on :8792 (npu/npu_server.py). It serves
//	# the llama.cpp default paths, so no CHAT_PATH/HEALTH_PATH override is needed.
//	$env:FOXCO_EVAL_ENDPOINT="http://127.0.0.1:8792"
//	$env:FOXCO_EVAL_PROFILE="reason_decision_prompt"
//	$env:FOXCO_EVAL_BACKEND="npu"
//	go test -tags evalbench -run EvalBench ./internal/inference/ -v
package inference

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"sort"
	"testing"
	"time"

	"local-lfm-dlp-proxy/internal/dlp"
)

type evalCase struct {
	Text        string `json:"text"`
	SegmentType string `json:"segment_type"`
	Expect      string `json:"expect"` // ALLOW | BLOCK
}

func TestEvalBench(t *testing.T) {
	endpoint := os.Getenv("FOXCO_EVAL_ENDPOINT")
	if endpoint == "" {
		t.Skip("set FOXCO_EVAL_ENDPOINT to a live LFM sidecar base URL to run the eval/bench")
	}
	backend := evalEnvOr("FOXCO_EVAL_BACKEND", "unknown")
	model := evalEnvOr("FOXCO_EVAL_MODEL", "LFM2.5-1.2B")
	profileName := evalEnvOr("FOXCO_EVAL_PROFILE", DefaultProfileName)

	// Generous timeouts: NPU/iGPU cold/first-call can be slow.
	client := NewLlamaClient(endpoint, model, 15000, 3000)
	client.SetPaths(os.Getenv("FOXCO_EVAL_CHAT_PATH"), os.Getenv("FOXCO_EVAL_HEALTH_PATH"))
	p, ok := LookupProfile(profileName)
	if !ok {
		t.Fatalf("unknown profile %q", profileName)
	}
	client.SetProfile(p)

	ctx := context.Background()
	if err := client.Health(ctx); err != nil {
		t.Skipf("sidecar not healthy at %s: %v", endpoint, err)
	}

	cases := loadEvalCases(t)
	if len(cases) == 0 {
		t.Fatal("no eval cases loaded from testdata/eval_cases.jsonl")
	}

	// Cold/first-call latency, measured once before the warm loop.
	coldStart := time.Now()
	_, _ = client.Classify(ctx, dlp.ClassifyInput{SegmentType: "user_text", Text: "warmup ping"})
	cold := time.Since(coldStart)

	var (
		correct, fp, fn int
		benign, sens    int
		lat             []time.Duration
	)
	for _, c := range cases {
		start := time.Now()
		out, err := client.Classify(ctx, dlp.ClassifyInput{SegmentType: c.SegmentType, Text: c.Text})
		lat = append(lat, time.Since(start))

		// Fail-closed semantics: an error or unparseable verdict is treated as BLOCK,
		// matching how the proxy behaves in production.
		predictBlock := err != nil || out.NG
		expectBlock := c.Expect == "BLOCK"
		if expectBlock {
			sens++
		} else {
			benign++
		}
		switch {
		case predictBlock == expectBlock:
			correct++
		case predictBlock && !expectBlock:
			fp++ // benign -> BLOCK: the costly usability error
		default:
			fn++ // sensitive -> ALLOW: the dangerous error
		}
	}

	n := len(cases)
	t.Logf("=== eval/bench backend=%s model=%s profile=%s endpoint=%s ===", backend, model, profileName, endpoint)
	t.Logf("cases=%d  accuracy=%.1f%%", n, 100*float64(correct)/float64(n))
	t.Logf("FP (benign->BLOCK)   = %d/%d", fp, benign)
	t.Logf("FN (sensitive->ALLOW)= %d/%d", fn, sens)
	t.Logf("latency: warm p50=%v  p95=%v  cold/first=%v",
		evalPercentile(lat, 50), evalPercentile(lat, 95), cold)
}

func loadEvalCases(t *testing.T) []evalCase {
	f, err := os.Open("testdata/eval_cases.jsonl")
	if err != nil {
		t.Fatalf("open eval cases: %v", err)
	}
	defer f.Close()

	var out []evalCase
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 || line[0] == '#' {
			continue
		}
		var c evalCase
		if err := json.Unmarshal(line, &c); err != nil {
			t.Fatalf("bad eval line %q: %v", string(line), err)
		}
		if c.SegmentType == "" {
			c.SegmentType = "user_text"
		}
		out = append(out, c)
	}
	if err := sc.Err(); err != nil {
		t.Fatalf("scan eval cases: %v", err)
	}
	return out
}

func evalPercentile(ds []time.Duration, p int) time.Duration {
	if len(ds) == 0 {
		return 0
	}
	s := append([]time.Duration(nil), ds...)
	sort.Slice(s, func(i, j int) bool { return s[i] < s[j] })
	return s[(p*(len(s)-1))/100]
}

func evalEnvOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}
