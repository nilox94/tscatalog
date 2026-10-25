package tui

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"tscatalog/internal/catalog"
	"tscatalog/internal/composeclient"
	"tscatalog/internal/host"
	"tscatalog/internal/lifecycle"
	"tscatalog/internal/syncer"
	"tscatalog/internal/tsapi"
)

// live probes the machine and runs catalog start, stop, and restart.
// Restart republishes published services (VIP, Serve, advertise).
type live struct {
	cat *catalog.File
}

// probePrep is the shared tailnet and Docker state for one probe pass.
// Each service is probed on its own after this returns.
type probePrep struct {
	suffix  string
	serve   *catalog.ServeFile
	comp    *composeclient.Client
	dialErr error
}

// Prepare loads MagicDNS, Serve, and the Compose client. Each call is capped
// so one hung command does not hold the other rows.
func (l live) Prepare(ctx context.Context) (probePrep, error) {
	prepCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var prep probePrep
	var wg sync.WaitGroup
	wg.Add(3)
	go func() {
		defer wg.Done()
		if md, err := (host.CLI{}).MagicDNS(prepCtx); err == nil {
			prep.suffix = strings.TrimSuffix(md.Suffix, ".")
		}
	}()
	go func() {
		defer wg.Done()
		serve, err := (host.CLI{}).GetConfig(prepCtx)
		if err != nil {
			serve = nil
		}
		prep.serve = serve
	}()
	go func() {
		defer wg.Done()
		prep.comp, prep.dialErr = lifecycle.DialCompose(prepCtx, l.cat.Services)
	}()
	wg.Wait()
	return prep, nil
}

// Probe checks one service. The caller paints the row when this returns.
func (l live) Probe(ctx context.Context, prep probePrep, s catalog.Service) Row {
	return probeRow(ctx, l.cat, s, prep.serve, prep.suffix, prep.comp, prep.dialErr)
}

func probeRow(ctx context.Context, cat *catalog.File, s catalog.Service, serve *catalog.ServeFile, suffix string, comp *composeclient.Client, dialErr error) Row {
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
	lctx := ctx
	if composeLaunch(s) {
		var cancel context.CancelFunc
		lctx, cancel = context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
	}
	return Row{
		Service:  s,
		Report:   lifecycle.Probe(ctx, s, pub, hostSum),
		Serve:    serveText(cat, s, serve),
		URL:      urlText(s, suffix),
		Owned:    lifecycle.CatalogOwns(s),
		Fresh:    true,
		Launches: lifecycle.LaunchStatuses(lctx, s, comp, dialErr),
	}
}

func composeLaunch(s catalog.Service) bool {
	for _, spec := range s.Launch {
		kind, err := spec.ResolveKind()
		if err == nil && kind == catalog.KindCompose {
			return true
		}
	}
	return false
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
	return l.restartMany(ctx, []string{name})
}

func (l live) RestartAll(ctx context.Context, names []string) (string, error) {
	return l.restartMany(ctx, names)
}

func (l live) restartMany(ctx context.Context, names []string) (string, error) {
	var buf bytes.Buffer
	var restarted []string
	var first error
	for _, name := range names {
		if err := lifecycle.Restart(ctx, l.cat, name, &buf); err != nil {
			fmt.Fprintf(&buf, "%s\n", err)
			if first == nil {
				first = err
			}
			continue
		}
		restarted = append(restarted, name)
	}
	if len(restarted) == 0 {
		if first != nil {
			return strings.TrimSpace(buf.String()), first
		}
		return "", nil
	}
	publish := syncer.PublishedNames(l.cat, restarted)
	if len(publish) > 0 {
		if err := syncer.RepublishNames(ctx, l.cat, publish, &buf); err != nil {
			fmt.Fprintf(&buf, "%s\n", err)
			if first == nil {
				first = err
			}
		}
	}
	out := strings.TrimSpace(buf.String())
	if first != nil {
		return out, first
	}
	if len(names) == 1 {
		return out, nil
	}
	return fmt.Sprintf("restarted %d", len(restarted)), nil
}
