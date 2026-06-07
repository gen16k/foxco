// Package config loads the proxy configuration from YAML, applying the
// conservative defaults agreed in the implementation plan (fail-closed,
// rule guardrail on, no raw-text persistence).
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

type Config struct {
	// Mode selects how clients reach the proxy:
	//   transparent — hosts-file redirect + HTTPS interception on intercept.https_listen_addr (default)
	//   proxy       — legacy env-var mode (ANTHROPIC_BASE_URL -> server.listen_addr, plain HTTP)
	//   both        — run both listeners simultaneously
	Mode      string    `yaml:"mode"`
	Server    Server    `yaml:"server"`
	Intercept Intercept `yaml:"intercept"`
	TLS       TLS       `yaml:"tls"`
	Upstream  Upstream  `yaml:"upstream"`
	DLP       DLP       `yaml:"dlp"`
	Inference Inference `yaml:"inference"`
	Cache     Cache     `yaml:"cache"`
	Storage   Storage   `yaml:"storage"`
	Logging   Logging   `yaml:"logging"`
	Service   Service   `yaml:"service"`
	Admin     Admin     `yaml:"admin"`
}

type Server struct {
	ListenAddr string `yaml:"listen_addr"`
}

// Intercept configures transparent HTTPS interception: which hostnames are
// redirected to the proxy via the hosts file and where the TLS listener binds.
type Intercept struct {
	Hosts           []string `yaml:"hosts"`
	HTTPSListenAddr string   `yaml:"https_listen_addr"`
	ManageHostsFile bool     `yaml:"manage_hosts_file"`
}

// TLS configures the proxy-issued root CA used to mint per-host leaf
// certificates for the intercepted hosts.
type TLS struct {
	CACertPath      string   `yaml:"ca_cert_path"`
	CAKeyPath       string   `yaml:"ca_key_path"`
	NameConstraints []string `yaml:"name_constraints"`
}

type Upstream struct {
	BaseURL string `yaml:"base_url"`
	// ResolverDNS is the external DNS server (host:port) used to resolve the
	// real upstream, bypassing the hosts file the proxy itself installs.
	ResolverDNS string `yaml:"resolver_dns"`
	TimeoutMS   int    `yaml:"timeout_ms"`
}

type DLP struct {
	FailClosed        bool          `yaml:"fail_closed"`
	ClassifyTimeoutMS int           `yaml:"classify_timeout_ms"`
	BlockResponseMode string        `yaml:"block_response_mode"`
	RuleGuardrail     RuleGuardrail `yaml:"rule_guardrail"`
	Bypass            Bypass        `yaml:"bypass"`
}

type RuleGuardrail struct {
	Enabled bool `yaml:"enabled"`
}

// Bypass configures the explicit user override marker. When Enabled and the
// caller includes Marker in the latest user message, that turn is forwarded
// without DLP blocking (rules + classifier) and audited as a distinct BYPASS
// decision. It is an advisory escape hatch for obvious false positives, not a
// security boundary — see the threat model in docs/spec-proxy.md.
type Bypass struct {
	Enabled bool   `yaml:"enabled"`
	Marker  string `yaml:"marker"`
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
	// File, when set, directs logs to a file instead of stdout. Used when the
	// proxy runs as a Windows service (no console). Empty keeps stdout.
	File string `yaml:"file"`
}

type Service struct {
	Name string `yaml:"name"`
}

// Admin configures the read-only observability API (/admin/*) consumed by the
// local admin UI. It binds the same localhost address as the proxy.
type Admin struct {
	Enabled bool `yaml:"enabled"`
	// AuthToken, when non-empty, requires `Authorization: Bearer <token>` on every
	// /admin/* request. Recommended whenever store_raw_text is true (the audit DB
	// then contains secrets). Empty = no token (localhost-only, advisory).
	AuthToken string `yaml:"auth_token"`
}

