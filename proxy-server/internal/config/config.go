// Package config loads the proxy configuration from YAML, applying the
// conservative defaults agreed in the implementation plan (fail-closed,
// rule guardrail on, no raw-text persistence).
package config

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Server    Server    `yaml:"server"`
	Upstream  Upstream  `yaml:"upstream"`
	DLP       DLP       `yaml:"dlp"`
	Inference Inference `yaml:"inference"`
	Cache     Cache     `yaml:"cache"`
	Storage   Storage   `yaml:"storage"`
	Logging   Logging   `yaml:"logging"`
}

type Server struct {
	ListenAddr string `yaml:"listen_addr"`
}

type Upstream struct {
	BaseURL   string `yaml:"base_url"`
	TimeoutMS int    `yaml:"timeout_ms"`
}

type DLP struct {
	FailClosed        bool          `yaml:"fail_closed"`
	ClassifyTimeoutMS int           `yaml:"classify_timeout_ms"`
	BlockResponseMode string        `yaml:"block_response_mode"`
	RuleGuardrail     RuleGuardrail `yaml:"rule_guardrail"`
}

type RuleGuardrail struct {
	Enabled bool `yaml:"enabled"`
}

type Inference struct {
	Type             string `yaml:"type"`
	Endpoint         string `yaml:"endpoint"`
	Model            string `yaml:"model"`
	WarmupOnStart    bool   `yaml:"warmup_on_start"`
	HealthTimeoutMS  int    `yaml:"health_timeout_ms"`
	Profile          string `yaml:"profile"`            // LFM I/O contract name (see internal/inference/profile.go)
	SystemPromptFile string `yaml:"system_prompt_file"` // optional: override the profile's system prompt from a file
}

type Cache struct {
	Enabled       bool `yaml:"enabled"`
	MaxEntries    int  `yaml:"max_entries"`
	PersistSQLite bool `yaml:"persist_sqlite"`
}

type Storage struct {
	Type          string `yaml:"type"`
	Path          string `yaml:"path"`
	StoreRawText  bool   `yaml:"store_raw_text"`
	RetentionDays int    `yaml:"retention_days"`
}

type Logging struct {
	Level                 string `yaml:"level"`
	RedactSensitiveValues bool   `yaml:"redact_sensitive_values"`
}

// Default returns a configuration with the agreed safe defaults.
func Default() Config {
	return Config{
		Server: Server{ListenAddr: "127.0.0.1:8787"},
		Upstream: Upstream{
			BaseURL:   "https://api.anthropic.com",
			TimeoutMS: 60000,
		},
		DLP: DLP{
			FailClosed: true,
			// CPU LFM inference (esp. the first request after an idle gap, when
			// the prompt cache is cold) can exceed 1.5s; too tight a timeout
			// fail-closes benign requests. 5s gives CPU headroom; warm calls are
			// ~200-300ms. Lower this for GPU/NPU backends.
			ClassifyTimeoutMS: 5000,
			BlockResponseMode: "assistant_message",
			RuleGuardrail:     RuleGuardrail{Enabled: true},
		},
		Inference: Inference{
			Type:            "llama_cpp_http",
			Endpoint:        "http://127.0.0.1:8791",
			Model:           "LFM2.5-1.2B-JP-202606-Conf-Extract",
			WarmupOnStart:   true,
			HealthTimeoutMS: 500,
			Profile:         "jp_confidential_extraction",
		},
		Cache: Cache{Enabled: true, MaxEntries: 4096, PersistSQLite: false},
		Storage: Storage{
			Type:          "sqlite",
			Path:          expandLocal(`%LOCALAPPDATA%\LocalLfmDlpProxy\state\dlp.db`),
			StoreRawText:  false,
			RetentionDays: 30,
		},
		Logging: Logging{Level: "info", RedactSensitiveValues: true},
	}
}

// Load reads a YAML config file over the defaults. A missing path returns the
// defaults unchanged.
func Load(path string) (Config, error) {
	cfg := Default()
	if path == "" {
		return cfg, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return cfg, fmt.Errorf("read config: %w", err)
	}
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return cfg, fmt.Errorf("parse config: %w", err)
	}
	cfg.Storage.Path = expandLocal(cfg.Storage.Path)
	return cfg, nil
}

// expandLocal expands %LOCALAPPDATA% (and other env vars) in a Windows path.
func expandLocal(p string) string {
	if p == "" {
		return p
	}
	expanded := os.Expand(p, func(key string) string { return os.Getenv(key) })
	// os.Expand uses $VAR / ${VAR}; handle %VAR% (Windows style) explicitly.
	if local := os.Getenv("LOCALAPPDATA"); local != "" {
		expanded = replacePercent(expanded, "LOCALAPPDATA", local)
	}
	return filepath.Clean(expanded)
}

func replacePercent(s, key, val string) string {
	token := "%" + key + "%"
	out := ""
	for {
		i := indexOf(s, token)
		if i < 0 {
			return out + s
		}
		out += s[:i] + val
		s = s[i+len(token):]
	}
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
