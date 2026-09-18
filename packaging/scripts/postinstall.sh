#!/usr/bin/env bash
set -euo pipefail
systemctl daemon-reload || true
systemctl enable --now voltpanel.service || true
