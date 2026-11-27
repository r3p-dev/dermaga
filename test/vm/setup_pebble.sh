#!/usr/bin/env bash
# Local ACME for dermaga-vm; tests never use Let's Encrypt (DEC-44). Idempotent.
#
#   test/vm/setup_pebble.sh [VM_HOST] [up|down]      (default: dermaga-vm up)
#   PEBBLE_VALIDITY_SECONDS=300 test/vm/setup_pebble.sh   short certificates, to see the TLS-4 warning;
#                                                         run without it afterwards (default 7776000, 90 days)
#
# up starts, on podman network `web`:
#   pebble            Pebble with real validation: HTTP-01 on port 80 of the domain's A record
#   pebble-dns        pebble-challtestsrv, Pebble's DNS. Working test domains point to
#                     host.containers.internal (169.254.1.2): inside the rootless netns the VM's own
#                     address is local, so only that address reaches the host's sockets (M8 spike)
#   dermaga-test-dns  pebble-challtestsrv for dermaga's DNS check, on 127.0.0.1:8053 (VM loopback only),
#                     used through the user drop-in dermaga.service.d/90-test-dns.conf
#                     (DERMAGA_DNS_RESOLVER; `dermaga doctor` warns while it is set)
# then points Traefik's `le` resolver to Pebble (traefik.yml loses dermaga's marker, so a daemon restart
# cannot render it back to Let's Encrypt) and replays the DNS records kept by test/vm/test_dns.sh.
# Containers are recreated only when missing or their config changed: a new Pebble forgets every
# account and order (Traefik's account is then dropped from acme.json and Traefik restarted, so it
# registers again), and a new root signs the next certificates (the roots are collected in
# ~/.local/share/dermaga/traefik/pebble-minica.pem).
#
# down removes the three containers, the drop-in and the record list, and restarts dermaga. traefik.yml
# stays pointed at Pebble until the VM is reverted (SETUP §7.5).
set -Eeuo pipefail

VM_HOST="${1:-dermaga-vm}"
ACTION="${2:-up}"
VALIDITY="${PEBBLE_VALIDITY_SECONDS:-7776000}"
[[ $VALIDITY =~ ^[1-9][0-9]{1,8}$ ]] || { echo "PEBBLE_VALIDITY_SECONDS must be a number of seconds" >&2; exit 2; }
PEBBLE_IMAGE="ghcr.io/letsencrypt/pebble:2.10.3@sha256:0c81f05db371f436c97fb894bd6d2ab8fc9c8e2ce40f8bef8a8e15941bcd75ef"
DNS_IMAGE="ghcr.io/letsencrypt/pebble-challtestsrv:2.10.3@sha256:0152c8cb506e2ecba2900ed47d23932e27e3e6575a7d2c8ef93c7719ee3dea4c"

echo "date: $(date -Is)  host: $(hostname)  commit: $(git rev-parse --short HEAD 2>/dev/null || echo none)  action: $ACTION  validity: ${VALIDITY}s"

case $ACTION in
up | down) ;;
*)
  echo "usage: $0 [VM_HOST] [up|down]" >&2
  exit 2
  ;;
esac

ssh "$VM_HOST" bash -s -- "$ACTION" "$PEBBLE_IMAGE" "$DNS_IMAGE" "$VALIDITY" <<'EOF'
set -Eeuo pipefail
action=$1 pebble_image=$2 dns_image=$3 validity=$4
state=~/.config/dermaga-test
dropin_dir=~/.config/systemd/user/dermaga.service.d
dropin=$dropin_dir/90-test-dns.conf
traefik_dir=~/.local/share/dermaga/traefik

if [ "$action" = down ]; then
  podman rm -f pebble pebble-dns dermaga-test-dns >/dev/null 2>&1 || true
  rm -rf "$state"
  if [ -f "$dropin" ]; then
    rm -f "$dropin"
    rmdir "$dropin_dir" 2>/dev/null || true
    systemctl --user daemon-reload
    systemctl --user restart dermaga.service
  fi
  echo "removed: pebble, pebble-dns, dermaga-test-dns, $dropin, $state"
  echo "leftovers: $(podman ps -a --format '{{.Names}}' | grep -cE '^(pebble|pebble-dns|dermaga-test-dns)$' || true) containers," \
    "drop-in $([ -e "$dropin" ] && echo present || echo absent)"
  exit 0
fi

mkdir -p "$state"
cat > "$state/pebble-config.json.new" <<'JSON'
{
  "pebble": {
    "listenAddress": "0.0.0.0:14000",
    "managementListenAddress": "0.0.0.0:15000",
    "certificate": "test/certs/localhost/cert.pem",
    "privateKey": "test/certs/localhost/key.pem",
    "httpPort": 80,
    "tlsPort": 443,
    "ocspResponderURL": "",
    "externalAccountBindingRequired": false,
    "domainBlocklist": ["blocked.dermaga.test"],
    "retryAfter": { "authz": 3, "order": 5 },
    "keyAlgorithm": "ecdsa",
    "profiles": { "default": { "description": "certificate lifetime from setup_pebble.sh", "validityPeriod": 7776000 } }
  }
}
JSON
sed -i "s/\"validityPeriod\": 7776000/\"validityPeriod\": $validity/" "$state/pebble-config.json.new"
mv "$state/pebble-config.json.new" "$state/pebble-config.json"
want=$(sha256sum < "$state/pebble-config.json" | cut -c1-16)

