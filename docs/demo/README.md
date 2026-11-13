# Recordings

The GIFs in the [README](../../README.md) are `list`, `describe`, `ps`, and `health`, then cat9s, on [fixture/demo.yaml](fixture/demo.yaml). Every service in that file is `unpublished`, so `health` shows public `skip`. `plan` and `up` are not recorded.

The tapes put [tailscale](tailscale) on `PATH` ahead of the real CLI. That stub returns an empty MagicDNS suffix and does not talk to tailscaled, so `list` and `describe` print `https://<label>.<tailnet>.ts.net` instead of this machine's name.

Regenerate both from the repo root:

```
docs/demo/record
```

One argument regenerates one GIF: `docs/demo/record tscatalog` or `docs/demo/record cat9s`.

`record` builds `tscatalog` and `cat9s` into `docs/demo/bin/`. It writes the live MagicDNS suffix to a temp file, then puts [redact.py](redact.py) on `PATH` as `bash`. VHS runs that bash. The suffix is removed from the terminal text before a frame is drawn. The tapes refuse to start unless that filter is in place. Running `vhs` on a tape directly skips it.
