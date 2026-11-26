# Evidence: M6 (0.6.0) — First deploy: wizard, build, env, logs

Commit under review: `3b88d01` (`v0.6.0-rc.3`) + final M6b UI/VM script updates. Milestone **M6 (0.6.0)** complete.

## Acceptance criteria

| Criterion (req ID) | Status | Log file | Excerpt (≤20 lines) |
| --- | --- | --- | --- |
| `make check` (`ui`, `lint`, `test`, `vulncheck`, `build-all`, `policy`) | PASS | `tmp/evidence/M6/logs/make-check-m6a.log` | `=== make check passed successfully ===` |
| WIZ-2, WIZ-3, GHA-5: GitHub branch list & repo tree/Containerfile/`EXPOSE`/submodule/LFS inspection | PASS | `tmp/evidence/M6/logs/make-check-m6a.log` | `TestInspectRepo_WIZ3`, `TestParseExposedPort_VariableAndEmpty_WIZ3` |
| `0004` migration (`0003 -> 0004`), `FailInFlightDeployments`, `DeploymentStatusLabel` (`DEP-9`) | PASS | `tmp/evidence/M6/logs/make-check-m6a.log` | `TestStoreMigration0003To0004` |
| DEP-7, DEP-8, SEC-9, GHA-5: Mirrors via `GIT_CONFIG_*`, `podman build` limits, chunk-safe masking, path traversal check | PASS | `tmp/evidence/M6/logs/make-check-m6a.log` | `TestMasker_ChunkBoundaryAndLongestFirst_SEC9`, `TestBuilder_BuildAndMasking_GHA5_DEP7_DEP8`, `TestBuilder_PathTraversalRejected` |
| DEP-3, DEP-5, DEP-6, DEP-9: First deploy, failing deploy cleanup, running build cancel cleanup | PASS | `tmp/evidence/M6/logs/make-check-m6a.log` | `TestFirstDeploy_Success_DEP9`, `TestFirstDeploy_FailureLeavesNothing_DEP3`, `TestFirstDeploy_CancelRunningBuildLeavesNothing_DEP6` |
| WIZ-6, APP-1, APP-9, UI-5, UI-6, AUD-1: Apps API auth, field validation, hidden env + audited reveal, typed delete | PASS | `tmp/evidence/M6/logs/make-check-m6a.log` | `TestAppsAPI_CreateEnvRevealDelete_WIZ6_APP1_UI5_APP9` |
| VM (`v0.6.0-rc.2` from `fresh`): items 1–6 & 8 (Pebble TLS live, `DEC-07` history/inspect, `DEC-31` 0 cmdline/config/DB/log hits, `DEC-05` limits, timeout, fail/cancel cleanup, restart recovery, empty repo 409) | PASS | `tmp/evidence/M6/logs/vm-m6a-podman-rc2.log` | `PASS [1c]: HTTPS via Pebble cert returned: healthy`, `PASS [2]`/`[3]`/`[4a]`/`[4b]`/`[5a]`/`[5b]`/`[6]`/`[8]` |
| VM (`v0.6.0-rc.2`): item 7 (`APP-9` remove app) | FAIL | `tmp/evidence/M6/logs/vm-m6a-podman-rc2.log` | Superseded by `v0.6.0-rc.3` PASS below after `internal/deploy/deploy.go` cleanup fix |
| Release `v0.6.0-rc.3` tag, workflow & `checksums.txt` | PASS | `tmp/evidence/M6/logs/release-rc3-verify.log` | `3b88d01225553a474cd44757a513dff0e21a3ed7`, `sha256sum -c checksums.txt` (`OK`) |
| M6b: `make check` (`LineAccumulator` bounded flush + split secret, runtime log masking, SvelteKit UI) | PASS | `tmp/evidence/M6/logs/make-check-m6b.log` | `=== make check passed successfully ===` |
| M6b: Playwright E2E (`apps.test.ts` + full suite, desktop + narrow, light + dark, axe WCAG AA) | PASS | `tmp/evidence/M6/logs/playwright-e2e-m6b.log` | `8 passed` (Firefox + axe WCAG AA) |
| VM (`v0.6.0-rc.3` from `fresh`): full Podman suite items 1–8 including timeout, fail/cancel, and `APP-9` remove app (`0` leftover images/containers) | PASS | `tmp/evidence/M6/logs/vm-m6b-podman-rc3.log` | `PASS [4a]`/`[4b]`/`[5a]`/`[5b]`/`[6]`/`[7]`, `=== ALL M6a REAL-PODMAN CHECKS PASSED ===` |
| VM (`v0.6.0-rc.3`): browser wizard via SSH tunnel (`dermaga-test-app`, live build log, runtime + build-time env, Pebble TLS, audited reveal, runtime logs, typed delete) | PASS | `tmp/evidence/M6/logs/vm-m6b-browser-wizard.log` | `PASS [browser 1]`–`[browser 8]` |
| VM (`v0.6.0-rc.3`): wizard Go example `Containerfile` built on real Podman (non-root `uid=1000`, `200 go-ok` on port `8080`) | PASS | `tmp/evidence/M6/logs/vm-m6b-wizard-examples.log` | `go_uid=1000 go_resp=go-ok`, `PASS: wizard Go example Containerfile built, ran non-root (uid=1000), and answered on 8080` |
| VM (`v0.6.0-rc.3`): secret greps (journal, DB, runtime logs) & `secret_reveal` audit | PASS | `tmp/evidence/M6/logs/vm-m6b-greps.log` | `journal_secret_hits=0 db_secret_hits=0`, `Revealed runtime variable VM_RT_SECRET for app vmwizard` |
| VM (`v0.6.0-rc.3`): `m3_check.sh control` then `m3_check.sh access` LAST | PASS | `tmp/evidence/M6/logs/vm-m6b-m3-control.log`, `tmp/evidence/M6/logs/vm-m6b-m3-access.log` | `all checks passed` |
| Release `v0.6.0` tag (`e4434b5`), workflow, `checksums.txt`, tamper negative control & `install.sh` diff vs `rc.3` | PASS | `tmp/evidence/M6/logs/release-v060-verify.log` | `dermaga-linux-amd64: OK`, tamper `FAILED`, `install.sh` diff only `DERMAGA_VERSION` |
| VM (`v0.6.0` smoke from `fresh`): install, setup, login, browser wizard (`vmwizard`), Pebble TLS, remove with 0 leftovers, `m3_check.sh access` LAST | PASS | `tmp/evidence/M6/logs/vm-v060-smoke-wizard.log`, `tmp/evidence/M6/logs/vm-v060-smoke-clean.log`, `tmp/evidence/M6/logs/vm-v060-smoke-access.log` | `PASS [browser 1]`–`[browser 8]`, `leftovers: ps=0 img=0 unit=0 tr=0 env=0 mir=0`, `all checks passed` |

