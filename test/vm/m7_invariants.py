"""M7 and M8 invariants of a dermaga host (run on the VM by seed.sh; negative control: m7_invariants_control.sh).

Exactly one live release per deployed app, no verify router left in a route file, a running container
only for the live release, the live image present; since M8 (migration 0006), a live app's route file
has a router for exactly the domains past the DNS gate whose certificate order has not failed
(DEC-38). Exit 1 when one is broken.

  python3 m7_invariants.py [--db PATH] [--dynamic-dir DIR] [--running FILE] [--images FILE]

--running/--images name files with one container/image per line instead of asking podman.
"""
import argparse
import glob
import json
import os
import re
import sqlite3
import subprocess
import sys

home = os.path.expanduser("~/.local/share/dermaga")
ap = argparse.ArgumentParser()
ap.add_argument("--db", default=os.path.join(home, "dermaga.db"))
ap.add_argument("--dynamic-dir", default=os.path.join(home, "traefik/dynamic"))
ap.add_argument("--running")
ap.add_argument("--images")
args = ap.parse_args()


def listing(path, podman_args):
    if path:
        return set(open(path).read().split())
    return set(subprocess.run(["podman", *podman_args], capture_output=True, text=True).stdout.split())


running = listing(args.running, ["ps", "--format", "{{.Names}}"])
images = listing(args.images, ["images", "--format", "{{.Repository}}:{{.Tag}}"])
c = sqlite3.connect(args.db)
bad = 0
for app_id, name, live in c.execute("SELECT id, name, active_release_id FROM apps ORDER BY name"):
    domain = c.execute("SELECT domain FROM domains WHERE app_id=? AND is_primary=1", (app_id,)).fetchone()
    rels = c.execute(
        "SELECT id, release_number, status, image_ref FROM deployments WHERE app_id=? ORDER BY release_number DESC",
        (app_id,),
    ).fetchall()
    print(f"App {name} ({domain[0] if domain else '-'}): " + " ".join(f"r{n}:{s}" for _, n, s, _ in rels))
    active = [r for r in rels if r[2] == "active"]
    if live and (len(active) != 1 or active[0][0] != live):
        print(f"  FAIL: {len(active)} active releases, live={live}")
        bad += 1
    for _, n, s, img in rels:
        cont = f"app-{name}-r{n}"
        if (cont in running) != (s == "active"):
            print(f"  FAIL: r{n} ({s}) container running={cont in running}")
            bad += 1
        if s == "active" and img and img not in images:
            print(f"  FAIL: live image {img} missing")
            bad += 1
    retained = [n for _, n, s, img in rels if s in ("inactive", "rolled_back") and img in images]
    print(f"  live: r{active[0][1] if active else '-'}; releases with an image for rollback: {['r%d' % n for n in retained]}")
for f in sorted(glob.glob(os.path.join(args.dynamic_dir, "app-*.yml"))):
    if "-verify" in open(f).read():
        print(f"FAIL: verify router left in {os.path.basename(f)}")
        bad += 1
if "dns_verified_at" in {r[1] for r in c.execute("PRAGMA table_info(domains)")}:
    for app_id, name, live in c.execute("SELECT id, name, active_release_id FROM apps WHERE active_release_id IS NOT NULL"):
        path = os.path.join(args.dynamic_dir, f"app-{name}.yml")
        if not os.path.exists(path):
            print(f"FAIL: {name} is live but has no route file")
            bad += 1
            continue
        body = "".join(l for l in open(path) if not l.startswith("#"))
        routers = json.loads(body).get("http", {}).get("routers", {})
        hosts = set()
        for rname, r in routers.items():
            if not rname.endswith("-verify"):
                hosts.update(re.findall(r"Host\(`([^`]+)`\)", r.get("rule", "")))
        rows = c.execute("SELECT domain, dns_verified_at, cert_failed_at FROM domains WHERE app_id=? ORDER BY domain", (app_id,)).fetchall()
        for domain, verified, failed in rows:
            routed = verified is not None and failed is None
            if routed != (domain in hosts):
                print(f"  FAIL: {domain} routed={routed} but router in app-{name}.yml={domain in hosts}")
                bad += 1
        print(f"  routes of {name}: {sorted(hosts)}; not routed: {sorted(d for d, v, f in rows if v is None or f is not None)}")
print("PASS: M7 invariants hold" if bad == 0 else f"FAIL: {bad} invariant(s) broken")
sys.exit(1 if bad else 0)
