# Evidence: M5 (0.5.0) — Connect GitHub

Commit under review for VM run: `23486bc` (`v0.5.0-rc.1`).

## Acceptance criteria

| Criterion (req ID) | Status | Log file | Excerpt (≤20 lines) |
| --- | --- | --- | --- |
| GHA-1, GHA-2, DEC-09: Manifest flow, min permissions, `PATCH /app/hook/config`, `2026-03-10` header | PASS | `tmp/evidence/M5/logs/make-check-m5a.log` | `TestBuildManifest_GHA1_GHA2`, `TestSignAppJWT_RS256`, `TestFakeGitHub_ManifestWebhookInstallReposAndToken_GHA1_GHA5_DEC09` |
| GHA-3–GHA-6, SEC-3, SEC-9: Session-bound `state`, encrypted secrets, installations/repos sync, typed disconnect | PASS | `tmp/evidence/M5/logs/make-check-m5a.log` | `TestSettingsGitHubFlow_GHA1_GHA6_SEC3_SEC9`, `TestStoreMigration0002To0003` |
| UI-1, UI-8, NFR-7: Settings → GitHub (desktop + narrow, light + dark, axe WCAG AA) | PASS | `tmp/evidence/M5/logs/playwright-e2e-m5a.log` | `7 passed (19.9s)` (Firefox + axe WCAG AA) |
| SEC-11–SEC-14, GHA-7, GHA-8, DEC-30: `hook-relay` Quadlet, `hooks.sock` webhook unit/reconciler tests | PASS | `tmp/evidence/M5/logs/make-check-m5b.log` | `TestHookRelayGolden_DEC30`, `TestReconcile_HookRelayLifecycle_DEC30_GHA6`, `TestWebhookSecurity_SEC11_SEC12_SEC13_SEC14_GHA8`, `TestWebhookInstallationEvents_GHA7` |
| Release `v0.5.0-rc.1` tag, workflow & checksums | PASS | `tmp/evidence/M5/logs/release-rc1-verify.log` | `23486bc0a7e825d6840404ffa569e9ba3fcd78fd`, `sha256sum -c checksums.txt` (`OK`) |
| Release `v0.5.0` tag, workflow, checksums + tamper negative control & `install.sh` diff | PASS | `tmp/evidence/M5/logs/release-v050-verify.log` | `e985cbd9ecb98e77d86b3acce66912eea88c4218`, tamper `FAILED`, `install.sh` diff only `DERMAGA_VERSION` |
| M5b spike: `hook-relay` (`Rootfs=` empty dir, `ReadOnly=true`, `DropCapability=ALL`, `NoNewPrivileges=true`) | PASS | `tmp/evidence/M5/logs/vm-m5b-hook-relay-spike.log` | `active`, `Hook relay proxy forwarding :8080 -> /run/dermaga/hooks.sock`, `ReadOnly=true`, `SecurityOpt=[no-new-privileges]` |
| M5b spike: `hook-relay` reboot survival | FAIL | — | Invalid: spike used `virsh reboot` (forbidden by `SETUP.md` §3.2); superseded by safe reboot test below |
| VM `hook-relay` live security (`Uid=1001` host / container UID 0, `CapEff=0`, `NoNewPrivs=1`, `01-hook-domain.yml`) | PASS | `tmp/evidence/M5/logs/vm-m5b-relay-and-state.log` | `CapEff: 0000000000000000`, `NoNewPrivs: 1`, `http://hook-relay:8080` |
| VM real GitHub App + smee push + Pebble HTTPS replay/dedupe + bad-sig `401` (`GHA-1`, `GHA-3`, `GHA-8`, `SEC-11`, `SEC-13`) | PASS | `tmp/evidence/M5/logs/vm-m5b-webhook-delivery.log` | `smee->hooks.sock status=202`, `pebble-https->relay->hooks.sock replay status=202`, `bad-sig status=401`, 1 deduped row |
| VM `GHA-7` repo removal reflected via panel **Refresh** + `m3_check.sh` (`control`, `state`, `running`, `access` last) | PASS | `tmp/evidence/M5/logs/vm-m5b-final-checks.log` | `PASS: GHA-7 removal of r3p-dev/dermaga-test-app reflected in panel`, `all checks passed` |
| VM `GHA-7` live webhook-path (`installation_repositories` removed, `installation` `suspend`/`unsuspend`) | PASS | `tmp/evidence/M5/logs/vm-step0-gha7-suspend.log`, `tmp/evidence/M5/logs/vm-step0-gha7-unsuspend.log` | `v0.5.0` from `fresh` + `gosmee` -> `hooks.sock`: `removed` (`dermaga-test-empty` deleted), `suspend` (`suspended_at=2026-11-25T14:18:34Z`), `unsuspend` (`suspended_at=None`) reflected in DB, `webhook_deliveries`, and `audit_logs` before any Refresh |
| VM `v0.5.0` smoke run from `fresh` (hook-domain blocker without `hook_domain` + `m3_check.sh access` last) | PASS | `tmp/evidence/M5/logs/vm-v050-smoke.log` | `POST /api/settings/github/manifest` -> `400`, `all checks passed` |
| VM secret greps (journal, DB, `dermaga` cmdlines) & safe reboot (`virsh shutdown` + `virsh start`) | PASS | `tmp/evidence/M5/logs/vm-m5b-greps.log` | `0 hits in journal`, `0 plaintext secret hits in DB`, `0 secret hits in dermaga cmdlines`, all 3 units `active` after reboot (`vm-m5b-reboot-and-greps.log`) |

