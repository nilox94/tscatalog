package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"

	"charm.land/fang/v2"

	"github.com/spf13/cobra"

	"tailscale.com/client/tailscale/v2"

	"github.com/nilox94/tscatalog/internal/catalog"
	"github.com/nilox94/tscatalog/internal/composeclient"
	"github.com/nilox94/tscatalog/internal/host"
	"github.com/nilox94/tscatalog/internal/lifecycle"
	"github.com/nilox94/tscatalog/internal/policy"
	"github.com/nilox94/tscatalog/internal/style"
	"github.com/nilox94/tscatalog/internal/syncer"
	"github.com/nilox94/tscatalog/internal/tsapi"
)

// version is set with -ldflags -X main.version=...
var version = "dev"

const rootLong = `Named Tailscale Services as code.

Catalog file (when -f is omitted): TSCATALOG_CATALOG, then
$XDG_CONFIG_HOME/tscatalog/catalog.yaml, then ./catalog.yaml.

Auth (control plane), loaded from .env next to the catalog if unset.
Only TS_* keys in that file are loaded:
  TS_API_KEY                  access token
  TS_OAUTH_CLIENT_ID/SECRET   alternative to an API key (scope: all)
  TS_TAILNET                  default "-" (the credential's tailnet)

The tailnet name is not stored in the catalog. Public URLs are
https://<service>.<tailnet>.ts.net — always port 443.`

