//go:build !unix

package lifecycle

import (
	"fmt"
	"runtime"

	"tscatalog/internal/catalog"
)

func startDetached(name string, index int, spec catalog.LaunchSpec) error {
	_ = name
	_ = index
	_ = spec
	return fmt.Errorf("launch detach is only supported on unix (GOOS=%s)", runtime.GOOS)
}

func stopPID(name string, index int) error {
	_ = name
	_ = index
	return fmt.Errorf("launch detach is only supported on unix (GOOS=%s)", runtime.GOOS)
}
