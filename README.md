# tscatalog

Named Tailscale Services as code. Copy `catalog.example.yaml` and edit.

Catalog (`-f` unset): `TSCATALOG_CATALOG` → `$XDG_CONFIG_HOME/tscatalog/catalog.yaml` → `~/.config/tscatalog/catalog.yaml` → `./catalog.yaml`.
Auth: gitignored `.env` next to the catalog (`.env.example`). `TAILSCALE_API_KEY` or OAuth id/secret. Tailnet `-`. Not stored in the catalog.

VIP Services API + `tailscale serve --service --https`. Public URL is always `https://<label>.<tailnet>.ts.net` **443**. `vip:` is one DNS label (default: dots in `name` → hyphens). Local bind stays loopback; it is not in the Service.

IDs (any works as a command argument): **name** (catalog key) · **vip** (DNS label) · **svc:** (`svc:` + vip).

```
go run ./cmd/tscatalog up                # start + apply + health
go run ./cmd/tscatalog up web --local    # start + health
go run ./cmd/tscatalog health
go run ./cmd/tscatalog stop web
go run ./cmd/tscatalog restart web
go run ./cmd/tscatalog plan
go run ./cmd/tscatalog apply --dry-run
go run ./cmd/tscatalog apply web
go run ./cmd/tscatalog list
go run ./cmd/tscatalog describe web
go run ./cmd/tscatalog autostart
go install ./cmd/tscatalog && tscatalog autostart install
go run ./cmd/cat9s
```

## cat9s

Separate TUI. Needs a terminal. `r` / `R` restart republish published rows (VIP, Serve, advertise); no other ACL writes.

- failures first (published, down or unhealthy); restart count
- `/` filter · Enter describe · `x` stop that row · esc error banner
- `r` / `R`: start if down; stop+start if owned (stop argv or live detached pid) and local check fails; skip foreign port, `not-approved`, `draining`
- `requires` start first, not published

## launch

`up` / `stop` / `health` read optional `launch` and `check`. `start` is argv, not a shell. Several specs: start in order.

| kind | how |
| --- | --- |
| `detach: true` (Unix) | long-running; pidfile + log under `$XDG_STATE_HOME/tscatalog` or `~/.local/state/tscatalog` (`run/`, `logs/`) |
| no detach | helper that returns when ready; `stop` is its stop argv |
| no `stop` | another manager owns the listener; `up` may still run `start`; cat9s will not restart a live port |

`requires:` start locally first. `up app` publishes/probes only named rows. `stop app` does not stop shared deps.

`unpublished: true` — launch + health only; no VIP, Serve, advertise, ACL.

`autostart: true` — `tscatalog autostart` = `up --local` for those names (+ `requires`). macOS: `autostart install` writes `com.tscatalog.autostart` (`RunAtLoad`, no KeepAlive). Snapshots `PATH` and `TSCATALOG_CATALOG`. `go run` refused. `stop` stays down until `up` / restart / next login. `autostart uninstall` removes it.

## health / up

`health`: TCP bind · optional `check.path` / `expect` · public HTTPS (`check.public`, default 200/3xx) · host approval. 502 = Serve up, process down. Timeout ≈ host not approved.

`up`: launch → apply → health. Names limit launch, VIP, Serve, steady-state advertise, probes. ACL is always whole-catalog; a policy write drains and advertises every service. `--local` skips apply. Already listening on `local` → no relaunch.

## apply

ACL merge: `tagOwners` for `autoApproveHost`, that tag on each VIP Service, `autoApprovers.services["<tag>"]` (`svc:*` is invalid). 443 grant only if no `*:*`. Leaves SSH, Funnel, routes, other tags. `--no-policy` / `--policy-only`.

Apply requests `access.autoApproveHost` on this machine (`tailscale up --advertise-tags`) before Serve. VIP hosts must be tagged nodes; `access.tagOwners` must allow whoever runs apply. If the tag stays pending, approve the device in the admin console.

JSON ACL API may drop HuJSON comments — dry-run first. Policy merge before `tailscale serve advertise`. After ACL change: drain + re-advertise so auto-approval can attach. `not-approved` ads do not route TailVIPs.