# ensure NAME LABEL-VALUE ARGS... : (re)create the container unless it runs with the same label.
ensure() {
  local name=$1 tag=$2
  shift 2
  if [ "$(podman inspect "$name" --format '{{index .Config.Labels "dermaga-test"}} {{.State.Running}}' 2>/dev/null)" = "$tag true" ]; then
    echo "$name: up to date"
    return
  fi
  podman rm -f "$name" >/dev/null 2>&1 || true
  podman run -d --name "$name" --network web --label "dermaga-test=$tag" "$@" >/dev/null
  echo "$name: started"
}
dns_args=(-defaultIPv4 "" -defaultIPv6 "" -management :8055 -dnsserver :8053 -http01 "" -https01 "" -tlsalpn01 "" -doh "")
ensure pebble-dns "$dns_image" "$dns_image" "${dns_args[@]}"
ensure dermaga-test-dns "$dns_image" -p 127.0.0.1:8053:8053/udp -p 127.0.0.1:8053:8053/tcp "$dns_image" "${dns_args[@]}"
ensure pebble "$pebble_image-$want" -v "$state/pebble-config.json:/cfg/pebble-config.json:ro" "$pebble_image" \
  -config /cfg/pebble-config.json -dnsserver pebble-dns:8053 -strict=false

pip=""
for _ in $(seq 1 30); do
  pip=$(podman inspect pebble --format '{{(index .NetworkSettings.Networks "web").IPAddress}}' 2>/dev/null || true)
  if [ -n "$pip" ] && podman unshare --rootless-netns curl -fsSk --max-time 3 "https://$pip:15000/roots/0" -o "$state/root.pem"; then
    break
  fi
  sleep 1
done
[ -s "$state/root.pem" ] || { echo "pebble did not start" >&2; exit 1; }

# Roots of every Pebble run, so certificates issued before a restart still verify.
touch "$traefik_dir/pebble-minica.pem"
if ! grep -qF "$(sed -n 2p "$state/root.pem")" "$traefik_dir/pebble-minica.pem"; then
  cat "$state/root.pem" >> "$traefik_dir/pebble-minica.pem"
  echo "pebble root added to $traefik_dir/pebble-minica.pem"
fi
podman cp pebble:/test/certs/pebble.minica.pem "$traefik_dir/pebble-api-ca.pem"

# Traefik's le resolver -> Pebble. Rewritten (and Traefik restarted) only when it changes.
changed=$(python3 - "$traefik_dir/traefik.yml" <<'PY'
import json, sys
p = sys.argv[1]
raw = open(p).read()
lines = raw.splitlines(True)
body = "".join(lines[1:]) if lines and lines[0].startswith("#") else raw
cfg = json.loads(body)
acme = cfg["certificatesResolvers"]["le"]["acme"]
acme["caServer"] = "https://pebble:14000/dir"
acme["caCertificates"] = ["/etc/traefik/pebble-api-ca.pem"]
new = json.dumps(cfg, indent=2) + "\n"
if new != raw:
    open(p, "w").write(new)  # no dermaga marker: the reconciler treats it as foreign from now on
    print("yes")
PY
)
# A new Pebble container forgets every ACME account, and Traefik does not register again by itself
# (orders fail with accountDoesNotExist, M8a VM run). For each new Pebble container, drop Traefik's `le`
# account from acme.json (certificates stay) so the restart below registers a new one.
pebble_id=$(podman inspect pebble --format '{{.Id}}')
reset=no
if [ "$(cat "$state/pebble-account-for" 2>/dev/null)" != "$pebble_id" ]; then
  python3 - "$traefik_dir/acme.json" <<'PY'
import json, os, sys
p = sys.argv[1]
if os.path.exists(p) and os.path.getsize(p) > 0:
    d = json.load(open(p))
    if d.get("le", {}).get("Account"):
        d["le"]["Account"] = None
        tmp = p + ".new"
        with open(os.open(tmp, os.O_WRONLY | os.O_CREAT | os.O_TRUNC, 0o600), "w") as f:
            json.dump(d, f, indent=2)
        os.replace(tmp, p)
PY
  echo "$pebble_id" > "$state/pebble-account-for"
  reset=yes
  echo "Traefik's ACME account dropped for the new Pebble (certificates kept)"
fi
if [ "$changed" = yes ] || [ "$reset" = yes ]; then
  systemctl --user restart traefik.service
  echo "traefik restarted (traefik.yml changed: $changed, account reset: $reset)"
else
  echo "traefik.yml already points at Pebble; account current"
fi

mkdir -p "$dropin_dir"
printf '[Service]\nEnvironment=DERMAGA_DNS_RESOLVER=127.0.0.1:8053\n' > "$dropin.new"
if cmp -s "$dropin.new" "$dropin"; then
  rm -f "$dropin.new"
  echo "drop-in unchanged"
else
  mv "$dropin.new" "$dropin"
  systemctl --user daemon-reload
  systemctl --user restart dermaga.service
  echo "drop-in written; dermaga restarted"
fi
systemctl --user is-active traefik.service dermaga.service
EOF

"$(dirname "$0")/test_dns.sh" "$VM_HOST" replay
