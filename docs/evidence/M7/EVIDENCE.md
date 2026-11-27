# Evidence: M7 (0.7.0) — Zero downtime: push-to-deploy, redeploy, rollback

Commit under review: `5230917` + the uncommitted Step 0 changes (dirty: the owner commits them; the
suggested commits are checked one by one with `scripts/check-commits`). Checkpoint: **Step 0 (`v0.6.2`)**.
M7a and M7b follow in later sections.

## Step 0 (`v0.6.2`): full-width panel, server overview, plain wording

### Acceptance criteria

| Criterion (req ID) | Status | Log file | Excerpt |
| --- | --- | --- | --- |
| `make check` as a normal user (canonical Tailwind, Prettier, `svelte-check --fail-on-warnings`, Go, policy) | PASS | `tmp/evidence/M7/logs/v062-make-check.log` | see below |
| UI-8: main content fills the width at 1280, 1920 and 375, light and dark; axe WCAG AA on every page | PASS | `tmp/evidence/M7/logs/v062-test-browser.log` | see below |
| UI-2: server overview shows only `/api/server/status` and `/api/apps` values | PASS | `tmp/evidence/M7/logs/v062-test-browser.log` | `layout.test.ts` compares version and schema version with the API response |
| No requirement codes in visible text (rendered pages, both themes) | PASS | `tmp/evidence/M7/logs/v062-test-browser.log` | `layout.test.ts` checks `body.innerText` on 7 pages; its negative control asserts the matcher finds `NFR-2` in "Footprint (NFR-2)" |
| No requirement codes in visible strings (static, `ui/src`) | PASS | `tmp/evidence/M7/logs/v062-no-codes.log` | see below |
| Negative control for the static check | PASS | `tmp/evidence/M7/logs/v062-no-codes-negative.log` | see below |
| Release, checksums, VM install from `fresh`, UI smoke test via the tunnel, `m3_check.sh access` | PASS | see § After the tag | `v0.6.2` = `3e4d902` |
| "System" page (version, CPU, RAM, disk) | decision recorded | — | note in `docs/PLAN.md` M9 (not built) |
| Suggested commits each pass `make check` alone | PASS | `tmp/evidence/M7/logs/v062-check-commits.log` | see below |

#### `v062-check-commits.log`

```text
PASS static checks: 4 commits, 17 paths
PASS commit 1: docs(architecture): drop duplicated 0004 blocks and cloning status (log: .check-logs/commits/commit1.log)
PASS commit 2: feat(ui): use full width and show a plain server overview (log: .check-logs/commits/commit2.log)
PASS commit 3: docs(plan): record M7 split, owner decisions and M9 System page (log: .check-logs/commits/commit3.log)
PASS commit 4: chore(release): prepare v0.6.2 changelog and evidence (log: .check-logs/commits/commit4.log)
```

#### `v062-make-check.log`

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

#### `v062-test-browser.log`

```text
  ✓  5 [firefox] › e2e/apps.test.ts:31:2 › M6b: New App Wizard, App Tabs, Env, Logs (WIZ-1..6, APP-1/5/6/7/9, LOG-1..2, UI-1..8) › wizard stepper, missing Containerfile guidance, app tabs desktop+narrow light+dark, env reveal, and typed delete (13.8s)
  ✓  8 [firefox] › e2e/panel.test.ts:114:2 › Dermaga Theme & Panel Journeys (M1-dark) › full panel journeys, theme switching, computed backgrounds, and axe AA contrast (16.5s)
  ✓  6 [firefox] › e2e/layout.test.ts:54:2 › v0.6.2 layout (UI-2, UI-8) › UI-8: main content fills the width; UI-2: server overview shows only API values (24.7s)
  9 passed (29.6s)
```

The suite passed in three consecutive runs (same log name, the last run kept).

#### `v062-no-codes.log` / `v062-no-codes-negative.log`

```text
no requirement codes in visible strings under ui/src
# Exit: 0
```

```text
FOUND:
…/scratchpad/neg/x.svelte:1:<span>Footprint (NFR-2)</span>
# Exit: 0
```

The negative fixture also held `// comment DEP-1 ok`, which the check correctly ignored (comments may keep codes).

### After the tag (`v0.6.2` = `3e4d902`, binary from the published release)

| Check | Status | Log file |
| --- | --- | --- |
| Tag, `main` and `HEAD` all at `3e4d902`; CI and Release workflows `success` | PASS | (`git rev-parse`, `gh run list`) |
| Release assets match `checksums.txt`; the binary reports `v0.6.2 (commit 3e4d902)` | PASS | `tmp/evidence/M7/logs/v062-release-checksums.log` |
| Negative control: one appended byte makes `sha256sum -c` fail | PASS | `tmp/evidence/M7/logs/v062-release-checksums-negative.log` |
| Revert to `fresh` (announced), `on_reboot=restart` live after a cold start | PASS | `tmp/evidence/M7/logs/v062-vm-revert.log`, `v062-vm-coldstart.log` |
| Published installer from `fresh` (`drive_install.py`), exit 0 | PASS | `tmp/evidence/M7/logs/v062-vm-install.log` |
| UI smoke via the SSH tunnel (`test/vm/smoke_ui.mjs`): setup, 6 width × theme combinations, values = API, no codes | PASS | `tmp/evidence/M7/logs/v062-vm-smoke-ui.log` |
| `m3_check.sh access` (first attempt, ControlMaster unban) | FAIL | `tmp/evidence/M7/logs/v062-vm-m3-access.log` (overwritten by the rerun; the failure is described below) |
| `m3_check.sh access` (VM-side unban, after a workstation reboot and `virsh start`) | PASS | `tmp/evidence/M7/logs/v062-vm-m3-access.log` |
| New `ssh dermaga` with the owner's config 71 s after the last denied attempt | PASS | `tmp/evidence/M7/logs/v062-vm-ssh-dermaga.log` |

```text
PASS  password login not offered to server
PASS  root refused, even with the key
PASS  provider user debian refused, even with the key
INFO  last denied attempt: 2026-11-26T19:00:28+07:00
PASS  a new login works again after the VM-side unban (2026-11-26T19:01:28+07:00)
      2026-11-26 12:00:28,654 fail2ban.actions        [658]: NOTICE  [sshd] Ban <ip>
      2026-11-26 12:01:28,679 fail2ban.actions        [658]: NOTICE  [sshd] Unban <ip>
PASS  fail2ban banned the workstation during the denied logins (bans 1 -> 2)
all checks passed
```

```text
last denied: 2026-11-26T19:00:28+07:00  now: 2026-11-26T19:01:39+07:00  gap: 71 s
connected as server on dermaga-vm
dermaga version v0.6.2 (commit 3e4d902)
```

**First `m3_check.sh access` run failed.** The unban over the open ControlMaster hung: fail2ban's
`nftables-multiport` ban rejects every packet to port 4222 from the banned IP, including on established
connections, so the unban request never reached the VM (`ssh dermaga-vm` → "Connection refused" for the
1 h ban). The ControlMaster unban (commit `d08b137`) had never run against a VM before: both M6 access logs
predate it. Owner decision: the script now schedules the unban on the VM (`systemd-run --on-active=60s`
and `120s`, as root through the sudo password file) before the denied logins.

| Screenshot | Page | Width | Theme | Shows |
| --- | --- | --- | --- | --- |
| `tmp/evidence/M7/screenshots/vm-v062-dashboard-1920-light.png` | Dashboard (VM) | 1920 | light | Full width; Status Running, Memory 85.5 MB amber (right after an Argon2id login), Apps 0, Version `v0.6.2` / `3e4d902` |
| `tmp/evidence/M7/screenshots/vm-v062-dashboard-375-dark.png` | Dashboard (VM) | 375 | dark | Theme toggle and Log out in the top row, cards stacked, `v0.6.2` |

