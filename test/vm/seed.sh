#!/usr/bin/env bash
set -euo pipefail

# test/vm/seed.sh - Brings dermaga-vm (after M3 installer) to a usable demo state for M3.
# Idempotent: safe to run multiple times.

echo "=== Seeding dermaga-vm for Milestone M3 ==="
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

# 2. Verify M3 installer state and set demo admin password
ssh "$VM_HOST" 'bash -s' << 'EOF'
set -euo pipefail

if [ ! -f ~/.local/bin/dermaga ]; then
  echo "ERROR: ~/.local/bin/dermaga not found on VM. Run the M3 installer first." >&2
  exit 1
fi

mkdir -p ~/.config/dermaga

# Set default demo password for panel login (M3 has no first-run /setup link yet)
DEMO_PASS="correct-password-123"
PASS_FILE="$HOME/.config/dermaga/demo_password"
echo "$DEMO_PASS" > "$PASS_FILE"
chmod 0600 "$PASS_FILE"
echo "$DEMO_PASS" | ~/.local/bin/dermaga admin set-password

# Ensure dermaga, Traefik, and its sockets are active
systemctl --user start dermaga.service
systemctl --user is-active --quiet dermaga.service traefik.service traefik-http.socket traefik-https.socket || {
  echo "ERROR: dermaga or Traefik user units are not active." >&2
  systemctl --user status dermaga.service traefik.service traefik-http.socket traefik-https.socket --no-pager || true
  exit 1
}

~/.local/bin/dermaga doctor || true
~/.local/bin/dermaga panel > /dev/null
echo "PASS: dermaga daemon, Traefik, and panel ready"
EOF

echo ""
echo "=== Dermaga M3 Demo State Ready ==="
echo "Access the web panel:"
echo "  1. Start SSH tunnel: ssh -N -L 7070:127.0.0.1:7070 $VM_HOST"
echo "  2. Open browser:     http://127.0.0.1:7070"
echo "  3. Admin password stored on VM: ~/.config/dermaga/demo_password"
