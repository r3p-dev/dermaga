#!/usr/bin/env bash
# Sets up Pebble (local ACME test CA) on dermaga-vm network `web` and configures
# Traefik's `le` resolver to use Pebble + its API CA cert (idempotent).
set -Eeuo pipefail

VM_HOST="${1:-dermaga-vm}"
PEBBLE_IMAGE="ghcr.io/letsencrypt/pebble:2.10.3@sha256:0c81f05db371f436c97fb894bd6d2ab8fc9c8e2ce40f8bef8a8e15941bcd75ef"

ssh "$VM_HOST" bash -s -- "$PEBBLE_IMAGE" <<'EOF'
set -Eeuo pipefail
pebble_image="$1"

podman rm -f pebble >/dev/null 2>&1 || true
podman run -d --name pebble --network web -e PEBBLE_VA_ALWAYS_VALID=1 "$pebble_image" -strict=false >/dev/null

pip=""
for _ in $(seq 1 20); do
  pip="$(podman inspect pebble --format '{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}' 2>/dev/null || true)"
  if [ -n "$pip" ] && podman unshare --rootless-netns curl -fsSk "https://${pip}:15000/roots/0" >/dev/null 2>&1; then
    break
  fi
  sleep 1
done
if [ -z "$pip" ]; then
  echo "pebble failed to start on network web" >&2
  exit 1
fi

podman unshare --rootless-netns curl -fsSk "https://${pip}:15000/roots/0" > ~/.local/share/dermaga/traefik/pebble-minica.pem
podman unshare --rootless-netns openssl s_client -showcerts -connect "${pip}:14000" </dev/null 2>/dev/null \
  | awk '/BEGIN CERTIFICATE/,/END CERTIFICATE/{print}' > ~/.local/share/dermaga/traefik/pebble-api-ca.pem

python3 -c '
import json, os
p = os.path.expanduser("~/.local/share/dermaga/traefik/traefik.yml")
raw = open(p).read()
lines = raw.splitlines(True)
header = lines[0] if lines and lines[0].startswith("#") else ""
body = "".join(lines[1:] if header else lines)
cfg = json.loads(body)
acme = cfg["certificatesResolvers"]["le"]["acme"]
acme["caServer"] = "https://pebble:14000/dir"
acme["caCertificates"] = ["/etc/traefik/pebble-api-ca.pem"]
open(p, "w").write(header + json.dumps(cfg, indent=2) + "\n")
'

systemctl --user restart traefik.service
systemctl --user is-active traefik.service
EOF
