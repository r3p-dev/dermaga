#!/usr/bin/env bash
set -euo pipefail

# test/vm/seed.sh - Brings dermaga-vm to a usable demo state for M7 (daemon, Traefik, Pebble, apps
# with releases to redeploy and roll back) and checks the M7 invariants. Idempotent.
# Apps are created in the panel (they need the GitHub App); this script never creates or deletes one.

echo "=== Seeding dermaga-vm for Milestone M7 ==="
echo "Timestamp: $(date -Is)"
echo "Host: $(hostname)"
echo "Commit: $(git rev-parse --short HEAD)"

VM_HOST="${VM_HOST:-dermaga-vm}"

if ! ssh -q "$VM_HOST" true; then
  echo "ERROR: Cannot reach $VM_HOST via SSH." >&2
  exit 1
fi

# Pebble (local ACME CA) is needed for HTTPS to the apps; never the real Let's Encrypt.
if ! ssh "$VM_HOST" 'podman container exists pebble && [ "$(podman inspect pebble --format "{{.State.Running}}")" = true ]'; then
  "$(dirname "$0")/setup_pebble.sh" "$VM_HOST"
fi

ssh "$VM_HOST" 'bash -s' << 'EOF'
set -euo pipefail

if [ ! -f ~/.local/bin/dermaga ]; then
  echo "ERROR: ~/.local/bin/dermaga not found on VM. Run the installer first." >&2
  exit 1
fi

systemctl --user start dermaga.service
systemctl --user is-active --quiet dermaga.service traefik.service traefik-http.socket traefik-https.socket || {
  echo "ERROR: dermaga or Traefik user units are not active." >&2
  systemctl --user status dermaga.service traefik.service traefik-http.socket traefik-https.socket --no-pager || true
  exit 1
}

~/.local/bin/dermaga version
~/.local/bin/dermaga doctor || true
~/.local/bin/dermaga panel
grep -q '"verify"' ~/.local/share/dermaga/traefik/traefik.yml || { echo "ERROR: traefik.yml has no verify entrypoint" >&2; exit 1; }

# M7 invariants: exactly one live release per deployed app, no verify router left in a route file,
# a running container only for live releases, an image for the live release and each retained one.
python3 - << 'PY'
import glob, os, sqlite3, subprocess, sys
db = os.path.expanduser("~/.local/share/dermaga/dermaga.db")
c = sqlite3.connect(db)
running = set(subprocess.run(["podman", "ps", "--format", "{{.Names}}"], capture_output=True, text=True).stdout.split())
images = set(subprocess.run(["podman", "images", "--format", "{{.Repository}}:{{.Tag}}"], capture_output=True, text=True).stdout.split())
bad = 0
for app_id, name, live in c.execute("SELECT id, name, active_release_id FROM apps ORDER BY name"):
    domain = c.execute("SELECT domain FROM domains WHERE app_id=? AND is_primary=1", (app_id,)).fetchone()
    rels = c.execute("SELECT id, release_number, status, image_ref FROM deployments WHERE app_id=? ORDER BY release_number DESC", (app_id,)).fetchall()
    print(f"App {name} ({domain[0] if domain else '-'}): " + " ".join(f"r{n}:{s}" for _, n, s, _ in rels))
    active = [r for r in rels if r[2] == "active"]
    if live and (len(active) != 1 or active[0][0] != live):
        print(f"  FAIL: {len(active)} active releases, live={live}"); bad += 1
    for _, n, s, img in rels:
        cont = f"app-{name}-r{n}"
        if (cont in running) != (s == "active"):
            print(f"  FAIL: r{n} ({s}) container running={cont in running}"); bad += 1
        if s == "active" and img and img not in images:
            print(f"  FAIL: live image {img} missing"); bad += 1
    retained = [n for _, n, s, img in rels if s in ("inactive", "rolled_back") and img in images]
    print(f"  live: r{active[0][1] if active else '-'}; releases with an image for rollback: {['r%d' % n for n in retained]}")
for f in glob.glob(os.path.expanduser("~/.local/share/dermaga/traefik/dynamic/app-*.yml")):
    if "-verify" in open(f).read():
        print(f"FAIL: verify router left in {os.path.basename(f)}"); bad += 1
print("PASS: M7 invariants hold" if bad == 0 else f"FAIL: {bad} invariant(s) broken")
sys.exit(1 if bad else 0)
PY
echo "PASS: dermaga daemon, Traefik, Pebble and panel ready"
EOF

echo ""
echo "=== Dermaga M7 Demo State Ready ==="
echo "Access the web panel:"
echo "  1. Start SSH tunnel: ssh -N -L 7070:127.0.0.1:7070 $VM_HOST"
echo "  2. If uninitialized, open the http://localhost:7070/setup?token=... link printed above"
echo "  3. Otherwise open http://localhost:7070 -> app -> Deployments: Redeploy, or Roll back on a previous release"
echo "  4. HTTPS from the workstation: curl --cacert <Pebble root> --connect-to <domain>:443:<VM IP>:443 https://<domain>/"
