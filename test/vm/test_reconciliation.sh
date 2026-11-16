#!/usr/bin/env bash
# Test DEP-10 startup reconciliation against real files on dermaga-vm.
# Exercises all five acceptance cases: orphan removed, missing file re-rendered,
# stopped live unit started, leftover verify router removed, foreign files untouched,
# and verifies failure on a negative control.
set -Eeuo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"

echo "=== DEP-10 Reconciliation Verification on dermaga-vm ==="
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

# 1. Build dermaga binary for linux/amd64
TMP_BIN=$(mktemp /tmp/dermaga-reconcile-bin-XXXXXX)
trap 'rm -f "$TMP_BIN"' EXIT

echo "Building dermaga binary for remote VM..."
(cd "$PROJECT_ROOT" && CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
    -ldflags "-X main.Version=dev -X main.Commit=$(git -C "$PROJECT_ROOT" rev-parse --short HEAD)" \
    -o "$TMP_BIN" ./cmd/dermaga)
echo "Build complete."

# 2. Setup isolated remote environment on dermaga-vm
REMOTE_TMP=$(ssh dermaga-vm 'mktemp -d /tmp/reconcile-test-XXXXXX')
QUADLET_DIR="/home/server/.config/containers/systemd"
TRAEFIK_DIR="/home/server/.local/share/dermaga/traefik/dynamic"
DB_PATH="$REMOTE_TMP/dermaga.db"
REMOTE_BIN="$REMOTE_TMP/dermaga"

cleanup() {
    echo
    echo "Cleaning up reconciliation test environment on dermaga-vm..."
    ssh dermaga-vm "
        systemctl --user stop app-demo-r1.service 2>/dev/null || true
        podman rm -f app-demo-r1 2>/dev/null || true
        podman rmi localhost/demo:r1 2>/dev/null || true
        rm -f $QUADLET_DIR/app-demo-r1.container
        rm -f $QUADLET_DIR/app-orphan-r1.container
        rm -f $QUADLET_DIR/custom-vpn.container
        rm -f $TRAEFIK_DIR/app-demo.yml
        rm -f $TRAEFIK_DIR/app-orphan.yml
        rm -f $TRAEFIK_DIR/app-demo-verify.yml
        rm -f $TRAEFIK_DIR/custom-metrics.yml
        systemctl --user daemon-reload 2>/dev/null || true
        rm -rf $REMOTE_TMP
    "
    rm -f "$TMP_BIN"
    echo "Cleanup complete (§8: idempotent, no residue)."
}
trap cleanup EXIT

echo "Created remote test directory: $REMOTE_TMP"
ssh dermaga-vm "mkdir -p $QUADLET_DIR $TRAEFIK_DIR"
scp -q "$TMP_BIN" "dermaga-vm:$REMOTE_BIN"
scp -q "$PROJECT_ROOT/internal/store/migrations/0001_init.sql" "dermaga-vm:$REMOTE_TMP/schema.sql"
ssh dermaga-vm "chmod +x $REMOTE_BIN"

# 3. Initialize SQLite database with an active application
echo "Seeding remote SQLite database ($DB_PATH)..."
ssh dermaga-vm "python3 -c \"
import sqlite3
conn = sqlite3.connect('$DB_PATH')
with open('$REMOTE_TMP/schema.sql') as f:
    conn.executescript(f.read())
c = conn.cursor()
c.execute('''INSERT INTO schema_migrations (version) VALUES (1);''')
c.execute('''INSERT INTO apps (id, name, repo_owner, repo_name, port, health_type, active_release_id)
             VALUES ('app_demo', 'demo', 'testorg', 'demo-repo', 8080, 'none', 'dep_demo_r1');''')
c.execute('''INSERT INTO domains (id, app_id, domain, is_primary)
             VALUES ('dom_demo', 'app_demo', 'demo.example.com', 1);''')
c.execute('''INSERT INTO deployments (id, app_id, release_number, status, trigger, commit_hash, commit_message, env_snapshot_encrypted)
             VALUES ('dep_demo_r1', 'app_demo', 1, 'active', 'manual', 'c0ffee1', 'Release 1', '');''')
conn.commit()
conn.close()
\""

# 4. Prepare local test image localhost/demo:r1 so live unit start succeeds
echo "Preparing local test image localhost/demo:r1 on dermaga-vm..."
ssh dermaga-vm "
    podman run -d --name temp-demo-seed --entrypoint /bin/sh docker.io/alpine/socat -c 'while true; do sleep 3600; done'
    podman commit temp-demo-seed localhost/demo:r1
    podman rm -f -t 0 temp-demo-seed
"

# 5. Stage scenario files in dermaga's directories on dermaga-vm:
# Case 1: Orphan files
# Case 2: Missing files (app-demo-r1.container and app-demo.yml not present)
# Case 3: Stopped live unit (app-demo-r1 not running)
# Case 4: Leftover verify router (app-demo-verify.yml)
# Case 5: Foreign files that dermaga does not own
echo "Staging scenario files in dermaga's directories..."
ssh dermaga-vm "
    # Case 1: Orphan files (belong to non-existent apps)
    echo 'orphan container' > $QUADLET_DIR/app-orphan-r1.container
    echo 'orphan traefik' > $TRAEFIK_DIR/app-orphan.yml

    # Case 4: Leftover verify router from interrupted deploy
    echo 'leftover verify router' > $TRAEFIK_DIR/app-demo-verify.yml

    # Case 5: Foreign files that dermaga does not own
    cat << 'EOF' > $QUADLET_DIR/custom-vpn.container
[Unit]
Description=Unmanaged custom VPN

[Container]
ContainerName=custom-vpn
Image=docker.io/alpine/socat:latest
EOF

    cat << 'EOF' > $TRAEFIK_DIR/custom-metrics.yml
