# Plan

Milestones in order. **Slow, reviewable progress**: each milestone is small enough to review the
screens, read the code and understand how it works in one sitting. The implementer stops after every
milestone and waits for confirmation (`AGENTS.md` §3). A milestone that grows is split into
checkpoints (`M6a`, `M6b`, …), each with its own stop.

Requirement IDs refer to `docs/PRD/` and `docs/ARCHITECTURE.md` §1.2. In M0 the implementer completes
the **acceptance criteria** (by requirement ID) and adds an **hour estimate** to every milestone below,
and may propose a different split; the plan is final once approved.

**v1.0.0 scope** (`docs/PRD/summary.md` §7): web UI only, GitHub App only, stateless apps, blue-green
deploys, domains with automatic HTTPS, headers and middleware, server care and updates. Databases with
backups come in v1.1.

| #   | Version | Milestone                                        | Status      | Hours |
| --- | ------- | ------------------------------------------------ | ----------- | ----- |
| M0  | –       | Spikes and plan                                  | not started | –     |
| M1  | 0.1.0   | Foundation: daemon, login, panel shell           | not started | –     |
| M2  | 0.2.0   | Generators and reconciliation                    | not started | –     |
| M3  | 0.3.0   | Installer, hardening, lockout guard              | not started | –     |
| M4  | 0.4.0   | First-run setup and panel access                 | not started | –     |
| M5  | 0.5.0   | Connect GitHub                                   | not started | –     |
| M6  | 0.6.0   | First deploy: wizard, build, env, logs           | not started | –     |
| M7  | 0.7.0   | Zero downtime: push-to-deploy, redeploy, rollback | not started | –    |
| M8  | 0.8.0   | Domains, HTTPS, headers, middleware              | not started | –     |
| M9  | 0.9.0   | Server care: app status, disk, dashboard, doctor | not started | –     |
| M10 | 0.10.0  | Updating dermaga, migration guide                | not started | –     |
| M11 | 1.0.0   | Hardening and release                            | not started | –     |

Hours are the implementer's estimate for building, testing and writing the evidence of each
milestone; review time is not included.

**Every milestone** also meets the general acceptance criteria of `AGENTS.md` §2: tests for its
criteria (unit, golden, fake-host, fake-GitHub, API with authentication and the PRD rule, browser tests
for the main journeys, named by requirement ID), `make check` passes without deprecation warnings, the
VM checks run on `dermaga-vm` with a binary from the commit under review, the demo state updated,
`docs/ARCHITECTURE.md`, `docs/DEPENDENCIES.md`, `docs/SETUP.md` and `CHANGELOG.md` current,
`docs/evidence/M{N}/EVIDENCE.md` with screenshots (desktop and narrow) and logs in the same folder.
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
   config, and how push deliveries can reach the VM behind NAT. I do the GitHub clicks. If localhost
   redirects don't work, stop and report options before anything else.
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
      stored, and how dermaga can read certificate status without exposing Traefik's API (staging).
   5. Quadlet: `Notify=healthy` behavior and stop handling, dry-run validation, which limit keys
      Debian 13's Podman accepts, `DefaultDependencies=false` at boot, restart with backoff.
   6. `podman build` with build-time variables: how to pass them so they never appear in logs or image
      history.
   7. A binary with `modernc.org/sqlite` and an embedded SvelteKit build: size and idle RSS.
   8. SSH local forwarding to `127.0.0.1:7070` with `vps-setup.sh`'s sshd config; stream-local
      forwarding refused.
4. Check the current docs of Podman/Quadlet, Traefik v3 (including ACME), the GitHub App manifest flow
   and installation tokens, SvelteKit/Svelte 5 and Tailwind v4. Note anything that affects the design,
   and confirm each tool is maintained.
5. Write `docs/ARCHITECTURE.md` §2–§7 (including the state machines as tables) and `docs/SETUP.md`
   (dev machine, the test VM and its snapshots, the test GitHub App, teardown). Complete this plan
   (acceptance criteria and hours per milestone). List any decision in `docs/ARCHITECTURE.md` §1 you
   believe must change, and why.

