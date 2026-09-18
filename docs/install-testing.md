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

Note: as of the Phase 0–1 hardening pass, `internal/system`'s
Install/Uninstall are still no-ops for the *daemon's own* startup
registration on any OS — the packaging-level systemd/launchd/WiX assets
above are the only real registration path right now. Real, code-driven
service registration is Phase 5 work; check
`.claude/plans/volt-implementation-plan.md`'s progress log for current
status.

## QA Checklist

- Start the binary: `./dist/voltpanel`
- Visit `http://127.0.0.1:7788/health`
- `POST /api/v1/auth/token/verify` with the token from `~/.volt/config.json`
  to get a session cookie, then start a service via
  `POST /api/v1/services/:id/start` and watch its logs over
  `WS /api/v1/ws` (send `{"type":"auth","token":"..."}` as the first
  message unless you're already carrying the session cookie).
