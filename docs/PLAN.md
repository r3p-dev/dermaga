# Plan

Milestones in order. **Slow, reviewable progress**: each milestone is small enough to review the
screens, read the code and understand how it works in one sitting. The implementer stops after every
milestone and waits for confirmation (`AGENTS.md` "Milestone loop"). A milestone that grows is split into
checkpoints (`M6a`, `M6b`, …), each with its own stop.

Requirement IDs refer to `docs/PRD/` and `docs/ARCHITECTURE.md` §1.2. In M0 the implementer completes
the **acceptance criteria** (by requirement ID) and adds an **hour estimate** to every milestone below,
and may propose a different split; the plan is final once approved.

**v1.0.0 scope** (`docs/PRD/summary.md` §7): web UI only, GitHub App only, stateless apps, blue-green
deploys, domains with automatic HTTPS, headers and middleware, server care and updates. Databases with
backups come in v1.1.

| #   | Version | Milestone                                         | Status            | Confirmed                                                                                                                                                                       | Hours   |
| --- | ------- | ------------------------------------------------- | ----------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------- |
| M0  | –       | Spikes and plan                                   | completed         | yes: confirmed by owner, 2026-11-17 (next milestone started)                                                                                                                    | 28      |
| M1  | 0.1.0   | Foundation: daemon, login, panel shell            | completed         | yes: confirmed by owner, 2026-11-17 (owner tags v0.1.0–v0.1.4; M2 started)                                                                                                      | 36      |
| M2  | 0.2.0   | Generators and reconciliation                     | completed         | yes: confirmed by owner, 2026-11-17 (owner tags v0.2.0, v0.2.1; M3 started)                                                                                                     | 28      |
| M3  | 0.3.0   | Installer, hardening, lockout guard               | completed         | M3a yes: confirmed by owner, 2026-11-17 (owner tags v0.3.0-rc, v0.3.0-rc.2; M3b started); M3b yes: confirmed by owner, 2026-11-25 (owner tags v0.3.0-rc.3, v0.3.0-rc.4, v0.3.0) | 24      |
| M4  | 0.4.0   | First-run setup and panel access                  | completed         | M4a yes: confirmed by owner, 2026-11-25 (v0.4.0-rc.1); M4b yes: confirmed by owner, 2026-11-25 (owner tags v0.4.0-rc.2 [superseded], v0.4.0-rc.3, v0.4.0) | 20      |
| M5  | 0.5.0   | Connect GitHub                                    | completed         | M5a yes: confirmed by owner, 2026-11-25; M5b yes: confirmed by owner, 2026-11-25 (v0.5.0-rc.1, v0.5.0)                                                                       | 28      |
| M6  | 0.6.0   | First deploy: wizard, build, env, logs            | completed         | yes: confirmed by owner, 2026-11-26 (v0.6.0-rc.1–v0.6.0-rc.3, v0.6.0)                                                                                                          | 36      |
| M7  | 0.7.0   | Zero downtime: push-to-deploy, redeploy, rollback | completed         | yes: confirmed by owner, 2026-11-27 (v0.7.0-rc.1–v0.7.0-rc.5, v0.7.0) | 32      |
| M8  | 0.8.0   | Domains, HTTPS, headers, middleware               | not started       | –                                                                                                                                                                               | 28      |
| M9  | 0.9.0   | Server care: app status, disk, dashboard, doctor  | not started       | –                                                                                                                                                                               | 24      |
| M10 | 0.10.0  | Updating dermaga, migration guide                 | not started       | –                                                                                                                                                                               | 24      |
| M11 | 1.0.0   | Hardening and release                             | not started       | –                                                                                                                                                                               | 32      |
|     |         | **Total**                                         |                   |                                                                                                                                                                                 | **330** |

Hours are the implementer's estimate for building, testing and writing the evidence of each
milestone; review time is not included. **Confirmed** is filled only from a written record of the
owner's confirmation; `?` means no such record exists. The owner confirmed M0–M2 and M3a on
2026-11-17; the tags and the start of each next milestone are the supporting records.

Earlier milestones, so their evidence never needs to be opened to start a session:

- M0: completed (spikes, architecture, setup, plan); evidence in `docs/evidence/M0`.
- M1: completed through v0.1.4 (daemon, login, panel shell, brand and dark theme fixes); evidence in
  `docs/evidence/M1`.
