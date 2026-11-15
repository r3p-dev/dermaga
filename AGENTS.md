# Working on `dermaga`

**dermaga** turns one fresh Debian 13 VPS into a hardened host and lets its owner deploy web apps
from GitHub through a web panel, with zero downtime and without memorising commands. v1.0.0 is
web-UI-only and GitHub-App-only, for stateless apps (`docs/PRD/summary.md` §7). It is built one
milestone at a time from written specs. **Do not write application code until the active milestone
is defined in `docs/PLAN.md`.**

The codebase must be easy to read and explain: the owner wants to read it top to bottom and explain
every flow to someone else. Clarity beats cleverness. A feature that works but can't be explained is
not done.

## 1. Documents

Read these at the start of every session:

| Document                              | Defines                                                                                                   | Who writes it               |
| ------------------------------------- | --------------------------------------------------------------------------------------------------------- | --------------------------- |
| `AGENTS.md` (this file)               | How to work: milestone loop, rules, reporting, versioning, git, test environment                          | Owner                       |
| `docs/PRD/` (start with `summary.md`) | What dermaga does, per area, with requirement IDs                                                         | Owner                       |
| `docs/CONVENTIONS.md`                 | How code is written: project rules, Go, UI, installer, tests, comments                                    | Owner                       |
| `docs/ARCHITECTURE.md`                | §1 fixed decisions and security (owner); §2+ components, flows, state machines, decisions log (you)       | Both                        |
| `docs/SETUP.md`                       | Zero-to-running reproduction (dev machine and test VM) and complete teardown/cleanup steps                | You                         |
| `docs/PLAN.md`                        | Milestones, versions, acceptance criteria, estimates, status                                              | You (approved by the owner) |
| `docs/DEPENDENCIES.md`                | Approved Go modules, UI packages, host packages and container images, with reasons                        | You                         |
| `docs/evidence/M{N}/EVIDENCE.md`      | Proof that a milestone is done                                                                            | You                         |
| `CHANGELOG.md`                        | Notable changes per version (Keep a Changelog)                                                            | You                         |

On behavior the PRD wins; on technology and security `docs/ARCHITECTURE.md` §1 wins; on code style
`docs/CONVENTIONS.md` wins. If two documents conflict, say so in your report instead of choosing
silently.

**Visual design** (layout, components, colors, typography, copy, the logo) and anything the
documents leave open: ask me. I will point you to the reference.

Always check the current stable versions and official documentation of every tool and library
instead of relying on memory, and never use deprecated APIs. Pin exact versions of container images
(tag and digest).

## 2. Milestone loop

1. **Align:** read `docs/PLAN.md` and the existing `docs/evidence/M*/EVIDENCE.md` files to find the
   active milestone. If the previous milestone has no confirmation from me, stop and ask.
2. **Implement and test:** follow `docs/CONVENTIONS.md`. Cover the milestone's acceptance criteria
   with tests: unit tests for packages, state machines and generators; golden tests for every
   generated file; fake-host tests for everything that runs host commands; API tests for every
   route (happy path, authentication, and the PRD rule it implements, named by requirement ID);
   a fake GitHub API for everything that talks to GitHub;
   browser tests for the main UI journeys. `make check` passes with no deprecation warnings.
3. **Prove it on the VM:** run the milestone's VM checks on `dermaga-vm` (§8) with a binary built
   from the commit under review.
4. **Keep the docs current:** update `docs/ARCHITECTURE.md` §2+ when packages, routes, schema, state
   machines, generated files or decisions change; every workaround gets a short code comment and a
   decisions-log entry. Update `docs/DEPENDENCIES.md` for every module, package or image added. Keep
   `docs/SETUP.md` reproducible. Update the status column in `docs/PLAN.md`.
5. **Demo state:** keep a script that brings the VM to a usable demo state for the current milestone
   (`test/vm/seed.sh`): once GitHub is connected, the test app from my test repo with a domain,
   custom headers and basic auth. It prints what it created and how to reach it.
6. **Produce evidence:** a milestone is not done until `docs/evidence/M{N}/EVIDENCE.md` exists with
   the content of the report below (English is fine), with its screenshots and logs in the same
   folder, and known limitations or deferred items.
7. **Report and stop** (§4), then wait for my confirmation before the next milestone.

## 3. Pace

I want **slow, reviewable progress**. Every milestone is small enough that I can look at the screens,
read the code and understand how it works in one sitting. **Stop after every milestone.** If a
milestone grows, split it into checkpoints (`M5a`, `M5b`, …) and stop at each one.

## 4. Report format

Write the report to me **in Indonesian**, short and concrete:

1. **Ringkasan**: what was built, in a few sentences, with the requirement IDs covered.
2. **Cara menjalankan dan melihat**: exact commands from a clean checkout and on the VM, the SSH
   tunnel and URLs, the demo state, and what to click to see each new thing.
3. **Tampilan**: screenshots of every new or changed panel page (desktop and narrow width) in
   `docs/evidence/M{N}/`, linked.
4. **Cara kerja**: how each new flow works from the click (or the webhook, or the daemon) to the host
   and back (UI → API route → handler → package → host command or generated file → result), with a
   short sequence (text or Mermaid).
5. **Tur kode**: the new or changed files **in reading order**, one line each, so I can read them in
   that order.
6. **Keputusan dan penyimpangan**: decisions taken, deviations from the documents, every workaround
   added (with its reason).
7. **Bukti**: the exact verification commands and their real, unedited output.
8. **Pertanyaan terbuka**, then **Suggested commits** (§7), then the next milestone.

## 5. Working with me

