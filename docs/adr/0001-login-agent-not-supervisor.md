# One login agent; tscatalog owns processes

Autostart is a catalog intent, not a per-service LaunchAgent. macOS gets a single `RunAtLoad` agent that runs `tscatalog autostart` (`up --local` for flagged services). launchd is not the parent of user processes, does not KeepAlive them, and does not hold their logs.

Per-service agents would steal pidfiles, XDG logs, `stop`, and `CatalogOwns` from the reaper. A one-shot login hook keeps those, and a `stop` stays down until the next `up`/`restart` or a new session.