### Open items checked before M7a

| Item | Status | Log file | Result |
| --- | --- | --- | --- |
| Test app can serve 50 req/s and has a failable `/health` | FAIL (current repo) | — | Its `nc -l` loop serves one connection at a time and has no `/health`; it cannot prove zero downtime |
| Replacement Containerfile (busybox `httpd`, Alpine 3.24 by digest, `/health`, `/version`) | PASS (local podman) | `tmp/evidence/M7/logs/testapp-local-final.log` | 501 × 200 at 50 req/s, `/health` 200, `podman stop` 0.09 s |
| Same file without the `/health` line (the "broken health" commit) | PASS (local podman) | `tmp/evidence/M7/logs/testapp-local-broken.log` | `/` 200, `/health` 404 |
| `httpd` as PID 1 ignores SIGTERM | decision recorded | `tmp/evidence/M7/logs/testapp-local-stop.log` | Stop took 10.10 s (SIGKILL); the test app traps TERM, and dermaga will run apps with `RunInit=true` (owner decision) |
| `oha --cacert --connect-to` (Pebble-style private CA, name pinned to an IP) | PASS (local) | `tmp/evidence/M7/logs/oha-cacert-local.log` | `[200] 151 responses`; negative control without `--cacert`: `UnknownIssuer`, 0 % success |
| gosmee target in SETUP §4.2 | decision recorded | — | The old target (`localhost:7070`) never served `/hooks/github-app`; corrected, and the exact invocation is verified in M7b |
| Version card: a long dev version stays on one line, full value in `title` (owner request after `v0.6.2`) | PASS | `tmp/evidence/M7/logs/m7a-pre-test-browser.log` | `layout.test.ts` stubs `/api/server/status` with `v0.6.2-14-g3e4d902-dirty` at 1280: 9/9 pass; negative control (without `truncate`) fails with `long version on one line at 1280` |
| `oha` 1.17.0 and `gosmee` 0.32.1 pinned | PASS | `tmp/evidence/M7/logs/tools-query.log`, `tools-download.log` | `sha256sum -c` OK for both |

### Screens

| Local file | Page | Width | Theme | Shows |
| --- | --- | --- | --- | --- |
| `tmp/evidence/M7/screenshots/dashboard-1920-dark.png` | Dashboard | 1920 | dark | Content fills the width next to the sidebar; four server cards (Status "Running · Up 13s · Database: healthy", Memory 202.7 MB in amber, Apps 0, Version); no footer |
| `tmp/evidence/M7/screenshots/dashboard-1280-light.png` | Dashboard | 1280 | light | Same layout at 1280; the long dev version string wraps in the Version card and the commit is cut to "523…" (a tagged version such as `v0.6.2` fits) |
| `tmp/evidence/M7/screenshots/dashboard-375-light.png` | Dashboard | 375 | light | Theme toggle and Log out in the top row, four page links below without clipping; cards stacked |
| `tmp/evidence/M7/screenshots/dashboard-375-dark.png` | Dashboard | 375 | dark | Same as above in dark; amber memory value readable |
| `tmp/evidence/M7/screenshots/activity-1920-dark.png` | Activity | 1920 | dark | Table uses the full width; subtitle without "(AUD-1, AUD-2)" |

Memory is high in the test runs because every test logs in and Argon2id verification has a 64 MiB peak
(DEC-12); the amber tone is the intended warning. Idle RSS is measured on the VM (M9, NFR-2).

### How it works

`+page.svelte` loads `GET /api/server/status` (version, commit, uptime, `database_ok`, `schema_version`,
`daemon_rss`) and `GET /api/apps`, and renders four cards from them. Nothing else is shown: the API's
`total_memory`/`used_memory` are the Go runtime's `Sys`/`Alloc` (not host memory), so they are no longer displayed.

### Code tour

1. `ui/src/routes/+layout.svelte`: `<main>` without `max-w-*` (`px-4 md:px-6 lg:px-8`); footer removed; narrow header top row holds theme toggle and Log out.
2. `ui/src/routes/+page.svelte`: "Server" section; `daemonMemoryTarget` drives the amber tone; schema version in the `title` of "Database: healthy".
3. `ui/src/lib/components/AppsList.svelte`: `xl:grid-cols-3 2xl:grid-cols-4`.
4. `ui/src/routes/activity/+page.svelte`, `apps/[id]/+page.svelte`, `login/+page.svelte`: codes removed from visible text.
5. `ui/e2e/helpers.ts`: `settleTransitions` (axe waits for CSS transitions; cancelled ones resolve).
6. `ui/e2e/layout.test.ts`: widths × themes, full-width geometry, API-sourced values, no codes, axe.

### Decisions & deviations

- **Firefox content-process crash (Playwright Firefox build 1572).** An earlier version of the cards
  (a `<dl>` grid rendered through a snippet, then an `{#each}` loop) made Firefox's content process SIGSEGV in
  `libxul` in about 1 in 4 runs of `apps.test.ts` (`coredumpctl` listed 22 Firefox crashes during the
  bisect). Removing the `<dl>`, the snippet, the `title` attribute or the non-ASCII characters one at a time did not stop it. Starting
  from the v0.6.1 card markup and changing only text and values gave 0/10 crashes at every step, and no
  crash appeared in the three final full runs. This is a browser bug, not a dermaga defect. The kept markup is
  the v0.6.1 card structure.
