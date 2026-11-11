package policy

import (
	"testing"

	"tailscale.com/client/tailscale/v2"

	"github.com/nilox94/tscatalog/internal/catalog"
)

func sampleCatalog() *catalog.File {
	return &catalog.File{
		Endpoint: "tcp:443",
		Access: catalog.Access{
			Src:             []string{"autogroup:member"},
			AutoApproveHost: "tag:server",
		},
		Services: []catalog.Service{
			{Name: "web", Local: "http://127.0.0.1:8080"},
			{Name: "api", Local: "http://127.0.0.1:8081"},
		},
	}
}

func TestFromCatalogDefaultsTagOwners(t *testing.T) {
	d := FromCatalog(sampleCatalog())
	if d.HostTag != "tag:server" {
		t.Fatalf("host %q", d.HostTag)
	}
	if len(d.TagOwners) != 1 || d.TagOwners[0] != defaultTagOwner {
		t.Fatalf("owners %v", d.TagOwners)
	}
	if len(d.GrantDst) != 2 || d.GrantDst[0] != "svc:web" {
		t.Fatalf("dst %v", d.GrantDst)
	}
}

func TestPlanEmptyACL(t *testing.T) {
	want := FromCatalog(sampleCatalog())
	p := ComputePlan(want, &tailscale.ACL{})
	if !p.NeedsApply() {
		t.Fatal("expected apply")
	}
	got := map[string]string{}
	for _, c := range p.Changes {
		got[c.Name] = c.Action
	}
	if got["tagOwners.tag:server"] != "create" {
		t.Fatalf("%+v", p.Changes)
	}
	if got["autoApprovers.services"] != "create" {
		t.Fatalf("%+v", p.Changes)
	}
	if got["grants"] != "create" {
		t.Fatalf("%+v", p.Changes)
	}
}

func TestPlanFillsEmptyTagOwners(t *testing.T) {
	want := FromCatalog(sampleCatalog())
	have := &tailscale.ACL{
		TagOwners: map[string][]string{"tag:server": {}},
		AutoApprovers: &tailscale.ACLAutoApprovers{
			Services: map[string][]string{"tag:server": {"tag:server"}},
		},
		Grants: []tailscale.Grant{{
			Source:      []string{"*"},
			Destination: []string{"*"},
			IP:          []string{"*"},
		}},
	}
	p := ComputePlan(want, have)
	by := map[string]Change{}
	for _, c := range p.Changes {
		by[c.Name] = c
	}
	if by["tagOwners.tag:server"].Action != "update" {
		t.Fatalf("tagOwners %+v", by["tagOwners.tag:server"])
	}
	if by["autoApprovers.services"].Action != "unchanged" {
		t.Fatalf("autoApprovers %+v", by["autoApprovers.services"])
	}
	if by["grants"].Action != "unchanged" {
		t.Fatalf("grants %+v", by["grants"])
	}
}

func TestPlanCatchAllSkipsSvcGrants(t *testing.T) {
	want := FromCatalog(sampleCatalog())
	have := &tailscale.ACL{
		TagOwners: map[string][]string{"tag:server": {"autogroup:admin"}},
		AutoApprovers: &tailscale.ACLAutoApprovers{
			Services: map[string][]string{"tag:server": {"tag:server"}},
		},
		Grants: []tailscale.Grant{{
			Source:      []string{"*"},
			Destination: []string{"*"},
			IP:          []string{"*"},
		}},
	}
	p := ComputePlan(want, have)
	if p.NeedsApply() {
		t.Fatalf("expected no changes: %+v", p.Changes)
	}
}

func TestMergePreservesSSHAndCatchAll(t *testing.T) {
	want := FromCatalog(sampleCatalog())
	have := &tailscale.ACL{
		TagOwners: map[string][]string{"tag:other": {"autogroup:admin"}, "tag:server": {}},
		AutoApprovers: &tailscale.ACLAutoApprovers{
			ExitNode: []string{"tag:exit"},
			Routes:   map[string][]string{"10.0.0.0/8": {"tag:router"}},
		},
		Grants: []tailscale.Grant{{
			Source:      []string{"*"},
			Destination: []string{"*"},
			IP:          []string{"*"},
		}},
		SSH: []tailscale.ACLSSH{{
			Action:      "check",
			Source:      []string{"autogroup:member"},
			Destination: []string{"autogroup:self"},
			Users:       []string{"autogroup:nonroot", "root"},
		}},
		NodeAttrs: []tailscale.NodeAttrGrant{{
			Target: []string{"autogroup:member"},
			Attr:   []string{"funnel"},
		}},
	}
	out := Merge(have, want)
	if got := out.TagOwners["tag:server"]; len(got) != 1 || got[0] != defaultTagOwner {
		t.Fatalf("tagOwners %v", out.TagOwners)
	}
	if out.TagOwners["tag:other"][0] != "autogroup:admin" {
		t.Fatalf("lost other tag: %v", out.TagOwners)
	}
	if out.AutoApprovers.ExitNode[0] != "tag:exit" {
		t.Fatal("lost exitNode")
	}
	if out.AutoApprovers.Routes["10.0.0.0/8"][0] != "tag:router" {
		t.Fatal("lost routes")
	}
	if out.AutoApprovers.Services["tag:server"][0] != "tag:server" {
		t.Fatalf("services %v", out.AutoApprovers.Services)
	}
	if len(out.Grants) != 1 {
		t.Fatalf("should not add grant on catch-all: %d", len(out.Grants))
	}
	if len(out.SSH) != 1 || out.NodeAttrs[0].Attr[0] != "funnel" {
		t.Fatal("lost ssh/funnel")
	}
}

func TestMergeAddsGrantWithoutCatchAll(t *testing.T) {
	want := FromCatalog(sampleCatalog())
	have := &tailscale.ACL{}
	out := Merge(have, want)
	if len(out.Grants) != 1 {
		t.Fatalf("grants %d", len(out.Grants))
	}
	g := out.Grants[0]
	if g.Destination[0] != "svc:web" || g.IP[0] != "443" {
		t.Fatalf("%+v", g)
	}
}

func TestMergeDoesNotMutateInput(t *testing.T) {
	have := &tailscale.ACL{
		TagOwners: map[string][]string{"tag:server": {}},
	}
	_ = Merge(have, FromCatalog(sampleCatalog()))
	if len(have.TagOwners["tag:server"]) != 0 {
		t.Fatalf("mutated input: %v", have.TagOwners)
	}
}
