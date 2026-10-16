//go:build unix

package lifecycle

func detachedAlive(name string, index int) bool {
	rec, err := readPidfile(name, index)
	if err != nil || rec.PID <= 0 {
		return false
	}
	return sameProcess(rec.PID, rec.StartID)
}
