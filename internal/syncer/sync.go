package syncer

import (
	"context"
	"fmt"
	"io"
	"strings"

	"tailscale.com/client/tailscale/v2"

	"github.com/nilox94/tscatalog/internal/catalog"
	"github.com/nilox94/tscatalog/internal/policy"
	"github.com/nilox94/tscatalog/internal/style"
)

type API interface {
	List(ctx context.Context) ([]tailscale.Service, error)
	CreateOrUpdate(ctx context.Context, svc tailscale.Service) error
	Delete(ctx context.Context, name string) error
}

type PolicyAPI interface {
	Get(ctx context.Context) (*tailscale.ACL, error)
	Raw(ctx context.Context) (body, etag string, err error)
	SetRaw(ctx context.Context, body, etag string) error
	ValidateRaw(ctx context.Context, body string) error
}

type ClientPolicy struct {
	Policy *tailscale.PolicyFileResource
}

func (c ClientPolicy) Get(ctx context.Context) (*tailscale.ACL, error) {
	return c.Policy.Get(ctx)
}

func (c ClientPolicy) Raw(ctx context.Context) (string, string, error) {
	raw, err := c.Policy.Raw(ctx)
	if err != nil {
		return "", "", err
	}
	return raw.HuJSON, raw.ETag, nil
}

func (c ClientPolicy) SetRaw(ctx context.Context, body, etag string) error {
	return c.Policy.Set(ctx, body, etag)
}

func (c ClientPolicy) ValidateRaw(ctx context.Context, body string) error {
	return c.Policy.Validate(ctx, body)
}

type Host interface {
	GetConfig(ctx context.Context) (*catalog.ServeFile, error)
	EnsureHostTags(ctx context.Context, tags []string, resetNode bool) error
	Serve(ctx context.Context, name, endpoint, local string) error
	Advertise(ctx context.Context, svc string) error
	Drain(ctx context.Context, svc string) error
	Clear(ctx context.Context, name, endpoint string) error
}

type ClientAPI struct {
	Services *tailscale.ServicesResource
}

func (c ClientAPI) List(ctx context.Context) ([]tailscale.Service, error) {
	return c.Services.List(ctx)
}

func (c ClientAPI) CreateOrUpdate(ctx context.Context, svc tailscale.Service) error {
	return c.Services.CreateOrUpdate(ctx, svc)
}

func (c ClientAPI) Delete(ctx context.Context, name string) error {
	return c.Services.Delete(ctx, name)
}

type Plan struct {
	Tailnet []Change
	Host    []Change
	Policy  []policy.Change
}

type Change struct {
	Name   string
	Action string // create, update, unchanged
	Detail string
}

func (p Plan) NeedsApply() bool {
	for _, c := range p.Tailnet {
		if c.Action != "unchanged" {
			return true
		}
	}
	for _, c := range p.Host {
		if c.Action != "unchanged" {
			return true
		}
	}
	for _, c := range p.Policy {
		if c.Action != "unchanged" {
			return true
		}
	}
	return false
}

func ComputePlan(want *catalog.File, have []tailscale.Service, serve *catalog.ServeFile) Plan {
	byName := map[string]tailscale.Service{}
	for _, s := range have {
		byName[s.Name] = s
	}
	var p Plan
	for _, s := range want.Services {
		if !s.Published() {
			p.Tailnet = append(p.Tailnet, Change{
				Name:   s.SVCName(),
				Action: "unchanged",
				Detail: "unpublished",
			})
			continue
		}
		desired := s.VIPService(want.Endpoint, want.HostTags())
		cur, ok := byName[desired.Name]
		ch := Change{Name: desired.Name}
		switch {
		case !ok:
			ch.Action = "create"
			ch.Detail = fmt.Sprintf("ports=%s tags=%s", strings.Join(desired.Ports, ","), strings.Join(desired.Tags, ","))
		case !catalog.PortsEqual(cur.Ports, desired.Ports) || !catalog.HasAll(cur.Tags, desired.Tags):
			ch.Action = "update"
			ch.Detail = fmt.Sprintf("ports %v → %v tags %v → %v", cur.Ports, desired.Ports, cur.Tags, desired.Tags)
		default:
			ch.Action = "unchanged"
			ch.Detail = fmt.Sprintf("ports=%s", strings.Join(desired.Ports, ","))
		}
		p.Tailnet = append(p.Tailnet, ch)
	}

	if serve == nil {
		serve = &catalog.ServeFile{Services: map[string]catalog.ServeService{}}
	}
	wantServe := want.ServeConfig()
	for _, s := range want.Services {
		name := s.SVCName()
		if !s.Published() {
			p.Host = append(p.Host, Change{
				Name:   name,
				Action: "unchanged",
				Detail: "unpublished (loopback only)",
			})
			continue
		}
		wantEP := wantServe.Services[name].Endpoints[want.Endpoint]
		ch := Change{Name: name}
		got, ok := serve.Services[name]
		gotEP := ""
		if ok {
			gotEP = got.Endpoints[want.Endpoint]
		}
		switch {
		case !ok || gotEP == "":
			ch.Action = "create"
			ch.Detail = fmt.Sprintf("%s → %s", want.Endpoint, wantEP)
		case gotEP != wantEP:
			ch.Action = "update"
			ch.Detail = fmt.Sprintf("%s → %s (was %s)", want.Endpoint, wantEP, gotEP)
		default:
			ch.Action = "unchanged"
			ch.Detail = fmt.Sprintf("%s → %s", want.Endpoint, wantEP)
		}
		p.Host = append(p.Host, ch)
	}
	return p
}

