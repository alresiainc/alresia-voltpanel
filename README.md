# Alresia VoltPanel

A production-grade local server management panel. Single Go binary embeds a React UI.

- Local-only HTTP UI at 127.0.0.1:<port> (default 7788, auto-increment if taken)
- Process manager for NodeJS, PHP, and custom commands
- File manager, log streaming via WebSocket, system metrics
- Cross-platform service templates and packaging via GoReleaser

## Quickstart (dev)

Prereqs: Go 1.22+, pnpm 9+, Node 20+ (for building UI only)

```sh
make build     # builds UI (ui/dist) and voltpanel binary with embedded UI
./dist/voltpanel -dev
# Visit http://127.0.0.1:7788
```

For development with live UI:

```sh
make ui-dev    # runs Vite on http://localhost:5173
make dev       # runs backend; UI proxied to Vite or serves embedded if built
```

## Release snapshot (all installers)

```sh
go install github.com/goreleaser/goreleaser/v2@latest
make release-snapshot
```

See docs/install-testing.md for detailed install/service registration and testing.
