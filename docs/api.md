# API

All routes below are under `/api/v1` (the old flat routes from the pre-1.0
scaffold have been removed — this is the only API surface). `GET /health`
(no `/api/v1` prefix) stays as a plain liveness check.

## Auth

Every route except `/health`, `/api/v1/auth/token/verify`, and the pipeline
webhook endpoint (its own HMAC signature is its auth — see Pipelines below)
requires either:

- an `X-Volt-Token` header matching the daemon's token (see `config.json`
  in the config dir, or the terminal output on first run), or
- a `volt_session` cookie, obtained by `POST`ing that token once to
  `/api/v1/auth/token/verify`.

`POST /api/v1/auth/token/verify { token }` verifies the token and, on
success, sets `volt_session` as an HttpOnly, SameSite=Strict cookie (24h
TTL). The browser UI uses this so the raw token never needs to sit in
JS-reachable storage beyond the initial paste.

Mutating requests (`POST`/`PUT`/`DELETE`/`PATCH`) are rejected with `403`
if they carry a cross-origin `Origin` header (defense against a malicious
page in another tab riding the session cookie).

**Destructive-action confirmation**: delete, stop, uninstall, CA-trust,
and rollback endpoints require an explicit `confirm=true` (query param) or
`{"confirmed": true}` (body, for `ssl/ca/trust`) — checked server-side,
never just a UI affordance.

## Runtimes

- `GET /api/v1/runtimes` — re-detects every registered provider (Node,
  PHP, plus any enabled Extension of kind `runtime`) and returns the
  merged, persisted result.
- `POST /api/v1/runtimes/:kind/detect`
- `POST /api/v1/runtimes/:kind/versions/:version/default`

## Services

- `GET /api/v1/services` — merges persisted state with any live process
  state the daemon holds in memory.
- `POST /api/v1/services/:id/start { name, command, args[], cwd, env }`
- `POST /api/v1/services/:id/stop?confirm=true` — graceful (SIGTERM, then
  a timeout, then force-kill) by default; `&graceful=false` hard-kills.
- `POST /api/v1/services/:id/restart`
- `GET /api/v1/services/:id/logs?tail=true`
- `GET /api/v1/services/:id/metrics` — CPU/RSS for the running process.

## Files

Sandboxed to the daemon's file-manager root (currently the user's home
directory; traversal and symlink escapes are rejected — see
`internal/security.Sandbox`).

- `GET /api/v1/files?path=...`
- `PUT /api/v1/files { path, content }`
- `DELETE /api/v1/files?path=...&confirm=true`
- `POST /api/v1/files/mkdir { path }`
- `POST /api/v1/files/move { src, dst }`
- `POST /api/v1/files/copy { src, dst }`
- `POST /api/v1/files/upload?path=...` (multipart form, field `file`)

## Projects

- `GET /api/v1/projects`
- `POST /api/v1/projects { name, path }` — validates the path, runs
  framework detection (Laravel/Next.js/generic Node/generic PHP).
- `GET /api/v1/projects/:id`
- `DELETE /api/v1/projects/:id?confirm=true`
- `POST /api/v1/projects/:id/detect` — re-run detection.

## Docker

Every handler returns a clean `503 {"available": false}` (never a 500)
when no Docker daemon is reachable.

- `GET /api/v1/docker/containers` (`?group=compose` to group by the
  `com.docker.compose.project` label)
- `GET /api/v1/docker/containers/:id`
- `POST /api/v1/docker/containers/:id/start|stop|restart`
- `GET /api/v1/docker/containers/:id/logs?tail=200`
- `POST /api/v1/docker/containers/:id/exec { cmd: [...] }`
- `GET /api/v1/docker/images` / `/docker/volumes` / `/docker/networks`

## Domains + SSL

- `GET/POST /api/v1/domains { hostname, projectId?, port? }` — `POST`
  checks for hosts-file conflicts (a foreign, non-Volt-managed entry)
  before writing.
- `DELETE /api/v1/domains/:id?confirm=true`
- `GET /api/v1/domains/:id/conflicts`
- `POST /api/v1/ssl/ca/ensure` — creates the local CA once, idempotent.
- `POST /api/v1/ssl/certificates { domainId }` — issues a leaf cert.
- `POST /api/v1/ssl/ca/trust { confirmed: true }` — the **only** endpoint
  that modifies the real OS/browser trust store, and only when
  `confirmed` is explicitly `true`.

## Extensions

External providers (§7/§17 Phase 11) speaking a small newline-delimited
JSON protocol over stdio — see `internal/pluginhost`.

- `GET/POST /api/v1/extensions { path }` — installs from a local
  directory containing a `volt-extension.json` manifest (URL install is
  intentionally not implemented — a materially bigger trust question).
