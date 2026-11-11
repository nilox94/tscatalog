package host

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestGetConfigCommandError(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "tailscale")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	_, err := CLI{Bin: bin}.GetConfig(context.Background())
	if err == nil {
		t.Fatal("expected get-config failure")
	}
}
