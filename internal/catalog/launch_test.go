package catalog

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
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

func TestResolveKinds(t *testing.T) {
	proc, err := loadService(t, "      start: [sleep, \"30\"]\n      detach: true\n")
	if err != nil {
		t.Fatal(err)
	}
	if kind, err := proc.Launch[0].ResolveKind(); err != nil || kind != KindProcess {
		t.Fatalf("detach kind %s %v", kind, err)
	}

	comp, err := loadService(t, "      start: [docker, compose, up, -d]\n")
	if err != nil {
		t.Fatal(err)
	}
	if kind, err := comp.Launch[0].ResolveKind(); err != nil || kind != KindCompose {
		t.Fatalf("compose kind %s %v", kind, err)
	}

	ext, err := loadService(t, "      start: [helper, start]\n      stop: [helper, stop]\n")
	if err != nil {
		t.Fatal(err)
	}
	if kind, err := ext.Launch[0].ResolveKind(); err != nil || kind != KindExternal {
		t.Fatalf("external kind %s %v", kind, err)
	}
	warn := ext.Launch[0].Warnings()
	if len(warn) != 2 || warn[0] != "no status command; ps cannot tell if it is running" || warn[1] != "no logs command" {
		t.Fatalf("warnings %q", warn)
	}

	if _, err := loadService(t, "      kind: compose\n      services: [app]\n"); err != nil {
		t.Fatal(err)
	}
	if _, err := loadService(t, "      kind: process\n      start: [sleep]\n      services: [app]\n"); err == nil {
		t.Fatal("process with services should fail")
	}
	if _, err := loadService(t, "      start: [docker, compose, up, -d]\n      detach: true\n"); err == nil || !strings.Contains(err.Error(), "compose launch cannot set detach") {
		t.Fatalf("detach compose: %v", err)
	}
	if _, err := loadService(t, "      name: \"1\"\n      start: [sleep]\n      detach: true\n"); err == nil {
		t.Fatal("digit name should fail")
	}
	dup := `
      - name: api
        start: [a]
        detach: true
      - name: api
        start: [b]
        detach: true
`
	if _, err := loadService(t, dup); err == nil || !strings.Contains(err.Error(), "duplicate name") {
		t.Fatalf("duplicate: %v", err)
	}
}

func TestComposeUpArgv(t *testing.T) {
	services, files, project, ok := composeUp([]string{"docker", "compose", "-f", "compose.yml", "up", "-d", "app"})
	if !ok || project != "" || len(files) != 1 || files[0] != "compose.yml" || len(services) != 1 || services[0] != "app" {
		t.Fatalf("parsed services=%v files=%v project=%q ok=%v", services, files, project, ok)
	}
	if _, _, _, ok := composeUp([]string{"docker", "compose", "up", "--build"}); ok {
		t.Fatal("--build is not compose")
	}
}

func loadService(t *testing.T, launchYAML string) (Service, error) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "catalog.yaml")
	raw := fmt.Sprintf("endpoint: tcp:443\nservices:\n  - name: web\n    local: http://127.0.0.1:8080\n    launch:\n%s", launchYAML)
	if err := os.WriteFile(path, []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}
	f, err := Load(path)
	if err != nil {
		return Service{}, err
	}
	return f.Services[0], nil
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
