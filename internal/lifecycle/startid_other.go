//go:build unix && !linux && !darwin

package lifecycle

func processStartID(int) (string, error) {
	return "", nil
}