- M2: completed through v0.2.1 (generators, reconciliation, ownership markers, empty-database safe
  default); evidence in `docs/evidence/M2`.
- M3a: done (installer and hardening code, unit and fake-host tests, local checks, fixes after the
  review); evidence in `docs/evidence/M3`.

## Active milestone

**M7** (0.7.0): Zero downtime: push-to-deploy, redeploy, rollback (`completed`, confirmed by owner 2026-11-27; M8 starts in a new session).

- **Requirement IDs:** DEP-1, DEP-2, DEP-4, DEP-10, APP-8, REL-6.
- **Handoff** (2026-11-27, M7 completed: owner tagged `v0.7.0` (`4b37ae2`, CI and Release green) and confirmed M7; the gosmee forwarder now lives in `test/vm/gosmee-forward.sh`; next session: M8 plan first):
  - Done: rc.5 VM run (scratch swept on start, app removed twice with 0 leftovers, demo state restored); CHANGELOG `[0.7.0]`; owner cleanup of the smee run done.
  - Next: owner tags `v0.7.0` and confirms M7; then M8 (domains, HTTPS, headers, middleware).
- **rc.4 handoff** (2026-11-27, M7b VM run on `v0.7.0-rc.4` done):
  - Done: real pushes via smee/gosmee `--exec` (byte-exact proven, 1-byte negative control 401), push → live and failing health under `oha` 50 req/s (0 non-2xx, 0 errors each), other branch and unmatched watch path ignored, GitHub Redeliver deduped, app removed, demo state restored (testapp r1, watch paths `Containerfile`), denied logins last, new `ssh dermaga`; `make test-browser` on 7070.
  - Finding: removal left `builds/<app>` and crash-left `.dryrun-*` folders; fixed locally (RemoveApp + sweep on Start) with tests; needs `v0.7.0-rc.5` and a short VM proof (sweep on start, remove + recreate with 0 leftovers) before the final `v0.7.0`.
  - Owner cleanup: delete the `dev` branch, set the App's webhook URL back to `https://hooks.dermaga.test/hooks/github-app`.