### `tmp/evidence/M6/logs/make-check-m6a.log`

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

### `tmp/evidence/M6/logs/vm-m6a-podman-rc2.log`

```text
PASS [8]: dermaga-test-empty inspect has_containerfile=false
PASS [1a]: dermaga-test-app inspect containerfile=Containerfile port=8080
INFO [cmdline sample]: ghs_=0 build_val=0
PASS [1b]: testapp deployed status=active label=live
PASS [1c]: HTTPS via Pebble cert returned: healthy
PASS [1d]: runtime env verified in container; build-time var reached build (build=m6a_build_val_772)
PASS [2]: 0 hits in build_logs/journal/DB/cmdline; expected DEC-07 leak in podman history: <missing> ... |1 BUILD_VAR=m6a_build_val_772 /bin/sh
          expected DEC-07 leak in image inspect: /bin/sh -c #(nop) ARG BUILD_VAR
|1 BUILD_VAR=m6a_build_val_772 /bin/sh -c echo "build=$BUILD_VAR" > /etc/build-info
PASS [3]: 0 hits for installation token in /proc/*/cmdline, mirror .git/config, logs, and DB
PASS [4a]: podman inspect limits: Memory=536870912 NanoCpus=1000000000 PidsLimit=128
PASS [4b]: build exceeding timeout killed and marked failed: build timed out after 1s
PASS [5a]: failing first deploy left 0 containers, images, units, or Traefik routes
PASS [5b]: cancelled build left 0 containers, images, units, or Traefik routes
PASS [6]: daemon restart mid-build marked deployment failed (daemon restarted mid-deployment); testapp r1 still live
AssertionError: testapp residue: ps=0 img=1 unit=0 tr=0 env=0 mir=0
```

## Run & view

- Local checks: `make check`

## Screens

