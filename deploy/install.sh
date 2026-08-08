#!/bin/sh
# Installs salt.md as a systemd service. Run as root on the target machine:
#   ./deploy/install.sh ./salt
set -e

BIN="${1:-./salt}"
[ -f "$BIN" ] || { echo "usage: $0 <path-to-salt-binary>"; exit 1; }

id salt >/dev/null 2>&1 || useradd -r -s /usr/sbin/nologin salt
install -d /opt/salt
install -m 755 "$BIN" /opt/salt/salt
install -d -o salt -g salt /opt/salt/data
install -m 644 "$(dirname "$0")/salt.service" /etc/systemd/system/salt.service
systemctl daemon-reload
systemctl enable --now salt

echo "salt.md is running on port 80."
