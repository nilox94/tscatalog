package lifecycle

import (
	"context"
	"testing"

	"github.com/nilox94/tscatalog/internal/catalog"
)

func TestCaptureExternalLogs(t *testing.T) {
	s := catalog.Service{
		Name:  "web",
		Local: "http://127.0.0.1:9",
		Launch: catalog.Launch{{
			Start: []string{"true"},
			Logs:  []string{"echo", "web is idle"},
		}},
	}
	lines, _, _, err := CaptureLogs(context.Background(), s, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != 1 || lines[0] != "web is idle" {
		t.Fatalf("lines %#v", lines)
	}
}
