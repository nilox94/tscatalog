package composeclient

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestLoadComposeProject(t *testing.T) {
	if prev, ok := os.LookupEnv("COMPOSE_FILE"); ok {
		os.Unsetenv("COMPOSE_FILE")
		t.Cleanup(func() { _ = os.Setenv("COMPOSE_FILE", prev) })
	}
	if prev, ok := os.LookupEnv("COMPOSE_PROJECT_NAME"); ok {
		os.Unsetenv("COMPOSE_PROJECT_NAME")
		t.Cleanup(func() { _ = os.Setenv("COMPOSE_PROJECT_NAME", prev) })
	}
	dir := t.TempDir()
	body := []byte("name: clickstack\nservices:\n  app:\n    image: alpine\n")
	if err := os.WriteFile(filepath.Join(dir, "docker-compose.yml"), body, 0o644); err != nil {
		t.Fatal(err)
	}
	p, err := Load(context.Background(), dir, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if p.Name != "clickstack" {
		t.Fatalf("name %q", p.Name)
	}
	svc, ok := p.Services["app"]
	if !ok {
		t.Fatalf("services %#v", p.Services)
	}
	if svc.CustomLabels["com.docker.compose.project"] != "clickstack" ||
		svc.CustomLabels["com.docker.compose.service"] != "app" ||
		svc.CustomLabels["com.docker.compose.oneoff"] != "False" ||
		svc.CustomLabels["com.docker.compose.project.working_dir"] == "" {
		t.Fatalf("labels %#v", svc.CustomLabels)
	}
}

func TestLoadIncludeOverride(t *testing.T) {
	dir := t.TempDir()
	base := []byte("name: clickstack\nservices:\n  db:\n    image: alpine\n    volumes:\n      - .volumes/db:/data/db\n")
	if err := os.WriteFile(filepath.Join(dir, "base.yml"), base, 0o644); err != nil {
		t.Fatal(err)
	}
	top := []byte("name: clickstack\ninclude:\n  - path: base.yml\nservices:\n  db:\n    volumes: !override\n      - data:/data/db\nvolumes:\n  data:\n")
	if err := os.WriteFile(filepath.Join(dir, "docker-compose.yml"), top, 0o644); err != nil {
		t.Fatal(err)
	}
	p, err := Load(context.Background(), dir, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	svc, ok := p.Services["db"]
	if !ok {
		t.Fatalf("services %#v", p.Services)
	}
	if len(svc.Volumes) != 1 || svc.Volumes[0].Source != "data" {
		t.Fatalf("volumes %#v", svc.Volumes)
	}
}