- **Earlier handoff** (2026-11-27, M7b local part done; waiting for the owner's commits and the `v0.7.0-rc.4` tag before the VM run):
  - Done (M7b local): `webhook.Push` → `deploy.Engine.HandlePush` (branch, deleted ref, auto-deploy, watch paths, same-commit dedupe, DEP-5 queue), ignored pushes in Activity and under "Last ignored push" in Settings → GitHub, `auto_deploy`/`watch_paths` on `PATCH /api/apps/{id}` with server-side validation, Deploy on push section in the Settings form (owner choice A), 3 s polling while the tab is visible, rollback dialog push notice; unit, webhook, API and browser tests with negative controls; `make check` green. No migration. DEC-37.
  - Owner answers (2026-11-27): DEP-5 as is; ignored pushes in Activity + Settings → GitHub; auto-deploy stays on after rollback, with the dialog notice; dedupe same SHA queued/running/live; polling 3 s; Settings layout A; gosmee `--exec` + `curl --resolve --cacert`, stop and ask if the body is not byte-exact; release as `v0.7.0-rc.4`.
  - Next: owner commits and tags `v0.7.0-rc.4`; then the VM run: a new smee channel and the App's webhook URL (owner steps, exact commands given then), gosmee `--exec` forwarder verified byte-exact, real pushes via `r3p-dev/dermaga-test-app` (main → live with `oha` 50 req/s, failing health with `oha`, another branch ignored, unmatched watch path ignored), 0 leftovers after removing the app, denied logins last.
- **Previous handoff** (2026-11-27, M7a done on `v0.7.0-rc.3`):
  - Done: M7a code; rc.1 finding (`sniStrict`) fixed in rc.2; rc.2 VM run; rc.3 fixes for the rc.2 findings
    (recovery cleanup shared with the engine, stray env files and interrupted images swept, log saved during
    the release, 10 s drain on recovery, DEC-36 short values not masked). rc.3 VM run: binary upgrade, `kill -9`
    mid-switch (drain 10.3 s, env file gone, log up to the crash), rollback 6.8 s, oha 0 non-2xx/0 errors,
    invariants, denied logins last, new `ssh dermaga`. Evidence: `docs/evidence/M7/EVIDENCE.md`.
  - Owner preference: tag commands are annotated (`git tag -a vX -m "..."`).
  - Next: owner's go; then M7b (push-to-deploy, APP-8 watch paths, live status, gosmee journey).
  - Owner choices (2026-11-26, later): Deployments tab = rows layout (mockup A); rollback dialog = compact
    (mockup A, no typed confirmation); apps run with Quadlet `RunInit=true` (verified by dry-run on Debian 13
    before use); `m3_check.sh` unbans via VM-side `systemd-run` timers and proves the ban from fail2ban's log.
  - Owner decisions (2026-11-26): the M7a/M7b split below; refuse a deploy when `MemAvailable` < the
    candidate's memory limit (shown in the deploy dialog when checkable up front); refuse rollback while
    the same app is building ("Cancel the running build first"); labels `inactive` → "Previous release",
    `rolled_back` → "Rolled back", `switching` → "Switching traffic"; rollback uses the release-time env
    through immutable encrypted env versions; the owner does every push to `r3p-dev/dermaga-test-app`
    (fish commands given when needed); all M7 tests use `dermaga-test-app`, never `portfolio`; watch paths
    are one `path.Match` glob per line, a trailing `/` means the whole folder, validated server-side;
    requirement codes stay in CLI output and raw HTTP error bodies, and are mapped to plain wording if the
    panel ever shows one; ASCII mockups before the Deployments tab and rollback dialog are built.

**Every milestone** also meets the general acceptance criteria of `AGENTS.md` "Milestone loop": tests for its
criteria (unit, golden, fake-host, fake-GitHub, API with authentication and the PRD rule, browser tests
for the main journeys, named by requirement ID), `make check` passes without deprecation warnings, the
VM checks run on `dermaga-vm` with a binary from the commit under review, the demo state updated,
`docs/ARCHITECTURE.md`, `docs/DEPENDENCIES.md`, `docs/SETUP.md` and `CHANGELOG.md` current,
`docs/evidence/M{N}/EVIDENCE.md` with excerpts and screenshot descriptions (desktop and narrow); the raw
logs and screenshots stay in `tmp/evidence/M{N}/` (DEC-26).
**Every feature ships in the panel** (`UI-1`).

Until the installer exists (M3), development runs on the VM's `vps-setup` snapshot, with the daemon,
Traefik config and units installed the way `docs/SETUP.md` describes.

## M0: Spikes and plan

No application code. Throwaway spike code lives in `spikes/` and is deleted in M1. Stop and report
after this.

1. Read `docs/PRD/` and `docs/ARCHITECTURE.md` §1, and list anything unclear, contradictory or
   missing.
2. **GitHub App spike first** (it decides whether the panel can stay private): the manifest flow with
   `redirect_url` and `setup_url` on `http://localhost:7070`, the code conversion, the installation
   redirect, one repo-scoped installation token, a clone with the token passed through environment git
   config, and how push deliveries can reach the VM behind NAT. Record what happens when the App is
   created before the hook domain is set; decision: the hook domain must be set before the manifest
   flow can be initiated, though its DNS does not need to be verified yet (ensuring the App is created
   with its real webhook URL from the start without blocking setup on DNS propagation; deliveries begin
   arriving once DNS and certificates are in place). I do the GitHub clicks. If localhost redirects
   don't work, stop and report options before anything else.
3. Spikes on `dermaga-vm`, each with raw output:
   1. Traefik (rootless, pasta, network `web`) reaching a Unix socket through a relay container on
      `web`, with the socket's directory mounted so a daemon restart doesn't break it; no new host
      port; containers cannot reach host loopback.
   2. An HTTP probe into a container on `web` from the rootless netns, resolving names via
      aardvark-dns.
   3. Traefik file-provider reload with atomic rename in a mounted directory: latency, JSON in `.yml`,
      the "one invalid file freezes updates" behavior, and a token router on an internal entry point
      to verify a switch.
   4. Traefik ACME with socket-activated :80/:443: which challenge works, where certificates are
      stored, and how dermaga can read certificate status without exposing Traefik's API (Pebble).
   5. Quadlet: `Notify=healthy` behavior and stop handling, dry-run validation, which limit keys
      Debian 13's Podman accepts, `DefaultDependencies=false` at boot, restart with backoff.
   6. `podman build` with build-time variables: measure what `--build-arg` leaves in `podman history` /
      `podman image inspect` (with and without use in `RUN`), confirm values never appear in dermaga's
      build logs (SEC-9 masking still applies to logs), and estimate the cost of supporting `--secret` +
      `RUN --mount=type=secret`.
   7. A binary with `modernc.org/sqlite` and an embedded SvelteKit build: size and idle RSS.
   8. SSH local forwarding to `127.0.0.1:7070` with `vps-setup.sh`'s sshd config; stream-local
      forwarding refused.
4. Check the current docs of Podman/Quadlet, Traefik v3 (including ACME), the GitHub App manifest flow
   and installation tokens, SvelteKit/Svelte 5 and Tailwind v4. Note anything that affects the design,
   and confirm each tool is maintained.
5. Write `docs/ARCHITECTURE.md` §2–§7 (including the state machines as tables) and `docs/SETUP.md`
   (dev machine, the test VM and its snapshots, the test GitHub App, teardown, and real GitHub webhook
   deliveries to the VM via smee.io / `gosmee` with dev webhook URL overridden by a dev-only flag/env
   not present in the UI or release builds; signed-payload replay remains the method for automated
   tests). Complete this plan (acceptance criteria and hours per milestone). List any decision in
   `docs/ARCHITECTURE.md` §1 you believe must change, and why.

## M1 (0.1.0): Foundation: daemon, login, panel shell

Repository skeleton (Go module, `Makefile`, CI, `LICENSE` Apache-2.0 with the copyright line I give
you, `README.md`), the daemon, login and sessions, the recovery commands, the panel shell.

Acceptance criteria:

- `make check` passes locally and in CI (Debian 13 container, actions pinned by SHA); binaries for both
  architectures; the panel shows the version from the tag.
- SEC-1 (refuses root), SEC-3 (foreign Host 421, missing/foreign Origin 403), SEC-7, SEC-15, ADM-1–3,
  ADM-6, SRV-1–2 (recovery commands; `dermaga panel` prints the SSH block), AUD-1–2; migrations (fresh,
  re-run, newer DB refused); secrets (round-trip, tamper, wrong key).
- Panel: login, layout and navigation, a dashboard showing real daemon status, the audit log page.
- On the VM: the daemon runs as a user service; login through `ssh -L` from the laptop works and shows
  in the audit log; idle RSS and binary size re-measured with the real embedded SvelteKit production
  build (NFR-2; M0 Spike 3.7 measured an estimated ~12 MB binary and ~18.8 MB RSS using simulated assets).
- `spikes/` deleted; relay and probe ported into the project.

## M2 (0.2.0): Generators and reconciliation

Typed structs → Quadlet units, Traefik static/global/app files (full middleware model, ACME resolver,
verify router); dry-run validation; startup reconciliation.

Acceptance criteria:

- Golden tests for every generator (minimal app, every middleware toggle, aliases + www, path prefix,
  basic auth, allowlist, body size, custom/removed headers, ACME on every router, verify router only in
  the switch rendering).
- SEC-16–SEC-19: injection tests on every user-controlled field; units pass the dry run on the VM.
- DEP-10: reconciliation tests with a fake host (orphan removed, missing file re-rendered, stopped live
  unit started, leftover verify router removed, foreign files untouched).
- Default headers equal `vps-setup.sh`'s (REL-8, generator side).

## M3 (0.3.0): Installer, hardening, lockout guard

Acceptance criteria:

- INST-1–INST-12 (INST-10 removed from v1.0.0, owner decision DEC-23), HOST-1–HOST-7, LOCK-1–LOCK-5,
  LAP-1–LAP-2, SEC-22 (installer side).
- VM from `fresh`, each run separately (needs a `v0.3.0-rc.2` pre-release tag from me; `v0.3.0-rc`
  stays without a release): (a) provider key present → only Enter at every choice, plus typing the
  sudo password when passwd asks for it; (b) no key → guided paste; (c) wrong key → confirmation can't complete,
  old access works, re-run succeeds; (d) confirmation window closed → old access works; (e) a port scan
  during the whole install never shows 7070; (f) laptop command pasted twice in bash and in fish → one
  block; (g) re-run is a no-op; (h) reboot → the daemon and Traefik are back.

Open items / decisions for M3:

- **LAP-1 laptop command identity matching**: The `LAP-1` laptop command should get the login public key's
  fingerprint embedded by the installer, look for a matching `~/.ssh/*.pub` on the laptop, and write
  `IdentityFile` (with `IdentitiesOnly yes`) only for a real match; with no match it writes no `IdentityFile`
  and tells the user. The logic runs inside `sh -c '...'` so it pastes in bash, zsh and fish.
  (Does not conflict with PRD `LAP-1`; refines the exact `Host dermaga` block content to prevent multi-key SSH failures).
  Implemented in M3a: `internal/access`, DEC-21.

Checkpoints: **M3a** installer and hardening code with unit and fake-host tests, local checks only;
**M3b** scenarios a–h on `dermaga-vm` from `fresh` with the `v0.3.0-rc.2` pre-release.

**Between M3 and M4** (owner decision DEC-25): one separate change upgrades `prettier-plugin-svelte`
to 4.x, Go to 1.27 and Bun to 1.4.4, each version and checksum taken from a logged query.

## M4 (0.4.0): First-run setup and panel access

Acceptance criteria:

- SETUP-1–SETUP-3, LAP-3, ADM-4–ADM-5, SEC-8, SEC-10, TLS-5 (hook domain certificate, Pebble).
- From a fresh install: `ssh dermaga` + browser → `/setup` → panel, without any command typed on the
  server; token reuse, expiry and use after setup refused; a password change ends other sessions; TOTP
  changes require re-auth.
- INST-12 completed (owner decision DEC-23): once SETUP-1 exists, the installer prints the first-run
  link; the `dermaga admin set-password` hint from M3 becomes the fallback only.
- ACME without an e-mail (DEC-20, owner agreed): with Pebble, check how Traefik registers and reuses
  its ACME account when `email` is empty, and record the result.
- Carried over from M1 (owner decision, 2026-11-17): the audit log table becomes stacked rows on
  narrow screens, per `docs/CONVENTIONS.md` §5.2, with a narrow-width screenshot.

Checkpoints: **M4a (~10 h)** first-run setup (SETUP-1, SEC-8, SETUP-2 password + TOTP steps with
persistent warning and `doctor` check, SETUP-3 `dermaga panel`, INST-12 first-run link, login TOTP);
**M4b (~10 h)** hook-domain step with DNS check, TLS-5 and ACME without e-mail with Pebble (DEC-20),
LAP-3, ADM-4, ADM-5, SEC-10, and narrow-screen stacked audit-log rows.

## M5 (0.5.0): Connect GitHub

Creating the GitHub App from the panel, installing it, listing repositories, the server-wide webhook
endpoint, disconnecting.

Acceptance criteria:

- GHA-1–GHA-8, SEC-9, SEC-11–SEC-14.
- With a real GitHub account (I click): App created from the panel, installed on a test repo, the repo
  visible in the panel; removing the repo on GitHub is reflected.
- Webhook tests: bad signature 401 before parsing, > 25 MiB 413, unknown repo 202 ignored, duplicate
  delivery deduped, rate limit; a real delivery reaches the daemon and shows as "last delivery" in the
  panel.
- The App's key and tokens never appear in `ps`, logs, files or the database in plaintext (greps
  shown).

