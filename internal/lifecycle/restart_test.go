package lifecycle

import (
	"testing"

	"tscatalog/internal/catalog"
)

func TestDecide(t *testing.T) {
	down := Report{Bind: "down", Local: "down", OK: false}
	wedged := Report{Bind: ":8080", Local: "500", OK: false}
	healthy := Report{Bind: ":8080", Local: "tcp", Host: "approved / ready", OK: true}
	unapproved := Report{Bind: ":8080", Local: "tcp", Host: "not-approved / ready", OK: false}
	draining := Report{Bind: ":8080", Local: "200", Host: "approved / draining", OK: false}

	withLaunch := catalog.Service{
		Name:   "web",
		Local:  "http://127.0.0.1:8080",
		Launch: catalog.Launch{{Start: []string{"run"}}},
	}
	withCheck := withLaunch
	withCheck.Check.Expect = []int{200}
	noLaunch := catalog.Service{Name: "web", Local: "http://127.0.0.1:8080"}

	cases := []struct {
		name   string
		svc    catalog.Service
		rep    Report
		owned  bool
		action Action
		note   string
	}{
		{name: "down starts", svc: withLaunch, rep: down, action: ActionStart},
		{name: "down without launch", svc: noLaunch, rep: down, note: "no launch in catalog"},
		{name: "owned wedge restarts", svc: withCheck, rep: wedged, owned: true, action: ActionRestart},
		{name: "foreign wedge", svc: withCheck, rep: wedged, note: "something else holds http://127.0.0.1:8080"},
		{name: "healthy", svc: withLaunch, rep: healthy, note: "local process is healthy"},
		{name: "not approved", svc: withLaunch, rep: unapproved, note: "host is not-approved; restart will not fix it"},
		{name: "draining", svc: withCheck, rep: draining, note: "host is draining; restart will not fix it"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Decide(tc.svc, tc.rep, tc.owned)
			if got.Action != tc.action || got.Note != tc.note {
				t.Fatalf("got action=%d note=%q", got.Action, got.Note)
			}
		})
	}
}

func TestCatalogOwnsStopArgv(t *testing.T) {
	s := catalog.Service{Launch: catalog.Launch{{Start: []string{"start"}, Stop: []string{"stop"}}}}
	if !CatalogOwns(s) {
		t.Fatal("stop argv should count as owned")
	}
}

func TestCatalogOwnsDetachWithoutPID(t *testing.T) {
	s := catalog.Service{
		Name:   "missing-pid",
		Launch: catalog.Launch{{Start: []string{"sleep"}, Detach: true}},
	}
	if CatalogOwns(s) {
		t.Fatal("detach without a live pid is not owned")
	}
}
