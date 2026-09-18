# Volt — Architecture & Implementation Plan

> Status: Phase 0 in progress. First implementation slice = Phase 0 (see §21).
> Source review performed by direct inspection of the repository on 2026-09-17.
>
> **Naming decision (2026-09-18):** the product/binary/module keep the **VoltPanel/voltpanel**
> name — "Volt" alone was judged too generic (heavy collision with existing "Volt" products/
> brands online). Every §16/§17 reference to renaming to `volt` is superseded by this decision;
> treat those sections as historical rationale for *why a rename was considered*, not as active
> work items. The module path, binary name (`voltpanel`), and systemd unit stay as-is.
>
> **Config-dir decision (2026-09-18):** the daemon's data directory unifies to `~/.volt/`
> (a plain, low-drama internal path — distinct from the product-branding decision above).
> Existing `~/.alresia-voltpanel/` (and `~/.alresia-volt/`, in case an old build ever used that
> name) is imported into `~/.volt/` on first run and left untouched, never deleted.

## Phase 0 progress log

Completed and verified (`go build ./...`, `go vet ./...`, `go test ./...` all green):
- **File-manager path sandboxing** (§9.5, §21.1): new `internal/security.Sandbox` — rejects
  `..` traversal and symlink escapes, refuses to delete its own root. Wired into
  `internal/storage.Store.ListPath/WriteFile/DeletePath`, rooted at the user's home directory
  (no Project entity exists yet to scope it tighter). This closes the arbitrary-filesystem-access
  hole described in §2/§3 — the single most serious existing issue found in the review.
- **WebSocket auth fix** (§9.3, §21.2): `internal/ws` now requires a first-message
  `{"type":"auth","token":"..."}` frame (browsers can't set custom headers on a WS handshake,
  which made the old header-based check unreachable). Also fixed a related bug found while in
  this file: the hub never read from client connections after upgrade, so gorilla/websocket's
  close-frame handling — and therefore client cleanup — never actually ran. UI (`Logs.tsx`)
  updated to send the auth frame on open.
