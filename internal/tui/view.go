package tui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"

	"tscatalog/internal/lifecycle"
	"tscatalog/internal/style"
)

func (m model) View() tea.View {
	v := tea.NewView(m.render())
	v.AltScreen = true
	v.WindowTitle = "cat9s"
	if y, ok := m.cursorY(); ok {
		v.Cursor = tea.NewCursor(0, y)
	}
	return v
}

// cursorY is the screen line of the selected service, under the key bar,
// summary, status, the blank line, and the column header.
func (m model) cursorY() (int, bool) {
	if m.detail || m.filtering {
		return 0, false
	}
	rows, cursor := m.viewRows()
	if len(rows) == 0 || cursor < m.offset || cursor >= m.offset+m.bodyHeight() {
		return 0, false
	}
	return 5 + (cursor - m.offset), true
}

func (m model) render() string {
	var b strings.Builder
	if m.filtering {
		fmt.Fprintf(&b, "%s %s█\n", style.Bold("/"), m.filter)
	} else if m.detail {
		fmt.Fprintln(&b, style.Faint(detailBar))
	} else {
		fmt.Fprintln(&b, style.Faint(keyBar))
	}
	fmt.Fprintln(&b, summary(m.rows, m.filter))
	fmt.Fprintln(&b, paintStatus(m.status))

	if m.detail {
		if row, ok := m.current(); ok {
			b.WriteString(m.renderDetail(row))
		}
		return b.String()
	}

	panel := m.panelLines()
	rows, cursor := m.viewRows()
	nameW := nameWidth(rows)
	fmt.Fprintln(&b)
	fmt.Fprintf(&b, "  %-*s  %-8s  %-10s  %-10s  %s\n", nameW, "SERVICE", "BIND", "LOCAL", "PUBLIC", "HOST")
	room := m.bodyHeight()
	if len(rows) == 0 {
		fmt.Fprintln(&b, style.Faint("  (no match)"))
		writeBlank(&b, room-1)
		b.WriteString(joinPanel(panel))
		return b.String()
	}
	end := m.offset + room
	if end > len(rows) {
		end = len(rows)
	}
	start := m.offset
	if start < 0 {
		start = 0
	}
	if start > end {
		start = end
	}
	shown := 0
	for i := start; i < end; i++ {
		fmt.Fprintln(&b, formatRow(rows[i], i == cursor, nameW, m.width))
		shown++
	}
	writeBlank(&b, room-shown)
	b.WriteString(joinPanel(panel))
	return b.String()
}

// panelLines is the bottom error panel, including its rule.
// One list row stays visible. The panel itself stays within 8 lines.
func (m model) panelLines() []string {
	if m.errText == "" {
		return nil
	}
	budget := m.height - 6
	if budget < 2 {
		budget = 2
	}
	if budget > 8 {
		budget = 8
	}
	width := m.width
	if width < 1 {
		width = 1
	}
	text := wrapText(m.errText, width)
	room := budget - 1
	if len(text) > room {
		text = text[len(text)-room:]
		text[0] = "… " + text[0]
	}
	lines := make([]string, 0, 1+len(text))
	lines = append(lines, panelRule(width))
	for i, line := range text {
		if i == len(text)-1 {
			lines = append(lines, style.Bad(line))
			continue
		}
		lines = append(lines, style.Faint(line))
	}
	return lines
}

func (m model) panelHeight() int {
	return len(m.panelLines())
}

func panelRule(width int) string {
	title := " error "
	hint := " esc close "
	if width < len(title)+len(hint)+2 {
		return style.Bad("error") + "  " + style.Faint("esc close")
	}
	gap := width - len(title) - len(hint)
	return style.Bad(title) + style.Faint(strings.Repeat("-", gap)+hint)
}

func commandError(out string, err error) string {
	msg := strings.TrimSpace(err.Error())
	out = strings.TrimSpace(out)
	if out == "" {
		return msg
	}
	if msg == "" || strings.Contains(out, msg) {
		return out
	}
	return out + "\n" + msg
}

func wrapText(s string, width int) []string {
	if width < 1 {
		width = 1
	}
	var lines []string
	for _, para := range strings.Split(s, "\n") {
		runes := []rune(para)
		if len(runes) == 0 {
			lines = append(lines, "")
			continue
		}
		for len(runes) > width {
			lines = append(lines, string(runes[:width]))
			runes = runes[width:]
		}
		lines = append(lines, string(runes))
	}
	if len(lines) == 0 {
		return []string{""}
	}
	return lines
}

