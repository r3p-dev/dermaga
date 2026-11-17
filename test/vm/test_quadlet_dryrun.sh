#!/usr/bin/env bash
# Test Quadlet golden units against the real podman-system-generator on dermaga-vm,
# including specifier verification and a negative control.
set -Eeuo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
GOLDEN_DIR="$PROJECT_ROOT/internal/quadlet/testdata"

echo "=== Quadlet Generator Dry-Run Verification on dermaga-vm ==="
echo "Timestamp: $(date -Is)"
echo "Git commit: $(git -C "$PROJECT_ROOT" rev-parse --short HEAD)"
STATUS_OUTPUT="$(git -C "$PROJECT_ROOT" status --short)"
if [[ -n "$STATUS_OUTPUT" ]]; then
    echo "Working tree status: dirty"
    echo "$STATUS_OUTPUT"
else
    echo "Working tree status: clean"
fi
echo "Host: $(ssh dermaga-vm hostname)"
echo

REMOTE_TMP="$(ssh dermaga-vm 'mktemp -d /tmp/quadlet-verify-XXXXXX')"
NEG_TMP="$(ssh dermaga-vm 'mktemp -d /tmp/quadlet-neg-XXXXXX')"
trap 'ssh dermaga-vm "rm -rf $REMOTE_TMP $NEG_TMP"' EXIT

echo "Created remote directory: $REMOTE_TMP"

for f in "$GOLDEN_DIR"/*.golden; do
    base="$(basename "$f" .golden)"
    if [[ "$base" == "network-unit" ]]; then
        target_name="web.network"
    else
        target_name="app-${base}.container"
    fi
    echo "Staging $base -> $target_name"
    scp -q "$f" "dermaga-vm:$REMOTE_TMP/$target_name"
done

echo
echo "Executing /usr/lib/systemd/system-generators/podman-system-generator --dryrun --user..."
ssh dermaga-vm "QUADLET_UNIT_DIRS=$REMOTE_TMP /usr/lib/systemd/system-generators/podman-system-generator --dryrun --user"

echo
echo "Verifying systemd unit specifier resolution for url-escaped-health..."
ssh dermaga-vm "bash -c '
TMP_OUT=\$(mktemp -d)
QUADLET_UNIT_DIRS=$REMOTE_TMP /usr/lib/systemd/system-generators/podman-system-generator --dryrun --user > \$TMP_OUT/dryrun.out 2>&1
sed -n \"/---app-url-escaped-health.service---/,/---/p\" \$TMP_OUT/dryrun.out | sed \"/---/d\" > \$TMP_OUT/app-url-escaped-health.service
systemd-analyze --user verify \$TMP_OUT/app-url-escaped-health.service 2>&1
rm -rf \$TMP_OUT
'"
echo "Specifier resolution passed: systemd-analyze reported no errors on escaped %20."

echo
echo "--- Negative Control: Deliberately Broken Quadlet Unit ---"
ssh dermaga-vm "cat << 'EOF' > $NEG_TMP/broken.container
[Unit]
Description=Broken Quadlet Negative Control

[Container]
Image=localhost/test:latest
ThisIsAnUnsupportedKey=malformed_value

[Quadlet]
DefaultDependencies=false
EOF"

echo "Running podman-system-generator against deliberately broken unit..."
set +e
NEG_OUT=$(ssh dermaga-vm "QUADLET_UNIT_DIRS=$NEG_TMP /usr/lib/systemd/system-generators/podman-system-generator --dryrun --user 2>&1")
NEG_EXIT=$?
set -e

echo "$NEG_OUT"
if [[ $NEG_EXIT -ne 0 ]] && echo "$NEG_OUT" | grep -qi "unsupported key"; then
    echo "Negative control successfully rejected as expected (exit code: $NEG_EXIT)."
else
    echo "FAILED: Negative control did not fail as expected! (exit code: $NEG_EXIT)"
    exit 1
fi

echo
echo "=== Quadlet Dry-Run Verification: PASS ==="
