package lifecycle

import (
	"bytes"
	"context"
	"strings"
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

func TestExternalLogsStripTailscaleEnv(t *testing.T) {
	t.Setenv("TS_API_KEY", "tskey-api-secret")
	s := catalog.Service{
		Name:  "web",
		Local: "http://127.0.0.1:9",
		Launch: catalog.Launch{{
			Start: []string{"true"},
			Logs:  []string{"sh", "-c", "printf '%s\n' \"$TS_API_KEY\""},
		}},
	}
	var buf bytes.Buffer
	if err := Logs(context.Background(), s, "", 100, false, false, &buf, nil); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(buf.String(), "tskey-api-secret") {
		t.Fatalf("logs saw TS_API_KEY: %q", buf.String())
	}
	lines, _, _, err := CaptureLogs(context.Background(), s, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(strings.Join(lines, "\n"), "tskey-api-secret") {
		t.Fatalf("capture saw TS_API_KEY: %#v", lines)
	}
}
