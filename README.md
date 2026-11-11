# tscatalog

Named Tailscale Services as code. One catalog file is the local processes and the Tailscale VIP Services, Serve mappings, and ACL grant.

## Install

Go 1.26.

```
go install github.com/nilox94/tscatalog/cmd/...@latest
```

That installs `tscatalog` and `cat9s`. From a checkout, `go install ./cmd/...` does the same.

A `v*` tag publishes `tscatalog-$GOOS-$GOARCH` and `cat9s-$GOOS-$GOARCH` for linux and darwin, amd64 and arm64.

![tscatalog list, describe, ps, and health](docs/demo/tscatalog.gif)

![cat9s](docs/demo/cat9s.gif)

## Auth

An API access token or an OAuth client, in the environment or in a gitignored `.env` next to the catalog. See [`.env.example`](.env.example). Process environment wins. Only `TS_*` keys are loaded. Launched processes do not inherit that file.

- `TS_API_KEY` — an [API access token](https://tailscale.com/docs/reference/key-secret-management) (`tskey-api-…`). Same token as [Tailscale API authentication](https://tailscale.com/docs/reference/tailscale-api#authentication). Not a device auth key.
- `TS_OAUTH_CLIENT_ID` and `TS_OAUTH_CLIENT_SECRET` — an [OAuth client](https://tailscale.com/docs/features/oauth-clients) with scope `all`.
- `TS_TAILNET` — optional. Default `-`, the credential's tailnet.

`plan` and `apply` refuse to run without one of those credentials. `list`, `describe`, `health`, `ps`, `logs`, `stop`, `autostart`, and `up --local` run without one. `list` then prints `note: set TS_API_KEY (or OAuth client id/secret)`. `describe` prints `note: live overlay unavailable`. Both still print the MagicDNS name.

## Catalog file

`-f` if set. Otherwise `TSCATALOG_CATALOG`, and that path is used even when the file is missing. Otherwise `$XDG_CONFIG_HOME/tscatalog/catalog.yaml`, or `~/.config/tscatalog/catalog.yaml` when `XDG_CONFIG_HOME` is unset, if that file exists. Otherwise `./catalog.yaml` if that file exists.

```yaml
endpoint: tcp:443
access:
  src: [autogroup:member]
  autoApproveHost: tag:server
  tagOwners: [autogroup:admin]
services:
  - name: web
    local: http://127.0.0.1:8080
    launch:
      start: [my-web, start]
      stop: [my-web, stop]
    check:
      path: /health
```

The public URL is `https://<label>.<tailnet>.ts.net` on port 443. The label is `vip`, or the service name with dots replaced by hyphens. The tailnet name is not stored in the catalog. `local` stays on loopback and is only the Serve backend.

Fields, Compose, and more snippets: [docs/catalog.md](docs/catalog.md). A longer copy is [`catalog.example.yaml`](catalog.example.yaml).

A command argument is a catalog name, a vip label, or `svc:` plus either of those. `plan`, `list`, and `autostart` take no names. `logs` takes a service and an optional NAME from `ps`.

## Commands

| Command | API key | What |
| --- | --- | --- |
| `plan` | yes | [Diff](docs/tailnet.md#plan) VIP Services, local Serve, and the ACL. No processes or probes. |
| `apply` | yes | [Reconcile](docs/tailnet.md#apply) VIP Services, Serve, advertisements, and the ACL. |
| `up` | yes, unless `--local` | Start, then apply, then [health](docs/health.md). `--local` skips the tailnet. |
| `restart` | yes, for a published name | Stop, start, then republish those names. No ACL write. |
| `health` | no | [Probe](docs/health.md) bind, local HTTP, public HTTPS, and host approval. |
| `list` | no | Names, API ports, Serve targets, and the public URL. |
| `describe` | no | One service, plus its launch rows. |
| `ps` | no | Processes and containers. |
| `logs` | no | `logs <service>` or `logs <service> <name>`. `--follow` keeps reading. `-f` is the catalog file. |
| `stop` | no | Stop argv, or `SIGTERM` then `SIGKILL` after 10s for a detached group. |
| `autostart` | no | `up --local` for `autostart: true`, plus `requires`. `install` / `uninstall` on macOS. |
| `completion` | no | Shell completion script. |
| `cat9s` | no, until a published restart | Browse and restart. Needs a terminal. |

`apply` also has `--dry-run`, `--tailnet-only`, `--host-only`, `--policy-only`, `--no-policy`, `--prune`, and `--reset-node`. `up` passes `--prune` and `--reset-node` through.

## cat9s

List: `j` / `k` move, `r` restart the selected row, `R` restart every visible row that needs it, `x` stop, `l` logs, `/` filter, Enter describe, `q` quit. Esc closes the error panel, or quits when there is no error and no filter.

Detail: Esc back, `q` quit.

Logs: `f` follow, `/` search, Tab next name, `j` / `k` scroll, Esc back.

Published rows that are down or unhealthy are listed first. Restart starts a dead process. If the port is open, it stops and starts only when this catalog launched it: the local check failed, or the local check passed and the overall probe failed. It skips a port something else holds, and it skips `not-approved` and `draining`. `requires` start first and are not published. A published restart republishes VIP, Serve, and advertise. cat9s does not write the ACL.
