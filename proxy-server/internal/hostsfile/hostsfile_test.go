package hostsfile

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func tempManager(t *testing.T, initial string) (*Manager, string) {
	t.Helper()
	p := filepath.Join(t.TempDir(), "hosts")
	if initial != "" {
		if err := os.WriteFile(p, []byte(initial), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return New(p, []string{"api.anthropic.com"}), p
}

func TestAddCreatesBlock(t *testing.T) {
	m, p := tempManager(t, "")
	if err := m.Add(); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(p)
	got := string(b)
	if !strings.Contains(got, "127.0.0.1 api.anthropic.com") {
		t.Errorf("block missing redirect line:\n%s", got)
	}
	present, _ := m.Present()
	if !present {
		t.Error("Present() = false after Add")
	}
}

func TestAddIsIdempotent(t *testing.T) {
	m, p := tempManager(t, "")
	for i := 0; i < 3; i++ {
		if err := m.Add(); err != nil {
			t.Fatal(err)
		}
	}
	b, _ := os.ReadFile(p)
	got := string(b)
	if n := strings.Count(got, beginMarker); n != 1 {
		t.Errorf("begin marker appears %d times, want 1:\n%s", n, got)
	}
	if n := strings.Count(got, "127.0.0.1 api.anthropic.com"); n != 1 {
		t.Errorf("redirect line appears %d times, want 1", n)
	}
}

func TestRemoveStripsBlockPreservingContent(t *testing.T) {
	const pre = "# system hosts\r\n127.0.0.1 localhost\r\n1.2.3.4 example.com\r\n"
	m, p := tempManager(t, pre)
	if err := m.Add(); err != nil {
		t.Fatal(err)
	}
	if err := m.Remove(); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(p)
	got := string(b)
	if strings.Contains(got, beginMarker) {
		t.Errorf("block still present after Remove:\n%s", got)
	}
	for _, want := range []string{"127.0.0.1 localhost", "1.2.3.4 example.com"} {
		if !strings.Contains(got, want) {
			t.Errorf("Remove dropped pre-existing line %q:\n%s", want, got)
		}
	}
	present, _ := m.Present()
	if present {
		t.Error("Present() = true after Remove")
	}
}

func TestRemoveMissingFileIsNoOp(t *testing.T) {
	m := New(filepath.Join(t.TempDir(), "nope"), []string{"api.anthropic.com"})
	if err := m.Remove(); err != nil {
		t.Errorf("Remove(missing) = %v, want nil", err)
	}
}

func TestAddPreservesCRLF(t *testing.T) {
	m, p := tempManager(t, "127.0.0.1 localhost\r\n")
	if err := m.Add(); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(p)
	if strings.Contains(strings.ReplaceAll(string(b), "\r\n", ""), "\n") {
		t.Errorf("found a bare LF; CRLF style not preserved:\n%q", string(b))
	}
}
