# tscatalog

Named Tailscale Services as code. Copy `catalog.example.yaml` to your catalog file and edit.

Default catalog path: `$XDG_CONFIG_HOME/tscatalog/catalog.yaml` (or `~/.config/tscatalog/catalog.yaml`).
Resolution order without `-f`: `TSCATALOG_CATALOG` → XDG config → `./catalog.yaml` in the current directory.
Put a gitignored `.env` next to the catalog for API keys (see `.env.example`).
The Go binary talks to the [VIP Services API](https://pkg.go.dev/tailscale.com/client/tailscale/v2#ServicesResource) (`tailscale.com/client/tailscale/v2`) and maps each service on this host with `tailscale serve --service --https`.

Public URL is always `https://<tailscale-label>.<tailnet>.ts.net` on **443**.
Tailscale only accepts a single DNS label, so the VIP label defaults to dots → hyphens unless you set `vip:` explicitly.
Local ports stay on loopback and never go in the Service definition.

A catalog **name** is the key you pass to commands. **vip** is the single DNS label (default: dots in the name become hyphens). **svc:** is the Tailscale API name (`svc:` plus the vip label). Commands accept any of the three.

```
go run ./cmd/tscatalog up                # start + apply + health
go run ./cmd/tscatalog up web --local    # start + health, no tailnet writes
go run ./cmd/tscatalog health
go run ./cmd/tscatalog stop web
go run ./cmd/tscatalog plan
go run ./cmd/tscatalog apply --dry-run
go run ./cmd/tscatalog apply web
go run ./cmd/tscatalog list
go run ./cmd/tscatalog describe web
```

`up`, `stop`, and `health` read optional `launch` and `check` on each catalog row.
`launch.start` is argv (not a shell).
A helper that returns when ready (`my-web start`, a service-manager installer) stays in the foreground.
Long-running processes (`npm run dev`) set `detach: true` (Unix only): logs and pidfiles under `$XDG_STATE_HOME/tscatalog/` or `~/.local/state/tscatalog/` (`logs/` and `run/`). Without `detach`, launch is a helper that returns when the listener is up (service manager, container, etc.); `stop` runs that helper’s stop argv.
Several specs (a list) start in order — UI + API on one named URL.

`requires` lists other catalog names that must be running first. `up momoma` starts
those dependencies locally, then Momoma, but only publishes and probes the names you
gave (dependencies are not advertised). `stop momoma` stops only Momoma — shared
dependencies keep running.

`unpublished: true` keeps a row in the catalog for launch and health, but skips VIP,
Serve, advertise, and ACL grants (loopback-only observability stacks).

`health` probes the local bind, optional local HTTP (`check.path` / `expect`), the public `https://<vip>.<tailnet>.ts.net` URL (`check.public`, default 200/3xx), and Tailscale host approval.
502 means Serve is up and the process is down.
Timeout usually means the host is not approved.

`up` is launch → apply → health. Names limit launch, VIP puts, Serve, steady-state advertise, and probes. The ACL merge is always the whole catalog; after an ACL write, every service is drained and advertised. `--local` skips apply. A repeated `up` does not relaunch when something is already listening on `local`.

Control-plane auth: `TAILSCALE_API_KEY` in the environment, or in a gitignored `.env` next to the catalog file (see `.env.example`).
OAuth client id/secret also works.
Tailnet defaults to `-`. This repo does not store a tailnet name.

`apply` also merges the tailnet ACL:
it fills `tagOwners` for `autoApproveHost`, stamps that tag on each VIP Service, and sets `autoApprovers.services["<that tag>"]` so tagged hosts are approved without a console click (`svc:*` is not a valid autoApprovers key).
A 443 grant is added only if there is no catch-all `*:*` grant.
Existing SSH, Funnel, routes, and other tags are left alone. `--no-policy` skips this; `--policy-only` applies just the ACL.

The JSON ACL API may drop HuJSON comments on save. Dry-run first:
`go run ./cmd/tscatalog apply --dry-run`.

Pending host advertisements (`approvalLevel: not-approved`) do not route TailVIPs.
`apply` merges policy *before* `tailscale serve advertise`, and drain/re-advertises after an ACL change so auto-approval can attach.
`svc:*` is not a valid `autoApprovers.services` key; services are tagged with `autoApproveHost` instead.
