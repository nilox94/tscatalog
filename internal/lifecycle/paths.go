package lifecycle

import (
	"fmt"
	"os"
	"path/filepath"
)

// stateRoot is $XDG_STATE_HOME/tscatalog or ~/.local/state/tscatalog.
func stateRoot() (string, error) {
	if v := os.Getenv("XDG_STATE_HOME"); v != "" {
		return filepath.Join(v, "tscatalog"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".local", "state", "tscatalog"), nil
}

func runDir() (string, error) {
	root, err := stateRoot()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(root, "run")
	return dir, os.MkdirAll(dir, 0o700)
}

func logDir() (string, error) {
	root, err := stateRoot()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(root, "logs")
	return dir, os.MkdirAll(dir, 0o755)
}

func pidPath(name string, index int) (string, error) {
	dir, err := runDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, fmt.Sprintf("%s.%d.pid", name, index)), nil
}

// legacyRunDir is the pre-XDG macOS layout; read-only fallback for stop.
func legacyRunDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "Library", "Application Support", "tscatalog", "run"), nil
}

func legacyPidPath(name string, index int) (string, error) {
	dir, err := legacyRunDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, fmt.Sprintf("%s.%d.pid", name, index)), nil
}

// pidfilePaths returns candidate pidfile paths (current, then legacy macOS).
func pidfilePaths(name string, index int) ([]string, error) {
	cur, err := pidPath(name, index)
	if err != nil {
		return nil, err
	}
	leg, err := legacyPidPath(name, index)
	if err != nil {
		return nil, err
	}
	if cur == leg {
		return []string{cur}, nil
	}
	return []string{cur, leg}, nil
}

func logPath(name string) (string, error) {
	dir, err := logDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, name+".log"), nil
}