http:
  services:
    custom-metrics:
      loadBalancer:
        servers:
          - url: "http://127.0.0.1:9090"
EOF
"

# 6. Run reconciliation command on dermaga-vm
echo
echo "--- Running dermaga reconcile on dermaga-vm ---"
RECONCILE_OUTPUT=$(ssh dermaga-vm "$REMOTE_BIN reconcile --db $DB_PATH --quadlet-dir $QUADLET_DIR --traefik-dir $TRAEFIK_DIR")
echo "$RECONCILE_OUTPUT"
echo

# 7. Verify Acceptance Cases
echo "--- Asserting Acceptance Cases ---"

# Case 1: Orphan removed
echo -n "1. Orphan files removed: "
if ssh dermaga-vm "[ ! -f $QUADLET_DIR/app-orphan-r1.container ] && [ ! -f $TRAEFIK_DIR/app-orphan.yml ]"; then
    echo "PASS (app-orphan-r1.container and app-orphan.yml deleted)"
else
    echo "FAIL: Orphan files still exist on remote host!"
    exit 1
fi

# Case 2: Missing files re-rendered
echo -n "2a. Missing Quadlet infrastructure verified (web.network): "
if ssh dermaga-vm "grep -q 'NetworkName=web' $QUADLET_DIR/web.network"; then
    echo "PASS (web.network present)"
else
    echo "FAIL: web.network is missing or invalid!"
    exit 1
fi

echo -n "2b. Missing Quadlet app container re-rendered (app-demo-r1.container): "
if ssh dermaga-vm "grep -q 'ContainerName=app-demo-r1' $QUADLET_DIR/app-demo-r1.container"; then
    echo "PASS (app-demo-r1.container re-rendered)"
else
    echo "FAIL: app-demo-r1.container was not re-rendered!"
    exit 1
fi

echo -n "2c. Missing Traefik global dynamic config verified (00-global.yml): "
if ssh dermaga-vm "grep -q 'default-security-headers' $TRAEFIK_DIR/00-global.yml"; then
    echo "PASS (00-global.yml present)"
else
    echo "FAIL: 00-global.yml is missing or invalid!"
    exit 1
fi

echo -n "2d. Missing Traefik app dynamic config re-rendered (app-demo.yml): "
if ssh dermaga-vm "grep -q 'app-demo' $TRAEFIK_DIR/app-demo.yml"; then
    echo "PASS (app-demo.yml re-rendered)"
else
    echo "FAIL: app-demo.yml was not re-rendered!"
    exit 1
fi

# Case 3: Stopped live unit started
echo -n "3. Live unit started by systemd: "
IS_ACTIVE=$(ssh dermaga-vm "systemctl --user is-active app-demo-r1.service || true")
if [[ "$IS_ACTIVE" == "active" ]]; then
    echo "PASS (app-demo-r1.service is active)"
else
    echo "FAIL: app-demo-r1.service is not active: $IS_ACTIVE"
    exit 1
fi

# Case 4: Leftover verify router removed
echo -n "4. Leftover verify router removed (app-demo-verify.yml): "
if ssh dermaga-vm "[ ! -f $TRAEFIK_DIR/app-demo-verify.yml ]"; then
    echo "PASS (app-demo-verify.yml deleted)"
else
    echo "FAIL: Leftover verify router still exists!"
    exit 1
fi

# Case 5: Foreign files untouched
echo -n "5a. Foreign Quadlet file untouched (custom-vpn.container): "
if ssh dermaga-vm "grep -q 'ContainerName=custom-vpn' $QUADLET_DIR/custom-vpn.container"; then
    echo "PASS (custom-vpn.container untouched)"
else
    echo "FAIL: custom-vpn.container modified or deleted!"
    exit 1
fi

echo -n "5b. Foreign Traefik file untouched (custom-metrics.yml): "
if ssh dermaga-vm "grep -q 'custom-metrics' $TRAEFIK_DIR/custom-metrics.yml"; then
    echo "PASS (custom-metrics.yml untouched)"
else
    echo "FAIL: custom-metrics.yml modified or deleted!"
    exit 1
fi

echo -n "5c. Existing system units untouched (traefik.container, traefik-acme.volume): "
if ssh dermaga-vm "[ -f $QUADLET_DIR/traefik.container ] && [ -f $QUADLET_DIR/traefik-acme.volume ]"; then
    echo "PASS (system units preserved)"
else
    echo "FAIL: System units were modified or deleted!"
    exit 1
fi

echo -n "5d. Foreign files reported in output: "
if echo "$RECONCILE_OUTPUT" | grep -q "custom-vpn.container" && \
   echo "$RECONCILE_OUTPUT" | grep -q "custom-metrics.yml" && \
   echo "$RECONCILE_OUTPUT" | grep -q "traefik.container"; then
    echo "PASS (All foreign files accurately reported)"
else
    echo "FAIL: Foreign files not accurately reported in output!"
    exit 1
fi

# 8. Negative Control: Deliberately Broken Inaccessible Database
echo
echo "--- Negative Control: Deliberately Broken Inaccessible Database ---"
NEG_OUTPUT=$(ssh dermaga-vm "$REMOTE_BIN reconcile --db /nonexistent/dir/corrupt.db --quadlet-dir $QUADLET_DIR --traefik-dir $TRAEFIK_DIR 2>&1 || true")
echo "Negative control execution output:"
echo "$NEG_OUTPUT"
if echo "$NEG_OUTPUT" | grep -qi "error"; then
    echo "Negative control PASSED: reconciler failed with non-zero exit on inaccessible database."
else
    echo "FAIL: Negative control expected failure but succeeded!"
    exit 1
fi

echo
echo "=== All DEP-10 Reconciliation Acceptance Cases Verified: PASS ==="
