package tui

import (
	"context"
	"fmt"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"

	"tscatalog/internal/catalog"
	"tscatalog/internal/lifecycle"
)

const (
	keyBar    = "r restart  R restart all  x stop  / filter  enter describe  q quit"
	detailBar = "esc back  q quit"
)

// Loader fetches probes. The TUI calls it on a timer.
type Loader interface {
	Load(ctx context.Context) ([]Row, error)
}

// Runner stops and restarts local processes. It does not publish.
// Restart starts a service that is down.
type Runner interface {
	Stop(ctx context.Context, name string) (string, error)
	Restart(ctx context.Context, name string) (string, error)
	RestartAll(ctx context.Context, names []string) (string, error)
}

type model struct {
	ctx       context.Context
	cat       *catalog.File
	loader    Loader
	runner    Runner
	rows      []Row
	selected  string
	filter    string
	filtering bool
	detail    bool
	placed    bool
	busy      bool
	loading   bool
	status    string
	// errText is the open bottom panel. Empty means the panel is closed.
	errText string
	width   int
	height  int
	offset  int
	// anchor is the selected service's row inside the visible body.
	// Reloads keep that screen line when the service's list index changes.
	anchor int
}

type loadedMsg struct {
	rows []Row
	err  error
}

type actionDone struct {
	out string
	err error
}

type tickMsg struct{}

func newModel(ctx context.Context, cat *catalog.File, loader Loader, runner Runner) model {
	if ctx == nil {
		ctx = context.Background()
	}
	m := model{
		ctx:     ctx,
		cat:     cat,
		loader:  loader,
		runner:  runner,
		rows:    placeholders(cat),
		width:   100,
		height:  30,
		loading: true,
	}
	if len(m.rows) > 0 {
		m.selected = m.rows[0].Service.Name
	}
	return m
}

func placeholders(cat *catalog.File) []Row {
	if cat == nil {
		return nil
	}
	rows := make([]Row, len(cat.Services))
	for i, s := range cat.Services {
		rows[i] = Row{
			Service: s,
			Report: lifecycle.Report{
				Name:   s.Name,
				Bind:   "…",
				Local:  "…",
				Public: "…",
				Host:   "…",
				OK:     true,
			},
			Serve: "…",
			URL:   "…",
		}
	}
	return rows
}

// Run is the cat9s screen. It needs a terminal; the caller checks that.
func Run(ctx context.Context, cat *catalog.File) error {
	back := live{cat: cat}
	p := tea.NewProgram(newModel(ctx, cat, back, back), tea.WithContext(ctx))
	_, err := p.Run()
	return err
}

func (m model) Init() tea.Cmd {
	return tea.Batch(m.loadCmd(), m.tickCmd())
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		if msg.Width > 0 {
			m.width = msg.Width
		}
		if msg.Height > 0 {
			m.height = msg.Height
		}
		m.ensureVisible()
		return m, nil
	case tea.KeyPressMsg:
		return m.onKey(msg.String())
	case tickMsg:
		if m.loading || m.busy {
			return m, m.tickCmd()
		}
		m.loading = true
		return m, tea.Batch(m.loadCmd(), m.tickCmd())
	case loadedMsg:
		m.loading = false
		if msg.err != nil {
			m.status = msg.err.Error()
		}
		if len(msg.rows) > 0 {
			m.rows = msg.rows
		}
		if !m.placed {
			for _, row := range order(m.rows, m.filter) {
				if attention(row) {
					m.selected = row.Service.Name
					break
				}
			}
			m.placed = true
		}
		m.follow()
		return m, nil
	case actionDone:
		m.busy = false
		if msg.err != nil {
			m.errText = commandError(msg.out, msg.err)
			m.status = ""
		} else {
			m.errText = ""
			m.status = statusFrom(msg.out)
		}
		m.ensureVisible()
		m.syncAnchor()
		if m.loader == nil {
			return m, nil
		}
		m.loading = true
		return m, m.loadCmd()
	}
	return m, nil
}

func (m model) onKey(s string) (model, tea.Cmd) {
	if m.filtering {
		switch s {
		case "esc":
			m.filtering = false
			m.filter = ""
			m.ensureVisible()
		case "enter":
			m.filtering = false
			m.ensureVisible()
		case "backspace":
			m.filter = dropLast(m.filter)
			m.ensureVisible()
		case "ctrl+c":
			return m, tea.Quit
		default:
			if isPrintable(s) {
				m.filter += s
				m.ensureVisible()
			}
		}
		return m, nil
	}
	if m.detail {
		switch s {
		case "esc":
			m.detail = false
			m.ensureVisible()
			return m, nil
		case "q", "ctrl+c":
			return m, tea.Quit
		default:
			return m, nil
		}
	}
	switch s {
	case "q", "ctrl+c":
		return m, tea.Quit
	case "/":
		m.filtering = true
		m.filter = ""
		return m, nil
	case "esc":
		if m.errText != "" {
			m.errText = ""
			m.ensureVisible()
			m.syncAnchor()
			return m, nil
		}
		if m.filter != "" {
			m.filter = ""
			m.ensureVisible()
		}
		return m, nil
	case "enter":
		if _, ok := m.current(); ok {
			m.detail = true
		}
		return m, nil
	case "up", "k":
		m.move(-1)
		return m, nil
	case "down", "j":
		m.move(1)
		return m, nil
	case "r":
		return m.restartOne()
	case "R":
		return m.restartAll()
	case "x":
		return m.stopOne()
	}
	return m, nil
}

