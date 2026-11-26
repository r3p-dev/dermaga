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
| Release, checksums, VM install from `fresh`, UI smoke test via the tunnel, `m3_check.sh access` | NOT RUN | — | runs after the owner tags `v0.6.2` |
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
