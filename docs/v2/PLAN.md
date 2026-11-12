# dermaga v2 plan

Phases run in order. Each phase ends with `make check` passing, its VM acceptance run on
`dermaga-vm` with raw evidence in `docs/v2/P<n>-EVIDENCE.md` (BRIEF §4.6), a report, and suggested
commits. **Stop after each phase and wait for my confirmation.** Hours are estimates.

Before Phase 0 starts: read `docs/BRIEF.md`, `docs/v2/BRIEF.md`, `docs/v2/ARCHITECTURE.md`,
`docs/ARCHITECTURE.md` and the code. Do not trust `docs/M3`–`M10-EVIDENCE.md` or the "done" marks in
`docs/PLAN.md`; treat every v1 feature as unverified until Phase 0 proves it.

| Phase | Goal                                                                | Hours   |
| ----- | ------------------------------------------------------------------- | ------- |
| 0     | Finish v1 honestly: close gaps, re-run acceptance                   | 30      |
| 1     | Zero-file installer, lockout guard, laptop command, first-run setup | 18      |
| 2     | GUI completeness: every CLI capability in the UI                    | 20      |
| 3     | GitHub App (spike first)                                            | 22      |
| 4     | Template catalog for ready-made apps                                | 10      |
| 5     | Railpack (spike-gated)                                              | 16      |
| 6     | Tailscale add-on (decision-gated, optional)                         | 8       |
|       | **Total**                                                           | **124** |

---

## Phase 0 — finish v1 honestly (30 h)

### 0.1 Audit

Produce `docs/v2/P0-AUDIT.md`: for every requirement in v1 BRIEF §3–§9, its status (works / partial
/ missing) with the file and line that implements it, or "not found". Known gaps to confirm and fix:

| #   | Gap                                                                                              | Where                                              |
| --- | ------------------------------------------------------------------------------------------------ | -------------------------------------------------- |
| G1  | App page has no Domains & Middleware tab and no Settings tab (declared in `Tab`, never rendered) | `ui/src/routes/apps/[name]/+page.svelte`           |
| G2  | No API for editing middleware from the UI                                                        | `internal/api`                                     |
| G3  | `install.sh` does not install `restic` or `git`                                                  | `install/install.sh` `setup_base_system`           |
| G4  | `doctor` lacks Traefik health, domain DNS, restic reachability                                   | `internal/doctor`                                  |
| G5  | Wizard does not detect Containerfile/Dockerfile or `EXPOSE`                                      | `ui/src/routes/apps/new/+page.svelte`, API         |
| G6  | restic password not shown once / no offline-storage confirmation                                 | `internal/backup`, UI                              |
| G7  | `release.yml` uses `bun-version: latest`; `ui/package.json` uses `^` ranges                      | `.github/workflows/release.yml`, `ui/package.json` |
| G8  | M7 evidence claims CLI↔UI parity that does not exist                                             | `docs/M7-EVIDENCE.md`                              |

Add any further gap the audit finds. Fix them all in this phase.

**Decisions after the audit (2026-11-12).** Gap numbers refer to `docs/v2/P0-AUDIT.md` §3–§4.

- G28 (D-a): v1 texts live in `docs/*.md`, v2 texts in `docs/v2/*.md`.
- G26 (D-b): keep `net.ipv4.ip_unprivileged_port_start = 80`. Closed by documentation (v2 BRIEF
  §2.3, v2 THREAT-MODEL §5, v1 THREAT-MODEL §7.4), not by code. No nftables redirect, no VM spike.
- G24 (D-c): D54 stays for v1.0; password and TOTP changes are CLI-only in Phase 0. Moved to
  **Phase 1** (UI forms with re-authentication, next to `/setup`).
- G31 (D-d): Prettier (`prettier`, `prettier-plugin-svelte`, exact pins) with `format` and
  `format:check`; `format:check` is part of `make check`.
- D-e, moved to **Phase 2**:

  | Gap    | Item                                                             |
  | ------ | ---------------------------------------------------------------- |
  | G23    | Global defaults (limits, headers, rate limit) stored and editable |
  | G29    | Wizard creates `image` apps                                      |
  | G32    | Database "pull and restart"                                      |
  | G33    | CPU/RAM charts and sparklines                                    |
  | parity | `deploy --ref`, `logs -n`, extra `link` aliases, `self-update` in the UI (audit §5) |

- Fix order: D-a → CS1 → Prettier reformat → G9 → CS3 → CS4 → CS2 → CS5 → CS6 → CS7 → CS8 (G5 only)
  → CS9 → CS10 → CS11 (G38, G34) → CS12 (G35 only). G9 (mandatory checksum) is its own change set;
  the rest of CS7 keeps its place. G36: measure Traefik idle RSS after a fresh restart before
  calling it a violation; if over budget, propose `GOMEMLIMIT` first.

### 0.2 Re-run v1 acceptance with real evidence

Each item below needs something from me. **Stop and ask** at each one; do not simulate.

- M4: `oha` 50 req/s during a successful and a failing blue-green deploy of portfolio.
- M6: I provide an R2/B2 test bucket. Scheduled backup observed; test restore for all three engines;
  failing dump fails the backup and notifies. **DR drill**: I revert `dermaga-vm` to `fresh` →
  install → `dermaga restore-system` from the bucket → apps and DBs back, verified by content. A
  volume wipe is not a DR drill.
- M8: from the `fresh` snapshot, one install command → `doctor` all green; re-run is a no-op; port
  scan from the laptop.
- M10: ask me for the Kapturae repo URL and discover its build, port and health from the repo. The
  public push-to-live and certificate check happens on the real migration, which I schedule.
