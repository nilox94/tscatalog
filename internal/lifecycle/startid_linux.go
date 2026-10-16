//go:build linux

package lifecycle

import (
	"fmt"
	"os"
	"strings"
)

// processStartID is field 22 of /proc/<pid>/stat (starttime ticks).
// The comm field may contain spaces and parentheses, so the fields after the
// last ")" are what we count. Index 19 of that tail is starttime.
func processStartID(pid int) (string, error) {
	raw, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return "", err
	}
	text := string(raw)
	end := strings.LastIndex(text, ")")
	if end < 0 || end+1 >= len(text) {
		return "", fmt.Errorf("stat: no comm")
	}
	fields := strings.Fields(text[end+1:])
	if len(fields) <= 19 {
		return "", fmt.Errorf("stat: short")
	}
	return fields[19], nil
}
