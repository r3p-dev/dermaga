#!/usr/bin/env bash
# Test Quadlet golden units against the real podman-system-generator on dermaga-vm.
set -Eeuo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
GOLDEN_DIR="$PROJECT_ROOT/internal/quadlet/testdata"

echo "=== Quadlet Generator Dry-Run Verification on dermaga-vm ==="
echo "Timestamp: $(date -Is)"
echo "Git commit: $(git -C "$PROJECT_ROOT" rev-parse --short HEAD)"
if [[ -n "$(git -C "$PROJECT_ROOT" status --short)" ]]; then
    echo "Working tree status: dirty"
    git -C "$PROJECT_ROOT" status --short
else
    echo "Working tree status: clean"
fi
echo "Host: $(ssh dermaga-vm hostname)"
echo

REMOTE_TMP="$(ssh dermaga-vm 'mktemp -d /tmp/quadlet-verify-XXXXXX')"
trap 'ssh dermaga-vm "rm -rf $REMOTE_TMP"' EXIT

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
echo "=== Quadlet Dry-Run Verification: PASS ==="
