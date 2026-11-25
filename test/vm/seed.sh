#!/usr/bin/env bash
set -euo pipefail

# test/vm/seed.sh - Brings dermaga-vm to a usable demo state for M6 (apps, daemon, Traefik).
# Idempotent: safe to run multiple times.

echo "=== Seeding dermaga-vm for Milestone M6 ==="
echo "Timestamp: $(date -Is)"
echo "Host: $(hostname)"
echo "Commit: $(git rev-parse --short HEAD)"

# Target host
VM_HOST="${VM_HOST:-dermaga-vm}"

# 1. Verify SSH connectivity
if ! ssh -q "$VM_HOST" true; then
  echo "ERROR: Cannot reach $VM_HOST via SSH." >&2
  exit 1
fi

# 2. Verify M4a installer state and ensure daemon + Traefik are active
ssh "$VM_HOST" 'bash -s' << 'EOF'
set -euo pipefail

if [ ! -f ~/.local/bin/dermaga ]; then
  echo "ERROR: ~/.local/bin/dermaga not found on VM. Run the installer first." >&2
  exit 1
fi

# Ensure dermaga, Traefik, and its sockets are active
systemctl --user start dermaga.service
systemctl --user is-active --quiet dermaga.service traefik.service traefik-http.socket traefik-https.socket || {
  echo "ERROR: dermaga or Traefik user units are not active." >&2
  systemctl --user status dermaga.service traefik.service traefik-http.socket traefik-https.socket --no-pager || true
  exit 1
}

~/.local/bin/dermaga doctor || true
~/.local/bin/dermaga panel
python3 -c '
import os, sqlite3
db = os.path.expanduser("~/.local/share/dermaga/dermaga.db")
if os.path.exists(db):
    c = sqlite3.connect(db)
    apps = c.execute("SELECT a.name, d.domain, a.active_release_id FROM apps a LEFT JOIN domains d ON d.app_id=a.id AND d.is_primary=1").fetchall()
    print("Apps:", apps)
'
echo "PASS: dermaga daemon, Traefik, and panel ready"
EOF

echo ""
echo "=== Dermaga M6 Demo State Ready ==="
echo "Access the web panel:"
echo "  1. Start SSH tunnel: ssh -N -L 7070:127.0.0.1:7070 $VM_HOST"
echo "  2. If uninitialized, open the http://localhost:7070/setup?token=... link printed above"
echo "  3. Otherwise open: http://localhost:7070"
