#!/usr/bin/env bash
# test/vm/gosmee-forward.sh - gosmee --exec forwarder for real GitHub deliveries to dermaga-vm (dev
# only, docs/SETUP.md §4.2). gosmee cannot pin a name to an IP or trust Pebble's root, so this hook
# posts the delivery body byte for byte (curl --data-binary) with GitHub's headers to the VM's public
# hook path (Traefik 443 -> hook-relay -> hooks.sock), with --resolve and --cacert.
#
#   tmp/tools/bin/gosmee client --nocolor --noReplay --exec "$PWD/test/vm/gosmee-forward.sh" \
#     "$(cat tmp/smee-url)" http://127.0.0.1:9
#
# Logs one line per delivery (time, event, delivery ID, body size and hash, HTTP status) to
# $GOSMEE_FORWARD_DIR/logs/gosmee-forward.log and keeps body and headers under
# $GOSMEE_FORWARD_DIR/deliveries/ for later checks (default dir: tmp/evidence/M7, gitignored). Never
# logs the body, the signature or the smee URL. Overrides: HOOK_DOMAIN, VM_IP, PEBBLE_ROOT.
set -u
root=$(cd "$(dirname "$0")/../.." && pwd)
out=${GOSMEE_FORWARD_DIR:-$root/tmp/evidence/M7}
domain=${HOOK_DOMAIN:-hooks.dermaga.test}
ip=${VM_IP:-192.168.122.217}
cacert=${PEBBLE_ROOT:-$out/pebble-minica.pem}
hdr() { jq -r --arg k "$1" '.[$k] // empty' "$GOSMEE_HEADERS_FILE"; }
args=(-sS -o /dev/null -w '%{http_code}' --max-time 20
  --resolve "$domain:443:$ip" --cacert "$cacert"
  -H "Content-Type: $(hdr Content-Type)" -H "User-Agent: $(hdr User-Agent)"
  -H "X-GitHub-Event: $(hdr X-Github-Event)" -H "X-GitHub-Delivery: $(hdr X-Github-Delivery)"
  -H "X-GitHub-Hook-ID: $(hdr X-Github-Hook-Id)" -H "X-Hub-Signature-256: $(hdr X-Hub-Signature-256)"
  --data-binary "@$GOSMEE_PAYLOAD_FILE")
code=$(curl "${args[@]}" "https://$domain/hooks/github-app")
d=$(hdr X-Github-Delivery)
mkdir -p "$out/logs" "$out/deliveries"
cp "$GOSMEE_PAYLOAD_FILE" "$out/deliveries/$d.body"; cp "$GOSMEE_HEADERS_FILE" "$out/deliveries/$d.headers"
printf '%s event=%s delivery=%s bytes=%s sha256=%s status=%s\n' "$(date -Is)" "$(hdr X-Github-Event)" "$d" \
  "$(wc -c < "$GOSMEE_PAYLOAD_FILE")" "$(sha256sum < "$GOSMEE_PAYLOAD_FILE" | cut -c1-12)" "$code" |
  tee -a "$out/logs/gosmee-forward.log"
