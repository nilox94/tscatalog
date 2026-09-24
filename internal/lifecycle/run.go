package lifecycle

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"tscatalog/internal/catalog"
)

const defaultWait = 45 * time.Second

func expandHome(p string) string {
	if p == "" {
		return p
	}
	if p == "~" {
		home, err := os.UserHomeDir()
		if err != nil {
			return p
		}
		return home
	}
	if strings.HasPrefix(p, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return p
		}
		return filepath.Join(home, p[2:])
	}
	return p
}

func runDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(home, "Library", "Application Support", "tscatalog", "run")
	return dir, os.MkdirAll(dir, 0o700)
}

func logDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(home, "Library", "Logs", "tscatalog")
	return dir, os.MkdirAll(dir, 0o755)
}

func pidPath(name string, index int) (string, error) {
	dir, err := runDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, fmt.Sprintf("%s.%d.pid", name, index)), nil
}

func logPath(name string) (string, error) {
	dir, err := logDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, name+".log"), nil
}

func command(spec catalog.LaunchSpec) *exec.Cmd {
	cmd := exec.Command(spec.Start[0], spec.Start[1:]...)
	if spec.Cwd != "" {
		cmd.Dir = expandHome(spec.Cwd)
	}
	cmd.Env = os.Environ()
	return cmd
}

func commandStop(spec catalog.LaunchSpec) *exec.Cmd {
	cmd := exec.Command(spec.Stop[0], spec.Stop[1:]...)
	if spec.Cwd != "" {
		cmd.Dir = expandHome(spec.Cwd)
	}
	cmd.Env = os.Environ()
	return cmd
}

// Start runs each launch spec, then waits until the catalog local bind is ready.
func Start(ctx context.Context, s catalog.Service, w io.Writer) error {
	if !s.Launch.Defined() {
		return fmt.Errorf("%s: no launch in catalog (start it yourself, then tscatalog check)", s.Name)
	}
	if LocalReady(ctx, s) {
		fmt.Fprintf(w, "%s already listening on %s\n", s.Name, s.Local)
		return nil
	}
	for i, spec := range s.Launch {
		if spec.Detach {
			if err := startDetached(s.Name, i, spec); err != nil {
				return fmt.Errorf("%s launch[%d]: %w", s.Name, i, err)
			}
			fmt.Fprintf(w, "%s launch[%d] started (detached) %s\n", s.Name, i, strings.Join(spec.Start, " "))
			continue
		}
		cmd := command(spec)
		out, err := cmd.CombinedOutput()
		if len(out) > 0 {
			fmt.Fprint(w, string(out))
		}
		if err != nil {
			return fmt.Errorf("%s launch[%d]: %w", s.Name, i, err)
		}
		fmt.Fprintf(w, "%s launch[%d] %s\n", s.Name, i, strings.Join(spec.Start, " "))
	}
	deadline := time.Now().Add(defaultWait)
	for {
		if LocalReady(ctx, s) {
			fmt.Fprintf(w, "%s ready on %s\n", s.Name, s.Local)
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("%s: still not listening on %s after %s", s.Name, s.Local, defaultWait)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(250 * time.Millisecond):
		}
	}
}

func startDetached(name string, index int, spec catalog.LaunchSpec) error {
	logFile, err := logPath(name)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(logFile, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	cmd := command(spec)
	cmd.Stdout = f
	cmd.Stderr = f
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		f.Close()
		return err
	}
	path, err := pidPath(name, index)
	if err != nil {
		return err
	}
	if err := os.WriteFile(path, []byte(strconv.Itoa(cmd.Process.Pid)+"\n"), 0o600); err != nil {
		return err
	}
	go func() {
		_ = cmd.Wait()
		f.Close()
	}()
	return nil
}

// Stop runs each spec's stop argv, or SIGTERM to a detached pidfile.
func Stop(ctx context.Context, s catalog.Service, w io.Writer) error {
	_ = ctx
	if !s.Launch.Defined() {
		return fmt.Errorf("%s: no launch in catalog", s.Name)
	}
	var first error
	for i, spec := range s.Launch {
		if len(spec.Stop) > 0 {
			cmd := commandStop(spec)
			out, err := cmd.CombinedOutput()
			if len(out) > 0 {
				fmt.Fprint(w, string(out))
			}
			if err != nil && first == nil {
				first = fmt.Errorf("%s stop[%d]: %w", s.Name, i, err)
			}
			fmt.Fprintf(w, "%s stop[%d] %s\n", s.Name, i, strings.Join(spec.Stop, " "))
			continue
		}
		if !spec.Detach {
			if first == nil {
				first = fmt.Errorf("%s launch[%d]: no stop argv (and not detached)", s.Name, i)
			}
			continue
		}
		if err := stopPID(s.Name, i); err != nil && first == nil {
			first = err
		} else if err == nil {
			fmt.Fprintf(w, "%s stop[%d] killed detached pid\n", s.Name, i)
		}
	}
	return first
}

func stopPID(name string, index int) error {
	path, err := pidPath(name, index)
	if err != nil {
		return err
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("%s: no pidfile (not started by tscatalog?)", name)
		}
		return err
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(raw)))
	if err != nil {
		return fmt.Errorf("%s: bad pidfile: %w", name, err)
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	if err := proc.Signal(syscall.SIGTERM); err != nil {
		return fmt.Errorf("%s: signal %d: %w", name, pid, err)
	}
	_ = os.Remove(path)
	return nil
}
