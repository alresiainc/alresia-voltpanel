# API

All routes below are under `/api/v1` (the old flat routes from the pre-1.0
scaffold have been removed — this is the only API surface). `GET /health`
(no `/api/v1` prefix) stays as a plain liveness check.

## Auth

Every route except `/health` and `/api/v1/auth/token/verify` itself
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

## Services

- `GET /api/v1/services` — merges persisted state with any live process
  state the daemon holds in memory.
- `POST /api/v1/services/:id/start { name, command, args[], cwd, env }`
- `POST /api/v1/services/:id/stop`
- `POST /api/v1/services/:id/restart`
- `GET /api/v1/services/:id/logs?tail=true`
- `GET /api/v1/services/:id/metrics` — CPU/RSS for the running process.

## Files

Sandboxed to the daemon's file-manager root (currently the user's home
directory; traversal and symlink escapes are rejected — see
`internal/security.Sandbox`).

- `GET /api/v1/files?path=...`
- `PUT /api/v1/files { path, content }`
- `DELETE /api/v1/files?path=...&confirm=true` — `confirm=true` is
  required server-side, not just a UI affordance.
- `POST /api/v1/files/mkdir { path }`
- `POST /api/v1/files/move { src, dst }`
- `POST /api/v1/files/copy { src, dst }`
- `POST /api/v1/files/upload?path=...` (multipart form, field `file`)

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
(actor, action, target type/id, result, timestamp) — not yet exposed via
the API itself, inspect the daemon's `volt.db` directly if needed.
