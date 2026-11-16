#!/usr/bin/env bash
set -euo pipefail

echo "=== SPIKE 3.2: Rootless Netns HTTP Probe with Aardvark-DNS ==="
echo "Date: $(date -Is)"
echo "Host: $(hostname)"
echo "User: $(whoami)"

NETNS_PATH="${XDG_RUNTIME_DIR}/containers/networks/rootless-netns/rootless-netns"
DNS_SERVER="10.89.0.1:53"
PROBE_BIN="${HOME}/spikes/probe"
TARGET_NAME="spike-probe-target"

cleanup() {
  echo "--- Cleanup ---"
  podman rm -f "${TARGET_NAME}" 2>/dev/null || true
  echo "Spike 3.2 cleaned up."
}
trap cleanup EXIT

echo "--- 1. Starting test candidate container on network web ---"
podman rm -f "${TARGET_NAME}" 2>/dev/null || true
podman run -d --name "${TARGET_NAME}" \
  --network web \
  --entrypoint /target_app \
  -v "${HOME}/spikes/target_app:/target_app:ro" \
  docker.io/alpine/socat:latest

sleep 1
podman ps --filter name="${TARGET_NAME}"

echo "--- 2. Verifying Netns and Aardvark DNS entry ---"
ls -la "${NETNS_PATH}"
grep -E "${TARGET_NAME}" "${XDG_RUNTIME_DIR}/containers/networks/aardvark-dns/web"

echo "--- 3. Running HTTP Probe into container from rootless netns ---"
podman unshare nsenter --net="${NETNS_PATH}" "${PROBE_BIN}" -dns "${DNS_SERVER}" -timeout 3s "http://${TARGET_NAME}:8080/health"

echo "--- 4. Negative test: Probe non-existent path (404) ---"
STATUS=0
podman unshare nsenter --net="${NETNS_PATH}" "${PROBE_BIN}" -dns "${DNS_SERVER}" -timeout 3s "http://${TARGET_NAME}:8080/not-found" || STATUS=$?
echo "Exit code for 404 path: ${STATUS} (Expected non-zero)"

echo "--- 5. Negative test: Probe non-existent container (DNS failure) ---"
STATUS=0
podman unshare nsenter --net="${NETNS_PATH}" "${PROBE_BIN}" -dns "${DNS_SERVER}" -timeout 3s "http://nonexistent-app-xyz:8080/health" || STATUS=$?
echo "Exit code for non-existent host: ${STATUS} (Expected 2)"

echo "--- 6. Negative test: Stopped container (Connection refused) ---"
podman stop "${TARGET_NAME}"
STATUS=0
podman unshare nsenter --net="${NETNS_PATH}" "${PROBE_BIN}" -dns "${DNS_SERVER}" -timeout 3s "http://${TARGET_NAME}:8080/health" || STATUS=$?
echo "Exit code for stopped container: ${STATUS} (Expected 2)"

echo "=== SPIKE 3.2 SUCCESS ==="
