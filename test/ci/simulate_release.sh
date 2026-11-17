#!/usr/bin/env bash
# Runs a release workflow's steps locally, as GitHub runs them: as root, with `sh -e`, in the same
# job container image, in a workspace owned by uid 1001 (the runner's user). Checkout is a clone;
# `gh` is a stub that only records what it would publish.
#
#   test/ci/simulate_release.sh WORKFLOW REF TAG
#     WORKFLOW  path to a workflow file (e.g. .github/workflows/release.yml)
#     REF       a commit of a git repository to check out (e.g. HEAD), REPO=path picks the repo
#     TAG       the tag to simulate; created in the throwaway clone only
set -Eeuo pipefail

workflow=${1:?workflow file}
ref=${2:?commit}
tag=${3:?tag}
repo=${REPO:-$(git rev-parse --show-toplevel)}

echo "date: $(date -Is)"
echo "hostname: $(hostname)"
echo "commit: $(git rev-parse --short HEAD)"
echo "simulating $workflow at $(git -C "$repo" rev-parse --short "$ref") as tag $tag"

sim=$(mktemp -d "${TMPDIR:-/tmp}/release-sim-XXXXXX")
trap 'podman unshare rm -rf "$sim"' EXIT

git clone --quiet --no-local "$repo" "$sim/checkout"
git -C "$sim/checkout" checkout --quiet --detach "$ref"
git -C "$sim/checkout" tag -f "$tag" >/dev/null # the clone may already carry this tag

image=$(python3 -I -c 'import sys, yaml; w = yaml.safe_load(open(sys.argv[1])); print(next(iter(w["jobs"].values()))["container"]["image"])' "$workflow")

# One script per step; ${{ … }} expressions are replaced with the values GitHub would use.
python3 -I - "$workflow" "$sim" "$tag" <<'EOF'
import sys, yaml, shlex
wf, sim, tag = sys.argv[1:4]
job = next(iter(yaml.safe_load(open(wf))["jobs"].values()))
values = {"${{ github.ref_name }}": tag, "${{ secrets.GITHUB_TOKEN }}": "simulated-token",
          "${{ github.repository }}": "r3p-dev/dermaga"}
def sub(text):
    for k, v in values.items():
        text = text.replace(k, v)
    return text
names = []
for i, step in enumerate(job["steps"]):
    with open(f"{sim}/step{i:02d}.sh", "w") as f:
        if "uses" in step:
            assert step["uses"].startswith("actions/checkout@"), step["uses"]
            f.write('cp -a /sim/checkout/. "$GITHUB_WORKSPACE"/\nchown -R 1001:1001 "$GITHUB_WORKSPACE"\n')
        else:
            for k, v in (step.get("env") or {}).items():
                f.write(f"export {k}={shlex.quote(sub(str(v)))}\n")
            f.write(sub(step["run"]))
    names.append(f"step{i:02d}.sh\t{step.get('name', step.get('uses'))}")
open(f"{sim}/steps.txt", "w").write("\n".join(names) + "\n")
EOF

cat >"$sim/gh" <<'EOF'
#!/bin/sh
echo "SIMULATED (not published): gh $*"
for f in "$@"; do case $f in dist/*) ls -l "$f" || exit 1 ;; esac; done
EOF
chmod 755 "$sim/gh"

cat >"$sim/driver.sh" <<'EOF'
set -u
export GITHUB_WORKSPACE=/__w/dermaga/dermaga GITHUB_PATH=/sim/github_path GITHUB_REPOSITORY=r3p-dev/dermaga
mkdir -p "$GITHUB_WORKSPACE" && chown 1001:1001 "$GITHUB_WORKSPACE" && touch "$GITHUB_PATH"
install -m 755 /sim/gh /usr/local/bin/gh
while IFS="$(printf '\t')" read -r script name; do
  echo "::: step: $name"
  extra=$(tac "$GITHUB_PATH" | paste -sd: -)
  if ! (cd "$GITHUB_WORKSPACE" && PATH="${extra:+$extra:}$PATH" sh -e "/sim/$script"); then
    echo "::: step failed: $name"
    exit 1
  fi
done </sim/steps.txt
echo "::: all steps passed"
EOF

podman run --rm -v "$sim:/sim:Z" "$image" bash /sim/driver.sh