- Ask before any decision that is hard to reverse or contradicts the documents. Otherwise decide,
  record the decision and its reason in the decisions log of `docs/ARCHITECTURE.md`, and continue.
- Never claim something works without having run it. If you can't run something, write **NOT RUN**
  and give me the exact commands to run. Evidence output is raw and unedited, from the commit under
  review, never summarised or reconstructed.
- **Every step that needs me stops and asks**: GitHub clicks, DNS records, snapshot reverts, repo URLs,
  tags. Never invent a value and never assume it happened.
- **The UI shows only what the server returned.** No placeholder results, no hard-coded success
  text, no guessed hosts or ports. A feature that exists only in the API is not done.
- Never call real outside services from tests (GitHub, Let's Encrypt). On the VM: Pebble (local ACME
  test server) for certificates; a real GitHub App only when the milestone says so, created by me, on a
  test repo I give you. Staging and production certificates stay only for the real VPS at the end.
- **No production systems**: never connect to `server` (my production VPS) or run anything on it.

## 6. Versioning

**Semantic Versioning.** The version comes from the git tag (embedded at build time and shown by
`dermaga version`); changes are recorded in `CHANGELOG.md` (Keep a Changelog).

- **Before 1.0.0:** each milestone ends with a **minor** bump (the version in `docs/PLAN.md`). Fixes
  after my review of that milestone are **patch** bumps (`0.N.1`, …).
- **1.0.0** = every requirement in the approved `docs/PLAN.md`, plus the hardening milestone. That
  is the version I can put on my production VPS.
- **After 1.0.0:** new features → minor, fixes → patch, breaking changes → major. "Breaking" means an
  API change, or an on-disk state change that can't be migrated automatically.
- You suggest the bump and the tag command; **I create tags myself**.

## 7. Git: I commit, you suggest

- **Never create commits, push, tag, rebase, reset, stash, or create/switch branches. Don't stage
  files with `git add` either.** Read-only git (`status`, `diff`, `log`, `show`) is fine. I review
  and commit everything myself.
- At the end of every milestone (and at any natural checkpoint inside one), add a **"Suggested
  commits"** section: an ordered list of small, focused commits, each passing `make check` on its
  own. For each:
  - the Conventional Commits message: subject ≤ 72 characters, imperative mood, a scope where it
    helps (`feat(deploy): …`, `test(webhook): …`, `docs: …`), and a short body explaining **why**;
  - the exact paths it includes;
  - a copy-pasteable command for the **fish** shell (no heredocs, no `$'…'`):
    `git add path/a path/b; and git commit -m "type(scope): subject" -m "Body paragraph."`
- The last suggested commit of a milestone is the version bump (`CHANGELOG.md`), followed by the tag
  command for me to run, e.g. `git tag -a v0.3.0 -m "v0.3.0"`.
- Every changed path appears in exactly one suggested commit. Run `git status --short` at the end
  and confirm nothing is left out or listed twice.
- Commit messages contain **no AI attribution**: no `Co-Authored-By`, no "Generated with", no
  mention of Claude or any assistant.
- Never put secrets, tokens, `master.key`, GitHub App keys, `.env` files, VM credentials or build
  output in a suggested commit. Keep `.gitignore` up to date and mention every change to it.

## 8. Test environment

- All VM work runs on **`dermaga-vm`** (`ssh dermaga-vm`: user `server`, port 4222, passwordless
  sudo), a local libvirt VM. **`server` in my SSH config is my production VPS. Never connect to it.**
- libvirt snapshots: `fresh` (bare Debian 13 cloud image, only cloud-init's user `debian`) and
  `vps-setup` (after `vps-setup.sh init`). For `dermaga-vm` only:
  - allowed without asking: `virsh --connect qemu:///system start|shutdown|reboot|snapshot-list
    dermaga-vm`, and `virt-xml --connect qemu:///system dermaga-vm --edit --events
    on_reboot=restart` (run it again after every snapshot revert);
  - ask me first: `snapshot-revert`, `snapshot-create-as`, `snapshot-delete`;
  - never: any other domain, libvirt networks, storage pools, `undefine`.
- The VM is behind NAT: no inbound webhooks and no real certificates. Replay signed deliveries in
  tests; for real GitHub deliveries to the App's webhook, find a forwarding method that works with
  GitHub Apps (check current docs) and confirm it with me.
- VM scenarios live in `test/vm/*.sh`: idempotent, no residue, each printing `date -Is`, `hostname`
  and `git rev-parse --short HEAD` first.

## 9. Reference material

Read these when a milestone needs them; never assume what they contain.

1. **`vps-setup.sh`** in `https://github.com/r3p-dev/dotfiles`: my tested Bash script for this kind
   of server. Reuse its **hardening and Traefik knowledge** (base system, user, sudo, subids, memory,
   kernel, auto updates, Podman, firewall, fail2ban, SSH, the Traefik static/dynamic/container
   renderers, its validators and default limits). Do **not** copy its deploy flow (Podlet, compose
   files, GitHub Actions → GHCR, forced SSH commands); that is what dermaga replaces. Its response
   headers are the reference for dermaga's defaults.
2. **Litepod** (`https://github.com/litepodsh/litepod`, Apache-2.0 except `ee/`, which you must not
   read or copy): inspiration only, for UX and rollout. Adapted logic keeps
   Apache-2.0 attribution. Do not fork it.
3. **`https://github.com/r3p-dev/portfolio`**: the first real app. Discover its build, port,
   health check and runtime from the repo.
4. **Kapturae**: the second real app. Ask me for its repo URL when a milestone needs it.
