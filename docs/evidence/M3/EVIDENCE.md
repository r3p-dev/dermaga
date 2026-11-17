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

