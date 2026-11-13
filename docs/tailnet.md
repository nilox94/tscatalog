# Tailnet

`plan` and `apply` need `TS_API_KEY`, or both `TS_OAUTH_CLIENT_ID` and `TS_OAUTH_CLIENT_SECRET`. Without a credential they exit 1, including `apply --dry-run`. The error is:

```
set TS_API_KEY (or OAuth client id/secret)
```

If only one of the OAuth variables is set, the error is `set both TS_OAUTH_CLIENT_ID and TS_OAUTH_CLIENT_SECRET`.

How to create the token is in the [README](../README.md#auth). What `list` and `describe` print without a credential is there too.

## Plan

`plan` diffs the whole catalog against VIP Services, local Serve, and the ACL. It does not start processes, probe health, or write.

This is a published `web` service at `http://127.0.0.1:8080`, with `access.src: [autogroup:member]`, `autoApproveHost: tag:server`, and `tagOwners: [autogroup:admin]`. Nothing of that exists on the tailnet yet:

```
tailnet (VIP Services API)
  create    svc:web             ports=tcp:443 tags=tag:server
host (tailscale serve)
  create    svc:web             tcp:443 → http://127.0.0.1:8080
policy (ACL)
  create    tagOwners.tag:server          owners=autogroup:admin
  create    autoApprovers.services        tag:server ← tag:server (service tag, not svc:*)
  create    grants                        autogroup:member → svc:web ip=443
```

After that, `plan` prints public URLs. When `tailscale status --json` returns a MagicDNS suffix, there is one `https://<label>.<suffix>` line per published service. When that call fails, the line is:

```
public: https://<label>.<tailnet>.ts.net
```

## Apply

`apply` writes that diff.

Names limit VIP puts, Serve mappings, and steady-state advertise to those catalog services. The ACL merge is always the whole catalog. After an ACL write, every published service is drained, re-served, and advertised so auto-approval can attach.

No names means every published service. When the selected rows and the ACL are already in sync, apply prints `tailnet unchanged` and does not advertise.

### Flags

| Flag | Effect |
| --- | --- |
| `--dry-run` | Print the plan and exit. |
| `--tailnet-only` | Update VIP Services. Skip local Serve. |
| `--host-only` | Update local Serve only. |
| `--policy-only` | Merge the ACL only. |
| `--no-policy` | Skip the ACL merge. |
| `--prune` | Delete VIP Services and Serve mappings whose `svc:` name is not in this catalog. Rewrite the grant whose destinations are all `svc:` names on port 443. |
| `--reset-node` | Retry `tailscale up` with `--reset` when the node tag cannot be requested otherwise. That drops settings the command did not repeat. |

`up` passes `--prune` and `--reset-node` through. The other flags are `apply` only.

Without `--prune`, deleting a catalog row does not delete its VIP Service, Serve mapping, or grant destination. `tagOwners` stay a union either way. Taking an owner out of the catalog does not remove them from the tailnet.

### ACL

The merge writes:

- `tagOwners` for `autoApproveHost`
- that tag on each VIP Service
- `autoApprovers.services["<tag>"]` (`svc:*` is invalid)
- a port 443 grant, only if the policy has no `*:*`

An empty `access.src` becomes `autogroup:member`. `plan` shows that default on the grant line before a write. An empty `tagOwners` becomes `autogroup:admin` when the tag is set.

SSH, Funnel, routes, other tags, and comments outside the edited values stay. Policy is written as HuJSON with the etag.

### Host tag

Apply requests `access.autoApproveHost` on this machine (`tailscale up --advertise-tags`) before Serve. VIP hosts must be tagged nodes. `access.tagOwners` must allow whoever runs apply. A user-authenticated device cannot host a VIP Service.

If Tailscale refuses until every non-default flag is repeated, apply runs the `tailscale up` command Tailscale printed. `--reset-node` is the only path that retries with `--reset`. If the tag stays pending, approve the device in the admin console.

`not-approved` ads do not route TailVIPs.

## Up

`up` is launch, then apply, then health. Names limit launch, VIP puts, Serve, steady-state advertise, and probes. The ACL merge is always the whole catalog. A policy write drains and advertises every published service.

`--local` skips apply. Already listening on `local` does not relaunch that process. Launch runs before the credential check.

## Restart

When the local port is already open, `restart` stops the catalog-owned process first (stop argv, detached pid, or compose down), then starts again. Published names then get the same VIP, Serve, and advertise pass as apply, for those names only. Unpublished names are local-only.

`restart` does not write the ACL. `requires` are started locally and are not published. `stop` does not follow `requires`.

`restart` and cat9s republish VIP, Serve, and advertise.
