package catalog

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolvePathExplicit(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "mine.yaml")
	if err := os.WriteFile(path, []byte("services:\n  - name: x\n    local: http://127.0.0.1:1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := ResolvePath(path)
	if err != nil {
		t.Fatal(err)
	}
	if got != path {
		t.Fatalf("got %q want %q", got, path)
	}
}

func TestResolvePathEnv(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "from-env.yaml")
	if err := os.WriteFile(path, []byte("services:\n  - name: x\n    local: http://127.0.0.1:1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TSCATALOG_CATALOG", path)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(t.TempDir(), "cfg"))
	got, err := ResolvePath("")
	if err != nil {
		t.Fatal(err)
	}
	if got != path {
		t.Fatalf("got %q want %q", got, path)
	}
}

func TestResolvePathXDG(t *testing.T) {
	cfg := filepath.Join(t.TempDir(), "xdg-config")
	t.Setenv("XDG_CONFIG_HOME", cfg)
	t.Setenv("TSCATALOG_CATALOG", "")
	xdgDir := filepath.Join(cfg, "tscatalog")
	if err := os.MkdirAll(xdgDir, 0o700); err != nil {
		t.Fatal(err)
	}
	xdg := filepath.Join(xdgDir, DefaultFilename)
	if err := os.WriteFile(xdg, []byte("services:\n  - name: x\n    local: http://127.0.0.1:1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := ResolvePath("")
	if err != nil {
		t.Fatal(err)
	}
	if got != xdg {
		t.Fatalf("got %q want %q", got, xdg)
	}
}

func TestResolvePathCWD(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(t.TempDir(), "empty-cfg"))
	t.Setenv("TSCATALOG_CATALOG", "")
	local := filepath.Join(dir, DefaultFilename)
	if err := os.WriteFile(local, []byte("services:\n  - name: x\n    local: http://127.0.0.1:1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)
	got, err := ResolvePath("")
	if err != nil {
		t.Fatal(err)
	}
	if got != local {
		t.Fatalf("got %q want %q", got, local)
	}
}

func TestResolvePathMissing(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(t.TempDir(), "no-catalog"))
	t.Setenv("TSCATALOG_CATALOG", "")
	dir := t.TempDir()
	t.Chdir(dir)
	if _, err := ResolvePath(""); err == nil {
		t.Fatal("expected error")
	}
}

func TestConfigDirXDG(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", root)
	got, err := ConfigDir()
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(root, "tscatalog")
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}
