# Working on `dermaga`

**dermaga** turns one fresh Debian 13 VPS into a hardened host and lets its owner deploy web apps from GitHub through
a web panel, with zero downtime and without memorising commands (v1.0.0 scope: `docs/PRD/summary.md` §7). Code must
be explainable: the owner reads it top to bottom and explains every flow to someone else; a feature that works but
can't be explained is not done. **No application code until `docs/PLAN.md` defines the active milestone.**

## Never

- `sudo` without asking and getting explicit approval, also on `dermaga-vm`: root changes are hard to undo.
- `ssh server`, or anything on it: `server` in the owner's SSH config is the production VPS.
- State-changing git (add, commit, push, tag, rebase, reset, stash, branches): the owner commits; read-only is fine.
- Pushing to or triggering repos with production CD (e.g. `portfolio`): use `r3p-dev/dermaga-test-app`.
- Real outside services (GitHub, Let's Encrypt) from tests: tests use fakes, the VM uses Pebble.
- Invasive workarounds when blocked (process memory, unowned sockets, bypassing permissions, hunting secrets): ask.
- Secrets (tokens, `master.key`, App keys, `.env`, VM credentials) in chat, logs, evidence or commits: permanent.
- AI attribution in commit messages (`Co-Authored-By`, "Generated with", Claude or any assistant).
- Reading or copying Litepod's `ee/`: it is not Apache-2.0.
- Inventing a value or assuming an owner step happened (GitHub clicks, DNS, snapshot reverts, repo URLs, tags).

## Session start

Always read this file and, in `docs/PLAN.md`, the status table and **Active milestone** (its handoff says where the
last session stopped). When the milestone needs them: the `docs/PRD/` sections of its requirement IDs (glossary and
scope in `summary.md`); `docs/ARCHITECTURE.md` from the code map (§3) to the sections you touch; the CONVENTIONS
section for your language, and §7 for tests; DEPENDENCIES before adding a dependency; SETUP when the setup changes.
Not routinely: old milestones' evidence (summarised in PLAN). Never `cat` long logs: use `grep -n`, `tail`, subagents.

- The owner writes this file, `docs/PRD/`, `docs/CONVENTIONS.md` and ARCHITECTURE §1; you write ARCHITECTURE §2+,
  `docs/SETUP.md`, `docs/PLAN.md` (owner-approved), `docs/DEPENDENCIES.md`, evidence and `CHANGELOG.md`.
- The PRD wins on behavior, ARCHITECTURE §1 on technology and security, CONVENTIONS on style; report conflicts.
- Visual design (layout, components, colors, typography, copy, the logo) and anything left open: ask the owner.
- Check current stable versions and official docs, not memory; no deprecated APIs; pin images by tag and digest.

## Milestone loop

1. **Align** with the PLAN status table. If the previous milestone or checkpoint is not confirmed, stop and ask.
2. **Plan:** before writing code, send a 5–10 line plan (what you'll build, which files) and wait for approval.
3. **Implement and test** per CONVENTIONS, every criterion covered: unit (packages, state machines, generators),
   golden (every generated file), fake host (host commands), API (each route: happy path, authentication, the PRD
   rule by ID), fake GitHub API, browser (main journeys). `make check` passes with no deprecation warnings.
4. **VM checks** on `dermaga-vm` with a binary built from the commit under review.
5. **Docs:** ARCHITECTURE §2+ when packages, routes, schema, state machines, generated files or decisions change
   (every workaround: a short code comment and a decisions-log entry); DEPENDENCIES for every addition; SETUP
   reproducible; PLAN status. **Demo state:** `test/vm/seed.sh` brings the VM to a usable state for the milestone
   (once GitHub is connected: the test app with a domain, custom headers, basic auth) and prints what and where.
6. **Evidence and report** (below), then **stop** and wait for the owner's confirmation.

Slow, reviewable progress: a milestone fits one review sitting. **Stop after every milestone**; one that grows is
split into checkpoints (`M5a`, `M5b`, …) with a stop at each. At the end of every checkpoint, update the
**Handoff** in PLAN's Active milestone (done, remaining, new decisions); the next session starts there. Ask before
decisions that are hard to reverse or contradict the documents; otherwise decide, log the reason in ARCHITECTURE.

## Evidence

- Every verification of an acceptance criterion runs through `scripts/evidence-run M<N> <name> -- <cmd…>`: **one
  log per criterion, not per command**, in gitignored `tmp/evidence/M<N>/logs/` (screenshots: `screenshots/`).
  Raw artifacts are never committed; `docs/evidence/M<N>/` holds only `EVIDENCE.md` (per `docs/evidence/TEMPLATE.md`).
- Excerpts: ≤ ~20 lines of exact text from that run's local log (cite its `tmp/` path), from the commit under review.
- Status is only `PASS`, `FAIL`, `NOT RUN` (with the exact commands for the owner) or `decision recorded`. `PASS`
  only for what actually ran, with exit 0 and checked output. Every verification script has a negative control.
- Open every screenshot; describe it in one line in EVIDENCE.md (page, width, theme, what it shows) before `PASS`.
- Contrast and theme checks run on the rendered page (Playwright + axe, WCAG AA), never on assumed CSS colors.
- VM binaries come from committed state (a clean throwaway worktree is fine); a dirty build is labelled dirty;
  `VERSION` is never faked (`-X main.Version=…`).
- Every command dermaga prints and every value the UI shows comes from real state (no placeholders, hard-coded
  success text, guessed hosts or ports); follow printed instructions end to end yourself before `PASS`. A
  feature that exists only in the API is not done.
- Pinned values (digests, SHAs, versions, checksums, URLs) come from real queries; DEPENDENCIES has command and result.

## Report

To the owner, in Indonesian. Chat holds only **Ringkasan** (what was built, requirement IDs), the `EVIDENCE.md` link,
**Pertanyaan terbuka**, **Suggested commits** and the next milestone. Only in `EVIDENCE.md` (English, per TEMPLATE):
run and view (commands, tunnel, URLs, demo state, what to click), screens (desktop and narrow), how it works (UI →
route → handler → package → host → result, with a sequence), code tour in reading order, decisions and deviations
(every workaround, anything done as root), proof.

## Git

- Suggested commits: small, ordered, each passing `make check` alone; Conventional Commits (subject ≤ 72
  characters, imperative, scope where it helps, a short body explaining **why**); every changed path in exactly
  one commit; no secrets or build output; `.gitignore` kept current, every change to it mentioned.
- Fish form (no heredocs, no `$'…'`): `git add a b; and git commit -m "type(scope): subject" -m "Body."`
- Verify with `scripts/check-commits <plan>` (plan in `tmp/evidence/M<N>/`; `--print-fish` prints the commands);
  only its summary lines go into EVIDENCE.md. The milestone ends with a version bump (`CHANGELOG.md`, Keep a
  Changelog) and a tag command for the owner. Read tags with `git for-each-ref refs/tags` or `git describe --tags`.
- After the owner pushes, check CI with `gh run list` / `gh run view`; if you can't, ask the owner for the log.

## Versioning

SemVer, from the git tag (embedded at build time, shown by `dermaga version`). Before 1.0.0 each milestone ends
with a **minor** bump (the version in PLAN); fixes after the owner's review are **patch** bumps (`0.N.1`).
**1.0.0** = every requirement in the approved PLAN plus the hardening milestone. After it: features minor, fixes
patch, breaking changes (API, or on-disk state that can't migrate automatically) major. The owner tags.

## Test environment

- VM work runs on **`dermaga-vm`** (local libvirt; `ssh dermaga-vm`: user `server`, port 4222). Snapshots: `fresh`
  (bare Debian 13 cloud image, only cloud-init's user `debian`), `vps-setup` (after `vps-setup.sh init`). Only for it:
  - allowed without asking: `virsh --connect qemu:///system start|shutdown|reboot|snapshot-list dermaga-vm`, and
    `virt-xml --connect qemu:///system dermaga-vm --edit --events on_reboot=restart` (again after every revert);
  - ask first: `snapshot-revert`, `snapshot-create-as`, `snapshot-delete`;
  - never: any other domain, libvirt networks, storage pools, `undefine`.
- The VM is behind NAT: no inbound webhooks, no real certificates. Pebble for ACME; replay signed deliveries in
  tests; for real deliveries to the App's webhook, find a forwarding method that works with GitHub Apps (current
  docs) and confirm it with the owner. A real GitHub App only when the milestone says so, created by the owner, on
  a test repo the owner gives. Staging and production certificates only on the real VPS at the end.
- `test/vm/*.sh` scenarios: idempotent, no residue, first printing `date -Is`, `hostname`, `git rev-parse --short HEAD`.

## Reference material (read when a milestone needs it; never assume what it contains)

1. **`vps-setup.sh`** (`https://github.com/r3p-dev/dotfiles`): reuse its hardening and Traefik knowledge (base
   system, user, sudo, subids, memory, kernel, auto updates, Podman, firewall, fail2ban, SSH, Traefik renderers,
   validators, limits; its headers are dermaga's defaults). Do **not** copy its deploy flow (Podlet, compose
   files, Actions → GHCR, forced SSH commands): that is what dermaga replaces.
2. **Litepod** (`https://github.com/litepodsh/litepod`, Apache-2.0 except `ee/`): UX and rollout inspiration
   only; adapted logic keeps Apache-2.0 attribution; do not fork it.
3. **`https://github.com/r3p-dev/portfolio`**, the first real app (discover build, port, health check, runtime),
   and **Kapturae**, the second (ask the owner for its repo URL when a milestone needs it).
