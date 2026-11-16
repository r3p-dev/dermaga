#!/usr/bin/env bash
set -euo pipefail

echo "=== SPIKE 3.6: podman build Build-Time Variables, Secrets, and Log Masking ==="
echo "Date: $(date -Is)"
echo "Host: $(hostname)"
echo "User: $(whoami)"
echo "Podman Version: $(podman --version)"

WORKDIR=$(mktemp -d /tmp/spike_3_6_XXXXXX)
cleanup() {
  echo "--- Cleanup ---"
  podman rmi -f spike-arg-norun spike-arg-run spike-arg-used spike-secret-mount 2>/dev/null || true
  rm -rf "${WORKDIR}"
  echo "Spike 3.6 cleaned up."
}
trap cleanup EXIT

SECRET_NORUN="secret_norun_val_111"
SECRET_RUN="secret_run_val_222"
SECRET_USED="secret_used_val_333"
SECRET_MOUNT="secret_mount_val_444"

echo ""
echo "--- 1. Case A: --build-arg WITHOUT RUN ---"
mkdir -p "${WORKDIR}/case_a"
cat << 'EOF' > "${WORKDIR}/case_a/Dockerfile"
FROM docker.io/alpine/socat:latest
ARG BUILD_SECRET
ENV APP_ENV=production
EOF

echo "Running podman build for Case A..."
BUILD_LOG_A="${WORKDIR}/build_a.log"
podman build \
  --build-arg "BUILD_SECRET=${SECRET_NORUN}" \
  -t spike-arg-norun \
  "${WORKDIR}/case_a" > "${BUILD_LOG_A}" 2>&1 || cat "${BUILD_LOG_A}"

echo "Build Log (Case A) snippet:"
cat "${BUILD_LOG_A}"

echo "Checking if secret value appeared in Case A build log:"
if grep -q "${SECRET_NORUN}" "${BUILD_LOG_A}"; then
  echo "WARNING: Secret value appeared in build log (STEP ARG)!"
else
  echo "Secret value NOT found in build log"
fi

echo "Checking podman history for Case A:"
podman history --no-trunc spike-arg-norun
echo "Checking if secret value is in podman history:"
if podman history --no-trunc spike-arg-norun | grep -q "${SECRET_NORUN}"; then
  echo "LEAK CONFIRMED: Secret value found in podman history!"
else
  echo "Secret value NOT in history"
fi

echo "Checking podman image inspect for Case A:"
if podman inspect spike-arg-norun | grep -q "${SECRET_NORUN}"; then
  echo "LEAK CONFIRMED: Secret value found in podman inspect!"
  podman inspect spike-arg-norun | grep -C 2 "${SECRET_NORUN}" || true
else
  echo "Secret value NOT in inspect"
fi

echo ""
echo "--- 2. Case B: --build-arg WITH RUN (arg not referenced in RUN) ---"
mkdir -p "${WORKDIR}/case_b"
cat << 'EOF' > "${WORKDIR}/case_b/Dockerfile"
FROM docker.io/alpine/socat:latest
ARG BUILD_SECRET
RUN echo "building application" > /tmp/build.txt
EOF

echo "Running podman build for Case B..."
BUILD_LOG_B="${WORKDIR}/build_b.log"
podman build \
  --build-arg "BUILD_SECRET=${SECRET_RUN}" \
  -t spike-arg-run \
  "${WORKDIR}/case_b" > "${BUILD_LOG_B}" 2>&1 || cat "${BUILD_LOG_B}"

echo "Build Log (Case B) snippet:"
cat "${BUILD_LOG_B}"

echo "Checking if secret value appeared in Case B build log:"
if grep -q "${SECRET_RUN}" "${BUILD_LOG_B}"; then
  echo "WARNING: Secret value appeared in build log (STEP ARG)!"
else
  echo "Secret value NOT found in build log"
fi

echo "Checking podman history for Case B:"
podman history --no-trunc spike-arg-run
echo "Checking if secret value is in podman history (Case B):"
if podman history --no-trunc spike-arg-run | grep -q "${SECRET_RUN}"; then
  echo "LEAK CONFIRMED: Secret value found in podman history!"
else
  echo "Secret value NOT in history"
fi

echo "Checking podman image inspect for Case B:"
if podman inspect spike-arg-run | grep -q "${SECRET_RUN}"; then
  echo "LEAK CONFIRMED: Secret value found in podman inspect!"
  podman inspect spike-arg-run | grep -C 2 "${SECRET_RUN}" || true
