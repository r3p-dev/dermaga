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

EOF

# M7 invariants (negative control: test/vm/m7_invariants_control.sh).
ssh "$VM_HOST" python3 - < "$(dirname "$0")/m7_invariants.py"
echo "PASS: dermaga daemon, Traefik, Pebble and panel ready"

echo ""
echo "=== Dermaga M7 Demo State Ready ==="
echo "Access the web panel:"
echo "  1. Start SSH tunnel: ssh -N -L 7070:127.0.0.1:7070 $VM_HOST"
echo "  2. If uninitialized, open the http://localhost:7070/setup?token=... link printed above"
echo "  3. Otherwise open http://localhost:7070 -> app -> Deployments: Redeploy, or Roll back on a previous release"
echo "  4. HTTPS from the workstation: curl --cacert <Pebble root> --connect-to <domain>:443:<VM IP>:443 https://<domain>/"