func writeBlank(b *strings.Builder, n int) {
	for range n {
		b.WriteByte('\n')
	}
}

func joinPanel(lines []string) string {
	if len(lines) == 0 {
		return ""
	}
	return strings.Join(lines, "\n") + "\n"
}

func formatRow(row Row, selected bool, nameW, width int) string {
	mark := " "
	if selected {
		mark = style.Bold(">")
	}
	name := row.Service.Name
	if len(name) > nameW {
		name = name[:nameW]
	}
	name = fmt.Sprintf("%-*s", nameW, name)
	if attention(row) {
		if row.Report.Bind == "down" || row.Report.Bind == "error" {
			name = style.Bad(name)
		} else {
			name = style.Warn(name)
		}
	} else {
		name = style.Faint(name)
	}
	line := fmt.Sprintf(
		"%s %s  %s  %s  %s  %s",
		mark,
		name,
		probeCell(8, row.Report.Bind, row.Report.Bind != "" && row.Report.Bind != "down" && row.Report.Bind != "error"),
		probeCell(10, row.Report.Local, lifecycle.ValueOK(row.Report.Local, row.Service.Check.LocalExpect())),
		probeCell(10, row.Report.Public, lifecycle.ValueOK(row.Report.Public, row.Service.Check.PublicExpect())),
		hostCell(row),
	)
	if selected {
		return style.Bar(line, width)
	}
	return line
}

func probeCell(width int, value string, ok bool) string {
	if value == "…" {
		return style.Faint(fmt.Sprintf("%-*s", width, value))
	}
	return style.Probe(width, value, ok)
}

func hostCell(row Row) string {
	if row.Report.Host == "…" {
		return style.Faint("…")
	}
	return style.Host(row.Report.Host)
}

func (m model) renderDetail(row Row) string {
	s := row.Service
	var b strings.Builder
	fmt.Fprintln(&b, style.Bold(s.Name))
	writeField(&b, "svc", s.SVCName())
	if s.TailscaleLabel() != s.Name {
		writeField(&b, "vip", s.TailscaleLabel())
	}
	writeField(&b, "url", style.Faint(row.URL))
	endpoint := "tcp:443"
	if m.cat != nil && m.cat.Endpoint != "" {
		endpoint = m.cat.Endpoint
	}
	writeField(&b, "local", endpoint+" → "+s.Local)
	writeField(&b, "serve", row.Serve)
	writeField(&b, "host", hostCell(row))
	req := "(none)"
	if len(s.Requires) > 0 {
		req = strings.Join(s.Requires, ", ")
	}
	writeField(&b, "requires", req)
	writeField(&b, "bind", row.Report.Bind)
	writeField(&b, "probe", "local "+row.Report.Local+"  public "+row.Report.Public)
	return b.String()
}

func writeField(b *strings.Builder, key, value string) {
	fmt.Fprintf(b, "  %s%s\n", style.Faint(fmt.Sprintf("%-10s", key)), value)
}

func summary(rows []Row, filter string) string {
	for _, row := range rows {
		if row.Fresh {
			return summaryCount(restartCount(rows, filter))
		}
	}
	return style.Faint("probing…")
}

func summaryCount(n int) string {
	switch n {
	case 0:
		return style.Faint("nothing to restart")
	case 1:
		return style.Warn("1 needs restart")
	default:
		return style.Warn(fmt.Sprintf("%d need restart", n))
	}
}

func restartCount(rows []Row, filter string) int {
	n := 0
	for _, row := range order(rows, filter) {
		if needsRestart(row) {
			n++
		}
	}
	return n
}

func nameWidth(rows []Row) int {
	w := len("SERVICE")
	for _, row := range rows {
		if n := len(row.Service.Name); n > w {
			w = n
		}
	}
	if w > 24 {
		return 24
	}
	return w
}

func paintStatus(s string) string {
	if s == "" {
		return ""
	}
	if strings.Contains(s, "holds") ||
		strings.Contains(s, "no launch") ||
		strings.Contains(s, "will not fix") ||
		strings.Contains(s, "error") ||
		strings.Contains(s, "still listening") {
		return style.Bad(s)
	}
	return s
}
