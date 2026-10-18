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

// Loader fetches probes. Prepare runs once per pass; Probe runs per service
// so the table can paint each row as it finishes.
type Loader interface {
	Prepare(ctx context.Context) (probePrep, error)
	Probe(ctx context.Context, prep probePrep, s catalog.Service) Row
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
	probeGen  int
	pending   int
	status    string
	// errText is the open bottom panel. Empty means the panel is closed.
	errText string
	width   int
	height  int
	offset  int
	// anchor is the selected service's row inside the visible body.
	// Reloads keep that screen line when the service's list index changes.
	anchor int

	logView      bool
	logLines     []string
	logFilter    string
	logFiltering bool
	logFollow    bool
	logWhich     string
	logOffset    int
	logStick     bool
	logCancel    context.CancelFunc
	logFiles     []trackedLog
	logStream    bool
}

type trackedLog struct {
	path    string
	offset  int64
	prefix  string
	partial string
}

type loadedMsg struct {
	rows []Row
	err  error
}

type preparedMsg struct {
	gen  int
	prep probePrep
	err  error
}

type probedMsg struct {
	gen int
	row Row
}

type actionDone struct {
	out string
	err error
}

type tickMsg struct{}

type logLoadedMsg struct {
	which  string
	lines  []string
	files  []lifecycle.LogFile
	stream bool
	err    error
}

type logTickMsg struct {
	which string
}

type logStreamMsg struct {
	which string
	line  string
	err   error
	ch    <-chan string
	done  bool
}

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
		m.probeGen++
		return m, tea.Batch(m.loadCmd(), m.tickCmd())
	case loadedMsg:
		m.loading = false
		m.pending = 0
		if msg.err != nil {
			m.status = msg.err.Error()
		}
		if len(msg.rows) > 0 {
			m.rows = msg.rows
		}
		m.placeInitial()
		m.follow()
		return m, nil
	case preparedMsg:
		if msg.gen != m.probeGen {
			return m, nil
		}
		if msg.err != nil {
			m.status = msg.err.Error()
		}
		if m.cat == nil || len(m.cat.Services) == 0 {
			m.pending = 0
			m.loading = false
			m.placeInitial()
			m.follow()
			return m, nil
		}
		m.pending = len(m.cat.Services)
		cmds := make([]tea.Cmd, 0, len(m.cat.Services))
		for _, s := range m.cat.Services {
			cmds = append(cmds, m.probeCmd(msg.gen, msg.prep, s))
		}
		return m, tea.Batch(cmds...)
	case probedMsg:
		if msg.gen != m.probeGen {
			return m, nil
		}
		m.applyProbe(msg.row)
		if m.pending > 0 {
			m.pending--
		}
		if m.loading && m.pending == 0 {
			m.loading = false
			m.placeInitial()
			m.follow()
			return m, nil
		}
		if m.placed {
			m.follow()
		}
		return m, nil
	case logLoadedMsg:
		if !m.logView || msg.which != m.logWhich {
			return m, nil
		}
		m.logFiles = nil
		for _, f := range msg.files {
			m.logFiles = append(m.logFiles, trackedLog{path: f.Path, offset: f.Offset, prefix: f.Prefix})
		}
		m.logStream = msg.stream
		m.logLines = nil
		m.logOffset = 0
		m.logStick = true
		if msg.err != nil {
			m.status = msg.err.Error()
		} else if m.status != "" && !m.logFollow {
			m.status = ""
		}
		m.appendLogs(msg.lines)
		if !m.logFollow {
			return m, nil
		}
		m.stopFollow()
		ctx, cancel := context.WithCancel(m.ctx)
		m.logCancel = cancel
		return m, m.followCmd(ctx)
	case logTickMsg:
		if !m.logView || !m.logFollow || msg.which != m.logWhich {
			return m, nil
		}
		var added []string
		for i := range m.logFiles {
			chunk, next, err := lifecycle.ReadSince(m.logFiles[i].path, m.logFiles[i].offset)
			if err != nil {
				continue
			}
			m.logFiles[i].offset = next
			text := m.logFiles[i].partial + string(chunk)
			parts := strings.Split(text, "\n")
			m.logFiles[i].partial = parts[len(parts)-1]
			for _, line := range parts[:len(parts)-1] {
				added = append(added, m.logFiles[i].prefix+line)
			}
		}
		m.appendLogs(added)
		return m, m.logTickCmd()
	case logStreamMsg:
		if !m.logView || !m.logFollow || msg.which != m.logWhich {
			return m, nil
		}
		if msg.err != nil {
			m.status = msg.err.Error()
		}
		if msg.line != "" {
			m.appendLogs([]string{msg.line})
		}
		if msg.done || msg.ch == nil {
			return m, nil
		}
		return m, readLogStream(m.logWhich, msg.ch)
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
		m.probeGen++
		return m, m.loadCmd()
	}
	return m, nil
}

