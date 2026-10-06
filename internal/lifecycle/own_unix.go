//go:build unix

package lifecycle

import (
	"errors"
	"syscall"
)

func detachedAlive(name string, index int) bool {
	_, pid, err := readPidfile(name, index)
	if err != nil || pid <= 0 {
		return false
	}
	err = syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}