Checkpoints: **M5a (~14 h)** App connect + repositories (`GHA-1`–`GHA-6`, `SEC-9`, `DEC-09`, `0003` migration, Settings → GitHub UI, fake-GitHub & Playwright tests); **M5b (~14 h)** webhooks + `hook-relay` Quadlet (`Rootfs=` empty-dir spike on `dermaga-vm`, `01-hook-domain.yml` wiring, `hooks.sock` handler, `GHA-7`, `GHA-8`, `SEC-11`–`SEC-14`, real GitHub + smee/replay VM verification, `0.5.0` bump).

## M6 (0.6.0): First deploy: wizard, build, env, logs

The wizard, mirrors and exports, builds with limits, build-time and runtime env, the first deploy (no
switch yet: one release), build and runtime logs, cancel.

Acceptance criteria:

- WIZ-1–WIZ-6, APP-1, APP-5–APP-7, APP-9, DEP-3, DEP-5–DEP-9, LOG-1–LOG-2, UI-1–UI-7.
- Portfolio created through the wizard and live (Pebble certificate); a build-time
  variable reaches the build and doesn't appear in build logs; a runtime variable reaches
  the container.
- A failing first deploy leaves nothing; a running build cancelled from the panel leaves nothing; a
  repo without a Containerfile shows the guidance.

