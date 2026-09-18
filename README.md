# VoltPanel

A local-first control plane for your dev environments, servers, and
deployments — reachable entirely from your browser. Single Go binary,
embeds its own UI, no external database.

- **Projects** — point it at a repo, it detects the framework (Laravel,
  Next.js, generic Node/PHP) and its run command.
- **Services** — start/stop/restart any process, graceful shutdown, crash
  detection with a restart policy, dependency-ordered autostart.
- **Runtimes** — detects installed Node/PHP versions, switch defaults.
- **Docker** — list/start/stop/exec containers, grouped by Compose project.
- **Local domains + HTTPS** — `myapp.test` via a managed hosts-file entry
  and a local CA (never trusted automatically — you decide when).
- **Remote servers** — connect over SSH (agent or key), browse files, run
  commands, see basic metrics.
- **Deployments** — checkout → install → restart → health-check over SSH,
  with deployment history and code-level rollback.
- **Pipelines** — a small YAML step runner, triggered manually or by a
  signed webhook.
- **Extensions** — external providers run as a separate process speaking
  a small, versioned protocol — not Go plugins, not WASM.
- **File manager**, log streaming over WebSocket, and system metrics.

Everything binds to `127.0.0.1` only — VoltPanel never listens on your
network, and the one operation that touches your OS's trust store (local
HTTPS) only ever runs when you explicitly confirm it.

## Install

Grab the latest release for your platform from the
[Releases page](https://github.com/alresiainc/alresia-voltpanel/releases):

- **macOS / Linux**: download the `.tar.gz` for your OS/arch, or the
  `.deb`/`.rpm` package (Linux) — installing the package also registers
  a systemd unit.
- **Windows**: download the `.zip`.

Homebrew support is on the way (the tap isn't public yet).

Once installed, run `voltpanel` (or `voltpanel start`) and visit
`http://127.0.0.1:7788`. The daemon token needed to log in is printed on
first run and stored in `~/.volt/config.json`.

```
voltpanel start | stop | restart | status | logs | doctor | update | version
```

See [docs/cli.md](docs/cli.md) for the full CLI reference and
[docs/install-testing.md](docs/install-testing.md) for service
registration details.

## Building from source

Prereqs: Go 1.25+, pnpm 9+, Node 20+ (for building the UI only).

```sh
make build     # builds the UI (ui/dist) and the voltpanel binary with it embedded
./dist/voltpanel -dev
# Visit http://127.0.0.1:7788
```

For UI development with hot reload:

```sh
make ui-dev    # Vite dev server on http://localhost:5173
make dev       # backend only, DEV=1 relaxes auth for local dev
```

## Docs

- [docs/architecture.md](docs/architecture.md) — how it's put together.
- [docs/api.md](docs/api.md) — the full `/api/v1/*` reference.
- [docs/cli.md](docs/cli.md) — CLI subcommands.
- [docs/install-testing.md](docs/install-testing.md) — building, testing,
  and service registration.
- [.claude/plans/volt-implementation-plan.md](.claude/plans/volt-implementation-plan.md)
  — the full phased architecture/implementation plan and progress log.

## License

Apache License 2.0 — see [LICENSE](LICENSE).
