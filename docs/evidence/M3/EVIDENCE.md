# Evidence: Milestone M3 (0.3.0) — Installer, Hardening, Lockout Guard

M3 is split into two checkpoints (`docs/PLAN.md`):

- **M3a** (this file, so far): installer and hardening code with unit and fake-host tests and local
  checks only. Nothing ran as root on any machine; the only contact with `dermaga-vm` was read-only
  (user `server`, no sudo) to copy two fixtures.
- **M3b** (not run yet): scenarios a–h on `dermaga-vm` from the `fresh` snapshot with the `v0.3.0-rc`
  pre-release.

Every log below starts with its command, commit and timestamp. Logs marked dirty ran on the M3a
working tree on top of `a88fe9f`; the per-commit `make check` logs (§8) ran on clean throwaway
worktrees.

---

## 1. Status per acceptance criterion

`PASS` here means exercised by an automated test or a local run with the output in a log. Nothing
in M3a is a VM result; every VM item is **NOT RUN** until M3b.

| Criterion | M3a (code and tests) | M3b (VM) |
| --- | --- | --- |
| INST-1 README "Before you start" | done (README §1–2) | — |
| INST-2 Debian 13 only, idempotent | PASS: `TestInstallRefusesBeforeChanging/not_debian_13`, re-run no-op in `TestInstallFreshServer` | NOT RUN (g) |
| INST-3 terminal prompts, defaults, no tty → stop | PASS: `TestInstallInteractiveEnterOnly`, `.../no_terminal` | NOT RUN (a) |
| INST-4 `--config` + `--yes` | PASS: `TestInstallFreshServer` | — |
| INST-5 key choice, current key marked, guided paste | PASS: `TestInstallInteractiveEnterOnly`, `TestInstallNoKeyGuidedPaste` | NOT RUN (a, b) |
| INST-6 port, address without third parties, stored | PASS: `TestInstallInteractiveEnterOnly`, `TestInstallPublicAddressNotAsked`, `TestCLIInstallSettingsFeedsLaptopCommand` | NOT RUN (a) |
| INST-7 plan before changes | PASS: `.../plan_declined` | NOT RUN |
| INST-8 :80/:443 busy → refuse | PASS: `.../port_80_busy` | NOT RUN |
| INST-9, SEC-22 checksum-verified binary | PASS: `TestInstallRefusesUnverifiedBinary` (mismatch, missing) | NOT RUN |
| INST-10 GitHub SSH host keys pinned | removed from v1.0.0 (owner decision, DEC-23); code, fixture and tests deleted | — |
| INST-11 user service, linger, doctor | PASS: `internal/doctor` tests, reconciler Traefik tests | NOT RUN |
| INST-12 laptop command, link, "Lost this?" | PASS (link: see §9 question 2) | NOT RUN |
| HOST-1 one login user `server` | PASS: `AllowUsers server` in `TestSSHDFinalWithRealSSHD` | NOT RUN |
| HOST-2 sshd hardening | PASS: `TestSSHDFinalWithRealSSHD` (real `sshd -T`) | NOT RUN |
| HOST-3 nftables default deny, no restart | PASS: rendered rules in `TestInstallFreshServer`, `TestInstallTimeoutKeepsOldAccess` | NOT RUN (e) |
| HOST-4 fail2ban sshd + recidive | PASS: jail in `TestInstallFreshServer` | NOT RUN |
| HOST-5 sysctl, upgrades, zram, clock | code done; files asserted in `TestInstallFreshServer` | NOT RUN |
| HOST-6 rootless Podman, Traefik socket activation, file provider only | PASS: `TestTraefikGolden`, `TestReconcile_Traefik*`, local Quadlet dry run | NOT RUN (h) |
| HOST-7 sudo password recommended | code done (see §9 question 1) | NOT RUN |
| LOCK-1 new port next to old, validated | PASS: `TestSSHDOpenPhaseWithRealSSHD`, rollback tests | NOT RUN (c) |
| LOCK-2 instruction text | PASS: `TestInstallFreshServer` | NOT RUN |
| LOCK-3 wait, detect, `yes` fallback | PASS: `TestLoginSeenOnRealJournal`, `TestInstallInteractiveEnterOnly` | NOT RUN |
| LOCK-4 close only after proof | PASS: `TestInstallFreshServer` (2 reloads), `TestInstallTimeoutKeepsOldAccess` | NOT RUN |
| LOCK-5 timeout/Ctrl+C/closed window keep access | PASS: `TestInstallTimeoutKeepsOldAccess` (timeout, resume) | NOT RUN (c, d) |
| LAP-1 laptop command | PASS: bash, fish, sh (bash) in `internal/access`; zsh and dash in a container | NOT RUN (f) |
| LAP-2 shell + tunnel, second session warns | block content PASS (`ssh -G`) | NOT RUN |

## 2. `make check`

[`logs/make_check.log`](logs/make_check.log) (dirty working tree):

```text
=== make check passed successfully ===
exit=0
```

## 3. Negative controls: the tests can fail

[`logs/mutation_negative_controls.log`](logs/mutation_negative_controls.log): each guarded rule was
broken in a copy of the tree and the test that guards it was run.

```text
=== mutant sshd-no-restore: install/install.sh: sed 's/^    restore$/    :/'
exit=1
NEGATIVE CONTROL PASSED: the test caught the broken rule
=== mutant login-any-port: install/install.sh: sed 's/ \&\& \$9 == port / /'
exit=1
NEGATIVE CONTROL PASSED: the test caught the broken rule
=== mutant close-without-proof: install/install.sh: sed 's/^    wait_for_new_login "\$since"$/    :/'
exit=1
NEGATIVE CONTROL PASSED: the test caught the broken rule
=== mutant lap1-not-idempotent: internal/access/access.go: sed 's/if grep -qF "\$m " "\$c"; then/if false; then/'
exit=1
NEGATIVE CONTROL PASSED: the test caught the broken rule
=== mutant foreign-traefik-owned: internal/reconciler/reconciler.go: sed 's/case readErr == nil \&\& !f.owned(string(current)):/case false:/'
exit=1
NEGATIVE CONTROL PASSED: the test caught the broken rule
=== mutant doctor-ignores-clock: internal/doctor/doctor.go: sed 's/== "yes" {/!= "nope" {/'
exit=1
NEGATIVE CONTROL PASSED: the test caught the broken rule
```

The lockout guard's own rollback is a negative control in the test suite:
`TestInstallRejectedSSHDConfigRolledBack` (sshd rejects the config → file restored, sshd never
reloaded) and `TestInstallSSHDNotListeningRolledBack` (sshd does not listen on the new port after
the reload → file restored, sshd reloaded again).

The port scanner's negative control, run in an empty network namespace on the dev machine
([`logs/port_scan_negative_control_local.log`](logs/port_scan_negative_control_local.log)):

```text
2026-11-17T08:41:04+07:00 round 1 open: 17070 49370 59448  UNEXPECTED: 17070 49370 59448 
FAIL: a port outside 7070 was open
exit=1
```
```text
2026-11-17T08:41:12+07:00 round 1 open: 17070
PASS: only 7070,17070 were ever open (1 rounds)
exit=0
```

49370 and 59448 in the first round are TCP self-connections: a loopback connect from an ephemeral
port to the same port number succeeds. They cannot happen when scanning another host.

## 4. Pinned values from queries