## M1 (0.1.0): Foundation: daemon, login, panel shell

Repository skeleton (Go module, `Makefile`, CI, `LICENSE` Apache-2.0 with the copyright line I give
you, `README.md`), the daemon, login and sessions, the recovery commands, the panel shell.

Draft acceptance criteria:

- `make check` passes locally and in CI (Debian 13 container, actions pinned by SHA); binaries for both
  architectures; the panel shows the version from the tag.
- SEC-1 (refuses root), SEC-3 (foreign Host 421, missing/foreign Origin 403), SEC-7, SEC-15, ADM-1–3,
  ADM-6, SRV-1–2 (recovery commands; `dermaga panel` prints the SSH block), AUD-1–2; migrations (fresh,
  re-run, newer DB refused); secrets (round-trip, tamper, wrong key).
- Panel: login, layout and navigation, a dashboard showing real daemon status, the audit log page.
- On the VM: the daemon runs as a user service; login through `ssh -L` from the laptop works and shows
  in the audit log; idle RSS recorded (NFR-2).
- `spikes/` deleted; relay and probe ported into the project.

## M2 (0.2.0): Generators and reconciliation

Typed structs → Quadlet units, Traefik static/global/app files (full middleware model, ACME resolver,
verify router); dry-run validation; startup reconciliation.

Draft acceptance criteria:

- Golden tests for every generator (minimal app, every middleware toggle, aliases + www, path prefix,
  basic auth, allowlist, body size, custom/removed headers, ACME on every router, verify router only in
  the switch rendering).
- SEC-16–SEC-19: injection tests on every user-controlled field; units pass the dry run on the VM.
- DEP-10: reconciliation tests with a fake host (orphan removed, missing file re-rendered, stopped live
  unit started, leftover verify router removed, foreign files untouched).
- Default headers equal `vps-setup.sh`'s (REL-8, generator side).

## M3 (0.3.0): Installer, hardening, lockout guard

Draft acceptance criteria:

- INST-1–INST-12, HOST-1–HOST-7, LOCK-1–LOCK-5, LAP-1–LAP-2, SEC-22 (installer side).
- VM from `fresh`, each run separately (needs a `v0.3.0-rc` pre-release tag from me): (a) provider key
  present → only Enter pressed; (b) no key → guided paste; (c) wrong key → confirmation can't complete,
  old access works, re-run succeeds; (d) confirmation window closed → old access works; (e) a port scan
  during the whole install never shows 7070; (f) laptop command pasted twice in bash and in fish → one
  block; (g) re-run is a no-op; (h) reboot → the daemon and Traefik are back.

## M4 (0.4.0): First-run setup and panel access

Draft acceptance criteria:

- SETUP-1–SETUP-3, LAP-3, ADM-4–ADM-5, SEC-8, SEC-10, TLS-5 (hook domain certificate, staging).
- From a fresh install: `ssh dermaga` + browser → `/setup` → panel, without any command typed on the
  server; token reuse, expiry and use after setup refused; a password change ends other sessions; TOTP
  changes require re-auth.

## M5 (0.5.0): Connect GitHub

Creating the GitHub App from the panel, installing it, listing repositories, the server-wide webhook
endpoint, disconnecting.

Draft acceptance criteria:

- GHA-1–GHA-8, SEC-9, SEC-11–SEC-14.
- With a real GitHub account (I click): App created from the panel, installed on a test repo, the repo
  visible in the panel; removing the repo on GitHub is reflected.
- Webhook tests: bad signature 401 before parsing, > 1 MiB 413, unknown repo 202 ignored, duplicate
  delivery deduped, rate limit; a real delivery reaches the daemon and shows as "last delivery" in the
  panel.
- The App's key and tokens never appear in `ps`, logs, files or the database in plaintext (greps
  shown).

## M6 (0.6.0): First deploy: wizard, build, env, logs

