#!/usr/bin/env bash
set -euo pipefail
systemctl daemon-reload || true
systemctl enable --now devpanel.service || true
