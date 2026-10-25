//go:build unix

package lifecycle

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unicode/utf8"

	"tscatalog/internal/catalog"
)

const pidfileWait = 30 * time.Second

type pidRecord struct {
	Path    string
	PID     int
	StartID string
}

type noPidfileError struct {
	name string
}

func (e noPidfileError) Error() string {
	return fmt.Sprintf("%s: no pidfile (not started by tscatalog?)", e.name)
}

func isNoPidfile(err error) bool {
	var e noPidfileError
	return errors.As(err, &e)
}

// reaperExecutable is the binary that implements lifecycle.Reap (__reap).
// Override with TSCATALOG_REAPER when the caller is not tscatalog or cat9s.
func reaperExecutable() (string, error) {
	if p := strings.TrimSpace(os.Getenv("TSCATALOG_REAPER")); p != "" {
		return p, nil
	}
	return os.Executable()
}

func startDetached(name string, index int, spec catalog.LaunchSpec) error {
	id := spec.ID(index)
	pidfile, err := pidPath(name, index)
	if err != nil {
		return err
	}
	exitfile, err := exitPath(name, id)
	if err != nil {
		return err
	}
	logfile, err := logPath(name, id)
	if err != nil {
		return err
	}
	intent, err := stopIntentPath(name, id)
	if err != nil {
		return err
	}
	exe, err := reaperExecutable()
	if err != nil {
		return err
	}
	argv := append([]string{"__reap", pidfile, exitfile, logfile, intent, "--"}, spec.Start...)
	cmd := exec.Command(exe, argv...)
	// A previous run can leave these behind. The poll treats an exit file as
	// this attempt, so drop them before the reaper writes new ones.
	_ = os.Remove(pidfile)
	_ = os.Remove(exitfile)
	_ = os.Remove(intent)
	cmd.Dir = expandHome(spec.Cwd)
	cmd.Env = os.Environ()
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() { _ = cmd.Wait() }()

	deadline := time.Now().Add(pidfileWait)
	for {
		if _, err := os.Stat(pidfile); err == nil {
			return nil
		}
		if _, err := os.Stat(exitfile); err == nil {
			return fmt.Errorf("%s", launchExitReason(name, id))
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("reaper did not write a pidfile")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func stopPID(name string, index int, id string) error {
	rec, err := readPidfile(name, index)
	if err != nil {
		return err
	}
	if rec.StartID != "" && !sameProcess(rec.PID, rec.StartID) {
		return fmt.Errorf("%s: pid %d is not the process we started", name, rec.PID)
	}
	if rec.StartID != "" {
		intent, err := stopIntentPath(name, id)
		if err != nil {
			return err
		}
		if err := os.WriteFile(intent, []byte("1\n"), 0o600); err != nil {
			return err
		}
		if err := syscall.Kill(rec.PID, syscall.SIGTERM); err != nil {
			_ = os.Remove(intent)
			return fmt.Errorf("%s: signal %d: %w", name, rec.PID, err)
		}
		return nil
	}
	if err := syscall.Kill(rec.PID, syscall.SIGTERM); err != nil {
		return fmt.Errorf("%s: signal %d: %w", name, rec.PID, err)
	}
	exitfile, err := exitPath(name, id)
	if err != nil {
		return err
	}
	if err := writeExit(exitfile, ExitRecord{State: "stopped", At: time.Now()}); err != nil {
		return err
	}
	return os.Remove(rec.Path)
}

func readPidfile(name string, index int) (pidRecord, error) {
	paths, err := pidfilePaths(name, index)
	if err != nil {
		return pidRecord{}, err
	}
	sawMissing := false
	for _, p := range paths {
		raw, err := os.ReadFile(p)
		if err != nil {
			if os.IsNotExist(err) {
				sawMissing = true
				continue
			}
			return pidRecord{}, err
		}
		return parsePidfile(name, p, raw)
	}
	if sawMissing {
		return pidRecord{}, noPidfileError{name: name}
	}
	return pidRecord{}, noPidfileError{name: name}
}

func parsePidfile(name, path string, raw []byte) (pidRecord, error) {
	text := strings.TrimRight(string(raw), "\n")
	if text == "" {
		return pidRecord{}, fmt.Errorf("%s: bad pidfile: empty", name)
	}
	lines := strings.Split(text, "\n")
	pid, err := strconv.Atoi(strings.TrimSpace(lines[0]))
	if err != nil {
		return pidRecord{}, fmt.Errorf("%s: bad pidfile: %w", name, err)
	}
	rec := pidRecord{Path: path, PID: pid}
	if len(lines) >= 2 {
		rec.StartID = strings.TrimSpace(lines[1])
	}
	return rec, nil
}

func sameProcess(pid int, startID string) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	if err != nil && !errors.Is(err, syscall.EPERM) {
		return false
	}
	if startID == "" {
		return true
	}
	got, err := processStartID(pid)
	if err != nil || got == "" {
		return false
	}
	return got == startID
}

// Reap is the parent of a detached process. tscatalog exits after up, so this
// copy of the binary (re-exec'd with __reap) is what waits and records the exit.
func Reap(args []string) error {
	dash := -1
	for i, a := range args {
		if a == "--" {
			dash = i
			break
		}
	}
	if dash != 4 || dash+1 >= len(args) {
		return fmt.Errorf("reap: want pidfile exitfile logfile stopintent -- argv")
	}
	pidfile, exitfile, logfile, intent := args[0], args[1], args[2], args[3]
	argv := args[dash+1:]

	if _, err := os.Stat(logfile); err == nil {
		if err := os.Rename(logfile, logfile+".1"); err != nil {
			return err
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	f, err := os.OpenFile(logfile, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()

	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Stdout = f
	cmd.Stderr = f
	if err := cmd.Start(); err != nil {
		_ = writeExit(exitfile, ExitRecord{State: "exited", Code: 127, At: time.Now()})
		return err
	}
	startID, err := processStartID(cmd.Process.Pid)
	if err != nil {
		startID = ""
	}
	body := fmt.Sprintf("%d\n%s\n", cmd.Process.Pid, startID)
	if err := os.WriteFile(pidfile, []byte(body), 0o600); err != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return err
	}
	waitErr := cmd.Wait()
	rec := ExitRecord{At: time.Now()}
	if _, err := os.Stat(intent); err == nil {
		rec.State = "stopped"
		if sig := signalOf(waitErr); sig != "" {
			rec.Signal = sig
		}
		_ = os.Remove(intent)
	} else if sig := signalOf(waitErr); sig != "" {
		rec.State = "signaled"
		rec.Signal = sig
	} else if waitErr != nil {
		rec.State = "exited"
		if cmd.ProcessState != nil {
			rec.Code = cmd.ProcessState.ExitCode()
		} else {
			rec.Code = 1
		}
	} else {
		rec.State = "exited"
		rec.Code = 0
	}
	if err := writeExit(exitfile, rec); err != nil {
		_ = os.Remove(pidfile)
		return err
	}
	return os.Remove(pidfile)
}

// exitedLaunch is the reason a process launch has already been reaped.
// A live pidfile is not an exit, so a slow start is left to the ready wait.
func exitedLaunch(s catalog.Service) string {
	for i, spec := range s.Launch {
		kind, err := spec.ResolveKind()
		if err != nil || kind != catalog.KindProcess {
			continue
		}
		if detachedAlive(s.Name, i) {
			continue
		}
		exitfile, err := exitPath(s.Name, spec.ID(i))
		if err != nil {
			continue
		}
		if _, err := os.Stat(exitfile); err != nil {
			continue
		}
		return launchExitReason(s.Name, spec.ID(i))
	}
	return ""
}

func launchExitReason(name, id string) string {
	exitfile, err := exitPath(name, id)
	if err != nil {
		return "exited"
	}
	rec, err := readExit(exitfile)
	state := "exited"
	if err == nil && rec.State != "" {
		state = rec.State
	}
	logf, err := logPath(name, id)
	if err != nil {
		return state
	}
	if reason := logReason(logf); reason != "" {
		return state + ": " + reason
	}
	return state
}

func logReason(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return ""
	}
	const tail = 8192
	if st.Size() > tail {
		if _, err := f.Seek(st.Size()-tail, io.SeekStart); err != nil {
			return ""
		}
	}
	b, err := io.ReadAll(io.LimitReader(f, tail))
	if err != nil {
		return ""
	}
	var fallback, withError string
	for _, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || skipLogLine(line) {
			continue
		}
		fallback = line
		if strings.Contains(strings.ToLower(line), "error:") {
			withError = line
		}
	}
	if withError != "" {
		return cutReason(withError)
	}
	return cutReason(fallback)
}

func skipLogLine(line string) bool {
	if strings.HasPrefix(line, "at ") {
		return true
	}
	if i := strings.Index(line, " | "); i > 0 && i < 12 {
		return true
	}
	return false
}

func cutReason(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) <= 220 {
		return s
	}
	b := s[:220]
	for !utf8.ValidString(b) && len(b) > 0 {
		b = b[:len(b)-1]
	}
	return b + "…"
}

func signalOf(err error) string {
	if err == nil {
		return ""
	}
	var ee *exec.ExitError
	if !errors.As(err, &ee) {
		return ""
	}
	ws, ok := ee.Sys().(syscall.WaitStatus)
	if !ok || !ws.Signaled() {
		return ""
	}
	return ws.Signal().String()
}