// Default returns a configuration with the agreed safe defaults.
func Default() Config {
	cfg := Config{
		Mode:   "transparent",
		Server: Server{ListenAddr: "127.0.0.1:8787"},
		Intercept: Intercept{
			Hosts:           []string{"api.anthropic.com"},
			HTTPSListenAddr: "127.0.0.1:443",
			ManageHostsFile: true,
		},
		TLS: TLS{
			CACertPath:      `%ProgramData%\LocalLfmDlpProxy\ca\ca.crt`,
			CAKeyPath:       `%ProgramData%\LocalLfmDlpProxy\ca\ca.key`,
			NameConstraints: []string{"anthropic.com"},
		},
		Upstream: Upstream{
			BaseURL:     "https://api.anthropic.com",
			ResolverDNS: "1.1.1.1:53",
			TimeoutMS:   60000,
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
			Bypass:            Bypass{Enabled: true, Marker: "#dlp-allow"},
		},
		Inference: Inference{
			Type:            "llama_cpp_http",
			Endpoint:        "http://127.0.0.1:8791",
			Model:           "LFM2.5-1.2B",
			WarmupOnStart:   true,
			HealthTimeoutMS: 500,
			Profile:         "reason_decision",
		},
		Cache: Cache{Enabled: true, MaxEntries: 4096, PersistSQLite: false},
		Storage: Storage{
			Type: "sqlite",
			// Under the Windows service (LocalSystem) %LOCALAPPDATA% resolves to
			// the systemprofile; use the machine-wide %ProgramData% tree instead.
			Path:          `%ProgramData%\LocalLfmDlpProxy\state\dlp.db`,
			StoreRawText:  false,
			RetentionDays: 30,
		},
		Logging: Logging{Level: "info", RedactSensitiveValues: true},
		Service: Service{Name: "LocalLfmDlpProxy"},
		Admin:   Admin{Enabled: true},
	}
	cfg.expandPaths()
	return cfg
}

// Load reads a YAML config file over the defaults. A missing path returns the
// defaults unchanged.
func Load(path string) (Config, error) {
	cfg := Default()
	if path != "" {
		data, err := os.ReadFile(path)
		switch {
		case err == nil:
			if err := yaml.Unmarshal(data, &cfg); err != nil {
				return cfg, fmt.Errorf("parse config: %w", err)
			}
		case os.IsNotExist(err):
			// fall through with defaults
		default:
			return cfg, fmt.Errorf("read config: %w", err)
		}
	}
	// An empty marker would substring-match every request, silently disabling
	// DLP. Treat "enabled with no marker" as disabled.
	if cfg.DLP.Bypass.Enabled && cfg.DLP.Bypass.Marker == "" {
		cfg.DLP.Bypass.Enabled = false
	}
	cfg.expandPaths()
	return cfg, nil
}

// expandPaths expands Windows %VAR% tokens in every filesystem-path field.
// It is idempotent (no-op once tokens are resolved), so calling it from both
// Default and Load is safe.
func (c *Config) expandPaths() {
	c.Storage.Path = expandLocal(c.Storage.Path)
	c.TLS.CACertPath = expandLocal(c.TLS.CACertPath)
	c.TLS.CAKeyPath = expandLocal(c.TLS.CAKeyPath)
	c.Logging.File = expandLocal(c.Logging.File)
	c.Inference.SystemPromptFile = expandLocal(c.Inference.SystemPromptFile)
}

// expandLocal expands Windows-style %VAR% references (e.g. %ProgramData%,
// %LOCALAPPDATA%) using the process environment, then cleans the path. Unknown
// variables are left intact. An empty input returns empty.
func expandLocal(p string) string {
	if p == "" {
		return p
	}
	return filepath.Clean(expandPercent(p))
}

// expandPercent replaces %NAME% tokens with os.Getenv(NAME). %% is a literal %.
// A token whose variable is unset (or empty) is left verbatim.
func expandPercent(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); {
		if s[i] != '%' {
			b.WriteByte(s[i])
			i++
			continue
		}
		end := strings.IndexByte(s[i+1:], '%')
		if end < 0 { // no closing %, emit the rest verbatim
			b.WriteString(s[i:])
			break
		}
		name := s[i+1 : i+1+end]
		if name == "" { // "%%" -> literal "%"
			b.WriteByte('%')
			i += 2
			continue
		}
		if val := os.Getenv(name); val != "" {
			b.WriteString(val)
		} else {
			b.WriteString("%" + name + "%") // unknown: leave intact
		}
		i += end + 2
	}
	return b.String()
}