Traefik ([`logs/traefik_image_query.log`](logs/traefik_image_query.log)):

```text
$ HEAD /v2/library/traefik/manifests/v3.7.19 (Accept: OCI index, Docker manifest list)
content-type: application/vnd.oci.image.index.v1+json
docker-content-digest: sha256:aa942a00bce033c909bf1f7e861d93761be4b31a035f10d2410d33e38a8f41cf
```
```text
linux/amd64 sha256:44b61e76a251fe586d17d881fc900452d0a7b4a4b08c97ce6eac86d45b4c8b9d
```

The M2 VM test pinned `44b61e…`, the linux/amd64 manifest; dermaga now pins the index (DEC-20).

Traefik ACME e-mail and `acme.json` writes
([`logs/traefik_acme_source_check.log`](logs/traefik_acme_source_check.log)):

```text
46:	Email                string   `description:"Email address used for registration." json:"email,omitempty" toml:"email,omitempty" yaml:"email,omitempty"`
```
```text
172:				if err := os.WriteFile(s.filename, data, 0o600); err != nil {
```

Debian trixie packages used by the installer and CI:
[`logs/debian_packages_query.log`](logs/debian_packages_query.log), e.g.

```text
gh                        gh | 2.46.0-3 | trixie | source, amd64, arm64, armel, armhf, i386, ppc64el, riscv64, s390x
```

## 5. Fixtures copied from `dermaga-vm` (read-only)

[`logs/vm_fixture_capture.log`](logs/vm_fixture_capture.log): Debian 13's `/etc/ssh/sshd_config`
(for the real `sshd -T` tests) and sshd log lines with `LogLevel VERBOSE` (for the login detection):

```text
Connection from 192.168.122.1 port 37160 on 192.168.122.217 port 4222 rdomain ""
```
```text
Accepted publickey for server from 192.168.122.1 port 37160 ssh2: ED25519 SHA256:V+KZKBpZYT+xiu/DXAsTjf2LTerWPN+DYUlih/Z1U9A
```


## 6. Local checks outside `go test`

Quadlet dry run of the Traefik unit with the dev machine's Podman 5.8.9 (early check only; Debian's
5.4.2 in M3b is authoritative) ([`logs/quadlet_dryrun_local.log`](logs/quadlet_dryrun_local.log)):

```text
ExecStart=/usr/bin/podman run --name traefik --replace --rm --cgroups=split --pids-limit 256 --network web --sdnotify=container -d --security-opt=no-new-privileges --cap-drop all --read-only -v /home/server/.local/share/dermaga/traefik:/etc/traefik -v /home/server/.local/share/dermaga/traefik/dynamic:/etc/traefik/dynamic:ro --memory=256m docker.io/library/traefik:v3.7.19@sha256:aa942a00bce033c909bf1f7e861d93761be4b31a035f10d2410d33e38a8f41cf
```
```text
quadlet-generator[780807]: converting "traefik.container": unsupported key 'NotAQuadletKey' in group 'Container' in /tmp/claude-1000/-home-r3p-Projects-containers/ddf38bc0-5ba1-4108-8b00-9c22e59b4d51/scratchpad/quadlet-TOxB/traefik.container
quadlet-generator[780807]: processing encountered some errors
exit=1
```

