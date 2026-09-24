package policy

import (
	"fmt"
	"slices"
	"strings"

	"tailscale.com/client/tailscale/v2"

	"tscatalog/internal/catalog"
)

const defaultTagOwner = "autogroup:admin"

// Desired is the catalog-driven slice of the tailnet policy.
type Desired struct {
	HostTag   string   // e.g. tag:server; empty skips host/tag mutations
	TagOwners []string // owners of HostTag; default autogroup:admin
	GrantSrc  []string
	GrantDst  []string // svc:name entries
	GrantIP   []string // always 443
}

func FromCatalog(f *catalog.File) Desired {
	d := Desired{
		HostTag:   strings.TrimSpace(f.Access.AutoApproveHost),
		TagOwners: append([]string(nil), f.Access.TagOwners...),
		GrantSrc:  append([]string(nil), f.Access.Src...),
		GrantIP:   []string{"443"},
	}
	if len(d.GrantSrc) == 0 {
		d.GrantSrc = []string{"autogroup:member"}
	}
	if d.HostTag != "" && len(d.TagOwners) == 0 {
		d.TagOwners = []string{defaultTagOwner}
	}
	for _, s := range f.Services {
		d.GrantDst = append(d.GrantDst, s.SVCName())
	}
	return d
}

type Change struct {
	Name   string
	Action string // create, update, unchanged
	Detail string
}

type Plan struct {
	Changes []Change
}

func (p Plan) NeedsApply() bool {
	for _, c := range p.Changes {
		if c.Action != "unchanged" {
			return true
		}
	}
	return false
}

func ComputePlan(want Desired, have *tailscale.ACL) Plan {
	var p Plan
	if have == nil {
		have = &tailscale.ACL{}
	}

	if want.HostTag != "" {
		owners := have.TagOwners[want.HostTag]
		switch {
		case have.TagOwners == nil || !mapHas(have.TagOwners, want.HostTag):
			p.Changes = append(p.Changes, Change{
				Name:   "tagOwners." + want.HostTag,
				Action: "create",
				Detail: fmt.Sprintf("owners=%s", strings.Join(want.TagOwners, ",")),
			})
		case len(owners) == 0:
			p.Changes = append(p.Changes, Change{
				Name:   "tagOwners." + want.HostTag,
				Action: "update",
				Detail: fmt.Sprintf("[] → [%s]", strings.Join(want.TagOwners, ", ")),
			})
		case !containsAll(owners, want.TagOwners):
			p.Changes = append(p.Changes, Change{
				Name:   "tagOwners." + want.HostTag,
				Action: "update",
				Detail: fmt.Sprintf("%v + %v", owners, missing(owners, want.TagOwners)),
			})
		default:
			p.Changes = append(p.Changes, Change{
				Name:   "tagOwners." + want.HostTag,
				Action: "unchanged",
				Detail: fmt.Sprintf("owners=%s", strings.Join(owners, ",")),
			})
		}

		var svcApprovers []string
		if have.AutoApprovers != nil {
			svcApprovers = have.AutoApprovers.Services[want.HostTag]
		}
		switch {
		case have.AutoApprovers == nil || have.AutoApprovers.Services == nil || !contains(svcApprovers, want.HostTag):
			action := "create"
			if have.AutoApprovers != nil && have.AutoApprovers.Services != nil {
				if _, ok := have.AutoApprovers.Services[want.HostTag]; ok {
					action = "update"
				}
			}
			p.Changes = append(p.Changes, Change{
				Name:   "autoApprovers.services",
				Action: action,
				Detail: want.HostTag + " ← " + want.HostTag + " (service tag, not svc:*)",
			})
		default:
			p.Changes = append(p.Changes, Change{
				Name:   "autoApprovers.services",
				Action: "unchanged",
				Detail: want.HostTag + " includes " + want.HostTag,
			})
		}
	}

	switch {
	case hasCatchAll(have.Grants):
		p.Changes = append(p.Changes, Change{
			Name:   "grants",
			Action: "unchanged",
			Detail: "catch-all *:* already permits 443",
		})
	case grantCovers(have.Grants, want.GrantSrc, want.GrantDst, want.GrantIP):
		p.Changes = append(p.Changes, Change{
			Name:   "grants",
			Action: "unchanged",
			Detail: "existing grant covers catalog services on 443",
		})
	default:
		p.Changes = append(p.Changes, Change{
			Name:   "grants",
			Action: "create",
			Detail: fmt.Sprintf("%s → %s ip=%s", strings.Join(want.GrantSrc, ","), strings.Join(want.GrantDst, ","), strings.Join(want.GrantIP, ",")),
		})
	}
	return p
}

