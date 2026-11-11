package lifecycle

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/nilox94/tscatalog/internal/catalog"
)

func TestComposeProjectResolvesRelativeFilesAgainstCwd(t *testing.T) {
	if prev, ok := os.LookupEnv("COMPOSE_FILE"); ok {
		os.Unsetenv("COMPOSE_FILE")
		t.Cleanup(func() { _ = os.Setenv("COMPOSE_FILE", prev) })
	} else {
		os.Unsetenv("COMPOSE_FILE")
	}
	if prev, ok := os.LookupEnv("COMPOSE_PROJECT_NAME"); ok {
		os.Unsetenv("COMPOSE_PROJECT_NAME")
		t.Cleanup(func() { _ = os.Setenv("COMPOSE_PROJECT_NAME", prev) })
	}

	proj := t.TempDir()
	away := t.TempDir()
	body := []byte("name: demo\nservices:\n  web:\n    image: nginx:alpine\n")
	if err := os.WriteFile(filepath.Join(proj, "compose.yaml"), body, 0o644); err != nil {
		t.Fatal(err)
	}
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(away); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(wd) })

	spec := catalog.LaunchSpec{
		Kind:     "compose",
		Cwd:      proj,
		Files:    []string{"compose.yaml"},
		Services: []string{"web"},
	}
	p, err := composeProject(context.Background(), spec)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := p.Services["web"]; !ok {
		t.Fatalf("services %#v", p.Services)
	}
}
