package catalog

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLaunchSingleAndList(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "catalog.yaml")
	raw := []byte(`
endpoint: tcp:443
services:
  - name: web
    local: http://127.0.0.1:8080
    launch:
      start: [my-web, start]
      stop: [my-web, stop]
    check:
      path: /health
      expect: [200]
      public: [200]
  - name: app
    local: http://127.0.0.1:8081
    launch:
      - cwd: /tmp/ui
        start: [npm, run, dev]
        detach: true
      - cwd: /tmp/api
        start: [api, serve]
        detach: true
`)
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	f, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	web := f.Services[0]
	if len(web.Launch) != 1 || web.Launch[0].Start[0] != "my-web" {
		t.Fatalf("web launch %+v", web.Launch)
	}
	if web.Check.LocalPath() != "/health" || web.Check.LocalExpect()[0] != 200 {
		t.Fatalf("web check %+v", web.Check)
	}
	app := f.Services[1]
	if len(app.Launch) != 2 || !app.Launch[1].Detach {
		t.Fatalf("app launch %+v", app.Launch)
	}
}

func TestLaunchRequiresStart(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "catalog.yaml")
	if err := os.WriteFile(path, []byte(`
services:
  - name: web
    local: http://127.0.0.1:8080
    launch:
      cwd: /tmp
`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("expected error")
	}
}

func TestCheckDefaults(t *testing.T) {
	var c Check
	if c.LocalExpect() != nil {
		t.Fatalf("tcp-only local expect %v", c.LocalExpect())
	}
	if got := c.PublicExpect(); len(got) < 2 || got[0] != 200 {
		t.Fatalf("public default %v", got)
	}
	c.Path = "health"
	if c.LocalPath() != "/health" {
		t.Fatalf("path %q", c.LocalPath())
	}
	if c.LocalExpect()[0] != 200 {
		t.Fatalf("path implies 200")
	}
}
