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

	"local-lfm-dlp-proxy/internal/admin"
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
	flag.Parse()

	cfg, err := config.Load(*configPath)
	if err != nil {
		slog.Error("config load failed", "err", err)
		os.Exit(1)
	}

	log := newLogger(cfg.Logging.Level)
	startedAt := time.Now().UTC().Format(time.RFC3339)

	if cfg.Storage.StoreRawText {
		log.Warn("store_raw_text is ENABLED: the audit DB will persist live prompt text (including any secrets). " +
			"This relaxes the default no-raw-content posture; keep it off in production and set admin.auth_token.")
	}

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
	var store *storage.Store
	if cfg.Storage.Type == "sqlite" {
		st, err := storage.Open(cfg.Storage.Path, cfg.Storage.RetentionDays)
		if err != nil {
			log.Warn("audit storage disabled", "err", err)
		} else {
			defer st.Close()
			audit = st
			store = st
			log.Info("audit storage open", "path", cfg.Storage.Path)
		}
	}

	h := proxy.New(detector, forwarder, audit, log, cfg.DLP.FailClosed, cfg.Inference.Model, backend, cfg.Storage.StoreRawText)
	mux := http.NewServeMux()
	h.Register(mux)

	// Read-only observability API for the local admin UI (localhost-only).
	if cfg.Admin.Enabled {
		if store != nil {
			ah := admin.New(store, admin.Meta{
				StoreRawText:  cfg.Storage.StoreRawText,
				RetentionDays: cfg.Storage.RetentionDays,
				Model:         cfg.Inference.Model,
				Backend:       backend,
				ListenAddr:    cfg.Server.ListenAddr,
				StartedAt:     startedAt,
			}, cfg.Admin.AuthToken, log)
			ah.Register(mux)
			log.Info("admin api enabled", "routes", "/admin/*", "auth_required", cfg.Admin.AuthToken != "")
		} else {
			log.Warn("admin api requested but audit storage is not open; skipping")
		}
	}

	srv := &http.Server{Addr: cfg.Server.ListenAddr, Handler: mux}
	go func() {
		log.Info("listening", "addr", cfg.Server.ListenAddr, "upstream", cfg.Upstream.BaseURL,
			"classifier", backend, "fail_closed", cfg.DLP.FailClosed)
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
	return client, cfg.Inference.Model
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
