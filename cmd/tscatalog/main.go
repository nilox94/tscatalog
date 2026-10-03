package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"charm.land/fang/v2"

	"github.com/spf13/cobra"

	"tailscale.com/client/tailscale/v2"

	"tscatalog/internal/catalog"
	"tscatalog/internal/host"
	"tscatalog/internal/lifecycle"
	"tscatalog/internal/policy"
	"tscatalog/internal/syncer"
	"tscatalog/internal/tsapi"
)

const rootLong = `Named Tailscale Services as code.

Catalog file (when -f is omitted): TSCATALOG_CATALOG, then
$XDG_CONFIG_HOME/tscatalog/catalog.yaml, then ./catalog.yaml.

Auth (control plane), loaded from .env next to the catalog if unset:
  TAILSCALE_API_KEY                  access token with Services write
  TAILSCALE_OAUTH_CLIENT_ID/SECRET   alternative to an API key
  TAILSCALE_TAILNET                  default "-" (the credential's tailnet)

The tailnet name is not stored in the catalog. Public URLs are
https://<service>.<tailnet>.ts.net — always port 443.`

func main() {
	if err := fang.Execute(context.Background(), newRootCmd()); err != nil {
		os.Exit(1)
	}
}

func newRootCmd() *cobra.Command {
	var file string
	var cat *catalog.File

	root := &cobra.Command{
		Use:           "tscatalog",
		Short:         "Named Tailscale Services as code",
		Long:          rootLong,
		SilenceUsage:  true,
		SilenceErrors: true,
		PersistentPreRunE: func(cmd *cobra.Command, _ []string) error {
			catPath, err := catalog.ResolvePath(file)
			if err != nil {
				return err
			}
			if err := tsapi.LoadDotEnv(filepath.Join(filepath.Dir(catPath), ".env")); err != nil {
				return err
			}
			cat, err = catalog.Load(catPath)
			return err
		},
	}
	root.PersistentFlags().StringVarP(&file, "file", "f", "", "catalog file (default: auto-discover)")

	completeNames := func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
		catPath, err := catalog.ResolvePath(file)
		if err != nil {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		c, err := catalog.Load(catPath)
		if err != nil {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		return shortNames(c), cobra.ShellCompDirectiveNoFileComp
	}
	named := func(use, short, long string, run func(context.Context, *catalog.File, []string) error) *cobra.Command {
		return &cobra.Command{
			Use:               use + " [name ...]",
			Short:             short,
			Long:              long,
			ValidArgsFunction: completeNames,
			RunE: func(cmd *cobra.Command, args []string) error {
				return run(cmd.Context(), cat, args)
			},
		}
	}

	root.AddCommand(&cobra.Command{
		Use:   "plan",
		Short: "Diff VIP Services, local Serve, and the tailnet ACL against the catalog",
		Long:  "Full catalog diff. Does not include local processes or health probes.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runPlan(cmd.Context(), cat)
		},
	})

	var dry, tailnetOnly, hostOnly, policyOnly, noPolicy bool
	apply := &cobra.Command{
		Use:   "apply [name ...]",
		Short: "Reconcile VIP Services, Serve, advertisements, and the ACL",
		Long: `Names limit VIP puts, Serve mappings, and steady-state advertise
to those catalog services. The ACL merge is always the whole catalog.
After an ACL write, every service is drained, re-served, and advertised
so auto-approval can attach.

No names means every catalog service. When the selected rows and the
ACL are already in sync, apply prints "tailnet unchanged" and does not
advertise.

Arguments may be a catalog name, a vip label, or svc:<label>.`,
		ValidArgsFunction: completeNames,
		RunE: func(cmd *cobra.Command, args []string) error {
			ids, err := catalogIDs(cat, args)
			if err != nil {
				return err
			}
			opts := syncer.Options{
				DryRun:    dry,
				Tailnet:   !hostOnly && !policyOnly,
				Host:      !tailnetOnly && !policyOnly,
				Advertise: !tailnetOnly && !policyOnly,
				Policy:    (!noPolicy && !hostOnly && !tailnetOnly) || policyOnly,
				Names:     ids,
			}
			return runApply(cmd.Context(), cat, opts)
		},
	}
	af := apply.Flags()
	af.BoolVar(&dry, "dry-run", false, "print the plan and exit")
	af.BoolVar(&tailnetOnly, "tailnet-only", false, "only update VIP Services via the API")
	af.BoolVar(&hostOnly, "host-only", false, "only update local Serve mappings")
	af.BoolVar(&policyOnly, "policy-only", false, "only merge the tailnet ACL")
	af.BoolVar(&noPolicy, "no-policy", false, "skip ACL merge")
	apply.MarkFlagsMutuallyExclusive("policy-only", "host-only")
	apply.MarkFlagsMutuallyExclusive("policy-only", "tailnet-only")
	apply.MarkFlagsMutuallyExclusive("policy-only", "no-policy")
	root.AddCommand(apply)

	root.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List catalog services with API ports and Serve targets",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runList(cmd.Context(), cat)
		},
	})

	var localOnly bool
	up := named("up", "Start selected services, reconcile the tailnet, then probe",
		`Phases, in order: start catalog launch, apply, health.
Names limit launch, VIP puts, Serve, steady-state advertise, and probes.
The ACL merge is always the whole catalog. After an ACL write, every
service is drained, re-served, and advertised.

No names means every catalog service. --local skips apply.
"Already listening" means the local port is open; up does not restart
a process that still holds that port.`,
		func(ctx context.Context, c *catalog.File, names []string) error {
			return runUp(ctx, c, names, !localOnly)
		})
	up.Flags().BoolVar(&localOnly, "local", false, "start and probe only; skip VIP, Serve, advertise, and ACL")
	root.AddCommand(
		up,
		named("stop", "Run catalog stop argv, or SIGTERM a process tscatalog detached", "", runStop),
		named("health", "Probe TCP + HTTP locally, HTTPS on the public URL, and host approval", "", runHealth),
		named("describe", "Print catalog services with VIP, Serve, approval, and public URL",
			"Arguments may be a catalog name, a vip label, or svc:<label>.", runDescribe),
	)
	return root
}

