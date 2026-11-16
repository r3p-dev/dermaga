#!/usr/bin/env bash
set -euo pipefail

# test/vm/seed.sh - Brings dermaga-vm to a usable demo state for M1.
# Idempotent: safe to run multiple times.

echo "=== Seeding dermaga-vm for Milestone M1 ==="
echo "Timestamp: $(date -Is)"
echo "Host: $(hostname)"

# Target host
VM_HOST="${VM_HOST:-dermaga-vm}"

# 1. Verify SSH connectivity
if ! ssh -q "$VM_HOST" true; then
  echo "ERROR: Cannot reach $VM_HOST via SSH." >&2
  exit 1
fi

# 2. Ensure dermaga user service directory and binary exist
ssh "$VM_HOST" 'bash -s' << 'EOF'
set -euo pipefail

# Ensure ~/.local/bin, ~/.config/dermaga, and systemd user config exist
mkdir -p ~/.local/bin ~/.config/dermaga ~/.config/systemd/user ~/.local/share/dermaga

# Check binary exists
if [ ! -f ~/.local/bin/dermaga ]; then
  echo "ERROR: ~/.local/bin/dermaga not found on VM. Build and deploy binary first." >&2
  exit 1
fi

# Set default demo password if database does not exist yet or reset for demo
DEMO_PASS="correct-password-123"
PASS_FILE="$HOME/.config/dermaga/demo_password"
echo "$DEMO_PASS" > "$PASS_FILE"
chmod 0600 "$PASS_FILE"
echo "$DEMO_PASS" | ~/.local/bin/dermaga admin set-password

# Install systemd user service if not present
UNIT_FILE="$HOME/.config/systemd/user/dermaga.service"
if [ ! -f "$UNIT_FILE" ]; then
  cat > "$UNIT_FILE" << 'UNIT'
[Unit]
Description=Dermaga Host Agent
After=network.target

[Service]
Type=simple
ExecStart=%h/.local/bin/dermaga daemon
Restart=always
RestartSec=3s
Environment=XDG_DATA_HOME=%h/.local/share
Environment=XDG_CONFIG_HOME=%h/.config

[Install]
WantedBy=default.target
UNIT
  systemctl --user daemon-reload
  systemctl --user enable dermaga.service
fi

# Restart daemon to ensure clean state
systemctl --user restart dermaga.service
systemctl --user is-active --quiet dermaga.service || {
  echo "ERROR: dermaga.service failed to start." >&2
  journalctl --user -u dermaga.service -n 20 --no-pager
  exit 1
}

# Verify unix socket responsiveness
~/.local/bin/dermaga panel > /dev/null
echo "PASS: dermaga daemon responded via Unix socket"
EOF

echo ""
echo "=== Dermaga M1 Demo State Ready ==="
echo "Access the web panel:"
echo "  1. Start SSH tunnel: ssh -N -L 7070:127.0.0.1:7070 $VM_HOST"
echo "  2. Open browser:     http://127.0.0.1:7070"
echo "  3. Admin password stored on VM: ~/.config/dermaga/demo_password"
