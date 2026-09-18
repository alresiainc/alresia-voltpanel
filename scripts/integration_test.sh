#!/usr/bin/env bash
set -euo pipefail
BIN=./dist/voltpanel
PORT=7799
DEV=1 $BIN -port $PORT &
PID=$!
trap 'kill $PID 2>/dev/null || true' EXIT
sleep 2
curl -sSf http://127.0.0.1:$PORT/health
curl -sSf -X POST http://127.0.0.1:$PORT/api/v1/services/echo/start -H 'Content-Type: application/json' -d '{"name":"echo","command":"/bin/sh","args":["-c","echo hello && sleep 1"],"cwd":"/tmp"}'
sleep 1
curl -sSf "http://127.0.0.1:$PORT/api/v1/services/echo/logs?tail=true"
curl -sSf http://127.0.0.1:$PORT/api/v1/system/metrics
