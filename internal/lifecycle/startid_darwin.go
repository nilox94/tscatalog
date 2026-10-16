//go:build darwin

package lifecycle

import (
	"fmt"

	"golang.org/x/sys/unix"
)

func processStartID(pid int) (string, error) {
	kp, err := unix.SysctlKinfoProc("kern.proc.pid", pid)
	if err != nil {
		return "", err
	}
	tv := kp.Proc.P_starttime
	return fmt.Sprintf("%d.%d", tv.Sec, tv.Usec), nil
}
