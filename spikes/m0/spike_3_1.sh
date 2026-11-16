#!/usr/bin/env bash
set -euo pipefail

echo "=== SPIKE 3.1: Hook Relay & Host Loopback Isolation ==="
echo "Date: $(date -Is)"
echo "Host: $(hostname)"
echo "User: $(whoami)"

# Cleanup trap to ensure units are restored even if script fails
cleanup() {
  echo "--- Cleanup & Restoring Environment ---"
  kill -9 "${DAEMON_PID:-}" 2>/dev/null || true
  kill -9 "${DAEMON_PID_2:-}" 2>/dev/null || true
  kill -9 "${HOST_LISTENER_PID:-}" 2>/dev/null || true
  podman rm -f spike-traefik spike-relay 2>/dev/null || true
  rm -rf "${SOCKET_DIR:-}" "${SPIKE_TRAEFIK_DIR:-}"
  echo "Restarting vps-setup Traefik units..."
  systemctl --user start traefik-http.socket traefik-https.socket traefik.service
  systemctl --user is-active traefik.service && echo "vps-setup traefik.service restored"
}
trap cleanup EXIT

echo "--- 1. Stopping vps-setup Traefik to prevent contamination ---"
systemctl --user stop traefik.service traefik-http.socket traefik-https.socket
systemctl --user is-active traefik.service || echo "traefik.service is inactive"
systemctl --user is-active traefik-http.socket || echo "traefik-http.socket is inactive"

echo "--- 2. Setting up mock daemon on Unix socket ---"
SOCKET_DIR="${XDG_RUNTIME_DIR}/dermaga"
SOCKET_PATH="${SOCKET_DIR}/hooks.sock"
mkdir -p "${SOCKET_DIR}"
chmod 700 "${SOCKET_DIR}"
rm -f "${SOCKET_PATH}"

# Start mock daemon computing dynamic Content-Length
python3 -c '
import socket, os, sys

sock_path = sys.argv[1]
if os.path.exists(sock_path):
    os.unlink(sock_path)

server = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
server.bind(sock_path)
os.chmod(sock_path, 0o600)
server.listen(5)
print(f"Mock daemon listening on {sock_path}", flush=True)

count = 0
while True:
    conn, _ = server.accept()
    data = conn.recv(4096)
    count += 1
    body = f"daemon-v1-msg-{count}\n"
    resp = f"HTTP/1.1 200 OK\r\nContent-Type: text/plain\r\nContent-Length: {len(body)}\r\nConnection: close\r\n\r\n{body}".encode("utf-8")
    conn.sendall(resp)
    conn.close()
' "${SOCKET_PATH}" &
DAEMON_PID=$!
sleep 1
ls -la "${SOCKET_PATH}"

echo "--- 3. Starting relay container on network web with mounted directory ---"
podman rm -f spike-relay 2>/dev/null || true
podman run -d --name spike-relay \
  --network web \
  -v "${SOCKET_DIR}:/run/dermaga:ro" \
  docker.io/alpine/socat:latest \
  TCP-LISTEN:8080,fork,reuseaddr UNIX-CONNECT:/run/dermaga/hooks.sock

echo "--- 4. Starting spike Traefik container routing /hooks/github-app to spike-relay ---"
SPIKE_TRAEFIK_DIR="/tmp/spike-traefik-dynamic"
mkdir -p "${SPIKE_TRAEFIK_DIR}"
cat << 'TRAEFIK_CFG' > "${SPIKE_TRAEFIK_DIR}/relay.yml"
http:
  routers:
    hook-router:
      rule: "PathPrefix(`/hooks/github-app`)"
      entryPoints:
        - web
      service: hook-service
  services:
    hook-service:
      loadBalancer:
        servers:
          - url: "http://spike-relay:8080"
TRAEFIK_CFG

podman rm -f spike-traefik 2>/dev/null || true
podman run -d --name spike-traefik \
  --network web \
  -v "${SPIKE_TRAEFIK_DIR}:/dynamic:ro" \
  docker.io/library/traefik:v3.7 \
  --entrypoints.web.address=:80 \
  --providers.file.directory=/dynamic

# Allow Traefik a second to load dynamic config
sleep 2

