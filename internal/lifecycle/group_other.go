//go:build !unix

package lifecycle

import (
	"os/exec"
	"syscall"
)

func setPgroup(cmd *exec.Cmd, sig syscall.Signal) {
	_ = cmd
	_ = sig
}

func signalGroup(cmd *exec.Cmd, _ syscall.Signal) error {
	if cmd == nil || cmd.Process == nil {
		return nil
	}
	return cmd.Process.Kill()
}
