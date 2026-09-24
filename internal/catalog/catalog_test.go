package catalog

import (
	"os"
	"path/filepath"
	"testing"

	"tailscale.com/client/tailscale/v2"
)

func TestLoadAndServeConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "catalog.yaml")
	raw := []byte(`
endpoint: tcp:443
services:
  - name: web
    displayName: Example web UI
    local: http://127.0.0.1:8080
  - name: api
    local: http://127.0.0.1:8081
    comment: API
`)
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	f, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if f.Endpoint != "tcp:443" {
		t.Fatalf("endpoint %q", f.Endpoint)
	}
	sc := f.ServeConfig()
	got := sc.Services["svc:web"].Endpoints["tcp:443"]
	if got != "http://127.0.0.1:8080" {
		t.Fatalf("serve mapping %q", got)
	}
	vips := f.VIPServices()
	if len(vips) != 2 {
		t.Fatalf("vips %d", len(vips))
	}
	want := tailscale.Service{
		Name:  "svc:web",
		Ports: []string{"tcp:443"},
	}
	if vips[0].Name != want.Name || !PortsEqual(vips[0].Ports, want.Ports) {
		t.Fatalf("vip[0] = %+v", vips[0])
	}
}

func TestRejectsInvalidDottedName(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "catalog.yaml")
	raw := []byte(`
endpoint: tcp:443
services:
  - name: bad..name
    local: http://127.0.0.1:8080
`)
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("expected invalid name error")
	}
}

func TestLoadDottedServiceName(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "catalog.yaml")
	raw := []byte(`
endpoint: tcp:443
services:
  - name: clickstack.momoma
    local: http://127.0.0.1:8080
`)
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	f, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if f.Services[0].SVCName() != "svc:clickstack-momoma" {
		t.Fatalf("svc name %q", f.Services[0].SVCName())
	}
}

func TestRejectsNonLoopback(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "catalog.yaml")
	if err := os.WriteFile(path, []byte(`
services:
  - name: bad
    local: http://0.0.0.0:8080
`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("expected error")
	}
}

func TestRejectsBarePortLocal(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "catalog.yaml")
	if err := os.WriteFile(path, []byte(`
services:
  - name: bad
    local: "8080"
`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("expected error")
	}
}

func TestPortsEqualNormalizes(t *testing.T) {
	if !PortsEqual([]string{"443"}, []string{"tcp:443"}) {
		t.Fatal("443 should equal tcp:443")
	}
	if PortsEqual([]string{"tcp:443"}, []string{"tcp:8080"}) {
		t.Fatal("different ports")
	}
}

func TestLookup(t *testing.T) {
	f, err := Load(filepath.Join("..", "..", "catalog.example.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	s, ok := f.Lookup("svc:web")
	if !ok || s.Name != "web" || s.Local != "http://127.0.0.1:8080" {
		t.Fatalf("lookup web: ok=%v %+v", ok, s)
	}
	if _, ok := f.Lookup("missing"); ok {
		t.Fatal("expected miss")
	}
}

func TestExampleCatalog(t *testing.T) {
	f, err := Load(filepath.Join("..", "..", "catalog.example.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"web", "api"}
	if len(f.Services) != len(want) {
		t.Fatalf("got %d services", len(f.Services))
	}
	for i, name := range want {
		if f.Services[i].Name != name {
			t.Fatalf("services[%d]=%s want %s", i, f.Services[i].Name, name)
		}
	}
}
