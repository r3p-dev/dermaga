# Evidence: M5 (0.5.0) — Connect GitHub

Checkpoint **M5a** (App connect + repositories) verified locally against fake GitHub and Playwright/axe. Checkpoint **M5b** (webhooks, `hook-relay`, and real GitHub VM run) is pending.

## Acceptance criteria

| Criterion (req ID) | Status | Log file | Excerpt (≤20 lines) |
| --- | --- | --- | --- |
| GHA-1, GHA-2, DEC-09: Manifest flow, min permissions, `PATCH /app/hook/config`, `2026-03-10` header | PASS | `tmp/evidence/M5/logs/make-check-m5a.log` | `TestBuildManifest_GHA1_GHA2`, `TestSignAppJWT_RS256`, `TestFakeGitHub_ManifestWebhookInstallReposAndToken_GHA1_GHA5_DEC09` |
| GHA-3–GHA-6, SEC-3, SEC-9: Session-bound `state`, encrypted secrets, installations/repos sync, typed disconnect | PASS | `tmp/evidence/M5/logs/make-check-m5a.log` | `TestSettingsGitHubFlow_GHA1_GHA6_SEC3_SEC9`, `TestStoreMigration0002To0003` |
| UI-1, UI-8, NFR-7: Settings → GitHub (desktop + narrow, light + dark, axe WCAG AA) | PASS | `tmp/evidence/M5/logs/playwright-e2e-m5a.log` | `7 passed (19.9s)` (Firefox + axe WCAG AA) |
| M5b: `hook-relay` Quadlet (`Rootfs=` VM spike), `hooks.sock`, `GHA-7`, `GHA-8`, `SEC-11`–`SEC-14`, real GitHub VM run | NOT RUN | — | Pending M5b |

### `tmp/evidence/M5/logs/make-check-m5a.log`

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

### `tmp/evidence/M5/logs/playwright-e2e-m5a.log`

```text
Running 7 tests using 4 workers
  ✓  2 [firefox] › e2e/panel.test.ts:87:3 › Dermaga Theme & Panel Journeys (M1-dark) › system preference prefers-color-scheme: dark produces fully dark page without saved choice (2.8s)
  ✓  5 [firefox] › e2e/panel.test.ts:106:3 › Dermaga Theme & Panel Journeys (M1-dark) › unauthenticated access redirects to /login (1.3s)
  ✓  4 [firefox] › e2e/settings.test.ts:29:3 › Settings & Narrow Audit Log (M4b: LAP-3, ADM-4, ADM-5, SEC-10, CONVENTIONS §5.2) › Settings tabs (Access, Security, Hook Domain) and narrow Audit Log stacked rows (7.8s)
  ✓  1 [firefox] › e2e/github.test.ts:29:3 › Settings -> GitHub (M5a: GHA-1, GHA-3, GHA-6, GHA-8) › hook domain blocker, guided connect card, connected repos desktop+narrow light+dark, and typed disconnect (7.7s)
  ✓  3 [firefox] › e2e/setup.test.ts:137:3 › First-Run Setup Journey (M4a: SETUP-1, SETUP-2, SETUP-3, SEC-8) › full setup with TOTP enrolment, theme/viewport axe AA checks, and TOTP login (8.9s)
  ✓  7 [firefox] › e2e/setup.test.ts:225:3 › First-Run Setup Journey (M4a: SETUP-1, SETUP-2, SETUP-3, SEC-8) › skipping TOTP shows persistent warning banner in panel (2.2s)
  ✓  6 [firefox] › e2e/panel.test.ts:112:3 › Dermaga Theme & Panel Journeys (M1-dark) › full panel journeys, theme switching, computed backgrounds, and axe AA contrast (12.8s)
  7 passed (19.9s)
# Exit: 0
```

## Run & view

- Local checks: `make check` and `make build && (cd ui && EVIDENCE_MILESTONE=M5 bunx playwright test)`.
- Panel: `http://localhost:7070/settings?tab=github`.

## Screens

- `tmp/evidence/M5/screenshots/m5a-github-connect-light-desktop.png`: Settings → GitHub (`1280×800`, light), guided 3-step Connect GitHub card with webhook URL `https://hooks.example.com/hooks/github-app`.
- `tmp/evidence/M5/screenshots/m5a-github-connected-light-desktop.png`: Settings → GitHub (`1280×800`, light), connected App summary (`Dermaga VM`, App ID `12345`), installations (`r3p-dev`), and accessible repositories table (`r3p-dev/dermaga-test-app`).
- `tmp/evidence/M5/screenshots/m5a-github-connected-dark-narrow.png`: Settings → GitHub (`375×812`, dark), connected App card and stacked layout with zero horizontal overflow.

## How it works (M5a)

1. **Manifest start (`POST /api/settings/github/manifest`)**: Requires `hook_domain` to be set; mints a 32-byte single-use `state` bound to the active session ID and returns the GitHub form action URL (`https://github.com/settings/apps/new?state=...`) and manifest JSON (`Contents: read`, `Metadata: read`, `push` event, private App, callbacks `/settings/github/callback` and `/settings/github/install-callback`).
2. **Callback (`GET /settings/github/callback` → `POST /api/settings/github/callback`)**: Because session cookies use `SameSite=Strict` (`SEC-7`), the cross-site redirect from GitHub lands on the SvelteKit SPA route, which sends a same-origin `POST` with `{code, state}` (carrying both the session cookie and `Origin` header for `SEC-3`). The daemon validates and consumes `state`, exchanges `code` via `POST /app-manifests/{code}/conversions`, generates a 32-byte webhook secret, pushes it via `PATCH /app/hook/config` (`content_type: "json"`), and stores the PEM key, client secret, and webhook secret AES-256-GCM encrypted (`GHA-4`, `SEC-9`).
3. **Install & sync (`GHA-3`, `GHA-5`, `GHA-6`)**: The browser redirects to GitHub's App installation URL and returns to `/settings/github/install-callback`, which calls `POST /api/settings/github/sync`. The daemon lists installations with an RS256 App JWT and fetches each installation's repositories using an ephemeral in-memory installation token.
4. **Disconnect (`DELETE /api/settings/github`)**: Requires typed `"disconnect"` confirmation, clears stored GitHub credentials, installations, repositories, and deliveries, and returns the GitHub App advanced settings link for manual deletion on GitHub.

## Code tour

- `internal/store/migrations/0003_github.sql`, `internal/store/github.go`: `github_installations`, `github_repositories`, `webhook_deliveries` tables and persistence methods.
- `internal/github/github.go`: GitHub App manifest builder, RS256 JWT signer, REST client (`X-GitHub-Api-Version: 2026-03-10`).
- `internal/server/github.go`, `internal/server/types.go`, `internal/doctor/doctor.go`: GitHub settings API handlers, single-use session-bound `state`, and `doctor` check.
- `ui/src/lib/components/GitHubSettings.svelte`, `ui/src/routes/settings/github/`: Settings → GitHub UI and callback routes.

## Decisions and deviations

- **DEC-29** (`docs/ARCHITECTURE.md` §5.2, §6.3, §7): Aligned Connect GitHub with PRD `GHA-1` under **Settings → GitHub** (`/settings/github/callback` and `/settings/github/install-callback`), blocking Connect GitHub until `hook_domain` is configured.
- **SameSite=Strict callback handoff**: Cross-site redirects from `github.com` omit `SameSite=Strict` session cookies, so `/settings/github/callback` and `/settings/github/install-callback` are SPA pages that immediately issue same-origin `POST` requests with `Origin` and the session cookie.
