package lifecycle

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// AgentLabel is the launchd label for the one-shot login hook.
const AgentLabel = "com.tscatalog.autostart"

// LoginAgent is the macOS login hook. It is not the parent of catalog processes.
type LoginAgent struct {
	Executable string
	Catalog    string
	Path       string
	Log        string
}

// AutostartLogPath is stdout/stderr for the login hook, not for service processes.
func AutostartLogPath() (string, error) {
	dir, err := logDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "autostart.log"), nil
}

func agentPlistPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "Library", "LaunchAgents", AgentLabel+".plist"), nil
}

func rejectEphemeralExecutable(exe string) error {
	if strings.TrimSpace(exe) == "" {
		return fmt.Errorf("empty tscatalog path")
	}
	abs, err := filepath.Abs(exe)
	if err != nil {
		return err
	}
	if strings.Contains(abs, string(os.PathSeparator)+"go-build") {
		return fmt.Errorf("refusing to install a `go run` binary (%s); go install ./cmd/tscatalog first", abs)
	}
	return nil
}

func xmlEscape(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	s = strings.ReplaceAll(s, `"`, "&quot;")
	return s
}

// Plist is a RunAtLoad agent with no KeepAlive. stop of a child stays down
// until the next autostart, up, restart, or a new login session.
func (a LoginAgent) Plist() ([]byte, error) {
	if err := rejectEphemeralExecutable(a.Executable); err != nil {
		return nil, err
	}
	if strings.TrimSpace(a.Catalog) == "" {
		return nil, fmt.Errorf("empty catalog path")
	}
	if strings.TrimSpace(a.Path) == "" {
		return nil, fmt.Errorf("empty PATH")
	}
	if strings.TrimSpace(a.Log) == "" {
		return nil, fmt.Errorf("empty autostart log path")
	}
	exe, err := filepath.Abs(a.Executable)
	if err != nil {
		return nil, err
	}
	cat, err := filepath.Abs(a.Catalog)
	if err != nil {
		return nil, err
	}
	body := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key>
  <string>%s</string>
  <key>ProgramArguments</key>
  <array>
    <string>%s</string>
    <string>autostart</string>
  </array>
  <key>RunAtLoad</key>
  <true/>
  <key>EnvironmentVariables</key>
  <dict>
    <key>PATH</key>
    <string>%s</string>
    <key>TSCATALOG_CATALOG</key>
    <string>%s</string>
  </dict>
  <key>StandardOutPath</key>
  <string>%s</string>
  <key>StandardErrorPath</key>
  <string>%s</string>
</dict>
</plist>
`, AgentLabel, xmlEscape(exe), xmlEscape(a.Path), xmlEscape(cat), xmlEscape(a.Log), xmlEscape(a.Log))
	return []byte(body), nil
}
