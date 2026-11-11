# Health

`tscatalog health` prints one row per selected service, after expanding `requires`. Exit 0 when every row passes. Otherwise it exits 1 with `<n> service(s) unhealthy`.

```
SERVICE        BIND     LOCAL      PUBLIC     HOST
```

This catalog, with nothing listening and no API credential:

```yaml
services:
  - name: web
    local: http://127.0.0.1:59991
    check:
      path: /health
      expect: [200]
  - name: side
    local: http://127.0.0.1:59992
    unpublished: true
    requires: [web]
```

prints:

```
SERVICE        BIND     LOCAL      PUBLIC     HOST
web            down     refused    unknown    -
side           down     down       skip       local
```

`health side` prints both rows, because `side` requires `web`. `health web` prints only `web`.

## Bind

The cell is `:<port>` when TCP accepts, `down` when it does not, and `error` when `local` cannot be parsed. The dial budget is one second.

## Local

No `check.path` and no `check.expect`: `tcp` when the port is open, `down` when it is not. That is `side` above.

A `path` with no `expect` accepts 200 only. `path` defaults to `/` once an expect list exists, and a path without a leading slash gets one.

`expect` is the local status list. A closed port with a path set is `refused`, not `down`. That is `web` above.

`timeout` is a deadline (two seconds on the local request). `error` is any other HTTP failure. A number is the status code. `502` is that code. It fails unless it is listed in `expect`.

## Public

Unpublished rows skip the request. The cell is `skip`. The host cell is `local`.

Published rows GET `https://<label>.<tailnet>.ts.net` plus the check path. The budget is eight seconds. Redirects are not followed. `check.public` is the accepted list. When it is omitted the list is `200`, `204`, `301`, `302`, `303`, `307`, `308`.

No public URL, or an approval lookup that did not return a host list, is `unknown` and the row fails. Without an API credential the lookup does not run, so a published row is `unknown` and `-`, as `web` is above.

`not-approved` fails the row even when the public request does not run.

## Host

`-` when this command did not get a host list. Otherwise `approval / configured`, several hosts joined with `; `, or `(no host advertised)`. `local` is an unpublished row.
