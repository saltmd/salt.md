#!/bin/sh
# salt.md installer: downloads the right prebuilt binary, installs it, and
# starts it. Set SALT_NO_START=1 to install without starting.
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

# `salt version` already prints a leading v, and $ver is "latest" when nobody
# pinned one — so normalise instead of gluing a v in front of whatever comes
# back, which produced "vv1.0.0" and could have produced "vlatest".
installed=$("$bindir/salt" version 2>/dev/null || echo "$ver")
installed=${installed#v}
case "$installed" in latest|'') installed='' ;; *) installed="v$installed   " ;; esac

# --- the address somebody can actually open ---------------------------------
# Almost nobody installs this on the machine they are sitting at. Printing
# "localhost" to a person three hops into an SSH session sends them to their
# own laptop, and that is the most common way a fresh install goes nowhere.
#
# Every probe ends in `|| true`: `set -e` is on, and a machine without
# `hostname -I` must not lose the whole summary over it. Linux first, because
# that is where this mostly runs.
lan=$(hostname -I 2>/dev/null | awk '{ print $1 }' || true)
[ -n "$lan" ] || lan=$(ip route get 1.1.1.1 2>/dev/null | awk '{ for (i = 1; i < NF; i++) if ($i == "src") { print $(i + 1); exit } }' || true)
[ -n "$lan" ] || lan=$(ipconfig getifaddr en0 2>/dev/null || true)

# The port is not always 8420: SALT_ADDR moves it, and printing the default
# regardless sends people to a port nothing is listening on. Take whatever
# follows the last colon, fall back to the default when SALT_ADDR is unset or
# is a bare address.
# `set -u` is on, so SALT_ADDR has to be defaulted before it is expanded —
# ${SALT_ADDR##*:} on its own aborts the whole install when nobody set it,
# which is every ordinary run.
addr=${SALT_ADDR:-}
port=${addr##*:}
case "$port" in ''|*[!0-9]*) port=8420 ;; esac

case "$lan" in
  ''|127.*) url="http://localhost:$port"; also='' ;;
  *)        url="http://$lan:$port";      also="http://localhost:$port" ;;
esac

# --- run it ------------------------------------------------------------------
# One command should end with a running instance, not with homework.
#
# On a Linux server that means a service, not a foreground process. A process
# holds the terminal, dies with the SSH session, and is gone after a reboot;
# nobody ships software that way. So when this is root on a machine running
# systemd, it installs a unit and starts it: the terminal comes back, a crash
# restarts it, and a reboot brings it up again.
#
# Everywhere else it runs here in the foreground, which is the right thing on a
# laptop somebody is trying it out on.
service_possible() {
  [ "$os" = linux ] || return 1
  [ "$(id -u)" = 0 ] || return 1
  command -v systemctl >/dev/null 2>&1 || return 1
  # systemctl can be installed in a container where systemd runs nothing. This
  # directory is what tells a real init apart from a leftover binary.
  [ -d /run/systemd/system ] || return 1
}

install_service() {
  id salt >/dev/null 2>&1 || useradd --system --home-dir "$svcdata" --shell /usr/sbin/nologin salt 2>/dev/null || true
  install -d -o salt -g salt "$svcdata"

  cat > /etc/systemd/system/salt.service <<UNIT
[Unit]
Description=salt.md
Documentation=https://salt.md/wiki/
After=network-online.target
Wants=network-online.target

[Service]
User=salt
Group=salt
ExecStart=$bindir/salt
Environment=SALT_ADDR=:$port
Environment=SALT_DATA=$svcdata
WorkingDirectory=$svcdata
Restart=on-failure
RestartSec=2
KillSignal=SIGTERM
TimeoutStopSec=20
NoNewPrivileges=true
PrivateTmp=true
ProtectSystem=full
ProtectHome=true

[Install]
WantedBy=multi-user.target
UNIT

  systemctl daemon-reload
  systemctl enable salt >/dev/null 2>&1 || true
  systemctl restart salt
}

svcdata=/var/lib/salt

