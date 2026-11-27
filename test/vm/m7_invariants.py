"""M7 invariants of a dermaga host (run on the VM by seed.sh; negative control: m7_invariants_control.sh).

Exactly one live release per deployed app, no verify router left in a route file, a running container
only for the live release, the live image present. Exit 1 when one is broken.

  python3 m7_invariants.py [--db PATH] [--dynamic-dir DIR] [--running FILE] [--images FILE]

--running/--images name files with one container/image per line instead of asking podman.
"""
import argparse
import glob
import os
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
print("PASS: M7 invariants hold" if bad == 0 else f"FAIL: {bad} invariant(s) broken")
sys.exit(1 if bad else 0)
