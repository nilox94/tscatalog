package lifecycle

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"tscatalog/internal/catalog"
	"tscatalog/internal/style"
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
		return fmt.Errorf("%s: no launch in catalog (start it yourself, then tscatalog health)", s.Name)
	}
	if LocalReady(ctx, s) {
		fmt.Fprintf(w, "%s %s on %s\n", s.Name, style.Faint("already listening"), s.Local)
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
			fmt.Fprintf(w, "%s %s %s\n", s.Name, style.Good("ready on"), s.Local)
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
