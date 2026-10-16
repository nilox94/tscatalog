package tui

import (
	"context"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"tscatalog/internal/catalog"
	"tscatalog/internal/lifecycle"
)

func TestKeyBarAndFilter(t *testing.T) {
	m := testModel([]Row{svc("web", true, true), svc("api", true, false)})
	if !strings.Contains(m.View().Content, keyBar) {
		t.Fatalf("missing key bar:\n%s", m.View().Content)
	}
	if !strings.Contains(m.View().Content, "1 needs restart") {
		t.Fatalf("summary:\n%s", m.View().Content)
	}
	webAt := strings.Index(m.View().Content, "web")
	apiAt := strings.Index(m.View().Content, "api")
	if apiAt < 0 || webAt < 0 || apiAt > webAt {
		t.Fatalf("failing api should sort above web:\n%s", m.View().Content)
	}

	m, _ = m.onKey("/")
	m, _ = m.onKey("w")
	if !m.filtering || m.filter != "w" {
		t.Fatalf("filter %+v", m)
	}
	if strings.Contains(m.View().Content, keyBar) {
		t.Fatal("key bar should yield to the filter prompt")
	}
	if !strings.Contains(m.View().Content, "w█") {
		t.Fatalf("prompt:\n%s", m.View().Content)
	}
	m, _ = m.onKey("enter")
	if m.filtering || m.filter != "w" {
		t.Fatalf("enter should keep the filter, got %q filtering=%v", m.filter, m.filtering)
	}
	if !strings.Contains(m.View().Content, keyBar) {
		t.Fatal("key bar should return after enter")
	}
	if strings.Contains(m.View().Content, "api") {
		t.Fatal("filter should hide api")
	}
	m, _ = m.onKey("esc")
	if m.filter != "" {
		t.Fatalf("esc should clear, got %q", m.filter)
	}
}

func TestRestartKeys(t *testing.T) {
	run := &fakeRun{}
	down := svc("web", true, false)
	foreign := svc("api", true, false)
	foreign.Owned = false
	foreign.Report = lifecycle.Report{Bind: ":8080", Local: "500", OK: false}
	foreign.Service.Check.Expect = []int{200}
	healthy := svc("ok", true, true)
	hidden := svc("extra", true, false)
	m := testModel([]Row{healthy, foreign, down, hidden})
	m.runner = run
	m.selected = "api"

	next, cmd := m.onKey("r")
	if cmd != nil {
		t.Fatal("foreign holder should not restart")
	}
	if !strings.Contains(next.status, "something else holds") {
		t.Fatalf("status %q", next.status)
	}
	if next.errText != "" {
		t.Fatalf("refusal should stay on the status line, panel %q", next.errText)
	}
	if len(run.restarts) != 0 {
		t.Fatalf("restarts %v", run.restarts)
	}

	m.selected = "web"
	next, cmd = m.onKey("r")
	if cmd == nil {
		t.Fatal("expected restart command")
	}
	if _, ok := cmd().(actionDone); !ok {
		t.Fatalf("cmd result %T", cmd())
	}
	if len(run.restarts) != 1 || run.restarts[0] != "web" {
		t.Fatalf("restarts %v", run.restarts)
	}

	m.filter = "ex"
	_, cmd = m.onKey("R")
	msg := cmd().(actionDone)
	if msg.err != nil {
		t.Fatal(msg.err)
	}
	if len(run.bulk) != 1 || run.bulk[0][0] != "extra" {
		t.Fatalf("bulk %v", run.bulk)
	}
}

func TestBulkRestartSkipsHealthyAndUnpublished(t *testing.T) {
	run := &fakeRun{}
	m := testModel([]Row{
		svc("web", true, false),
		svc("ok", true, true),
		svc("stack", false, false),
	})
	m.runner = run
	_, cmd := m.onKey("R")
	_ = cmd().(actionDone)
	if len(run.bulk) != 1 || strings.Join(run.bulk[0], ",") != "web" {
		t.Fatalf("bulk %v", run.bulk)
	}
}

