# tscatalog

One catalog file starts local processes and publishes each one at `https://<label>.<tailnet>.ts.net` on port 443.

## Result

| Piece | What it is |
| --- | --- |
| Local process | A command or a Compose stack. `local` stays on loopback. |
| VIP Service | The Tailscale name for that process, `svc:<label>`. |
| Serve | The proxy on this machine from the HTTPS name to `local`. |
| ACL grant | The tailnet rule that lets `access.src` open the name. |

The label is the `vip` field. When `vip` is empty, the label is the service name, with dots replaced by hyphens. The tailnet name is not stored in the catalog.

This machine must carry `access.autoApproveHost`. The example uses `tag:server`. A device logged in only as a user cannot host a VIP Service. `up` and `apply` request the tag with `tailscale up --advertise-tags`. `access.tagOwners` must include whoever runs that command.

## Requirements

- Linux or macOS. Release binaries are amd64 and arm64.
- `tailscale` on `PATH`, and `tailscale status` works.
- Go 1.26, or a binary from the release.
- `TS_API_KEY` or an OAuth client before `plan`, `apply`, or `up`.

Windows is not a supported target. `detach: true` fails there with `launch detach is only supported on unix`. `autostart install` is macOS only.

## Install

### Go

```
go install github.com/nilox94/tscatalog/cmd/...@latest
```

That installs `tscatalog` and `cat9s` into `$(go env GOPATH)/bin`. From a checkout, `go install ./cmd/...` does the same.

### Release

