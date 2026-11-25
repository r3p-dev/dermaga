# Evidence: M6 (0.6.0) — First deploy: wizard, build, env, logs

Commit under review: `HEAD` (working tree). Checkpoint: **M6a** (build pipeline & first deploy backend).

## Acceptance criteria

| Criterion (req ID) | Status | Log file | Excerpt (≤20 lines) |
| --- | --- | --- | --- |
| `make check` (`ui`, `lint`, `test`, `vulncheck`, `build-all`, `policy`) | PASS | `tmp/evidence/M6/logs/make-check-m6a.log` | `=== make check passed successfully ===` |
| WIZ-2, WIZ-3, GHA-5: GitHub branch list & repo tree/Containerfile/`EXPOSE`/submodule/LFS inspection | PASS | `tmp/evidence/M6/logs/make-check-m6a.log` | `TestInspectRepo_WIZ3`, `TestParseExposedPort_VariableAndEmpty_WIZ3` |
| `0004` migration (`0003 -> 0004`), `FailInFlightDeployments`, `DeploymentStatusLabel` (`DEP-9`) | PASS | `tmp/evidence/M6/logs/make-check-m6a.log` | `TestStoreMigration0003To0004` |
| DEP-7, DEP-8, SEC-9, GHA-5: Mirrors via `GIT_CONFIG_*`, `podman build` limits, chunk-safe masking, path traversal check | PASS | `tmp/evidence/M6/logs/make-check-m6a.log` | `TestMasker_ChunkBoundaryAndLongestFirst_SEC9`, `TestBuilder_BuildAndMasking_GHA5_DEP7_DEP8`, `TestBuilder_PathTraversalRejected` |
| DEP-3, DEP-5, DEP-6, DEP-9: First deploy, failing deploy cleanup, running build cancel cleanup | PASS | `tmp/evidence/M6/logs/make-check-m6a.log` | `TestFirstDeploy_Success_DEP9`, `TestFirstDeploy_FailureLeavesNothing_DEP3`, `TestFirstDeploy_CancelRunningBuildLeavesNothing_DEP6` |
| WIZ-6, APP-1, APP-9, UI-5, UI-6, AUD-1: Apps API auth, field validation, hidden env + audited reveal, typed delete | PASS | `tmp/evidence/M6/logs/make-check-m6a.log` | `TestAppsAPI_CreateEnvRevealDelete_WIZ6_APP1_UI5_APP9` |
| M6b: Wizard stepper, App tabs, Playwright E2E, VM journey from `fresh` (`v0.6.0-rc.1`), `0.6.0` bump | NOT RUN | — | Pending M6b |

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

## Run & view

- Local checks: `make check`

## Screens

- Deferred to M6b (UI views & Playwright E2E).

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
