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
	good      = lipgloss.NewStyle().Foreground(lipgloss.Green)
	warn      = lipgloss.NewStyle().Foreground(lipgloss.Yellow)
	bad       = lipgloss.NewStyle().Foreground(lipgloss.Red)
	key       = lipgloss.NewStyle().Foreground(lipgloss.Blue)
	faint     = lipgloss.NewStyle().Faint(true)
	bold      = lipgloss.NewStyle().Bold(true)
	selectMark = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Blue)
	boldGood  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Green)
	boldWarn  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Yellow)
	boldBad   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Red)
	boldWhite = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.White)
	command   = lipgloss.NewStyle().Background(lipgloss.Color("#000000")).Foreground(lipgloss.White)
)

// Writer downgrades or strips ANSI from the environment and the destination.
func Writer(w io.Writer) io.Writer {
	return colorprofile.NewWriter(w, os.Environ())
}

// Bold marks a section title or a service name.
func Bold(s string) string { return bold.Render(s) }

// Faint marks unchanged, missing, and secondary text.
func Faint(s string) string { return faint.Render(s) }

// Key marks a keyboard shortcut in a shortcut bar.
func Key(s string) string { return key.Render(s) }

// SelectMark is the bold blue row cursor in cat9s.
func SelectMark(s string) string { return selectMark.Render(s) }

// Good marks a successful or steady state.
func Good(s string) string { return good.Render(s) }

// Warn marks a change that is not a failure.
func Warn(s string) string { return warn.Render(s) }

// Bad marks a failed probe or a process that is down.
func Bad(s string) string { return bad.Render(s) }

// BoldBad is Bad with emphasis for a selected table row.
func BoldBad(s string) string { return boldBad.Render(s) }

// BoldWarn is Warn with emphasis for a selected table row.
func BoldWarn(s string) string { return boldWarn.Render(s) }

// BoldWhite is faint table text on the selected row.
func BoldWhite(s string) string { return boldWhite.Render(s) }

// Command is a running argv on an explicit black background.
func Command(s string) string { return command.Render(s) }

// CommandCell paints a running argv, truncated to width runes with an ellipsis.
func CommandCell(width int, cmd string) string {
	if cmd == "" {
		return ""
	}
	return Command(truncateRunes(cmd, width))
}

func truncateRunes(s string, width int) string {
	if width <= 0 {
		return s
	}
	r := []rune(s)
	if len(r) < width {
		return s
	}
	if width == 1 {
		return "…"
	}
	return string(r[:width-1]) + "…"
}

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
	return probeCell(width, value, ok, false)
}

// ProbeEmph is Probe with bold for the selected table row.
func ProbeEmph(width int, value string, ok bool) string {
	return probeCell(width, value, ok, true)
}

func probeCell(width int, value string, ok bool, emph bool) string {
	cell := fmt.Sprintf("%-*s", width, value)
	switch value {
	case "skip", "-", "":
		if emph {
			return boldWhite.Render(cell)
		}
		return faint.Render(cell)
	}
	if ok {
		if emph {
			return boldGood.Render(cell)
		}
		return good.Render(cell)
	}
	if emph {
		return boldBad.Render(cell)
	}
	return bad.Render(cell)
}

// Host colors the approvalLevel and configured words of an approval summary.
func Host(summary string) string {
	return hostSummary(summary, false)
}

// HostEmph is Host with bold for the selected table row.
func HostEmph(summary string) string {
	return hostSummary(summary, true)
}

func hostSummary(summary string, emph bool) string {
	switch summary {
	case "", "-", "(no host advertised)", "(approval unavailable)":
		if summary == "" {
			return ""
		}
		if emph {
			return boldWhite.Render(summary)
		}
		return faint.Render(summary)
	}
	parts := strings.Split(summary, "; ")
	for i, part := range parts {
		level, cfg, ok := strings.Cut(part, " / ")
		if !ok {
			parts[i] = hostWord(part, emph)
			continue
		}
		parts[i] = hostWord(level, emph) + " / " + hostWord(cfg, emph)
	}
	return strings.Join(parts, "; ")
}

func hostWord(s string, emph bool) string {
	switch s {
	case "approved", "ready":
		if emph {
			return boldGood.Render(s)
		}
		return good.Render(s)
	case "draining":
		if emph {
			return boldWarn.Render(s)
		}
		return warn.Render(s)
	case "not-approved":
		if emph {
			return boldBad.Render(s)
		}
		return bad.Render(s)
	default:
		if emph {
			return boldWhite.Render(s)
		}
		return faint.Render(s)
	}
}
