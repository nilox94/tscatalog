# Tailnet

`plan` and `apply` need `TS_API_KEY` or an OAuth client id and secret. Without either, both exit 1 with `Set TS_API_KEY (or OAuth client id/secret).` That includes `apply --dry-run`.

`list`, `describe`, `health`, `ps`, and `logs` run without a credential. `list` prints `note: set TS_API_KEY (or OAuth client id/secret)`. `describe` prints `note: live overlay unavailable`. Both still print a MagicDNS URL from the local Tailscale daemon. How to create the token is in the [README](../README.md#auth).

## Plan

Full catalog diff of VIP Services, local Serve, and the tailnet ACL. No local processes and no health probes.

## Apply

Names limit VIP puts, Serve mappings, and steady-state advertise to those catalog services. The ACL merge is always the whole catalog. After an ACL write, every service is drained, re-served, and advertised so auto-approval can attach.

No names means every catalog service. When the selected rows and the ACL are already in sync, apply prints `tailnet unchanged` and does not advertise.

`--prune` deletes VIP Services and Serve mappings whose `svc:` name is not in this catalog, and rewrites the catalog 443 grant. `tagOwners` stay a union. Without `--prune`, removing a row does not delete tailnet state.

`--reset-node` retries `tailscale up` with `--reset` when the node tag cannot be requested otherwise. That drops settings the command did not repeat.

`--tailnet-only` updates VIP Services and skips local Serve. `--host-only` updates local Serve only. `--policy-only` merges the ACL only. `--no-policy` skips the ACL merge. `--dry-run` prints the plan and exits.

ACL merge: `tagOwners` for `autoApproveHost`, that tag on each VIP Service, `autoApprovers.services["<tag>"]` (`svc:*` is invalid). The 443 grant is written only if the policy has no `*:*`. An empty `access.src` becomes `autogroup:member`, and `plan` shows that default on the grant line before a write. Leaves SSH, Funnel, routes, other tags, and comments outside the edited values.

`tagOwners` is a union. Taking an owner out of the catalog does not remove them from the tailnet.

Without `--prune`, deleting a catalog row does not delete its VIP Service, Serve mapping, or grant destination. `--prune` deletes VIP Services and Serve mappings whose `svc:` name is absent from this catalog, and rewrites the grant whose destinations are all `svc:` names on port 443.

Apply requests `access.autoApproveHost` on this machine (`tailscale up --advertise-tags`) before Serve. If Tailscale refuses until every non-default flag is repeated, apply runs the `tailscale up` command Tailscale printed. `--reset-node` is the only path that retries with `--reset`, and that drops settings the command did not repeat. VIP hosts must be tagged nodes; `access.tagOwners` must allow whoever runs apply. If the tag stays pending, approve the device in the admin console.

Policy is written as HuJSON with the etag. `restart` and cat9s republish VIP, Serve, and advertise, and do not write the ACL. After an ACL change: drain and re-advertise so auto-approval can attach. `not-approved` ads do not route TailVIPs.

## Up

`up` is launch, then apply, then health. Names limit launch, VIP puts, Serve, steady-state advertise, and probes. The ACL merge is always the whole catalog. A policy write drains and advertises every service. `--local` skips apply. `--prune` and `--reset-node` are passed through to apply. Already listening on `local` does not relaunch that process.

## Restart

When the local port is already open, `restart` stops the catalog-owned process first (stop argv, detached pid, or compose down), then starts again. Published names then get the same VIP, Serve, and advertise pass as apply, for those names only. Unpublished names are local-only. `restart` does not write the ACL. `requires` are started locally and are not published. `stop` does not follow `requires`.
