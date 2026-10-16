//go:build unix

package lifecycle

import (
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"tscatalog/internal/catalog"
)

func TestStartReportsExitReason(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	s := catalog.Service{
		Name:   "fastfail",
		Local:  "http://127.0.0.1:9",
		Launch: catalog.Launch{{Start: []string{"sh", "-c", "echo error: broker missing >&2; exit 1"}, Detach: true}},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	err := Start(ctx, s, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "broker missing") {
		t.Fatalf("err %v", err)
	}
	if ctx.Err() != nil {
		t.Fatal("start waited out the deadline instead of the exit")
	}
}

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

func TestStopPIDLegacyPath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, "xdg-state"))

	legDir := filepath.Join(home, "Library", "Application Support", "tscatalog", "run")
	if err := os.MkdirAll(legDir, 0o700); err != nil {
		t.Fatal(err)
	}
	legPath := filepath.Join(legDir, "legacy.0.pid")
	script := filepath.Join(t.TempDir(), "short.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nsleep 60\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(script)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = syscall.Kill(cmd.Process.Pid, syscall.SIGKILL) }()
	if err := os.WriteFile(legPath, []byte(strconv.Itoa(cmd.Process.Pid)+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := stopPID("legacy", 0, "0"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(legPath); !os.IsNotExist(err) {
		t.Fatalf("legacy pidfile should be removed: %v", err)
	}
}
