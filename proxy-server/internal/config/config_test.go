package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultTransparentMode(t *testing.T) {
	c := Default()
	if c.Mode != "transparent" {
		t.Errorf("Mode = %q, want transparent", c.Mode)
	}
	if len(c.Intercept.Hosts) != 1 || c.Intercept.Hosts[0] != "api.anthropic.com" {
		t.Errorf("Intercept.Hosts = %v, want [api.anthropic.com]", c.Intercept.Hosts)
	}
	if !c.Intercept.ManageHostsFile {
		t.Error("Intercept.ManageHostsFile = false, want true")
	}
	if c.Intercept.HTTPSListenAddr != "127.0.0.1:443" {
		t.Errorf("HTTPSListenAddr = %q, want 127.0.0.1:443", c.Intercept.HTTPSListenAddr)
	}
	if c.Upstream.ResolverDNS != "1.1.1.1:53" {
		t.Errorf("ResolverDNS = %q, want 1.1.1.1:53", c.Upstream.ResolverDNS)
	}
	if len(c.TLS.NameConstraints) != 1 || c.TLS.NameConstraints[0] != "anthropic.com" {
		t.Errorf("NameConstraints = %v, want [anthropic.com]", c.TLS.NameConstraints)
	}
	if c.Service.Name != "PromptGate" {
		t.Errorf("Service.Name = %q, want PromptGate", c.Service.Name)
	}
	// Defaults must still uphold the security invariants.
	if !c.DLP.FailClosed {
		t.Error("DLP.FailClosed = false, want true")
	}
	if c.Storage.StoreRawText {
		t.Error("Storage.StoreRawText = true, want false")
	}
}

func TestExpandPercent(t *testing.T) {
	t.Setenv("ProgramData", `C:\ProgramData`)
	cases := []struct {
		in, want string
	}{
		{`%ProgramData%\PromptGate\ca\ca.crt`, `C:\ProgramData\PromptGate\ca\ca.crt`},
		{`no-vars-here`, `no-vars-here`},
		{`%DEFINITELY_UNSET_VAR_XYZ%\x`, `%DEFINITELY_UNSET_VAR_XYZ%\x`}, // unknown left intact
		{`100%%done`, `100%done`},                                        // %% -> literal %
		{``, ``},
	}
	for _, tc := range cases {
		got := expandLocal(tc.in)
		// expandLocal also filepath.Clean()s; compare on cleaned want.
		want := tc.want
		if want != "" {
			want = filepath.Clean(want)
		}
		if got != want {
			t.Errorf("expandLocal(%q) = %q, want %q", tc.in, got, want)
		}
	}
}

func TestLoadOverlaysDefaults(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	// Override only mode + hosts; omitted keys (e.g. manage_hosts_file) must keep defaults.
	yaml := "mode: proxy\nintercept:\n  hosts: [\"api.anthropic.com\", \"extra.example.com\"]\n"
	if err := os.WriteFile(cfgPath, []byte(yaml), 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := Load(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if c.Mode != "proxy" {
		t.Errorf("Mode = %q, want proxy", c.Mode)
	}
	if len(c.Intercept.Hosts) != 2 {
		t.Errorf("Hosts = %v, want 2 entries", c.Intercept.Hosts)
	}
	if !c.Intercept.ManageHostsFile { // omitted in YAML -> default true survives
		t.Error("ManageHostsFile lost its default (true) on partial override")
	}
	if c.Upstream.BaseURL != "https://api.anthropic.com" { // omitted -> default
		t.Errorf("BaseURL = %q, want default", c.Upstream.BaseURL)
	}
}

func TestLoadMissingFileReturnsDefaults(t *testing.T) {
	c, err := Load(filepath.Join(t.TempDir(), "does-not-exist.yaml"))
	if err != nil {
		t.Fatalf("Load(missing) error = %v, want nil", err)
	}
	if c.Mode != "transparent" {
		t.Errorf("Mode = %q, want transparent (defaults)", c.Mode)
	}
}
