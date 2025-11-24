# API

- GET /health
- POST /auth/token/verify
- GET /services
- POST /services/start { id, name, command, args[], cwd, env }
- POST /services/stop { id }
- POST /services/restart { id }
- GET /processes
- GET /files/list?path=...
- POST /files/write { path, content }
- DELETE /files?path=...
- GET /logs/:id?tail=true
- WS /ws/events: emits { type: 'log'|'process_start'|'process_exit', ... }

Add header X-Volt-Token with the token from config.json.