- **Constant-time token compare** (§9.2): HTTP token check now uses `crypto/subtle`.
- **CI hardening** (§21.3): removed the `|| true` that was swallowing UI-build and integration-test
  failures in `.github/workflows/ci.yml`; added `go vet`; added a `cross-platform` matrix job
  (ubuntu/macos/windows) running `go vet`/`go test` against `internal/...` (scoped to that
  package because `cmd/voltpanel` needs `ui/dist` populated first, which the matrix job doesn't do).
- **Config-dir migration** (§16.2, adjusted per the naming decision above): `~/.volt/` is now
  the live config dir; a one-time, idempotent, non-destructive import from
  `~/.alresia-voltpanel/`/`~/.alresia-volt/` runs on first `EnsureDirs()` call.
- **Bonus fix, found while verifying the build**: `internal/server/server.go`'s static-file
  handler passed an `fs.File` to `http.ServeContent`, which requires `io.ReadSeeker` —
  `fs.File`'s interface doesn't declare `Seek`, so this was a genuine, deterministic compile
  error. The repository could not have produced a working `go build ./...` before this fix,
  independent of anything else in this plan. Fixed with a type-assertion + `io.Copy` fallback.
- Added real test coverage for all of the above (`internal/security/sandbox_test.go`,
  `internal/ws/ws_test.go`, `internal/storage/store_test.go`) — this repo had zero tests
  before Phase 0.

Not yet done from the original Phase 0 scope:
- **Dead packaging asset cleanup** (§5, §17 Phase 0): stale `formula/voltpanel.rb` (superseded
  by goreleaser's `brews:` block), and picking one of `packaging/wix/template.wxs` /
  `packaging/nsis/installer.nsi` (currently both exist, neither wired into goreleaser). Not
  started — lower urgency than the security/correctness fixes, and untouched by anything above.
- **Full embedded-binary verification** (`make build` / `make ui-build`): blocked mid-session by
  the machine repeatedly running critically low on disk space (down to ~100–300MB free at
  points). `go build`/`vet`/`test` against `internal/...` and `cmd/voltpanel` (Go-only, no
  embed-dependent UI step) are verified; the full `pnpm`-driven UI build was deliberately
  **not** forced through (it wanted to purge/reinstall `node_modules` and needed `CI=true` to
  run non-interactively) given how little headroom the disk had at the time. Re-run `make build`
  once disk space is comfortably free to close this out.
- The module/binary/service rename items in §16/§17's Phase 0 description no longer apply
  per the naming decision above.

All Phase 0 items are now done, including the two above (packaging cleanup done; full
`make build` verified end-to-end with the real embedded UI, disk space was the only blocker
and has since cleared).

## Phase 1 progress log (2026-09-18, overnight autonomous run)

Done and verified (`go build`/`vet`/`test` green; full `make build` + curl smoke test +
`scripts/integration_test.sh` all exercised against the real binary):
- SQLite storage (`modernc.org/sqlite`, pure-Go) with the full §6 schema created in one
  migration (`internal/storage/migrations/0001_init.sql`) so later phases add repositories,
  not schema churn. `apps.json` imports into the `services` table once, idempotently, on
  first startup; the JSON file is never written again or deleted.
- `server.go`'s inline routes decomposed into `internal/api/v1/*`; routes moved to
  `/api/v1/*` with an aggressive cutover (no deprecated-alias period) since nothing has
  shipped the old flat routes to real users yet.
- `internal/security.SessionAuth`: session-cookie auth layered on the static token; the WS
  hub checks the same cookie at handshake time ahead of Phase 0's first-message-frame
  fallback. CSRF/Origin check on mutating requests; file deletes require `confirm=true`;
  every mutating call writes an `audit_events` row.
- Files API gained mkdir/move/copy. `GET /services` merges persisted + live state.
- Fixed dead code in `internal/metrics.scanLocalPorts` (a 65535-port bind/close loop whose
  result was discarded, flagged in the original review).
- `ui/src/lib/{api,ws}.ts`: one typed client + one shared WS connection, replacing every
  page's hand-rolled fetch/WebSocket calls. Pages updated to the new API; still the old
  tab-switcher `App.tsx` (react-router migration deferred to whenever the first
  deep-linkable detail view — a Project or Server page — actually lands, per §11's own
  reasoning for why that's the trigger, not before).
- go.mod bumped to go 1.25 (sqlite driver's transitive requirement); CI's go-version matched.

Not done: nothing deferred from Phase 1's stated scope.

## Phases 2/3/5/6 progress log (2026-09-18, parallel overnight run)

Built in parallel (isolated git worktrees, one per phase, merged back into `main`
sequentially with manual conflict resolution where they touched shared files like
`router.go`/`App.tsx`/`deps.go`/`server.go`). All green after every merge:
`go build`/`vet`/`test` (including `-race` for Phase 5, cross-compiled clean for
linux+windows), UI `tsc --noEmit`, and a live end-to-end smoke test of the built binary
exercising services/runtimes/projects/docker together.

- **Phase 2 (Providers + Node/PHP)**: `internal/providers/contract.go` defines all nine
  §7 interfaces (only RuntimeProvider implemented); real Node detection via `node
  --version` + nvm-managed installs, real PHP detection via Homebrew/update-alternatives.
  Verified against this machine's actual installs (3 nvm Node versions, PHP 8.2.23 via
  AMPPS) -- read-only detection only, no installs/removals performed. `internal/domain/runtime`
  persists results to the `runtimes`/`runtime_versions` tables. `GET/POST /api/v1/runtimes/*`
  + a Runtimes UI page.
- **Phase 3 (Projects)**: `internal/domain/project` + an extensible framework-detection
  engine (Laravel/Next.js/generic Node/generic PHP, registry-based so adding a framework
  never touches existing detectors), driven by real `testdata/*-fixture` marker files.
  `internal/storage/migrations/0002_project_detection.sql` adds detected_kind/run_command
  columns. `GET/POST/DELETE /api/v1/projects/*` + a Projects UI page.
- **Phase 5 (Services + real startup)**: `internal/domain/service` replaces
  `internal/agent` entirely -- graceful stop (SIGTERM, then timeout, then force-kill),
  crash detection with a restart-policy + backoff, Kahn's-algorithm dependency-ordered
  autostart with cycle detection, wired into `cmd/voltpanel/main.go` right after
  `server.New()`. `internal/platform/{darwin,linux,windows}` replace `internal/system`'s
  no-op stubs with real launchd/systemd-user/Windows-SCM registration code --
  file-generation is fully unit-tested against temp dirs, but **no real service was
  installed on this machine** (independently verified: `~/Library/LaunchAgents`,
  `launchctl list`, `~/.config/systemd/user/` all checked clean of any volt entries
  after the merge). `internal/storage/migrations/0003_service_lifecycle.sql` adds
  restart-policy/graceful-timeout columns (renumbered from a colliding `0002_*.sql` --
  the Phase 3 and Phase 5 agents independently created migration files both named
  `0002_*.sql` in their isolated worktrees, which would have silently dropped one
  migration's columns entirely since the migration runner keys by the numeric filename
  prefix; caught and fixed during the merge, before it ever reached a running database).
- **Phase 6 (Docker)**: `internal/providers/docker` -- a minimal stdlib-only Engine API
  client over the Unix socket (deliberately not the full moby/docker SDK), covering
  containers/images/volumes/networks/exec/logs, grouped by Compose project label. This
  machine has the Docker CLI but no daemon running; verified the "Docker not available"
  path returns a clean `503`, not a 500, both in tests and against the live built binary.
  `GET/POST /api/v1/docker/*` + a Docker UI page.

Not done / deferred: nothing from these four phases' stated scope. Windows service
registration is cross-compile-verified only (`GOOS=windows go build`), never executed --
no Windows machine available here.

Next: Phase 4 (Domains+SSL, depends on Phase 3 ✓) and Phase 7 (Remote/SSH, depends on
Phase 5's ServiceLifecycle shape ✓ + Phase 1's Secret model ✓) can now start in parallel.
Phase 11 (Extensions) also unblocked (needs Phase 2's contracts ✓). Phase 8 (Git) needs
Phase 3 ✓ + Phase 1 ✓ and can start too. Phase 9/10 still wait on 7+8.

## Environment notes from the review

1. At review time the shell reported "no space left on device" for every command (even `echo`), which blocked `go build`/`goreleaser` verification. This appears to have cleared since (bash is working again as of this writing) — worth a `df -h` sanity check before any large build/install step.
2. `.hide` in the repo root contains what looks like a live GitHub personal access token (`ghp_...`). It is untracked and gitignored (never entered git history), but sits in plaintext in the working tree. Recommend revoking/rotating it if real and removing it from the project folder.

Neither blocks the plan below.

---

## 1. Executive Summary

**What it is today:** VoltPanel is a single-shot, AI-scaffolded (Windsurf-generated, per `instruction.txt`) MVP: a Go/Gin daemon (`cmd/voltpanel`) that embeds a Vite/React/Tailwind UI, manages OS processes via `os/exec`, exposes a flat REST API + one WebSocket broadcast channel, and persists state in two JSON files under `~/.alresia-voltpanel/`. It has no tests, no path sandboxing on its file-manager endpoints, no real service-registration code (the "systemd/launchd" package is a stub that does nothing), and several internal naming inconsistencies (`voltpanel` vs `devpanel.service` vs `alresia-volt` config dir vs `alresia-voltpanel` config dir). It is a working proof of concept for exactly one feature: "list and run a few processes on your own machine," built to prove the packaging pipeline (GoReleaser → Homebrew/deb/rpm/zip) more than the product itself.

**What it needs to become:** Volt — a long-lived, provider-based control plane for local dev environments, remote servers, and deployments, reachable entirely from a browser, with a Go daemon as the only backend and a small CLI for lifecycle operations. That means introducing: a real data model (projects, runtimes, domains, certificates, servers, deployments — not just "apps"), a provider/plugin architecture so runtimes/databases/web-servers/git-hosts aren't hardcoded, a security model with actual authn/authz/sandboxing, cross-platform OS integration that isn't a no-op, and a UI built around "projects" rather than "start a process."

**Major architectural changes required:**
- Replace the single monolithic `server.New()` (170 lines of inline route closures) with a layered API (versioned routes, real handler structs, middleware chain) — not a rewrite of Gin usage, just decomposition.
- Replace ad-hoc JSON files with SQLite for structured state, keep JSON only for the initial secret/token bootstrap.
- Introduce a `Provider` interface layer so runtimes, databases, web servers, and remote targets aren't each a bespoke code path.
- Turn `internal/system`'s no-op `Install`/`Uninstall` into real per-OS service registration.
- Add a sandboxed, project-root-scoped file manager (the current one is a straight arbitrary-path passthrough — this is a real vulnerability, not a future gap).
- Rename `voltpanel` → `volt` deliberately and in one pass (binary, module path, config dir, service unit name, docs), with a migration shim for existing installs.

**Major risks:** scope explosion (the vision doc covers ~15 subsystems); doing OS-level things (hosts file, cert trust store, service registration, sudo) safely across three OSes; keeping the extension story honest without committing to Go plugins (portability trap) prematurely; SSH/remote-server work being security-sensitive from day one.

---

## 2. Current Architecture (as it actually exists)

```
cmd/voltpanel/main.go
  - reads PORT/DEV env + flags
  - storage.EnsureDirs()      → ~/.alresia-voltpanel/{logs,runtime}
  - storage.LoadOrInitConfig()→ config.json {token, port, createdAt}
  - probePort(): linear scan 7788..7807, first free wins
  - server.New(opts).Run()    → gin.Run on 127.0.0.1:<port>
  - //go:embed dist/*          (UI must be manually copied here by `make ui-build`)

internal/server/server.go
  - one function New() wires ALL routes inline (no route groups per feature, no versioning)
  - auth: single shared token, header `X-Volt-Token`, plain string compare (not constant-time),
    bypassed entirely when Dev=true
  - routes: /health, /ws/events, /auth/token/verify, /services (CRUD-ish),
    /processes, /files/list|write|upload|delete, /logs/:id, /metrics
  - static SPA fallback via NoRoute serving embedded dist/

internal/agent/manager.go
  - Manager{mu, procs map[string]*proc}  — LIVE processes only, in-memory
  - Start(): os/exec.Command, pipes stdout/stderr to per-id log file AND ws hub, no working-dir
    validation, Env = os.Environ() + overrides
  - Stop(): hard Process.Kill() (SIGKILL) — no graceful shutdown/timeout
  - Restart(): reads persisted metadata from store, re-Start()s
  - No crash-detection/auto-restart/backoff, no dependency ordering
  - Two parallel "list" concepts: store.ListApps() (persisted apps.json, survives restart,
    stale PID/status after daemon restart) vs manager.List() (live procs, empty after
    daemon restart until re-started) — exposed as two different endpoints (/services vs
    /processes), which is confusing and a real source of UI bugs

internal/storage/store.go
  - Pure JSON files: config.json (token/port), apps.json (process metadata)
  - ListPath/WriteFile/DeletePath wrap os.* directly with **zero path sandboxing** —
    any absolute path on the filesystem is readable/writable/deletable via the API.
    instruction.txt explicitly asked for "safe path whitelisting" — never implemented.
  - No SQLite despite instruction.txt mentioning meta.db

internal/system/service_{unix,windows}.go
  - Install()/Uninstall() are literal no-ops with a comment saying registration is
    "handled by packaging templates and docs" — i.e., there is no runtime service
    management code at all today.

internal/ws/ws.go
  - gorilla/websocket Hub, single global broadcast channel (no per-service subscription),
    CheckOrigin always returns true ("auth handled upstream" — but see below)
  - Real bug: browsers cannot set custom headers on a WebSocket handshake, so the
    X-Volt-Token check on /ws/events is unreachable from actual browser JS clients —
    WS auth is effectively decorative today.

internal/metrics/metrics.go
  - gopsutil CPU/mem/disk; "open ports" scan has dead code (a full 65535-port bind/close
    loop that discards its own result) followed by the actual check (dial a fixed list
    of common ports).

ui/ (React 18 + Vite 5 + Tailwind, no shadcn despite instruction.txt asking for it)
  - App.tsx: tab switcher (not a router), token pasted manually into a text input,
    stored in localStorage — no first-run reveal flow, no regenerate-token UI despite
    Settings.tsx claiming you should "delete config.json and restart" to do that
  - Dashboard.tsx: fetches /metrics once on mount, no polling/streaming
  - Processes.tsx: start/stop only, no restart button, no log deep-link
  - Files.tsx: arbitrary absolute-path browser/editor, no upload UI (endpoint exists,
    unused), no mkdir/rename/move/copy/archive
  - Logs.tsx: opens its own WS connection, filters broadcast stream client-side by id

Packaging
  - goreleaser.yml: builds linux/darwin/windows × amd64/arm64, nfpm deb/rpm (installs
    systemd unit as /lib/systemd/system/devpanel.service — note the name mismatch),
    Windows zip only, brews: block auto-publishes to alresiainc/homebrew-tap with a
    brew-services launchd definition
  - packaging/wix/template.wxs and packaging/nsis/installer.nsi exist but are NOT
    referenced anywhere in goreleaser.yml — dead/unintegrated packaging assets
  - packaging/com.alresia.voltpanel.plist and packaging/scripts/postinstall.sh are a
    second, manual service-registration path that duplicates what nfpm+brews already do
  - formula/voltpanel.rb is a static stub (sha256 "REPLACE_ME", points at
    github.com/alresia/voltpanel — wrong org, no "inc") superseded by goreleaser's
    brews: block — dead file

CI (.github/workflows/ci.yml)
  - single ubuntu-latest job: install pnpm, `make ui-build || true` (failure swallowed),
    `make build`, `go test ./...` (no test files exist, so this always trivially passes),
    `bash scripts/integration_test.sh || true` (failure swallowed)
  - no macOS/Windows runners, no lint, no UI typecheck, no security scanning

Tests: none exist (`*_test.go` search returns nothing).

Naming inconsistencies already present, before any Volt rename:
  - module: github.com/alresiainc/alresia-voltpanel; binary: voltpanel; systemd unit:
    devpanel.service; config dir in code: ~/.alresia-voltpanel/; config dir per
    README/instruction.txt/docs/Settings.tsx: ~/.alresia-volt/. These are three
    different names for what should be one product.
```

**What's genuinely reusable** (do not rewrite for style): the Gin+embed serving pattern, the port-probe logic, the WS hub's core broadcast loop, the gopsutil metrics wrapper, the goreleaser cross-compile matrix, the overall packaging *shape* (systemd/launchd/nfpm/brew — the mechanism is right, the wiring is inconsistent). The process pipe-to-logfile-and-hub pattern in `agent.Manager.pipeLogs` is also sound and should become the base of the future `ProcessProvider`.

---

## 3. Architecture Gap Analysis

| Area | Existing | Target | Gap | Priority |
|---|---|---|---|---|
| Core daemon structure | One `server.New()` with inline routes | Layered API, versioned, per-domain handler modules | Decompose into `internal/api/{v1}/<domain>` handlers, router assembly separate from wiring | P0 |
| Persistent storage | Two flat JSON files, no schema | SQLite with migrations, secrets kept out of DB | New `internal/storage` (SQLite) + migration from JSON | P0 |
| Auth/security | Single shared token, non-constant-time compare, WS auth unreachable from browsers, no CSRF/origin checks, files endpoints unsandboxed | Session-aware auth, WS auth via subprotocol/first-message, path-sandboxed file provider, audit log | New `internal/security` middleware + provider sandbox | P0 |
| Process management | In-memory only, hard-kill, no crash detection, dual "apps vs processes" model | Unified Process/Service model, graceful stop, crash detection, restart policy | Merge `agent.Manager` + `storage.App` into one `ProcessProvider` | P0 |
| Providers (runtime/db/webserver) | None — hardcoded to "run any command" | Provider interface, pluggable PHP/Node/MySQL/Postgres/Redis/Nginx | New `internal/providers/*` packages + registry | P1 |
| Projects | Does not exist as a concept | First-class entity: path, runtime, domains, services, DB | New `internal/domain/project` + detection engine | P1 |
| Local domains | Does not exist | `.test` domains via hosts-file/DNS provider, dashboard-managed | New `internal/providers/domain` + `internal/platform/*` hosts helpers | P1 |
| Local SSL | Does not exist | Local CA + mkcert-style cert issuance, explicit trust install | New `internal/providers/ssl` | P1 |
| Web server / reverse proxy | Does not exist | Nginx/Apache vhost generation, managed-vs-unmanaged config detection | New `internal/providers/webserver` | P1 |
| Databases | "run any command" only | MySQL/Postgres/Redis lifecycle providers, version vs data-dir separation | New `internal/providers/database/*` | P2 |
| Docker | Does not exist | Local/remote Docker management via Docker Engine API | New `internal/providers/docker` | P2 |
| Service lifecycle/startup | `internal/system` Install/Uninstall are no-ops | Real launchd/systemd/Windows-service registration with dependency ordering | Implement `internal/platform/{darwin,linux,windows}` | P0 (fixing an existing lie in the code) |
| File manager | Arbitrary absolute-path read/write/delete, no sandbox | Root-scoped, permissioned, traversal-safe | Rewrite `storage.ListPath/WriteFile/DeletePath` behind a sandbox layer | P0 (security) |
| Remote servers/SSH | Does not exist | SSH-based remote provider, no daemon-on-remote assumption | New `internal/providers/remote/ssh` | P2 |
| Git integration | Does not exist | GitHub/GitLab/Bitbucket via provider interface | New `internal/integrations/git/*` | P3 |
| CI/CD pipelines | Does not exist | YAML pipeline engine, SSH/Docker steps, health checks | New `internal/pipeline` | P3 |
| Deployment | Does not exist | Deploy target model, history, rollback (code vs artifact vs migration, separated) | New `internal/domain/deployment` | P3 |
| Provider/extension system | Does not exist | Stable versioned interfaces + out-of-process executable providers | New `internal/providers` contract + `internal/pluginhost` | P1 (interfaces), P4 (ecosystem polish) |
| WebSocket | Single global broadcast, no subscriptions, auth unreachable via browser | Topic-scoped events, browser-reachable auth | Rework `internal/ws` to subscription model + first-message auth | P0/P1 |
| Naming | voltpanel/devpanel/alresia-volt(-panel) inconsistent | Volt everywhere: binary `volt`, module, config dir, service name | Rename pass + compatibility shim | P0 |
| Tests | None | Unit + integration + platform matrix | Add as each phase lands, not retrofitted at the end | P0 (process, not a "phase") |
| CI | Single Ubuntu job, failures swallowed with `\|\| true` | Real gating CI, macOS/Windows runners, lint | Fix `ci.yml` early | P0 |
| Packaging | Goreleaser matrix works; WiX/NSIS unwired; two competing service-registration paths; stale Homebrew formula | Single coherent packaging path per OS | Cleanup pass, not a rewrite | P1 |

---

## 4. Proposed Target Architecture

```
                              Browser (React UI)
                                     │
                         REST (/api/v1/*) + WebSocket (/ws)
                                     │
                        ┌────────────────────────┐
                        │       Volt Daemon        │
                        │ ─────────────────────── │
                        │ internal/api  (HTTP/WS)  │
                        │ internal/security        │
                        │ internal/domain/*        │  ← Project, Server, Deployment...
                        │ internal/providers/*     │  ← Runtime/DB/WebServer/Domain/SSL/Docker
                        │ internal/platform/*      │  ← darwin/linux/windows OS glue
                        │ internal/storage         │  ← SQLite + migrations
                        └───────────┬──────────────┘
             ┌───────────────────┬─┴──────────────────┬───────────────────┐
             ▼                   ▼                    ▼                   ▼
     Local Environment     Remote Servers        Providers            Integrations
     PHP/Node/Python/Go    SSH-managed hosts      Native / Docker      GitHub/GitLab/
     MySQL/Postgres/Redis  (no daemon assumed)    runtime backends     Bitbucket/SSH/
     Nginx/Apache, files                                               cloud (later)
     domains, SSL
```

**Subsystem responsibilities:**
- `internal/api` — HTTP/WS transport only: routing, request decoding, response encoding, versioning. No business logic.
- `internal/security` — auth (session + token), CSRF/origin checks, audit logging, the path-sandbox primitive every filesystem-touching provider must use.
- `internal/domain/*` — the entities from Section 6 (Project, Runtime, Service, Domain, Certificate, Server, Deployment, Pipeline, Integration, Extension) as plain Go structs + repositories backed by SQLite. No provider-specific logic here.
- `internal/providers/*` — one package per capability (`runtime/php`, `runtime/node`, `database/mysql`, `webserver/nginx`, `domain/hosts`, `ssl/mkcert`, `docker`, `remote/ssh`), each implementing a small interface from Section 7.
- `internal/platform/{darwin,linux,windows}` — the *only* place `//go:build` OS tags appear for system integration (service registration, hosts file, cert trust, privilege escalation, startup paths).
- `internal/pipeline` — CI/CD execution engine (phase 10+), consumes Git + SSH + Docker providers.

---

## 5. Proposed Repository Structure

```
cmd/
  volt/                      # renamed from cmd/voltpanel — daemon entrypoint
    main.go
    dist/                    # embedded UI build output (retain pattern, keep gitignored)
  volt-cli/                  # (optional, phase-dependent) if CLI diverges from daemon binary;
                              # otherwise keep CLI as subcommands of cmd/volt (see §12)

internal/
  api/
    v1/
      projects.go            # NEW
      runtimes.go            # NEW
      services.go            # RENAME/REFACTOR of current services+processes split
      files.go                # REFACTOR — sandboxed
      domains.go              # NEW
      ssl.go                  # NEW
      databases.go            # NEW
      docker.go                # NEW
      servers.go               # NEW (remote/SSH)
      deployments.go           # NEW (phase 9+)
      pipelines.go             # NEW (phase 10+)
      integrations.go          # NEW (phase 8+)
      extensions.go            # NEW (phase 11+)
    router.go                 # assembles gin engine, mounts v1, static UI fallback
    middleware.go              # auth, CSRF, logging — extracted from server.go
  security/
    auth.go                    # session/token verification (RETAIN token concept, harden it)
    sandbox.go                  # path-jail primitive (NEW — closes current vuln)
    audit.go                    # NEW
  domain/
    project/                    # NEW
    runtime/                    # NEW
    service/                    # RETAIN concepts from agent.Manager, relocated
    domainname/                 # NEW (local domains — named to avoid clashing with Go "domain")
    certificate/                # NEW
    server/                     # NEW (remote servers)
    deployment/                 # NEW
    pipeline/                   # NEW
    integration/                # NEW
    extension/                  # NEW
  providers/
    contract.go                  # RuntimeProvider/ServiceProvider/etc interfaces (NEW)
    runtime/{php,node,python,go}/ # NEW
    database/{mysql,postgres,redis}/ # NEW
    webserver/{nginx,apache}/     # NEW
    domainprovider/hosts/         # NEW
    ssl/localca/                  # NEW
    docker/                        # NEW
    remote/ssh/                    # NEW
    git/{github,gitlab,bitbucket}/ # NEW (phase 8+)
  platform/
    darwin/    # launchd registration, keychain, hosts file, cert trust
    linux/     # systemd registration, hosts file, cert trust (ca-certificates)
    windows/   # Windows service, hosts file, certutil trust
    paths.go    # cross-platform config/data dir resolution (RETAIN + generalize storage.EnsureDirs)
  storage/
    sqlite.go    # NEW — connection, migrations
    migrations/  # NEW — versioned .sql files
    jsonlegacy/  # RETAIN old store.go temporarily, read-only, for migration import
  ws/
    hub.go        # REFACTOR — topic subscriptions, not single broadcast
    auth.go        # NEW — first-message token auth (fixes browser header limitation)
  metrics/
    metrics.go     # RETAIN gopsutil wrapper, extend with per-process metrics
  pipeline/         # NEW (phase 10+) — CI/CD engine
  pluginhost/       # NEW (phase 11+) — external provider process protocol

ui/
  src/
    app/              # NEW — router (react-router) replacing tab-switch App.tsx
    pages/            # RETAIN concept, expand: Overview, Projects, Runtimes, Services,
                      # Databases, Docker, Files, Domains, Servers, Deployments,
                      # Pipelines, Git, Extensions, Settings
    components/       # NEW — currently empty despite instruction.txt listing it
    lib/api.ts        # NEW — typed API client (currently every page hand-rolls fetch)
    lib/ws.ts         # NEW — shared WS client (currently Logs.tsx owns its own connection)

packaging/
  systemd/volt.service          # RENAME devpanel.service → volt.service, fix path
  launchd/com.alresia.volt.plist # RENAME, keep under packaging/launchd/ (was flat)
  windows/                        # consolidate wix+nsis here, wire ONE into goreleaser
  scripts/postinstall.sh          # RETAIN, fix service name

formula/
  volt.rb   # DEPRECATE static stub entirely; rely solely on goreleaser `brews:` block

docs/
  architecture.md, api.md, install-testing.md   # RETAIN, rewrite for Volt
  providers.md, security.md, extensions.md, cli.md, migration.md   # NEW

.github/workflows/
  ci.yml       # FIX: remove `|| true`, add macOS+Windows runners, add lint
```

**Deprecate/remove:** `formula/voltpanel.rb` (superseded by goreleaser `brews:`), `packaging/nsis/installer.nsi` *or* `packaging/wix/template.wxs` (pick one MSI path, not two unwired ones), `internal/system/service_unix.go`/`service_windows.go` as no-ops (replaced by real `internal/platform/*` implementations — keep the build-tag *pattern*, not the empty bodies).

---

## 6. Data Model

Core entities (SQLite tables, one repository each under `internal/domain/*`):

```
Project
  id, name, path, runtime_id, runtime_version, primary_domain_id,
  webserver_kind, database_id, redis_enabled, created_at, updated_at
  → has many: Domain, ProcessSpec, DeploymentTarget

Runtime
  id, kind (php|node|python|go|ruby|java), name
RuntimeVersion
  id, runtime_id, version, install_path, is_default, status (installed|installing|missing)

Service   (RETAINED concept from agent.Manager/storage.App, formalized)
  id, project_id (nullable — some services are global, e.g. MySQL), kind (native|docker|managed-db),
  command, args, cwd, env, autostart, depends_on[], status, pid, log_path

Domain
  id, hostname (myapp.test), project_id, port, provider (hosts|dns), ssl_enabled, enabled

Certificate
  id, domain_id, ca_id, not_before, not_after, status (valid|expiring|expired|revoked)

Server        (remote SSH targets)
  id, name, hostname, port, username, auth_method (key|agent|password-discouraged),
  secret_ref (→ OS keychain / encrypted secret, never plaintext in DB), last_connected_at, os_info

Deployment
  id, project_id, server_id, commit_sha, branch, pipeline_run_id, status, started_at,
  finished_at, duration_ms, log_ref
  # rollback fields kept SEPARATE per §18: code_rollback_ref, artifact_rollback_ref,
  # migration_rollback_supported (bool, default false — never assumed)

Pipeline
  id, project_id, name, definition_yaml, created_at, updated_at
PipelineRun
  id, pipeline_id, trigger (manual|push|webhook), status, steps[] (ordered, each with
  log_ref/status/duration), started_at, finished_at

Integration
  id, kind (github|gitlab|bitbucket|dockerhub|...), account_ref, secret_ref, scopes[]

Extension
  id, name, version, kind (provider|plugin), source (path|url), enabled, permissions[]

Secret        (metadata only — never the plaintext secret)
  id, owner_type, owner_id, kind (ssh_key|api_token|db_password), storage_backend
  (os_keychain|encrypted_sqlite_blob), created_at, rotated_at

AuditEvent
  id, actor, action, target_type, target_id, result, created_at, metadata_json
```

**Relationships:** Project 1—N Domain, Project 1—N Service, Project 0/1—1 Deployment-target-set, Server 1—N Deployment, Pipeline 1—N PipelineRun, Domain 0/1—1 Certificate. Secrets are referenced by ID from Server/Integration/Extension, never embedded — this is the line that keeps "SQLite for structured state, not for secrets" (Section 23) enforceable.

---

## 7. Provider Architecture

**Problem with today's code:** there is no provider abstraction — `agent.Manager` just runs whatever command string it's given. That's fine for "start a script" but wrong as the foundation for "install PHP 8.3" or "manage a MySQL cluster."

**Design:** three tiers, matching Section 20's Providers/Integrations/Plugins split, but expressed as Go interfaces + a registry, not one giant interface:

```go
// internal/providers/contract.go

type RuntimeProvider interface {
    Kind() string                                  // "php", "node"
    DetectInstalled(ctx context.Context) ([]RuntimeVersion, error)
    Install(ctx context.Context, version string, progress ProgressFunc) error
    Remove(ctx context.Context, version string) error
    SetDefault(ctx context.Context, version string) error
}

type ServiceLifecycle interface {
    Start(ctx context.Context, svc Service) error
    Stop(ctx context.Context, svc Service, graceful bool) error
    Status(ctx context.Context, svc Service) (ServiceStatus, error)
    Logs(ctx context.Context, svc Service, tail bool) (io.ReadCloser, error)
}

type DatabaseProvider interface {
    ServiceLifecycle
    CreateDatabase(ctx context.Context, name string) error
    Versions(ctx context.Context) ([]string, error)   // separate from data dir — §9
    DataDir(ctx context.Context) (string, error)
}

type WebServerProvider interface {
    ServiceLifecycle
    ApplyVHost(ctx context.Context, domain Domain, project Project) (ConfigDiff, error)
    RemoveVHost(ctx context.Context, domain Domain) error
    Reload(ctx context.Context) error
    ManagedConfigs(ctx context.Context) ([]string, error) // distinguishes Volt-owned vs foreign config
}

type DomainProvider interface {
    Add(ctx context.Context, hostname string) error
    Remove(ctx context.Context, hostname string) error
    List(ctx context.Context) ([]string, error)
    Conflicts(ctx context.Context, hostname string) ([]string, error)
}

type SSLProvider interface {
    EnsureCA(ctx context.Context) (CAInfo, error)
    IssueCertificate(ctx context.Context, domain string) (Certificate, error)
    Renew(ctx context.Context, certID string) (Certificate, error)
    Revoke(ctx context.Context, certID string) error
    // TrustCA never runs implicitly — always a distinct, explicitly user-confirmed call
    TrustCA(ctx context.Context, confirmed bool) error
}

type RemoteProvider interface {
    Connect(ctx context.Context, server Server) (RemoteSession, error)
    // RemoteSession exposes Exec/FileSystem/Docker sub-interfaces, all over the one
    // SSH connection — no assumption a Volt daemon runs on the far end.
}

type GitProvider interface {
    ListRepos(ctx context.Context) ([]Repo, error)
    Clone(ctx context.Context, repo Repo, dest string) error
    Branches(ctx context.Context, repo Repo) ([]Branch, error)
    // ... pull/push/commits/tags/deploy-keys/webhooks
}

type DeploymentProvider interface {
    Deploy(ctx context.Context, spec DeploymentSpec) (*Deployment, error)
    Rollback(ctx context.Context, deploymentID string, scope RollbackScope) error
}
```

A `Registry` (`internal/providers/registry.go`) holds `map[string]RuntimeProvider` etc., populated at startup from (a) built-in providers compiled into the binary, and (b) discovered external providers (Section 21). API handlers and domain services depend only on the interface + registry, never on a concrete provider package — that's what lets `volt-provider-python` slot in without touching `internal/api` or `internal/domain`.

**How a community developer adds `volt-provider-python` without touching core:** see Section 21 — it's an external executable speaking a small JSON-over-stdio (or HTTP) protocol that implements one of these interfaces; the daemon shells out to it and treats it exactly like a built-in provider through the same `Registry`.

---

## 8. Cross-Platform Strategy

| Concern | macOS | Linux | Windows |
|---|---|---|---|
| Startup registration | `launchd` LaunchAgent (per-user, no sudo needed) via `launchctl` | `systemd` user or system unit via `systemctl --user`/`systemctl` | Windows Service via `golang.org/x/sys/windows/svc`, registered through an elevated install step |
| Service management (PHP-FPM, DBs, etc.) | Homebrew services (`brew services`) where installed via brew; else Volt-managed launchd agents per service | systemd user units per managed service, or direct process supervision when no systemd (containers) | Managed as Volt-supervised child processes (Windows lacks a systemd equivalent for user services) |
| Hosts file | `/etc/hosts`, requires privilege escalation (`osascript` admin prompt or a small privileged helper) | `/etc/hosts`, requires sudo — invoke via a one-time privileged helper binary, not the daemon itself running as root | `C:\Windows\System32\drivers\etc\hosts`, requires elevated write — UAC prompt via a helper |
| SSL trust store | `security add-trusted-cert` into the System or login keychain — explicit user confirmation required, never silent | Distro-dependent: `update-ca-certificates` (Debian/Ubuntu) or `update-ca-trust` (Fedora) — detect and branch | `certutil -addstore` into the Windows cert store, elevation required |
| Package installation | Homebrew (detect via `which brew`) | Detect `apt`/`dnf`/`pacman` via `PATH` probing, never assume one | `winget` first, `choco` fallback, else point to official installer |
| Privilege escalation | Single small `internal/platform/darwin/privileged` helper invoked via `osascript -e 'do shell script ... with administrator privileges'`, scoped to one operation at a time | A `pkexec`/`sudo -A` wrapper, same one-operation-at-a-time rule | A separate elevated helper process (UAC), same rule |
| Config/data paths | `~/Library/Application Support/Volt/` (or keep `~/.volt/` for parity — decide in Phase 0, see migration) | `~/.local/share/volt/` (XDG) | `%APPDATA%\Volt\` |

**Principle carried through every row:** the daemon itself never runs elevated. Anything requiring privilege (hosts file, cert trust, service install) goes through a narrowly-scoped helper invocation, triggered by an explicit user action in the UI, never automatically. This directly satisfies Section 22's "no silent OS trust store modification."

---

## 9. Security Architecture

Today's actual posture: token compared with `==` (not constant-time — low severity given it's localhost, but still wrong), Dev-mode flag fully bypasses auth, WS auth unreachable from real browsers, **file endpoints accept arbitrary absolute paths with no root restriction** (the most serious existing issue — a request to `DELETE /files?path=/` is not hypothetical, it's literally what the code allows today), no CSRF protection, no audit trail, no distinction between "read-only" and "destructive" operations.

Target model:
1. **Binding:** stays 127.0.0.1-only by default (already true) — never make this configurable without an explicit, loud opt-in and warning, since remote-server features (SSH) mean Volt itself never needs to be network-reachable to manage remote things.
2. **AuthN:** replace the single static token with a session model: first-run generates a token shown once in the browser (or terminal via `volt status`), browser exchanges it for a short-lived signed session cookie (HttpOnly, SameSite=Strict) on `/auth/token/verify`. Token itself moves to constant-time compare (`crypto/subtle.ConstantTimeCompare`) regardless.
3. **WS auth:** fix the real bug — since browsers can't set headers on the WS handshake, authenticate via a first-message protocol (client sends `{type:"auth", token}` as the first frame; server closes the connection if it doesn't arrive within N seconds) or via the session cookie (already sent automatically on same-origin WS upgrade) — prefer the cookie approach since it's already solved by AuthN above.
4. **CSRF/Origin:** validate `Origin` header on state-changing requests against `127.0.0.1`/`localhost` allowlist; reject cross-origin state changes even though the UI is same-origin (defense against a malicious page in another tab).
5. **Path sandbox (closes the current hole):** every provider that touches the filesystem receives a `security.Sandbox` bound to a specific allowed root (a project's path, or the Volt config dir) — `sandbox.Resolve(userPath)` rejects `..` traversal and symlink escapes and returns an error rather than a raw OS path. `internal/api/v1/files.go` must call through this for every operation; no endpoint accepts a bare absolute path again.
6. **Destructive-action confirmation:** delete/stop/uninstall/CA-trust/rollback endpoints require an explicit `confirm: true` field (checked server-side, not just a UI checkbox) — cheap, and prevents a stray script or extension from nuking things silently.
7. **Secrets:** SSH keys and integration tokens never stored as plaintext DB rows — `Secret.storage_backend` prefers OS keychain (macOS Keychain, Windows Credential Manager, Linux Secret Service via `go-keyring` or similar) and falls back to an AES-GCM-encrypted blob keyed by a machine-local key file (0600) only when no OS keychain is available (e.g., headless Linux).
8. **Audit log:** every mutating API call writes an `AuditEvent` row (actor, action, target, result) — this is what makes "who deployed to production and when" answerable later, and it's cheap to add now versus retrofit.
9. **SSH key handling:** prefer referencing the user's existing `ssh-agent` over importing/storing keys at all; if Volt must hold a key, it goes through the Secret abstraction above, never a plain file under the config dir.
10. **Sudo/privilege:** per Section 8 — narrow, one-shot, explicit, never the daemon process itself.

---

## 10. API Evolution

Current API is flat, unversioned, and mixes concerns (`/services` = persisted metadata, `/processes` = live state — same underlying "thing," two inconsistent views).

| Current | Problem | Proposed |
|---|---|---|
| `GET /services`, `GET /processes` | Two views of one concept, confusing | `GET /api/v1/services` (merges persisted + live status server-side) |
| `POST /services/start\|stop\|restart` | Fine shape, wrong prefix/versioning | `POST /api/v1/services/{id}/start\|stop\|restart` (RESTful, id in path not body) |
| `GET/POST/DELETE /files/*` | No sandboxing, inconsistent verbs (`write` for both create+update) | `GET /api/v1/projects/{id}/files?path=`, sandboxed to project root; `PUT` for write, `DELETE` for delete, `POST .../mkdir`, `.../move`, `.../copy` |
| `GET /logs/:id` | Fine, keep shape | `GET /api/v1/services/{id}/logs` |
| `GET /metrics` | Fine, keep shape, extend | `GET /api/v1/system/metrics`, add `GET /api/v1/services/{id}/metrics` |
| `POST /auth/token/verify` | Works but doesn't establish a session | Same path, but sets session cookie on success |
| — | Doesn't exist | `/api/v1/projects`, `/runtimes`, `/domains`, `/ssl`, `/databases`, `/docker`, `/servers`, `/deployments`, `/pipelines`, `/git`, `/integrations`, `/extensions` — added incrementally per phase, not all at once |
| `WS /ws/events` (global broadcast) | Every client gets every event | `WS /api/v1/ws` with subscription messages (`{subscribe: "service:<id>"}`), server filters server-side |

**Migration strategy:** keep the old flat routes mounted as deprecated aliases (log a warning, forward to the new handler) for one release cycle so any existing integration/script doesn't break instantly, then remove. Since there are effectively zero external consumers yet (pre-1.0, no public API contract published), this can be aggressive — but keeping aliases for one cycle costs almost nothing and matches "preserve backwards compatibility where practical."

---

## 11. UI Architecture

**Navigation** (per Section 26, mapped to what's real vs. future):
```
Overview | Projects | Runtimes | Services | Databases | Docker | Files
Domains | Servers | Deployments | Pipelines | Git | Extensions | Settings
```
Phase-gate which of these actually render vs. show a disabled "coming soon" — do not ship 13 nav items where 10 are empty shells; add nav entries as their phase lands (Section 17).

**Structural changes from today's `App.tsx`:**
- Replace the `useState<tab>` switcher with `react-router` — needed the moment there's a Project detail view, a Server detail view, etc. (deep-linkable URLs matter for a tool people bookmark/share internally).
- Introduce `lib/api.ts`: one typed client wrapping `fetch`, instead of every page hand-rolling `fetch(..., {headers})`. Centralizes auth-header/session handling and error formatting.
- Introduce `lib/ws.ts`: one shared WS connection with subscribe/unsubscribe, replacing `Logs.tsx` owning its own private socket — this is what makes the WS subscription model in Section 10 actually usable from the UI.
- State management: given the scope (projects, servers, deployments, live process status), move from ad-hoc `useState`+`useEffect` per page to a small global store (Zustand is a reasonable "boring" choice — avoid Redux ceremony, avoid rolling a custom context-per-domain pattern).
- WebSocket usage: real-time service status, log tail, deployment progress, Docker container state — polling stays only for things that don't have a natural push model (e.g., a one-off "list installed PHP versions" call).

**UX principle carried from the vision doc:** the dashboard is project-and-status centric, not a grid of unrelated cards — Overview should answer "what's running, what's broken, what needs attention" in one glance, not list 50 toggles.

---

## 12. CLI Architecture

Keep it exactly as small as Section 30 specifies. Today there is no CLI beyond flags on the daemon binary (`-port`, `-dev`) — this is the right foundation, just needs subcommands:

```
volt start      # launches the daemon (today: just running the binary)
volt stop       # signals the running daemon to exit gracefully
volt restart
volt status     # health + version + port, hits /api/v1/health locally
volt logs       # tails the daemon's own log (not a project's — that's the browser's job)
volt doctor     # environment checks: port availability, config dir permissions, disk space
volt update     # self-update via GoReleaser-published binaries
volt version
```
Implementation: subcommands of the *same* `cmd/volt` binary (via a minimal command dispatch, e.g. `cobra` or a hand-rolled switch — given "keep the CLI small," a hand-rolled `flag`-based dispatch is defensible and avoids a new dependency; use `cobra` only if command count grows past ~10). `volt start` with no subcommand and no flags should behave like today's default (`./voltpanel` boots the server) for continuity.

**Explicitly do not add** `volt project`, `volt deploy`, `volt server`, `volt pipeline` in early phases — Section 30 lists them as "potential future," and Section 20 of the vision doc is clear the browser owns management. Add a CLI verb only when there's a proven case the browser can't cover (e.g., scripting a deploy from another CI system) — and even then keep it a thin wrapper calling the same REST API the UI uses, never a second implementation of the logic.

---

## 13. CI/CD and Deployment Architecture

```
Git push/webhook → Pipeline trigger → Checkout → Build steps → SSH connect to Server
     → Deploy steps (rsync/git pull, install deps, run migrations) → Reload service
     → Health check (HTTP probe) → Record Deployment{status, duration, commit}
     → (on failure) optional automatic rollback of code, never of DB migrations
```
- **Pipeline definition:** YAML, parsed into the same `Pipeline`/`PipelineRun` entities from Section 6. Step types map directly onto existing/planned providers: `checkout` (GitProvider), `run` (RemoteProvider.Exec or local Exec), `ssh` (RemoteProvider), `healthcheck` (simple HTTP GET with expected status/timeout).
- **Execution engine** (`internal/pipeline`): a linear step runner with per-step status/log capture (reuses the exact log-piping pattern already proven in `agent.Manager.pipeLogs` — don't reinvent that part). Each step's log becomes part of the `PipelineRun`'s stored log, retrievable and WS-streamable exactly like a service's logs are today.
- **Secrets in pipelines:** referenced by name, resolved from the `Secret` store at execution time, never written into the stored pipeline YAML or logs (redact known secret values from captured output).
- **Rollback:** strictly the three-way split from Section 18 — `code_rollback_ref` (git revert/checkout), `artifact_rollback_ref` (redeploy a previous build artifact), and migration rollback is **never automatic**; the UI must show "this deployment included N migrations; rolling back code will NOT undo them" whenever `migration_rollback_supported` is false (the default).

This is explicitly Phase 9/10 work (Section 17) — flagged here architecturally so the `Deployment`/`Pipeline` entities in Section 6 are shaped correctly from the start, even though nothing executes until much later.

---

## 14. Local Domain and SSL Architecture

**Path from browser to app for `https://myapp.test`:**
```
Browser resolves "myapp.test"
  → OS resolver checks hosts file first (Volt wrote "127.0.0.1  myapp.test" there via
    DomainProvider("hosts").Add(), through the privileged helper from §8)
  → Browser connects to 127.0.0.1:443
  → A local reverse proxy (Nginx/Apache, managed by WebServerProvider) is listening there,
    with a vhost Volt generated (ApplyVHost) matching server_name myapp.test
  → That vhost presents the certificate SSLProvider.IssueCertificate() issued for
    myapp.test, signed by the Local CA
  → Browser trusts the cert because the Local CA was added to the OS/browser trust store
    via SSLProvider.TrustCA() — which only ever runs after an explicit user click, never
    automatically (§7, §22)
  → Vhost reverse-proxies to the project's actual dev server port (e.g., Node on :3000,
    or PHP-FPM via the webserver's fastcgi config)
```
**Certificate lifecycle:** `EnsureCA` creates the CA once (mkcert-compatible so users who already trust a mkcert CA don't need a second one — detect and offer to reuse an existing mkcert root if present); `IssueCertificate` per domain, checked for expiry on a schedule and surfaced in the UI (Section 22 wants explicit renewal, not silent auto-renewal without visibility — show it, let a "renew" action or an opt-in auto-renew toggle drive it).

**Conflict/duplicate prevention:** `DomainProvider.Conflicts()` checks both the hosts file and other projects' registered domains before `Add()` succeeds — this is what stops two projects silently fighting over `api.myapp.test`.

---

## 15. Service Startup / Dependency Architecture

```
OS boot
  → Volt daemon starts (launchd/systemd/Windows Service registration from §8)
  → Volt reads Service.depends_on[] and autostart flags from SQLite
  → Topological sort of autostart services by depends_on
  → For each service in order: ServiceLifecycle.Start(), then poll Status() until
    healthy (or a defined timeout) before starting anything that depends on it
  → Project-level processes (dev servers) start last, after their declared runtime/
    database/redis dependencies report healthy
```
- **Dependency resolution:** a simple Kahn's-algorithm topo-sort over `depends_on[]` is sufficient — no need for a general DAG scheduler library; detect cycles and refuse to start with a clear error rather than silently picking an order.
- **Health checks:** "healthy" means different things per provider (a DB might need to accept a TCP connection and respond to a ping query; a web server might need to answer an HTTP request; a generic process might just need to still be running after N seconds) — this is exactly why `ServiceLifecycle.Status()` is provider-specific rather than one generic "is the PID alive" check (which is roughly all `agent.Manager` does today).
- **Independent services stay independent:** per the vision doc's explicit caution — don't force e.g. Redis to depend on Nginx just because both exist; `depends_on[]` is empty by default and only set when a project or the user declares it.

---

## 16. Migration Strategy

Going from today's VoltPanel to Volt without breaking existing installs:

1. **Rename in one deliberate pass, not gradually:** module path (`github.com/alresiainc/alresia-voltpanel` → `github.com/alresiainc/volt` — confirm before touching `go.mod`, since it's a real breaking change for the module path), binary (`voltpanel` → `volt`), systemd unit (`devpanel.service` → `volt.service` — this also fixes the existing internal inconsistency), launchd label, config dir.
2. **Config dir migration:** on first run of the new binary, if the old `~/.alresia-voltpanel/` (or `~/.alresia-volt/`, given the doc/code mismatch — check both) exists and the new dir doesn't, copy `config.json`/`apps.json` across, import them into SQLite via a one-time migration path (`internal/storage/jsonlegacy`), and leave the old files untouched (never delete a user's old data automatically) with a log line noting the import.
3. **Storage migration:** JSON → SQLite migration runs once, is idempotent (safe to run twice), and is covered by a test that seeds a JSON fixture and asserts the SQLite result.
4. **API compatibility:** old flat routes remain mounted as deprecated aliases for one release (Section 10).
5. **Packaging migration:** goreleaser `project_name`, nfpm package name, and Homebrew formula name all move to `volt`; publish the Homebrew tap update as a new formula rather than mutating `voltpanel.rb` in place, and have the old `voltpanel` formula's `caveats`/description point at the new one for one release. If a `homebrew-tap` repo already has real users, this needs sign-off before automating (touches a repo outside this one).
6. **Documentation migration:** every doc gets rewritten for Volt naming as part of the phase that introduces the feature it documents, not a single end-of-project doc sweep (Section 32 folded into Section 17's phases).
7. **Binary self-update (`volt update`):** only meaningful once the rename has shipped at least one Volt release — sequence it after Phase 0/1.

---

## 17. Phased Implementation Plan

### Phase 0 — Foundation & Rename
- **Objective:** Fix existing correctness/security issues and rename, before adding any new feature surface.
- **Features:** module/binary/service rename (§16); fix file-manager path sandboxing (currently arbitrary FS access); fix WS auth (currently unreachable from browsers); constant-time token compare; fix CI's `|| true` swallowing failures; add macOS+Windows CI runners; remove dead packaging assets (stale `formula/voltpanel.rb`, pick one of WiX/NSIS).
- **Files/modules:** `go.mod`, `cmd/voltpanel`→`cmd/volt`, `internal/storage/store.go` (sandbox), `internal/ws/ws.go` (auth), `internal/server/server.go` (token compare), `.github/workflows/ci.yml`, `packaging/*`, `formula/*`.
- **Dependencies:** none — this is the prerequisite for everything else.
- **Migration concerns:** config-dir/JSON compatibility shim (§16.2) must land here, even though SQLite itself doesn't arrive until Phase 1.
- **Tests:** unit tests for the path sandbox (traversal attempts, symlink escape), WS auth handshake, token compare timing-safety (structural, not timing-measurement); CI test for the rename not breaking `make build`.
- **Acceptance criteria:** `volt` binary builds and serves; old JSON config auto-imports with a log line; `DELETE /files?path=/etc/passwd`-style requests are rejected; CI fails on a real test failure (verify by intentionally breaking one temporarily).

### Phase 1 — Core Daemon Restructure + SQLite Storage
- **Objective:** Decompose `server.go`, introduce SQLite, keep behavior identical from the outside.
- **Features:** `internal/api/v1/*` handler split; `internal/storage/sqlite.go` + migrations; JSON→SQLite import path; session-cookie auth on top of the existing token (§9.2).
- **Files/modules:** new `internal/api`, `internal/storage/sqlite.go`, `internal/security/auth.go`.
- **Dependencies:** Phase 0.
- **Migration concerns:** every existing endpoint must keep working during this refactor — do it as "extract handler, same route" commits, verified by the integration test script (fixed, no longer swallowing failures) after each step.
- **Tests:** repository-level unit tests per entity CRUD; integration test hitting every existing endpoint through the new router.
- **Acceptance criteria:** `scripts/integration_test.sh` passes without `|| true`; no behavior change visible from the UI; `go test ./...` covers storage + api packages meaningfully (not the current zero).

### Phase 2 — Provider Contracts + First Runtime Providers
- **Objective:** Introduce the `Provider` interfaces (§7) and implement 1–2 real ones (Node, PHP) to validate the abstraction before building five more.
- **Features:** `internal/providers/contract.go`; `internal/providers/runtime/node`, `.../php`; version detect/install/remove/default for those two only.
- **Files/modules:** `internal/providers/*`, `internal/api/v1/runtimes.go`, `internal/domain/runtime`.
- **Dependencies:** Phase 1 (needs SQLite for RuntimeVersion rows).
- **Migration concerns:** none new — additive.
- **Tests:** provider unit tests with a faked package manager (don't actually install Node/PHP in unit tests — mock the installer call, integration-test the real install path only in a platform-tagged, opt-in test).
- **Acceptance criteria:** UI can list detected Node/PHP versions on the developer's own machine and switch a "default."

### Phase 3 — Projects
- **Objective:** Introduce Project as the organizing concept.
- **Features:** Project CRUD, path validation, framework detection (Laravel/Next.js/generic Node/PHP first — extensible list), linking a project to a runtime version and a service.
- **Files/modules:** `internal/domain/project`, `internal/api/v1/projects.go`, `ui/src/pages/Projects`.
- **Dependencies:** Phase 2 (projects reference runtimes).
- **Migration concerns:** existing `Service`/`App` entries have no project — leave them as "unassigned services," don't force-migrate.
- **Tests:** detection-engine unit tests per framework fixture (a tiny fixture repo per framework, checked into `testdata/`).
- **Acceptance criteria:** pointing Volt at a local Laravel and a local Next.js repo correctly identifies both and their default run command.

### Phase 4 — Domains + SSL
- **Objective:** `.test` domains and local HTTPS, the first OS-privileged feature.
- **Features:** hosts-file `DomainProvider`, local CA + cert issuance `SSLProvider`, explicit trust-install flow in UI, per-OS privileged-helper pattern (§8).
- **Files/modules:** `internal/providers/domainprovider/hosts`, `internal/providers/ssl/localca`, `internal/platform/{darwin,linux,windows}` (hosts write, cert trust), `internal/api/v1/domains.go`, `.../ssl.go`.
- **Dependencies:** Phase 3 (domains attach to projects).
- **Migration concerns:** none.
- **Tests:** hosts-file writer unit tests against a temp file (never the real `/etc/hosts` in CI); manual/platform-tagged tests for actual trust-store operations, run in a dedicated matrix job per OS (§8, §33) rather than assumed to work.
- **Acceptance criteria:** on the developer's own macOS machine, adding `myapp.test` and enabling SSL makes `https://myapp.test` load with a trusted cert, and removing it cleans up the hosts entry.

### Phase 5 — Services + Real Startup Registration
- **Objective:** Replace the no-op `internal/system` and merge the dual `apps.json`/live-process model into one `Service` concept with dependency ordering.
- **Features:** real launchd/systemd/Windows service registration for the Volt daemon itself; unified `ServiceLifecycle`; dependency-ordered autostart (§15); graceful stop (not just `Process.Kill()`); crash detection + restart policy.
- **Files/modules:** `internal/platform/*` (service registration), `internal/domain/service` (replaces `internal/agent`), `internal/api/v1/services.go`.
- **Dependencies:** Phase 1 (SQLite), independent of Phases 2–4 otherwise.
- **Migration concerns:** this retires `internal/agent.Manager` — carry over its log-piping logic rather than rewriting it (§2 flags this as reusable).
- **Tests:** dependency-topo-sort unit tests including cycle detection; crash-detection test (kill a child process externally, assert Volt notices and restarts per policy); platform-tagged service-registration tests.
- **Acceptance criteria:** `volt` installed via Homebrew actually auto-starts on login (today it doesn't reliably, since `internal/system` does nothing); stopping a service is graceful with a timeout before force-kill.

### Phase 6 — Docker
- **Objective:** Local Docker management.
- **Features:** container/image/volume/network listing, start/stop/restart, logs, Compose awareness, basic terminal (exec).
- **Files/modules:** `internal/providers/docker`, `internal/api/v1/docker.go`, `ui/src/pages/Docker`.
- **Dependencies:** Phase 1 only (independent of projects/domains — can actually be pulled earlier if you want Docker sooner; sequenced here for team-bandwidth reasons, not a hard technical dependency).
- **Migration concerns:** none.
- **Tests:** integration tests against the Docker Engine API using a disposable test container; skip cleanly (not fail) in environments without Docker.
- **Acceptance criteria:** UI shows real local containers and can start/stop/tail logs on one.

### Phase 7 — Remote Servers + SSH
- **Objective:** First remote-management capability.
- **Features:** Server CRUD with SSH credential setup (key/agent preferred, §9.9), connection test, remote terminal, remote file browser, remote service status — all without assuming a Volt daemon on the far end.
- **Files/modules:** `internal/providers/remote/ssh`, `internal/domain/server`, `internal/api/v1/servers.go`, `ui/src/pages/Servers`.
- **Dependencies:** Phase 1 (secrets need the `Secret` model) + Phase 5's `ServiceLifecycle` shape (remote services should look like local ones to the UI).
- **Migration concerns:** none — fully new surface.
- **Tests:** SSH provider unit tests against a local sshd (Docker container in CI) — real connection, not mocked, since SSH auth edge cases are exactly where mocks lie.
- **Acceptance criteria:** add a real VPS by hostname+key, see its disk/CPU/mem, browse its files, and view a running service's status, over SSH only.

### Phase 8 — Git Integration
- **Objective:** GitHub first (most common), interface designed for GitLab/Bitbucket to follow.
- **Features:** OAuth/PAT connection, repo listing, clone, branches, pull, commits.
- **Files/modules:** `internal/integrations/git/github` (+ `contract.go` shared interface), `internal/api/v1/git.go`.
- **Dependencies:** Phase 3 (clone target is a Project), Phase 1 (Integration/Secret models).
- **Migration concerns:** none.
- **Tests:** contract tests run against a fixture repo (a scratch GitHub repo owned by the project, or a local bare git repo for anything not GitHub-API-specific).
- **Acceptance criteria:** connect a GitHub account, clone a repo into a new Project, and have Volt auto-run Phase 3's detection on it.

### Phase 9 — Deployment
- **Objective:** Ship code to a Server.
- **Features:** DeploymentTarget config per project, manual "Deploy" action, deployment history, three-way rollback split (§18).
- **Files/modules:** `internal/domain/deployment`, `internal/providers/contract.go` additions (`DeploymentProvider`), `internal/api/v1/deployments.go`.
- **Dependencies:** Phase 7 (SSH) + Phase 8 (Git) + Phase 5 (service reload on the remote end).
- **Migration concerns:** none.
- **Tests:** end-to-end deploy test against a disposable container-as-server in CI (clone a fixture repo, deploy, verify a health endpoint responds).
- **Acceptance criteria:** deploy a real small app to a test VPS/container from the UI, see it live, roll back the code cleanly.

### Phase 10 — CI/CD Pipelines
- **Objective:** Automate what Phase 9 made possible manually.
- **Features:** YAML pipeline editor + engine, triggers (manual/push/webhook), step log capture, health-check step.
- **Files/modules:** `internal/pipeline`, `internal/domain/pipeline`, `internal/api/v1/pipelines.go`, `ui/src/pages/Pipelines`.
- **Dependencies:** Phase 9 (pipelines orchestrate deployments), Phase 8 (push triggers need webhooks from Git provider).
- **Migration concerns:** none.
- **Tests:** pipeline engine unit tests (step ordering, failure propagation, secret redaction in logs); one full webhook-triggered E2E test.
- **Acceptance criteria:** a push to a test repo's main branch triggers a pipeline that deploys and health-checks automatically.

### Phase 11 — Extension Ecosystem
- **Objective:** Let external providers register without core changes.
- **Features:** `internal/pluginhost` protocol (§21), a manifest format, a minimal "install extension from URL/path" flow, at least one real third-party-style provider built against the protocol as a dogfood test (e.g., a Python runtime provider, built as if by an outside contributor).
- **Files/modules:** `internal/pluginhost`, `internal/domain/extension`, `internal/api/v1/extensions.go`.
- **Dependencies:** Phase 2 (provider contracts must be stable before externalizing them).
- **Migration concerns:** versioning the protocol from day one (Section 21) so it doesn't break on the first Volt update.
- **Tests:** protocol conformance tests; a sample external provider used as the test fixture itself.
- **Acceptance criteria:** the sample Python provider runs entirely as an external process and appears in the Runtimes UI identically to built-in Node/PHP.

### Phase 12 — Hardening / Release
- **Objective:** 1.0 readiness.
- **Features:** full security review against §9's checklist, docs sweep (§32) for every subsystem shipped so far, platform test matrix completion (§33), performance pass on metrics/WS under load, final packaging cleanup.
- **Files/modules:** touches everything, adds nothing new architecturally.
- **Dependencies:** all prior phases.
- **Migration concerns:** this is where the deprecated flat-API aliases (§10) actually get removed.
- **Tests:** full platform matrix (macOS/Linux/Windows) for every OS-touching feature, load test on WS hub with many subscribed clients.
- **Acceptance criteria:** a fresh install on each of the three OSes, from binary to "project running with HTTPS domain," works end to end with no manual file edits.

---

## 18. Dependency Graph

```
Phase 0 (Foundation/Rename)
   └─▶ Phase 1 (SQLite + API restructure)
          ├─▶ Phase 2 (Provider contracts + Node/PHP)
          │      ├─▶ Phase 3 (Projects)
          │      │      ├─▶ Phase 4 (Domains + SSL)
          │      │      └─▶ Phase 8 (Git) ──┐
          │      └─▶ Phase 11 (Extensions, needs stable contracts)
          ├─▶ Phase 5 (Services + real startup)
          │      └─▶ Phase 7 (Remote/SSH, needs ServiceLifecycle shape) ─┐
          └─▶ Phase 6 (Docker, independent)                              │
                                                                          ▼
                                                    Phase 9 (Deployment) ◀── Phase 8
                                                             │
                                                             ▼
                                                    Phase 10 (CI/CD)
                                                             │
                                                             ▼
                                                    Phase 12 (Hardening/Release)
```
Phases 6 (Docker) and the Node/PHP half of Phase 2 can run in parallel with Phase 3/4/5 if more than one contributor is available — they don't share files.

---

## 19. Risks

| Risk | Mitigation |
|---|---|
| Scope explosion — vision doc covers ~15 subsystems | Strict phase gating (§17); explicitly deferred list (§20); no phase starts before its dependency phase's acceptance criteria are met |
| OS-privileged operations (hosts, cert trust, service install) break trust if done wrong | Never run the daemon elevated; every privileged op is a single, explicit, user-confirmed, narrowly-scoped helper call (§8, §22); platform-tagged tests, not "should work" assumptions |
| SSH/remote-server surface is security-sensitive from day one | Secrets never touch plaintext DB (§9.7); prefer ssh-agent over key storage; connection-test before any destructive remote action is offered |
| Go native plugins chosen for "speed" and causing portability breakage later | Explicitly reject Go plugins in §21 — commit to out-of-process providers up front |
| SQLite migration corrupting existing users' JSON state | Migration is additive-only (never deletes old JSON files), idempotent, covered by a fixture-based test before it ships (§16.3) |
| CI silently passing broken code (today's `\|\| true` pattern) | Fixed in Phase 0, before any other phase — a hard gate, not a nice-to-have |
| Naming rename breaking existing installs silently | Config-dir/JSON import shim + deprecated route aliases for one release (§16) |
| Provider interface designed too early, wrong shape discovered after 5 providers built against it | Validate with exactly 2 runtime providers (Phase 2) before committing the interface, per the phase plan — don't build 5 providers against an unvalidated contract |
| Rollback UX implying safety it doesn't have (DB migrations) | `migration_rollback_supported` defaults false and is surfaced in the UI, never silently assumed true (§6, §13) |

---

## 20. What NOT to Build Yet

- Cloud provider integrations (AWS/DigitalOcean/Cloudflare) — Section 20 lists them as future "Integrations," but nothing in the current codebase or near-term phases needs them; defer past Phase 12.
- A general WASM or Go-native-plugin extension runtime — start with the out-of-process protocol (§21) and only revisit WASM if a real performance/isolation need shows up (it likely won't for "call an installer and report status").
- Multi-user/team accounts, RBAC beyond "the one local operator" — the entire security model (§9) assumes a single trusted operator per daemon instance, matching the product's "your dev environment" framing; multi-user is a different product decision, not a phase.
- A full CI/CD YAML DSL with conditionals/matrices/reusable-templates — Phase 10's engine should stay a linear step runner; don't build a GitHub-Actions-equivalent DSL until real usage demands it.
- Cross-project dependency graphs (Project A's API as a dependency of Project B) — projects stay independent entities for the foreseeable phases.
- Windows Docker/WSL2-specific deep integration — Phase 6 targets the Docker Engine API generically; WSL2-specific ergonomics are a later refinement, not a blocker.
- A second CLI binary (`volt-cli` as a separate artifact) — keep CLI as subcommands of the one daemon binary (§12) until there's a concrete reason to split.
- Rewriting the Gin/embed serving foundation, the WS hub's core loop, or the goreleaser cross-compile matrix — these already work; touch them only where a specific gap (§3) requires it.

---

## 21. First Implementation Slice

**Phase 0 in full, plus the first half of Phase 1 (API decomposition only, SQLite storage can be its own follow-up commit).**

Concretely, the first slice is:
1. Fix the file-manager path-traversal vulnerability (`internal/storage` sandbox) — this is a real, exploitable gap in code that already exists and already ships; it's not "architecture," it's a bug, and it's the highest-risk item in the entire repo today.
2. Fix WS auth so it's actually enforceable from a browser.
3. Fix the CI pipeline so `|| true` stops hiding failures, and add a real (even if minimal) test for #1 and #2 above.
4. Do the rename pass (`voltpanel` → `volt`, `devpanel.service` → `volt.service`, config dir unification) with the JSON-compat import shim.
5. Decompose `server.go`'s inline routes into `internal/api/v1/*` handler files with the exact same external behavior — no new endpoints yet.

**Why this slice and not "start on Projects" or "start on Providers":** every later phase depends on the daemon having a trustworthy security boundary and a codebase that isn't one 190-line function. Building Providers (Phase 2) or Projects (Phase 3) on top of today's unsandboxed file manager and no-op service registration would mean re-doing security work under time pressure later, and building on top of `server.go`'s current shape would mean every subsequent PR fights the same monolithic function. This slice produces no new user-visible features, but it's the one that makes every subsequent phase estimate in the plan above actually achievable rather than optimistic — and it directly resolves the one finding in this review (§3, file-manager sandboxing) that qualifies as an active vulnerability rather than a gap.

---

## Open items needing a decision before/during Phase 0

- Confirm go.mod module path target: `github.com/alresiainc/volt` (breaking change — needs explicit sign-off since it touches import paths everywhere).
- Confirm config-dir target: `~/.volt/` vs OS-native paths per §8's table (XDG on Linux, Application Support on macOS, %APPDATA% on Windows) vs keeping one flat `~/.volt/` everywhere for simplicity in early phases.
- Decide now vs. later whether `homebrew-tap` repo gets touched as part of Phase 0, since that's a separate repo outside this one.
- `.hide` file with an apparent live GitHub token — rotate/remove, unrelated to the plan but sitting in the working tree.
