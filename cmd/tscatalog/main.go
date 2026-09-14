package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"tailscale.com/client/tailscale/v2"

	"tscatalog/internal/catalog"
	"tscatalog/internal/host"
	"tscatalog/internal/syncer"
	"tscatalog/internal/tsapi"
)

func usage() {
	fmt.Fprintf(os.Stderr, `tscatalog — named Tailscale Services as code

Usage:
  tscatalog plan    [-f catalog.yaml]
  tscatalog apply   [-f catalog.yaml] [--dry-run] [--tailnet-only] [--host-only]
  tscatalog status  [-f catalog.yaml]
  tscatalog show    [-f catalog.yaml] [name ...]

plan     Diff the catalog against the VIP Services API and local Serve config.
apply    PUT missing/changed VIP Services, then tailscale serve --https for
         each new host mapping and advertise each svc:*. --dry-run is plan + no writes.
status   List catalog rows with current API ports and Serve targets.
show     Print one service (or all) with catalog, VIP, Serve, and public URL.

Auth (control plane), loaded from .env next to the catalog if unset:
  TAILSCALE_API_KEY                  access token with Services write
  TAILSCALE_OAUTH_CLIENT_ID/SECRET   alternative to an API key
  TAILSCALE_TAILNET                  default "-" (the credential's tailnet)

The tailnet name is not stored in the catalog. Public URLs are
https://<service>.<tailnet>.ts.net — always port 443.
`)
}

func main() {
	if len(os.Args) < 2 || os.Args[1] == "-h" || os.Args[1] == "--help" {
		usage()
		if len(os.Args) < 2 {
			os.Exit(2)
		}
		return
	}
	cmd := os.Args[1]
	fs := flag.NewFlagSet(cmd, flag.ExitOnError)
	file := fs.String("f", "catalog.yaml", "catalog file")
	dry := fs.Bool("dry-run", false, "print the plan and exit (apply)")
	tailnetOnly := fs.Bool("tailnet-only", false, "only update VIP Services via the API")
	hostOnly := fs.Bool("host-only", false, "only update local Serve mappings")
	fs.Usage = usage
	if err := fs.Parse(os.Args[2:]); err != nil {
		os.Exit(2)
	}

	catPath, err := filepath.Abs(*file)
	if err != nil {
		fatal(err)
	}
	if err := tsapi.LoadDotEnv(filepath.Join(filepath.Dir(catPath), ".env")); err != nil {
		fatal(err)
	}
	cat, err := catalog.Load(catPath)
	if err != nil {
		fatal(err)
	}

	ctx := context.Background()
	switch cmd {
	case "plan":
		if err := runPlan(ctx, cat); err != nil {
			fatal(err)
		}
	case "apply":
		opts := syncer.Options{
			DryRun:    *dry,
			Tailnet:   !*hostOnly,
			Host:      !*tailnetOnly,
			Advertise: !*tailnetOnly,
		}
		if err := runApply(ctx, cat, opts); err != nil {
			fatal(err)
		}
	case "status":
		if err := runStatus(ctx, cat); err != nil {
			fatal(err)
		}
	case "show":
		if err := runShow(ctx, cat, fs.Args()); err != nil {
			fatal(err)
		}
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n", cmd)
		usage()
		os.Exit(2)
	}
}

func runPlan(ctx context.Context, cat *catalog.File) error {
	api, serve, err := snapshot(ctx, true, true)
	if err != nil {
		return err
	}
	p := syncer.ComputePlan(cat, api, serve)
	syncer.FprintPlan(os.Stdout, p)
	printURLs(ctx, cat)
	printACLHint(cat)
	return nil
}

func runApply(ctx context.Context, cat *catalog.File, opts syncer.Options) error {
	var api syncer.API
	if opts.Tailnet {
		c, err := tsapi.NewClient()
		if err != nil {
			return err
		}
		api = syncer.ClientAPI{Services: c.Services()}
	}
	var h syncer.Host
	if opts.Host {
		h = host.CLI{}
	}
	if err := syncer.Apply(ctx, cat, api, h, opts, os.Stdout); err != nil {
		return err
	}
	if !opts.DryRun {
		printURLs(ctx, cat)
		printACLHint(cat)
	}
	return nil
}

func runStatus(ctx context.Context, cat *catalog.File) error {
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
		url := "https://" + s.Name + ".<tailnet>.ts.net"
		if suffix != "" {
			url = "https://" + s.Name + "." + suffix
		}
		fmt.Printf("%-14s %-10s %-28s %s\n", s.Name, apiPorts, serveT, url)
	}
	return nil
}

func runShow(ctx context.Context, cat *catalog.File, names []string) error {
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
		printShow(s, cat.Endpoint, byName[s.SVCName()], serve, suffix)
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

func printShow(s catalog.Service, endpoint string, api tailscale.Service, serve *catalog.ServeFile, suffix string) {
	fmt.Printf("%s\n", s.SVCName())
	if s.DisplayName != "" {
		fmt.Printf("  display   %s\n", s.DisplayName)
	}
	if s.Comment != "" {
		fmt.Printf("  comment   %s\n", s.Comment)
	}
	url := "https://" + s.Name + ".<tailnet>.ts.net"
	if suffix != "" {
		url = "https://" + s.Name + "." + suffix
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
		fmt.Printf("  https://%s.%s\n", s.Name, suffix)
	}
}

func magicSuffix(ctx context.Context) string {
	md, err := (host.CLI{}).MagicDNS(ctx)
	if err != nil {
		return ""
	}
	return strings.TrimSuffix(md.Suffix, ".")
}

func printACLHint(cat *catalog.File) {
	src := cat.Access.Src
	if len(src) == 0 {
		src = []string{"autogroup:member"}
	}
	dst := make([]string, 0, len(cat.Services))
	for _, s := range cat.Services {
		dst = append(dst, s.SVCName())
	}
	hint := map[string]any{
		"grants": []map[string]any{{
			"src": src,
			"dst": dst,
			"ip":  []string{"443"},
		}},
	}
	if hostTag := cat.Access.AutoApproveHost; hostTag != "" {
		hint["autoApprovers"] = map[string]any{
			"services": map[string][]string{"svc:*": {hostTag}},
		}
	}
	raw, err := json.MarshalIndent(hint, "", "  ")
	if err != nil {
		return
	}
	fmt.Println("policy (paste into the tailnet policy file if missing; not applied by this tool)")
	fmt.Println(string(raw))
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
