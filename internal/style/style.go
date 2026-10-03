// Package style paints status words for a terminal.
// Printers emit lipgloss sequences. Writer strips them when the destination
// is not a color terminal: a pipe, a buffer, or NO_COLOR.
package style

import (
	"fmt"
	"io"
	"os"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/colorprofile"
)

var (
	good  = lipgloss.NewStyle().Foreground(lipgloss.Green)
	warn  = lipgloss.NewStyle().Foreground(lipgloss.Yellow)
	bad   = lipgloss.NewStyle().Foreground(lipgloss.Red)
	faint = lipgloss.NewStyle().Faint(true)
	bold  = lipgloss.NewStyle().Bold(true)
)

// Writer downgrades or strips ANSI from the environment and the destination.
func Writer(w io.Writer) io.Writer {
	return colorprofile.NewWriter(w, os.Environ())
}

// Bold marks a section title or a service name.
func Bold(s string) string { return bold.Render(s) }

// Faint marks unchanged, missing, and secondary text.
func Faint(s string) string { return faint.Render(s) }

// Good marks a successful or steady state.
func Good(s string) string { return good.Render(s) }

// Warn marks a change that is not a failure.
func Warn(s string) string { return warn.Render(s) }

// Action pads a plan action to 8 columns, then colors the padded cell.
// Padding comes first so ANSI codes do not shift the column.
func Action(action string) string {
	cell := fmt.Sprintf("%-8s", action)
	switch action {
	case "create":
		return good.Render(cell)
	case "update":
		return warn.Render(cell)
	case "unchanged":
		return faint.Render(cell)
	default:
		return cell
	}
}

// Probe pads a health cell, then colors success green and failure red.
// skip, "-", and empty stay faint.
func Probe(width int, value string, ok bool) string {
	cell := fmt.Sprintf("%-*s", width, value)
	switch value {
	case "skip", "-", "":
		return faint.Render(cell)
	}
	if ok {
		return good.Render(cell)
	}
	return bad.Render(cell)
}

// Host colors the approvalLevel and configured words of an approval summary.
func Host(summary string) string {
	switch summary {
	case "", "-", "(no host advertised)", "(approval unavailable)":
		if summary == "" {
			return ""
		}
		return faint.Render(summary)
	}
	parts := strings.Split(summary, "; ")
	for i, part := range parts {
		level, cfg, ok := strings.Cut(part, " / ")
		if !ok {
			parts[i] = hostWord(part)
			continue
		}
		parts[i] = hostWord(level) + " / " + hostWord(cfg)
	}
	return strings.Join(parts, "; ")
}

func hostWord(s string) string {
	switch s {
	case "approved", "ready":
		return good.Render(s)
	case "draining":
		return warn.Render(s)
	case "not-approved":
		return bad.Render(s)
	default:
		return faint.Render(s)
	}
}