else
  echo "Secret value NOT in inspect"
fi

echo ""
echo "--- 3. Case C: --build-arg WITH RUN (arg explicitly used in RUN) ---"
mkdir -p "${WORKDIR}/case_c"
cat << 'EOF' > "${WORKDIR}/case_c/Dockerfile"
FROM docker.io/alpine/socat:latest
ARG BUILD_SECRET
RUN echo "Using build secret: ${BUILD_SECRET}" > /tmp/secret_output.txt
EOF

echo "Running podman build for Case C..."
BUILD_LOG_C="${WORKDIR}/build_c.log"
podman build \
  --build-arg "BUILD_SECRET=${SECRET_USED}" \
  -t spike-arg-used \
  "${WORKDIR}/case_c" > "${BUILD_LOG_C}" 2>&1 || cat "${BUILD_LOG_C}"

echo "Build Log (Case C) snippet:"
cat "${BUILD_LOG_C}"

echo "Checking if secret value appeared in Case C build log:"
if grep -q "${SECRET_USED}" "${BUILD_LOG_C}"; then
  echo "WARNING: Secret value appeared in build log (STEP RUN output)!"
else
  echo "Secret value NOT found in build log"
fi

echo "Checking podman history for Case C:"
podman history --no-trunc spike-arg-used
echo "Checking if secret value is in podman history (Case C):"
if podman history --no-trunc spike-arg-used | grep -q "${SECRET_USED}"; then
  echo "LEAK CONFIRMED: Secret value found in podman history!"
else
  echo "Secret value NOT in history"
fi

echo "Checking container filesystem for Case C:"
podman run --rm --entrypoint /bin/sh spike-arg-used -c "cat /tmp/secret_output.txt"
echo "Checking podman image inspect for Case C:"
if podman inspect spike-arg-used | grep -q "${SECRET_USED}"; then
  echo "LEAK CONFIRMED: Secret value found in podman inspect!"
else
  echo "Secret value NOT in inspect"
fi

echo ""
echo "--- 4. Case D: --secret with RUN --mount=type=secret ---"
mkdir -p "${WORKDIR}/case_d"
cat << 'EOF' > "${WORKDIR}/case_d/Dockerfile"
FROM docker.io/alpine/socat:latest
RUN --mount=type=secret,id=my_secret \
    cat /run/secrets/my_secret > /tmp/secret_hash.txt
EOF

SECRET_FILE="${WORKDIR}/secret_file.txt"
echo -n "${SECRET_MOUNT}" > "${SECRET_FILE}"
chmod 600 "${SECRET_FILE}"

echo "Running podman build with --secret..."
BUILD_LOG_D="${WORKDIR}/build_d.log"
podman build \
  --secret "id=my_secret,src=${SECRET_FILE}" \
  -t spike-secret-mount \
  "${WORKDIR}/case_d" > "${BUILD_LOG_D}" 2>&1 || cat "${BUILD_LOG_D}"

echo "Build Log (Case D) snippet:"
cat "${BUILD_LOG_D}"

echo "Checking if secret value appeared in Case D build log:"
if grep -q "${SECRET_MOUNT}" "${BUILD_LOG_D}"; then
  echo "WARNING: Secret value appeared in build log!"
else
  echo "PASS: Secret value NOT found in build log"
fi

echo "Checking podman history for Case D:"
podman history --no-trunc spike-secret-mount
echo "Checking if secret value is in podman history (Case D):"
if podman history --no-trunc spike-secret-mount | grep -q "${SECRET_MOUNT}"; then
  echo "FAIL: Secret value found in podman history!"
else
  echo "PASS: Secret value NOT in history"
fi

echo "Checking podman image inspect for Case D:"
if podman inspect spike-secret-mount | grep -q "${SECRET_MOUNT}"; then
  echo "FAIL: Secret value found in podman inspect!"
else
  echo "PASS: Secret value NOT in inspect"
fi

echo "Verifying secret mount does not persist at /run/secrets in built image:"
if podman run --rm --entrypoint /bin/sh spike-secret-mount -c "ls -la /run/secrets" 2>&1 | grep -q "No such file"; then
  echo "PASS: /run/secrets does not exist in built image"
else
  echo "Notice /run/secrets check result:"
  podman run --rm --entrypoint /bin/sh spike-secret-mount -c "ls -la /run/secrets || true"
fi

echo ""
echo "=== SPIKE 3.6 COMPLETE ==="
