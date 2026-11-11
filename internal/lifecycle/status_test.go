package lifecycle

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/nilox94/tscatalog/internal/catalog"
)

func TestLaunchStatusUnwatched(t *testing.T) {
	s := catalog.Service{
		Name: "helper",
		Launch: catalog.Launch{{
			Start: []string{"helper", "start"},
			Stop:  []string{"helper", "stop"},
		}},
	}
	rows := LaunchStatuses(context.Background(), s, nil, nil)
	if len(rows) != 1 {
		t.Fatalf("rows %+v", rows)
	}
	if rows[0].State != LaunchUnwatched || rows[0].Detail != "no status command" {
		t.Fatalf("%+v", rows[0])
	}
	if rows[0].Shape != "" || rows[0].PID != 0 || rows[0].ID != "" {
		t.Fatalf("%+v", rows[0])
	}
}

func TestDescribePartsCommand(t *testing.T) {
	key, status, cmd := (LaunchStatus{
		Shape:   ShapeProcess,
		State:   LaunchRunning,
		PID:     42,
		Command: "/bin/app --flag",
		Detail:  "/bin/app --flag",
	}).DescribeParts()
	if key != "process" || cmd != "/bin/app --flag" {
		t.Fatalf("key=%q status=%q cmd=%q", key, status, cmd)
	}
	if strings.Contains(status, "/bin/app") {
		t.Fatalf("status should omit duplicated command: %q", status)
	}
	if !strings.Contains(status, "pid 42") {
		t.Fatalf("status %q", status)
	}
	key, status, cmd = (LaunchStatus{
		Shape:   ShapeContainer,
		ID:      "app",
		State:   LaunchRunning,
		Detail:  "healthy",
		Command: "/usr/sbin/nginx",
	}).DescribeParts()
	if key != "container" || cmd != "/usr/sbin/nginx" || !strings.Contains(status, "healthy") {
		t.Fatalf("key=%q status=%q cmd=%q", key, status, cmd)
	}
	_, _, cmd = (LaunchStatus{Shape: ShapeProcess, State: LaunchExited, Command: "leftover"}).DescribeParts()
	if cmd != "" {
		t.Fatalf("exited cmd %q", cmd)
	}
}

func TestFormatPS(t *testing.T) {
	got := FormatPS([]LaunchStatus{
		{
			Service: "helper",
			State:   LaunchUnwatched,
			Detail:  "no status command",
		},
		{
			Service: "stack",
			ID:      "app",
			Shape:   ShapeContainer,
			State:   LaunchRunning,
			Detail:  "healthy",
		},
	})
	header := fmt.Sprintf("%-16s %-18s %-9s %-10s %-8s %s", "SERVICE", "NAME", "SHAPE", "STATE", "PID", "DETAIL")
	if !strings.HasPrefix(got, header+"\n") {
		t.Fatalf("header:\n%s", got)
	}
	if !strings.Contains(got, "helper") || !strings.Contains(got, "-") {
		t.Fatalf("external row:\n%s", got)
	}
	lines := strings.Split(strings.TrimRight(got, "\n"), "\n")
	if len(lines) != 3 {
		t.Fatalf("lines %d\n%s", len(lines), got)
	}
	if !strings.Contains(lines[1], "helper") || !strings.Contains(lines[1], "-") {
		t.Fatalf("external %q", lines[1])
	}
	// shape and pid of a single unnamed external row are dashes
	fields := strings.Fields(lines[1])
	if len(fields) < 5 || fields[1] != "-" || fields[2] != "-" || fields[4] != "-" {
		t.Fatalf("external fields %v", fields)
	}
	cfields := strings.Fields(lines[2])
	if len(cfields) < 4 || cfields[1] != "app" || cfields[2] != "container" {
		t.Fatalf("container fields %v", cfields)
	}
	if strings.Contains(got, "workload") || strings.Contains(got, "launch") {
		t.Fatalf("forbidden word:\n%s", got)
	}
	if FormatPS(nil) != header+"\n" {
		t.Fatalf("empty:\n%s", FormatPS(nil))
	}
}
