# Architecture

## Core daemon

- Single Go binary (`voltpanel`) embeds the React/Vite UI build
  (`cmd/voltpanel/dist`, produced by `make ui-build`).
- Gin HTTP server; versioned API under `/api/v1/*` (`internal/api/v1`) —
  transport-only handlers, business logic lives in the packages they call
  into. `internal/server` wires everything together and owns process
  lifecycle (graceful `Shutdown`/`Close` on `SIGTERM`/`SIGINT`, wired from
  `cmd/voltpanel/main.go` — this is what makes `voltpanel stop` work).
- CLI: a handful of subcommands on the same binary (`start`/`stop`/
  `restart`/`status`/`logs`/`doctor`/`update`/`version`) via a hand-rolled
  dispatch in `cmd/voltpanel/cli.go` — see `docs/cli.md`.
- Auth: a static bearer token (`X-Volt-Token`) plus session cookies issued
  on top of it (`internal/security.SessionAuth`) — see `docs/api.md`.
- Service/process management (`internal/domain/service`): graceful stop
  (SIGTERM, then a timeout, then force-kill), crash detection with a
  restart policy, and dependency-ordered autostart (Kahn's algorithm,
  cycle detection) at daemon startup.
- Real per-OS service registration for the daemon itself
  (`internal/platform/{darwin,linux,windows}`, replacing what used to be
  a no-op stub) — launchd (macOS), a systemd **user** unit (Linux, no
  root needed), and a Windows Service via `golang.org/x/sys/windows/svc`.
- Structured state lives in SQLite (`internal/storage`, driven by the
  pure-Go `modernc.org/sqlite` so cross-compilation with `CGO_ENABLED=0`
  keeps working) at `<config dir>/volt.db`, with migrations in
  `internal/storage/migrations/`. The daemon's own token/port/session
  secret stay in a plain `config.json` file (never in SQLite) —
  intentional: secrets/bootstrap config vs. structured state are kept on
  separate persistence paths.
- Config dir: `~/.volt/` on every OS today (no OS-native path split yet).
  On first run, matching data from a prior `~/.alresia-voltpanel/` or
  `~/.alresia-volt/` install is imported in, non-destructively.

## Domain entities (`internal/domain/*`)

Project, Runtime, Service, Domain/Certificate, Server, Deployment,
Pipeline, Integration, Extension — one repository package each, backed by
SQLite. Domain packages never import a concrete provider; they depend
only on the provider *interfaces* in `internal/providers/contract.go`.

## Providers (`internal/providers/*`)

One package per capability, each implementing an interface from
`contract.go`:

- `runtime/{node,php}` — real version detection (nvm-managed Node,
  Homebrew/update-alternatives PHP) and default-switching.
- `docker` — a minimal stdlib-only Engine API client over the Unix
  socket (deliberately not the full moby/docker SDK).
- `domainprovider/hosts` — hosts-file entries, distinguishing
  Volt-managed lines from foreign ones.
- `ssl/localca` — a local CA (stdlib `crypto/x509`) issuing per-domain
  leaf certificates; OS/browser trust-store installation is a single,
  explicit, `confirmed:true`-gated operation, never automatic.
- `remote/ssh` — `golang.org/x/crypto/ssh`, prefers the user's
  `ssh-agent` over stored keys.

External providers (`internal/pluginhost`) run as a genuinely separate
process speaking a small, versioned, newline-delimited-JSON protocol over
stdio — not Go plugins, not WASM (both explicitly rejected: portability
and complexity, respectively, for what this needs). An enabled extension
registers into the same `providers.Registry` built-ins use, so callers
can't tell the difference.

## Security (`internal/security`)

- `Sandbox`: a path-jail primitive every filesystem-touching provider
  goes through — rejects `..` traversal and symlink escapes. Currently
  rooted at the user's home directory; narrowing to a per-project root is
  a real future improvement now that Project exists, not yet done.
- `SessionAuth`: signs/verifies session cookies on top of the static
  token.
- `SecretStore`: the two-tier Secret abstraction (§9.7) — OS keychain
  first (macOS Keychain via the `security` CLI today), an AES-GCM
  encrypted blob (machine-local key file, 0600) as the fallback. SSH keys
  and git PATs go through this; the `secrets` SQLite table only ever
  holds metadata, never plaintext.
- CSRF/Origin check on every mutating request; destructive actions
  (delete/stop/uninstall/CA-trust/rollback) require an explicit
  server-side `confirm=true`; every mutating call writes an
  `audit_events` row.
- The daemon binds `127.0.0.1` only, hardcoded, with no flag to change
  it. The one genuinely privileged operation anywhere in the codebase —
  installing the local CA into the real OS/browser trust store — never
  runs implicitly.

## Deployment + CI/CD

`internal/domain/deployment.Engine`: checkout over SSH (clone or
fetch+reset), optional install command, optional restart command,
optional HTTP health check, one log file per deployment. Rollback checks
out a previous deployment's commit and restarts — it never claims to undo
a database migration (`migrationRollbackSupported` is always `false`).

`internal/pipeline.Engine`: a linear YAML step runner (`run` = local
shell, `ssh` = remote command via a Server, `deploy` = delegates wholesale
to the Deployment engine above, `healthcheck` = a standalone HTTP check).
Triggered manually or via a webhook whose HMAC-SHA256 signature (keyed by
a per-pipeline secret, generated once and shown once) is verified before
anything runs — a pipeline can execute an arbitrary local shell command,
so this signature check is the only thing standing between an HTTP POST
and remote code execution.

## Packaging

Via GoReleaser: linux/darwin/windows × amd64/arm64, nfpm deb/rpm (installs
`packaging/voltpanel.service` as a systemd unit), Windows zip, and a
Homebrew tap formula generated by GoReleaser's `brews:` block.
`packaging/wix/template.wxs` is the one MSI path kept (NSIS and the old
static Homebrew formula stub were removed as dead, unwired assets).

## Naming

The product name is **VoltPanel** (binary `voltpanel`) — a rename to
`volt` was considered and explicitly rejected (name collision risk) as of
2026-09-18; see `.claude/plans/volt-implementation-plan.md`'s top-of-file
naming decision and its progress log for the full phased history.