- Published ports (decision D-b, G26): verify on `dermaga-vm` that no dermaga-managed container
  publishes a host port: `podman ps --format '{{.Names}} {{.Ports}}'` plus `ss -tlnp`. The
  threat-model claim (v1 THREAT-MODEL §7.4, v2 THREAT-MODEL §5) rests on a grep only until then.

### 0.3 Docs correction

Mark the M3–M10 evidence files as "superseded by docs/v2/P0-EVIDENCE.md" at the top (do not delete
them; they are history). Update `docs/PLAN.md` statuses to match the audit.

**Acceptance:** audit table complete with zero "missing" items left; every 0.2 item has raw evidence
or is explicitly NOT RUN with my agreement to defer; `make check` and `bun run check` pass;
exact pins everywhere; tag-ready `v1.0.0` (I tag it).

---

## Phase 1 — install without files (18 h)

ARCHITECTURE §2–§4.

- Interactive installer reading from `/dev/tty`; `--config`/`--yes` non-interactive path kept.
- Key reuse from `authorized_keys`, session-key detection, fingerprint confirmation; guided paste
  when none.
- Lockout guard: dual access during transition, journal-based detection of the confirmation login,
  safe timeout/abort path.
- Laptop command (`sh -c`), idempotent marker block, backup, alias collision handling.
- `dermaga setup-token`, `needs-setup` state, `/setup` page (password, TOTP enrolment, hook domain
  with DNS check), `dermaga panel` reprints access info.
- **G24 (moved from Phase 0, decision D-c):** Settings forms to change the admin password and to
  enable/disable TOTP, each requiring re-authentication (current password + current TOTP code when
  TOTP is on). Replaces v1 ADR D54 (socket-only); record the new ADR.
- Release workflow attaches `install.sh` to releases; README "Before you start" section (provider
  SSH key, how to create one, root-password-only case).

**Acceptance (VM, from `fresh`, each run separately):** (a) provider key present → zero questions
answered except Enter; (b) no key → guided paste works; (c) wrong key pasted → user cannot finish
confirmation, old access still works, re-run succeeds; (d) confirmation window closed mid-wait →
old access still works; (e) port scan during the install never shows 7070; (f) laptop command pasted
twice in bash and in fish → one block; (g) `ssh dermaga` + browser → `/setup` → panel; (h) reboot.

---

## Phase 2 — GUI completeness (20 h)

ARCHITECTURE §6. Build a parity matrix: every CLI command and flag → the UI control that does the
same. Fill every hole (Domains & Middleware and Settings tabs if not finished in Phase 0, Settings →
Access, backup settings, notification settings, database actions, audit log filters).

Deferred from Phase 0 (decision D-e, 2026-11-12), all required here: **G23** global defaults
(limits, headers, rate limit) stored and editable; **G29** wizard creates `image` apps; **G32**
database "pull and restart"; **G33** CPU/RAM charts and sparklines; parity partials from
`docs/v2/P0-AUDIT.md` §5: `deploy --ref`, `logs -n`, extra `link` aliases, `self-update` in the UI.

Upgrade review: actions (checkout v7, setup-go v7, action-gh-release v3), @sveltejs/kit 3.x, vite 8.

**Acceptance:** the parity matrix, each row exercised on the VM through the UI (screenshots or
recorded API calls from the browser's network log), with no row requiring the CLI. Header edits in
the UI verified with `curl -sI` before/after.

---

## Phase 3 — GitHub App (22 h)

ARCHITECTURE §5.

1. **Spike (stop and report):** manifest flow with `localhost` redirect/setup URLs on real GitHub,
   conversion call, installation redirect, one installation token scoped to one repo, a fetch with
   the token passed via environment config. I do the GitHub clicks.
2. Implementation: storage (encrypted), JWT, token cache, repo/branch/inspect endpoints, wizard repo
   picker, `/hooks/github-app` with all v1 webhook checks, installation event handling, disconnect.
3. Deploy-key source stays under "Advanced".

**Acceptance:** private repo deployed from the picker; push → live; push to another branch ignored;
replayed delivery deduped; bad signature 401 before parsing; token never appears in `ps`, logs,
`.git/config` or the DB in plain text (show the greps); webhook for an unknown repo → 202 ignored;
uninstalling the app on GitHub is reflected in the panel.

---

## Phase 4 — templates (10 h)

ARCHITECTURE §7. Catalog, form, deploy as `image` app.

**Acceptance:** each template deployed on the VM from the UI, reachable on its domain, data survives
a restart; entries that fail are removed, not shipped.

---

## Phase 5 — Railpack (16 h, gated)

ARCHITECTURE §9. Spike first, report, wait for my go/no-go. If go: on-demand rootless BuildKit,
build limits, cache cap, wizard option for repos without a Containerfile.

**Acceptance:** a Node app and a Go app without Containerfiles deploy through the normal pipeline;
idle RSS unchanged (before/after numbers); a failing Railpack build leaves the live release serving.

---

## Phase 6 — Tailscale (8 h, optional)

Only if I approve after Phase 5. BRIEF §2.4 conditions, ARCHITECTURE §10.

---

## Integration harness

`test/vm/*.sh` scenarios over `ssh dermaga-vm`, idempotent, from v1 PLAN. v2 adds:
`install-fresh-provider-key.sh`, `install-fresh-no-key.sh`, `install-lockout-wrong-key.sh`,
`laptop-command-idempotent.sh`, `github-app-push.sh` (needs me), `template-<name>.sh`. Each scenario
prints `date -Is`, `hostname`, `git rev-parse --short HEAD` first.