- `DELETE /api/v1/extensions/:id?confirm=true`
- `POST /api/v1/extensions/:id/enable` — loads the subprocess, registers
  it into the same provider registry built-in providers use.
- `POST /api/v1/extensions/:id/disable`

## Git integrations

- `POST /api/v1/integrations { kind: "github", token }` — validates the
  PAT against the real API before storing it via the Secret abstraction
  (never as a plaintext DB row).
- `GET /api/v1/integrations` / `DELETE /api/v1/integrations/:id`
- `GET /api/v1/git/repos?integrationId=...`
- `GET /api/v1/git/repos/:owner/:repo/branches?integrationId=...`
- `POST /api/v1/git/clone { integrationId, repo: {id, name, cloneUrl}, dest }`

## Remote servers (SSH)

- `GET/POST /api/v1/servers { name, hostname, port, username, authMethod, key? }`
  — `authMethod` is `"agent"` (preferred, uses the local ssh-agent) or
  `"key"` (the provided private-key PEM is stored via the Secret
  abstraction, never returned or logged).
- `DELETE /api/v1/servers/:id?confirm=true` — also deletes the stored key.
- `POST /api/v1/servers/:id/test` — connect, run a no-op, close.
- `GET /api/v1/servers/:id/metrics` — best-effort uptime/load/memory.
- `POST /api/v1/servers/:id/exec { command }` — every call audit-logged
  with the full command string.
- `GET /api/v1/servers/:id/files?path=.` / `/files/read?path=...`
- `PUT /api/v1/servers/:id/files { path, content }`

## Deployments

- `GET/POST /api/v1/deployment-targets?projectId=...` — a reusable
  per-project deploy config (repo, server, branch, deploy path,
  install/restart commands, optional health-check URL).
- `DELETE /api/v1/deployment-targets/:id?confirm=true`
- `GET /api/v1/deployments?projectId=...` — history.
- `POST /api/v1/deployments { targetId }` — runs synchronously: checkout
  over SSH, optional install, optional restart, optional health check.
  A failed deploy is still recorded (`status: "failed"`), never discarded.
- `GET /api/v1/deployments/:id/log`
- `POST /api/v1/deployments/:id/rollback?confirm=true` — checks out the
  previous deployment's commit and restarts. `migrationRollbackSupported`
  is always `false` in every response — rolling back code here never
  implies a database migration was undone.

## Pipelines

A linear step runner over a small YAML definition (no conditionals,
matrices, or templates) — see `internal/pipeline`.

```yaml
steps:
  - name: test
    run: npm test          # local shell command, on the daemon's own host
  - name: deploy
    deploy: <deployment-target-id>   # delegates to Deployments above
  - name: verify
    healthcheck: { url: https://myapp.test/health, expectedStatus: 200 }
  # an "ssh" step is also available: { ssh: { serverId, command } }
```

- `GET/POST /api/v1/pipelines?projectId=... { name, definitionYaml }` —
  the create response includes `webhookSecret`/`webhookPath` **once**;
  every later response (list/get) omits them entirely.
- `DELETE /api/v1/pipelines/:id?confirm=true`
- `POST /api/v1/pipelines/:id/run` — manual trigger.
- `GET /api/v1/pipelines/:id/runs` — history with per-step results.
- `POST /api/v1/pipelines/:id/webhook` — **not** under the normal
  session/token auth (an external git host can't present either); instead
  requires header `X-Volt-Signature: sha256=<hex HMAC-SHA256 of the raw
  body, keyed by that pipeline's webhookSecret>` — the same scheme
  GitHub/GitLab/Bitbucket webhooks use. A missing or incorrect signature
  is rejected with `401` before anything runs.

## System

- `GET /api/v1/system/metrics` — CPU/mem/disk + a fixed common-port scan.

## WebSocket

`GET /api/v1/ws` — a single broadcast channel today (per-topic
subscriptions are future work). Auth: if a valid `volt_session` cookie is
present at handshake time, the connection is authorized immediately;
otherwise the first message must be `{"type":"auth","token":"..."}`
matching the daemon's token, sent within 5 seconds or the server closes
the connection. This two-path design exists because browsers cannot set
custom headers on a WebSocket handshake, so a header-based check (like the
HTTP API's) is unreachable from real browser JS.

Emits `{"type":"log","id":"<serviceId>","data":"<line>\n","ts":<unixMilli>}`
per log line from any running service.

## Audit log

Every mutating call above writes a row to the `audit_events` SQLite table
(actor, action, target type/id, result, timestamp, and — for a handful of
security-relevant calls like `server.exec`/`file.write` — a small
`metadata_json` blob, e.g. the executed command string). Not yet exposed
via the API itself; inspect the daemon's `volt.db` directly if needed.
