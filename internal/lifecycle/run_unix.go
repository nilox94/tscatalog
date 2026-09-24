//go:build unix

package lifecycle

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"syscall"

	"tscatalog/internal/catalog"
)

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

func stopPID(name string, index int) error {
	path, pid, err := readPidfile(name, index)
	if err != nil {
		return err
	}
	if err := syscall.Kill(pid, syscall.SIGTERM); err != nil {
		return fmt.Errorf("%s: signal %d: %w", name, pid, err)
	}
	_ = os.Remove(path)
	return nil
}

func readPidfile(name string, index int) (path string, pid int, err error) {
	paths, err := pidfilePaths(name, index)
	if err != nil {
		return "", 0, err
	}
	var sawMissing bool
	for _, p := range paths {
		raw, err := os.ReadFile(p)
		if err != nil {
			if os.IsNotExist(err) {
				sawMissing = true
				continue
			}
			return "", 0, err
		}
		pid, err := strconv.Atoi(strings.TrimSpace(string(raw)))
		if err != nil {
			return "", 0, fmt.Errorf("%s: bad pidfile: %w", name, err)
		}
		return p, pid, nil
	}
	if sawMissing {
		return "", 0, fmt.Errorf("%s: no pidfile (not started by tscatalog?)", name)
	}
	return "", 0, fmt.Errorf("%s: no pidfile (not started by tscatalog?)", name)
}
