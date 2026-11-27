#!/usr/bin/env bash
# DNS records of test domains on dermaga-vm (needs test/vm/setup_pebble.sh up). Idempotent.
#
#   test/vm/test_dns.sh [VM_HOST] good|wrong|failing DOMAIN   set a domain's case and remember it
#   test/vm/test_dns.sh [VM_HOST] forget DOMAIN               remove its records and forget it
#   test/vm/test_dns.sh [VM_HOST] replay                      set every remembered domain again
#   test/vm/test_dns.sh [VM_HOST] list                        show the remembered domains
#
# Cases (dermaga's DNS check asks dermaga-test-dns, Pebble asks pebble-dns):
#   good     dermaga sees the VM's address; Pebble reaches the host through 169.254.1.2 -> certificate
#   wrong    no record anywhere -> dermaga shows "waiting for DNS" with the record to create
#   failing  dermaga sees the VM's address (DNS gate passes); Pebble gets 192.0.2.1 (TEST-NET-1,
#            unreachable) -> the HTTP-01 challenge fails with a timeout
# challtestsrv keeps records in memory; the list in ~/.config/dermaga-test/dns-records survives a
# restart of its containers and is replayed by setup_pebble.sh.
set -Eeuo pipefail

VM_HOST=dermaga-vm
if [[ $# -gt 0 && $1 != good && $1 != wrong && $1 != failing && $1 != forget && $1 != replay && $1 != list ]]; then
  VM_HOST=$1
  shift
fi
cmd=${1:-list}
domain=${2:-}
case $cmd in
good | wrong | failing | forget)
  [[ $domain =~ ^[a-z0-9]([a-z0-9.-]{0,251}[a-z0-9])?$ ]] || {
    echo "usage: $0 [VM_HOST] good|wrong|failing|forget DOMAIN" >&2
    exit 2
  }
  ;;
replay | list) ;;
*)
  echo "usage: $0 [VM_HOST] good|wrong|failing|forget DOMAIN | replay | list" >&2
  exit 2
  ;;
esac

ssh "$VM_HOST" bash -s -- "$cmd" "$domain" <<'EOF'
set -Eeuo pipefail
cmd=$1 domain=${2:-}
list=~/.config/dermaga-test/dns-records
mkdir -p "$(dirname "$list")"
touch "$list"

server_ip=$(ip -4 route get 192.0.2.1 | awk '{for (i = 1; i < NF; i++) if ($i == "src") print $(i + 1)}')
mgmt() { # CONTAINER PATH JSON
  local ip
  ip=$(podman inspect "$1" --format '{{(index .NetworkSettings.Networks "web").IPAddress}}')
  podman unshare --rootless-netns curl -fsS --max-time 5 -d "$3" "http://$ip:8055$2"
}
set_case() { # DOMAIN CASE
  local d=$1 c=$2 json
  json=$(printf '{"host":"%s."}' "$d")
  mgmt dermaga-test-dns /clear-a "$json"
  mgmt pebble-dns /clear-a "$json"
  case $c in
  good)
    mgmt dermaga-test-dns /add-a "$(printf '{"host":"%s.","addresses":["%s"]}' "$d" "$server_ip")"
    mgmt pebble-dns /add-a "$(printf '{"host":"%s.","addresses":["169.254.1.2"]}' "$d")"
    ;;
  failing)
    mgmt dermaga-test-dns /add-a "$(printf '{"host":"%s.","addresses":["%s"]}' "$d" "$server_ip")"
    mgmt pebble-dns /add-a "$(printf '{"host":"%s.","addresses":["192.0.2.1"]}' "$d")"
    ;;
  esac
  echo "$d: $c (dermaga sees $( [ "$c" = wrong ] && echo nothing || echo "$server_ip" ), Pebble sees $(case $c in good) echo 169.254.1.2 ;; failing) echo 192.0.2.1 ;; *) echo nothing ;; esac))"
}

case $cmd in
good | wrong | failing)
  set_case "$domain" "$cmd"
  { grep -v "^$domain " "$list" || true; echo "$domain $cmd"; } > "$list.new"
  mv "$list.new" "$list"
  ;;
forget)
  set_case "$domain" wrong >/dev/null
  { grep -v "^$domain " "$list" || true; } > "$list.new"
  mv "$list.new" "$list"
  echo "$domain: forgotten"
  ;;
replay)
  while read -r d c; do
    [ -n "$d" ] && set_case "$d" "$c"
  done < "$list"
  echo "replayed $(grep -c . "$list" || true) domain(s)"
  ;;
list)
  echo "server address: $server_ip"
  cat "$list"
  ;;
esac
EOF
