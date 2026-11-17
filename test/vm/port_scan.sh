#!/usr/bin/env bash
# REL-3 / M3 scenario e: scans every TCP port of HOST from the laptop, again and again, until
# STOP_FILE exists (or once with --once). Fails if a port outside ALLOWED is ever open.
#
#   test/vm/port_scan.sh HOST ALLOWED STOP_FILE      e.g. 192.168.122.217 22,80,443,4222 /tmp/stop
#   test/vm/port_scan.sh --once HOST ALLOWED
#
# UDP is not scanned (an open UDP port does not answer a probe); 443/udp is checked from the
# firewall rules instead.
set -Eeuo pipefail

once=no
if [[ ${1-} == --once ]]; then
  once=yes
  shift
fi
host=${1:?host}
allowed=${2:?allowed ports, comma separated}
stop=${3:-}
[[ $once == yes || -n $stop ]] || {
  echo "usage: $0 HOST ALLOWED STOP_FILE | --once HOST ALLOWED" >&2
  exit 2
}

echo "date: $(date -Is)"
echo "hostname: $(hostname)"
echo "commit: $(git rev-parse --short HEAD)"
echo "scanning $host, allowed: $allowed"

# One sweep of all 65535 TCP ports with 1000 connects in flight and a 1 s timeout each.
sweep() {
  python3 -I - "$host" <<'EOF'
import asyncio, sys

host = sys.argv[1]

async def probe(port, sem, found):
    async with sem:
        try:
            _, w = await asyncio.wait_for(asyncio.open_connection(host, port), 1.0)
            found.append(port)
            w.close()
        except Exception:
            pass

async def main():
    sem, found = asyncio.Semaphore(1000), []
    await asyncio.gather(*(probe(p, sem, found) for p in range(1, 65536)))
    print(" ".join(map(str, sorted(found))))

asyncio.run(main())
EOF
}

bad=0
round=0
while :; do
  round=$((round + 1))
  open=$(sweep)
  unexpected=""
  for p in $open; do
    [[ ",$allowed," == *",$p,"* ]] || unexpected+="$p "
  done
  echo "$(date -Is) round $round open: ${open:-none}${unexpected:+  UNEXPECTED: $unexpected}"
  [[ -z $unexpected ]] || bad=1
  if [[ $once == yes ]] || [[ -e $stop ]]; then break; fi
done

if ((bad)); then
  echo "FAIL: a port outside $allowed was open"
  exit 1
fi
echo "PASS: only $allowed were ever open ($round rounds)"