### `tmp/evidence/M5/logs/make-check-m5b.log`

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

### `tmp/evidence/M5/logs/vm-m5b-relay-and-state.log`

```text
2026-11-25T13:19:27+00:00
dermaga-vm
active
Hook relay proxy forwarding :8080 -> /run/dermaga/hooks.sock
hook-relay host pid=4121
Uid:	1001	1001	1001	1001
Gid:	1001	1001	1001	1001
CapInh:	0000000000000000
CapPrm:	0000000000000000
CapEff:	0000000000000000
CapBnd:	0000000000000000
CapAmb:	0000000000000000
NoNewPrivs:	1
```

### `tmp/evidence/M5/logs/vm-m5b-webhook-delivery.log`

```text
smee->hooks.sock status=202 body={"status":"accepted"}
pebble-https->relay->hooks.sock replay status=202 body={"status":"accepted"}
negative-control bad-sig status=401
webhook_deliveries: [('568985fe-e8d3-11f1-8162-5708322608b1', 'push', 'r3p-dev/dermaga-test-app', 'accepted')]
# Exit: 0
```

### `tmp/evidence/M5/logs/vm-m5b-greps.log`

```text
active
active
active
github_app_pem_encrypted len= 2276 encrypted= True
github_client_secret_encrypted len= 92 encrypted= True
github_webhook_secret_encrypted len= 124 encrypted= True
0 plaintext secret hits in DB
0 secret hits in dermaga cmdlines
# Exit: 0
```

### `tmp/evidence/M5/logs/vm-step0-gha7-unsuspend.log`

```text
2026-11-25T14:20:32+00:00
dermaga-vm
installations: [(185789752, 'r3p-dev', None)]
repos: [(1458365617, 'r3p-dev/dermaga-test-app')]
deliveries: [('044f05f4-e8db-11f1-87b7-7d6b682a0ff8', 'installation_repositories', 'removed', '', 'accepted', '2026-11-25T14:17:59Z'), ('192daa2a-e8db-11f1-8500-3ae4b3dfed13', 'installation', 'suspend', '', 'accepted', '2026-11-25T14:18:35Z'), ('55ab91ce-e8db-11f1-8347-4dae8e8ac44c', 'installation', 'unsuspend', '', 'accepted', '2026-11-25T14:20:17Z')]
audit_logs: [('webhook', 'github_installation_unsuspended', 'github', 'GitHub App installation 185789752 (r3p-dev) unsuspended', '2026-11-25T14:20:17Z'), ('webhook', 'github_installation_suspended', 'github', 'GitHub App installation 185789752 (r3p-dev) suspended', '2026-11-25T14:18:35Z'), ('webhook', 'github_repos_removed', '044f05f4-e8db-11f1-87b7-7d6b682a0ff8', 'Removed 1 repository access(es) from installation 185789752', '2026-11-25T14:17:59Z'), ...]
# Exit: 0
```

## Run & view

- Local checks: `make check` and `make build && (cd ui && EVIDENCE_MILESTONE=M5 bunx playwright test)`.
- Panel: `http://localhost:7070/settings?tab=github`.

## Screens

- `tmp/evidence/M5/screenshots/m5a-github-connect-light-desktop.png`: Settings → GitHub (`1280×800`, light), guided 3-step Connect GitHub card with webhook URL `https://hooks.example.com/hooks/github-app`.
- `tmp/evidence/M5/screenshots/m5a-github-connected-light-desktop.png`: Settings → GitHub (`1280×800`, light), connected App summary (`Dermaga VM`, App ID `12345`), installations (`r3p-dev`), and accessible repositories table (`r3p-dev/dermaga-test-app`).
- `tmp/evidence/M5/screenshots/m5a-github-connected-dark-narrow.png`: Settings → GitHub (`375×812`, dark), connected App card and stacked layout with zero horizontal overflow.

## How it works