# Decide BEFORE anything is printed, so the summary can name the directory that
# will actually be used. Printing $PWD/data and then installing a service that
# reads /var/lib/salt is how somebody goes looking for a database that was never
# there.
if [ -z "${SALT_NO_SERVICE:-}" ] && service_possible; then
  as_service=yes
  datadir=$svcdata
else
  as_service=no
  datadir=$PWD/data
fi

# Colour only for a terminal. Piped into a file or a log, the escapes would be
# the only thing anybody sees.
if [ -t 1 ]; then
  b=$(printf '\033[1m'); d=$(printf '\033[2m'); r=$(printf '\033[0m')
  g1=$(printf '\033[38;5;211m'); g3=$(printf '\033[38;5;177m'); g5=$(printf '\033[38;5;110m')
else
  b=''; d=''; r=''; g1=''; g3=''; g5=''
fi

printf '\n'
printf '  %s█▀▀▀ █▀▀█ █    ▀█▀   █▄ ▄█ █▀▀▄%s\n' "$g1" "$r"
printf '  %s▀▀▀█ █▄▄█ █     █    █ ▀ █ █  █%s\n' "$g3" "$r"
printf '  %s▀▀▀▀ ▀  ▀ ▀▀▀▀  ▀    ▀   ▀ ▀▀▀%s\n' "$g5" "$r"
printf '\n  %s%sthe workspace people and agents share%s\n\n' "$d" "$installed" "$r"

printf '  Open in browser   %s%s%s\n' "$b" "$url" "$r"
[ -n "$also" ] && printf '                    %s%s on the machine itself%s\n' "$d" "$also" "$r"
printf '  Star it           %shttps://github.com/%s%s\n' "$d" "$REPO" "$r"
printf '  Data              %s%s%s\n' "$d" "$datadir" "$r"
printf '\n'


if [ "$as_service" = yes ]; then
  had_unit=no
  [ -f /etc/systemd/system/salt.service ] && had_unit=yes
  install_service

  # Report it running because it IS, not because systemctl exited 0.
  i=0
  while [ "$i" -lt 30 ] && ! systemctl is-active --quiet salt; do
    i=$((i + 1))
    sleep 1
  done

  if systemctl is-active --quiet salt; then
    if [ "$had_unit" = yes ]; then
      printf '  %sUpdated. It was already running as a service, and was restarted.%s\n' "$d" "$r"
    else
      printf '  %sInstalled as a service: it starts on boot and restarts after a crash.%s\n' "$d" "$r"
    fi
    printf '  %ssystemctl status salt   ·   journalctl -u salt -f%s\n' "$d" "$r"
    # A foreground run earlier wrote its database into whatever directory it was
    # started from. The service reads a different one, so an empty workspace here
    # is a file in the other place and not a lost one.
    if [ -f ./data/salt.db ] && [ ! -f "$svcdata/salt.db" ]; then
      printf '\n  %sNote: ./data/salt.db is from an earlier foreground run. The service uses%s\n' "$d" "$r"
      printf '  %s%s, so it starts empty. Nothing was deleted.%s\n' "$d" "$svcdata" "$r"
    fi
    printf '\n'
  else
    printf '  %sThe service did not come up. journalctl -u salt says why.%s\n\n' "$d" "$r"
  fi
  exit 0
fi

# Not a systemd server. Piped into a Dockerfile or a provisioning script a
# foreground process would block forever, so a non-tty install prints the
# command and gets out of the way.
if [ -n "${SALT_NO_START:-}" ] || [ ! -t 1 ]; then
  case ":$PATH:" in
    *":$bindir:"*) printf '  Run it:   salt\n' ;;
    *)             printf '  %s is not on your PATH. Run it with:\n            %s/salt\n' "$bindir" "$bindir" ;;
  esac
  exit 0
fi

# No "starting it now" line: the server's own "listening on" line arrives a
# breath later and says it better. What a person needs here is the way back out.
printf '  %sCtrl-C stops it. Run%s salt %sto start it again.%s\n\n' "$d" "$r" "$d" "$r"
exec "$bindir/salt"
