package lifecycle

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"tscatalog/internal/catalog"
)

func TestStartDetachedAndStop(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "loop.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nwhile true; do sleep 30; done\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	s := catalog.Service{
		Name: "looptest-" + filepath.Base(dir),
		Launch: catalog.Launch{{
			Start:  []string{script},
			Detach: true,
		}},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := startDetached(s.Name, 0, s.Launch[0]); err != nil {
		t.Fatal(err)
	}
	path, err := pidPath(s.Name, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.ReadFile(path); err != nil {
		t.Fatal(err)
	}
	if err := Stop(ctx, s, io.Discard); err != nil {
		t.Fatal(err)
	}
}
