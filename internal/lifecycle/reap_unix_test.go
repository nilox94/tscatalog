//go:build unix

package lifecycle

import (
	"context"
	"os"
	"strconv"
	"syscall"
	"testing"
	"time"

	"github.com/nilox94/tscatalog/internal/catalog"
)

func TestReaperRecordsExit(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	s := catalog.Service{
		Name:   "exit3",
		Launch: catalog.Launch{{Start: []string{"sh", "-c", "exit 3"}, Detach: true}},
	}
	_ = startDetached(s.Name, 0, s.Launch[0])
	exitfile, err := exitPath(s.Name, s.Launch[0].ID(0))
	if err != nil {
		t.Fatal(err)
	}
	rec := waitExit(t, exitfile)
	if rec.State != "exited" || rec.Code != 3 {
		t.Fatalf("%+v", rec)
	}
	pidfile, err := pidPath(s.Name, 0)
	if err != nil {
		t.Fatal(err)
	}
	waitGone(t, pidfile)
}

func TestReaperRecordsStop(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	s := catalog.Service{
		Name:   "sleeper",
		Launch: catalog.Launch{{Start: []string{"sleep", "30"}, Detach: true}},
	}
	if err := startDetached(s.Name, 0, s.Launch[0]); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		rec, err := readPidfile(s.Name, 0)
		if err == nil {
			_ = signalGroupPID(rec.PID)
		}
	})
	if err := stopPID(s.Name, 0, s.Launch[0].ID(0)); err != nil {
		t.Fatal(err)
	}
	exitfile, err := exitPath(s.Name, s.Launch[0].ID(0))
	if err != nil {
		t.Fatal(err)
	}
	rec := waitExit(t, exitfile)
	if rec.State != "stopped" {
		t.Fatalf("%+v", rec)
	}
}

func TestStartIDMismatch(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	s := catalog.Service{
		Name:   "mismatch",
		Launch: catalog.Launch{{Start: []string{"sleep", "30"}, Detach: true}},
	}
	path, err := pidPath(s.Name, 0)
	if err != nil {
		t.Fatal(err)
	}
	body := strconv.Itoa(os.Getpid()) + "\nnope\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	if sameProcess(os.Getpid(), "nope") {
		t.Fatal("start id nope should not match this process")
	}
	rows := LaunchStatuses(context.Background(), s, nil, nil)
	if len(rows) != 1 || rows[0].State == LaunchRunning {
		t.Fatalf("%+v", rows)
	}
}

func waitExit(t *testing.T, path string) ExitRecord {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	var last error
	for {
		rec, err := readExit(path)
		if err == nil {
			return rec
		}
		last = err
		if time.Now().After(deadline) {
			t.Fatalf("exit file %s: %v", path, last)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func waitGone(t *testing.T, path string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		_, err := os.Stat(path)
		if os.IsNotExist(err) {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("still present: %s (%v)", path, err)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestStopRejectsBadPidfile(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	s := catalog.Service{Name: "badpid", Launch: catalog.Launch{{Start: []string{"sleep", "30"}, Detach: true}}}
	path, err := pidPath(s.Name, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("0\nabc\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := stopPID(s.Name, 0, s.Launch[0].ID(0)); err == nil {
		t.Fatal("pid 0 should not be signaled")
	}
	if err := os.WriteFile(path, []byte("123\n\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := stopPID(s.Name, 0, s.Launch[0].ID(0)); err == nil {
		t.Fatal("missing start id should not be signaled")
	}
}

func signalGroupPID(pid int) error {
	return syscall.Kill(pid, syscall.SIGKILL)
}