echo "--- 5. Sending request to Traefik -> spike-relay -> Unix socket ---"
RESPONSE=$(podman run --rm --network web docker.io/curlimages/curl:latest -s --connect-timeout 3 --max-time 5 http://spike-traefik/hooks/github-app)
echo "Response from Traefik: ${RESPONSE}"
if [[ "${RESPONSE}" == *"daemon-v1-msg-"* ]]; then
  echo "PASS: Traefik reached Unix socket daemon through relay"
else
  echo "FAIL: Unexpected response: ${RESPONSE}"
  exit 1
fi

echo "--- 6. Testing daemon restart across directory mount ---"
echo "Killing mock daemon PID ${DAEMON_PID} and recreating socket..."
kill -9 "${DAEMON_PID}" || true
rm -f "${SOCKET_PATH}"

# Start second daemon instance (v2) on the exact same socket path
python3 -c '
import socket, os, sys

sock_path = sys.argv[1]
if os.path.exists(sock_path):
    os.unlink(sock_path)

server = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
server.bind(sock_path)
os.chmod(sock_path, 0o600)
server.listen(5)
print(f"Mock daemon v2 listening on {sock_path}", flush=True)

count = 0
while True:
    conn, _ = server.accept()
    data = conn.recv(4096)
    count += 1
    body = f"daemon-v2-msg-{count}\n"
    resp = f"HTTP/1.1 200 OK\r\nContent-Type: text/plain\r\nContent-Length: {len(body)}\r\nConnection: close\r\n\r\n{body}".encode("utf-8")
    conn.sendall(resp)
    conn.close()
' "${SOCKET_PATH}" &
DAEMON_PID_2=$!
sleep 1
ls -la "${SOCKET_PATH}"

echo "Calling Traefik again after daemon socket recreation..."
RESPONSE_V2=$(podman run --rm --network web docker.io/curlimages/curl:latest -s --connect-timeout 3 --max-time 5 http://spike-traefik/hooks/github-app)
echo "Response from Traefik: ${RESPONSE_V2}"
if [[ "${RESPONSE_V2}" == *"daemon-v2-msg-"* ]]; then
  echo "PASS: Directory mount survived daemon restart and socket recreation"
else
  echo "FAIL: Unexpected response after daemon recreation: ${RESPONSE_V2}"
  exit 1
fi

echo "--- 7. Verifying no new host ports listening (ss -tuln) ---"
if ss -tuln | grep -E ':8080\b'; then
  echo "FAIL: Port 8080 is exposed on host!"
  exit 1
else
  echo "PASS: Port 8080 is not exposed on host"
fi

echo "--- 8. Verifying container cannot reach host loopback ---"
# Bind a test listener strictly to host 127.0.0.1:9999
python3 -c '
import socket, time
s = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
s.bind(("127.0.0.1", 9999))
s.listen(1)
print("Host loopback listener active on 127.0.0.1:9999", flush=True)
time.sleep(30)
' &
HOST_LISTENER_PID=$!
sleep 1

# List of paths to test from container on network web to host loopback
CANDIDATE_ADDRS=(
  "127.0.0.1"
  "host.containers.internal"
  "169.254.1.2"
  "10.89.0.1"
  "192.168.122.217"
)

ALL_REFUSED=true
for ADDR in "${CANDIDATE_ADDRS[@]}"; do
  echo -n "Testing connection from container to ${ADDR}:9999 ... "
  STATUS=0
  OUTPUT=$(podman run --rm --network web docker.io/curlimages/curl:latest -s --connect-timeout 2 --max-time 3 "http://${ADDR}:9999" 2>&1) || STATUS=$?
  if [ $STATUS -eq 0 ]; then
    echo "CONNECTED (SECURITY FAILURE!)"
    ALL_REFUSED=false
  else
    echo "REFUSED/TIMEOUT (Exit code ${STATUS})"
  fi
done

if [ "$ALL_REFUSED" = true ]; then
  echo "PASS: Host 127.0.0.1:9999 is completely unreachable from container on network web across all tested paths"
else
  echo "FAIL: One or more paths reached host loopback!"
  exit 1
fi

echo "=== SPIKE 3.1 SUCCESS ==="
