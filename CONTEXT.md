# tscatalog

Word list for this repo. Install and first run are in the [README](README.md).

One host. The catalog says which Tailscale HTTPS names should exist, and how this machine starts the local listeners behind them.

## Language

**Service**:
A named catalog row with a local bind and a public Tailscale identity.
_Avoid_: app, site, workload

**Launch**:
One catalog slot that starts a process, a Compose project, or an external helper.
_Avoid_: workload, job, unit

**Process**:
A long-running program tscatalog detaches and reaps.
_Avoid_: daemon, LaunchAgent, service (the catalog row)

**External**:
A helper tscatalog runs and then forgets. Some other manager owns the listener.
_Avoid_: unmanaged (it still has a catalog row)

**Autostart**:
A Service that should be started when the user session begins.
_Avoid_: KeepAlive, login item, LaunchAgent (those are how a host may implement it)
