# tscatalog

Named Tailscale Services as code. Copy `catalog.example.yaml` to `catalog.yaml`
(gitignored) and edit. The Go binary talks to the
[VIP Services API](https://pkg.go.dev/tailscale.com/client/tailscale/v2#ServicesResource)
(`tailscale.com/client/tailscale/v2`) and maps each service on this host with
`tailscale serve --service --https`.

Public URL is always `https://<name>.<tailnet>.ts.net` on **443**. Local ports
stay on loopback and never go in the Service definition.

```
go run ./cmd/tscatalog plan
go run ./cmd/tscatalog apply --dry-run
go run ./cmd/tscatalog apply
go run ./cmd/tscatalog status
go run ./cmd/tscatalog show
go run ./cmd/tscatalog show web
```

Control-plane auth: `TAILSCALE_API_KEY` in the environment, or in a gitignored
`.env` next to the catalog file (see `.env.example`). OAuth client id/secret
also works. Tailnet defaults to `-`. This repo does not store a tailnet name.

`apply` does not rewrite the tailnet ACL. `plan` prints an `autoApprovers` +
`grants` snippet to paste if a new service 403s.
