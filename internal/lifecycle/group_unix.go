//go:build unix

package lifecycle

import (
	"os/exec"
	"syscall"
	"time"
)

func setPgroup(cmd *exec.Cmd, sig syscall.Signal) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		return signalGroup(cmd, sig)
	}
	cmd.WaitDelay = 200 * time.Millisecond
}

func signalGroup(cmd *exec.Cmd, sig syscall.Signal) error {
	if cmd == nil || cmd.Process == nil {
		return nil
	}
	return syscall.Kill(-cmd.Process.Pid, sig)
}