func (m *model) placeInitial() {
	if m.placed {
		return
	}
	for _, row := range order(m.rows, m.filter) {
		if attention(row) {
			m.selected = row.Service.Name
			break
		}
	}
	m.placed = true
}

func (m *model) applyProbe(row Row) {
	for i := range m.rows {
		if m.rows[i].Service.Name == row.Service.Name {
			m.rows[i] = row
			return
		}
	}
	m.rows = append(m.rows, row)
}

func (m model) onKey(s string) (model, tea.Cmd) {
	if m.logView {
		return m.onLogKey(s)
	}
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
			return m, nil
		}
		return m, tea.Quit
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
	case "l":
		return m.openLogs()
	}
	return m, nil
}

func (m model) onLogKey(s string) (model, tea.Cmd) {
	if m.logFiltering {
		switch s {
		case "esc":
			m.logFiltering = false
		case "enter":
			m.logFiltering = false
			m.pinLogs()
		case "backspace":
			m.logFilter = dropLast(m.logFilter)
			m.pinLogs()
		case "ctrl+c":
			return m, tea.Quit
		default:
			if isPrintable(s) {
				m.logFilter += s
				m.pinLogs()
			}
		}
		return m, nil
	}
	switch s {
	case "q", "ctrl+c":
		return m, tea.Quit
	case "/":
		m.logFiltering = true
		m.logFilter = ""
		m.pinLogs()
		return m, nil
	case "esc":
		if m.logFilter != "" {
			m.logFilter = ""
			m.pinLogs()
			return m, nil
		}
		m.closeLogs()
		return m, nil
	case "f":
		if m.logFollow {
			m.logFollow = false
			m.stopFollow()
			return m, nil
		}
		m.logFollow = true
		m.logStick = true
		m.pinLogs()
		m.stopFollow()
		ctx, cancel := context.WithCancel(m.ctx)
		m.logCancel = cancel
		return m, m.followCmd(ctx)
	case "tab":
		return m.cycleLog()
	case "j", "down":
		m.scrollLog(1)
		return m, nil
	case "k", "up":
		m.scrollLog(-1)
		return m, nil
	}
	return m, nil
}

func (m model) openLogs() (model, tea.Cmd) {
	if m.busy {
		return m, nil
	}
	row, ok := m.current()
	if !ok {
		return m, nil
	}
	if logsBlocked(row.Service) {
		m.status = row.Service.Name + ": no logs command"
		return m, nil
	}
	m.logView = true
	m.logWhich = ""
	m.logFollow = false
	m.logStick = true
	m.logLines = nil
	m.logOffset = 0
	m.logFilter = ""
	m.logFiltering = false
	m.logFiles = nil
	m.logStream = false
	m.status = ""
	return m, m.loadLogs()
}

func logsBlocked(s catalog.Service) bool {
	if len(s.Launch) == 0 {
		return true
	}
	for _, spec := range s.Launch {
		kind, err := spec.ResolveKind()
		if err != nil {
			return false
		}
		if kind != catalog.KindExternal || len(spec.Logs) > 0 {
			return false
		}
	}
	return true
}

func (m *model) closeLogs() {
	m.stopFollow()
	m.logFollow = false
	m.logView = false
	m.logLines = nil
	m.logFiltering = false
	m.ensureVisible()
}

func (m *model) stopFollow() {
	if m.logCancel != nil {
		m.logCancel()
		m.logCancel = nil
	}
}

func (m model) loadLogs() tea.Cmd {
	row, ok := m.current()
	if !ok {
		return nil
	}
	which := m.logWhich
	svc := row.Service
	ctx := m.ctx
	return func() tea.Msg {
		lines, files, stream, err := lifecycle.CaptureLogs(ctx, svc, which, nil)
		return logLoadedMsg{which: which, lines: lines, files: files, stream: stream, err: err}
	}
}

