#!/usr/bin/env bash
set -euo pipefail
BIN=./dist/voltpanel
PORT=7799
DEV=1 $BIN -port $PORT &
PID=$!
sleep 2
curl -sSf http://127.0.0.1:$PORT/health
curl -sS -X POST http://127.0.0.1:$PORT/services/start -H 'Content-Type: application/json' -d '{"id":"echo","name":"echo","command":"/bin/sh","args":["-c","echo hello && sleep 1"],"cwd":"/tmp"}'
sleep 1
curl -sS http://127.0.0.1:$PORT/logs/echo?tail=true
kill $PID || true
