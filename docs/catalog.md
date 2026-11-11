# Catalog

The file `tscatalog` and `cat9s` load. Discovery and a minimal example are in the [README](../README.md). This page is the schema.

`endpoint` defaults to `tcp:443`. `443` is accepted and stored as `tcp:443`.

`access.src` is the grant source. Empty becomes `autogroup:member` when the ACL is planned. `access.autoApproveHost` is a tag placed on this machine and on each published VIP Service (`tag:server`). `access.tagOwners` is who may apply that tag. Empty becomes `autogroup:admin` when the tag is set. See [Tailnet](tailnet.md).

## Service

| Field | |
| --- | --- |
| `name` | Dot-separated DNS labels: lowercase, digits, hyphen. `Web` is rejected. |
| `vip` | One DNS label. Default is `name`, or `name` with each dot replaced by a hyphen. |
| `displayName`, `comment` | Shown by `describe`. Not sent as the DNS name. |
| `local` | `http` or `https`, host `127.0.0.1`, `localhost`, or `::1`, and a port. |
| `requires` | Other catalog names. `up`, `restart`, `health`, and `autostart` start or probe them first. `stop` does not stop them. |
| `unpublished` | Launch and health only. No VIP, Serve, advertise, or ACL grant. `list` still prints the MagicDNS name. |
| `autostart` | Included in `tscatalog autostart`. Requires `launch`. |
| `launch` | One mapping, or a list. Specs start in order. |
| `check` | Optional HTTP probe. See [Health](health.md). |

A command argument is the catalog `name`, the vip label, or `svc:` plus either. `svc:abc.xyz` and `svc:abc-xyz` both select a service named `abc.xyz`.

```yaml
services:
  - name: abc.xyz
    displayName: Dotted
    local: http://127.0.0.1:8080
    launch:
      start: ["true"]
    check:
      path: /health
      expect: [200]
      public: [200, 401]
  - name: api
    local: http://127.0.0.1:8081
    unpublished: true
    requires: [abc.xyz]
    autostart: true
    launch:
      - name: api
        start: ["true"]
      - kind: compose
        name: stack
        cwd: ~/src/stack
        files: [compose.yaml]
        services: [web]
```

`files` are relative to that spec's `cwd`, not the directory you ran `tscatalog` from. `~/` in `cwd` is the home directory.

## Launch

`start`, `stop`, `status`, and `logs` are argv, not a shell. `kind` may be omitted: a strict `docker compose up` argv is compose, `detach: true` is a process, and anything else is an external helper.

### Process

`detach: true`, or `kind: process`. `start` is required. `services`, `files`, `status`, and `logs` are rejected.

```yaml
launch:
  cwd: ~/src/api
  start: [api, serve, --bind, 127.0.0.1:8081]
  detach: true
```

The pid file and log are under `$XDG_STATE_HOME/tscatalog` or `~/.local/state/tscatalog` (`run/`, `logs/`). `stop` signals the process group: `SIGTERM`, then `SIGKILL` after 10 seconds. A child that calls `setsid` leaves the group and is not signaled.

### External

A helper that returns when ready. `start` is required. `detach` is rejected. Optional `stop`, `status`, and `logs`.

```yaml
launch:
  cwd: ~/src/web
  start: [my-web, start]
  stop: [my-web, stop]
  status: [my-web, status]
  logs: [my-web, logs]
```

Without `status`, `ps` prints `no status command; ps cannot tell if it is running`. Without `logs`, it prints `no logs command`. `up` may still run `start`. cat9s will not restart a live port this catalog does not own.

### Compose

`kind: compose`. `cwd`, `files`, `services`, and `name` are the project. `start`, `stop`, `status`, `logs`, and `detach` are rejected (`launch[0]: kind compose does not use detach`).

```yaml
launch:
  kind: compose
  name: stack
  cwd: ~/src/stack
  files: [compose.yaml]
  services: [web]
```

An empty `files` list discovers `compose.yaml`, `compose.yml`, `docker-compose.yml`, and `docker-compose.yaml` in `cwd`. The same discovery walks a parent directory, which is compose-go's behavior. `services` limits `up` and `down` to those Compose services. An empty list is the whole project.

This argv is also compose. `-d` does not change that. `-f` and `-p` / `--project-name` are read from the argv, and a relative `-f` is still joined to `cwd`:

```yaml
launch:
  cwd: ~/src/stack
  start: [docker, compose, -f, compose.yaml, up, -d, web]
```

`ps` prints one row per container. `describe`'s `KIND` column is the runtime shape, not `launch.kind`: `process`, `container`, or `external`. A compose spec that fails to load prints `external` there. `ps` prints shape `-`, state `gone`, and the load error.

## ps

```
SERVICE          NAME               SHAPE     STATE      PID      DETAIL
```

A process row is a child this program detached. A container row is one Compose service. Shape `-` is a start/stop helper, not a process or a container. `logs <service> <name>` uses the NAME column.

## autostart

`tscatalog autostart` is `up --local` for every `autostart: true` service, plus their `requires`. No VIP, Serve, advertise, or ACL writes.

On macOS, `autostart install` writes one login LaunchAgent, `com.tscatalog.autostart` (`RunAtLoad`, no `KeepAlive`). It snapshots `PATH` and the catalog path. `go run` is refused. `stop` stays down until the next `up`, `restart`, or login. `autostart uninstall` removes the agent.
