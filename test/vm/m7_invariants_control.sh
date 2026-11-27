#!/usr/bin/env bash
# Negative control for test/vm/m7_invariants.py (the seed.sh invariant check), run locally: a correct
# state must PASS and each broken state must FAIL with its own message.
set -uo pipefail
echo "date: $(date -Is)"
echo "hostname: $(hostname)"
echo "commit: $(git rev-parse --short HEAD)"
here=$(cd "$(dirname "$0")" && pwd)
work=$(mktemp -d); trap 'rm -rf "$work"' EXIT
failures=0

# state <name>: writes $work/<name>/{db,dynamic/,running,images} for app shop (r1 previous, r2 live).
state() {
  local d=$work/$1; mkdir -p "$d/dynamic"
  python3 -I - "$d/db" <<'PY'
import sqlite3, sys
c = sqlite3.connect(sys.argv[1])
c.executescript("""
CREATE TABLE apps (id TEXT, name TEXT, active_release_id TEXT);
CREATE TABLE domains (app_id TEXT, domain TEXT, is_primary INTEGER, dns_verified_at TEXT, cert_failed_at TEXT);
CREATE TABLE deployments (id TEXT, app_id TEXT, release_number INTEGER, status TEXT, image_ref TEXT);
INSERT INTO apps VALUES ('a1', 'shop', 'd2');
INSERT INTO domains VALUES ('a1', 'shop.example.com', 1, '2026-11-27T00:00:00Z', NULL);
INSERT INTO domains VALUES ('a1', 'wait.example.com', 0, NULL, NULL);
INSERT INTO deployments VALUES ('d1', 'a1', 1, 'inactive', 'localhost/dermaga/shop:r1');
INSERT INTO deployments VALUES ('d2', 'a1', 2, 'active', 'localhost/dermaga/shop:r2');
""")
c.commit()
PY
  # shellcheck disable=SC2016 # literal backticks: Traefik rule syntax, nothing to expand
  echo '{"http":{"routers":{"app-shop-host-0":{"rule":"Host(`shop.example.com`)"}}}}' > "$d/dynamic/app-shop.yml"
  printf 'app-shop-r2\ntraefik\n' > "$d/running"
  printf 'localhost/dermaga/shop:r1\nlocalhost/dermaga/shop:r2\n' > "$d/images"
}

run() { python3 -I "$here/m7_invariants.py" --db "$work/$1/db" --dynamic-dir "$work/$1/dynamic" --running "$work/$1/running" --images "$work/$1/images"; }

expect() { # expect <name> <exit> <text>
  local out rc; out=$(run "$1" 2>&1); rc=$?
  if [ "$rc" = "$2" ] && grep -qF "$3" <<<"$out"; then echo "PASS  $1: exit $rc, \"$3\""
  else echo "FAIL  $1: exit $rc (want $2), output:"; printf '%s\n' "$out"; failures=$((failures + 1)); fi
}

state good;          expect good 0 "PASS: M7 invariants hold"
state two-live;      python3 -I -c "import sqlite3,sys; c=sqlite3.connect(sys.argv[1]); c.execute(\"UPDATE deployments SET status='active' WHERE id='d1'\"); c.commit()" "$work/two-live/db"
                     expect two-live 1 "FAIL: 2 active releases"
state verify-left
# shellcheck disable=SC2016 # literal backticks: Traefik rule syntax, nothing to expand
echo '{"http":{"routers":{"app-shop-host-0":{"rule":"Host(`shop.example.com`)"},"app-shop-verify":{}}}}' > "$work/verify-left/dynamic/app-shop.yml"
                     expect verify-left 1 "FAIL: verify router left in app-shop.yml"
state old-running;   echo app-shop-r1 >> "$work/old-running/running"
                     expect old-running 1 "FAIL: r1 (inactive) container running=True"
state image-missing; echo localhost/dermaga/shop:r1 > "$work/image-missing/images"
                     expect image-missing 1 "FAIL: live image localhost/dermaga/shop:r2 missing"

state pending-routed
# shellcheck disable=SC2016 # literal backticks: Traefik rule syntax, nothing to expand
echo '{"http":{"routers":{"app-shop-host-0":{"rule":"Host(`shop.example.com`)"},"app-shop-host-1":{"rule":"Host(`wait.example.com`)"}}}}' > "$work/pending-routed/dynamic/app-shop.yml"
                     expect pending-routed 1 "FAIL: wait.example.com routed=False but router in app-shop.yml=True"
state routed-missing; echo '{"http":{"routers":{}}}' > "$work/routed-missing/dynamic/app-shop.yml"
                     expect routed-missing 1 "FAIL: shop.example.com routed=True but router in app-shop.yml=False"

[ "$failures" = 0 ] && echo "all checks passed"
exit "$failures"
