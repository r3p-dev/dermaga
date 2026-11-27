#!/usr/bin/env bash
set -euo pipefail

# test/vm/seed.sh - Brings dermaga-vm to a usable demo state for M8 (daemon, Traefik, Pebble with real
# validation and the test DNS, apps with releases to redeploy and roll back, domains in each
# certificate state) and checks the M7/M8 invariants. Idempotent.
# Apps and their domains are set in the panel (they need the GitHub App); this script never creates or
# deletes an app. DNS cases of the demo domains (test/vm/test_dns.sh) are kept on the VM and replayed.

echo "=== Seeding dermaga-vm for Milestone M8 ==="
echo "Timestamp: $(date -Is)"
echo "Host: $(hostname)"
echo "Commit: $(git rev-parse --short HEAD)"

VM_HOST="${VM_HOST:-dermaga-vm}"

if ! ssh -q "$VM_HOST" true; then
  echo "ERROR: Cannot reach $VM_HOST via SSH." >&2
  exit 1
fi

# Pebble (local ACME CA) with real validation and the test DNS; never the real Let's Encrypt. Idempotent:
# running containers with the same config are kept.
here=$(dirname "$0")
"$here/setup_pebble.sh" "$VM_HOST" up
# Demo domains and their DNS case (DEMO_DNS="domain:case ..."; cases: good, wrong, failing).
for entry in ${DEMO_DNS:-testapp.dermaga.test:good hooks.dermaga.test:good www.testapp.dermaga.test:good good.dermaga.test:good wrong.dermaga.test:wrong fail.dermaga.test:failing}; do
  "$here/test_dns.sh" "$VM_HOST" "${entry##*:}" "${entry%%:*}"
done

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

# M7 and M8 invariants (negative control: test/vm/m7_invariants_control.sh).
ssh "$VM_HOST" python3 - < "$here/m7_invariants.py"
echo "PASS: dermaga daemon, Traefik, Pebble, test DNS and panel ready"

echo ""
echo "=== Dermaga M8 Demo State Ready ==="
echo "Access the web panel:"
echo "  1. Start SSH tunnel: ssh -N -L 7070:127.0.0.1:7070 $VM_HOST"
echo "  2. If uninitialized, open the http://localhost:7070/setup?token=... link printed above"
echo "  3. Otherwise open http://localhost:7070 -> app -> Deployments (Redeploy, Roll back) or Domains"
echo "     (testapp: good.dermaga.test active, wrong.dermaga.test waiting for DNS, fail.dermaga.test failed"
echo "     with the next try, www.testapp.dermaga.test redirecting; change a case with test/vm/test_dns.sh,"
echo "     then press Check again)"
echo "  4. HTTPS from the workstation: scp $VM_HOST:.local/share/dermaga/traefik/pebble-minica.pem /tmp/roots.pem; then"
echo "     curl --cacert /tmp/roots.pem --connect-to <domain>:443:<VM IP>:443 https://<domain>/"