func runPlan(ctx context.Context, cat *catalog.File) error {
	api, serve, err := snapshot(ctx, true, true)
	if err != nil {
		return err
	}
	p := syncer.ComputePlan(cat, api, serve)
	c, err := tsapi.NewClient()
	if err != nil {
		return err
	}
	acl, err := c.PolicyFile().Get(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "note: ACL unavailable: %v\n", err)
	} else {
		p.Policy = policyplan(cat, acl)
	}
	syncer.FprintPlan(os.Stdout, p)
	printURLs(ctx, cat)
	return nil
}

func runApply(ctx context.Context, cat *catalog.File, opts syncer.Options) error {
	var api syncer.API
	var pol syncer.PolicyAPI
	if opts.Tailnet || opts.Policy {
		c, err := tsapi.NewClient()
		if err != nil {
			return err
		}
		if opts.Tailnet {
			api = syncer.ClientAPI{Services: c.Services()}
		}
		if opts.Policy {
			pol = syncer.ClientPolicy{Policy: c.PolicyFile()}
		}
	}
	var h syncer.Host
	if opts.Host {
		h = host.CLI{}
	}
	if err := syncer.Apply(ctx, cat, api, h, pol, opts, os.Stdout); err != nil {
		return err
	}
	if !opts.DryRun {
		printURLs(ctx, cat)
	}
	return nil
}

func runList(ctx context.Context, cat *catalog.File) error {
	have, serve, err := snapshot(ctx, true, true)
	if err != nil {
		fmt.Fprintf(os.Stderr, "note: %v\n", err)
	}
	byName := map[string][]string{}
	for _, s := range have {
		byName[s.Name] = s.Ports
	}
	fmt.Printf("%-14s %-10s %-28s %s\n", "SERVICE", "API PORTS", "SERVE", "URL")
	suffix := magicSuffix(ctx)
	for _, s := range cat.Services {
		apiPorts := "-"
		if p := byName[s.SVCName()]; len(p) > 0 {
			apiPorts = strings.Join(p, ",")
		}
		serveT := "-"
		if serve != nil {
			if ep, ok := serve.Services[s.SVCName()]; ok {
				serveT = ep.Endpoints[cat.Endpoint]
				if serveT == "" {
					for k, v := range ep.Endpoints {
						serveT = k + "→" + v
						break
					}
				}
			}
		}
		url := "https://" + s.TailscaleLabel() + ".<tailnet>.ts.net"
		if suffix != "" {
			url = "https://" + s.TailscaleLabel() + "." + suffix
		}
		fmt.Printf("%-14s %-10s %-28s %s\n", s.Name, apiPorts, serveT, url)
	}
	return nil
}

