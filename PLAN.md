# CLI refactor plan

Catalog services stay the noun. `plan` / `apply` stay the tailnet reconciler. Local processes are a phase of `up`, not their own command. The interactive browser is the separate `cat9s` binary.

Defaults below are what implementation should follow unless you override the open decisions at the end.

## Direction

Keep two layers. Do not invent a third verb (`sync`, `publish`, `ensure`).

| Layer | Question it answers | Commands |
| --- | --- | --- |
| Service | What is this catalog entry, is it alive, make it so | `list`, `describe`, `health`, `up`, `stop` |
| Tailnet | Does VIP + Serve + ACL match the catalog | `plan`, `apply` |

`plan` and `apply` stay at the root. They are already the Terraform-shaped pair, and they are not scoped to one catalog name today. Nesting them under `sync` adds a word Tailscale already uses for something else and hides the commands you wanted to keep.

`start` goes away. Bringing a process up is the first phase of `up`. Runtime-only work is `up --local`. No compatibility aliases.

`check`, `show`, and `status` collapse:

| Now | Becomes | Why |
| --- | --- | --- |
| `status` | `list` | Catalog table. No probes. |
| `show` | `describe` | One service's catalog row plus live VIP, Serve, and approval. Avoids colliding with Terraform `show`. |
| `check` | `health` | Active probes. Read-only. Non-zero exit. |
| `start` | removed | Covered by `up`. |
| `up` | `up` | Same pipeline, scoped and quieter. |
| `stop` | `stop` | Unchanged role. |
| `plan`, `apply` | unchanged names | Still catalog-wide reads; writes gain an optional name filter. |

`health --repair` goes away. Repairing a dead process is `up`. A second mutator on a read command recreates `start`.

`cat9s` is the browser over those nouns. `r` / `R` restart a local process; they do not call `apply`.

## What is wrong today

`up web` does not mean "web".

- `runStart` and `runCheck` honor the names.
- `runApply` always reconciles every catalog service, then `Apply` runs `tailscale serve advertise` for every service even when the host row is `unchanged`.

So a repeated `up` is safe but not cheap, and a named `up` still mutates siblings.

`NeedsApply` does not know about advertise. There is no observed "already advertised" bit in `ComputePlan`. Skipping advertise blindly is wrong after an ACL change: policy is applied first, then each service is drained and re-served so auto-approval can attach. That path must stay when policy actually changes.

`LocalReady` means "something is listening on `local`", not "the catalog pid is running". A repeated `up` will not restart a wedged process that still holds the port. That is acceptable for this refactor; say so in `up` help. Do not pretend pid-level convergence.

`File.Lookup` matches catalog `name` and a leading `svc:` on that name. It does not match `vip:` or `svc:<vip>`. `describe` prints `svc:` as the title while `list` prints `name`. Those are three identifiers:

- **name** — catalog key, CLI argument, first-class id
- **vip** — single DNS label when `name` contains dots
- **svc:** — Tailscale API name (`svc:` + vip label)

Help text and errors should say "catalog name". Completion stays on `name`. Lookup should also accept `vip` and `svc:<label>` so copied API names work.

Policy cannot become per-service. `access:` lives on the file. `policy.FromCatalog` builds grant destinations from every service. Filtering the file before `Merge` would drop siblings from the ACL. Policy reads and writes stay catalog-wide.

`plan` does not include runtime or advertise. Leave it that way. `plan` is the tailnet diff. `health` is the probe. Mixing them makes both names muddy again.

## Target behavior

### `up [name ...]`

Default names: every catalog service. Phases, in order:

1. **Runtime** — `lifecycle.Start` for the selected services. Already listening → one line, no launch.
2. **Tailnet** — one `apply` of VIP, Serve, and advertise for the selected services, plus catalog-wide ACL.
3. **Health** — probe the selected services. Non-zero if any are unhealthy.

`--local` skips phase 2.

When every selected VIP and Serve row is `unchanged` and policy `NeedsApply()` is false:

- do not PUT, do not `serve`, do not drain
- do not advertise
- print `tailnet unchanged` instead of the full plan

When policy does need apply:

- merge ACL once (whole catalog)
- drain, re-serve, and advertise **every** catalog service, not only the names on the command line (approval is host-wide after an ACL write)
- still limit runtime and health to the names given

`apply` with no names keeps today's catalog-wide writes. `apply [name ...]` uses the same filter as phase 2. `plan` stays a full diff (no name filter in this refactor). Add `--dry-run` behavior unchanged.

`list` is today's `status` table. `describe` is today's `show`, titled by catalog `name`, with `svc:` on its own line. `health` is today's `check` without `--repair`.

### Decisions

Locked:

- Daily verb stays `up`. Runtime-only is `up --local`.
- Named `up` / `apply` limit VIP, Serve, and steady-state advertise to those catalog services. ACL stays catalog-wide. A policy write re-advertises every service.
- Commands stay flat.
- No aliases for `start`, `check`, `show`, `status`, or `--no-publish`.

## Implementation phases

### 1. Scope and no-op apply

In `internal/syncer` only, plus tests.

- Add an optional name set to `Options` (catalog `name`, not `svc:`). Empty means all services.
- VIP PUT, Serve, and the steady-state advertise loop honor the set.
- Policy plan and merge always use the full catalog.
- If policy was written, drain + re-serve + advertise all services (ignore the name set for that follow-up only).
- If nothing in VIP, Serve, or policy needs apply, skip advertise and say `tailnet unchanged`.
- Do not try to detect "already advertised" until `tailscale serve` exposes it. Unchanged plan is the gate.

Tests in `sync_test.go`: name filter does not PUT or Serve siblings; policy change still advertises siblings; all-unchanged performs no host or API writes.

### 2. Identity

In `internal/catalog`.

- `Lookup` resolves `name`, `vip`, and `svc:` + either label.
- Ambiguous matches (two rows, same vip) are a validate error if not already rejected.
- Unknown-name errors keep listing catalog names.

### 3. Command tree

In `cmd/tscatalog/main.go` and `README.md`.

- Add `list`, `describe`, `health`, and `--local` on `up`.
- Wire `up` and named `apply` through the phase-1 filter. `up` still runs runtime then apply then health.
- Delete `start`. No aliases.
- Drop `health --repair`.
- `describe` heading is the catalog name.
- Fix `up` / `apply` help so name scope and the policy exception are explicit.
- Glossary line in the README: name vs vip vs `svc:`.
- No TUI, no JSON, no new package split. `main.go` may stay the printer; moving formatters is a later cleanup.

### 4. Later (not this refactor)

- `cat9s` — shipped. Bubble Tea browser: failures first, `/` filter, `r` / `R` restart. Still no `apply` key.
- `logs` — tail XDG state files for detached launches.
- Advertise as a real plan row, once it is observable.
- Pid-aware restart when the listener is up but the catalog process is dead.
## Out of scope

- Changing catalog YAML.
- Changing ACL merge rules, grant behavior, or drain-on-policy-change order.
- Replacing cobra or fang.
- Making `plan` exit non-zero when drift exists (worth a later `--exit-code`; not part of the rename).

