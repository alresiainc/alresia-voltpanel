# Install & Testing

## Dev

- make dev (backend) and make ui-dev (frontend dev)
- make build to produce dist/voltpanel with embedded UI

## Install as service

- Linux: install deb/rpm produced by GoReleaser; systemd unit at /lib/systemd/system/devpanel.service
- macOS: use launchd plist in packaging/com.alresia.volt.plist
- Windows: use WiX template to generate MSI

## QA Checklist

- Start binary: ./dist/voltpanel
- Visit http://127.0.0.1:7788/health
- Start a process via POST /services/start and watch logs via WS /ws/events