// Merge copies have and applies catalog policy without removing existing rules.
func Merge(have *tailscale.ACL, want Desired) *tailscale.ACL {
	out := cloneACL(have)
	if want.HostTag != "" {
		if out.TagOwners == nil {
			out.TagOwners = map[string][]string{}
		}
		out.TagOwners[want.HostTag] = union(out.TagOwners[want.HostTag], want.TagOwners)
		if out.AutoApprovers == nil {
			out.AutoApprovers = &tailscale.ACLAutoApprovers{}
		}
		if out.AutoApprovers.Services == nil {
			out.AutoApprovers.Services = map[string][]string{}
		}
		out.AutoApprovers.Services[want.HostTag] = union(out.AutoApprovers.Services[want.HostTag], []string{want.HostTag})
	}
	if !hasCatchAll(out.Grants) && !grantCovers(out.Grants, want.GrantSrc, want.GrantDst, want.GrantIP) {
		out.Grants = append(out.Grants, tailscale.Grant{
			Source:      append([]string(nil), want.GrantSrc...),
			Destination: append([]string(nil), want.GrantDst...),
			IP:          append([]string(nil), want.GrantIP...),
		})
	}
	return out
}

func cloneACL(in *tailscale.ACL) *tailscale.ACL {
	if in == nil {
		return &tailscale.ACL{}
	}
	out := *in
	if in.TagOwners != nil {
		out.TagOwners = copyMapSlice(in.TagOwners)
	}
	if in.AutoApprovers != nil {
		aa := *in.AutoApprovers
		aa.Routes = copyMapSlice(in.AutoApprovers.Routes)
		aa.ExitNode = slices.Clone(in.AutoApprovers.ExitNode)
		aa.Services = copyMapSlice(in.AutoApprovers.Services)
		out.AutoApprovers = &aa
	}
	out.Grants = slices.Clone(in.Grants)
	for i, g := range out.Grants {
		out.Grants[i].Source = slices.Clone(g.Source)
		out.Grants[i].Destination = slices.Clone(g.Destination)
		out.Grants[i].IP = slices.Clone(g.IP)
		out.Grants[i].SrcPosture = slices.Clone(g.SrcPosture)
		out.Grants[i].Via = slices.Clone(g.Via)
	}
	out.SSH = slices.Clone(in.SSH)
	out.NodeAttrs = slices.Clone(in.NodeAttrs)
	out.Tests = slices.Clone(in.Tests)
	return &out
}

func copyMapSlice(in map[string][]string) map[string][]string {
	if in == nil {
		return nil
	}
	out := make(map[string][]string, len(in))
	for k, v := range in {
		out[k] = slices.Clone(v)
	}
	return out
}

func hasCatchAll(grants []tailscale.Grant) bool {
	for _, g := range grants {
		if contains(g.Source, "*") && contains(g.Destination, "*") && ipAllows(g.IP, "443") {
			return true
		}
	}
	return false
}

func grantCovers(grants []tailscale.Grant, src, dst, ip []string) bool {
	for _, g := range grants {
		if !containsAll(g.Source, src) {
			continue
		}
		if !containsAll(g.Destination, dst) {
			continue
		}
		ok := true
		for _, p := range ip {
			if !ipAllows(g.IP, p) {
				ok = false
				break
			}
		}
		if ok {
			return true
		}
	}
	return false
}

func ipAllows(got []string, port string) bool {
	if len(got) == 0 || contains(got, "*") {
		return true
	}
	return contains(got, port)
}

func contains(ss []string, want string) bool {
	return slices.Contains(ss, want)
}

func containsAll(have, want []string) bool {
	for _, w := range want {
		if !contains(have, w) {
			return false
		}
	}
	return true
}

func missing(have, want []string) []string {
	var out []string
	for _, w := range want {
		if !contains(have, w) {
			out = append(out, w)
		}
	}
	return out
}

func union(have, add []string) []string {
	out := slices.Clone(have)
	for _, a := range add {
		if a != "" && !contains(out, a) {
			out = append(out, a)
		}
	}
	return out
}

func mapHas(m map[string][]string, key string) bool {
	if m == nil {
		return false
	}
	_, ok := m[key]
	return ok
}
