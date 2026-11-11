//go:build !unix

package lifecycle

import (
	"fmt"
	"runtime"

	"github.com/nilox94/tscatalog/internal/catalog"
)

func startDetached(name string, index int, spec catalog.LaunchSpec) error {
	_ = name
	_ = index
	_ = spec
	return fmt.Errorf("launch detach is only supported on unix (GOOS=%s)", runtime.GOOS)
}

func stopPID(name string, index int, id string) error {
	_ = name
	_ = index
	_ = id
	return fmt.Errorf("launch detach is only supported on unix (GOOS=%s)", runtime.GOOS)
}

func Reap(args []string) error {
	_ = args
	return fmt.Errorf("reap is only supported on unix (GOOS=%s)", runtime.GOOS)
}

func exitedLaunch(s catalog.Service) string {
	_ = s
	return ""
}
