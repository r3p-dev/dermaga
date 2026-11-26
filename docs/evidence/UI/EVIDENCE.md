# Evidence: UI Redesign (Coolify-inspired, pre-M7)

Visual and layout redesign of the dermaga panel inspired by Coolify's layout hierarchy (persistent left sidebar on `md+` with bottom-pinned theme toggle and log out, apps-first Dashboard with compact 4-stat host health strip, vertical sub-navigation on `md+` for App Detail and Settings).

## Verification Summary

| Check | Status | Log file | Notes |
| --- | --- | --- | --- |
| `make check` (`ui`, `lint`, `test`, `vulncheck`, `build-all`, `policy`) | PASS | `tmp/evidence/UI/logs/make-check.log` | All Go + Svelte + tygo + govulncheck + policy checks green |
| `EVIDENCE_MILESTONE=UI make test-browser` (Playwright + axe WCAG AA) | PASS | `tmp/evidence/UI/logs/test-browser.log` | 8/8 tests passed across desktop (1280px) & narrow (375px), light & dark |

### `tmp/evidence/UI/logs/make-check.log`

```text
ok   ui
ok   lint
ok   test
ok   vulncheck
ok   build-all
ok   policy
SKIP shellcheck (not installed)
=== make check passed successfully ===
```

### `tmp/evidence/UI/logs/test-browser.log`

```text
  8 passed (20.7s)
```

## Before / After Screenshots

Raw screenshots are stored in gitignored `tmp/evidence/UI/before/` (baseline) and `tmp/evidence/UI/screenshots/` (redesigned).

| Screen | Width | Theme | Before | After | What changed |
| --- | --- | --- | --- | --- | --- |
| Dashboard | 1280 | light | `tmp/evidence/UI/before/dashboard-desktop-light.png` | `tmp/evidence/UI/screenshots/dashboard-desktop-light.png` | Top bar replaced by left sidebar (bottom-pinned Dark mode & Log out); Applications moved above compact 4-stat Host Health strip; `StatusBadge` emerald pill |
| Dashboard | 375 | dark | `tmp/evidence/UI/before/dashboard-narrow-dark.png` | `tmp/evidence/UI/screenshots/dashboard-narrow-dark.png` | Compact mobile header + nav with theme toggle & Log out at end; apps-first hierarchy without horizontal overflow |
| App Environment | 1280 | light | `tmp/evidence/UI/before/m6b-app-env-light-desktop.png` | `tmp/evidence/UI/screenshots/m6b-app-env-light-desktop.png` | App header with breadcrumb + `StatusBadge` + Deploy CTA; horizontal tabs replaced by vertical `SubNav` sidebar; `UI-5` reveal/hide preserved |
| App Settings | 375 | dark | `tmp/evidence/UI/before/m6b-app-settings-dark-narrow.png` | `tmp/evidence/UI/screenshots/m6b-app-settings-dark-narrow.png` | Horizontal scrollable `SubNav` pill bar on narrow screens; grouped Health Check, Resources & Build cards |
| New App Wizard (Step 4) | 1280 | light | `tmp/evidence/UI/before/m6b-wizard-step4-live-light-desktop.png` | `tmp/evidence/UI/screenshots/m6b-wizard-step4-live-light-desktop.png` | Breadcrumb header, 4-step stepper, semantic `StatusBadge` (`live`), dark terminal log block |
| Settings (Access) | 1280 | light | `tmp/evidence/UI/before/m4b-settings-access-light-desktop.png` | `tmp/evidence/UI/screenshots/m4b-settings-access-light-desktop.png` | Vertical `SubNav` sidebar (`Access`, `Security`, `Hook Domain`, `GitHub`) alongside Laptop & Mobile Access and Active Sessions cards |
| Settings (GitHub) | 1280 | light | `tmp/evidence/UI/before/m5a-github-connected-light-desktop.png` | `tmp/evidence/UI/screenshots/m5a-github-connected-light-desktop.png` | Vertical `SubNav` with connected GitHub App card, Installations, Accessible Repositories, and isolated Disconnect card |
| Audit Log / Activity | 1280 | light | `tmp/evidence/UI/before/audit-desktop-light.png` | `tmp/evidence/UI/screenshots/activity-desktop-light.png` | Renamed to Activity (`/activity`, `/audit-logs` redirects); `PageHeader` with inline Origin/App/Time filters above table; stacked cards on `< md` |
| Apps | 1280 | light | — | `tmp/evidence/UI/screenshots/apps-desktop-light.png` | Dedicated `/apps` route with `AppsList` and `New app` CTA; sidebar `Apps` active on `/apps`, `/apps/new`, `/apps/[id]` |

## Code Tour

1. `ui/src/lib/components/StatusBadge.svelte`: unified semantic status pill (`live`, `Daemon Active`, `failed`, `cancelled`, in-flight states) with WCAG AA contrast.
2. `ui/src/lib/components/SubNav.svelte`: vertical sub-navigation on `md+` and horizontally scrollable pill bar on `< md` (`role="tablist"`).
3. `ui/src/lib/components/PageHeader.svelte`: consistent breadcrumb, title, subtitle, and right-aligned action slot.
4. `ui/src/routes/+layout.svelte`: persistent left sidebar on `md+` (no top utility bar; theme toggle and Log out pinned at bottom), mobile header + nav on `< md`, minimal header on unauthenticated `/login` and `/setup`.
5. `ui/src/routes/+page.svelte` & `ui/src/lib/components/AppsList.svelte`: apps-first Dashboard with compact 4-stat Host Health strip.
6. `ui/src/routes/apps/new/+page.svelte`, `ui/src/routes/apps/[id]/+page.svelte`, `ui/src/routes/settings/+page.svelte`, `ui/src/routes/audit-logs/+page.svelte`: updated to `PageHeader`, `SubNav`, and `StatusBadge`.
