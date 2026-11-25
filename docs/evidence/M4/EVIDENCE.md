# Evidence: M4 (0.4.0) — First-run setup and panel access

Commit under review: `f0e5736` + M4b working tree (checkpoint **M4b**; M4a confirmed on `v0.4.0-rc.1`).

## Acceptance criteria

| Criterion (req ID) | Status | Log file | Excerpt (≤20 lines) |
| --- | --- | --- | --- |
| SETUP-1: setup mode redirect & API refusal | PASS | `tmp/evidence/M4/logs/make-check.log` | `TestSetupModeEnforcement_SETUP1_SEC3` |
| SEC-8: 32B hashed single-use token, 24 h expiry, rate-limited, SEC-3 | PASS | `tmp/evidence/M4/logs/make-check.log` | `TestSetupTokenLifecycle_SEC8`, `TestSetupFlowWithTOTP_SETUP2_SEC8_ADM1` |
| SETUP-2: password + strength meter + skippable TOTP QR & warning | PASS | `tmp/evidence/M4/logs/playwright-e2e.log` | `5 passed (16.3s)` (Firefox + axe WCAG AA) |
| SETUP-2, APP-2: hook domain DNS check, `/setup` Step 3, `01-hook-domain.yml` | PASS | `tmp/evidence/M4/logs/make-check-m4b.log` | `TestCheckDomainDNS_SETUP2`, `TestRenderHookDomain_APP2_TLS5`, `TestSettingsHookDomain_SETUP2_TLS5` |
| LAP-3, ADM-5: Settings → Access, active sessions (`0002` migration) | PASS | `tmp/evidence/M4/logs/make-check-m4b.log` | `TestStoreMigration0001To0002`, `TestSettingsAccessAndSessions_LAP3_ADM5` |
| ADM-4, SEC-10: password change ends other sessions; TOTP enable/disable re-auth | PASS | `tmp/evidence/M4/logs/make-check-m4b.log` | `TestSettingsSecurityPasswordAndTOTP_ADM4_SEC10` |
| CONVENTIONS §5.2: narrow-screen stacked Audit Log rows | PASS | `tmp/evidence/M4/logs/playwright-e2e-m4b.log` | `6 passed` (`hasOverflow=false`, axe WCAG AA) |
| TLS-5, DEC-20: Pebble ACME without e-mail on `dermaga-vm` | NOT RUN | — | Needs owner pre-release tag `v0.4.0-rc.2` for VM run |
| DOC-2: `doctor` warns when TOTP is disabled | PASS | `tmp/evidence/M4/logs/make-check.log` | `TestDoctorTOTPWarning_DOC2` |
| SETUP-3, INST-12: `dermaga panel` via `daemon.sock` + `install.sh` link | PASS | `tmp/evidence/M4/logs/vm-run1-install.log` | `install.sh` printed first-run link; `dermaga panel` reissue & daemon-down failure verified |
| VM fresh-install journey (TOTP, token rules, reboot, secret grep, `m3_check.sh`) | PASS | `tmp/evidence/M4/logs/vm-run1-journey.log` | `v0.4.0-rc.1` from `fresh`; TOTP setup/login, token rules, reboot, 0 secret hits, `m3_check` |
| VM skip-TOTP journey (`dermaga-vm` from `fresh` revert #2) | PASS | `tmp/evidence/M4/logs/vm-run2-skip-totp.log` | Panel warning banner + `[WARN] totp` in `dermaga doctor` |
| Release `v0.4.0-rc.1` tag, workflow & checksums | PASS | `tmp/evidence/M4/logs/release-rc1-verify.log` | `sha256sum -c checksums.txt` (`OK`) |
| QR library query (`skip2/go-qrcode` vs `rsc.io/qr`) | decision recorded | `tmp/evidence/M4/logs/qr-library-query.log` | `DEPENDENCIES.md` §1–2 (unmaintained, encode-only acceptable) |

### `tmp/evidence/M4/logs/make-check.log`

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

### `tmp/evidence/M4/logs/playwright-e2e.log`

```text
  ✓  1 [firefox] › e2e/panel.test.ts:87:3 › Dermaga Theme & Panel Journeys (M1-dark) › system preference prefers-color-scheme: dark produces fully dark page without saved choice (2.4s)
  ✓  3 [firefox] › e2e/panel.test.ts:106:3 › Dermaga Theme & Panel Journeys (M1-dark) › unauthenticated access redirects to /login (652ms)
  ✓  2 [firefox] › e2e/setup.test.ts:137:3 › First-Run Setup Journey (M4a: SETUP-1, SETUP-2, SETUP-3, SEC-8) › full setup with TOTP enrolment, theme/viewport axe AA checks, and TOTP login (6.5s)
  ✓  5 [firefox] › e2e/setup.test.ts:219:3 › First-Run Setup Journey (M4a: SETUP-1, SETUP-2, SETUP-3, SEC-8) › skipping TOTP shows persistent warning banner in panel (2.1s)
  ✓  4 [firefox] › e2e/panel.test.ts:112:3 › Dermaga Theme & Panel Journeys (M1-dark) › full panel journeys, theme switching, computed backgrounds, and axe AA contrast (11.6s)

  5 passed (16.3s)
# Exit: 0
```

### `tmp/evidence/M4/logs/release-rc1-verify.log`

```text
95a5c06dce7ed8c5e91458cb94d07fe0c95273b8
completed	success	docs(m4): update plan, architecture, changelog, and M4a evidence	Release	v0.4.0-rc.1	push	45570396747	2m54s	2026-11-25T07:43:40Z
v0.4.0-rc.1
dermaga-linux-amd64: OK
dermaga-linux-arm64: OK
install.sh: OK
# Exit: 0
```

### `tmp/evidence/M4/logs/vm-run1-journey.log`

```text
    tcp dport { 4222 } accept comment "ssh"
    tcp dport { 80, 443 } accept comment "http, https"
    udp dport 443 accept comment "http/3"
PASS  firewall rules open only ssh, 80, 443/tcp, 443/udp
PASS  old port 22 closed
PASS  panel port 7070 closed from outside (SEC-2)
PASS  password login not offered to server
PASS  root refused, even with the key
PASS  provider user debian refused, even with the key
all checks passed
# Exit: 0
```

### `tmp/evidence/M4/logs/vm-run2-skip-totp.log`

```text
PASS: browser skip-TOTP path showed persistent warning banner
[OK  ] lingering      enabled for server
[OK  ] podman         version 5.4.2
[OK  ] traefik        traefik.service is active
[OK  ] clock          synchronised
[OK  ] disk           8% used
[WARN] totp           two-factor authentication is disabled
                      Enable TOTP in Settings -> Security to protect admin login.
PASS: dermaga doctor warned about disabled TOTP (DOC-2)
# Exit: 0
```

## Run & view

- Local checks: `make check` and `make build && (cd ui && EVIDENCE_MILESTONE=M4 bunx playwright test)`.
- Demo state script: `test/vm/seed.sh` verifies `dermaga.service` and `traefik.service`, runs `dermaga doctor`, and runs `dermaga panel` to print the SSH tunnel command and active setup/panel URL.
- In browser (`http://localhost:7070`): while uninitialized, `/` and `/login` redirect to `/setup`; pasting or opening `?token=...` exchanges the single-use token, sets the admin password, and offers TOTP QR enrolment (or skip with a persistent warning banner).

## Screens

| Local file | Page | Width | Theme | Shows |
| --- | --- | --- | --- | --- |
| `tmp/evidence/M4/screenshots/m4a-setup-token-light-desktop.png` | `/setup` (token) | 1280 | light | Token paste form with PRD `dermaga panel` guidance |
| `tmp/evidence/M4/screenshots/m4a-setup-password-light-desktop.png` | `/setup` (step 1) | 1280 | light | 3-step indicator, password fields, 4-segment green `Strong` meter |
| `tmp/evidence/M4/screenshots/m4a-setup-totp-light-desktop.png` | `/setup` (step 2) | 1280 | light | Inline QR code SVG, copyable Base32 key, `otpauth://` URI |
| `tmp/evidence/M4/screenshots/m4a-setup-totp-dark-narrow.png` | `/setup` (step 2) | 375 | dark | Responsive 3-step header and crisp QR code on dark surface |
| `tmp/evidence/M4/screenshots/m4a-login-totp-dark-desktop.png` | `/login` (TOTP) | 1280 | dark | Password + 6-digit TOTP authenticator prompt |
| `tmp/evidence/M4/screenshots/m4a-dashboard-totp-warning-light.png` | `/` (post-skip) | 1280 | light | Persistent amber TOTP-disabled warning and hook-domain notice |
| `tmp/evidence/M4/screenshots/m4b-setup-hook-domain-light-desktop.png` | `/setup` (step 3) | 1280 | dark | Step 3 Hook Domain form with DNS record expectation and skip button |
| `tmp/evidence/M4/screenshots/m4b-settings-access-light-desktop.png` | `/settings` (Access) | 1280 | light | Laptop/mobile SSH instructions and Active Sessions list with Current badge |
| `tmp/evidence/M4/screenshots/m4b-settings-security-light-desktop.png` | `/settings` (Security) | 1280 | light | Change Admin Password and TOTP setup forms |
| `tmp/evidence/M4/screenshots/m4b-settings-hook-domain-dark-desktop.png` | `/settings` (Hook Domain) | 1280 | dark | Reserved Hook Domain input with Check DNS and Save buttons |
| `tmp/evidence/M4/screenshots/m4b-audit-logs-narrow-dark.png` | `/audit-logs` | 375 | dark | Stacked card rows on narrow screen with zero horizontal scroll |

## How it works

```mermaid
sequenceDiagram
  participant CLI as dermaga panel
  participant Sock as daemon.sock
  participant UI as /setup (SvelteKit)
  participant API as Server (:7070)
  participant DB as SQLite + master.key

  CLI->>Sock: setup-token
  Sock->>DB: store SHA-256(token), 24h expiry
  Sock-->>CLI: raw token (once)
  UI->>API: POST /api/setup/verify-token (strips ?token= from URL)
  API->>DB: constant-time compare, delete token (single-use)
  API-->>UI: Set-Cookie: dermaga_setup_session (HttpOnly, SameSite=Strict)
  UI->>API: POST /api/setup/password
  API->>DB: argon2id hash -> admin_password_hash (state -> admin_created)
  UI->>API: POST /api/setup/totp/init
  API->>DB: encrypt pending secret with master.key
  API-->>UI: secret + otpauth:// URI + QR SVG
  UI->>API: POST /api/setup/totp/verify (or /skip)
  API->>DB: verify +/-1 skew + replay, clear setup session, create admin session
  API-->>UI: Set-Cookie: dermaga_session -> redirect /
```

## Code tour

1. `internal/auth/setup.go`: setup state machine (`uninitialized` → `admin_created` → `domain_set` → `ready`), 32-byte setup token issuance/exchange (`SEC-8`), setup session validation.
2. `internal/auth/totp.go`: stdlib RFC 6238 TOTP generation/verification (±1 skew, replay step tracking) and `go-qrcode` SVG rendering.
3. `internal/server/setup.go`: `/api/setup/*` handlers, setup session cookie helpers, encrypted TOTP enrolment.
4. `internal/server/server.go`: setup-mode redirect middleware, API refusal in setup mode, TOTP check on `/api/auth/login`, `setup-token` over `daemon.sock`.
5. `internal/doctor/doctor.go`: `DOC-2` TOTP warning check.
6. `cmd/dermaga/main.go` & `install/install.sh`: `dermaga panel` setup link via `daemon.sock` and installer summary fallback hint.
7. `ui/src/routes/setup/+page.svelte`, `login/+page.svelte`, `+layout.svelte`: setup wizard, URL token stripping, TOTP login, persistent warnings.

## Decisions & deviations

- **ARCHITECTURE §5.2 & §6.3:** Updated §5.2 step 2 from mandatory/`SEC-1` to skippable TOTP with persistent warning (`SETUP-2`), and clarified `admin_created` in §6.3 so the panel is usable while hook domain setup remains pending for M4b.
- **QR dependency (`DEPENDENCIES.md`):** Added `github.com/skip2/go-qrcode` (`v0.0.0-20200617195104-da1b6568686e`, MIT, zero transitive deps) over `rsc.io/qr` (`v0.2.0`, 2018).
- **Token issuance (`SETUP-3`):** `dermaga panel` requests tokens over `daemon.sock` so the daemon remains the single SQLite writer, failing with a clear error when the daemon is down.

## Known limitations / deferred

- **M4b scope:** Hook-domain step + DNS check (`SETUP-2`), `TLS-5` + Pebble ACME without e-mail (`DEC-20`), `LAP-3`, `ADM-4`, `ADM-5`, `SEC-10`, and narrow-screen stacked audit log rows.
- **LLMNR port 5355:** Pre-existing open port on `fresh` before `apply_firewall` (carried over from M3b; deferred).

## Suggested commits

See `scripts/check-commits tmp/evidence/M4/commits.plan`.