1. **Manifest start (`POST /api/settings/github/manifest`)**: Requires `hook_domain` to be set; mints a 32-byte single-use `state` bound to the active session ID and returns the GitHub form action URL (`https://github.com/settings/apps/new?state=...`) and manifest JSON (`Contents: read`, `Metadata: read`, `push` event, private App, callbacks `/settings/github/callback` and `/settings/github/install-callback`).
2. **Callback (`GET /settings/github/callback` → `POST /api/settings/github/callback`)**: Because session cookies use `SameSite=Strict` (`SEC-7`), the cross-site redirect from GitHub lands on the SvelteKit SPA route, which sends a same-origin `POST` with `{code, state}` (carrying both the session cookie and `Origin` header for `SEC-3`). The daemon validates and consumes `state`, exchanges `code` via `POST /app-manifests/{code}/conversions`, generates a 32-byte webhook secret, pushes it via `PATCH /app/hook/config` (`content_type: "json"`), stores the PEM key, client secret, and webhook secret AES-256-GCM encrypted (`GHA-4`, `SEC-9`), and calls `Reconciler.EnsureGitHubRelay` to start `hook-relay.service` and wire `01-hook-domain.yml`.
3. **Install & sync (`GHA-3`, `GHA-5`, `GHA-6`)**: The browser redirects to GitHub's App installation URL and returns to `/settings/github/install-callback`, which calls `POST /api/settings/github/sync`. The daemon lists installations with an RS256 App JWT and fetches each installation's repositories using an ephemeral in-memory installation token.
4. **Webhook intake (`GHA-7`, `GHA-8`, `SEC-11`–`SEC-14`)**: Traefik routes `POST /hooks/github-app` on `hook_domain` to `hook-relay:8080`, which forwards raw bytes (capped at `25 MiB + 64 KiB`) to `$XDG_RUNTIME_DIR/dermaga/hooks.sock` (`0600`). The daemon enforces `http.MaxBytesReader` (25 MiB → `413`), verifies `X-Hub-Signature-256` before JSON parsing (`401` with oracle-safe logging), dedupes on `X-GitHub-Delivery` (`202`), rate-limits per repo (`429`), filters to `push` / `installation` / `installation_repositories`, and updates DB state for `GHA-7` and `last_delivery` (`GHA-8`).
5. **Disconnect (`DELETE /api/settings/github`)**: Requires typed `"disconnect"` confirmation, clears stored GitHub credentials, installations, repositories, and deliveries, removes `hook-relay.container` and `01-hook-domain.yml`, and returns the GitHub App advanced settings link for manual deletion on GitHub.

## Code tour

- `internal/store/migrations/0003_github.sql`, `internal/store/github.go`: `github_installations`, `github_repositories`, `webhook_deliveries` tables and persistence methods.
- `internal/github/github.go`: GitHub App manifest builder, RS256 JWT signer, REST client (`X-GitHub-Api-Version: 2026-03-10`).
- `internal/quadlet/hook_relay.go`, `internal/reconciler/reconciler.go`: `hook-relay.container` (`Rootfs=` empty dir, `DEC-30`) and `01-hook-domain.yml` lifecycle.
- `internal/webhook/webhook.go`: `hooks.sock` HTTP server enforcing `SEC-11`–`SEC-14` and `GHA-7`–`GHA-8`.
- `internal/server/github.go`, `internal/server/types.go`, `internal/doctor/doctor.go`: GitHub settings API handlers, single-use session-bound `state`, and `doctor` check.
- `ui/src/lib/components/GitHubSettings.svelte`, `ui/src/routes/settings/github/`: Settings → GitHub UI and callback routes.

## Decisions and deviations

- **DEC-29** (`docs/ARCHITECTURE.md` §5.2, §6.3, §7): Aligned Connect GitHub with PRD `GHA-1` under **Settings → GitHub** (`/settings/github/callback` and `/settings/github/install-callback`), blocking Connect GitHub until `hook_domain` is configured.
- **DEC-30** (`docs/ARCHITECTURE.md` §7, `docs/DEPENDENCIES.md` §3): `hook-relay.container` uses `Rootfs=` on an empty directory with read-only `dermaga` binary (no base image or external digest), `ReadOnly=true`, `NoNewPrivileges=true`, `DropCapability=ALL` (`CapEff=0`), `[Quadlet] DefaultDependencies=false` (`DEC-06`), and `After=dermaga.service`. Inside its rootless user namespace, `hook-relay` runs as container UID `0` (observed as host `Uid=1001`, `server`, in `/proc/<pid>/status`) because without `UserNS=keep-id`, non-zero container UIDs map to subordinate UIDs and cannot connect to `hooks.sock` (`0600` owned by `server`); this is acceptable because `CapEff=0000000000000000`, `NoNewPrivs=1`, `ReadOnly=true`, empty `Rootfs=` (no shell or writable paths), and only `web` network + `:ro` mounts are present.
- **SameSite=Strict callback handoff**: Cross-site redirects from `github.com` omit `SameSite=Strict` session cookies, so `/settings/github/callback` and `/settings/github/install-callback` are SPA pages that immediately issue same-origin `POST` requests with `Origin` and the session cookie.