func runDescribe(ctx context.Context, cat *catalog.File, names []string) error {
	svcs := cat.Services
	if len(names) > 0 {
		svcs = make([]catalog.Service, 0, len(names))
		for _, name := range names {
			s, ok := cat.Lookup(name)
			if !ok {
				return fmt.Errorf("unknown service %q (catalog: %s)", name, strings.Join(shortNames(cat), ", "))
			}
			svcs = append(svcs, s)
		}
	}

	have, serve, err := snapshot(ctx, true, true)
	if err != nil {
		fmt.Fprintf(os.Stderr, "note: live overlay unavailable: %v\n", err)
	}
	byName := map[string]tailscale.Service{}
	for _, s := range have {
		byName[s.Name] = s
	}
	suffix := magicSuffix(ctx)

	for i, s := range svcs {
		if i > 0 {
			fmt.Println()
		}
		approval := ""
		if hosts, err := tsapi.ListHosts(ctx, s.SVCName()); err != nil {
			approval = "(approval unavailable)"
		} else {
			approval = tsapi.ApprovalSummary(hosts)
		}
		printShow(s, cat.Endpoint, byName[s.SVCName()], serve, suffix, approval)
	}
	return nil
}

func shortNames(cat *catalog.File) []string {
	out := make([]string, len(cat.Services))
	for i, s := range cat.Services {
		out[i] = s.Name
	}
	return out
}

func printShow(s catalog.Service, endpoint string, api tailscale.Service, serve *catalog.ServeFile, suffix, approval string) {
	fmt.Printf("%s\n", s.Name)
	fmt.Printf("  svc       %s\n", s.SVCName())
	if s.TailscaleLabel() != s.Name {
		fmt.Printf("  vip       %s\n", s.TailscaleLabel())
	}
	if s.DisplayName != "" {
		fmt.Printf("  display   %s\n", s.DisplayName)
	}
	if s.Comment != "" {
		fmt.Printf("  comment   %s\n", s.Comment)
	}
	url := "https://" + s.TailscaleLabel() + ".<tailnet>.ts.net"
	if suffix != "" {
		url = "https://" + s.TailscaleLabel() + "." + suffix
	}
	fmt.Printf("  url       %s\n", url)
	fmt.Printf("  catalog   %s → %s\n", endpoint, s.Local)

	if api.Name == "" {
		fmt.Printf("  api       (not defined)\n")
	} else {
		ports := strings.Join(api.Ports, ",")
		if ports == "" {
			ports = "-"
		}
		addrs := strings.Join(api.Addrs, ", ")
		if addrs == "" {
			fmt.Printf("  api       ports=%s\n", ports)
		} else {
			fmt.Printf("  api       ports=%s  addrs=%s\n", ports, addrs)
		}
		if len(api.Tags) > 0 {
			fmt.Printf("  tags      %s\n", strings.Join(api.Tags, ", "))
		}
	}

	serveT := "(not mapped)"
	if serve != nil {
		if ep, ok := serve.Services[s.SVCName()]; ok {
			if v := ep.Endpoints[endpoint]; v != "" {
				serveT = endpoint + " → " + v
			} else if len(ep.Endpoints) > 0 {
				parts := make([]string, 0, len(ep.Endpoints))
				for k, v := range ep.Endpoints {
					parts = append(parts, k+" → "+v)
				}
				serveT = strings.Join(parts, "; ")
			}
		}
	}
	fmt.Printf("  serve     %s\n", serveT)
	if approval != "" {
		fmt.Printf("  host      %s\n", approval)
	}
}

func snapshot(ctx context.Context, wantAPI, wantHost bool) ([]tailscale.Service, *catalog.ServeFile, error) {
	var have []tailscale.Service
	var serve *catalog.ServeFile
	var errs []error
	if wantAPI {
		c, err := tsapi.NewClient()
		if err != nil {
			errs = append(errs, err)
		} else {
			list, err := c.Services().List(ctx)
			if err != nil {
				errs = append(errs, err)
			} else {
				have = list
			}
		}
	}
	if wantHost {
		cfg, err := (host.CLI{}).GetConfig(ctx)
		if err != nil {
			errs = append(errs, err)
		} else {
			serve = cfg
		}
	}
	return have, serve, errors.Join(errs...)
}

