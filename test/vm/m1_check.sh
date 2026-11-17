#!/usr/bin/env bash
set -euo pipefail

# test/vm/m1_check.sh - M1 VM verification scenario.
# Idempotent, leaves no residue. Follows AGENTS.md "Test environment".

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_DIR="$(cd "$SCRIPT_DIR/../.." && pwd)"

cd "$REPO_DIR"

echo "Timestamp: $(date -Is)"
echo "Hostname:  $(hostname)"
echo "Commit:    $(git rev-parse --short HEAD)"

VM_HOST="${VM_HOST:-dermaga-vm}"

echo "=== Running M1 VM verification on $VM_HOST ==="

# 1. Verify SSH connectivity
ssh -q "$VM_HOST" true || {
  echo "FAIL: Cannot connect to $VM_HOST via SSH" >&2
  exit 1
}

# 2. Run remote checks on dermaga-vm
ssh "$VM_HOST" 'bash -s' << 'EOF'
set -euo pipefail

# Print remote identity
echo "Remote Hostname: $(hostname)"
echo "Remote Date:     $(date -Is)"

# Check user service status
echo "--- Checking systemd user service ---"
systemctl --user is-active --quiet dermaga.service || {
  echo "FAIL: dermaga.service is not active" >&2
  systemctl --user status dermaga.service --no-pager
  exit 1
}
echo "PASS: dermaga.service is active"

# Check daemon Unix socket status
echo "--- Checking daemon Unix socket ---"
~/.local/bin/dermaga panel > /dev/null
SOCKET_RESP=$(python3 -c 'import socket, os, json; s = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM); s.connect(os.environ["XDG_RUNTIME_DIR"] + "/dermaga/daemon.sock"); s.sendall(json.dumps({"action":"status"}).encode() + b"\n"); print(s.recv(1024).decode())')
echo "$SOCKET_RESP" | grep -q '"success":true' || {
  echo "FAIL: Unexpected socket response: $SOCKET_RESP" >&2
  exit 1
}
echo "PASS: Unix socket responded: $SOCKET_RESP"

# Check HTTP loopback endpoint
echo "--- Checking HTTP loopback /api/auth/session ---"
SESSION_RESP=$(curl -sS -f http://127.0.0.1:7070/api/auth/session)
echo "$SESSION_RESP" | grep -q '"authenticated":false' || {
  echo "FAIL: Unexpected session response: $SESSION_RESP" >&2
  exit 1
}
echo "PASS: HTTP loopback responded: $SESSION_RESP"

# Check memory footprint against NFR-2
echo "--- Checking process memory footprint ---"
PID=$(systemctl --user show -p MainPID --value dermaga.service)
if [ -z "$PID" ] || [ "$PID" -eq 0 ]; then
  echo "FAIL: Could not determine dermaga MainPID" >&2
  exit 1
fi

VM_RSS_KB=$(grep VmRSS /proc/$PID/status | awk '{print $2}')
echo "Current VmRSS: ${VM_RSS_KB} kB"
if [ "$VM_RSS_KB" -gt 30720 ]; then
  echo "WARN: VmRSS ${VM_RSS_KB} kB exceeds 30 MB (30720 kB) idle budget"
else
  echo "PASS: VmRSS ${VM_RSS_KB} kB is within < 30 MB idle budget"
fi
EOF

echo "=== All M1 VM verification checks passed ==="
