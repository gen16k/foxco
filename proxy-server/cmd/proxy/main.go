// Command proxy is PromptGate. It inspects outbound Claude Code
// requests with the LFM classifier (plus a deterministic secret guardrail),
// blocks sensitive egress, sanitizes re-sent history, and forwards approved
// traffic to the Anthropic API.
//
// Two interception modes (config "mode"):
//   - transparent (default): a hosts-file entry redirects the intercepted host
//     to 127.0.0.1 while the proxy runs, and the proxy terminates TLS on :443
//     with leaf certs minted by a Name-Constrained root CA. No ANTHROPIC_BASE_URL
//     needed. The upstream forward uses a hosts-bypassing resolver so it reaches
//     the REAL API instead of looping back through the redirect.
//   - proxy: legacy plain-HTTP listener selected via ANTHROPIC_BASE_URL.
//
// On Windows it runs either as a console process (dev/debug) or as a Windows
// service (production); see service_windows.go.
package main

import (
	"context"
	"crypto/tls"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"promptgate/internal/admin"
	"promptgate/internal/anthropic"
	"promptgate/internal/config"
	"promptgate/internal/dlp"
	"promptgate/internal/hostsfile"
	"promptgate/internal/inference"
	"promptgate/internal/mitm"
	"promptgate/internal/proxy"
	"promptgate/internal/storage"
	"promptgate/internal/upstreamdial"
)

const (
	upstreamDialTimeout = 10 * time.Second
	shutdownTimeout     = 5 * time.Second
)

func main() {
	configPath := flag.String("config", "", "path to config.yaml")
	classifierOverride := flag.String("classifier", "", "override classifier: llama|keyword")
	initCA := flag.Bool("init-ca", false, "generate the interception CA (if missing) and exit; used by install.ps1")
	flag.Parse()

	cfg, err := config.Load(*configPath)
	if err != nil {
		slog.Error("config load failed", "err", err)
		os.Exit(1)
	}

	log, closeLog := newLogger(cfg, isWindowsService())
	defer closeLog()

	if cfg.Storage.StoreRawText {
		log.Warn("store_raw_text is ENABLED: the audit DB will persist live prompt text (including any secrets). " +
			"This relaxes the default no-raw-content posture; keep it off in production and set admin.auth_token.")
	}

	// One-shot: generate/load the CA and exit (install.ps1 then trusts ca.crt).
	if *initCA {
		if _, err := mitm.EnsureCA(cfg.TLS.CACertPath, cfg.TLS.CAKeyPath, cfg.TLS.NameConstraints); err != nil {
			log.Error("init-ca failed", "err", err)
			os.Exit(1)
		}
		log.Info("CA ready", "cert", cfg.TLS.CACertPath, "key", cfg.TLS.CAKeyPath)
		fmt.Println(cfg.TLS.CACertPath)
		return
	}

	app, cleanup, err := buildApp(cfg, *classifierOverride, log)
	if err != nil {
		log.Error("startup failed", "err", err)
		os.Exit(1)
	}
	defer cleanup()

	if isWindowsService() {
		if err := runAsService(cfg.Service.Name, app, log); err != nil {
			log.Error("service run failed", "err", err)
			os.Exit(1)
		}
		return
	}

	// Console / interactive mode.
	if err := app.start(); err != nil {
		log.Error("start failed", "err", err)
		os.Exit(1)
	}
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop
	log.Info("shutting down")
	app.stop()
}

// application owns the running listeners and the hosts-file redirect; start and
// stop are shared by the console path and the Windows service handler.
type application struct {
	cfg      config.Config
	log      *slog.Logger
	servers  []*http.Server
	hostsMgr *hostsfile.Manager
	super    *supervisor // nil unless cfg.Supervise.Enabled (Windows service owns sidecar + web UI)
}

