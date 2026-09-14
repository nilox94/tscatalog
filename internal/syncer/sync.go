package syncer

import (
	"context"
	"fmt"
	"io"
	"strings"

	"tailscale.com/client/tailscale/v2"

	"tscatalog/internal/catalog"
)

type API interface {
	List(ctx context.Context) ([]tailscale.Service, error)
	CreateOrUpdate(ctx context.Context, svc tailscale.Service) error
}

type Host interface {
	GetConfig(ctx context.Context) (*catalog.ServeFile, error)
	Serve(ctx context.Context, name, endpoint, local string) error
	Advertise(ctx context.Context, svc string) error
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

type Plan struct {
	Tailnet []Change
	Host    []Change
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
	return false
}

func ComputePlan(want *catalog.File, have []tailscale.Service, serve *catalog.ServeFile) Plan {
	byName := map[string]tailscale.Service{}
	for _, s := range have {
		byName[s.Name] = s
	}
	var p Plan
	for _, s := range want.Services {
		desired := s.VIPService(want.Endpoint)
		cur, ok := byName[desired.Name]
		ch := Change{Name: desired.Name}
		switch {
		case !ok:
			ch.Action = "create"
			ch.Detail = fmt.Sprintf("ports=%s", strings.Join(desired.Ports, ","))
		case !catalog.PortsEqual(cur.Ports, desired.Ports):
			ch.Action = "update"
			ch.Detail = fmt.Sprintf("ports %v → %v", cur.Ports, desired.Ports)
		default:
			// Comment / tags are not managed as a reason to PUT: a replace
			// would drop console tags on an otherwise-correct service.
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
	fmt.Fprintln(w, "tailnet (VIP Services API)")
	for _, c := range p.Tailnet {
		fmt.Fprintf(w, "  %-8s  %-18s  %s\n", c.Action, c.Name, c.Detail)
	}
	fmt.Fprintln(w, "host (tailscale serve)")
	for _, c := range p.Host {
		fmt.Fprintf(w, "  %-8s  %-18s  %s\n", c.Action, c.Name, c.Detail)
	}
}

type Options struct {
	DryRun    bool
	Tailnet   bool
	Host      bool
	Advertise bool
}

func Apply(ctx context.Context, want *catalog.File, api API, host Host, opts Options, w io.Writer) error {
	if !opts.Tailnet && !opts.Host {
		opts.Tailnet = true
		opts.Host = true
		opts.Advertise = true
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
	FprintPlan(w, plan)
	if opts.DryRun {
		return nil
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
			if plan.Tailnet[i].Action == "unchanged" {
				continue
			}
			desired := s.VIPService(want.Endpoint)
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
		for i, s := range want.Services {
			if plan.Host[i].Action == "unchanged" {
				continue
			}
			if err := host.Serve(ctx, s.SVCName(), want.Endpoint, s.Local); err != nil {
				return fmt.Errorf("serve %s: %w", s.SVCName(), err)
			}
			fmt.Fprintf(w, "serve %s --https %s → %s\n", s.SVCName(), strings.TrimPrefix(want.Endpoint, "tcp:"), s.Local)
			if opts.Advertise {
				if err := host.Advertise(ctx, s.SVCName()); err != nil {
					return fmt.Errorf("advertise %s: %w", s.SVCName(), err)
				}
				fmt.Fprintf(w, "advertise %s\n", s.SVCName())
			}
		}
	}
	return nil
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
	// Addrs are assigned by control; do not send them back.
	out.Addrs = nil
	return out
}
