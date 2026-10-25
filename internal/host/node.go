package host

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"tscatalog/internal/catalog"
)

type nodeSelf struct {
	Tags []string `json:"Tags"`
}

// SelfTags returns ACL tags currently applied to this node (not merely requested).
func (c CLI) SelfTags(ctx context.Context) ([]string, error) {
	out, err := c.run(ctx, "status", "--json")
	if err != nil {
		return nil, err
	}
	var st struct {
		Self nodeSelf `json:"Self"`
	}
	if err := json.Unmarshal(jsonPayload(out), &st); err != nil {
		return nil, fmt.Errorf("parse tailscale status: %w", err)
	}
	return st.Self.Tags, nil
}

// EnsureHostTags makes this machine a tagged node so it can host VIP Services.
// It requests access.autoApproveHost via tailscale up when tags are missing.
func (c CLI) EnsureHostTags(ctx context.Context, want []string) error {
	if len(want) == 0 {
		return nil
	}
	have, err := c.SelfTags(ctx)
	if err != nil {
		return err
	}
	if catalog.HasAll(have, want) {
		return nil
	}
	if err := c.requestAdvertiseTags(ctx, want); err != nil {
		return err
	}
	have, err = c.SelfTags(ctx)
	if err != nil {
		return err
	}
	if catalog.HasAll(have, want) {
		return nil
	}
	return fmt.Errorf(
		"node still lacks %s after requesting advertise-tags — approve the tag on this device in the admin console (access.tagOwners must allow your identity)",
		strings.Join(want, ","),
	)
}

type debugPrefs struct {
	RouteAll               bool     `json:"RouteAll"`
	AdvertiseRoutes        []string `json:"AdvertiseRoutes"`
	ExitNodeAllowLANAccess bool     `json:"ExitNodeAllowLANAccess"`
	ExitNodeIP             string   `json:"ExitNodeIP"`
	ExitNodeID             string   `json:"ExitNodeID"`
	AdvertiseTags          []string `json:"AdvertiseTags"`
	ShieldsUp              bool     `json:"ShieldsUp"`
	RunSSH                 bool     `json:"RunSSH"`
}

func (c CLI) requestAdvertiseTags(ctx context.Context, want []string) error {
	prefs, err := c.debugPrefs(ctx)
	if err != nil {
		return err
	}
	tags := unionStrings(prefs.AdvertiseTags, want)
	tagArg := strings.Join(tags, ",")
	args := upArgsFromPrefs(prefs, tagArg)
	err = c.runUp(ctx, args)
	if err != nil && strings.Contains(err.Error(), "requires mentioning all") {
		err = c.runUp(ctx, append([]string{"--reset"}, args...))
	}
	if err != nil {
		msg := err.Error()
		if cmd := extractSuggestedUp(msg); cmd != "" {
			fields := strings.Fields(cmd)
			if len(fields) > 1 {
				args = fields[1:] // drop "tailscale"
				err = c.runUp(ctx, args)
			}
		}
	}
	if err != nil && strings.Contains(err.Error(), "exit-node-allow-lan-access") {
		args = dropFlag(args, "--exit-node-allow-lan-access")
		err = c.runUp(ctx, args)
	}
	if err != nil {
		return fmt.Errorf("request node tags %s: %w", tagArg, err)
	}
	return nil
}

func (c CLI) runUp(ctx context.Context, args []string) error {
	if len(args) == 0 || args[0] != "up" {
		args = append([]string{"up"}, args...)
	}
	_, err := c.run(ctx, args...)
	return err
}

func (c CLI) debugPrefs(ctx context.Context) (debugPrefs, error) {
	out, err := c.run(ctx, "debug", "prefs")
	if err != nil {
		return debugPrefs{}, err
	}
	var p debugPrefs
	if err := json.Unmarshal(jsonPayload(out), &p); err != nil {
		return debugPrefs{}, fmt.Errorf("parse tailscale prefs: %w", err)
	}
	return p, nil
}

func upArgsFromPrefs(p debugPrefs, tagArg string) []string {
	args := []string{"--advertise-tags=" + tagArg}
	if p.RouteAll {
		args = append(args, "--accept-routes")
	}
	if routesAdvertiseExitNode(p.AdvertiseRoutes) {
		args = append(args, "--advertise-exit-node")
	}
	if len(p.AdvertiseRoutes) > 0 && !routesAdvertiseExitNode(p.AdvertiseRoutes) {
		args = append(args, "--advertise-routes="+strings.Join(p.AdvertiseRoutes, ","))
	}
	if p.ExitNodeAllowLANAccess && usingExitNode(p) {
		args = append(args, "--exit-node-allow-lan-access")
	}
	if p.ShieldsUp {
		args = append(args, "--shields-up")
	}
	if p.RunSSH {
		args = append(args, "--ssh")
	}
	return args
}

func usingExitNode(p debugPrefs) bool {
	return strings.TrimSpace(p.ExitNodeIP) != "" || strings.TrimSpace(p.ExitNodeID) != ""
}

// prefsUpFlags mirrors non-default tailscale prefs as `up` flags so we can
// add --advertise-tags without --reset.
func prefsUpFlags(p debugPrefs) []string {
	return upArgsFromPrefs(p, "tag:placeholder")[1:] // drop placeholder tag
}

func dropFlag(args []string, flag string) []string {
	out := make([]string, 0, len(args))
	for _, a := range args {
		if a == flag || strings.HasPrefix(a, flag+"=") {
			continue
		}
		out = append(out, a)
	}
	return out
}

func routesAdvertiseExitNode(routes []string) bool {
	has4, has6 := false, false
	for _, r := range routes {
		switch r {
		case "0.0.0.0/0":
			has4 = true
		case "::/0":
			has6 = true
		}
	}
	return has4 && has6
}

var suggestedUpRE = regexp.MustCompile(`(?m)^\s*(tailscale up .+)$`)

func extractSuggestedUp(stderr string) string {
	m := suggestedUpRE.FindStringSubmatch(stderr)
	if len(m) < 2 {
		return ""
	}
	return strings.TrimSpace(m[1])
}

func unionStrings(have, add []string) []string {
	out := append([]string(nil), have...)
	for _, a := range add {
		a = strings.TrimSpace(a)
		if a == "" {
			continue
		}
		if !strings.HasPrefix(a, "tag:") {
			a = "tag:" + a
		}
		found := false
		for _, h := range out {
			if h == a {
				found = true
				break
			}
		}
		if !found {
			out = append(out, a)
		}
	}
	return out
}
