# Architecture

- Single Go binary embeds React UI (Vite build output).
- Gin HTTP server, WebSocket via gorilla/websocket.
- Process manager using os/exec, logs to ~/.alresia-volt/logs.
- Config and apps metadata in ~/.alresia-volt/config.json and apps.json.
- Packaging via GoReleaser; systemd/launchd/WiX templates provided.
