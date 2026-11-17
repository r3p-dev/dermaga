#!/usr/bin/env bash
set -euo pipefail

# test/vm/measure_nfr2.sh - Measure NFR-2 memory metrics on dermaga-vm
# (a) Fresh-start idle RSS
# (b) Post-login peak RSS & verification time via SSH tunnel
# (c) Settled idle RSS after 10 minutes without requests

VM_HOST="${VM_HOST:-dermaga-vm}"
REPO_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$REPO_DIR"
LOGS_DIR="$REPO_DIR/tmp/evidence/M1/logs"
mkdir -p "$LOGS_DIR"

echo "=== Milestone M1 NFR-2 Memory & Argon2id Measurement ==="
echo "Timestamp: $(date -Is)"
echo "Commit:    $(git rev-parse --short HEAD)"
echo "VM Host:   $VM_HOST"

# 1. Clean daemon restart
echo "--- Restarting dermaga.service for fresh idle baseline ---"
ssh "$VM_HOST" "systemctl --user restart dermaga.service"
sleep 3

# Fetch PID
PID=$(ssh "$VM_HOST" "systemctl --user show -p MainPID --value dermaga.service")
echo "Dermaga MainPID: $PID"

# (a) Fresh-start idle RSS
echo "--- Capturing (a) Fresh-start idle RSS ---"
{
  echo "# Command: Fresh-start idle RSS measurement"
  echo "# Commit: $(git rev-parse --short HEAD)"
  echo "# Timestamp: $(date -Is)"
  echo "PID: $PID"
  ssh "$VM_HOST" "grep -E '^(VmRSS|VmHWM|VmSize|VmData|VmStk):' /proc/$PID/status"
} 2>&1 | tee "$LOGS_DIR/vm_fresh_idle_rss.log"

# Setup SSH tunnel for real login
echo "--- Opening SSH tunnel to $VM_HOST on port 7070 ---"
ssh -N -L 7070:127.0.0.1:7070 "$VM_HOST" &
TUNNEL_PID=$!
sleep 2

cleanup() {
  if kill -0 "$TUNNEL_PID" 2>/dev/null; then
    kill "$TUNNEL_PID" 2>/dev/null || true
    wait "$TUNNEL_PID" 2>/dev/null || true
  fi
}
trap cleanup EXIT

# Read password from VM
DEMO_PASS=$(ssh "$VM_HOST" "cat ~/.config/dermaga/demo_password")

# (b) Real login via SSH tunnel and immediate post-login RSS
echo "--- Performing real login through SSH tunnel ---"
LOGIN_JSON=$(printf '{"password":"%s"}' "$DEMO_PASS")

START_NS=$(date +%s%N)
LOGIN_RESP=$(curl -sS -w "\nHTTP_STATUS:%{http_code}\nTIME_TOTAL:%{time_total}\n" -X POST \
  -H "Content-Type: application/json" \
  -H "Origin: http://localhost:7070" \
  -d "$LOGIN_JSON" \
  http://127.0.0.1:7070/api/auth/login)
END_NS=$(date +%s%N)
ELAPSED_MS=$(( (END_NS - START_NS) / 1000000 ))

echo "$LOGIN_RESP"

{
  echo "# Command: Post-login RSS measurement immediately after real login"
  echo "# Commit: $(git rev-parse --short HEAD)"
  echo "# Timestamp: $(date -Is)"
  echo "PID: $PID"
  echo "Measured login round-trip time: ${ELAPSED_MS} ms"
  ssh "$VM_HOST" "grep -E '^(VmRSS|VmHWM|VmSize|VmData|VmStk):' /proc/$PID/status"
} 2>&1 | tee "$LOGS_DIR/vm_post_login_rss.log"

# (c) Wait 10 minutes (600s) without requests, sampling every 60s
echo "--- Waiting 10 minutes without requests to measure settled idle RSS ---"
for i in {1..10}; do
  sleep 60
  CURRENT_RSS=$(ssh "$VM_HOST" "grep VmRSS /proc/$PID/status | awk '{print \$2}'")
  echo "Minute $i/10: VmRSS = ${CURRENT_RSS} kB"
done

echo "--- Capturing (c) 10-minute settled idle RSS ---"
{
  echo "# Command: Settled idle RSS measurement after 10 minutes without requests"
  echo "# Commit: $(git rev-parse --short HEAD)"
  echo "# Timestamp: $(date -Is)"
  echo "PID: $PID"
  ssh "$VM_HOST" "grep -E '^(VmRSS|VmHWM|VmSize|VmData|VmStk):' /proc/$PID/status"
} 2>&1 | tee "$LOGS_DIR/vm_settled_idle_rss.log"

echo "=== NFR-2 measurement complete ==="