func FprintPlan(w io.Writer, p Plan) {
	if len(p.Tailnet) > 0 {
		fmt.Fprintln(w, style.Bold("tailnet (VIP Services API)"))
		for _, c := range p.Tailnet {
			fmt.Fprintf(w, "  %s  %-18s  %s\n", style.Action(c.Action), c.Name, c.Detail)
		}
	}
	if len(p.Host) > 0 {
		fmt.Fprintln(w, style.Bold("host (tailscale serve)"))
		for _, c := range p.Host {
			fmt.Fprintf(w, "  %s  %-18s  %s\n", style.Action(c.Action), c.Name, c.Detail)
		}
	}
	if len(p.Policy) > 0 {
		fmt.Fprintln(w, style.Bold("policy (ACL)"))
		for _, c := range p.Policy {
			fmt.Fprintf(w, "  %s  %-28s  %s\n", style.Action(c.Action), c.Name, c.Detail)
		}
	}
}

type Options struct {
	DryRun    bool
	Tailnet   bool
	Host      bool
	Advertise bool
	Policy    bool
	// Names limits VIP puts, Serve, and steady-state advertise to these
	// catalog names (or vip / svc: aliases). Empty means every service.
	// ACL merge stays catalog-wide. After a policy write, every service
	// is drained, re-served, and advertised.
	Names []string
	// Prune deletes VIP Services and Serve mappings whose svc: name is
	// not in this catalog, and rewrites the catalog-owned 443 grant.
	Prune bool
	// ResetNode allows tailscale up --reset when tagging the node.
	ResetNode bool
}

