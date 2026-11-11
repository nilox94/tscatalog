package policy

import (
	"fmt"
	"slices"
	"strings"

	"github.com/tailscale/hujson"
	"tailscale.com/client/tailscale/v2"
)

// Edit applies the catalog policy to a HuJSON policy file.
// Comments and keys other than tagOwners, autoApprovers, and grants stay.
// Replacing a grant's dst drops comments on that value.
// prune rewrites the first grant whose destinations are all svc: names and
// whose ports are only 443, setting dst to the catalog's published services.
// tagOwners stay a union: owners already on the tag are not removed.
// The input string is returned unchanged when nothing was edited.
func Edit(raw string, want Desired, prune bool) (string, error) {
	body := raw
	if strings.TrimSpace(body) == "" {
		body = "{}"
	}
	v, err := hujson.Parse([]byte(body))
	if err != nil {
		return "", err
	}
	root, ok := v.Value.(*hujson.Object)
	if !ok {
		return "", fmt.Errorf("policy file: want a JSON object")
	}
	changed, err := editObject(root, want, prune)
	if err != nil {
		return "", err
	}
	if !changed {
		return raw, nil
	}
	return string(v.Pack()), nil
}

func editObject(root *hujson.Object, want Desired, prune bool) (bool, error) {
	changed := false
	if want.HostTag != "" {
		owners, created, err := ensureObject(root, "tagOwners")
		if err != nil {
			return false, err
		}
		changed = created || changed
		if unionKey(owners, want.HostTag, want.TagOwners) {
			changed = true
		}
		approvers, created, err := ensureObject(root, "autoApprovers")
		if err != nil {
			return false, err
		}
		changed = created || changed
		services, created, err := ensureObject(approvers, "services")
		if err != nil {
			return false, err
		}
		changed = created || changed
		if unionKey(services, want.HostTag, []string{want.HostTag}) {
			changed = true
		}
	}

	grants, have, err := existingGrants(root)
	if err != nil {
		return false, err
	}
	if grants != nil && prune && rewriteCatalogGrant(grants, want.GrantDst) {
		changed = true
		have = grantsOf(grants)
	}
	if !hasCatchAll(have) && !grantCovers(have, want.GrantSrc, want.GrantDst, want.GrantIP) && len(want.GrantDst) > 0 {
		if grants == nil {
			grants, _, err = ensureArray(root, "grants")
			if err != nil {
				return false, err
			}
		}
		grants.Elements = append(grants.Elements, grantValue(want.GrantSrc, want.GrantDst, want.GrantIP))
		changed = true
	}
	return changed, nil
}

func existingGrants(root *hujson.Object) (*hujson.Array, []tailscale.Grant, error) {
	m := findMember(root, "grants")
	if m == nil {
		return nil, nil, nil
	}
	arr, ok := m.Value.Value.(*hujson.Array)
	if !ok {
		return nil, nil, fmt.Errorf("policy grants: want an array")
	}
	return arr, grantsOf(arr), nil
}

// rewriteCatalogGrant sets dst on the first grant that is only svc: names on port 443.
func rewriteCatalogGrant(grants *hujson.Array, dst []string) bool {
	for i := range grants.Elements {
		obj, ok := grants.Elements[i].Value.(*hujson.Object)
		if !ok || !catalogGrant(obj) {
			continue
		}
		member := findMember(obj, "dst")
		if member == nil {
			continue
		}
		have, ok := stringsOf(member.Value)
		if ok && slices.Equal(have, dst) {
			return false
		}
		if len(dst) == 0 {
			grants.Elements = append(grants.Elements[:i], grants.Elements[i+1:]...)
			return true
		}
		member.Value = stringList(dst)
		return true
	}
	return false
}

func catalogGrant(obj *hujson.Object) bool {
	dstM := findMember(obj, "dst")
	ipM := findMember(obj, "ip")
	if dstM == nil || ipM == nil {
		return false
	}
	dst, ok := stringsOf(dstM.Value)
	if !ok || len(dst) == 0 {
		return false
	}
	for _, d := range dst {
		if !strings.HasPrefix(d, "svc:") {
			return false
		}
	}
	ips, ok := stringsOf(ipM.Value)
	if !ok || len(ips) == 0 {
		return false
	}
	for _, p := range ips {
		if p != "443" && p != "tcp:443" {
			return false
		}
	}
	return true
}

