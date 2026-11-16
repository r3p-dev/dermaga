#!/usr/bin/env bash
# Test Traefik golden configurations against real running Traefik on dermaga-vm.
set -Eeuo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
GOLDEN_DIR="$PROJECT_ROOT/internal/traefik/testdata"

echo "=== Traefik Golden Configurations Verification on dermaga-vm ==="
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

for f in "$GOLDEN_DIR"/*.golden; do
    base="$(basename "$f" .golden)"
    if [[ "$base" == "static-config" ]]; then
        # Static config is tested via JSON syntax validation on workstation
        echo -n "Validating static config $base (JSON syntax)... "
        jq . "$f" >/dev/null
        echo "OK"
        continue
    fi

    echo -n "Testing dynamic config $base... "
    # Ensure JSON syntax is valid on host first
    jq . "$f" >/dev/null

    # Clear recent logs marker by fetching log timestamp
    REMOTE_FILE="/home/server/.config/traefik/dynamic/test-${base}.yml"
    scp -q "$f" "dermaga-vm:$REMOTE_FILE"
    sleep 0.5

    # Check if Traefik logged any error
    ERRS=$(ssh dermaga-vm "podman logs --tail 10 traefik 2>&1 | grep -i 'level\":\"error' | grep 'test-${base}.yml' || true")
    ssh dermaga-vm "rm -f $REMOTE_FILE"
    sleep 0.2

    if [[ -n "$ERRS" ]]; then
        echo "FAILED"
        echo "Traefik reported error for $base:"
        echo "$ERRS"
        exit 1
    fi
    echo "OK (loaded by Traefik without errors)"
done

echo
echo "=== All Traefik Golden Configurations Verified: PASS ==="
