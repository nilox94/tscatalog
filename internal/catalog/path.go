package catalog

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const DefaultFilename = "catalog.yaml"

// ConfigDir is $XDG_CONFIG_HOME/tscatalog or ~/.config/tscatalog.
func ConfigDir() (string, error) {
	if v := os.Getenv("XDG_CONFIG_HOME"); v != "" {
		return filepath.Join(v, "tscatalog"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "tscatalog"), nil
}

// DefaultConfigPath is the XDG catalog file path (may not exist yet).
func DefaultConfigPath() (string, error) {
	dir, err := ConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, DefaultFilename), nil
}

// ResolvePath picks the catalog file. explicit is the -f flag; empty means auto-discover.
// Order: -f → TSCATALOG_CATALOG → $XDG_CONFIG_HOME/tscatalog/catalog.yaml (if present) → ./catalog.yaml (if present).
func ResolvePath(explicit string) (string, error) {
	if p := strings.TrimSpace(explicit); p != "" {
		return filepath.Abs(p)
	}
	if v := strings.TrimSpace(os.Getenv("TSCATALOG_CATALOG")); v != "" {
		return filepath.Abs(v)
	}
	if xdg, err := DefaultConfigPath(); err == nil {
		if _, err := os.Stat(xdg); err == nil {
			return xdg, nil
		}
	}
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	local := filepath.Join(cwd, DefaultFilename)
	if _, err := os.Stat(local); err == nil {
		return local, nil
	}
	xdg, err := DefaultConfigPath()
	if err != nil {
		return "", fmt.Errorf("no catalog found: use -f, set TSCATALOG_CATALOG, or create %s", filepath.Join("~/.config/tscatalog", DefaultFilename))
	}
	return "", fmt.Errorf("no catalog found: use -f, set TSCATALOG_CATALOG, or create %s", xdg)
}