- **Mid-transition contrast flake (already in v0.6.1).** Exporting `v0.6.1` with `git archive` into the scratchpad
  and running `apps.test.ts` 6 times failed axe 3/6 with half-blended colours (e.g. 4.18:1, while the settled pair
  #1A1B36 on #7C78EE is 4.61:1). Fixed in the tests (`settleTransitions`), not by changing colours.
- **Narrow header.** Theme toggle and Log out moved to the empty top row because "Log out" was clipped at 375 px.
- **Codes outside the panel stay** (owner decision): CLI output (`SEC-1`, `SEC-2`, `SEC-11`, `DEP-10`) and the
  plain-text `SEC-3` HTTP bodies. The panel parses only JSON `error` fields, so those bodies never reach the
  page; any future panel display maps them to plain wording.
- **ARCHITECTURE cleanup (separate docs commit):** six stray copies of the `0004` migration snippet removed
  from §2, §4.2 and §5.4; the `cloning` status folded into `building` (the code fetches inside `building`);
  timeouts in §6.1 corrected to the code and PRD (build 20 min `DEP-7`, probe = app health start timeout `APP-5`).

## M7a: zero-downtime redeploy and rollback (local part)

Commit under review: `208ebb6` + the uncommitted M7a changes (dirty: the owner commits them; the VM part
runs on `v0.7.0-rc.1` after the tag). Requirement IDs: `DEP-1`, `DEP-2`, `DEP-4`, `DEP-9`, `DEP-10`, `UI-3`,
`UI-7`, `UI-8`.

### Acceptance criteria

| Criterion (req ID) | Status | Log file | Proof |
| --- | --- | --- | --- |
| `make check` (Go, Svelte, Tailwind, Prettier, policy, shellcheck) | PASS | `tmp/evidence/M7/logs/m7a-make-check.log` | all steps `ok` |
| DEP-1: release started next to the live one, both verify steps proven, route moved, old drained and stopped, image kept | PASS | `tmp/evidence/M7/logs/m7a-go-tests.log` | `TestRedeploy_SwitchesWithProof_DEP1` |
| DEP-2: failing health keeps the live release, route and unit | PASS | same | `TestRedeploy_HealthFails_OldStaysLive_DEP2` |
| DEP-2: either verify step failing restores the live-only route | PASS | same | `TestRedeploy_VerifyFails_RestoresRoute_DEP2/step1`, `/step2` |
| Negative control: without the route restore both step tests fail (`live=r2 verify=true` at step 2) | PASS | (run in this session, edit reverted) | see Decisions |
| Memory check refuses before the build, with both values; live untouched (owner decision) | PASS | same | `TestMemoryCheck_RefusesDeploy` |
| DEP-4: rollback runs r1's image with r1's env version, no build; r2 → Rolled back; panel env unchanged | PASS | same | `TestRollback_RestoresReleaseEnv_DEP4` |
| Negative control: marking the replaced release `inactive` makes that test fail (`r2=inactive`) | PASS | (run in this session, edit reverted) | |
| Rollback refused while the app builds ("Cancel the running build first", 409) | PASS | same | `TestRollback_RefusedWhileBuilding`, `TestRollbackAPI_RefusedWhileBuilding` |
| DEP-4: retention keeps live + last 5; r1/r2 images removed; rollback to r2 refused, to r3 allowed | PASS | same | `TestRetention_KeepsLiveAndLastFive_DEP4` |
| Migration `0005` from a populated `0004` DB; `rolled_back` accepted, unknown status rejected; legacy snapshot converted | PASS | same | `TestStoreMigration0004To0005_DEP4`, `TestEnvVersions_DEC32`, `TestLegacySnapshotConversion` |
| DEP-10: crash after the main router moved → candidate adopted (also for a rollback); crash before → old kept; one live, one route, no verify router | PASS | same | `TestReconcile_CrashAfterMainSwitched_DEP10/push`, `/rollback`, `TestReconcile_CrashBeforeMainSwitched_DEP10` |
| Negative control: without `recoverSwitches` all three crash tests fail | PASS | (run in this session, edit reverted) | `after recovery: live=dep_shop_r1 r1=active r2=switching` |
| DEP-10: a re-rendered unit has the release's image, limits, init and env file (was `localhost/<name>:rN`) | PASS | same | `TestReconcile_ReRenderedUnitMatchesDeploy_DEP10` |
| API: rollback, preview (names only, no values), memory check, commit redeploy; authentication on each route; audit entry | PASS | same | `TestRollbackAPI_DEP4`, `TestRedeployCommitValidation` |
| `RunInit=true` on Podman 5.4.2: dry-run gives `--init`; unknown key rejected; stop 0.15 s vs 10.17 s | PASS | `tmp/evidence/M7/logs/runinit-vm.log` | DEC-34 |
| Golden files: only `RunInit=true` (7 units) and the verify-ack middleware (switch golden) changed | PASS | (git diff in this session) | `7 +RunInit=true` |
| UI: Deployments tab (mockup A), rollback dialog (mockup A), redeploy dialog, refusals, live status; axe AA at 1280/375, light and dark | PASS | `tmp/evidence/M7/logs/m7a-test-browser.log` | `10 passed`, `e2e/rollback.test.ts` |
| Negative control: showing "Roll back" on every row fails the journey (`Expected: 1, Received: 3`) | PASS | (run in this session, edit reverted) | |
| VM: oha 50 req/s over the Pebble certificate during (a) a redeploy and (b) a redeploy of a commit whose `/health` fails: 0 non-2xx, 0 connection errors | NOT RUN | — | needs `v0.7.0-rc.1`, a connected GitHub App on the VM and the owner's test-app pushes (report) |
| VM: rollback < 10 s with zero downtime; daemon killed mid-switch → one live release, no verify router | NOT RUN | — | same |

### Screens

| Local file | Page | Width | Theme | Shows |
| --- | --- | --- | --- | --- |
| `tmp/evidence/M7/screenshots/m7a-deployments-1280-light.png` | App → Deployments | 1280 | light | Rows r3 live, r2 Previous release with Roll back, r1 failed with its reason; Redeploy ▾ on the right |
| `tmp/evidence/M7/screenshots/m7a-deployments-375-dark.png` | App → Deployments | 375 | dark | Same rows stacked as cards, actions under each row, no clipping |
| `tmp/evidence/M7/screenshots/m7a-rollback-dialog-1280-dark.png` | Rollback dialog | 1280 | dark | "Roll back shop to r2?", four bullets, env differences (~ API_KEY, + NEW_FLAG, − OLD_FLAG), amber revoked-secret note |
| `tmp/evidence/M7/screenshots/m7a-rollback-dialog-375-light.png` | Rollback dialog | 375 | light | Same dialog fitting the narrow screen |
| `tmp/evidence/M7/screenshots/m7a-redeploy-commit-error-1280-light.png` | Redeploy dialog | 1280 | light | "A specific commit" with the server's field error under Commit SHA; free memory line |
| `tmp/evidence/M7/screenshots/m7a-redeploy-memory-1280-dark.png` | Redeploy dialog | 1280 | dark | Memory refusal (300 MiB available, 512 MiB needed); Deploy disabled |

The UI journey uses mocked API responses (like the M6 journeys); the real path runs on the VM.

### How it works

Deploy dialog → `GET /api/apps/{id}/memory-check` → `POST /api/apps/{id}/deploy {commit?}` → `deploy.Engine`
queue → build → `startAndSwitch` (unit with `RunInit`, health, three route writes proven through Traefik on
`127.0.0.1:443`) → `FinishSwitch` → 10 s drain → stop old → retention. Rollback dialog →
`GET /rollback-preview` (env names only) → `POST /rollback` → `Engine.Rollback` (no build, the release's image and
env version) → same `startAndSwitch`. Restart → `reconciler.recoverSwitches` reads the route file first.
Sequence and states: `docs/ARCHITECTURE.md` §5.4–§5.6, §6.1.

### Code tour

1. `internal/store/migrations/0005_m7_releases.sql`, `internal/store/releases.go`: env versions, `FinishSwitch`, one deployment column list.
2. `internal/deploy/release.go`: `RuntimeConfig`, `ContainerSpecFor` (shared with the reconciler).
3. `internal/deploy/switch.go`: memory check, `switchTraffic` (DEC-33), retention, legacy conversion.
4. `internal/deploy/deploy.go`: `runOne` → `startAndSwitch`, `failRelease`, `Rollback`.
5. `internal/traefik/render.go`: verify-ack middleware, `LiveRelease`.
6. `internal/reconciler/switch.go`: `recoverSwitches`, `writeAppRoute`, `unitSpecFor`.
7. `internal/server/rollback.go`: rollback, preview, memory check, commit validation.
8. `ui/src/lib/components/RollbackDialog.svelte`, `RedeployDialog.svelte`, `ui/src/routes/apps/[id]/+page.svelte`.

### Decisions & deviations

- DEC-32 (env versions), DEC-33 (switch proof), DEC-34 (`RunInit`), DEC-35 (memory check, rollback refusal,
  retention, crash recovery, reconciler unit fix) are in `docs/ARCHITECTURE.md` §7.
- Negative controls were temporary source edits from backup copies; each was reverted and the tests re-run
  green (`grep -c 'NEGATIVE CONTROL'` = 0 afterwards).
- rc.1 sent the verify request to `127.0.0.1:443`; that failed on the VM while a certificate was pending
  (see "M7a on the VM" below). rc.2 uses the internal `verify` entrypoint (DEC-33).
- The redeploy dialog had no mockup; its copy and layout follow the rollback dialog (open question in the report).
- Requests longer than the 10 s drain on the old release are cut (PRD `DEP-1` grace).

### Suggested commits (`tmp/evidence/M7/logs/m7a-check-commits.log`)

```text
PASS static checks: 8 commits, 43 paths
PASS commit 1: feat(store): add env versions and the rolled_back status
PASS commit 2: feat(quadlet): run app containers with an init process
PASS commit 3: feat(traefik): echo the verify token and read the live release
PASS commit 4: feat(deploy): switch traffic with proof, roll back, keep 5 releases
PASS commit 5: fix(reconciler): recover interrupted switches and render real units
PASS commit 6: feat(server): add rollback, preview, memory check and commit redeploy
PASS commit 7: feat(ui): add redeploy and rollback to the Deployments tab
PASS commit 8: docs: record M7a design, decisions and local evidence
```

## M7a on the VM

### rc.1 checks before the run (`v0.7.0-rc.1` = `main` = `8efd4ce`)

| Check | Status | Log (`tmp/evidence/M7/logs/`) |
|---|---|---|
| Release checksums match; a 1-byte-changed copy fails (negative control) | PASS | `rc1-release-checksums.log`, `rc1-release-checksums-negative.log` |
| Revert to `fresh` (announced), cold start, `on_reboot=restart` | PASS | `rc1-vm-revert.log` |
| Installer from the release, first-run setup | PASS | `rc1-vm-install.log`, `rc1-vm-drive.log` |
| UI smoke: 29 checks, schema version 5, no requirement codes | PASS | `rc1-vm-smoke-ui.log` |
| Traefik reachable on `127.0.0.1:443` from the daemon for a domain without a certificate | FAIL | `rc1-traefik-loopback.log`, `rc1-snistrict-proof.log` |

**Finding (rc.1).** Traefik listens on `*:443`, but its default TLS options set `sniStrict: true`. For a
router whose domain has no certificate yet (DNS not propagated, ACME pending), the handshake ends with
`tlsv1 unrecognized name` (curl exit 35, HTTP 000). The switch proof used that path, so every redeploy of
such an app would fail with "Traefik did not route to the new release" (the old release keeps serving).

### rc.2 fix (owner-approved): internal verify entrypoint

Traefik's static config gains a plain-HTTP entrypoint `verify` on `:8099`. Traefik binds it inside its
container; it is not socket-activated and not published on the host or in the firewall. The verify router
listens only there (no TLS). The deploy engine runs `dermaga probe -host <primary> -verify-token <t>
http://traefik:8099<path>` in the rootless netns, like the health probe. A binary upgrade applies it: the
startup reconciler rewrites the outdated dermaga-owned `traefik.yml` and restarts Traefik once; the next start
changes nothing.

| Check | Status | Log (`tmp/evidence/M7/logs/`) |
|---|---|---|
| Before writing code, on the VM: an entrypoint on `:8099` in a throwaway Traefik container on `web` (same image digest) answers `dermaga probe` from the rootless netns (HTTP 200); port 8098 refused (negative control); nothing listens on host port 8099; `traefik` resolves by name in the netns, and its socket-activated `:80` is refused there, which is why the verify entrypoint must be bound by Traefik itself | PASS | `rc2-netns-verify-precheck.log` (exit 1 comes from the final leftover count `grep -c` = 0) |
| `TestVerifyThroughTraefik_NetnsProbe_DEP1`, `TestCheckVerifyResponse_DEP1`, `TestACMEOnEveryRouter` (verify router only on `verify`, no TLS), `TestReconcile_UpgradeAddsVerifyEntrypoint_DEP1` (rc.1 config rewritten, one Traefik restart, second pass none) | PASS | `rc2-verify-negative-controls.log` |
| Negative controls: probe on 443, 502 accepted, verify router on `websecure`, reconciler keeps an existing `traefik.yml`: each makes its test FAIL; restored and green | PASS | `rc2-verify-negative-controls.log` |
| `make check` | PASS | run locally before the suggested commits |

### After the tag (`v0.7.0-rc.2` = `main` = `a45d968`, CI and Release green)

| Check | Status | Log (`tmp/evidence/M7/logs/`) |
|---|---|---|
| Release checksums match; the binary with 1 byte appended fails (negative control) | PASS | `rc2-release-checksums.log`, `rc2-release-checksums-negative.log` |
| Upgrade rc.1 → rc.2 per SETUP §6.3 (checksum-checked binary, atomic rename, daemon restart; no installer). `traefik.yml` diff adds only `verify :8099`; reconciler "1 file(s) re-rendered"; exactly one Traefik restart (new InvocationID, one "Started traefik"); `:8099` LISTEN inside the container (`/proc/net/tcp6`), netns probe HTTP 404; nothing on host port 8099 (loopback refused, LAN timeout); 80/443 still socket-activated; GitHub `connected` before and after | PASS | `rc2-vm-upgrade.log` |
| Second daemon start: Traefik InvocationID unchanged, 0 Traefik starts | PASS | `rc2-vm-upgrade.log` |
| Pebble (`setup_pebble.sh`, now also removes dermaga's marker from `traefik.yml`, owner choice) | PASS | `rc2-vm-pebble.log` |
| Normal redeploy r1 → r2 under `oha -q 50 -c 10 -z 150s`: 7501 × 200, 0 errors; r1 drained and stopped | PASS | `m7a-vm-redeploy-normal.log` |
| Redeploy of the broken-`/health` commit `a9e46d8` ("A specific commit"): r3 failed "HTTP health check /health timed out after 30s", r2 stays live, candidate container, unit and image removed; 7501 × 200, 0 errors | PASS | `m7a-vm-redeploy-broken-health.log` |
| Redeploy after an env change (`GREETING=v2`, build-time `BUILD_VAR`): `/version` changes at 08:02:50.856 with 0 non-200; 6001 × 200, 0 errors | PASS | `m7a-vm-redeploy-new-env.log` |
| Rollback r4 → r2 (new release r5): POST 08:04:48.502 → r2's image serving at 08:04:55.345 (6.8 s; API started→finished 7 s); r5 runs image `testapp:r2` with `GREETING=v1`, r4 `rolled_back`; 4501 × 200, 0 errors | PASS | `m7a-vm-rollback.log` |
| `kill -9` of the daemon at switch write 2 (main → r6, verify router present): restart after 3 s, "switches recovered: [testapp: r6 live (switch completed)]", 1 verify router cleaned, 1 live release, only `app-testapp-r6` running; 7501 × 200, 0 errors | PASS | `m7a-vm-kill-midswitch.log` |
| `seed.sh` (M7): daemon, Traefik, Pebble, panel; one live release per app, no verify router, containers only for live releases | PASS | `m7a-vm-seed.log` |
| `m3_check.sh access` last (VM-side unban), bans 0 → 1, new login after unban | PASS | `m7a-vm-m3-access.log` |
| New `ssh dermaga` (owner config) 30 s after the access check | PASS | `m7a-vm-new-ssh-dermaga.log` |

oha runs from the workstation: `oha -q 50 -c 10 -z <d> -w --cacert <Pebble root> --connect-to
testapp.dermaga.test:443:192.168.122.217:443 https://testapp.dermaga.test/`; a parallel `/version` curl loop (2/s)
shows when traffic moves. Script: `tmp/evidence/M7/m7a-oha-run.sh`; the kill watcher is
`tmp/evidence/M7/m7a-kill-midswitch.sh` (VM-side, reads the route file every 10 ms).

### Findings on the VM (rc.2) and owner decisions

1. After the crash recovery the stopped release's env file (`testapp-r5.env`, mode 0600, holds runtime
   secrets) stayed on disk: the reconciler's orphan removal did not delete it, unlike the engine's
   `stopRelease`. **Fixed in rc.3.**
2. A release adopted after a crash had no build log: logs were written to the DB only when the release ended.
   **Fixed in rc.3.**
3. On recovery the reconciler stopped the previous release without the 10 s drain. **Fixed in rc.3.**
4. A 2-character secret (`BUILD_VAR=b2`) was masked inside unrelated text: the commit SHA in the build log
   showed `…b9ff[MASKED]`. **Fixed in rc.3 (DEC-36).**
5. Traefik logs a warning on each start: "Unable to use socket activation for entrypoint verify"; it then
   binds `:8099` itself. **Accepted; documented in ARCHITECTURE DEC-33 and SETUP §6.3.**
6. In M6 the daemon restart test ran after `setup_pebble.sh` had edited the dermaga-owned `traefik.yml`; that
   restart re-rendered it back to Let's Encrypt. **Recorded as an unverified gap in M6 EVIDENCE; not re-run.**

### Notes

- The panel password on the VM did not match the stored one (changed while connecting GitHub); after 4
  failed logins it was reset from `tmp/evidence/M7/vm-panel-pass.txt` with `dermaga admin set-password` (owner
  choice), which revoked all sessions.
- The first redeploy's `/version` did not change: the build cache reused the layer with the build date.
  The env-change redeploy busts that cache and shows the moment of the switch.

## M7a fixes for rc.3 (local part)

Owner decisions on the rc.2 findings, all before M7b.

| Criterion | Status | Evidence (`tmp/evidence/M7/logs/`) |
|---|---|---|
| Orphan units go with the engine's cleanup (`deploy.RemoveReleaseUnit`: stop, unit, env file); a candidate's env file goes at once (`TestReconcile_CrashBeforeMainSwitched_DEP10`) | PASS | `rc3-make-check.log` |
| Sweep of other per-release files: stray env files without a live or draining unit removed (`TestReconcile_StrayEnvFilesRemoved_DEP10`); image of a release interrupted by the restart removed at engine start, never `image_ref` (a rollback's is the restored image) (`TestStart_RemovesImagesOfInterruptedReleases`); route file and verify routers were already covered (orphan route removal, `recoverSwitches`, `-verify.yml` cleanup) | PASS | `rc3-make-check.log` |
| Log saved at every stage line and every 2 s, so the DB holds it at the crash point (`TestReleaseLog_SavedUpToSwitch_DEP10`: at each verify step the DB log has "Starting release r2", then "Traefik reaches r2") | PASS | `rc3-make-check.log` |
| Recovery drains a release that lost its traffic for 10 s, then stops it and removes its unit and env file; the live release keeps its env file (`TestReconcile_CrashAfterMainSwitched_DEP10`, push and rollback) | PASS | `rc3-make-check.log` |
| Values under 4 characters (runes) not masked; 4+ still masked, also split across stream chunks (`TestMasker_ShortValuesNotMasked_SEC9`, the `b2` SHA line) | PASS | `rc3-make-check.log` |
| Env editor hint under a 1–3 character value, linked with `aria-describedby`; gone at 4 characters and when empty; desktop + narrow, light + dark, axe AA (`ui/e2e/env-mask.test.ts`) | PASS | `rc3-test-browser.log` (11 passed) |
| Negative controls: env sweep off; orphan path keeps the env file with the sweep off; no drain; stage lines not saved; short values masked again; `d.ImageRef` removed at start; interrupted image kept; hint never shown: each makes its test FAIL; files restored byte for byte and green | PASS | `rc3-negative-controls.log` |
| `seed.sh` invariant check (`test/vm/m7_invariants.py`) against a correct state and four broken ones (two live releases, verify router left, previous release running, live image missing) | PASS | `rc3-seed-invariants-control.log` |
| `make check` | PASS | `rc3-make-check.log` |
| VM checks on `v0.7.0-rc.3` | PASS | see "M7a on the VM, rc.3" |

Screens (`tmp/evidence/M7/screenshots/`):

- `m7a-env-short-value-1280-light.png`, `-1280-dark.png`: app Environment tab in edit mode, desktop; under the
  value of `BUILD_VAR` (two dots) the amber line "Shorter than 4 characters: this value is not masked in build
  and runtime logs."
- `m7a-env-short-value-375-light.png`, `-375-dark.png`: the same at 375 px; the hint wraps to two lines under
  the field, the Runtime/Build time row below it.

Deviation: the rollback exception first written in the engine's start-up image cleanup was dead code (a
rollback never builds, so the tag removed for it never exists); the negative control showed it and it was
removed. The control now targets the real risk (removing `image_ref`).

## M7a on the VM, rc.3 (`v0.7.0-rc.3` = `main` = `233d4b4`, CI and Release green)

The `v0.7.0-rc.3` tag is lightweight (the suggested command was `git tag`, not `git tag -a`); from rc.4 on the
tag commands are annotated (owner request).

| Check | Status | Log (`tmp/evidence/M7/logs/`) |
|---|---|---|
| Release checksums match; the binary with 1 byte appended fails (negative control) | PASS | `rc3-release-checksums.log`, `rc3-release-checksums-negative.log` |
| Upgrade rc.2 → rc.3 per SETUP §6.3 (no installer): rc.3 running, Traefik not restarted (same InvocationID; `traefik.yml` unmarked since Pebble), GitHub `connected`, app 200; the startup sweep removed the stale `testapp-r5.env` left by the rc.2 crash test ("1 orphan(s) removed [testapp-r5.env]") | PASS | `rc3-vm-upgrade.log` |
| `kill -9` at switch write 2 (main → r7, verify router present) under `oha -q 50 -c 10 -z 150s`: 7501 × 200, 0 errors; "switches recovered: [testapp: r7 live (switch completed)]", 1 verify router cleaned, one live release | PASS | `rc3-vm-kill-midswitch.log` |
| Drain observed: "Draining testapp r6 for 10s" → "Stopped testapp r6 after the drain" 10.3 s apart; 1 s samples show `app-testapp-r6` running and `testapp-r6.env` present until 02:08:15, both gone at 02:08:16 | PASS | `rc3-vm-kill-midswitch.log` |
| Log up to the crash: r7's log in the DB has every stage line up to "==> Traefik reaches r7 through the verify router" (the last stage before write 2); the commit SHA is no longer masked (`…b9ffb2`, DEC-36) | PASS | `rc3-vm-kill-midswitch.log` |
| Rollback r7 → r2 (new release r8): POST 09:10:38.695 → r2's image serving at 09:10:45.518 (6.8 s); r8 runs image `testapp:r2` with `GREETING=v1`, only `testapp-r8.env` left; 4501 × 200, 0 errors | PASS | `rc3-vm-rollback.log` |
| `m7_invariants.py`: one live release (r8), no verify router, containers only for live releases; images kept for r7, r6, r5, r4, r2 (retention: 5) | PASS | `rc3-vm-invariants.log` |
| `m3_check.sh access` last (VM-side unban), bans 1 → 2, new login after unban | PASS | `rc3-vm-m3-access.log` |
| New `ssh dermaga` (owner config) 30 s after the access check | PASS | `rc3-vm-new-ssh-dermaga.log` |

Excerpt (`tmp/evidence/M7/logs/rc3-vm-kill-midswitch.log`):

```text
02:08:02.746 SIGKILL dermaga pid 48101 at write 2: main -> http://app-testapp-r7:8080, verify router present
2026-11-27T02:08:05+00:00 dermaga-vm dermaga[49424]: Draining testapp r6 for 10s before stopping it (it lost its traffic before the restart)
2026-11-27T02:08:05+00:00 dermaga-vm dermaga[49424]: Startup reconciliation complete: 0 orphan(s) removed [], 0 file(s) re-rendered, 0 unit(s) started, 1 verify router(s) cleaned, switches recovered: [testapp: r7 live (switch completed)], draining: [testapp r6]
02:08:15 running: ['app-testapp-r6', 'app-testapp-r7'] env files: ['testapp-r6.env', 'testapp-r7.env']
02:08:16 running: ['app-testapp-r7'] env files: ['testapp-r7.env']
2026-11-27T02:08:16+00:00 dermaga-vm dermaga[49424]: Stopped testapp r6 after the drain
drain duration: 10.3 s
RESULT rc3-kill-midswitch: oha non-2xx=0 connection-errors=0 /version non-200=0 release=active
```

## M7b: push-to-deploy (local part, for `v0.7.0-rc.4`)

Commit under review: `c14b84a` + the working tree of the suggested commits below (dirty: not yet committed by the
owner). Requirement IDs: `APP-8`, `DEP-5`, `UI-7` (`REL-6` and the real-push journey are the VM part, after the tag).
Owner answers on the plan (2026-11-27) and the GitHub documentation behind them are recorded in DEC-37.

### Acceptance criteria

| Criterion | Status | Log (`tmp/evidence/M7/logs/`) | Excerpt |
|---|---|---|---|
| APP-8: a push to the app's branch queues a `push` release of that commit; Activity says so | PASS | `m7b-go-push-tests.log` | `TestHandlePush_DeploysMatchingPush_APP8` |
| APP-8: other branch, tag, deleted branch, deploy on push off, no matching watch path → no deployment, plain reason in Activity | PASS | `m7b-go-push-tests.log` | `TestHandlePush_IgnoredPushes_APP8` (5 subtests) |
| APP-8 negative control: a matching file, or no file list at all, deploys | PASS | `m7b-go-push-tests.log` | `TestHandlePush_MatchingPathOrNoFileListDeploys_APP8` |
| Same commit queued/building/live is ignored (replay under a new delivery ID); another commit, and the old one once not live, deploy | PASS | `m7b-go-push-tests.log` | `TestHandlePush_SameCommitDeduped_APP8` |
| DEP-5 by pushes: A building, B queued, C supersedes B, A goes live, then C | PASS | `m7b-go-push-tests.log` | `TestHandlePush_QueueKeepsNewest_DEP5` |
| Unknown repository leaves no deployment and no Activity entry | PASS | `m7b-go-push-tests.log` | `TestHandlePush_UnknownRepoRecordsNothing` |
| Webhook: signed push → one `Push` with ref, commit, message and files; bad signature → 401 and no call; same delivery ID (GitHub Redeliver) → 202, no second call | PASS | `m7b-go-push-tests.log` | `TestWebhookPush_OnPush_APP8` |
| Webhook: no commits, a commit without file lists, 2048 commits → files unknown (deploy anyway); 2047 commits are read (negative control) | PASS | `m7b-go-push-tests.log` | `TestWebhookPush_UnknownFilesAreNil_APP8` |
| Watch paths: matching rules and server-side validation (leading `/`, `..`, `//`, bad pattern, > 256 chars, > 50 lines) | PASS | `m7b-go-push-tests.log` | `TestWatchPathsMatch_APP8`, `TestNormalizeWatchPaths_APP8` |
| API: `PATCH /api/apps/{id}` 401 without a session; bad watch paths 400 with plain wording (no requirement code) and nothing saved; saved trimmed; omitted fields unchanged | PASS | `m7b-go-push-tests.log` | `TestAppsAPI_AutoDeployAndWatchPaths_APP8` |
| API: Settings → GitHub status carries the newest ignored push | PASS | `m7b-go-push-tests.log` | `TestGitHubStatus_LastIgnoredPush_APP8` |
| UI-7: a push-started deployment appears without reload; settings edits survive a poll; Deploy on push form, error and save; rollback dialog notice on, absent when off (negative control); last ignored push in Settings → GitHub; axe AA at 1280/375, light and dark | PASS | `m7b-test-browser.log` | `13 passed (34.9s)` (whole suite) |
| UI-7 negative control: with polling disabled the live-status test fails | PASS | `m7b-browser-polling-negative.log` | below |
| `make check` | PASS | `m7b-make-check.log` | below |
| Real pushes through the GitHub App via gosmee, `oha` 50 req/s during a push-triggered redeploy and a failing one, 0 leftovers after removal | NOT RUN | — | VM part after the owner tags `v0.7.0-rc.4` (owner steps for the smee channel and the App's webhook URL are given then) |

`m7b-go-push-tests.log` (`go test -race -count=1 -v -run 'Push|WatchPaths|APP8' ./internal/deploy/ ./internal/webhook/ ./internal/server/`):

```text
--- PASS: TestWatchPathsMatch_APP8 (0.00s)
--- PASS: TestNormalizeWatchPaths_APP8 (0.00s)
--- PASS: TestHandlePush_DeploysMatchingPush_APP8 (0.26s)
--- PASS: TestHandlePush_IgnoredPushes_APP8 (1.26s)
--- PASS: TestHandlePush_MatchingPathOrNoFileListDeploys_APP8 (0.49s)
--- PASS: TestHandlePush_UnknownRepoRecordsNothing (0.23s)
--- PASS: TestHandlePush_SameCommitDeduped_APP8 (0.28s)
--- PASS: TestHandlePush_QueueKeepsNewest_DEP5 (0.33s)
ok  	dermaga/internal/deploy	3.883s
--- PASS: TestWebhookPush_OnPush_APP8 (0.15s)
--- PASS: TestWebhookPush_UnknownFilesAreNil_APP8 (0.38s)
ok  	dermaga/internal/webhook	1.546s
--- PASS: TestAppsAPI_AutoDeployAndWatchPaths_APP8 (0.30s)
--- PASS: TestGitHubStatus_LastIgnoredPush_APP8 (0.28s)
ok  	dermaga/internal/server	1.647s
```

`m7b-browser-polling-negative.log`:

```text
    Error: expect(locator).toHaveCount(expected) failed
    Expected: 3
    Received: 2
  1 failed
NEGATIVE CONTROL OK: live status test fails without polling
```

`m7b-make-check.log`:

```text
ok   ui
ok   lint
ok   test
ok   vulncheck
ok   build-all
ok   policy
ok   shellcheck
=== make check passed successfully ===
```

Browser tests ran with a temporary Playwright config on port 7071 (not committed): the owner's `ssh dermaga`
session forwards `127.0.0.1:7070` to the VM panel, and the test daemon could not bind it. `ui/e2e/panel.test.ts`
compared full URLs with port 7070; it now checks paths, so the suite runs on either port. `make test-browser`
(port 7070) is unchanged.

### Screens

Captured by `ui/e2e/push.test.ts` with `EVIDENCE_MILESTONE=M7`; every file opened. Full-page captures show the
sticky sidebar where it was when the page scrolled (seen in earlier milestones too).

| Local file (`tmp/evidence/M7/screenshots/`) | Page | Width | Theme | Shows |
|---|---|---|---|---|
| `m7b-settings-push-1280-light.png` | App → Settings | 1280 | light | Deploy on push card under Build: checked toggle naming `main`, watch paths `web/` + `Containerfile`, help text with example; the red server error for `/etc/` above the form |
| `m7b-settings-push-1280-dark.png` | App → Settings | 1280 | dark | Same in dark, readable help text and error |
| `m7b-settings-push-375-light.png` | App → Settings | 375 | light | Cards stacked full width, help text wraps, Save settings below |
| `m7b-settings-push-375-dark.png` | App → Settings | 375 | dark | Same in dark |
| `m7b-rollback-push-notice-1280-light.png` | Rollback dialog | 1280 | light | "Roll back shop to r1?" with the fifth bullet "The next push to main will deploy over this rollback." |
| `m7b-rollback-push-notice-1280-dark.png` | Rollback dialog | 1280 | dark | Same in dark |
| `m7b-rollback-push-notice-375-light.png` | Rollback dialog | 375 | light | Dialog fits the width; behind it the push-started r3 row ("building", "push", `3333abc — change 3`) |
| `m7b-rollback-push-notice-375-dark.png` | Rollback dialog | 375 | dark | Same in dark |
| `m7b-github-ignored-push-1280-light.png` | Settings → GitHub | 1280 | light | App card: Last delivery, then "Last ignored push: Push to dev (abc1234) ignored for shop: the app deploys from main." with time |
| `m7b-github-ignored-push-1280-dark.png` | Settings → GitHub | 1280 | dark | Same in dark |
| `m7b-github-ignored-push-375-light.png` | Settings → GitHub | 375 | light | Reason wraps inside the card |
| `m7b-github-ignored-push-375-dark.png` | Settings → GitHub | 375 | dark | Same in dark |

### How it works

```mermaid
sequenceDiagram
  participant GH as GitHub
  participant T as Traefik + hook-relay
  participant W as webhook (hooks.sock)
  participant E as deploy.Engine
  participant S as SQLite
  participant UI as App page
  GH->>T: POST /hooks/github-app (push)
  T->>W: bytes, unparsed
  W->>W: HMAC check, then parse; rate limit
  W->>S: record delivery ID (duplicate → 202, stop)
  W->>E: HandlePush(Push{ref, sha, message, deleted, files})
  E->>S: apps of the repository
  loop every app
    E->>E: branch? deleted? auto-deploy? watch paths? same commit current?
    alt ignored
      E->>S: Activity push_ignored (plain reason)
    else deploy
      E->>S: Enqueue(push, sha) (DEP-5 supersede) + Activity push_deploy
    end
  end
  W-->>GH: 202 (same for unknown repositories)
  UI->>S: GET /api/apps/{id} every 3 s while visible
```

The queued release then follows the M7a flow (build, start next to the live release, verified switch, drain).

### Code tour

1. `internal/webhook/webhook.go`: `Push`, `maxPushCommits`, and the `push` case building the file list.
2. `internal/deploy/push.go`: `HandlePush`, the per-app decision in one `switch`, Activity wording.
3. `internal/deploy/watchpaths.go`: `NormalizeWatchPaths` (validation) and `watchPathsMatch`.
4. `internal/store/releases.go` `HasCurrentDeploymentOfCommit`; `internal/store/apps.go` `ListAppsByRepo`; `internal/store/store.go` `AuditFilter.Event`.
5. `cmd/dermaga/main.go`: `OnPush: depEng.HandlePush`.
6. `internal/server/apps.go` (`handleUpdateAppSettings`, `handleGetApp`), `internal/server/github.go` (last ignored push), `internal/server/types.go`.
7. `ui/src/routes/apps/[id]/+page.svelte`: `pollApp`, the Deploy on push card; `ui/src/lib/components/RollbackDialog.svelte` (`pushBranch`); `ui/src/lib/components/GitHubSettings.svelte`.
8. Tests: `internal/deploy/push_test.go`, `internal/webhook/webhook_test.go`, `internal/server/apps_test.go`, `ui/e2e/push.test.ts`.

### Decisions & deviations

- DEC-37 (owner decisions and the GitHub documentation quotes).
- No migration: `apps.auto_deploy` and `apps.watch_paths` already existed; the ignored-push reason lives in Activity.
- `HandlePush` holds a mutex across the duplicate check and the enqueue, so two deliveries of one commit cannot both pass.
- An Activity entry is written for every ignored push of a repository an app uses, also for other branches; this
  can be frequent on busy repositories (owner asked for every ignored push with its reason).

### Suggested commits (`tmp/evidence/M7/logs/m7b-check-commits.log`)

```text
PASS static checks: 5 commits, 24 paths
PASS commit 1: feat(deploy): deploy GitHub pushes to the app's branch (log: .check-logs/commits/commit1.log)
PASS commit 2: feat(api): save deploy on push settings and the last ignored push (log: .check-logs/commits/commit2.log)
PASS commit 3: feat(ui): add deploy on push settings and live deployment status (log: .check-logs/commits/commit3.log)
PASS commit 4: test(ui): check panel journey URLs by path (log: .check-logs/commits/commit4.log)
PASS commit 5: docs: record the M7b push-to-deploy design and local evidence (log: .check-logs/commits/commit5.log)
```

## M7b on the VM (`v0.7.0-rc.4` = `main` = `2bbb6ee`, annotated tag, CI and Release green)

Binary: the published release, upgraded per SETUP §6.3. Real deliveries: the owner pointed the GitHub App's webhook
URL at a fresh smee channel (secret unchanged); `gosmee` 0.32.1 ran on the workstation with
`--noReplay --exec tmp/evidence/M7/gosmee-forward.sh` (since moved to `test/vm/gosmee-forward.sh`, unchanged in behavior), which sends each body byte for byte to
`https://hooks.dermaga.test/hooks/github-app` (`curl --resolve … --cacert <Pebble root>`), so deliveries take the
public path: Traefik 443 → hook-relay → `hooks.sock`. The smee URL is in no log (`gosmee-client.log` has only
`<smee-url>`; its one `smee.io` line is gosmee's `/version` probe). The owner did every push to
`r3p-dev/dermaga-test-app`.

| Check | Status | Log (`tmp/evidence/M7/logs/`) |
|---|---|---|
| Release: checksums match, binary reports `v0.7.0-rc.4 (commit 2bbb6ee)`; 1 byte appended fails (negative control) | PASS | `rc4-release-checksums.log`, `rc4-release-checksums-negative.log` |
| Upgrade rc.3 → rc.4 (no installer): rc.4 active, Traefik not restarted (same InvocationID), GitHub connected, app 200 | PASS | `rc4-vm-upgrade.log` |
| `make test-browser` on port 7070 (owner's tunnel closed): 13 passed | PASS | `rc4-make-test-browser-7070.log` |
| Forwarder reaches the daemon: an unsigned delivery gets 401 and "signature mismatch" | PASS | `rc4-vm-forwarder-preflight.log` |
| Byte-exact: the README-only push (`6ad9e9b`) → 202, no warning, "Push to main (6ad9e9b) ignored for testapp: no changed file matches the watch paths.", no release; the same body with 1 byte changed → 401 (negative control) | PASS | `rc4-vm-byte-exact.log` |
| Push B (`Containerfile` change) → r9 live 9.3 s after it appeared, verified switch, 10 s drain; `oha` 50 req/s 150 s: 7501 × 200, 0 errors; `/version` 0 non-200 | PASS | `rc4-vm-push-journey.log`, `oha-rc4-B.*` |
| Push C (no `/health`) → r10 failed ("HTTP health check /health timed out after 30s"), r9 stays live; `oha`: 7501 × 200, 0 errors | PASS | `rc4-vm-push-journey.log`, `oha-rc4-C.*` |
| Push D (revert C) → r11 live; `oha`: 7501 × 200, 0 errors | PASS | `rc4-vm-push-journey.log`, `oha-rc4-D.*` |
| Push E to `dev` → "Push to dev (4b00e59) ignored for testapp: the app deploys from main.", no release | PASS | `rc4-vm-push-journey.log` |
| GitHub Redeliver of push B: same delivery ID and body hash, 202, still 1 Activity entry for it, no release | PASS | `rc4-vm-redeliver.log` (the journey script's own step failed on a wrong guess of the audit API shape and was stopped; fixed in the script, checked separately) |
| Remove the app (typed name; wrong name → 400): unit, env file, route, container, 5 images, mirror, API entry gone, route 404 | PASS | `rc4-vm-delete-app.log` |
| 0 leftovers after removal (rc.4) | **FAIL** (fixed in rc.5, PASS there) | `rc4-vm-delete-app.log`: `~/.cache/dermaga/builds/testapp` (empty folder the builder leaves) and 2 `.dryrun-dep_*` folders from the M7a `kill -9` runs (a crash skips the dry-run's deferred cleanup). Fixed for rc.5 (below); VM proof NOT RUN until then |
| Demo state restored: testapp recreated (documented demo env `GREETING=v1`, `BUILD_VAR=b2`; watch paths `Containerfile`), r1 live, `seed.sh` M7 invariants hold | PASS | `rc4-vm-restore-demo.log`, `rc4-vm-demo-recheck.log` |
| `m3_check.sh access` last (VM-side unban): bans 2 → 4, new login after unban | PASS | `rc4-vm-m3-access.log` |
| New `ssh dermaga` (owner config) 30 s after the access check | PASS | `rc4-vm-new-ssh-dermaga.log` |

Excerpt (`tmp/evidence/M7/logs/rc4-vm-push-journey.log`, push B):

```text
10:14:00.082 B: release r9 (dep_b98d0b071ad83193c58c562b) appeared; oha -q 50 -c 10 -z 150s starts now
10:14:09.341 B: r9 status active (appeared -> terminal: 9.3 s)
==> Traefik reaches r9 through the verify router
==> Traffic switched from r8 to r9
==> Draining r8 for 10s before stopping it
==> Deployment is live
2026-11-27T00:55:49Z 200  first=10:14:00.111  last=10:14:08.534
2026-11-27T03:14:01Z 200  first=10:14:09.059  last=10:16:29.689
oha: 150.003504076 s, 50.00549851288528 req/s, requests 7501, codes {"200":7501}, errors {}
RESULT B: oha non-2xx=0 connection-errors=0 /version non-200=0 release=active (expected active)
RESULT C: oha non-2xx=0 connection-errors=0 /version non-200=0 release=failed (expected failed)
RESULT D: oha non-2xx=0 connection-errors=0 /version non-200=0 release=active (expected active)
RESULT E: newest release r11 (expected r11, no deployment for dev)
```

Excerpt (`tmp/evidence/M7/logs/rc4-vm-redeliver.log`):

```text
2026-11-27T10:13:58+07:00 event=push delivery=95f3ce28-ea10-11f1-9a2d-9a6caad114ce bytes=7832 sha256=8b5964a308f0 status=202
2026-11-27T10:28:21+07:00 event=push delivery=95f3ce28-ea10-11f1-9a2d-9a6caad114ce bytes=7832 sha256=8b5964a308f0 status=202
2026-11-27T03:13:58Z push_deploy Push to main (e797640) queued release r9 for testapp.
RESULT R: forwards=2 (2 expected, both 202), Activity entries=1 (1 expected), releases of B=r9:inactive
```

### Finding and fix for rc.5 (local part)

`deploy.Engine.RemoveApp` now also removes `<builds>/<app>`, and `Engine.Start` clears the build scratch folder
(only source exports and Quadlet dry-runs live there; nothing builds before the worker starts). ARCHITECTURE §5.6.

| Check | Status | Log (`tmp/evidence/M7/logs/`) |
|---|---|---|
| `TestStartAndRemoveApp_LeaveNoBuildScratch_APP9`: scratch gone after Start, nothing of the app after RemoveApp; mirrors of another app untouched (negative controls inside) | PASS | `rc5-make-check.log` |
| Negative control: without either fix line the test fails | PASS | `rc5-scratch-negative.log` |
| `make check` | PASS | `rc5-make-check.log` |
| VM: upgrade to rc.5, crash residue swept on start, remove + recreate the app with 0 leftovers | PASS | see § rc.5 on the VM |

### Notes

- Right after the recreated app's first release went live, one HTTPS `/health` request got 404; a second later it
  was 200. A first deploy has no previous route and no switch proof (DEP-3 path), so this is Traefik loading the
  new file; nothing was served before. Not a zero-downtime case.
- Of two sudo password files in `tmp/evidence/M5/`, `vm-sudo-pw` is wrong (one failed sudo attempt on the VM,
  not an SSH failure); `vm-sudo-pass.txt` is the one `m3_check.sh` needs.
- Owner cleanup done (2026-11-27): `dev` deleted, App webhook URL back to `https://hooks.dermaga.test/hooks/github-app`, `tmp/smee-url` removed.

## M7b fix on the VM, rc.5 (`v0.7.0-rc.5` = `main` = `d2f24e6`, annotated tag, CI and Release green)

| Check | Status | Log (`tmp/evidence/M7/logs/`) |
|---|---|---|
| Release: checksums match, binary reports `v0.7.0-rc.5 (commit d2f24e6)`; 1 byte appended fails (negative control) | PASS | `rc5-release-checksums.log`, `rc5-release-checksums-negative.log` |
| Upgrade rc.4 → rc.5 per SETUP §6.3: Traefik not restarted, GitHub connected, app 200; build scratch before: 5 entries (`builds/testapp`, the two M7a `.dryrun-dep_*` folders), after the rc.5 start: 0 | PASS | `rc5-vm-upgrade-sweep.log` |
| Remove the app (typed name; wrong name → 400): 0 leftovers | PASS | `rc5-vm-delete-app.log` |
| Remove again after a release built on rc.5 (`builds/testapp` and the mirror present before): 0 leftovers, so `RemoveApp` itself removes the folder | PASS | `rc5-vm-delete-app-after-build.log` |
| Demo state restored: testapp r1 live (`GREETING=v1`, `BUILD_VAR=b2`, watch paths `Containerfile`), `seed.sh` M7 invariants hold | PASS | `rc5-vm-restore-demo-final.log` |
| Denied-login checks | NOT RUN | not needed: no access code changed since the rc.4 run (owner decision) |

Excerpt (`tmp/evidence/M7/logs/rc5-vm-upgrade-sweep.log`):

```text
=== scratch before
~/.cache/dermaga/builds/testapp
~/.cache/dermaga/builds/.dryrun-dep_25bc1bbd1e2c9ab435fdfb62
~/.cache/dermaga/builds/.dryrun-dep_91c882b1148e7bb7d9e89610
(entries: 5)
dermaga version v0.7.0-rc.5 (commit d2f24e6)
InvocationID=292dc407990e4117a180ea06f3e93faa
=== scratch after the rc.5 start
(entries: 0)
```

The wrong `tmp/evidence/M5/vm-sudo-pw` was deleted (owner request); `vm-sudo-pass.txt` stays.
