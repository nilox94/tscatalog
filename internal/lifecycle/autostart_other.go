//go:build !darwin

package lifecycle

import "fmt"

// InstallLoginAgent is only implemented on macOS.
func InstallLoginAgent(LoginAgent) error {
	return fmt.Errorf("autostart install is only implemented on macOS")
}

// UninstallLoginAgent is only implemented on macOS.
func UninstallLoginAgent() error {
	return fmt.Errorf("autostart uninstall is only implemented on macOS")
}