The wizard, mirrors and exports, builds with limits, build-time and runtime env, the first deploy (no
switch yet: one release), build and runtime logs, cancel.

Draft acceptance criteria:

- WIZ-1–WIZ-6, APP-1, APP-5–APP-7, APP-9, DEP-3, DEP-5–DEP-9, LOG-1–LOG-2, UI-1–UI-7.
- Portfolio created through the wizard and live (staging or self-signed certificate); a build-time
  variable reaches the build and doesn't appear in logs or image history; a runtime variable reaches
  the container.
- A failing first deploy leaves nothing; a running build cancelled from the panel leaves nothing; a
  repo without a Containerfile shows the guidance.

## M7 (0.7.0): Zero downtime: push-to-deploy, redeploy, rollback

Draft acceptance criteria:

- DEP-1, DEP-2, DEP-4, DEP-10, APP-8, REL-6.
- `git push` to the test repo → live; push to another branch ignored; watch paths respected.
- `oha -w` at 50 req/s against portfolio during (a) a successful redeploy and (b) a deploy whose health
  check fails: **0 non-2xx, 0 connection errors**, raw output.
- Rollback < 10 s with zero downtime; daemon killed mid-switch → one live release and no verify router
  after restart; the state machine covered with a fake host.

## M8 (0.8.0): Domains, HTTPS, headers, middleware

Draft acceptance criteria:

- APP-2–APP-4, APP-10, APP-11, TLS-1–TLS-4, REL-7, REL-8.
- Every control exercised in the browser; a header change shows in `curl -sI` within seconds;
  portfolio's headers match `vps-setup.sh`'s, compression on; a route edit Traefik can't load restores
  the old file and DB row and shows the error; global defaults affect only apps without overrides.
- Certificate status shown for a domain with correct DNS (staging certificate active), wrong DNS
  (waiting, with the record to create) and a failed challenge (reason shown).

## M9 (0.9.0): Server care: app status, disk, dashboard, doctor

Draft acceptance criteria:

- DASH-1–DASH-3, RUN-1–RUN-3, DISK-1–DISK-3, DOC-1–DOC-2, REL-10, NFR-2.
- A crashing app shows as crash loop with its last log lines and recovers after a fixed redeploy;
  Restart works; current CPU/RAM match `podman stats`.
- Cleanup keeps the live and rollback images and removes the rest (disk usage before/after shown); a
  disk filled above 95 % on the VM refuses new builds while apps keep serving.
- Each `doctor` failure shown when provoked; daemon idle RSS < 30 MB and Traefik + daemon < 100 MB
  after a fresh restart; everything back after a reboot.

## M10 (0.10.0): Updating dermaga, migration guide

Draft acceptance criteria:

- UPD-1–UPD-4, SEC-22 (update side), REL-11, MIG-1–MIG-3.
- Update from the previous pre-release to a new one in the panel while `oha` runs against portfolio:
  zero failed requests; a tampered checksum refused; a new version that fails to start is rolled back
  automatically.
- `docs/MIGRATING-FROM-VPS-SETUP.md` rehearsed on the `vps-setup` snapshot with portfolio and Kapturae
  (ask me for Kapturae's repo), downtime measured, rollback path tested.

## M11 (1.0.0): Hardening and release

Draft acceptance criteria:

- A written self-review of `docs/ARCHITECTURE.md` §1.2 (SEC-1–SEC-23) and NFR-1–NFR-8, each finding
  fixed or recorded with a reason; `docs/THREAT-MODEL.md`.
- Every release criterion REL-1–REL-11 demonstrated with evidence on a fresh VM; `test/vm/` harness
  complete; accessibility check of every page (automated plus a keyboard pass).
- `govulncheck` and `bun audit` reviewed with each finding explained; image versions re-checked.
- `docs/OPERATIONS.md` (everyday operation, recovery commands, updating) and `CHANGELOG.md` complete;
  version 1.0.0.

**After the tag**: I migrate the production VPS by following the guide (you never connect to it).
