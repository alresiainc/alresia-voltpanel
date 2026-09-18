#!/usr/bin/env python3
"""Sample VoltPanel extension: a minimal RuntimeProvider built "as if by an
outside contractor" (§17 Phase 11's dogfood requirement).

This file is intentionally self-contained. It does not import anything
from VoltPanel, does not know Go exists, and does not reach into any
VoltPanel internals -- it only reads newline-delimited JSON requests from
stdin and writes newline-delimited JSON responses to stdout, exactly per
the protocol documented in internal/pluginhost/protocol.go:

  1. On startup, write one JSON line describing this extension (the
     "handshake"): protocol version, provider kind, this provider's own
     name, and the methods it implements.
  2. Then loop forever: read one JSON request per line
     ({"id", "method", "params"}), handle it, write one JSON response per
     line back ({"id", "result"} on success or {"id", "error"} on
     failure).

The fake "kind" this provider manages is "python-demo" -- it does not
actually install, remove, or otherwise manage any real Python
installation (Install/Remove/SetDefault all just report "not supported").
DetectInstalled, however, performs a *real* detection: it shells out to
`python3 --version` and reports the actual Python 3 interpreter present on
this machine. That's genuinely true data, even though the provider itself
is a demo/dogfood fixture rather than a production runtime manager -- and
it never modifies anything on the machine it runs on.
"""
import json
import subprocess
import sys

PROTOCOL_VERSION = 1


def send(obj):
    sys.stdout.write(json.dumps(obj) + "\n")
    sys.stdout.flush()


def detect_installed():
    """Reports the real, locally-installed python3 -- read-only, no
    system modification of any kind."""
    version_proc = subprocess.run(
        ["python3", "--version"],
        capture_output=True,
        text=True,
        timeout=5,
    )
    version_line = (version_proc.stdout or version_proc.stderr or "").strip()
    version = version_line.replace("Python ", "").strip() or "unknown"

    path_proc = subprocess.run(
        ["python3", "-c", "import sys; sys.stdout.write(sys.executable)"],
        capture_output=True,
        text=True,
        timeout=5,
    )
    install_path = path_proc.stdout.strip()

    return [{"version": version, "installPath": install_path, "isDefault": True}]


def handle(method, params):
    if method == "DetectInstalled":
        return detect_installed(), None
    if method in ("Install", "Remove", "SetDefault"):
        return None, "{} is not supported by this sample/demo provider (read-only detection only)".format(method)
    return None, "unknown method: {}".format(method)


def main():
    send(
        {
            "protocolVersion": PROTOCOL_VERSION,
            "kind": "runtime",
            "name": "python-demo",
            "methods": ["DetectInstalled", "Install", "Remove", "SetDefault"],
        }
    )

    for raw_line in sys.stdin:
        line = raw_line.strip()
        if not line:
            continue
        try:
            req = json.loads(line)
        except ValueError as exc:
            # Malformed input from the daemon -- nothing sane to reply to,
            # since we don't even have a request id. Skip the line.
            sys.stderr.write("provider.py: could not parse request line: {}\n".format(exc))
            sys.stderr.flush()
            continue

        req_id = req.get("id")
        method = req.get("method")
        params = req.get("params")

        try:
            result, err = handle(method, params)
        except Exception as exc:  # defensive: never let a bug hang the daemon
            send({"id": req_id, "error": "internal error: {}".format(exc)})
            continue

        if err:
            send({"id": req_id, "error": err})
        else:
            send({"id": req_id, "result": result})


if __name__ == "__main__":
    main()
