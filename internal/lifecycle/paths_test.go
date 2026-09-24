package lifecycle

import (
	"os"
	"path/filepath"
	"testing"
)

func TestStateRootXDG(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_STATE_HOME", dir)
	root, err := stateRoot()
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(dir, "tscatalog")
	if root != want {
		t.Fatalf("got %q want %q", root, want)
	}
}

func TestPidfilePathsIncludesLegacy(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, "xdg-state"))
	paths, err := pidfilePaths("web", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) != 2 {
		t.Fatalf("paths %v", paths)
	}
	leg := filepath.Join(home, "Library", "Application Support", "tscatalog", "run", "web.0.pid")
	if paths[1] != leg {
		t.Fatalf("legacy %q want %q", paths[1], leg)
	}
}

func TestRunDirCreates(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_STATE_HOME", dir)
	run, err := runDir()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(run); err != nil {
		t.Fatal(err)
	}
}
