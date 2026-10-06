package tui

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"sync"

	"tscatalog/internal/catalog"
	"tscatalog/internal/host"
	"tscatalog/internal/lifecycle"
	"tscatalog/internal/tsapi"
)

// live probes the machine and runs catalog start, stop, and restart.
// It does not write VIP Services, Serve, or the ACL.
type live struct {
	cat *catalog.File
}

func (l live) Load(ctx context.Context) ([]Row, error) {
	suffix := ""
	if md, err := (host.CLI{}).MagicDNS(ctx); err == nil {
		suffix = strings.TrimSuffix(md.Suffix, ".")
	}
	serve, err := (host.CLI{}).GetConfig(ctx)
	if err != nil {
		serve = nil
	}
	rows := make([]Row, len(l.cat.Services))
	var wg sync.WaitGroup
	for i, s := range l.cat.Services {
		wg.Add(1)
		go func(i int, s catalog.Service) {
			defer wg.Done()
			rows[i] = probeRow(ctx, l.cat, s, serve, suffix)
		}(i, s)
	}
	wg.Wait()
	return rows, nil
}

func probeRow(ctx context.Context, cat *catalog.File, s catalog.Service, serve *catalog.ServeFile, suffix string) Row {
	hostSum := "local"
	if s.Published() {
		hostSum = "(approval unavailable)"
		if hosts, err := tsapi.ListHosts(ctx, s.SVCName()); err == nil {
			hostSum = tsapi.ApprovalSummary(hosts)
		}
	}
	pub := ""
	if s.Published() && suffix != "" {
		pub = "https://" + s.TailscaleLabel() + "." + suffix
	}
	return Row{
		Service: s,
		Report:  lifecycle.Probe(ctx, s, pub, hostSum),
		Serve:   serveText(cat, s, serve),
		URL:     urlText(s, suffix),
		Owned:   lifecycle.CatalogOwns(s),
		Fresh:   true,
	}
}

func serveText(cat *catalog.File, s catalog.Service, serve *catalog.ServeFile) string {
	if !s.Published() {
		return "unpublished"
	}
	if serve == nil {
		return "-"
	}
	ep, ok := serve.Services[s.SVCName()]
	if !ok {
		return "-"
	}
	if v := ep.Endpoints[cat.Endpoint]; v != "" {
		return cat.Endpoint + " → " + v
	}
	if len(ep.Endpoints) == 0 {
		return "-"
	}
	parts := make([]string, 0, len(ep.Endpoints))
	for k, v := range ep.Endpoints {
		parts = append(parts, k+" → "+v)
	}
	return strings.Join(parts, "; ")
}

func urlText(s catalog.Service, suffix string) string {
	if !s.Published() {
		return "loopback only"
	}
	if suffix == "" {
		return "https://" + s.TailscaleLabel() + ".<tailnet>.ts.net"
	}
	return "https://" + s.TailscaleLabel() + "." + suffix
}

func (l live) Stop(ctx context.Context, name string) (string, error) {
	s, ok := l.cat.Lookup(name)
	if !ok {
		return "", fmt.Errorf("unknown service %q", name)
	}
	var buf bytes.Buffer
	err := lifecycle.Stop(ctx, s, &buf)
	return strings.TrimSpace(buf.String()), err
}

func (l live) Restart(ctx context.Context, name string) (string, error) {
	var buf bytes.Buffer
	err := lifecycle.Restart(ctx, l.cat, name, &buf)
	return strings.TrimSpace(buf.String()), err
}

func (l live) RestartAll(ctx context.Context, names []string) (string, error) {
	var buf bytes.Buffer
	var first error
	for _, name := range names {
		if err := lifecycle.Restart(ctx, l.cat, name, &buf); err != nil {
			fmt.Fprintf(&buf, "%s\n", err)
			if first == nil {
				first = err
			}
		}
	}
	if first != nil {
		return strings.TrimSpace(buf.String()), first
	}
	return fmt.Sprintf("restarted %d", len(names)), nil
}
