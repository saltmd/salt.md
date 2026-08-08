#!/bin/sh
# salt.md installer — downloads the right prebuilt binary and installs it.
#
#   curl -fsSL https://raw.githubusercontent.com/saltmd/salt.md/main/install.sh | sh
#
# The binary is fully self-contained (frontend embedded, no CGO, no runtime
# deps). Override the target dir with BIN_DIR=/path, or pin a version with
# SALT_VERSION=v1.0.0.
set -eu

REPO="saltmd/salt.md"

say()  { printf '\033[1;32m»\033[0m %s\n' "$*"; }
err()  { printf '\033[1;31m✗\033[0m %s\n' "$*" >&2; exit 1; }

# --- detect platform --------------------------------------------------------
os=$(uname -s | tr '[:upper:]' '[:lower:]')
arch=$(uname -m)
case "$os" in
  linux|darwin) ;;
  *) err "Unsupported OS: $os (salt.md ships prebuilt binaries for linux and macOS; build from source for others)";;
esac
case "$arch" in
  x86_64|amd64) arch=amd64 ;;
  aarch64|arm64) arch=arm64 ;;
  *) err "Unsupported architecture: $arch" ;;
esac
asset="salt-${os}-${arch}"

# --- resolve download URL ---------------------------------------------------
ver="${SALT_VERSION:-latest}"
if [ "$ver" = "latest" ]; then
  url="https://github.com/$REPO/releases/latest/download/$asset"
else
  url="https://github.com/$REPO/releases/download/$ver/$asset"
fi

# --- pick a fetcher ---------------------------------------------------------
if command -v curl >/dev/null 2>&1; then
  fetch() { curl -fsSL "$1" -o "$2"; }
elif command -v wget >/dev/null 2>&1; then
  fetch() { wget -qO "$2" "$1"; }
else
  err "Neither curl nor wget is available."
fi

# --- pick an install dir ----------------------------------------------------
use_sudo=
if [ -n "${BIN_DIR:-}" ]; then
  bindir="$BIN_DIR"
elif [ -w /usr/local/bin ]; then
  bindir=/usr/local/bin
elif command -v sudo >/dev/null 2>&1; then
  bindir=/usr/local/bin
  use_sudo=1
else
  bindir="$HOME/.local/bin"
fi

say "Downloading salt.md ($asset, $ver)…"
tmp=$(mktemp)
trap 'rm -f "$tmp"' EXIT
fetch "$url" "$tmp" || err "Download failed. Is there a release yet? $url"
# A GitHub 404 page is HTML, not an ELF/Mach-O binary — catch that early.
if head -c 4 "$tmp" | grep -q '<!DO\|<htm\|<HTM' 2>/dev/null || [ ! -s "$tmp" ]; then
  err "Downloaded file is not a binary — the release asset '$asset' may not exist yet."
fi
chmod +x "$tmp"

say "Installing to $bindir/salt"
if [ -n "$use_sudo" ]; then
  sudo mkdir -p "$bindir" && sudo mv "$tmp" "$bindir/salt"
else
  mkdir -p "$bindir" && mv "$tmp" "$bindir/salt"
fi
trap - EXIT

say "Done. salt.md $("$bindir/salt" version 2>/dev/null || echo "$ver") installed."
echo
case ":$PATH:" in
  *":$bindir:"*) echo "  Run it:   salt" ;;
  *) echo "  $bindir is not on your PATH. Either add it, or run:"
     echo "            $bindir/salt" ;;
esac
echo "  Then open http://localhost:8420 and create your admin account."
