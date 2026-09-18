# CLI

Kept deliberately small (§12 of the plan): subcommands of the same
`voltpanel` binary, no separate CLI artifact.

- `voltpanel` / `voltpanel start` — boot the daemon (today's default
  behavior, unchanged for continuity with existing invocations/scripts).
- `voltpanel stop` — sends a graceful shutdown signal (SIGTERM) to the
  running daemon via its PID file (`<config dir>/daemon.pid`).
- `voltpanel restart` — stops the running daemon (if any) and relaunches.
- `voltpanel status` — hits `/health` on the daemon's configured port and
  reports up/down.
- `voltpanel logs [-f]` — prints (or follows) `<config dir>/logs/daemon.log`.
- `voltpanel doctor` — checks config-dir writability, that config loads,
  and disk space; reports whether a daemon appears to be running.
- `voltpanel update` — checks the latest GitHub release against the
  running binary's version and prints how to install it. Never replaces
  the running binary automatically — that's a deliberate choice: silently
  self-replacing a running executable is exactly the kind of hard-to-reverse
  action that should stay an explicit, separate step (re-run your original
  install method) rather than something this command does on its own.
- `voltpanel version` — prints the build version (set via `-ldflags
  -X main.Version=...`; `dev` for a plain local `go build`).

**Explicitly not added**: `volt project`/`deploy`/`server`/`pipeline` and
similar — the browser UI owns management for those. A CLI verb gets added
only when there's a proven case the browser can't cover (e.g. scripting a
deploy from another CI system), and even then as a thin wrapper over the
same REST API the UI uses, never a second implementation of the logic.