| Local file | Page | Width | Theme | Shows |
| --- | --- | --- | --- | --- |
| `tmp/evidence/M6/screenshots/m6b-wizard-step2-light-desktop.png` | `/apps/new` (Step 2) | 1280 | light | 4-step wizard with detected `Containerfile`, build context `.`, and suggested port `8080` |
| `tmp/evidence/M6/screenshots/m6b-wizard-step3-light-desktop.png` | `/apps/new` (Step 3) | 1280 | light | Health check, domain DNS match banner, and build-time `--build-arg` warning with hidden value |
| `tmp/evidence/M6/screenshots/m6b-wizard-step4-live-light-desktop.png` | `/apps/new` (Step 4) | 1280 | light | Summary card, `live` status badge, clickable live URL, and streamed build log |
| `tmp/evidence/M6/screenshots/m6b-app-env-light-desktop.png` | `/apps/[id]` (Environment) | 1280 | light | 6 active M6 tabs, `PUBLIC_SITE` with `Runtime` & `Build time (--build-arg)` badges and revealed value |
| `tmp/evidence/M6/screenshots/m6b-app-settings-dark-narrow.png` | `/apps/[id]` (Settings) | 375 | dark | Horizontally scrollable tab bar and stacked Health/Resources/Build settings with zero horizontal overflow |
| `tmp/evidence/M6/screenshots/m6b-vm-wizard-live.png` | `/apps/new` (Step 4, live VM) | 1280 | light | Real `vmwizard` deploy from `r3p-dev/dermaga-test-app` reaching `live` at `https://vmwizard.dermaga.test` with streamed `podman build` log |

## How it works

1. **Repo inspection (`WIZ-2`, `WIZ-3`)**: `GET /api/github/repos/{owner}/{repo}/branches` and `GET /api/github/repos/{owner}/{repo}/inspect` mint an in-memory repo-scoped installation token (`GHA-5`), read the Git tree and `Containerfile`/`Dockerfile`/`.gitattributes` via REST API (no clone), extract `EXPOSE` ports, and flag `.gitmodules` / `filter=lfs`.
2. **Build & masking (`DEP-5`, `DEP-7`, `DEP-8`, `SEC-9`)**: `deploy.Engine` maintains a single-concurrency global queue (superseding older queued triggers per app). `builder.Builder` clones/fetches bare mirrors in `~/.cache/dermaga/mirrors/<app>.git` using `GIT_CONFIG_*` auth (never in URLs, `.git/config`, or `ps`), exports via `git archive`, runs `podman build` with `--memory`/`--cpus`/timeout limits, and streams output through a chunk-boundary-safe `StreamMasker` into `LogHub`.
3. **First deploy & cleanup (`DEP-3`, `DEP-6`, `APP-5`, `APP-9`)**: Generates `app-<name>-r<num>.container` (`PodmanArgs=` per `DEC-05`, `DEC-17` markers), validates via `podman-system-generator --dryrun --user`, starts the user service, probes health (`http` netns probe, `cmd` `Notify=healthy`, or `none` 5 s stability window), and writes `app-<name>.yml` atomically. Any failure or cancellation removes candidate units, env files, images, and routes; `Start()` marks interrupted deployments `failed`.

## Code tour

1. `internal/github/inspect.go`: branch listing, commit lookup, and repo tree/Containerfile/`EXPOSE` inspection (`WIZ-2`, `WIZ-3`).
2. `internal/store/migrations/0004_m6_apps_deploy.sql`, `internal/store/apps.go`: schema upgrade, app/deployment CRUD, `FailInFlightDeployments`, `DeploymentStatusLabel`.
3. `internal/builder/mask.go`, `loghub.go`, `builder.go`: chunk-safe `SEC-9` masking, bounded `LogHub`, bare mirrors, `podman build`.
4. `internal/deploy/env.go`, `deploy.go`: encrypted env helpers, single global queue, first-deploy lifecycle, `APP-9` removal.
5. `internal/server/apps.go`, `types.go`, `server.go`, `cmd/dermaga/main.go`: M6a REST endpoints, SSE log streams (`LOG-1`, `LOG-2`), daemon wiring.

## Decisions & deviations

- Kept `0001_init.sql` deployment status enum unchanged and mapped `probing -> healthy` and `active -> live` in `store.DeploymentStatusLabel` (`DEP-9`), leaving `superseded`, `switching`, and `inactive` internal until M7.