func TestCommandErrorOpensPanel(t *testing.T) {
	run := &fakeRun{}
	m := testModel([]Row{svc("open-design", true, false)})
	m.runner = run
	m.height = 16
	m.width = 100

	_, cmd := m.onKey("r")
	updated, _ := m.Update(cmd().(actionDone))
	got := updated.(model)
	if strings.Contains(got.View().Content, "esc close") {
		t.Fatalf("success should not open the panel:\n%s", got.View().Content)
	}

	run.fail = errString("still listening on 127.0.0.1:7456")
	_, cmd = got.onKey("r")
	updated, _ = got.Update(cmd().(actionDone))
	got = updated.(model)
	body := got.View().Content
	if !strings.Contains(body, "still listening on 127.0.0.1:7456") || !strings.Contains(body, "esc close") {
		t.Fatalf("panel:\n%s", body)
	}
	if !strings.Contains(body, "open-design stop[0]") {
		t.Fatalf("panel should keep the command output:\n%s", body)
	}
	if !strings.Contains(cursorLine(body), "open-design") {
		t.Fatalf("list hidden by panel:\n%s", body)
	}
	closed, _ := got.onKey("esc")
	if closed.errText != "" || strings.Contains(closed.View().Content, "still listening") {
		t.Fatal("esc should close the panel")
	}

	_, cmd = got.onKey("x")
	updated, _ = got.Update(cmd().(actionDone))
	got = updated.(model)
	if !strings.Contains(got.View().Content, "still listening on 127.0.0.1:7456") {
		t.Fatalf("stop panel:\n%s", got.View().Content)
	}
}

type errString string

func (e errString) Error() string { return string(e) }

func TestFilterLogLines(t *testing.T) {
	lines := []string{"app | error", "app | ok"}
	got := filterLogLines(lines, "ERROR")
	if len(got) != 1 || got[0] != "app | error" {
		t.Fatalf("filtered %v", got)
	}
	if same := filterLogLines(lines, ""); len(same) != 2 || same[0] != lines[0] {
		t.Fatalf("empty filter %v", same)
	}
}

func TestDescribeAndQuit(t *testing.T) {
	m := testModel([]Row{svc("web", true, true)})
	m.rows[0].URL = "https://web.example.ts.net"
	m.rows[0].Serve = "tcp:443 → http://127.0.0.1:8080"
	m.rows[0].Service.Requires = []string{"db"}
	next, _ := m.onKey("enter")
	body := next.View().Content
	for _, want := range []string{"svc:web", "https://web.example.ts.net", "tcp:443 → http://127.0.0.1:8080", "db", detailBar} {
		if !strings.Contains(body, want) {
			t.Fatalf("detail missing %q\n%s", want, body)
		}
	}
	back, _ := next.onKey("esc")
	if back.detail {
		t.Fatal("esc should leave detail")
	}
	_, cmd := m.onKey("q")
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatalf("quit %T", cmd())
	}
}

func TestCursorFollowsServiceWhenRowJumps(t *testing.T) {
	// web is already down, so it stays above open-design after the kill.
	// A cursor stuck on the old line would land on api.
	rows := []Row{
		svc("web", true, false),
		svc("api", true, true),
		svc("open-design", true, true),
	}
	m := testModel(rows)
	m.selected = "open-design"
	m.height = 12
	m.offset = 0
	if !strings.Contains(cursorLine(m.View().Content), "open-design") {
		t.Fatalf("before:\n%s", m.View().Content)
	}

	killed := svc("open-design", true, false)
	updated, _ := m.Update(loadedMsg{rows: []Row{rows[0], rows[1], killed}})
	got := updated.(model)
	if got.selected != "open-design" {
		t.Fatalf("selected %q", got.selected)
	}
	body := got.View().Content
	line := cursorLine(body)
	if !strings.Contains(line, "open-design") {
		t.Fatalf("cursor left open-design\n%s", body)
	}
	webAt := strings.Index(body, "web")
	openAt := strings.Index(body, "open-design")
	apiAt := strings.Index(body, "api")
	if webAt < 0 || openAt < 0 || apiAt < 0 || !(webAt < openAt && openAt < apiAt) {
		t.Fatalf("order web, open-design, api:\n%s", body)
	}
}

