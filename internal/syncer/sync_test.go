package syncer

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"tailscale.com/client/tailscale/v2"

	"tscatalog/internal/catalog"
)

func sampleCatalog() *catalog.File {
	return &catalog.File{
		Endpoint: "tcp:443",
		Services: []catalog.Service{
			{Name: "web", DisplayName: "Example web UI", Local: "http://127.0.0.1:8080"},
			{Name: "api", Local: "http://127.0.0.1:8081"},
		},
	}
}

func TestComputePlanCreate(t *testing.T) {
	want := sampleCatalog()
	p := ComputePlan(want, nil, nil)
	if !p.NeedsApply() {
		t.Fatal("expected apply")
	}
	if p.Tailnet[0].Action != "create" || p.Tailnet[1].Action != "create" {
		t.Fatalf("%+v", p.Tailnet)
	}
	if p.Host[0].Action != "create" {
		t.Fatalf("%+v", p.Host)
	}
}

func TestComputePlanDetectsPortMismatch(t *testing.T) {
	want := sampleCatalog()
	have := []tailscale.Service{{
		Name:  "svc:web",
		Ports: []string{"tcp:8080"},
	}}
	p := ComputePlan(want, have, nil)
	if p.Tailnet[0].Action != "update" {
		t.Fatalf("want update, got %+v", p.Tailnet[0])
	}
}

func TestComputePlanMissingHostTagIsUpdate(t *testing.T) {
	want := sampleCatalog()
	want.Access.AutoApproveHost = "tag:server"
	have := []tailscale.Service{
		{Name: "svc:web", Ports: []string{"tcp:443"}},
		{Name: "svc:api", Ports: []string{"tcp:443"}, Tags: []string{"tag:server"}},
	}
	p := ComputePlan(want, have, want.ServeConfig())
	if p.Tailnet[0].Action != "update" {
		t.Fatalf("missing service tag should PUT: %+v", p.Tailnet[0])
	}
	if p.Tailnet[1].Action != "unchanged" {
		t.Fatalf("tagged service should stay: %+v", p.Tailnet[1])
	}
}

func TestComputePlanCommentOnlyIsUnchanged(t *testing.T) {
	want := sampleCatalog()
	want.Services[0].Comment = "example UI"
	have := []tailscale.Service{
		{Name: "svc:web", Comment: "", Ports: []string{"tcp:443"}, Tags: []string{"tag:server"}},
		{Name: "svc:api", Ports: []string{"tcp:443"}},
	}
	p := ComputePlan(want, have, want.ServeConfig())
	if p.Tailnet[0].Action != "unchanged" {
		t.Fatalf("comment-only should not PUT: %+v", p.Tailnet[0])
	}
}

func TestComputePlanUnchangedNormalizesPort(t *testing.T) {
	want := sampleCatalog()
	have := []tailscale.Service{
		{Name: "svc:web", Comment: "", Ports: []string{"443"}},
		{Name: "svc:api", Ports: []string{"tcp:443"}},
	}
	serve := want.ServeConfig()
	p := ComputePlan(want, have, serve)
	if p.NeedsApply() {
		t.Fatalf("expected no changes: %+v %+v", p.Tailnet, p.Host)
	}
}

type fakeAPI struct {
	have []tailscale.Service
	puts []tailscale.Service
}

func (f *fakeAPI) List(context.Context) ([]tailscale.Service, error) { return f.have, nil }
func (f *fakeAPI) CreateOrUpdate(_ context.Context, svc tailscale.Service) error {
	f.puts = append(f.puts, svc)
	return nil
}

type fakeHost struct {
	cfg        *catalog.ServeFile
	served     []string
	advertised []string
}