## M7 (0.7.0): Zero downtime: push-to-deploy, redeploy, rollback

Acceptance criteria:

- DEP-1, DEP-2, DEP-4, DEP-10, APP-8, REL-6.
- `git push` to the test repo → live; push to another branch ignored; watch paths respected.
- `oha -w` at 50 req/s against `r3p-dev/dermaga-test-app` (owner decision: never `portfolio`, which has
  production CD) during (a) a successful redeploy and (b) a deploy whose health check fails: **0 non-2xx,
  0 connection errors**, raw output.
- Rollback < 10 s with zero downtime; daemon killed mid-switch → one live release and no verify router
  after restart; the state machine covered with a fake host.

Checkpoints (owner-approved 2026-11-26), each reaching the panel:

- **M7a (~18 h, `v0.7.0-rc.1`)**: blue-green redeploy and rollback end to end (`DEP-1`, `DEP-2`, `DEP-4`,
  `DEP-9`, `DEP-10`): verify-router switch proof, drain and stop, failure at every step keeps the old
  release, restart recovery, memory pre-check, image retention (live + 5), immutable encrypted env versions
  (migration `0005` with a `0004` upgrade test), Deployments tab with Redeploy and Rollback; VM: `oha` during
  redeploy, failing health, rollback, daemon killed mid-switch.
