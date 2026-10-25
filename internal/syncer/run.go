package syncer

import (
	"context"
	"io"

	"tscatalog/internal/catalog"
	"tscatalog/internal/host"
	"tscatalog/internal/tsapi"
)

// ApplyConfigured wires API and host clients, then reconciles the catalog.
func ApplyConfigured(ctx context.Context, cat *catalog.File, opts Options, w io.Writer) error {
	var api API
	var pol PolicyAPI
	if opts.Tailnet || opts.Policy {
		c, err := tsapi.NewClient()
		if err != nil {
			return err
		}
		if opts.Tailnet {
			api = ClientAPI{Services: c.Services()}
		}
		if opts.Policy {
			pol = ClientPolicy{Policy: c.PolicyFile()}
		}
	}
	var h Host
	if opts.Host {
		h = host.CLI{}
	}
	return Apply(ctx, cat, api, h, pol, opts, w)
}

// RepublishNames runs VIP, Serve, advertise, and ACL for catalog names.
func RepublishNames(ctx context.Context, cat *catalog.File, names []string, w io.Writer) error {
	if len(names) == 0 {
		return nil
	}
	opts := Options{
		Tailnet:   true,
		Host:      true,
		Advertise: true,
		Policy:    true,
		Names:     names,
	}
	return ApplyConfigured(ctx, cat, opts, w)
}

// PublishedNames returns catalog names that are published among the given ids.
func PublishedNames(cat *catalog.File, names []string) []string {
	var out []string
	for _, name := range names {
		s, ok := cat.Lookup(name)
		if !ok || !s.Published() {
			continue
		}
		out = append(out, s.Name)
	}
	return out
}