func (f *fakeHost) GetConfig(context.Context) (*catalog.ServeFile, error) { return f.cfg, nil }
func (f *fakeHost) Serve(_ context.Context, name, endpoint, local string) error {
	f.served = append(f.served, name+" "+endpoint+" "+local)
	return nil
}
func (f *fakeHost) Advertise(_ context.Context, svc string) error {
	f.advertised = append(f.advertised, svc)
	return nil
}
func (f *fakeHost) Drain(_ context.Context, svc string) error { return nil }

func TestApplyWritesVIPAndServe(t *testing.T) {
	want := sampleCatalog()
	api := &fakeAPI{}
	h := &fakeHost{cfg: &catalog.ServeFile{Services: map[string]catalog.ServeService{}}}
	var buf bytes.Buffer
	err := Apply(context.Background(), want, api, h, nil, Options{Tailnet: true, Host: true, Advertise: true}, &buf)
	if err != nil {
		t.Fatal(err)
	}
	if len(api.puts) != 2 {
		t.Fatalf("puts %d", len(api.puts))
	}
	if strings.Join(h.served, "|") != "svc:web tcp:443 http://127.0.0.1:8080|svc:api tcp:443 http://127.0.0.1:8081" {
		t.Fatalf("served %v", h.served)
	}
	if strings.Join(h.advertised, ",") != "svc:web,svc:api" {
		t.Fatalf("advertised %v", h.advertised)
	}
}

func TestApplyDryRunDoesNotMutate(t *testing.T) {
	want := sampleCatalog()
	api := &fakeAPI{}
	h := &fakeHost{}
	if err := Apply(context.Background(), want, api, h, nil, Options{DryRun: true, Tailnet: true, Host: true}, ioDiscard()); err != nil {
		t.Fatal(err)
	}
	if len(api.puts) != 0 || len(h.served) != 0 {
		t.Fatal("dry-run mutated")
	}
}

func ioDiscard() *bytes.Buffer { return &bytes.Buffer{} }

func TestApplySkipsUnchangedAndMergesTags(t *testing.T) {
	want := sampleCatalog()
	want.Services[0].Comment = "catalog comment"
	api := &fakeAPI{have: []tailscale.Service{
		{Name: "svc:web", Ports: []string{"tcp:443"}, Tags: []string{"tag:server"}, Comment: "console"},
	}}
	h := &fakeHost{cfg: &catalog.ServeFile{Services: map[string]catalog.ServeService{
		"svc:web": {Endpoints: map[string]string{"tcp:443": "http://127.0.0.1:8080"}},
	}}}
	if err := Apply(context.Background(), want, api, h, nil, Options{Tailnet: true, Host: true, Advertise: true}, ioDiscard()); err != nil {
		t.Fatal(err)
	}
	if len(api.puts) != 1 || api.puts[0].Name != "svc:api" {
		t.Fatalf("expected only api put, got %+v", api.puts)
	}
	if strings.Join(h.served, ",") != "svc:api tcp:443 http://127.0.0.1:8081" {
		t.Fatalf("should not re-serve web: %v", h.served)
	}
	if strings.Join(h.advertised, ",") != "svc:web,svc:api" {
		t.Fatalf("advertise all catalog services: %v", h.advertised)
	}
}

func TestMergeForPutKeepsTags(t *testing.T) {
	got := mergeForPut(
		tailscale.Service{Name: "svc:web", Ports: []string{"tcp:443"}, Comment: "new", Tags: []string{"tag:server"}},
		tailscale.Service{Name: "svc:web", Ports: []string{"tcp:8080"}, Tags: []string{"tag:server"}, Addrs: []string{"100.1.2.3", "fd7a::1"}},
	)
	if got.Tags[0] != "tag:server" {
		t.Fatalf("tags %v", got.Tags)
	}
	if len(got.Addrs) != 2 || got.Addrs[0] != "100.1.2.3" {
		t.Fatalf("addrs should be kept on update: %v", got.Addrs)
	}
	if got.Comment != "new" || !catalog.PortsEqual(got.Ports, []string{"tcp:443"}) {
		t.Fatalf("%+v", got)
	}
}