// buildApp wires the DLP pipeline, forwarder, listeners, and hosts manager for
// the configured mode. The returned cleanup closes the audit store.
func buildApp(cfg config.Config, classifierOverride string, log *slog.Logger) (*application, func(), error) {
	transparent := cfg.Mode == "transparent" || cfg.Mode == "both"
	legacy := cfg.Mode == "proxy" || cfg.Mode == "both"
	if !transparent && !legacy {
		return nil, nil, fmt.Errorf("unknown mode %q (want transparent|proxy|both)", cfg.Mode)
	}

	classifier, backend := buildClassifier(cfg, classifierOverride, log)
	detector := dlp.NewDetector(
		dlp.NewRuleEngine(),
		cfg.DLP.RuleGuardrail.Enabled,
		classifier,
		dlp.NewCache(cacheSize(cfg)),
		cfg.DLP.FailClosed,
	)
	detector.SetLogger(log)

	// In transparent mode the upstream host is redirected to us in the hosts
	// file, so the forward must resolve it via an external DNS to reach the real
	// API; in legacy mode normal resolution is fine.
	var rt http.RoundTripper
	if transparent {
		rt = upstreamdial.New(cfg.Upstream.ResolverDNS, cfg.Intercept.Hosts, upstreamDialTimeout)
	}
	forwarder := anthropic.NewForwarderWithTransport(cfg.Upstream.BaseURL, cfg.Upstream.TimeoutMS, rt)

	var audit storage.Recorder = storage.NopRecorder{}
	var store *storage.Store
	cleanup := func() {}
	if cfg.Storage.Type == "sqlite" {
		st, err := storage.Open(cfg.Storage.Path, cfg.Storage.RetentionDays)
		if err != nil {
			log.Warn("audit storage disabled", "err", err)
		} else {
			audit = st
			store = st
			cleanup = func() { _ = st.Close() }
			log.Info("audit storage open", "path", cfg.Storage.Path)
		}
	}

	bypass := proxy.BypassConfig{Enabled: cfg.DLP.Bypass.Enabled, Marker: cfg.DLP.Bypass.Marker}
	if bypass.Enabled {
		log.Warn("DLP bypass marker enabled; a request whose latest user turn contains the marker is forwarded without blocking (audited as BYPASS)",
			"marker", bypass.Marker)
	}
	h := proxy.New(detector, forwarder, audit, log, cfg.DLP.FailClosed, cfg.Inference.Model, backend, cfg.Storage.StoreRawText, bypass)
	mux := http.NewServeMux()
	h.Register(mux)

	startedAt := time.Now().UTC().Format(time.RFC3339)
	app := &application{cfg: cfg, log: log}
	if cfg.Supervise.Enabled {
		app.super = newSupervisor(cfg, log)
	}

	// Read-only observability API for the local admin UI (localhost-only). Registered
	// on the shared mux, so it is served on every configured listener (app.servers).
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

	if transparent {
		ca, err := mitm.EnsureCA(cfg.TLS.CACertPath, cfg.TLS.CAKeyPath, cfg.TLS.NameConstraints)
		if err != nil {
			cleanup()
			return nil, nil, fmt.Errorf("ensure CA: %w", err)
		}
		app.servers = append(app.servers, &http.Server{
			Addr:    cfg.Intercept.HTTPSListenAddr,
			Handler: mux,
			TLSConfig: &tls.Config{
				GetCertificate: ca.GetCertificate,
				MinVersion:     tls.VersionTLS12,
			},
		})
		if cfg.Intercept.ManageHostsFile {
			app.hostsMgr = hostsfile.New("", cfg.Intercept.Hosts)
		}
	}
	if legacy {
		app.servers = append(app.servers, &http.Server{Addr: cfg.Server.ListenAddr, Handler: mux})
	}

	return app, cleanup, nil
}

// start binds every listener BEFORE installing the hosts redirect (so traffic is
// never redirected to an unbound port), then serves each in its own goroutine.
func (a *application) start() error {
	type bound struct {
		srv *http.Server
		ln  net.Listener
	}
	var listeners []bound
	for _, srv := range a.servers {
		ln, err := net.Listen("tcp", srv.Addr)
		if err != nil {
			for _, b := range listeners { // unwind partial binds
				_ = b.ln.Close()
			}
			return fmt.Errorf("listen %s: %w", srv.Addr, err)
		}
		listeners = append(listeners, bound{srv, ln})
	}

	if a.hostsMgr != nil {
		if err := a.hostsMgr.Add(); err != nil {
			for _, b := range listeners {
				_ = b.ln.Close()
			}
			return fmt.Errorf("hosts redirect: %w", err)
		}
		a.log.Info("hosts redirect added", "hosts", a.cfg.Intercept.Hosts)
	}

	for _, b := range listeners {
		b := b
		tlsOn := b.srv.TLSConfig != nil
		a.log.Info("listening", "addr", b.srv.Addr, "tls", tlsOn,
			"mode", a.cfg.Mode, "upstream", a.cfg.Upstream.BaseURL)
		go func() {
			var err error
			if tlsOn {
				err = b.srv.ServeTLS(b.ln, "", "")
			} else {
				err = b.srv.Serve(b.ln)
			}
			if err != nil && err != http.ErrServerClosed {
				a.log.Error("listener stopped", "addr", b.srv.Addr, "err", err)
			}
		}()
	}

	// Listeners are live; bring up the user-session dependents (sidecar + web UI).
	// Best-effort: a failure here is logged, not returned, so the proxy still runs
	// (and fail-closes) even if no interactive user is logged in.
	if a.super != nil {
		a.super.Start()
	}
	return nil
}

// stop removes the hosts redirect FIRST (so the name resolves normally again
// before the listener goes away), then gracefully shuts the servers down.
func (a *application) stop() {
	if a.hostsMgr != nil {
		if err := a.hostsMgr.Remove(); err != nil {
			a.log.Warn("hosts redirect removal failed", "err", err)
		} else {
			a.log.Info("hosts redirect removed")
		}
	}
	// Tear down the user-session dependents (web UI then sidecar) before closing
	// our own listeners, with a port-scoped force-kill so nothing is orphaned.
	if a.super != nil {
		a.super.Stop()
	}
	ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	for _, srv := range a.servers {
		_ = srv.Shutdown(ctx)
	}
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

// newLogger returns a logger writing to cfg.Logging.File (or, when running as a
// service with no file configured, a default %ProgramData% log), else stdout.
func newLogger(cfg config.Config, asService bool) (*slog.Logger, func()) {
	lvl := slog.LevelInfo
	switch cfg.Logging.Level {
	case "debug":
		lvl = slog.LevelDebug
	case "warn":
		lvl = slog.LevelWarn
	case "error":
		lvl = slog.LevelError
	}

	out := os.Stdout
	closer := func() {}
	file := cfg.Logging.File
	if file == "" && asService {
		file = defaultServiceLog()
	}
	if file != "" {
		if err := os.MkdirAll(filepath.Dir(file), 0o755); err == nil {
			if f, err := os.OpenFile(file, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644); err == nil {
				out = f
				closer = func() { _ = f.Close() }
			}
		}
	}
	return slog.New(slog.NewTextHandler(out, &slog.HandlerOptions{Level: lvl})), closer
}

func defaultServiceLog() string {
	pd := os.Getenv("ProgramData")
	if pd == "" {
		return ""
	}
	return filepath.Join(pd, "PromptGate", "logs", "proxy.log")
}