func Apply(ctx context.Context, want *catalog.File, api API, host Host, pol PolicyAPI, opts Options, w io.Writer) error {
	names, err := canonicalNames(want, opts.Names)
	if err != nil {
		return err
	}
	opts.Names = names

	if !opts.Tailnet && !opts.Host && !opts.Policy {
		opts.Tailnet = true
		opts.Host = true
		opts.Advertise = true
		opts.Policy = true
	}

	var have []tailscale.Service
	if api != nil && opts.Tailnet {
		var err error
		have, err = api.List(ctx)
		if err != nil {
			return fmt.Errorf("list VIP services: %w", err)
		}
	}
	var serve *catalog.ServeFile
	if host != nil && opts.Host {
		var err error
		serve, err = host.GetConfig(ctx)
		if err != nil {
			return fmt.Errorf("get serve config: %w", err)
		}
	}

	plan := ComputePlan(want, have, serve)
	if opts.Prune {
		plan.Tailnet = append(plan.Tailnet, pruneTailnet(want, have)...)
		plan.Host = append(plan.Host, pruneHost(want, serve)...)
	}
	var haveACL *tailscale.ACL
	if opts.Policy {
		if pol == nil {
			return fmt.Errorf("policy API client required")
		}
		var err error
		haveACL, err = pol.Get(ctx)
		if err != nil {
			return fmt.Errorf("get ACL: %w", err)
		}
		plan.Policy = policy.ComputePlan(policy.FromCatalog(want), haveACL).Changes
		if opts.Prune && grantNeedsPrune(haveACL, policy.FromCatalog(want).GrantDst) {
			plan.Policy = append(plan.Policy, policy.Change{
				Name:   "grants",
				Action: "update",
				Detail: "prune dst to catalog",
			})
		}
	}
	shown := filterPlan(want, plan, opts.Names)
	if !opts.Tailnet {
		shown.Tailnet = nil
	}
	if !opts.Host {
		shown.Host = nil
	}
	if !scopeNeedsApply(want, plan, opts) {
		fmt.Fprintln(w, style.Faint("tailnet unchanged"))
		return nil
	}
	FprintPlan(w, shown)
	if opts.DryRun {
		return nil
	}

	policyChanged := false
	if opts.Policy {
		if pol == nil {
			return fmt.Errorf("policy API client required")
		}
		rawBody, etag, err := pol.Raw(ctx)
		if err != nil {
			return fmt.Errorf("get ACL: %w", err)
		}
		edited, err := policy.Edit(rawBody, policy.FromCatalog(want), opts.Prune)
		if err != nil {
			return fmt.Errorf("edit ACL: %w", err)
		}
		if edited == rawBody {
			fmt.Fprintln(w, style.Faint("policy unchanged"))
		} else {
			if err := pol.ValidateRaw(ctx, edited); err != nil {
				return fmt.Errorf("validate ACL: %w", err)
			}
			if err := pol.SetRaw(ctx, edited, etag); err != nil {
				return fmt.Errorf("set ACL: %w", err)
			}
			policyChanged = true
			fmt.Fprintln(w, style.Warn("policy merged (tagOwners, autoApprovers; grants only if no catch-all)"))
		}
	}

	if opts.Prune && opts.Tailnet {
		if api == nil {
			return fmt.Errorf("control-plane API client required")
		}
		for _, svc := range have {
			if _, ok := publishedSet(want)[svc.Name]; ok {
				continue
			}
			if err := api.Delete(ctx, svc.Name); err != nil {
				return fmt.Errorf("delete %s: %w", svc.Name, err)
			}
			fmt.Fprintf(w, "delete %s\n", svc.Name)
		}
	}

	if opts.Tailnet {
		if api == nil {
			return fmt.Errorf("control-plane API client required")
		}
		byName := map[string]tailscale.Service{}
		for _, s := range have {
			byName[s.Name] = s
		}
		for i, s := range want.Services {
			if !s.Published() {
				continue
			}
			if !selected(opts.Names, s.Name) || plan.Tailnet[i].Action == "unchanged" {
				continue
			}
			desired := s.VIPService(want.Endpoint, want.HostTags())
			body := mergeForPut(desired, byName[desired.Name])
			if err := api.CreateOrUpdate(ctx, body); err != nil {
				return fmt.Errorf("put %s: %w", desired.Name, err)
			}
			fmt.Fprintf(w, "put %s\n", desired.Name)
		}
	}

	if opts.Host {
		if host == nil {
			return fmt.Errorf("local tailscale CLI required")
		}
		if tags := want.HostTags(); len(tags) > 0 {
			if err := host.EnsureHostTags(ctx, tags, opts.ResetNode); err != nil {
				return fmt.Errorf("host identity: %w", err)
			}
		}
		if opts.Prune && serve != nil {
			keep := publishedSet(want)
			for name := range serve.Services {
				if _, ok := keep[name]; ok {
					continue
				}
				if err := host.Clear(ctx, name, want.Endpoint); err != nil {
					return fmt.Errorf("clear %s: %w", name, err)
				}
				fmt.Fprintf(w, "clear %s\n", name)
			}
		}
		for i, s := range want.Services {
			inScope := selected(opts.Names, s.Name)
			if !s.Published() {
				continue
			}
			if inScope && plan.Host[i].Action != "unchanged" {
				if err := serveOne(ctx, host, w, s, want.Endpoint); err != nil {
					return err
				}
			}
			// An ACL write has to re-attach every service, not only the names
			// on the command line, or auto-approval never lands on siblings.
			if policyChanged && opts.Advertise {
				if !s.Published() {
					continue
				}
				if err := host.Drain(ctx, s.SVCName()); err != nil {
					return fmt.Errorf("drain %s: %w", s.SVCName(), err)
				}
				fmt.Fprintf(w, "drain %s\n", s.SVCName())
				if err := serveOne(ctx, host, w, s, want.Endpoint); err != nil {
					return err
				}
				if err := host.Advertise(ctx, s.SVCName()); err != nil {
					return fmt.Errorf("advertise %s: %w", s.SVCName(), err)
				}
				fmt.Fprintf(w, "advertise %s\n", s.SVCName())
				continue
			}
			if inScope && opts.Advertise {
				if err := host.Advertise(ctx, s.SVCName()); err != nil {
					return fmt.Errorf("advertise %s: %w", s.SVCName(), err)
				}
				fmt.Fprintf(w, "advertise %s\n", s.SVCName())
			}
		}
	}
	return nil
}

func serveOne(ctx context.Context, host Host, w io.Writer, s catalog.Service, endpoint string) error {
	if err := host.Serve(ctx, s.SVCName(), endpoint, s.Local); err != nil {
		return fmt.Errorf("serve %s: %w", s.SVCName(), err)
	}
	fmt.Fprintf(w, "serve %s --https %s → %s\n", s.SVCName(), strings.TrimPrefix(endpoint, "tcp:"), s.Local)
	return nil
}