[Latest release](https://github.com/nilox94/tscatalog/releases/latest). The assets are uncompressed binaries.

| | amd64 | arm64 |
| --- | --- | --- |
| Linux | `tscatalog-linux-amd64`, `cat9s-linux-amd64` | `tscatalog-linux-arm64`, `cat9s-linux-arm64` |
| macOS | `tscatalog-darwin-amd64`, `cat9s-darwin-amd64` | `tscatalog-darwin-arm64`, `cat9s-darwin-arm64` |

`chmod +x` both files and put them on `PATH`. Check `SHA256SUMS` on that release. Linux uses `sha256sum -c SHA256SUMS`. macOS uses `shasum -a 256 -c SHA256SUMS`.

The binaries are not notarized and have no Developer ID. macOS quarantines the download. If Gatekeeper blocks them:

```
xattr -d com.apple.quarantine tscatalog cat9s
```

## First run

From a clone of this repo:

```
mkdir -p ~/.config/tscatalog
cp catalog.example.yaml ~/.config/tscatalog/catalog.yaml
```

`./catalog.yaml` in the current directory is also found. Replace the placeholder commands before `up`. Those commands are not programs in this repo.

With no file, the command exits 1:

```
no catalog found: use -f, set TSCATALOG_CATALOG, or create <path>
```

`<path>` is `$XDG_CONFIG_HOME/tscatalog/catalog.yaml`, or `~/.config/tscatalog/catalog.yaml` when `XDG_CONFIG_HOME` is unset.

Discovery when `-f` is omitted:

1. `TSCATALOG_CATALOG`, even when that file is missing.
2. The XDG path above, if the file exists.
3. `./catalog.yaml`, if the file exists.

Then:

1. `tscatalog plan` diffs VIP Services, local Serve, and the ACL. It does not start processes and it does not write.
2. `tscatalog up` starts processes, writes the tailnet, then probes.

`up` starts processes before it checks the credential. A missing credential still starts them, then apply exits 1. Run `plan` first. `up --local` starts and probes without writing the tailnet. A published row still fails its public probe without a credential.

`plan` output for a new service is in [docs/tailnet.md](docs/tailnet.md#plan).

## Auth

An API access token or an OAuth client. Put it in the environment, or in a gitignored `.env` next to the catalog. See [`.env.example`](.env.example). Process environment wins. Only `TS_*` keys are loaded from that file.

`start`, `stop`, and a detached process do not inherit the file. Every `TS_*` variable is removed from their environment. `status` and `logs` still see the process environment. Compose can interpolate `TS_*`. A container receives one only if its Compose file passes it through.

| Variable | |
| --- | --- |
| `TS_API_KEY` | An [API access token](https://tailscale.com/docs/reference/key-secret-management) (`tskey-api-…`). Same token as [Tailscale API authentication](https://tailscale.com/docs/reference/tailscale-api#authentication). Not a device auth key. |
| `TS_OAUTH_CLIENT_ID` and `TS_OAUTH_CLIENT_SECRET` | An [OAuth client](https://tailscale.com/docs/features/oauth-clients). Scope `all`. Tailscale has no narrower scope for VIP Services. |
| `TS_TAILNET` | Optional. Default `-`, the credential's tailnet. |

`plan` and `apply` exit 1 without a credential, including `apply --dry-run`. The error is:

```
set TS_API_KEY (or OAuth client id/secret)
```

If only one of the OAuth variables is set, the error is `set both TS_OAUTH_CLIENT_ID and TS_OAUTH_CLIENT_SECRET`.

`list`, `describe`, `health`, `ps`, `logs`, `stop`, `autostart`, and `up --local` do not require one.

`list` still calls the API and `tailscale serve get-config`. On failure it prints `note:` and the error, then continues. That error is `set TS_API_KEY (or OAuth client id/secret)` only when a missing credential is the failure. A missing `tailscale` binary is included in the same note.

`describe` prints:

```
note: live overlay unavailable: <error>
```

Public URLs come from `tailscale status --json`. When that works, the host is the MagicDNS suffix. When it fails, the URL stays `https://<label>.<tailnet>.ts.net`.

## Catalog file

A service is a command or a Compose stack. One file can include both. A command is an argv (`start`, `stop`), not a shell string. A Compose stack is `kind: compose` plus the project directory.

The commands and directories below are placeholders. `my-web` is not included. `~/src/edge` is not a project in this repo.

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

  - name: edge
    local: http://127.0.0.1:8083
    launch:
      kind: compose
      cwd: ~/src/edge
      files: [compose.yaml]
      services: [proxy]
```

A command argument is a catalog name, a vip label, or `svc:` plus either of those. `plan`, `list`, and `autostart` take no names. `logs` takes a service and an optional NAME from `ps`.

Fields, Compose, `requires`, and autostart: [docs/catalog.md](docs/catalog.md). The file to copy is [`catalog.example.yaml`](catalog.example.yaml).

## Commands

| Command | Credential | What |
| --- | --- | --- |
| `plan` | yes | [Diff](docs/tailnet.md#plan) VIP Services, local Serve, and the ACL. No processes, no probes, no writes. |
| `apply` | yes | [Write](docs/tailnet.md#apply) VIP Services, Serve, advertisements, and the ACL. |
| `up` | yes, unless `--local` | Start, then apply, then [health](docs/health.md). Start runs before the credential check. `--local` skips the tailnet. |
| `restart` | for the republish | Stop, then start, then republish those names. No ACL write. Stop and start run before the credential check. |
| `health` | no | [Probe](docs/health.md) bind, local HTTP, public HTTPS, and host approval. |
| `list` | no | Names, API ports, Serve targets, and the public URL. |
| `describe` | no | Every service when no name is given. One service when a name is given, plus its launch rows. |
| `ps` | no | Processes and containers. |
| `logs` | no | `logs <service>` or `logs <service> <name>`. Last 100 lines for a process or container. `--tail` sets the count (`-1` is all). `--follow` keeps reading. `-f` is the catalog file. Shape `-` runs its logs argv unchanged. |
| `stop` | no | Stop argv, or `SIGTERM` then `SIGKILL` after 10s for a detached group. Does not follow `requires`. |
| `autostart` | no | `up --local` for `autostart: true`, plus `requires`. [`install` / `uninstall`](docs/catalog.md#autostart) are macOS. On Linux, run the same command from a user service. |
| `completion` | no | Shell completion script. |

`apply` flags (`--dry-run`, `--tailnet-only`, `--host-only`, `--policy-only`, `--no-policy`, `--prune`, `--reset-node`) are in [docs/tailnet.md](docs/tailnet.md#apply). `up` passes `--prune` and `--reset-node` through.

`cat9s` is not a `tscatalog` command. It is the other binary.

The recording below is `list`, `describe`, `ps`, and `health` on [`docs/demo/fixture/demo.yaml`](docs/demo/fixture/demo.yaml). Every service in that file is `unpublished`, so the picture has no public URL. `plan` and `up` are not in it.

![tscatalog list, describe, ps, and health on an unpublished catalog](docs/demo/tscatalog.gif)

## cat9s

`cat9s` is a separate program. It is a terminal UI for the same catalog. It needs a terminal. `tscatalog cat9s` fails as an unknown command.

A published restart needs a credential. Browsing does not. A published restart republishes VIP, Serve, and advertise. cat9s does not write the ACL.

The same unpublished fixture as the other recording, so a restart in this picture cannot publish.

![cat9s on an unpublished catalog](docs/demo/cat9s.gif)

| Keys | |
| --- | --- |
| List | `j` / `k` move. `r` restart the selected row. `R` restart every visible row that needs it. `x` stop. `l` logs. `/` filter. Enter describe. `q` quit. Esc closes the error panel, or quits when there is no error and no filter. |
| Detail | Esc back. `q` quit. |
| Logs | `f` follow. `/` search. Tab next name. `j` / `k` scroll. Esc back. |

Published rows that are down or unhealthy are listed first. Restart starts a dead process. If the port is open, it stops and starts only when this catalog launched it. That is when the local check failed, or when the local check passed and the overall probe failed. It skips a port something else holds. It skips `not-approved` and `draining`. `requires` start first and are not published.

## Words

| Word | Meaning |
| --- | --- |
| Service | One catalog row. A local address and a public Tailscale name. |
| Launch | How that row starts. A process, a Compose project, or an external helper. |
| Process | A program this tool detaches and later stops. |
| External | A helper this tool runs and then leaves alone. Something else owns the listener. |

The same list, including words this repo does not use, is [CONTEXT.md](CONTEXT.md).

## More

| Page | |
| --- | --- |
| [docs/catalog.md](docs/catalog.md) | Fields, launch, Compose, logs, autostart. |
| [docs/tailnet.md](docs/tailnet.md) | `plan`, `apply`, `up`, `restart`. |
| [docs/health.md](docs/health.md) | Probe columns and a closed-port example. |
| [docs/adr/0001-login-agent-not-supervisor.md](docs/adr/0001-login-agent-not-supervisor.md) | Why autostart is one login agent. |