func (m model) followCmd(ctx context.Context) tea.Cmd {
	var cmds []tea.Cmd
	if len(m.logFiles) > 0 {
		cmds = append(cmds, m.logTickCmd())
	}
	if m.logStream {
		cmds = append(cmds, startLogStream(ctx, m.currentService(), m.logWhich))
	}
	return tea.Batch(cmds...)
}

func (m model) currentService() catalog.Service {
	row, ok := m.current()
	if !ok {
		return catalog.Service{}
	}
	return row.Service
}

func (m model) logTickCmd() tea.Cmd {
	which := m.logWhich
	return tea.Tick(200*time.Millisecond, func(time.Time) tea.Msg {
		return logTickMsg{which: which}
	})
}

func startLogStream(ctx context.Context, s catalog.Service, which string) tea.Cmd {
	ch := make(chan string, 64)
	go func() {
		_ = lifecycle.FollowStreams(ctx, s, which, ch)
		close(ch)
	}()
	return readLogStream(which, ch)
}

func readLogStream(which string, ch <-chan string) tea.Cmd {
	return func() tea.Msg {
		line, ok := <-ch
		if !ok {
			return logStreamMsg{which: which, done: true}
		}
		return logStreamMsg{which: which, line: line, ch: ch}
	}
}

func (m model) cycleLog() (model, tea.Cmd) {
	row, ok := m.current()
	if !ok {
		return m, nil
	}
	names := logNames(row)
	if len(names) <= 1 {
		return m, nil
	}
	m.stopFollow()
	m.logFollow = false
	m.logWhich = nextLogName(m.logWhich, names)
	m.logLines = nil
	m.logOffset = 0
	m.logStick = true
	m.logFiles = nil
	return m, m.loadLogs()
}

func logNames(row Row) []string {
	var names []string
	for _, st := range row.Launches {
		if st.ID != "" {
			names = append(names, st.ID)
		}
	}
	return names
}

func nextLogName(cur string, names []string) string {
	if cur == "" {
		return names[0]
	}
	for i, name := range names {
		if name == cur {
			if i+1 < len(names) {
				return names[i+1]
			}
			return ""
		}
	}
	return names[0]
}

func (m *model) scrollLog(delta int) {
	lines := filterLogLines(m.logLines, m.logFilter)
	maxOff := len(lines) - m.logBody()
	if maxOff < 0 {
		maxOff = 0
	}
	m.logOffset += delta
	if m.logOffset < 0 {
		m.logOffset = 0
	}
	if m.logOffset > maxOff {
		m.logOffset = maxOff
	}
	if delta < 0 {
		m.logStick = false
	}
	if m.logOffset >= maxOff {
		m.logStick = true
	}
}

func (m *model) pinLogs() {
	lines := filterLogLines(m.logLines, m.logFilter)
	maxOff := len(lines) - m.logBody()
	if maxOff < 0 {
		maxOff = 0
	}
	if m.logStick {
		m.logOffset = maxOff
		return
	}
	if m.logOffset > maxOff {
		m.logOffset = maxOff
	}
	if m.logOffset < 0 {
		m.logOffset = 0
	}
}

func (m *model) appendLogs(lines []string) {
	if len(lines) > 0 {
		m.logLines = append(m.logLines, lines...)
		if extra := len(m.logLines) - 2000; extra > 0 {
			m.logLines = m.logLines[extra:]
			if !m.logStick {
				m.logOffset -= extra
				if m.logOffset < 0 {
					m.logOffset = 0
				}
			}
		}
	}
	m.pinLogs()
}

func (m model) logBody() int {
	h := m.height - 2
	if h < 1 {
		return 1
	}
	return h
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
	gen := m.probeGen
	loader := m.loader
	ctx := m.ctx
	return func() tea.Msg {
		prep, err := loader.Prepare(ctx)
		return preparedMsg{gen: gen, prep: prep, err: err}
	}
}

func (m model) probeCmd(gen int, prep probePrep, s catalog.Service) tea.Cmd {
	loader := m.loader
	ctx := m.ctx
	return func() tea.Msg {
		return probedMsg{gen: gen, row: loader.Probe(ctx, prep, s)}
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