func canonicalNames(want *catalog.File, names []string) ([]string, error) {
	if len(names) == 0 {
		return nil, nil
	}
	out := make([]string, 0, len(names))
	seen := map[string]struct{}{}
	for _, name := range names {
		s, ok := want.Lookup(name)
		if !ok {
			return nil, fmt.Errorf("unknown service %q", name)
		}
		if _, ok := seen[s.Name]; ok {
			continue
		}
		seen[s.Name] = struct{}{}
		out = append(out, s.Name)
	}
	return out, nil
}

func selected(names []string, name string) bool {
	if len(names) == 0 {
		return true
	}
	for _, n := range names {
		if n == name {
			return true
		}
	}
	return false
}

func scopeNeedsApply(want *catalog.File, plan Plan, opts Options) bool {
	for i, s := range want.Services {
		if !selected(opts.Names, s.Name) {
			continue
		}
		if opts.Tailnet && i < len(plan.Tailnet) && plan.Tailnet[i].Action != "unchanged" {
			return true
		}
		if opts.Host && i < len(plan.Host) && plan.Host[i].Action != "unchanged" {
			return true
		}
	}
	if opts.Policy {
		for _, c := range plan.Policy {
			if c.Action != "unchanged" {
				return true
			}
		}
	}
	if opts.Prune {
		for _, c := range plan.Tailnet {
			if c.Action == "delete" {
				return true
			}
		}
		for _, c := range plan.Host {
			if c.Action == "delete" {
				return true
			}
		}
	}
	return false
}

func grantNeedsPrune(acl *tailscale.ACL, dst []string) bool {
	if acl == nil {
		return false
	}
	for _, g := range acl.Grants {
		if len(g.Destination) == 0 || len(g.IP) == 0 {
			continue
		}
		owned := true
		for _, d := range g.Destination {
			if !strings.HasPrefix(d, "svc:") {
				owned = false
				break
			}
		}
		if !owned {
			continue
		}
		for _, p := range g.IP {
			if p != "443" && p != "tcp:443" {
				owned = false
				break
			}
		}
		if !owned {
			continue
		}
		if len(g.Destination) != len(dst) {
			return true
		}
		for i := range dst {
			if g.Destination[i] != dst[i] {
				return true
			}
		}
	}
	return false
}

func publishedSet(want *catalog.File) map[string]struct{} {
	keep := map[string]struct{}{}
	for _, s := range want.Services {
		if s.Published() {
			keep[s.SVCName()] = struct{}{}
		}
	}
	return keep
}

func pruneTailnet(want *catalog.File, have []tailscale.Service) []Change {
	keep := publishedSet(want)
	var out []Change
	for _, svc := range have {
		if _, ok := keep[svc.Name]; ok {
			continue
		}
		out = append(out, Change{Name: svc.Name, Action: "delete", Detail: "not in catalog"})
	}
	return out
}

func pruneHost(want *catalog.File, serve *catalog.ServeFile) []Change {
	if serve == nil {
		return nil
	}
	keep := publishedSet(want)
	var out []Change
	for name := range serve.Services {
		if _, ok := keep[name]; ok {
			continue
		}
		out = append(out, Change{Name: name, Action: "delete", Detail: "not in catalog"})
	}
	return out
}

func filterPlan(want *catalog.File, plan Plan, names []string) Plan {
	out := Plan{Policy: plan.Policy}
	if len(names) == 0 {
		out.Tailnet = plan.Tailnet
		out.Host = plan.Host
		return out
	}
	for i, s := range want.Services {
		if !selected(names, s.Name) {
			continue
		}
		if i < len(plan.Tailnet) {
			out.Tailnet = append(out.Tailnet, plan.Tailnet[i])
		}
		if i < len(plan.Host) {
			out.Host = append(out.Host, plan.Host[i])
		}
	}
	for _, c := range plan.Tailnet {
		if c.Action == "delete" {
			out.Tailnet = append(out.Tailnet, c)
		}
	}
	for _, c := range plan.Host {
		if c.Action == "delete" {
			out.Host = append(out.Host, c)
		}
	}
	return out
}

func mergeForPut(desired, existing tailscale.Service) tailscale.Service {
	if existing.Name == "" {
		return desired
	}
	out := existing
	out.Name = desired.Name
	out.Ports = desired.Ports
	if desired.Comment != "" {
		out.Comment = desired.Comment
	}
	out.Tags = unionTags(out.Tags, desired.Tags)
	// Control assigned these; PUT requires them on update.
	out.Addrs = existing.Addrs
	return out
}

func unionTags(have, add []string) []string {
	out := append([]string(nil), have...)
	seen := map[string]struct{}{}
	for _, t := range out {
		seen[t] = struct{}{}
	}
	for _, t := range add {
		if t == "" {
			continue
		}
		if _, ok := seen[t]; ok {
			continue
		}
		seen[t] = struct{}{}
		out = append(out, t)
	}
	return out
}
