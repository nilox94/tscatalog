package lifecycle

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/nilox94/tscatalog/internal/style"
)

const (
	LaunchKindWidth    = 10
	LaunchNameWidth    = 18
	LaunchStatusWidth  = 24
	LaunchCommandWidth = 40
)

// TableParts is one launch row for the describe table.
func (st LaunchStatus) TableParts() (kind, name, status, command string) {
	switch st.Shape {
	case ShapeProcess:
		kind = "process"
	case ShapeContainer:
		kind = "container"
	default:
		kind = "external"
	}
	if st.ID != "" {
		name = st.ID
	} else {
		name = "-"
	}
	var parts []string
	parts = append(parts, string(st.State))
	if st.State == LaunchRunning && st.PID != 0 {
		parts = append(parts, fmt.Sprintf("pid %d", st.PID))
	}
	detail := st.Detail
	if st.State == LaunchRunning && detail == st.Command {
		detail = ""
	}
	if detail != "" {
		parts = append(parts, detail)
	}
	status = strings.Join(parts, "  ")
	if st.State == LaunchRunning {
		command = st.Command
	}
	return kind, name, status, command
}

// FormatLaunchTable renders process, container, and external rows for describe views.
func FormatLaunchTable(rows []LaunchStatus) string {
	if len(rows) == 0 {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(
		&b, "  %-*s  %-*s  %-*s  %s\n",
		LaunchKindWidth, "KIND",
		LaunchNameWidth, "NAME",
		LaunchStatusWidth, "STATUS",
		"COMMAND",
	)
	for _, st := range rows {
		kind, name, status, cmd := st.TableParts()
		fmt.Fprintf(
			&b, "  %-*s  %-*s  %-*s  %s\n",
			LaunchKindWidth, clipCell(kind, LaunchKindWidth),
			LaunchNameWidth, clipCell(name, LaunchNameWidth),
			LaunchStatusWidth, clipCell(status, LaunchStatusWidth),
			style.CommandCell(LaunchCommandWidth, cmd),
		)
	}
	return "\n" + b.String()
}

func clipCell(s string, width int) string {
	if width <= 0 {
		return ""
	}
	if utf8.RuneCountInString(s) < width {
		return s
	}
	if width == 1 {
		return "…"
	}
	r := []rune(s)
	return string(r[:width-1]) + "…"
}