func (m model) restartOne() (model, tea.Cmd) {
	if m.busy {
		return m, nil
	}
	row, ok := m.current()
	if !ok {
		return m, nil
	}
	if !row.Fresh {
		m.status = "still probing"
		return m, nil
	}
	d := lifecycle.Decide(row.Service, row.Report, row.Owned)
	if d.Action == lifecycle.ActionNone {
		m.status = row.Service.Name + ": " + d.Note
		return m, nil
	}
	name := row.Service.Name
	return m.begin("restarting "+name+"…", func(ctx context.Context) (string, error) {
		return m.runner.Restart(ctx, name)
	})
}

func (m model) restartAll() (model, tea.Cmd) {
	if m.busy {
		return m, nil
	}
	names := m.restartNames()
	if len(names) == 0 {
		m.status = "nothing to restart"
		return m, nil
	}
	return m.begin(fmt.Sprintf("restarting %d…", len(names)), func(ctx context.Context) (string, error) {
		return m.runner.RestartAll(ctx, names)
	})
}

func (m model) stopOne() (model, tea.Cmd) {
	if m.busy {
		return m, nil
	}
	row, ok := m.current()
	if !ok {
		return m, nil
	}
	if !row.Service.Launch.Defined() {
		m.status = row.Service.Name + ": no launch in catalog"
		return m, nil
	}
	name := row.Service.Name
	return m.begin("stopping "+name+"…", func(ctx context.Context) (string, error) {
		return m.runner.Stop(ctx, name)
	})
}

func (m model) begin(label string, fn func(context.Context) (string, error)) (model, tea.Cmd) {
	m.busy = true
	m.errText = ""
	m.status = label
	m.ensureVisible()
	m.syncAnchor()
	ctx := m.ctx
	return m, func() tea.Msg {
		out, err := fn(ctx)
		return actionDone{out: out, err: err}
	}
}

func (m model) restartNames() []string {
	var names []string
	for _, row := range order(m.rows, m.filter) {
		if needsRestart(row) {
			names = append(names, row.Service.Name)
		}
	}
	return names
}

func (m model) loadCmd() tea.Cmd {
	if m.loader == nil {
		return nil
	}
	return func() tea.Msg {
		rows, err := m.loader.Load(m.ctx)
		return loadedMsg{rows: rows, err: err}
	}
}

func (m model) tickCmd() tea.Cmd {
	return tea.Tick(2*time.Second, func(time.Time) tea.Msg { return tickMsg{} })
}

func (m model) viewRows() ([]Row, int) {
	rows := order(m.rows, m.filter)
	if len(rows) == 0 {
		return nil, 0
	}
	for i, row := range rows {
		if row.Service.Name == m.selected {
			return rows, i
		}
	}
	return rows, 0
}

func (m model) current() (Row, bool) {
	rows, i := m.viewRows()
	if len(rows) == 0 {
		return Row{}, false
	}
	return rows[i], true
}

func (m *model) move(delta int) {
	rows, i := m.viewRows()
	if len(rows) == 0 {
		return
	}
	m.placed = true
	i += delta
	if i < 0 {
		i = 0
	}
	if i >= len(rows) {
		i = len(rows) - 1
	}
	m.selected = rows[i].Service.Name
	m.ensureVisible()
	m.syncAnchor()
}

// follow keeps the selected service on its previous screen line when a reload
// reorders the table. A short list stays pinned to the top, so the highlight
// moves up the screen with the service.
func (m *model) follow() {
	rows, cursor := m.viewRows()
	body := m.bodyHeight()
	if len(rows) <= body {
		m.offset = 0
	} else {
		m.offset = cursor - m.anchor
		if m.offset < 0 {
			m.offset = 0
		}
		if maxOff := len(rows) - body; m.offset > maxOff {
			m.offset = maxOff
		}
	}
	m.ensureVisible()
	m.syncAnchor()
}

func (m *model) syncAnchor() {
	_, cursor := m.viewRows()
	m.anchor = cursor - m.offset
	if m.anchor < 0 {
		m.anchor = 0
	}
}

func (m *model) ensureVisible() {
	_, cursor := m.viewRows()
	body := m.bodyHeight()
	if cursor < m.offset {
		m.offset = cursor
	}
	if cursor >= m.offset+body {
		m.offset = cursor - body + 1
	}
	if m.offset < 0 {
		m.offset = 0
	}
}

func (m model) bodyHeight() int {
	h := m.height - 5 - m.panelHeight()
	if h < 1 {
		return 1
	}
	return h
}

func isPrintable(s string) bool {
	r, size := utf8.DecodeRuneInString(s)
	return size == len(s) && unicode.IsPrint(r)
}

func dropLast(s string) string {
	runes := []rune(s)
	if len(runes) == 0 {
		return ""
	}
	return string(runes[:len(runes)-1])
}

func statusFrom(out string) string {
	out = strings.TrimSpace(out)
	if out == "" {
		return "done"
	}
	if i := strings.LastIndex(out, "\n"); i >= 0 {
		return strings.TrimSpace(out[i+1:])
	}
	return out
}
