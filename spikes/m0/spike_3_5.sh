#!/usr/bin/env bash
set -euo pipefail

echo "=== SPIKE 3.5: Quadlet Notify=healthy, Limits, Dry-Run, DefaultDependencies, and Restart Backoff ==="
echo "Date: $(date -Is)"
echo "Host: $(hostname)"
echo "User: $(whoami)"

QUADLET_USER_DIR="${HOME}/.config/containers/systemd"
mkdir -p "${QUADLET_USER_DIR}"
QUADLET_GEN="/usr/lib/systemd/system-generators/podman-system-generator"

cleanup() {
  echo "--- Cleanup ---"
  systemctl --user stop spike-notify-test.service 2>/dev/null || true
  podman rm -f spike-notify-test 2>/dev/null || true
  rm -f "${QUADLET_USER_DIR}/spike-notify-test.container"
  rm -f "${QUADLET_USER_DIR}/spike-invalid-test.container"
  systemctl --user daemon-reload
  echo "Spike 3.5 cleaned up."
}
trap cleanup EXIT

echo "--- 1. Testing Quadlet Dry-Run Validation ---"
# Test 1a: Valid Quadlet file
cat << 'VALID_EOF' > "${QUADLET_USER_DIR}/spike-notify-test.container"
[Unit]
Description=Spike Notify Healthy Test
DefaultDependencies=false

[Container]
Image=docker.io/alpine/socat:latest
ContainerName=spike-notify-test
Notify=healthy
PidsLimit=128
NoNewPrivileges=true
ReadOnly=true
ReadOnlyTmpfs=true
DropCapability=ALL
PodmanArgs=--memory=128m
HealthCmd=test -f /tmp/healthy
HealthInterval=1s
HealthRetries=3
HealthStartPeriod=1s
Entrypoint=/bin/sh
Exec=-c "sleep 2 && touch /tmp/healthy && sleep 3600"

[Service]
Restart=on-failure
RestartSec=1s
RestartSteps=3
RestartMaxDelaySec=10s
TimeoutStartSec=15
VALID_EOF

echo "Running dryrun generator on valid file:"
DRYRUN_OUTPUT=$("${QUADLET_GEN}" --dryrun --user 2>&1)
echo "${DRYRUN_OUTPUT}" | grep -A 10 -E 'spike-notify-test.service'
echo "PASS: Valid Quadlet file parsed by generator"

# Test 1b: Invalid key behavior in Quadlet
cat << 'INVALID_EOF' > "${QUADLET_USER_DIR}/spike-invalid-test.container"
[Container]
Image=docker.io/alpine/socat:latest
InvalidUnknownKey=foobar
Memory=128m
INVALID_EOF

echo "Running dryrun generator on invalid file with unknown keys:"
INVALID_OUTPUT=$("${QUADLET_GEN}" --dryrun --user 2>&1) || true
echo "${INVALID_OUTPUT}" | grep -iE 'invalid|unsupported|unknown|memory|spike-invalid' || true
rm -f "${QUADLET_USER_DIR}/spike-invalid-test.container"

echo "--- 2. Testing DefaultDependencies=false and Limit Keys in Generated Service ---"
echo "${DRYRUN_OUTPUT}" | grep -E 'DefaultDependencies|pids-limit|memory|security-opt|cap-drop|read-only|RestartSteps|RestartMaxDelaySec'

echo "--- 3. Testing Notify=healthy Behavior (systemctl start blocks until healthy) ---"
systemctl --user daemon-reload

echo "Starting spike-notify-test.service (expecting ~2-3s delay while healthcheck passes)..."
START_TIME=$(date +%s%N)
systemctl --user start spike-notify-test.service
END_TIME=$(date +%s%N)
DURATION_MS=$(( (END_TIME - START_TIME) / 1000000 ))
echo "systemctl start finished in ${DURATION_MS} ms"

STATUS=$(systemctl --user is-active spike-notify-test.service)
echo "Service active status: ${STATUS}"
PODMAN_HEALTH=$(podman inspect spike-notify-test --format '{{.State.Health.Status}}')
echo "Podman container health status: ${PODMAN_HEALTH}"

if [ "${STATUS}" == "active" ] && [ "${PODMAN_HEALTH}" == "healthy" ]; then
  echo "PASS: Notify=healthy blocked until container became healthy and reported active"
else
  echo "FAIL: Expected active & healthy, got status=${STATUS}, health=${PODMAN_HEALTH}"
  exit 1
fi

echo "--- 4. Testing Stop Handling (systemctl stop) ---"
STOP_START=$(date +%s%N)
systemctl --user stop spike-notify-test.service
STOP_END=$(date +%s%N)
STOP_MS=$(( (STOP_END - STOP_START) / 1000000 ))
echo "systemctl stop finished in ${STOP_MS} ms"
systemctl --user is-active spike-notify-test.service || echo "Service cleanly stopped (inactive)"
podman ps -a --filter name=spike-notify-test | grep spike-notify-test || echo "Container automatically removed on stop (--rm behavior)"

echo "=== SPIKE 3.5 SUCCESS ==="
