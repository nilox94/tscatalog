# tscatalog

Named Tailscale Services as code. Copy `catalog.example.yaml` to `catalog.yaml`
(gitignored) and edit. The Go binary talks to the
[VIP Services API](https://pkg.go.dev/tailscale.com/client/tailscale/v2#ServicesResource)
(`tailscale.com/client/tailscale/v2`) and maps each service on this host with
`tailscale serve --service --https`.

Public URL is always `https://<name>.<tailnet>.ts.net` on **443**. Local ports
stay on loopback and never go in the Service definition.

```
go run ./cmd/tscatalog up                # start + publish + check
go run ./cmd/tscatalog check
go run ./cmd/tscatalog start open-design
go run ./cmd/tscatalog stop open-design
go run ./cmd/tscatalog plan
go run ./cmd/tscatalog apply --dry-run
go run ./cmd/tscatalog apply
go run ./cmd/tscatalog status
go run ./cmd/tscatalog show web
```

`start` / `stop` / `check` / `up` read optional `launch` and `check` on each
catalog row. `launch.start` is argv (not a shell). A helper that returns when
ready (`my-web start`, a LaunchAgent installer) stays in the
foreground. Long-running processes (`npm run dev`) set `detach: true`; logs go
to `~/Library/Logs/tscatalog/` and pidfiles to
`~/Library/Application Support/tscatalog/run/`. Several specs (a list) start
in order — UI + API on one named URL.

`check` probes the local bind, optional local HTTP (`check.path` / `expect`),
the public `https://<name>.<tailnet>.ts.net` URL (`check.public`, default
200/3xx), and Tailscale host approval. 502 means Serve is up and the process is
down. Timeout usually means the host is not approved.

`up` is start → apply → check. `--no-publish` skips apply.

Control-plane auth: `TAILSCALE_API_KEY` in the environment, or in a gitignored
`.env` next to the catalog file (see `.env.example`). OAuth client id/secret
also works. Tailnet defaults to `-`. This repo does not store a tailnet name.

`apply` also merges the tailnet ACL: it fills `tagOwners` for `autoApproveHost`,
stamps that tag on each VIP Service, and sets
`autoApprovers.services["<that tag>"]` so tagged hosts are approved without a
console click (`svc:*` is not a valid autoApprovers key). A 443 grant is added
only if there is no catch-all `*:*` grant. Existing SSH, Funnel, routes, and
other tags are left alone. `--no-policy` skips this; `--policy-only` applies
just the ACL.

The JSON ACL API may drop HuJSON comments on save. Dry-run first:
`go run ./cmd/tscatalog apply --dry-run`.

Pending host advertisements (`approvalLevel: not-approved`) do not route TailVIPs.
`apply` merges policy *before* `tailscale serve advertise`, and drain/re-advertises
after an ACL change so auto-approval can attach. `svc:*` is not a valid
`autoApprovers.services` key; services are tagged with `autoApproveHost` instead.