func main() {
	if len(os.Args) > 1 && os.Args[1] == "__reap" {
		if err := lifecycle.Reap(os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
	if err := fang.Execute(context.Background(), newRootCmd(), fang.WithVersion(version)); err != nil {
		os.Exit(1)
	}
}

func newRootCmd() *cobra.Command {
	var file string
	var cat *catalog.File
	var catPath string

	root := &cobra.Command{
		Use:           "tscatalog",
		Short:         "Named Tailscale Services as code",
		Long:          rootLong,
		SilenceUsage:  true,
		SilenceErrors: true,
		PersistentPreRunE: func(cmd *cobra.Command, _ []string) error {
			var err error
			catPath, err = catalog.ResolvePath(file)
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

	var dry, tailnetOnly, hostOnly, policyOnly, noPolicy, prune, resetNode bool
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

--prune deletes VIP Services and Serve mappings whose svc: name is not
in this catalog, and rewrites the catalog 443 grant. tagOwners stay a
union. Without --prune, removing a row does not delete tailnet state.

--reset-node retries tailscale up with --reset when the node tag cannot
be requested otherwise. That drops settings the command did not repeat.

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
				Prune:     prune,
				ResetNode: resetNode,
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
	af.BoolVar(&prune, "prune", false, "delete VIP Services and Serve mappings absent from this catalog")
	af.BoolVar(&resetNode, "reset-node", false, "allow tailscale up --reset when requesting node tags")
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
		`Phases, in order: start catalog programs, apply, health.
Names limit those starts, VIP puts, Serve, steady-state advertise, and probes.
The ACL merge is always the whole catalog. After an ACL write, every
service is drained, re-served, and advertised.

No names means every catalog service. --local skips apply.
--prune and --reset-node are passed through to apply.
"Already listening" means the local port is open; up does not restart
a process that still holds that port.`,
		func(ctx context.Context, c *catalog.File, names []string) error {
			return runUp(ctx, c, names, !localOnly, prune, resetNode)
		})
	up.Flags().BoolVar(&localOnly, "local", false, "start and probe only; skip VIP, Serve, advertise, and ACL")
	up.Flags().BoolVar(&prune, "prune", false, "delete VIP Services and Serve mappings absent from this catalog")
	up.Flags().BoolVar(&resetNode, "reset-node", false, "allow tailscale up --reset when requesting node tags")
	restart := named("restart", "Stop then start selected services, then republish on the tailnet",
		`When the local port is already open, restart stops the catalog-owned
process first (stop argv, detached pid, or compose down), then starts again.
Published services then get the same VIP, Serve, and advertise pass as apply
for those names only. Unpublished services are local-only.`,
		runRestart)
	root.AddCommand(
		up,
		named("stop", "Run catalog stop argv, or signal a process tscatalog detached",
			`A detached process is signaled as a group: SIGTERM, then SIGKILL after 10s.
A child that calls setsid leaves the group and is not signaled.`, runStop),
		restart,
		named("health", "Probe TCP + HTTP locally, HTTPS on the public URL, and host approval", "", runHealth),
		named("describe", "Print catalog services with VIP, Serve, approval, and public URL",
			"Arguments may be a catalog name, a vip label, or svc:<label>.", runDescribe),
	)

	ps := &cobra.Command{
		Use:   "ps [name ...]",
		Short: "Show processes and containers",
		Long: `A process row is a child this program detached (pid and exit from the reaper).
A container row is one Compose service.
A row with shape - is a start/stop helper, not a process or a container.`,
		ValidArgsFunction: completeNames,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runPS(cmd.Context(), cat, args)
		},
	}
	var follow bool
	var tail int
	logsCmd := &cobra.Command{
		Use:   "logs [service] [name]",
		Short: "Show output for a service",
		Long: `logs <service> follows every row of that service.
logs <service> <name> uses the NAME column from ps.
--tail -1 shows every line. A row with shape - runs its logs argv unchanged.`,
		ValidArgsFunction: completeNames,
		Args:              cobra.MaximumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runLogs(cmd, cat, args, tail, follow)
		},
	}
	logsCmd.Flags().BoolVarP(&follow, "follow", "f", false, "keep reading until interrupted")
	logsCmd.Flags().IntVar(&tail, "tail", 100, "lines to show (-1 is all)")
	root.AddCommand(ps, logsCmd, newAutostartCmd(func() *catalog.File { return cat }, func() string { return catPath }))
	return root
}

func newAutostartCmd(cat func() *catalog.File, catalogFile func() string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "autostart",
		Short: "Start services flagged autostart (local only)",
		Long: `Starts every catalog service with autostart: true, plus their requires,
the same way as up --local. No VIP, Serve, advertise, or ACL writes.

On macOS, install writes one login LaunchAgent that runs this command.
The agent is one-shot (no KeepAlive): stop leaves the process down until
the next up, restart, or login.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runAutostart(cmd.Context(), cat())
		},
	}
	cmd.AddCommand(&cobra.Command{
		Use:   "install",
		Short: "Install the macOS login agent (snapshots PATH and catalog path)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runAutostartInstall(catalogFile())
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "uninstall",
		Short: "Remove the macOS login agent",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return lifecycle.UninstallLoginAgent()
		},
	})
	return cmd
}

func runAutostart(ctx context.Context, cat *catalog.File) error {
	names := cat.AutostartNames()
	if len(names) == 0 {
		fmt.Fprintln(os.Stderr, "no services with autostart: true")
		return nil
	}
	return runUp(ctx, cat, names, false, false, false)
}

func runAutostartInstall(catalogFile string) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	logf, err := lifecycle.AutostartLogPath()
	if err != nil {
		return err
	}
	pathEnv := os.Getenv("PATH")
	if err := lifecycle.InstallLoginAgent(lifecycle.LoginAgent{
		Executable: exe,
		Catalog:    catalogFile,
		Path:       pathEnv,
		Log:        logf,
	}); err != nil {
		return err
	}
	fmt.Fprintf(style.Writer(os.Stdout), "installed %s (PATH and TSCATALOG_CATALOG snapshotted)\n", lifecycle.AgentLabel)
	return nil
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
	syncer.FprintPlan(style.Writer(os.Stdout), p)
	printURLs(ctx, cat)
	return nil
}

func runApply(ctx context.Context, cat *catalog.File, opts syncer.Options) error {
	if err := syncer.ApplyConfigured(ctx, cat, opts, style.Writer(os.Stdout)); err != nil {
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
	w := style.Writer(os.Stdout)
	fmt.Fprintf(w, "%-14s %-10s %-28s %s\n", "SERVICE", "API PORTS", "SERVE", "URL")
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
		serveCell := fmt.Sprintf("%-28s", serveT)
		if serveT == "-" {
			serveCell = style.Faint(serveCell)
		}
		fmt.Fprintf(
			w, "%s %s %s %s\n",
			style.Bold(fmt.Sprintf("%-14s", s.Name)),
			fmt.Sprintf("%-10s", apiPorts),
			serveCell,
			style.Faint(url),
		)
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
	comp, dialErr := lifecycle.DialCompose(ctx, svcs)

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
		printShow(ctx, style.Writer(os.Stdout), s, cat.Endpoint, byName[s.SVCName()], serve, suffix, approval, comp, dialErr)
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

func printShow(ctx context.Context, w io.Writer, s catalog.Service, endpoint string, api tailscale.Service, serve *catalog.ServeFile, suffix, approval string, comp *composeclient.Client, dialErr error) {
	fmt.Fprintln(w, style.Bold(s.Name))
	field(w, "svc", s.SVCName())
	if s.TailscaleLabel() != s.Name {
		field(w, "vip", s.TailscaleLabel())
	}
	if s.DisplayName != "" {
		field(w, "display", s.DisplayName)
	}
	if s.Comment != "" {
		field(w, "comment", s.Comment)
	}
	if len(s.Requires) > 0 {
		field(w, "requires", strings.Join(s.Requires, ", "))
	}
	if s.Unpublished {
		field(w, "publish", style.Faint("loopback only (unpublished)"))
	}
	if s.Autostart {
		field(w, "autostart", "login")
	}
	url := "https://" + s.TailscaleLabel() + ".<tailnet>.ts.net"
	if suffix != "" {
		url = "https://" + s.TailscaleLabel() + "." + suffix
	}
	field(w, "url", style.Faint(url))
	field(w, "catalog", endpoint+" → "+s.Local)

	if api.Name == "" {
		field(w, "api", style.Faint("(not defined)"))
	} else {
		ports := strings.Join(api.Ports, ",")
		if ports == "" {
			ports = "-"
		}
		addrs := strings.Join(api.Addrs, ", ")
		if addrs == "" {
			field(w, "api", "ports="+ports)
		} else {
			field(w, "api", fmt.Sprintf("ports=%s  addrs=%s", ports, addrs))
		}
		if len(api.Tags) > 0 {
			field(w, "tags", strings.Join(api.Tags, ", "))
		}
	}

	serveT := style.Faint("(not mapped)")
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
	field(w, "serve", serveT)
	if approval != "" {
		field(w, "host", style.Host(approval))
	}
	if table := lifecycle.FormatLaunchTable(lifecycle.LaunchStatuses(ctx, s, comp, dialErr)); table != "" {
		fmt.Fprint(w, table)
	}
}

func field(w io.Writer, key, value string) {
	fmt.Fprintf(w, "  %s%s\n", style.Faint(fmt.Sprintf("%-10s", key)), value)
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
	w := style.Writer(os.Stdout)
	suffix := magicSuffix(ctx)
	if suffix == "" {
		fmt.Fprintln(w, style.Faint("public: https://<name>.<tailnet>.ts.net"))
		return
	}
	fmt.Fprintln(w, style.Bold("public URLs"))
	for _, s := range cat.Services {
		if !s.Published() {
			continue
		}
		fmt.Fprintf(w, "  %s\n", style.Faint("https://"+s.TailscaleLabel()+"."+suffix))
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
	svcs, err := cat.ExpandForRuntime(names)
	if err != nil {
		return err
	}
	comp, err := lifecycle.DialCompose(ctx, svcs)
	if err != nil {
		return err
	}
	ctx = lifecycle.WithCompose(ctx, comp)
	var first error
	for _, s := range svcs {
		if err := lifecycle.Start(ctx, s, style.Writer(os.Stdout)); err != nil {
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
	comp, err := lifecycle.DialCompose(ctx, svcs)
	if err != nil {
		return err
	}
	ctx = lifecycle.WithCompose(ctx, comp)
	var first error
	for _, s := range svcs {
		if err := lifecycle.Stop(ctx, s, style.Writer(os.Stdout)); err != nil {
			fmt.Fprintln(os.Stderr, err)
			if first == nil {
				first = err
			}
		}
	}
	return first
}

func runUp(ctx context.Context, cat *catalog.File, names []string, publish, prune, resetNode bool) error {
	if err := runRuntime(ctx, cat, names); err != nil {
		return err
	}
	if publish {
		ids, err := catalogIDs(cat, names)
		if err != nil {
			return err
		}
		opts := syncer.Options{
			Tailnet: true, Host: true, Advertise: true, Policy: true, Names: ids,
			Prune: prune, ResetNode: resetNode,
		}
		if err := runApply(ctx, cat, opts); err != nil {
			return err
		}
	}
	return runHealth(ctx, cat, names)
}

func runRestart(ctx context.Context, cat *catalog.File, names []string) error {
	if len(names) == 0 {
		names = shortNames(cat)
	}
	var first error
	var restarted []string
	w := style.Writer(os.Stdout)
	for _, name := range names {
		if err := lifecycle.Restart(ctx, cat, name, w); err != nil {
			fmt.Fprintln(os.Stderr, err)
			if first == nil {
				first = err
			}
			continue
		}
		restarted = append(restarted, name)
	}
	if len(restarted) == 0 {
		return first
	}
	ids, err := catalogIDs(cat, syncer.PublishedNames(cat, restarted))
	if err != nil {
		return err
	}
	if len(ids) > 0 {
		if err := runApply(ctx, cat, syncer.Options{
			Tailnet: true, Host: true, Advertise: true, Names: ids,
		}); err != nil {
			fmt.Fprintln(os.Stderr, err)
			if first == nil {
				first = err
			}
		}
	}
	return first
}

func runHealth(ctx context.Context, cat *catalog.File, names []string) error {
	svcs, err := cat.ExpandForRuntime(names)
	if err != nil {
		return err
	}
	w := style.Writer(os.Stdout)
	fmt.Fprintf(w, "%-14s %-8s %-10s %-10s %s\n", "SERVICE", "BIND", "LOCAL", "PUBLIC", "HOST")
	var failed int
	for _, s := range svcs {
		host := "-"
		if s.Published() {
			if hosts, err := tsapi.ListHosts(ctx, s.SVCName()); err == nil {
				host = tsapi.ApprovalSummary(hosts)
			}
		} else {
			host = "local"
		}
		pub := publicBase(ctx, s.TailscaleLabel())
		if !s.Published() {
			pub = ""
		}
		r := lifecycle.Probe(ctx, s, pub, host)
		fmt.Fprintf(
			w, "%s %s %s %s %s\n",
			fmt.Sprintf("%-14s", r.Name),
			style.Probe(8, r.Bind, bindOK(r.Bind)),
			style.Probe(10, r.Local, lifecycle.ValueOK(r.Local, s.Check.LocalExpect())),
			style.Probe(10, r.Public, lifecycle.ValueOK(r.Public, s.Check.PublicExpect())),
			style.Host(r.Host),
		)
		if !r.OK {
			failed++
		}
	}
	if failed == 0 {
		return nil
	}
	return fmt.Errorf("%d service(s) unhealthy", failed)
}

func bindOK(value string) bool {
	return value != "" && value != "down" && value != "error"
}

func runPS(ctx context.Context, cat *catalog.File, names []string) error {
	svcs, err := selectServices(cat, names)
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(ctx, os.Interrupt)
	defer stop()
	comp, dialErr := lifecycle.DialCompose(ctx, svcs)
	w := style.Writer(os.Stdout)
	for _, s := range svcs {
		for _, spec := range s.Launch {
			for _, warning := range spec.Warnings() {
				fmt.Fprintf(w, "%s: %s\n", s.Name, warning)
			}
		}
	}
	var rows []lifecycle.LaunchStatus
	for _, s := range svcs {
		rows = append(rows, lifecycle.LaunchStatuses(ctx, s, comp, dialErr)...)
	}
	fmt.Fprint(w, lifecycle.FormatPS(rows))
	return nil
}

func runLogs(cmd *cobra.Command, cat *catalog.File, args []string, tail int, follow bool) error {
	if len(args) == 0 {
		return fmt.Errorf("logs needs a catalog service")
	}
	s, ok := cat.Lookup(args[0])
	if !ok {
		return fmt.Errorf("unknown service %q (catalog: %s)", args[0], strings.Join(shortNames(cat), ", "))
	}
	which := ""
	if len(args) > 1 {
		which = args[1]
	}
	ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt)
	defer stop()
	comp, err := lifecycle.DialCompose(ctx, []catalog.Service{s})
	if err != nil {
		return err
	}
	return lifecycle.Logs(ctx, s, which, tail, follow, cmd.Flags().Changed("tail"), os.Stdout, comp)
}