func grantsOf(arr *hujson.Array) []tailscale.Grant {
	if arr == nil {
		return nil
	}
	out := make([]tailscale.Grant, 0, len(arr.Elements))
	for _, el := range arr.Elements {
		obj, ok := el.Value.(*hujson.Object)
		if !ok {
			continue
		}
		g := tailscale.Grant{}
		if m := findMember(obj, "src"); m != nil {
			g.Source, _ = stringsOf(m.Value)
		}
		if m := findMember(obj, "dst"); m != nil {
			g.Destination, _ = stringsOf(m.Value)
		}
		if m := findMember(obj, "ip"); m != nil {
			g.IP, _ = stringsOf(m.Value)
		}
		out = append(out, g)
	}
	return out
}

func grantValue(src, dst, ip []string) hujson.Value {
	return hujson.Value{Value: &hujson.Object{Members: []hujson.ObjectMember{
		{Name: nameValue("src"), Value: stringList(src)},
		{Name: nameValue("dst"), Value: stringList(dst)},
		{Name: nameValue("ip"), Value: stringList(ip)},
	}}}
}

func ensureObject(parent *hujson.Object, key string) (*hujson.Object, bool, error) {
	if m := findMember(parent, key); m != nil {
		obj, ok := m.Value.Value.(*hujson.Object)
		if !ok {
			return nil, false, fmt.Errorf("policy %s: want an object", key)
		}
		return obj, false, nil
	}
	obj := &hujson.Object{}
	parent.Members = append(parent.Members, hujson.ObjectMember{
		Name:  nameValue(key),
		Value: hujson.Value{Value: obj},
	})
	return obj, true, nil
}

func ensureArray(parent *hujson.Object, key string) (*hujson.Array, bool, error) {
	if m := findMember(parent, key); m != nil {
		arr, ok := m.Value.Value.(*hujson.Array)
		if !ok {
			return nil, false, fmt.Errorf("policy %s: want an array", key)
		}
		return arr, false, nil
	}
	arr := &hujson.Array{}
	parent.Members = append(parent.Members, hujson.ObjectMember{
		Name:  nameValue(key),
		Value: hujson.Value{Value: arr},
	})
	return arr, true, nil
}

func unionKey(obj *hujson.Object, key string, add []string) bool {
	if m := findMember(obj, key); m != nil {
		have, ok := stringsOf(m.Value)
		if !ok {
			return false
		}
		merged := union(have, add)
		if slices.Equal(have, merged) {
			return false
		}
		m.Value = stringList(merged)
		return true
	}
	if len(add) == 0 {
		return false
	}
	obj.Members = append(obj.Members, hujson.ObjectMember{
		Name:  nameValue(key),
		Value: stringList(add),
	})
	return true
}

func findMember(obj *hujson.Object, key string) *hujson.ObjectMember {
	for i := range obj.Members {
		lit, ok := obj.Members[i].Name.Value.(hujson.Literal)
		if ok && lit.String() == key {
			return &obj.Members[i]
		}
	}
	return nil
}

func nameValue(s string) hujson.Value {
	return hujson.Value{Value: hujson.String(s)}
}

func stringList(ss []string) hujson.Value {
	elems := make([]hujson.Value, len(ss))
	for i, s := range ss {
		elems[i] = hujson.Value{Value: hujson.String(s)}
	}
	return hujson.Value{Value: &hujson.Array{Elements: elems}}
}

func stringsOf(v hujson.Value) ([]string, bool) {
	arr, ok := v.Value.(*hujson.Array)
	if !ok {
		return nil, false
	}
	out := make([]string, 0, len(arr.Elements))
	for _, el := range arr.Elements {
		lit, ok := el.Value.(hujson.Literal)
		if !ok || lit.Kind() != '"' {
			return nil, false
		}
		out = append(out, lit.String())
	}
	return out, true
}