- **M7b (~14 h, `v0.7.0-rc.4`; rc.2 and rc.3 went to M7a fixes)**: push-to-deploy (`APP-8`, `DEP-5`, `UI-7`, `REL-6`): branch and watch-path
  filters, dedupe by delivery and commit, auto-deploy toggle, live deployment status; VM: real pushes via
  smee/gosmee with `oha`, 0 leftovers after removal.

**Step 0 before M7 (`v0.6.2`)**: full-width content, a server overview with API values only, no requirement
codes in visible text, no tagline footer.

## M8 (0.8.0): Domains, HTTPS, headers, middleware

Acceptance criteria:

- APP-2–APP-4, APP-10, APP-11, TLS-1–TLS-4, REL-7, REL-8.
- Every control exercised in the browser; a header change shows in `curl -sI` within seconds;
  portfolio's headers match `vps-setup.sh`'s, compression on; a route edit Traefik can't load restores
  the old file and DB row and shows the error; global defaults affect only apps without overrides.
- Certificate status shown for a domain with correct DNS (Pebble certificate active), wrong DNS
  (waiting, with the record to create) and a failed challenge (reason shown).

## M9 (0.9.0): Server care: app status, disk, dashboard, doctor

Acceptance criteria:

- DASH-1–DASH-3, RUN-1–RUN-3, DISK-1–DISK-3, DOC-1–DOC-2, REL-10, NFR-2.
- A crashing app shows as crash loop with its last log lines and recovers after a fixed redeploy;
  Restart works; current CPU/RAM match `podman stats`.
- Cleanup keeps the live and rollback images and removes the rest (disk usage before/after shown); a
  disk filled above 95 % on the VM refuses new builds while apps keep serving.
- Each `doctor` failure shown when provoked; daemon idle RSS < 30 MB and Traefik + daemon < 100 MB
  after a fresh restart; everything back after a reboot.

Note (owner, 2026-11-26): M9 also adds a **System** page (app version, CPU, RAM, disk). The v0.6.2 Dashboard
shows only the values the API already returns; detailed host metrics belong on that page.

## M10 (0.10.0): Updating dermaga, migration guide

Acceptance criteria:

- UPD-1–UPD-4, SEC-22 (update side), REL-11, MIG-1–MIG-3.
- Update from the previous pre-release to a new one in the panel while `oha` runs against portfolio:
  zero failed requests; a tampered checksum refused; a new version that fails to start is rolled back
  automatically.
- `docs/MIGRATING-FROM-VPS-SETUP.md` rehearsed on the `vps-setup` snapshot with portfolio and Kapturae
  (ask me for Kapturae's repo), downtime measured, rollback path tested.

## M11 (1.0.0): Hardening and release

Acceptance criteria:

- A written self-review of `docs/ARCHITECTURE.md` §1.2 (SEC-1–SEC-23) and NFR-1–NFR-8, each finding
  fixed or recorded with a reason; `docs/THREAT-MODEL.md`.
- Every release criterion REL-1–REL-11 demonstrated with evidence on a fresh VM; `test/vm/` harness
  complete; accessibility check of every page (automated plus a keyboard pass).
- `govulncheck` and `bun audit` reviewed with each finding explained; image versions re-checked.
- `docs/OPERATIONS.md` (everyday operation, recovery commands, updating) and `CHANGELOG.md` complete;
  version 1.0.0.

**After the tag**: I migrate the production VPS by following the guide (you never connect to it).