func TestCursorStaysOnScreenWhenRowJumpsAboveViewport(t *testing.T) {
	rows := make([]Row, 0, 20)
	rows = append(rows, svc("web", true, false))
	for i := 0; i < 18; i++ {
		rows = append(rows, svc(string(rune('a'+i)), true, true))
	}
	rows = append(rows, svc("open-design", true, true))
	m := testModel(rows)
	m.height = 10 // body is 5 rows
	m.selected = "open-design"
	m.offset = 14
	m.ensureVisible()
	if !strings.Contains(cursorLine(m.View().Content), "open-design") {
		t.Fatalf("before:\n%s", m.View().Content)
	}

	killed := svc("open-design", true, false)
	next := append([]Row{}, rows...)
	next[len(next)-1] = killed
	updated, _ := m.Update(loadedMsg{rows: next})
	got := updated.(model)
	body := got.View().Content
	if !strings.Contains(cursorLine(body), "open-design") {
		t.Fatalf("cursor off screen after jump\noffset=%d\n%s", got.offset, body)
	}
}

func cursorLine(body string) string {
	for _, line := range strings.Split(body, "\n") {
		if strings.Contains(line, ">") {
			return line
		}
	}
	return ""
}

func TestRowUpdatesBeforeTheRestFinish(t *testing.T) {
	m := testModel([]Row{svc("web", true, true), svc("api", true, true)})
	m.loading = true
	m.placed = false
	m.pending = 2
	m.probeGen = 1
	m.rows[0].Report.Bind = "…"
	m.rows[1].Report.Bind = "…"

	done := svc("web", true, false)
	updated, _ := m.Update(probedMsg{gen: 1, row: done})
	got := updated.(model)
	if !got.loading {
		t.Fatal("load should stay open until the other row finishes")
	}
	body := got.View().Content
	if !strings.Contains(body, "down") || !strings.Contains(body, "…") {
		t.Fatalf("partial table:\n%s", body)
	}
	if got.selected != "web" {
		t.Fatalf("cursor moved before the pass finished: %s", got.selected)
	}

	stale := svc("api", true, false)
	updated, _ = got.Update(probedMsg{gen: 0, row: stale})
	got = updated.(model)
	if got.pending != 1 || got.rows[1].Report.Bind != "…" {
		t.Fatalf("stale probe applied: pending=%d bind=%s", got.pending, got.rows[1].Report.Bind)
	}

	other := svc("api", true, true)
	other.Report.Bind = ":9"
	updated, _ = got.Update(probedMsg{gen: 1, row: other})
	got = updated.(model)
	if got.loading || got.pending != 0 {
		t.Fatalf("loading=%v pending=%d", got.loading, got.pending)
	}
	if got.selected != "web" {
		t.Fatalf("selected %s", got.selected)
	}
	if !strings.Contains(got.View().Content, ":9") {
		t.Fatalf("second row:\n%s", got.View().Content)
	}
}

func TestFirstProbeLandsOnFailure(t *testing.T) {
	m := testModel([]Row{svc("web", true, true), svc("api", true, true)})
	m.placed = false
	m.selected = "web"
	failed := svc("api", true, false)
	ok := svc("web", true, true)
	updated, _ := m.Update(loadedMsg{rows: []Row{ok, failed}})
	got := updated.(model)
	if got.selected != "api" {
		t.Fatalf("selected %q", got.selected)
	}
}

type fakeRun struct {
	restarts []string
	stops    []string
	bulk     [][]string
	fail     error
}

func (f *fakeRun) Stop(_ context.Context, name string) (string, error) {
	f.stops = append(f.stops, name)
	if f.fail != nil {
		return "open-design stop[0] open-design stop", f.fail
	}
	return "stopped " + name, nil
}

func (f *fakeRun) Restart(_ context.Context, name string) (string, error) {
	f.restarts = append(f.restarts, name)
	if f.fail != nil {
		return "open-design stop[0] open-design stop", f.fail
	}
	return "restarted " + name, nil
}

func (f *fakeRun) RestartAll(_ context.Context, names []string) (string, error) {
	f.bulk = append(f.bulk, append([]string(nil), names...))
	return "restarted", nil
}

func testModel(rows []Row) model {
	cat := &catalog.File{Endpoint: catalog.DefaultEndpoint}
	for _, row := range rows {
		cat.Services = append(cat.Services, row.Service)
	}
	m := newModel(context.Background(), cat, nil, &fakeRun{})
	m.rows = rows
	m.loading = false
	m.placed = true
	if len(rows) > 0 {
		m.selected = rows[0].Service.Name
	}
	return m
}