func printURLs(ctx context.Context, cat *catalog.File) {
	suffix := magicSuffix(ctx)
	if suffix == "" {
		fmt.Println("public: https://<name>.<tailnet>.ts.net")
		return
	}
	fmt.Println("public URLs")
	for _, s := range cat.Services {
		fmt.Printf("  https://%s.%s\n", s.TailscaleLabel(), suffix)
	}
}

func magicSuffix(ctx context.Context) string {
	md, err := (host.CLI{}).MagicDNS(ctx)
	if err != nil {
		return ""
	}
	return strings.TrimSuffix(md.Suffix, ".")
}

func policyplan(cat *catalog.File, acl *tailscale.ACL) []policy.Change {
	return policy.ComputePlan(policy.FromCatalog(cat), acl).Changes
}

func catalogIDs(cat *catalog.File, names []string) ([]string, error) {
	svcs, err := selectServices(cat, names)
	if err != nil || len(names) == 0 {
		return nil, err
	}
	out := make([]string, len(svcs))
	for i, s := range svcs {
		out[i] = s.Name
	}
	return out, nil
}

func selectServices(cat *catalog.File, names []string) ([]catalog.Service, error) {
	if len(names) == 0 {
		return cat.Services, nil
	}
	out := make([]catalog.Service, 0, len(names))
	for _, name := range names {
		s, ok := cat.Lookup(name)
		if !ok {
			return nil, fmt.Errorf("unknown service %q (catalog: %s)", name, strings.Join(shortNames(cat), ", "))
		}
		out = append(out, s)
	}
	return out, nil
}

func publicBase(ctx context.Context, name string) string {
	suffix := magicSuffix(ctx)
	if suffix == "" {
		return ""
	}
	return "https://" + name + "." + suffix
}

func runRuntime(ctx context.Context, cat *catalog.File, names []string) error {
	svcs, err := selectServices(cat, names)
	if err != nil {
		return err
	}
	var first error
	for _, s := range svcs {
		if err := lifecycle.Start(ctx, s, os.Stdout); err != nil {
			fmt.Fprintln(os.Stderr, err)
			if first == nil {
				first = err
			}
		}
	}
	return first
}

func runStop(ctx context.Context, cat *catalog.File, names []string) error {
	svcs, err := selectServices(cat, names)
	if err != nil {
		return err
	}
	var first error
	for _, s := range svcs {
		if err := lifecycle.Stop(ctx, s, os.Stdout); err != nil {
			fmt.Fprintln(os.Stderr, err)
			if first == nil {
				first = err
			}
		}
	}
	return first
}

func runHealth(ctx context.Context, cat *catalog.File, names []string) error {
	svcs, err := selectServices(cat, names)
	if err != nil {
		return err
	}
	fmt.Printf("%-14s %-8s %-10s %-10s %s\n", "SERVICE", "BIND", "LOCAL", "PUBLIC", "HOST")
	var failed int
	for _, s := range svcs {
		host := "-"
		if hosts, err := tsapi.ListHosts(ctx, s.SVCName()); err == nil {
			host = tsapi.ApprovalSummary(hosts)
		}
		r := lifecycle.Probe(ctx, s, publicBase(ctx, s.TailscaleLabel()), host)
		fmt.Printf("%-14s %-8s %-10s %-10s %s\n", r.Name, r.Bind, r.Local, r.Public, r.Host)
		if !r.OK {
			failed++
		}
	}
	if failed == 0 {
		return nil
	}
	return fmt.Errorf("%d service(s) unhealthy", failed)
}

func runUp(ctx context.Context, cat *catalog.File, names []string, publish bool) error {
	if err := runRuntime(ctx, cat, names); err != nil {
		return err
	}
	if publish {
		ids, err := catalogIDs(cat, names)
		if err != nil {
			return err
		}
		opts := syncer.Options{Tailnet: true, Host: true, Advertise: true, Policy: true, Names: ids}
		if err := runApply(ctx, cat, opts); err != nil {
			return err
		}
	}
	return runHealth(ctx, cat, names)
}
