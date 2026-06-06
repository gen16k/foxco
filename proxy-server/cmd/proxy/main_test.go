package main

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"local-lfm-dlp-proxy/internal/config"
)

func quietLog() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

// baseTestCfg returns a config safe for hermetic tests: keyword classifier, no
// warmup, no audit DB, CA written to a temp dir, and hosts management OFF so
// nothing touches the real system.
func baseTestCfg(t *testing.T) config.Config {
	t.Helper()
	cfg := config.Default()
	cfg.Storage.Type = "" // NopRecorder, no sqlite file
	cfg.Inference.WarmupOnStart = false
	cfg.Intercept.ManageHostsFile = false
	cfg.Intercept.HTTPSListenAddr = "127.0.0.1:0"
	cfg.Server.ListenAddr = "127.0.0.1:0"
	cfg.TLS.CACertPath = filepath.Join(t.TempDir(), "ca.crt")
	cfg.TLS.CAKeyPath = filepath.Join(t.TempDir(), "ca.key")
	return cfg
}

func TestBuildAppProxyModeStartsAndStops(t *testing.T) {
	cfg := baseTestCfg(t)
	cfg.Mode = "proxy"
	app, cleanup, err := buildApp(cfg, "keyword", quietLog())
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	if len(app.servers) != 1 {
		t.Fatalf("proxy mode: want 1 listener, got %d", len(app.servers))
	}
	if app.hostsMgr != nil {
		t.Error("proxy mode must not manage the hosts file")
	}
	if app.servers[0].TLSConfig != nil {
		t.Error("proxy-mode listener must be plain HTTP")
	}
	if err := app.start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	app.stop() // must not hang
}

func TestBuildAppTransparentWiring(t *testing.T) {
	cfg := baseTestCfg(t)
	cfg.Mode = "transparent"
	app, cleanup, err := buildApp(cfg, "keyword", quietLog())
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	if len(app.servers) != 1 {
		t.Fatalf("transparent mode: want 1 listener, got %d", len(app.servers))
	}
	if app.servers[0].TLSConfig == nil || app.servers[0].TLSConfig.GetCertificate == nil {
		t.Error("transparent-mode listener must have a TLS GetCertificate callback")
	}
	if app.hostsMgr != nil {
		t.Error("ManageHostsFile=false should leave hostsMgr nil")
	}
	// EnsureCA must have persisted the CA to the temp paths.
	if _, err := os.Stat(cfg.TLS.CACertPath); err != nil {
		t.Errorf("CA cert not persisted: %v", err)
	}
	if _, err := os.Stat(cfg.TLS.CAKeyPath); err != nil {
		t.Errorf("CA key not persisted: %v", err)
	}
}

func TestBuildAppBothModeHasTwoListeners(t *testing.T) {
	cfg := baseTestCfg(t)
	cfg.Mode = "both"
	app, cleanup, err := buildApp(cfg, "keyword", quietLog())
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	if len(app.servers) != 2 {
		t.Fatalf("both mode: want 2 listeners, got %d", len(app.servers))
	}
}

func TestBuildAppUnknownModeErrors(t *testing.T) {
	cfg := baseTestCfg(t)
	cfg.Mode = "nonsense"
	if _, _, err := buildApp(cfg, "keyword", quietLog()); err == nil {
		t.Error("unknown mode should error")
	}
}
