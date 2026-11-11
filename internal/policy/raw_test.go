package policy

import (
	"strings"
	"testing"
)

func TestEditPreservesCommentsAndUnknownKeys(t *testing.T) {
	raw := `{
  // keep me
  "ssh": [{"action": "check", "src": ["autogroup:member"], "dst": ["autogroup:self"], "users": ["root"]}],
  "tagOwners": {"tag:other": ["autogroup:admin"]},
  "extra": {"keep": true}
}
`
	want := FromCatalog(sampleCatalog())
	out, err := Edit(raw, want, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, keep := range []string{"// keep me", "tag:other", `"extra"`, "svc:web"} {
		if !strings.Contains(out, keep) {
			t.Fatalf("missing %s in %s", keep, out)
		}
	}
}

func TestEditPruneRewritesCatalogGrant(t *testing.T) {
	raw := `{
  "grants": [{"src": ["autogroup:member"], "dst": ["svc:web", "svc:gone"], "ip": ["443"]}],
  "ssh": [{"action": "accept"}]
}
`
	want := FromCatalog(sampleCatalog())
	out, err := Edit(raw, want, true)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "svc:gone") {
		t.Fatalf("prune kept removed dst: %s", out)
	}
	if !strings.Contains(out, "svc:web") || !strings.Contains(out, "svc:api") {
		t.Fatalf("missing catalog dst: %s", out)
	}
	if !strings.Contains(out, `"ssh"`) {
		t.Fatalf("lost ssh: %s", out)
	}
}

func TestPlanShowsDefaultSrc(t *testing.T) {
	cat := sampleCatalog()
	cat.Access.Src = nil
	p := ComputePlan(FromCatalog(cat), nil)
	var detail string
	for _, c := range p.Changes {
		if c.Name == "grants" && c.Action == "create" {
			detail = c.Detail
		}
	}
	if !strings.Contains(detail, "access.src default autogroup:member") {
		t.Fatalf("detail %q", detail)
	}
}
