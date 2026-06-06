// Command proxy is the Local LFM DLP Proxy. It listens on localhost, inspects
// outbound Claude Code requests with the LFM classifier (plus a deterministic
// secret guardrail), blocks sensitive egress, sanitizes re-sent history, and
// forwards approved traffic to the Anthropic API.
package main

import (
	"context"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"local-lfm-dlp-proxy/internal/anthropic"
	"local-lfm-dlp-proxy/internal/config"
	"local-lfm-dlp-proxy/internal/dlp"
	"local-lfm-dlp-proxy/internal/inference"
	"local-lfm-dlp-proxy/internal/proxy"
	"local-lfm-dlp-proxy/internal/storage"
)

func main() {
	configPath := flag.String("config", "", "path to config.yaml")
	classifierOverride := flag.String("classifier", "", "override classifier: llama|keyword")
	// These let start.ps1 be the single source of runtime wiring: it picks a
	// backend (npu/vulkan/cpu), launches the matching sidecar, and passes the
	// endpoint/paths/profile/label here so the config file need not change per run.
	endpointOverride := flag.String("endpoint", "", "override inference.endpoint")
	modelOverride := flag.String("model", "", "override inference.model (e.g. the NPU model dir/label)")
	profileOverride := flag.String("profile", "", "override inference.profile")
	chatPathOverride := flag.String("chat-path", "", "override inference.chat_path")
	healthPathOverride := flag.String("health-path", "", "override inference.health_path")
	backendOverride := flag.String("backend", "", "audit runtime label: npu|vulkan|cpu|llama_cpp")
	flag.Parse()

	cfg, err := config.Load(*configPath)
	if err != nil {
		slog.Error("config load failed", "err", err)
		os.Exit(1)
	}
	// CLI flags win over the config file (non-empty only) so a launcher can wire
	// the runtime without rewriting config.yaml.
	overrideStr(&cfg.Inference.Endpoint, *endpointOverride)
	overrideStr(&cfg.Inference.Model, *modelOverride)
	overrideStr(&cfg.Inference.Profile, *profileOverride)
	overrideStr(&cfg.Inference.ChatPath, *chatPathOverride)
	overrideStr(&cfg.Inference.HealthPath, *healthPathOverride)
	overrideStr(&cfg.Inference.Backend, *backendOverride)

	log := newLogger(cfg.Logging.Level)

	classifier, backend := buildClassifier(cfg, *classifierOverride, log)
	detector := dlp.NewDetector(
		dlp.NewRuleEngine(),
		cfg.DLP.RuleGuardrail.Enabled,
		classifier,
		dlp.NewCache(cacheSize(cfg)),
		cfg.DLP.FailClosed,
	)
	detector.SetLogger(log)
	forwarder := anthropic.NewForwarder(cfg.Upstream.BaseURL, cfg.Upstream.TimeoutMS)

	var audit storage.Recorder = storage.NopRecorder{}
	if cfg.Storage.Type == "sqlite" {
		if st, err := storage.Open(cfg.Storage.Path, cfg.Storage.RetentionDays); err != nil {
			log.Warn("audit storage disabled", "err", err)
		} else {
			defer st.Close()
			audit = st
			log.Info("audit storage open", "path", cfg.Storage.Path)
		}
	}

	h := proxy.New(detector, forwarder, audit, log, cfg.DLP.FailClosed, cfg.Inference.Model, backend)
	mux := http.NewServeMux()
	h.Register(mux)

	srv := &http.Server{Addr: cfg.Server.ListenAddr, Handler: mux}
	go func() {
		log.Info("listening", "addr", cfg.Server.ListenAddr, "upstream", cfg.Upstream.BaseURL,
			"backend", backend, "model", cfg.Inference.Model, "fail_closed", cfg.DLP.FailClosed)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Error("server error", "err", err)
			os.Exit(1)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop
	log.Info("shutting down")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = srv.Shutdown(ctx)
}

func buildClassifier(cfg config.Config, override string, log *slog.Logger) (dlp.Classifier, string) {
	mode := cfg.Inference.Type
	if override != "" {
		mode = map[string]string{"keyword": "keyword", "llama": "llama_cpp_http"}[override]
	}
	if mode == "keyword" {
		log.Info("using keyword classifier (fallback, no LFM)")
		return inference.NewKeywordClassifier(), "keyword"
	}

	client := inference.NewLlamaClient(cfg.Inference.Endpoint, cfg.Inference.Model,
		cfg.DLP.ClassifyTimeoutMS, cfg.Inference.HealthTimeoutMS)
	// Empty paths keep the llama.cpp defaults, which the NPU shim also serves; only
	// an OGA runtime like Lemonade (/api/v1) needs these set.
	client.SetPaths(cfg.Inference.ChatPath, cfg.Inference.HealthPath)

	// Select the LFM I/O contract (swappable for fine-tuned models).
	prof := inference.DefaultProfile()
	if name := cfg.Inference.Profile; name != "" {
		if p, ok := inference.LookupProfile(name); ok {
			prof = p
		} else {
			log.Warn("unknown inference profile; using default", "profile", name, "default", inference.DefaultProfileName)
		}
	}
	if f := cfg.Inference.SystemPromptFile; f != "" {
		if data, err := os.ReadFile(f); err != nil {
			log.Warn("could not read system_prompt_file; keeping profile prompt", "file", f, "err", err)
		} else {
			prof.System = string(data)
			log.Info("system prompt overridden from file", "file", f)
		}
	}
	client.SetProfile(prof)
	log.Info("LFM contract", "profile", prof.Name)

	if cfg.Inference.WarmupOnStart {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := client.Health(ctx); err != nil {
			log.Warn("LFM health check failed; requests will fail closed until the sidecar is up",
				"endpoint", cfg.Inference.Endpoint, "err", err)
		} else if err := client.Warmup(ctx); err != nil {
			log.Warn("LFM warmup failed", "err", err)
		} else {
			log.Info("LFM warm", "endpoint", cfg.Inference.Endpoint, "model", cfg.Inference.Model)
		}
	}
	// The audit log records the runtime that served the verdict (npu/vulkan/cpu),
	// not the model name. start.ps1 sets -backend per launched sidecar; default to
	// the generic llama.cpp label when unset.
	backend := cfg.Inference.Backend
	if backend == "" {
		backend = "llama_cpp"
	}
	return client, backend
}

// overrideStr sets *dst to v when v is non-empty (CLI flag over config file).
func overrideStr(dst *string, v string) {
	if v != "" {
		*dst = v
	}
}

func cacheSize(cfg config.Config) int {
	if !cfg.Cache.Enabled {
		return 0
	}
	if cfg.Cache.MaxEntries <= 0 {
		return 4096
	}
	return cfg.Cache.MaxEntries
}

func newLogger(level string) *slog.Logger {
	lvl := slog.LevelInfo
	switch level {
	case "debug":
		lvl = slog.LevelDebug
	case "warn":
		lvl = slog.LevelWarn
	case "error":
		lvl = slog.LevelError
	}
	return slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: lvl}))
}
