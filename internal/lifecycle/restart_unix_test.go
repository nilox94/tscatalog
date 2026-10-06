//go:build unix

package lifecycle

import (
	"context"
	"fmt"
	"io"
	"net"
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

func TestCatalogOwnsLivePID(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	cmd := exec.Command("sleep", "30")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = syscall.Kill(cmd.Process.Pid, syscall.SIGKILL) })
	path, err := pidPath("owned", 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(strconv.Itoa(cmd.Process.Pid)+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	s := catalog.Service{Name: "owned", Launch: catalog.Launch{{Start: []string{"sleep", "30"}, Detach: true}}}
	if !CatalogOwns(s) {
		t.Fatal("live pidfile should be owned")
	}
}

func TestRestartRefusesForeignPort(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	port := ln.Addr().(*net.TCPAddr).Port
	s := catalog.Service{
		Name:   "foreign",
		Local:  fmt.Sprintf("http://127.0.0.1:%d", port),
		Launch: catalog.Launch{{Start: []string{"sleep", "30"}, Detach: true}},
	}
	cat := &catalog.File{Services: []catalog.Service{s}}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	err = Restart(ctx, cat, "foreign", io.Discard)
	if err == nil || !strings.Contains(err.Error(), "something else holds") {
		t.Fatalf("got %v", err)
	}
}

func TestRestartLaunchesWhenDown(t *testing.T) {
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 not installed")
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()

	dir := t.TempDir()
	t.Setenv("XDG_STATE_HOME", dir)
	script := filepath.Join(dir, "serve.py")
	body := fmt.Sprintf("import socket\ns=socket.create_server(('127.0.0.1', %d))\nwhile True:\n    c,_=s.accept()\n    c.close()\n", port)
	if err := os.WriteFile(script, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	s := catalog.Service{
		Name:   "uptest",
		Local:  fmt.Sprintf("http://127.0.0.1:%d", port),
		Launch: catalog.Launch{{Start: []string{"python3", script}, Detach: true}},
	}
	cat := &catalog.File{Services: []catalog.Service{s}}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	t.Cleanup(func() { _ = Stop(context.Background(), s, io.Discard) })
	if err := Restart(ctx, cat, "uptest", io.Discard); err != nil {
		t.Fatal(err)
	}
	if !CatalogOwns(s) {
		t.Fatal("launched process should be owned")
	}
}
