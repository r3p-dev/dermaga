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
