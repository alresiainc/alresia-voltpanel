# Install & Testing

## Dev

- `make dev` (backend only, `DEV=1` relaxes auth for local dev) and
  `make ui-dev` (Vite dev server for the frontend).
- `make build` to produce `dist/voltpanel` with the real UI embedded
  (runs `make ui-build` first, which needs `pnpm`; if pnpm blocks on
  `[ERR_PNPM_IGNORED_BUILDS]`, run `pnpm approve-builds --all` once inside
  `ui/`).
- `make test` runs `go test ./...`.
- `bash scripts/integration_test.sh` runs the built binary in `-dev` mode
  and exercises health/start/logs/metrics end to end via curl.

## Install as service

- Linux: install the deb/rpm produced by GoReleaser; systemd unit at
  `/lib/systemd/system/voltpanel.service`.
- macOS: use the launchd plist in `packaging/com.alresia.volt.plist`.
- Windows: use the WiX template in `packaging/wix/template.wxs` to
  generate an MSI (not yet wired into the GoReleaser pipeline — manual
  step today).

Note: `internal/system`'s Install/Uninstall (Phase 5) now generate real
launchd/systemd-user/Windows-Service registration content
(`internal/platform/{darwin,linux,windows}`) instead of doing nothing —
but nothing in the daemon calls them automatically, and there's no API
endpoint wired to trigger one either. Actually registering the daemon as
a startup service today still goes through the packaging-level
systemd/launchd/WiX assets above (via the deb/rpm postinstall script or a
manual step), which is the safer, well-understood path; the code-driven
registration exists and is unit-tested (file-generation only — it's
never been invoked for real, deliberately, since that would leave a
persistent background service behind as a side effect of running tests).

Update-check-only: `voltpanel update` checks the latest published GitHub
release against the running binary's version and tells you how to
install it (same method you used originally) — it never replaces the
running binary automatically.

## QA Checklist

- Start the binary: `./dist/voltpanel`
- Visit `http://127.0.0.1:7788/health`
- `POST /api/v1/auth/token/verify` with the token from `~/.volt/config.json`
  to get a session cookie, then start a service via
  `POST /api/v1/services/:id/start` and watch its logs over
  `WS /api/v1/ws` (send `{"type":"auth","token":"..."}` as the first
  message unless you're already carrying the session cookie).
- `voltpanel stop` from a second terminal — should cleanly shut down
  (confirm with `voltpanel status` afterward, and that `~/.volt/daemon.pid`
  is gone).
- Create a Project, then a Server + a Deployment target pointed at a
  real (or intentionally unreachable, to test the failure path) host, and
  run a Deploy — check `/api/v1/deployments` history either way.
- Create a Pipeline with a `run` step, trigger it via `POST
  /pipelines/:id/run`, then again via `POST /pipelines/:id/webhook` with a
  correctly-computed `X-Volt-Signature` header — and once more with a
  wrong one, which must come back `401`.