LAP-1 in zsh 5.9 and dash (Debian's `/bin/sh`), pasted twice into a config that already has an
unmarked `Host dermaga` ([`logs/lap1_dash_zsh_container.log`](logs/lap1_dash_zsh_container.log)):

```text
sh is: /usr/bin/dash
zsh 5.9 (x86_64-debian-linux-gnu)
```
```text
Saved. From now on: ssh dermaga-dermaga-vm, then open http://localhost:7070
Already saved. From now on: ssh dermaga-dermaga-vm, then open http://localhost:7070
```
```text
--- blocks: 1, backup: Host dermaga
```

## 7. Deviations and decisions

DEC-18 to DEC-22 in `docs/ARCHITECTURE.md` §7. In short: Traefik files are written by the
reconciler (not the installer); the binary is in `~server/.local/bin`; two-phase sshd guard with
journal detection; the Traefik image is pinned by index digest; the laptop command puts its block
first, followed by `Host *`; no `full-upgrade`, no swapfile fallback, the provider's user is not
deleted.

## 8. Per-commit checks

Each suggested commit was checked with `make check` alone in a throwaway worktree:
`logs/commit1_check.log` … `logs/commit7_check.log` (M3a), `logs/commit8_check.log` onward (§10).

## 9. Known limitations and open items

- Every VM scenario (a–h) is NOT RUN until M3b.
- No first-run link yet: the setup token (`SETUP-1`) arrives in M4, so the summary tells the user
  to set the admin password with `dermaga admin set-password`.
- `doctor` runs the checks that exist so far; DNS, GitHub, TOTP and certificates follow (M4–M9).
  "Traefik's file provider loaded without errors" is not checked yet (M9).
- Without `ACME_EMAIL`, Traefik's account behaviour is verified with Pebble in M4.
- `ssh.socket` setups are refused (Debian 13 uses `ssh.service`).
- UDP 443 is not port-scanned; the rules are checked from `/etc/nftables.conf`.

## 10. Fixes after the M3a review (before M3b)

### 10.1 The `v0.3.0-rc` release failed

[`logs/release_rc1_failure.log`](logs/release_rc1_failure.log) (the Actions run of tag `v0.3.0-rc`):

```text
Build and publish | Run make check | success
Build and publish | Assemble release files | failure
Build and publish | Publish the release | skipped
```
```text
2026-11-17T01:57:54.5302152Z fatal: detected dubious ownership in repository at '/__w/dermaga/dermaga'
```
```text
$ gh release view v0.3.0-rc
release not found
exit=1
```

Cause: "Run make check" gives the workspace to `server` and adds `safe.directory` only to
`server`'s git config; "Assemble release files" then ran `git rev-parse` as root. The fix (DEC-24):
`make release` assembles `dist/` and refuses a dirty build or a tag that differs from `git describe`;
CI runs `make check release` as `server` on every push; the release workflow runs the same command
the same way, and its root-only publish step calls no git.

`make release` refuses what must never be released
([`logs/make_release_negative_controls.log`](logs/make_release_negative_controls.log)):

```text
refusing to assemble a release from version v0.3.0-rc-dirty
make: *** [Makefile:59: release] Error 1
exit=2
```
```text
tag v9.9.9 but the build version is v0.0.1
make: *** [Makefile:60: release] Error 1
exit=2
```

The release job simulated as root in the same image (`test/ci/simulate_release.sh`). Negative
control, the workflow of `c7c4671`
([`logs/release_sim_c7c4671.log`](logs/release_sim_c7c4671.log)):

```text
::: step: Assemble release files
fatal: detected dubious ownership in repository at '/__w/dermaga/dermaga'
	git config --global --add safe.directory /__w/dermaga/dermaga
::: step failed: Assemble release files
exit=1
```

The fixed workflow on the working tree, committed in a throwaway clone and tagged `v0.3.0-rc.2`
there ([`logs/release_sim_fixed.log`](logs/release_sim_fixed.log)):

```text
::: step: Run make check and assemble the release files
=== make check passed successfully ===
```
```text
::: step: Publish the release
SIMULATED (not published): gh release create v0.3.0-rc.2 dist/dermaga-linux-amd64 dist/dermaga-linux-arm64 dist/install.sh dist/checksums.txt --verify-tag --title v0.3.0-rc.2 --notes See CHANGELOG.md. Install: curl -fsSL https://github.com/r3p-dev/dermaga/releases/download/v0.3.0-rc.2/install.sh | sudo bash --prerelease
```
```text
::: all steps passed
exit=0
```

### 10.2 Dependencies

[`logs/dependency_versions_query.log`](logs/dependency_versions_query.log):

```text
golang.org/x/crypto | v0.0.0-20210817164053-32db794688a5 | v0.59.0 | 2026-11-11T07:23:42Z
```
```text
{"peerDependencies":{"vite":"^8.0.12","svelte":"^5.57.1","typescript":"^6.0.0","@opentelemetry/api":"^1.0.0","@sveltejs/vite-plugin-svelte":"^7.0.0"}}
```

[`logs/dependency_update.log`](logs/dependency_update.log):

```text
go: upgraded golang.org/x/crypto v0.0.0-20210817164053-32db794688a5 => v0.59.0
```
```text
typescript 6.0.3 | bun.lock: 6.0.3
```

How the 2021 version got in: the tool `github.com/gzuidhof/tygo` v0.2.21 requires
`github.com/spf13/cobra` v1.3.0, which requires `github.com/spf13/viper` v1.10.0, which requires
`golang.org/x/crypto v0.0.0-20210817164053-32db794688a5`. Tool dependencies share the module graph,
so when `internal/auth` first imported `golang.org/x/crypto/argon2` (commit `ade8963`),
`go mod tidy` kept the version already selected in that graph instead of fetching the latest. The
M1 row in `docs/DEPENDENCIES.md` said v0.59.0 without being checked against `go.mod`.
`gopkg.in/yaml.v2`, used by one M2 test, was shown as `// indirect` for the same reason; `go mod
tidy` now lists it as a direct (test-only) requirement. It is not in the binary
([`logs/binary_modules.log`](logs/binary_modules.log)):

```text
	dep	golang.org/x/crypto	v0.59.0	h1:xS2jhqYbzNrpvkWP2YTDf4TJwBy3ykR03i8vFDxEc7c=
	dep	golang.org/x/term	v0.48.0	h1:G6UU1eiSB8nl3DzeUGt7Mg+2SctI8tPS6Z5/mBt6cOk=
	dep	modernc.org/sqlite	v1.62.0	h1:gMaCjSM1qpl+iOjiWhR1LYP3zqrYDI+5GYLGt6veP2g=
yaml linked: 0
```

### 10.3 INST-10 removed

Removed (owner decision, DEC-23): `pin_github_keys` in `install/install.sh` and its call, `jq` from
the installer's packages and the CI jobs, `install/testdata/github_meta.json`,
`docs/evidence/M3/logs/github_meta_fixture.log`, `TestInstallGitHubKeyMismatch`, the INST-10
assertions and the `meta` download stub in `install/install_test.go`. The installer tests' sample
key was GitHub's ed25519 host key from that fixture; it is now a key generated for the tests.

---

## Process checkpoint (2026-11-17)

<!-- Process-only work during M3 (not part of the M3a acceptance criteria above). Raw logs are
local only, in tmp/evidence/M3/ (DEC-26). -->

Commit under review: `70bf0a2` (dirty: the changes below, plus M3b work in progress that predates
this checkpoint: `test/vm/drive_install.py` and three M3 logs). No application code, UI or installer
behavior changed; M3/M3b scope is untouched. Nothing ran on `dermaga-vm`, nothing ran as root.

This section has two parts: the first checkpoint (below, up to "Known limitations / deferred") and
the follow-up that applied the owner's decisions and moved raw evidence out of the repository
(section "Follow-up"). The first part's logs were moved afterwards from `docs/evidence/M3/logs/` to
`tmp/evidence/M3/logs/` unchanged; their excerpts are as captured. Where the follow-up changed a rule,
the rule mapping shows the current wording.

### Acceptance criteria

| Criterion | Status | Log file | Excerpt (≤20 lines) |
| --- | --- | --- | --- |
| `make check` on the final state, short output | PASS | `tmp/evidence/M3/logs/process-make-check.log` | below |
| First `make check` run: `policy` caught the selftest fixture's unpinned image (fixed in `scripts/selftest`, then the row above) | FAIL | `tmp/evidence/M3/logs/process-make-check-first-run.log` | below |
| Negative control: `make check` fails on a gofmt violation in a temporary worktree and prints the tail | PASS | `tmp/evidence/M3/logs/process-negative-make-check.log` | below |
| Negative controls: `evidence-run -- false` exits non-zero; header fields; exit code passthrough; committed logs never overwritten (guard removed in the follow-up: logs are never committed) | PASS | `tmp/evidence/M3/logs/process-negative-evidence-run.log` | below |
| Negative controls: `check-policy` fails on an image without a digest (temp fixture), an unignored secret, a missing `.gitignore` | PASS | `tmp/evidence/M3/logs/process-negative-check-policy.log` | below |
| Negative controls: `check-commits` fails on a plan missing a path, a subject naming an assistant, a bad subject and an unchanged path | PASS | `tmp/evidence/M3/logs/process-negative-check-commits.log` | below |
| `shellcheck` on every new script (now a CI step, DEC-27; see Follow-up) | NOT RUN | `tmp/evidence/M3/logs/process-shellcheck-version-query.log` (version and digest query) | owner: see below |
| `bash -n` on every new script (substitute for the above) | PASS | `tmp/evidence/M3/logs/process-bash-syntax.log` | below |
| Session-start reading set, before and after | PASS | `tmp/evidence/M3/logs/process-reading-set-size.log` | below |
| Suggested commits checked one by one in a throwaway worktree (superseded: the owner voided these commits; the follow-up's set replaces them) | PASS | `tmp/evidence/M3/logs/process-check-commits.log` | below |
| Rule mapping: every old AGENTS.md rule has a new place | decision recorded | — | table below |
| Claude Code permission syntax and `AGENTS.md` loading checked against current docs | decision recorded | — | "Decisions & deviations" |

#### process-make-check.log

```text
ok   ui
ok   lint
ok   test
ok   vulncheck
ok   build-all
ok   policy
=== make check passed successfully ===
# Exit: 0
```

#### process-make-check-first-run.log

```text
ok   build-all
FAIL policy (full log: .check-logs/policy.log)
scripts/check-policy
check-policy: images not pinned by digest:
  scripts/selftest: docker.io/library/debian:trixie-slim
make[1]: *** [Makefile:53: policy] Error 1
make: *** [Makefile:64: check] Error 1
# Exit: 2
```

#### process-negative-make-check.log

```text
# Command: scripts/selftest make-check 
ok: make check fails at lint and prints the offending file
# Exit: 0
```

The case appends `func  selftestBadFormat( ) {}` to `cmd/dermaga/main.go` in a detached worktree and
asserts exit 2 and the lines `FAIL lint (full log: .check-logs/lint.log)`, `gofmt -s would change:`
and `cmd/dermaga/main.go` (`scripts/selftest`, case `make-check`).

#### process-negative-evidence-run.log

```text
ok: '-- false' exits 1 and prints FAIL
ok: log header has command, timestamp, host, commit, dirty status, exit code
ok: exit code 3 passed through; output goes to the log
ok: '-- true' prints exactly one PASS line, no command output
ok: refuses to overwrite committed docs/evidence/M1/logs/brand_colors_raw.log
# Exit: 0
```

#### process-negative-check-policy.log

```text
ok: an image without a digest fails
ok: the same image with a digest passes
ok: an unignored secret and a missing .gitignore fail
# Exit: 0
```

#### process-negative-check-commits.log

```text
ok: a plan missing .github/workflows/ci.yml fails
ok: a subject naming an assistant fails
ok: a long, non-conventional subject and an unchanged path fail
# Exit: 0
```

#### shellcheck: NOT RUN

`shellcheck` is not installed on the dev machine, and running its container was refused by the
agent's permission layer. The version and digest come from `tmp/evidence/M3/logs/process-shellcheck-version-query.log`:

```text
{"isPrerelease":false,"publishedAt":"2025-08-04T00:27:19Z","tagName":"v0.11.0"}
docker.io/koalaman/shellcheck:v0.11.0@sha256:61862eba1fcf09a484ebcc6feea46f1782532571a34ed51fedf90dd25f925a8d
```

For the owner, from the repository root:

```sh
scripts/evidence-run M3 process-shellcheck -- podman run --rm -v "$PWD/scripts:/mnt:ro,Z" docker.io/koalaman/shellcheck:v0.11.0@sha256:61862eba1fcf09a484ebcc6feea46f1782532571a34ed51fedf90dd25f925a8d /mnt/evidence-run /mnt/check-commits /mnt/check-policy /mnt/selftest
```

#### process-bash-syntax.log

```text
bash -n ok: scripts/evidence-run
bash -n ok: scripts/check-commits
bash -n ok: scripts/check-policy
bash -n ok: scripts/selftest
# Exit: 0
```

#### process-reading-set-size.log

```text
  15191 docs/evidence/M3/EVIDENCE.md
  12552 CHANGELOG.md
before total: 304412 bytes (without CHANGELOG.md: 291860)
HEAD AGENTS.md lines: 245

== after: AGENTS.md + docs/PLAN.md up to the first milestone section (status table, earlier milestones, Active milestone), working tree
   9594 AGENTS.md
   6532 docs/PLAN.md (lines 1-88, before ## M0)
after total: 16126 bytes
AGENTS.md lines: 120
```

"Before" is every document the old §1 told agents to read each session plus every `EVIDENCE.md`
(§2.1), at HEAD. "After" is the always-read tier of the new "Session start"; the rest is read when a
milestone needs it.

#### process-check-commits.log

```text
PASS static checks: 6 commits, 33 paths
PASS commit 1: build: quiet make check and add a repository policy check (log: .check-logs/commits/commit1.log)
PASS commit 2: build: add evidence-run, check-commits and selftest scripts (log: .check-logs/commits/commit2.log)
PASS commit 3: docs(agents): slim AGENTS.md and move rules to their documents (log: .check-logs/commits/commit3.log)
PASS commit 4: docs: add the active milestone, code map and DEC-25 (log: .check-logs/commits/commit4.log)
PASS commit 5: docs(m3): record the process checkpoint evidence (log: .check-logs/commits/commit5.log)
PASS commit 6: test(vm): add the M3b installer driver and rc.2 release logs (log: .check-logs/commits/commit6.log)
# Exit: 0
```

This excerpt was pasted into PROCESS.md (now this section) after the run, a documentation-only
edit. The fish commands from `--print-fish` parsed with `fish --no-execute`. The owner voided these
six commits before committing; the follow-up's set covers all uncommitted work.

### Rule mapping

Every rule of the old `AGENTS.md` (at `70bf0a2`), item by item, and where it lives now. Section
names refer to the new `AGENTS.md`. No rule was dropped; where the mechanism changed, the row says so.

| Old rule | Now |
| --- | --- |
| Intro: what dermaga is, v1.0.0 scope | AGENTS intro |
| Intro: no application code until the active milestone is in PLAN | AGENTS intro |
| Intro: code easy to read and explain; unexplainable = not done | AGENTS intro |
| §1 table: documents, what each defines, who writes it | AGENTS "Session start" (tiers + who writes); ARCHITECTURE §1 vs §2+ split kept |
| §1 "read these at the start of every session" | AGENTS "Session start": replaced by tiers (always / when needed / not routinely), as asked |
| §1 precedence: PRD behavior, ARCHITECTURE §1 technology and security, CONVENTIONS style; report conflicts | AGENTS "Session start" |
| §1 visual design and open questions: ask the owner | AGENTS "Session start"; also CONVENTIONS §5.1 (existing) |
| §1 current versions and docs, no deprecated APIs, images pinned by tag and digest | AGENTS "Session start"; CONVENTIONS §11 (existing); `scripts/check-policy` (digests, no exceptions since the follow-up: the test images are pinned) |
| §2.1 align: read PLAN and every EVIDENCE.md; no confirmation → stop | AGENTS "Milestone loop" 1 (PLAN status table and Confirmed column; evidence no longer read) |
| §2.2 test kinds, `make check` without deprecation warnings | AGENTS "Milestone loop" 3; CONVENTIONS §7 (existing) |
| §2.3 VM checks with a binary from the commit under review | AGENTS "Milestone loop" 4; AGENTS "Evidence" (VM binaries) |
| §2.4 docs current: ARCHITECTURE §2+, workaround comment + decisions log, DEPENDENCIES, SETUP, PLAN status | AGENTS "Milestone loop" 5 |
| §2.5 demo state `test/vm/seed.sh` | AGENTS "Milestone loop" 5 |
| §2.6 EVIDENCE.md with the report content, screenshots and logs in the folder, limitations | AGENTS "Evidence" and "Report"; `docs/evidence/TEMPLATE.md`. Changed (DEC-26): the folder holds only `EVIDENCE.md`; logs and screenshots stay in `tmp/evidence/M<N>/` |
| §2.7 report and stop, wait for confirmation | AGENTS "Milestone loop" 6 |
| §3 slow reviewable progress, one sitting | AGENTS "Milestone loop" (paragraph) |
| §3 stop after every milestone | AGENTS "Milestone loop" (paragraph) |
| §3 checkpoints `M5a`, `M5b` with a stop each | AGENTS "Milestone loop" (paragraph); new: handoff in PLAN at each checkpoint |
| §4 report in Indonesian, short | AGENTS "Report" |
| §4.1 Ringkasan with requirement IDs | AGENTS "Report" (chat) |
| §4.2 Cara menjalankan dan melihat | TEMPLATE "Run & view" (moved out of chat) |
| §4.3 Tampilan, desktop and narrow | TEMPLATE "Screens" (local `tmp/evidence/M<N>/screenshots/`, one line each: page, width, theme, what it shows); AGENTS "Report" |
| §4.4 Cara kerja with a sequence | TEMPLATE "How it works" |
| §4.5 Tur kode in reading order | TEMPLATE "Code tour" |
| §4.6 Keputusan dan penyimpangan, every workaround | TEMPLATE "Decisions & deviations" |
| §4.7 Bukti: exact commands and raw output | TEMPLATE criteria table and excerpts from the local log, path cited; `scripts/evidence-run` |
| §4.8 open questions, suggested commits, next milestone | AGENTS "Report" (chat) |
| §5 ask before hard-to-reverse decisions; otherwise decide and log | AGENTS "Milestone loop" (paragraph) |
| §5 never claim without running; NOT RUN with commands; raw output from the commit under review | AGENTS "Evidence" |
| §5 every step that needs the owner stops and asks; never invent a value | AGENTS "Never" (last item) |
| §5 UI shows only what the server returned; API-only is not done | AGENTS "Evidence" (real state); CONVENTIONS §5 `UI-2` (existing) |
| §5 no real outside services from tests; Pebble; real GitHub App only when the milestone says; staging/production certificates only on the real VPS | AGENTS "Never"; AGENTS "Test environment" |
| §5 no production systems (`server`) | AGENTS "Never" |
| §5 test repositories only | AGENTS "Never" |
| §6 SemVer from the tag, `dermaga version`, CHANGELOG (Keep a Changelog) | AGENTS "Versioning"; AGENTS "Git" (CHANGELOG) |
| §6 before 1.0.0: minor per milestone, patch after review | AGENTS "Versioning" |
| §6 1.0.0 definition | AGENTS "Versioning" |
| §6 after 1.0.0: minor, patch, major; "breaking" defined | AGENTS "Versioning" |
| §6 you suggest bump and tag command; owner tags | AGENTS "Versioning" and "Git" |
| §7 no commit, push, tag, rebase, reset, stash, branches, `git add`; read-only git fine | AGENTS "Never"; `.claude/settings.json` deny rules (`git tag` and `git stash` stay denied: owner decision); AGENTS "Git": read tags with `git for-each-ref refs/tags` or `git describe --tags` |
| §7 suggested commits: ordered, small, each passing `make check`, Conventional Commits, ≤72, why-body, paths | AGENTS "Git"; `scripts/check-commits` |
| §7 fish form, no heredocs | AGENTS "Git"; `scripts/check-commits --print-fish` |
| §7 last commit version bump + tag command | AGENTS "Git" |
| §7 every path in exactly one commit; check `git status` | AGENTS "Git"; `scripts/check-commits` |
| §7 no AI attribution | AGENTS "Never"; `scripts/check-commits` |
| §7 no secrets or build output; `.gitignore` current and mentioned | AGENTS "Never" and "Git"; `scripts/check-commits`, `scripts/check-policy` (also raw files under `docs/evidence/`) |
| §8 `dermaga-vm`: user, port; `server` is production | AGENTS "Test environment" and "Never" |
| §8 `dermaga-vm` has passwordless sudo (fact) | `docs/SETUP.md` §2.1 |
| §8 sudo only with explicit approval | AGENTS "Never" |
| §8 snapshots `fresh` / `vps-setup`; allowed, ask-first and never lists | AGENTS "Test environment" (lists unchanged) |
| §8 NAT, replay signed deliveries, forwarding method confirmed with the owner | AGENTS "Test environment" |
| §8 `test/vm/*.sh` idempotent, no residue, header lines | AGENTS "Test environment" |
| §9 read reference material when needed, never assume | AGENTS "Reference material" |
| §9.1 `vps-setup.sh`: reuse hardening and Traefik, not the deploy flow | AGENTS "Reference material" 1 |
| §9.2 Litepod: inspiration, never `ee/`, attribution, no fork | AGENTS "Reference material" 2; AGENTS "Never" (`ee/`) |
| §9.3 portfolio: discover from the repo | AGENTS "Reference material" 3 |
| §9.4 Kapturae: ask for the URL | AGENTS "Reference material" 3 |
| §10.1 PASS only when exercised, exit 0, output checked; decisions are not PASS | AGENTS "Evidence" (status list) |
| §10.2 control test before negative conclusions | CONVENTIONS §7.1 "Control test" |
| §10.3 blocked: stop and ask, no invasive workarounds | AGENTS "Never" |
| §10.4 sudo rule | duplicate of §8 sudo → AGENTS "Never" |
| §10.5 script hygiene: non-interactive, timeouts, no `sudo` | CONVENTIONS §7.1 "Script hygiene"; `evidence-run` runs with stdin from `/dev/null`; CONVENTIONS §7.1 "shellcheck" (CI gate, DEC-27) |
| §10.6 report lists significant actions, workarounds, root changes | TEMPLATE "Decisions & deviations"; AGENTS "Report" |
| §10.7 secrets random, `0600`, gitignored, never printed | CONVENTIONS §4 "Secrets" (added) |
| §10.7 SEC-11 HMAC failure logging | duplicate of CONVENTIONS §4 "Secrets" (already there) |
| §10.8 state tracking, baseline restore | CONVENTIONS §7.1 "State tracking" |
| §10.9 test repos rule | duplicate of §5 → AGENTS "Never" |
| §10.10 evidence location | duplicate of §2.6 → AGENTS "Evidence". Changed (DEC-26): `docs/evidence/M<N>/EVIDENCE.md` only; artifacts in `tmp/evidence/M<N>/`; `docs/evidence/README.md` |
| §10.11 evidence logging (`tee`, header, committed, quote only committed logs, read the log if truncated) | `scripts/evidence-run` (header, `tmp/evidence/M<N>/logs/`); AGENTS "Evidence" (excerpts are exact text from that run's local log, from the commit under review, path cited); CONVENTIONS §7.1 "Tooling". Changed: one log per criterion instead of per command; logs are never committed (DEC-26) |
| §10.12 negative controls | CONVENTIONS §7.1 "Negative controls"; AGENTS "Evidence"; `scripts/selftest` for the tooling |
| §10.13 committed binary for VM checks, never fake `VERSION`, label dirty | AGENTS "Evidence" |
| §10.14 pinned values from logged queries | `docs/DEPENDENCIES.md` rules (each entry: exact query command, exact result line, date; full output in a local log); AGENTS "Evidence"; `scripts/check-policy` (digests) |
| §10.15 real state; follow printed instructions end to end | AGENTS "Evidence" |
| §10.16 open screenshots; contrast on the rendered page | AGENTS "Evidence" (screenshots in `tmp/evidence/M<N>/screenshots/`, each opened and described in one line in EVIDENCE.md before `PASS`); TEMPLATE "Screens" |
| §10.17 per-commit `make check` in a throwaway worktree; one commit per path; fish form | `scripts/check-commits`; AGENTS "Git"; CONVENTIONS §7.1 "Tooling". Changed: plan in `tmp/evidence/M<N>/`, per-commit logs in `.check-logs/commits/` (both gitignored); only its summary lines go into EVIDENCE.md |
| §10.18 CI checked after every owner push | AGENTS "Git" |
| §10.19 test isolation from live service directories | CONVENTIONS §7.1 "Isolation from live services" |

### Run & view

```sh
make check                      # one line per step; full logs in .check-logs/
make check VERBOSE=1            # the old full output (CI uses this)
scripts/evidence-run M3 <name> -- <command…>
scripts/check-commits tmp/evidence/M3/process-commits.plan
scripts/check-commits --print-fish tmp/evidence/M3/process-commits.plan
scripts/selftest evidence-run   # also: check-policy, check-commits, make-check
```

### Screens

None: no panel page changed.

### How it works

- `make check` without `VERBOSE=1` runs each step (`ui`, `lint`, `test`, `vulncheck`, `build-all`,
  `policy`) as a sub-make with output to `.check-logs/<step>.log`; steps after `ui` pass `-o ui` so
  the UI is built once. With `VERBOSE=1`, `check` keeps its old prerequisite form, so CI's
  `make check release` still reuses `build-all`.
- `scripts/evidence-run` writes the header, runs the command with stdin from `/dev/null` and its
  output only into the log, appends `# Exit: N`, and prints one line.
- `scripts/check-commits` parses the plan, matches each `git status --porcelain` path to exactly one
  listed path, checks subjects, attribution and forbidden paths, then copies each commit's files into
  a detached worktree of HEAD cumulatively and runs `make check VERBOSE=1` into one log per commit.
- `scripts/check-policy` scans tracked and unignored files for registry image references and YAML
  `image:` values, checks forbidden paths, and asks `git check-ignore --no-index` about secrets and
  build output.

### Code tour

1. `AGENTS.md`: the new structure, Never first.
2. `docs/PLAN.md`: Confirmed column, earlier milestones, Active milestone and Handoff.
3. `docs/evidence/TEMPLATE.md`: what moved out of the chat report.
4. `docs/CONVENTIONS.md` §4 Secrets and §7.1; `docs/DEPENDENCIES.md` rules; `docs/SETUP.md` §2.1.
5. `docs/ARCHITECTURE.md` §3 code map.
6. `scripts/evidence-run`, then `scripts/check-policy`, `scripts/check-commits`, `scripts/selftest`.
7. `Makefile` (`test`, `lint`, `policy`, `check`), `.github/workflows/*.yml` (`VERBOSE=1`), `.gitignore`.
8. `.claude/settings.json`.

### Decisions & deviations

- **Claude Code docs** (fetched 2026-11-17: `code.claude.com/docs/en/permissions.md`, `memory.md`,
  `settings.md`): the space form `Bash(git commit *)` is the current rule form; deny rules match each
  subcommand of `&&`, `;`, `|` chains and past leading `VAR=value`; forms such as `git -C dir push`
  are not matched, and the docs call argument patterns fragile and "not a security boundary". So the
  deny list is a second layer and `AGENTS.md` stays the rule. `Bash(git tag *)` and
  `Bash(git stash *)` also block the read-only `git tag -l` and `git stash list`; use
  `git for-each-ref refs/tags` instead.
- **No `CLAUDE.md`:** the memory docs say Claude Code reads `AGENTS.md` when no `CLAUDE.md` exists
  (v2.1.277 and later). If an older client is used, a `CLAUDE.md` containing only `@AGENTS.md` is the
  documented import.
- **`check-policy` scope:** images that are pulled (workflows, Go code outside tests, installer, test
  scripts). Not scanned: `*_test.go` and `testdata/` (generator inputs, never pulled), `*.md` and
  `docs/` (prose and history logs), `localhost/` images (local builds). Four existing tag-only
  references in `test/vm/test_reconciliation.sh` and `test/vm/test_traefik_configs.sh` were listed
  as exact exceptions with a `shortcut:` comment. The follow-up pinned them (owner decision) and
  removed the exceptions.
- **`make check` lint message:** `test -z "$(gofmt -s -l .)"` became a check that prints the files
  gofmt would change, so a failure's tail names them.
- **`check-commits` runs `make check VERBOSE=1`** per commit so its one log per commit is complete;
  stdout stays one line per commit.
- **Pre-existing changes:** `docs/PLAN.md` (DEC-25 paragraph), `docs/ARCHITECTURE.md` (DEC-25) and
  `docs/CONVENTIONS.md` (`test/ci/` line) were already modified before this checkpoint; they share
  files with this change. `test/vm/drive_install.py` (M3b work in progress) gets its own commit in
  the follow-up's set; its three logs moved to `tmp/evidence/M3/logs/`.
- **References updated:** `AGENTS.md` §-numbers in PLAN, CONVENTIONS and `test/vm/m1_check.sh`
  (comment only) now name the new sections.
- **Confirmed column:** no written owner confirmation was found for M0, M1, M2 or M3a, so all were
  `?`. Resolved in the follow-up: "confirmed by owner, 2026-11-17".

### Known limitations / deferred

- `shellcheck` NOT RUN here; CI runs it on every push (DEC-27, follow-up).
- `check-policy` finds images by registry host (`docker.io`, `ghcr.io`, `quay.io`, `gcr.io`,
  `registry.k8s.io`, `mcr.microsoft.com`, `public.ecr.aws`) and YAML `image:`; a short name such as
  `alpine:3` after `podman run` is not detected.
- ARCHITECTURE §3 detail still describes packages that do not exist yet (`internal/github`,
  `internal/builder`); the code map lists only real directories.
- Cleanup candidates listed in the report were handled in the follow-up (raw artifacts deleted, DEC-26).

### Follow-up (2026-11-17): raw evidence out of the repository, owner decisions applied

Commit under review: `70bf0a2` (dirty: the first checkpoint plus this follow-up; nothing committed in
between). Logs: `tmp/evidence/M3/logs/process2-*.log` (local only, DEC-26). Nothing ran on
`dermaga-vm`, nothing as root, no state-changing git.

Owner decisions applied:

1. Confirmed column: M0, M1, M2 and M3a "confirmed by owner, 2026-11-17" (supporting records: the
   owner's tags and the start of each next milestone) in `docs/PLAN.md`.
2. `test/vm/drive_install.py` gets its own commit; its three logs moved to `tmp/evidence/M3/logs/`.
3. shellcheck runs in CI (`ci.yml` installs Debian trixie's package and runs
   `make shellcheck SHELLCHECK_REQUIRED=1`); locally `make check` prints
   `SKIP shellcheck (not installed)` (DEC-27).
4. The socat and curl images in `test/vm/test_reconciliation.sh` and `test/vm/test_traefik_configs.sh`
   are pinned by tag and digest; the `shortcut:` exception is gone from `scripts/check-policy`.
5. `git tag` and `git stash` stay denied; AGENTS.md "Git" says to read tags with
   `git for-each-ref refs/tags` or `git describe --tags`.

Evidence decision (DEC-26): `docs/evidence/` keeps only `README.md`, `TEMPLATE.md` and
`M<N>/EVIDENCE.md`. 62 logs and 23 screenshots of M0–M3a were deleted (they stay in git history); the
first checkpoint's logs and plan and the M3b logs moved to `tmp/evidence/M3/`. `scripts/evidence-run`,
`scripts/check-commits`, Playwright (`EVIDENCE_MILESTONE`), `ui/scripts/capture_vm.ts` and
`test/vm/measure_nfr2.sh` write to `tmp/evidence/<milestone>/`; the three VM scripts that excluded
`docs/evidence/M2/logs` from their dirty check now check the whole tree (`tmp/` is ignored).

| Criterion | Status | Log file | Excerpt (≤20 lines) |
| --- | --- | --- | --- |
| Negative controls, `evidence-run` (writes only to `tmp/evidence/`) | PASS | `tmp/evidence/M3/logs/process2-selftest-evidence-run.log` | below |
| Negative controls, `check-policy` (unpinned image, raw files under `docs/evidence/`) | PASS | `tmp/evidence/M3/logs/process2-selftest-check-policy.log` | below |
| Negative controls, `check-commits` (plan adding `docs/evidence/M9/x.log`) | PASS | `tmp/evidence/M3/logs/process2-selftest-check-commits.log` | below |
| Negative control, `make check` (gofmt violation) | PASS | `tmp/evidence/M3/logs/process2-selftest-make-check.log` | below |
| `make check`, short output with a shellcheck line | PASS | `tmp/evidence/M3/logs/process2-make-check.log` | below |
| shellcheck on every shell script | NOT RUN | `tmp/evidence/M3/logs/process2-shellcheck-debian-query.log` | below; CI runs it |
| Registry queries for the pinned test images match the scripts and DEPENDENCIES | PASS | `tmp/evidence/M3/logs/process2-test-image-digests.log`, `process2-pins-match.log` | below |
| `docs/evidence/` holds only `README.md`, `TEMPLATE.md`, `M*/EVIDENCE.md` | PASS | `tmp/evidence/M3/logs/process2-evidence-files.log` | below |
| No reference to a raw artifact under `docs/evidence/` remains, except explained pointers | PASS | `tmp/evidence/M3/logs/process2-evidence-refs.log` | below |
| `scripts/evidence-run M3 smoke -- true` writes only under `tmp/` | PASS | `tmp/evidence/M3/logs/process2-smoke.log` | below |
| Playwright: a plain run writes no screenshot; `EVIDENCE_MILESTONE=M3` writes them to `tmp/evidence/M3/screenshots/` | PASS | `tmp/evidence/M3/logs/process2-playwright.log` | below |
| Tracked bytes under `docs/evidence/`, before and after | PASS | `tmp/evidence/M3/logs/process2-evidence-bytes.log` | below |
| Suggested commits checked one by one in a throwaway worktree | PASS | `tmp/evidence/M3/logs/process2-check-commits.log` | below |
| References in ARCHITECTURE, DEPENDENCIES and Go code rewritten with their essential lines | decision recorded | — | list below |

#### process2-selftest-*.log

```text
ok: '-- false' exits 1 and prints FAIL
ok: log header has command, timestamp, host, commit, dirty status, exit code
ok: exit code 3 passed through; output goes to the log
ok: '-- true' prints exactly one PASS line, no command output
ok: logs land in ignored tmp/evidence/, nothing under docs/
ok: an image without a digest fails
ok: the same image with a digest passes
ok: an unignored secret and a missing .gitignore fail
ok: a screenshot and a stray markdown file under docs/evidence/ fail; EVIDENCE.md passes
ok: a plan missing .github/workflows/ci.yml fails
ok: a subject naming an assistant fails
ok: a long, non-conventional subject and an unchanged path fail
ok: a plan adding docs/evidence/M9/x.log fails
ok: make check fails at lint and prints the offending file
```

Lines 1–5 from `process2-selftest-evidence-run.log`, 6–9 from `-check-policy.log`, 10–13 from
`-check-commits.log`, 14 from `-make-check.log`; each log ends with `# Exit: 0`.

#### process2-make-check.log

```text
ok   ui
ok   lint
ok   test
ok   vulncheck
ok   build-all
ok   policy
SKIP shellcheck (not installed)
=== make check passed successfully ===
# Exit: 0
```

#### shellcheck: NOT RUN

Not installed on the dev machine; running it in a container (the pinned CI image) was refused by the
agent's permission layer. CI is the gate (DEC-27). The package exists in the CI job's distribution:

```text
$ curl -fsS --connect-timeout 10 --max-time 30 "https://qa.debian.org/madison.php?package=shellcheck&s=trixie&text=on"
 shellcheck | 0.10.0-1 | trixie | source, amd64, arm64, armel, armhf, i386, ppc64el, riscv64, s390x
$ command -v shellcheck (this machine)
not installed
```

The four new scripts were reviewed by hand for SC2164 (`cd` without `|| exit`), SC2016 (`$` in single
quotes) and SC2053 (unquoted right-hand side of `==`) and fixed; the older scripts' findings are
unknown until CI runs. For the owner, on a machine with shellcheck: `make shellcheck SHELLCHECK_REQUIRED=1`.

#### process2-test-image-digests.log and process2-pins-match.log

```text
== docker.io/alpine/socat: which version tag latest points to; digest = skopeo inspect --raw docker://IMAGE | skopeo manifest-digest /dev/stdin
latest sha256:71131702f671a88b3808a70b7330f9679cd9b9933adcafadc191508edfe55bba
1.8.1.3 sha256:71131702f671a88b3808a70b7330f9679cd9b9933adcafadc191508edfe55bba
== docker.io/curlimages/curl:8.12.1
8.12.1 sha256:94e9e444bcba979c2ea12e27ae39bee4cd10bc7041a472c4727a558e213744e6
== references in scripts and DEPENDENCIES
test/vm/test_reconciliation.sh:3
test/vm/test_traefik_configs.sh:7
== unpinned socat or curl references left:
none
== shortcut exception in check-policy:
0
```

The first four lines are from `process2-test-image-digests.log`, the rest from `process2-pins-match.log`
(which also shows the curl reference count `1` and the DEPENDENCIES result lines `1`, `1`). The
pinned images are not yet exercised on the VM (M3b).

#### process2-evidence-files.log

```text
docs/evidence/M0/EVIDENCE.md
docs/evidence/M1/EVIDENCE.md
docs/evidence/M2/EVIDENCE.md
docs/evidence/M3/EVIDENCE.md
docs/evidence/README.md
docs/evidence/TEMPLATE.md
$ find docs/evidence -type f ! -name "*.md"
(end)
# Exit: 0
```

#### process2-evidence-refs.log

```text
./docs/evidence/M3/EVIDENCE.md:315:`docs/evidence/M3/logs/github_meta_fixture.log`, `TestInstallGitHubKeyMismatch`, the INST-10
./docs/evidence/M3/EVIDENCE.md:398:ok: refuses to overwrite committed docs/evidence/M1/logs/brand_colors_raw.log
./docs/PLAN.md:55:  `docs/evidence/M3/EVIDENCE.md` (logs and screenshots in `tmp/evidence/M3/`), `docs/SETUP.md` §7, this file; `install/install.sh`
./scripts/check-policy:70:  docs/evidence/M9/x.log docs/evidence/M9/x.png; do
./scripts/selftest:82:    touch "$fixture/docs/evidence/M9/x.png" "$fixture/docs/evidence/M9/NOTES.md" "$fixture/docs/evidence/M9/EVIDENCE.md"
./scripts/selftest:84:    contains "  docs/evidence/M9/x.png"
./scripts/selftest:122:    touch "$fixture/docs/evidence/M9/x.log"
./scripts/selftest:123:    printf 'subject: docs(m9): add a log\npath: scripts/\npath: docs/evidence/M9/x.log\n' >"$tmp/log.plan"
./scripts/selftest:125:    contains "FAIL docs/evidence/M9/x.log: raw evidence is never committed (tmp/evidence/)"
./scripts/selftest:126:    ok "a plan adding docs/evidence/M9/x.log fails"
# Exit: 0
```

Explained pointers: line 315 is the M3a record of a file removed with INST-10 (not a link); line 398
is an exact excerpt of the first checkpoint's log; `PLAN.md:55` matches only because the pattern runs
on to `install.sh`; `scripts/` lines are the `.gitignore` probe and the negative-control fixtures for a
nonexistent `M9`. The M3 line numbers refer to this file before this follow-up section was added.

#### process2-smoke.log

```text
$ scripts/evidence-run M3 smoke -- true
PASS exit=0 log=tmp/evidence/M3/logs/smoke.log
5104 Nov 17 11:30 tmp/evidence/M3/logs/smoke.log
git status of docs/: unchanged
files under docs/ (including ignored): unchanged
smoke log deleted
# Exit: 0
```

#### process2-playwright.log

```text
== plain run: make test-browser
  3 passed (14.7s)
screenshots after the plain run: 0
files under docs/evidence that are not markdown: 0
== evidence run: EVIDENCE_MILESTONE=M3 bunx playwright test (ui/)
  3 passed (14.3s)
screenshots after the evidence run:
tmp/evidence/M3/screenshots/audit-desktop-dark.png
tmp/evidence/M3/screenshots/audit-desktop-light.png
tmp/evidence/M3/screenshots/audit-narrow-dark.png
tmp/evidence/M3/screenshots/audit-narrow-light.png
tmp/evidence/M3/screenshots/dashboard-desktop-dark.png
tmp/evidence/M3/screenshots/dashboard-desktop-light.png
tmp/evidence/M3/screenshots/dashboard-narrow-dark.png
tmp/evidence/M3/screenshots/dashboard-narrow-light.png
tmp/evidence/M3/screenshots/login-desktop-dark.png
tmp/evidence/M3/screenshots/login-desktop-light.png
tmp/evidence/M3/screenshots/login-narrow-dark.png
tmp/evidence/M3/screenshots/login-narrow-light.png
```

Each screenshot was opened (local run on the dev machine, binary `v0.3.0-rc.2-dirty`, commit `70bf0a2`):

| Local file | Page | Width | Theme | Shows |
| --- | --- | --- | --- | --- |
| `tmp/evidence/M3/screenshots/login-desktop-light.png` | login | 1280 | light | "Admin Login" card with logo, masked password, Sign In button, `dermaga admin set-password` hint |
| `tmp/evidence/M3/screenshots/login-narrow-light.png` | login | 375 | light | same card full width, hint wrapped over two lines |
| `tmp/evidence/M3/screenshots/login-desktop-dark.png` | login | 1280 | dark | same card on a near-black page, sun icon for the theme toggle |
| `tmp/evidence/M3/screenshots/login-narrow-dark.png` | login | 375 | dark | same as above at narrow width |
| `tmp/evidence/M3/screenshots/dashboard-desktop-light.png` | dashboard | 1280 | light | "Server Overview": Running (uptime 7s), RSS 144.1 MB with target < 30 MB idle, Schema v1, version `v0.3.0-rc.2-dirty`, commit 70bf0a2 |
| `tmp/evidence/M3/screenshots/dashboard-narrow-light.png` | dashboard | 375 | light | the same cards stacked, tabs under the header |
| `tmp/evidence/M3/screenshots/dashboard-desktop-dark.png` | dashboard | 1280 | dark | the same four cards in dark colours |
| `tmp/evidence/M3/screenshots/dashboard-narrow-dark.png` | dashboard | 375 | dark | stacked cards in dark colours |
| `tmp/evidence/M3/screenshots/audit-desktop-light.png` | audit log | 1280 | light | nine entries (login, login_failed, daemon_start, admin_set_password, logout) with origin badges and filters set to all |
| `tmp/evidence/M3/screenshots/audit-narrow-light.png` | audit log | 375 | light | filters stacked; the table is wider than the screen and cut at the Event column |
| `tmp/evidence/M3/screenshots/audit-desktop-dark.png` | audit log | 1280 | dark | filtered to origin "panel", last hour: five panel entries |
| `tmp/evidence/M3/screenshots/audit-narrow-dark.png` | audit log | 375 | dark | the same five entries; Time, Origin and Event columns visible |

The 144.1 MB RSS is a local dev-machine run of a dirty build, not an NFR-2 measurement (those run on
the VM, M9).

#### process2-evidence-bytes.log

```text
== before: tracked at HEAD (git ls-tree -r -l HEAD docs/evidence)
log   62 files    724636 bytes
md     4 files    132011 bytes
png   23 files   1356248 bytes
total         2212895 bytes
== after: files that will be tracked (existing tracked files plus unignored untracked files)
md     6 files    171197 bytes
total          171197 bytes
# Exit: 0
```

#### process2-check-commits.log

```text
PASS static checks: 9 commits, 112 paths
PASS commit 1: chore(evidence): delete the reviewed M0-M3a logs (log: .check-logs/commits/commit1.log)
PASS commit 2: chore(evidence): delete the reviewed M1 screenshots (log: .check-logs/commits/commit2.log)
PASS commit 3: test: pin VM test images and write evidence only to tmp/evidence (log: .check-logs/commits/commit3.log)
PASS commit 4: build: quiet make check and add policy and shellcheck steps (log: .check-logs/commits/commit4.log)
PASS commit 5: build: add evidence-run, check-commits and selftest scripts (log: .check-logs/commits/commit5.log)
PASS commit 6: docs(agents): slim AGENTS.md and move rules to their documents (log: .check-logs/commits/commit6.log)
PASS commit 7: docs: add the active milestone and code map, record DEC-25 to DEC-27 (log: .check-logs/commits/commit7.log)
PASS commit 8: docs(m3): record the process checkpoint in the M3 evidence (log: .check-logs/commits/commit8.log)
PASS commit 9: test(vm): add the M3b installer driver (log: .check-logs/commits/commit9.log)
# Exit: 0
```

Plan: `tmp/evidence/M3/commits.plan`. Both excerpts above were pasted after their runs, so this
file is a few kilobytes larger than the byte count shows (a documentation-only edit; the set of paths
is unchanged). Commits 1–3 predate the `policy` and `shellcheck` steps; from commit 4 on, both run.

#### References rewritten

Each link to a deleted log now carries the essential exact lines and "removed 2026-11-17, see git
history before this change":

- `docs/ARCHITECTURE.md` DEC-20: `traefik_image_query.log`, `traefik_acme_source_check.log` (index
  digest, amd64 manifest, ACME `0o600` write and empty-e-mail account branch).
- `docs/ARCHITECTURE.md` DEC-21: `lap1_dash_zsh_container.log` (zsh and dash, one block each, exit 0).
- `docs/ARCHITECTURE.md` DEC-24: `release_rc1_failure.log`, `release_sim_c7c4671.log`,
  `release_sim_fixed.log` (dubious ownership failure; simulation fails then passes).
- `docs/DEPENDENCIES.md`: `dependency_versions_query.log` (command and result lines), and
  `debian_packages_query.log` (madison result lines for gh, zsh, fish, openssh-server).
- `internal/traefik/types.go` comment: now points to `docs/DEPENDENCIES.md` §3 (comment only).
- `test/vm/measure_nfr2.sh`, `ui/e2e/panel.test.ts`, `ui/scripts/capture_vm.ts`: write to
  `tmp/evidence/`; the six screenshots that duplicated a light brand shot (same page state) are no
  longer taken twice.
- Not changed: `CHANGELOG.md` (history, out of scope) mentions `docs/evidence/M1/brand/`; the older
  EVIDENCE.md files of M0–M2 keep their links, explained in `docs/evidence/README.md`.

#### Follow-up decisions and limitations

- DEC-26 and DEC-27 added to the ARCHITECTURE decisions log; no existing entry rewritten except the
  evidence-link cells of DEC-20, DEC-21 and DEC-24.
- `scripts/check-policy` checks only files that exist, so unstaged deletions of old artifacts pass
  before the owner commits them.
- shellcheck findings in the older scripts are unknown until the first CI run; the CI step fails on
  any finding.
- The newly pinned socat and curl images run only in M3b.
