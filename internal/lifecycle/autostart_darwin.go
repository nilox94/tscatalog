//go:build darwin

package lifecycle

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
)

func guiDomain() string {
	return "gui/" + strconv.Itoa(os.Getuid())
}

// InstallLoginAgent writes the user LaunchAgent and bootstraps it.
func InstallLoginAgent(a LoginAgent) error {
	raw, err := a.Plist()
	if err != nil {
		return err
	}
	path, err := agentPlistPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		return err
	}
	domain := guiDomain()
	target := domain + "/" + AgentLabel
	_ = exec.Command("launchctl", "bootout", target).Run()
	cmd := exec.Command("launchctl", "bootstrap", domain, path)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("launchctl bootstrap %s: %w\n%s", path, err, out)
	}
	return nil
}

// UninstallLoginAgent unloads the login agent and removes its plist.
func UninstallLoginAgent() error {
	path, err := agentPlistPath()
	if err != nil {
		return err
	}
	target := guiDomain() + "/" + AgentLabel
	_ = exec.Command("launchctl", "bootout", target).Run()
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}
