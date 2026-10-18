package lifecycle

import (
	"strings"
	"testing"

	"github.com/acarl005/stripansi"
)

func TestFormatLaunchTableCommandWidth(t *testing.T) {
	long := strings.Repeat("x", 50)
	got := stripansi.Strip(FormatLaunchTable([]LaunchStatus{{
		Shape:   ShapeProcess,
		State:   LaunchRunning,
		Command: long,
		Detail:  long,
	}}))
	if !strings.Contains(got, "KIND") || !strings.Contains(got, "COMMAND") {
		t.Fatalf("header:\n%s", got)
	}
	wantCmd := strings.Repeat("x", LaunchCommandWidth-1) + "…"
	if !strings.Contains(got, wantCmd) {
		t.Fatalf("missing truncated command %q in:\n%s", wantCmd, got)
	}
}
