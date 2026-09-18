#!/bin/sh
# VoltPanel installer — one command, no package manager, no manual
# download, in the same spirit as `curl ... | sh` installers for CLI
# tools like Claude Code.
#
#   curl -fsSL https://raw.githubusercontent.com/alresiainc/alresia-voltpanel/main/scripts/install.sh | sh
#
# Env overrides:
#   VOLT_VERSION       install this release tag instead of the latest (e.g. v0.2.0)
#   VOLT_INSTALL_DIR   where to put the binary (default: $HOME/.local/bin)

set -eu

REPO="alresiainc/alresia-voltpanel"
INSTALL_DIR="${VOLT_INSTALL_DIR:-$HOME/.local/bin}"

say() { printf '%s\n' "$*" >&2; }
die() { say "error: $*"; exit 1; }
need() { command -v "$1" >/dev/null 2>&1 || die "'$1' is required but not found"; }

need curl
need tar

os=$(uname -s)
case "$os" in
  Darwin) goos="darwin" ;;
  Linux) goos="linux" ;;
  *) die "unsupported OS: $os (Windows: download the .zip from https://github.com/$REPO/releases)" ;;
esac

arch=$(uname -m)
case "$arch" in
  x86_64|amd64) goarch="amd64" ;;
  arm64|aarch64) goarch="arm64" ;;
  *) die "unsupported architecture: $arch" ;;
esac

if [ -n "${VOLT_VERSION:-}" ]; then
  tag="$VOLT_VERSION"
else
  tag=$(curl -fsSL "https://api.github.com/repos/$REPO/releases/latest" \
    | grep '"tag_name"' | sed -E 's/.*"tag_name": *"([^"]+)".*/\1/')
  [ -n "$tag" ] || die "couldn't determine the latest release tag"
fi
version="${tag#v}"

archive="voltpanel_${version}_${goos}_${goarch}.tar.gz"
base_url="https://github.com/$REPO/releases/download/$tag"

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

say "Downloading VoltPanel $tag for $goos/$goarch..."
curl -fsSL "$base_url/$archive" -o "$tmp/$archive" || die "download failed: $base_url/$archive"
curl -fsSL "$base_url/checksums.txt" -o "$tmp/checksums.txt" || die "download failed: $base_url/checksums.txt"

say "Verifying checksum..."
expected=$(grep " $archive\$" "$tmp/checksums.txt" | awk '{print $1}')
[ -n "$expected" ] || die "no checksum entry for $archive in checksums.txt"
if command -v sha256sum >/dev/null 2>&1; then
  actual=$(sha256sum "$tmp/$archive" | awk '{print $1}')
else
  actual=$(shasum -a 256 "$tmp/$archive" | awk '{print $1}')
fi
[ "$expected" = "$actual" ] || die "checksum mismatch for $archive"

say "Extracting..."
tar -xzf "$tmp/$archive" -C "$tmp" voltpanel

mkdir -p "$INSTALL_DIR"
mv "$tmp/voltpanel" "$INSTALL_DIR/voltpanel"
chmod +x "$INSTALL_DIR/voltpanel"

say "Installed voltpanel $tag to $INSTALL_DIR/voltpanel"

case ":$PATH:" in
  *":$INSTALL_DIR:"*) ;;
  *)
    say ""
    say "$INSTALL_DIR is not on your PATH. Add it, e.g.:"
    say "  echo 'export PATH=\"$INSTALL_DIR:\$PATH\"' >> ~/.zshrc   # or ~/.bashrc"
    ;;
esac

say ""
say "Run 'voltpanel start' then visit http://127.0.0.1:7788"
