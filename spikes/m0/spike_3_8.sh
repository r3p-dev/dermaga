#!/usr/bin/env bash
set -euo pipefail

echo "=== SPIKE 3.8: SSH Port Forwarding & Stream-Local Forwarding Enforcement ==="
echo "Date: $(date -Is)"
echo "Host: $(hostname)"
echo "Target: dermaga-vm (port 4222)"

LOCAL_PORT=17070
LOCAL_SOCK="/tmp/spike_3_8_local.sock"
REMOTE_SOCK="/tmp/spike_3_8_remote.sock"
SSH_PID_TCP=""
SSH_PID_SOCK=""

cleanup() {
  echo "--- Cleanup ---"
  if [ -n "${SSH_PID_TCP}" ]; then
    kill -TERM "${SSH_PID_TCP}" 2>/dev/null || true
  fi
  if [ -n "${SSH_PID_SOCK}" ]; then
    kill -TERM "${SSH_PID_SOCK}" 2>/dev/null || true
  fi
  rm -f "${LOCAL_SOCK}" /tmp/ssh_streamlocal.log
  ssh dermaga-vm "pkill -f '[m]ock_7070|[m]ock_sock' 2>/dev/null || true; rm -f ${REMOTE_SOCK}"
  echo "Spike 3.8 cleaned up."
}
trap cleanup EXIT

echo ""
echo "--- 1. Setting up mock listeners on dermaga-vm ---"
ssh dermaga-vm "cat << 'EOF' > ~/spikes/mock_7070.py
import socket, sys

server = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
server.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
server.bind(('127.0.0.1', 7070))
server.listen(5)
print('Mock 7070 listening', flush=True)

while True:
    conn, _ = server.accept()
    data = conn.recv(1024)
    body = 'dermaga-panel-7070\n'
    resp = f'HTTP/1.1 200 OK\r\nContent-Type: text/plain\r\nContent-Length: {len(body)}\r\nConnection: close\r\n\r\n{body}'.encode('utf-8')
    conn.sendall(resp)
    conn.close()
EOF
python3 ~/spikes/mock_7070.py >/dev/null 2>&1 &
MOCK_TCP_PID=\$!

cat << 'EOF' > ~/spikes/mock_sock.py
import socket, os, sys

sock_path = '${REMOTE_SOCK}'
if os.path.exists(sock_path):
    os.unlink(sock_path)

server = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
server.bind(sock_path)
server.listen(5)
print(f'Mock sock listening on {sock_path}', flush=True)

while True:
    conn, _ = server.accept()
    data = conn.recv(1024)
    body = 'dermaga-unix-socket-ok\n'
    resp = f'HTTP/1.1 200 OK\r\nContent-Type: text/plain\r\nContent-Length: {len(body)}\r\nConnection: close\r\n\r\n{body}'.encode('utf-8')
    conn.sendall(resp)
    conn.close()
EOF
python3 ~/spikes/mock_sock.py >/dev/null 2>&1 &
MOCK_SOCK_PID=\$!
sleep 1
ss -tlpn | grep 7070 || echo 'Listening on 7070'
ls -la ${REMOTE_SOCK}
"

echo ""
echo "--- 2. Testing SSH Local TCP Port Forwarding (-L ${LOCAL_PORT}:127.0.0.1:7070) ---"
ssh -N -L "${LOCAL_PORT}:127.0.0.1:7070" dermaga-vm &
SSH_PID_TCP=$!
sleep 2

echo "Attempting HTTP request through local forwarded port ${LOCAL_PORT}:"
RESP_TCP=$(curl -s --max-time 5 "http://127.0.0.1:${LOCAL_PORT}/")
echo "Response: ${RESP_TCP}"

if [ "${RESP_TCP}" = "dermaga-panel-7070" ]; then
  echo "PASS: SSH local TCP forwarding to 127.0.0.1:7070 succeeded"
else
  echo "FAIL: Expected 'dermaga-panel-7070', got '${RESP_TCP}'"
  exit 1
fi

kill -TERM "${SSH_PID_TCP}" 2>/dev/null || true
SSH_PID_TCP=""
sleep 1

echo ""
echo "--- 3. Testing SSH Stream-Local Forwarding (-L ${LOCAL_SOCK}:${REMOTE_SOCK}) ---"
rm -f "${LOCAL_SOCK}"
ssh -v -N -L "${LOCAL_SOCK}:${REMOTE_SOCK}" dermaga-vm > /tmp/ssh_streamlocal.log 2>&1 &
SSH_PID_SOCK=$!
sleep 2

echo "Attempting to connect to local forwarded Unix socket ${LOCAL_SOCK}:"
SOCK_STATUS="refused"
if curl -s --max-time 3 --unix-socket "${LOCAL_SOCK}" http://localhost/ > /tmp/sock_resp.txt 2>&1; then
  SOCK_STATUS="connected"
  echo "FAIL: Stream-local forwarding succeeded unexpectedly! Response: $(cat /tmp/sock_resp.txt)"
  exit 1
else
  echo "Connection to stream-local socket failed as expected."
fi

echo "Inspecting SSH verbose log for stream-local rejection:"
grep -iE "administratively prohibited|open failed|refused|rejected" /tmp/ssh_streamlocal.log || true

if grep -qiE "administratively prohibited|open failed" /tmp/ssh_streamlocal.log; then
  echo "PASS: OpenSSH rejected stream-local forwarding with 'open failed' (AllowStreamLocalForwarding no)"
else
  echo "SSH log output:"
  cat /tmp/ssh_streamlocal.log
  exit 1
fi

echo ""
echo "=== SPIKE 3.8 SUCCESS ==="
