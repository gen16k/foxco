// Package inference talks to the local LFM runtime. The MVP backend is a
// llama.cpp server (llama-server) speaking its OpenAI-compatible chat API over
// localhost HTTP, with the response constrained to a small JSON object so the
// 1.2B-class model reliably returns the verdict. The exact prompt/schema/parse
// contract lives in a swappable PromptProfile (see profile.go) so a fine-tuned
// model with a different output format can be dropped in via config. The Adapter
// shape stays narrow so an ONNX Runtime / Vitis AI NPU backend can replace it.
package inference

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"local-lfm-dlp-proxy/internal/dlp"
)

// LlamaClient is a dlp.Classifier backed by a llama.cpp server.
type LlamaClient struct {
	base          string
	model         string
	profile       PromptProfile
	httpc         *http.Client
	timeout       time.Duration
	healthTimeout time.Duration
}

// NewLlamaClient builds a client using the default prompt profile. endpoint is
// the llama-server base URL (e.g. http://127.0.0.1:8791).
func NewLlamaClient(endpoint, model string, timeoutMS, healthTimeoutMS int) *LlamaClient {
	return &LlamaClient{
		base:          strings.TrimRight(endpoint, "/"),
		model:         model,
		profile:       DefaultProfile(),
		httpc:         &http.Client{},
		timeout:       time.Duration(timeoutMS) * time.Millisecond,
		healthTimeout: time.Duration(healthTimeoutMS) * time.Millisecond,
	}
}

// SetProfile swaps the prompt/schema/parse contract (e.g. for a fine-tuned model).
func (c *LlamaClient) SetProfile(p PromptProfile) { c.profile = p }

// ProfileName reports the active profile name (for logging).
func (c *LlamaClient) ProfileName() string { return c.profile.Name }

type chatRequest struct {
	Model          string          `json:"model"`
	Messages       []chatMessage   `json:"messages"`
	Temperature    float64         `json:"temperature"`
	MaxTokens      int             `json:"max_tokens"`
	ResponseFormat *responseFormat `json:"response_format,omitempty"`
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type responseFormat struct {
	Type       string        `json:"type"`
	JSONSchema schemaWrapper `json:"json_schema"`
}

type schemaWrapper struct {
	Name   string          `json:"name"`
	Strict bool            `json:"strict"`
	Schema json.RawMessage `json:"schema"`
}

type chatResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
}

// defaultMaxTokens caps the model reply when the active profile does not set its
// own. A binary verdict needs only a few tokens; this preserves the historical
// behavior for the classifier profiles.
const defaultMaxTokens = 128

// maxTokens is the reply cap for the active profile, falling back to the default
// when the profile leaves it at 0. Extraction profiles raise it so the full
// multi-key JSON object is not truncated (a truncated body fails to parse and
// then fails closed, blocking benign text).
func (c *LlamaClient) maxTokens() int {
	if c.profile.MaxTokens > 0 {
		return c.profile.MaxTokens
	}
	return defaultMaxTokens
}

// Classify implements dlp.Classifier using the active PromptProfile.
func (c *LlamaClient) Classify(ctx context.Context, in dlp.ClassifyInput) (dlp.ClassifyOutput, error) {
	if c.timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, c.timeout)
		defer cancel()
	}

	body := chatRequest{
		Model:       c.model,
		Temperature: 0,
		MaxTokens:   c.maxTokens(),
		Messages: []chatMessage{
			{Role: "system", Content: c.profile.System},
			{Role: "user", Content: c.profile.BuildUser(in)},
		},
	}
	if len(c.profile.Schema) > 0 {
		body.ResponseFormat = &responseFormat{
			Type:       "json_schema",
			JSONSchema: schemaWrapper{Name: "dlp_verdict", Strict: true, Schema: c.profile.Schema},
		}
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return dlp.ClassifyOutput{}, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+"/v1/chat/completions", bytes.NewReader(raw))
	if err != nil {
		return dlp.ClassifyOutput{}, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpc.Do(req)
	if err != nil {
		return dlp.ClassifyOutput{}, fmt.Errorf("inference request: %w", err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		return dlp.ClassifyOutput{}, fmt.Errorf("inference status %d", resp.StatusCode)
	}

	var cr chatResponse
	if err := json.Unmarshal(data, &cr); err != nil || len(cr.Choices) == 0 {
		return dlp.ClassifyOutput{}, fmt.Errorf("inference decode: %w", err)
	}
	return c.profile.Parse(cr.Choices[0].Message.Content)
}

// Health checks that the llama-server is up and a model is loaded.
func (c *LlamaClient) Health(ctx context.Context) error {
	if c.healthTimeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, c.healthTimeout)
		defer cancel()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+"/health", nil)
	if err != nil {
		return err
	}
	resp, err := c.httpc.Do(req)
	if err != nil {
		return fmt.Errorf("health: %w", err)
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<16))
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("health status %d", resp.StatusCode)
	}
	return nil
}

// Warmup triggers first-token compilation/caching with a trivial classification
// so the first real request is not penalized.
func (c *LlamaClient) Warmup(ctx context.Context) error {
	_, err := c.Classify(ctx, dlp.ClassifyInput{SegmentType: "user_text", Text: "hello"})
	return err
}
