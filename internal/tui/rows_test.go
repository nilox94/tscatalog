package tui

import (
	"testing"

	"tscatalog/internal/catalog"
	"tscatalog/internal/lifecycle"
)

func TestOrderPutsPublishedFailuresFirst(t *testing.T) {
	rows := []Row{
		svc("api", true, true),
		svc("stack", false, false),
		svc("web", true, false),
	}
	rows[2].Report.Bind = "down"
	got := order(rows, "")
	if names(got) != "web,api,stack" {
		t.Fatalf("order = %s", names(got))
	}
	if !needsRestart(got[0]) || needsRestart(got[1]) || needsRestart(got[2]) {
		t.Fatalf("restart set = %v %v %v", needsRestart(got[0]), needsRestart(got[1]), needsRestart(got[2]))
	}
}

func TestOrderFiltersByNameAndVIP(t *testing.T) {
	web := svc("web", true, true)
	api := svc("api.internal", true, true)
	api.Service.VIP = "api"
	got := order([]Row{web, api}, "api")
	if names(got) != "api.internal" {
		t.Fatalf("got %s", names(got))
	}
}

func svc(name string, published, ok bool) Row {
	s := catalog.Service{
		Name:        name,
		Local:       "http://127.0.0.1:8080",
		Unpublished: !published,
		Launch:      catalog.Launch{{Start: []string{"run"}}},
	}
	rep := lifecycle.Report{Name: name, Bind: ":8080", Local: "tcp", Host: "approved / ready", OK: ok}
	if !ok {
		rep.Bind = "down"
		rep.Local = "down"
	}
	return Row{Service: s, Report: rep, Fresh: true, Owned: true}
}

func names(rows []Row) string {
	out := make([]string, len(rows))
	for i, row := range rows {
		out[i] = row.Service.Name
	}
	return join(out)
}

func join(parts []string) string {
	if len(parts) == 0 {
		return ""
	}
	s := parts[0]
	for _, p := range parts[1:] {
		s += "," + p
	}
	return s
}
