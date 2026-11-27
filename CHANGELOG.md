# Changelog

All notable changes to this project are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and versions follow
[Semantic Versioning](https://semver.org/).

## [Unreleased]

### Added
- **Domains tab (`APP-2`)**: primary domain, aliases and redirects to the primary (301, with a one-click `www` ↔ apex redirect), an app-wide path prefix, make primary and remove; every change is the whole set, validated by the server (no wildcards, `xn--` form for non-ASCII names, not the hook domain, not another app's domain).
- **Safe route changes (`APP-10`)**: a domain change goes live only after Traefik answers through a verify router on the live release; otherwise the previous route file is written back, nothing is saved and Traefik's own error is shown. Refused with "starting or switching traffic" while a release of the app is starting or switching.
- **DNS gate (`TLS-2`)**: a domain is routed, and so gets its certificate requested, only once its DNS points to this server; waiting domains are checked every 5 minutes and on "Check again", with the record to create shown in the panel (DEC-38).
- **Certificate status (`TLS-3`, `TLS-5`)**: active with expiry, waiting for DNS, requesting, or failed with the certificate authority's reason in plain words, its own detail and the next automatic try; the hook domain shows its certificate in Settings → Hook Domain (DEC-41).
- A domain without a certificate whose order failed leaves the route until "Check again" or the hourly retry, because Traefik retries every failed order on any config change (DEC-38).
- **Expiry warnings (`TLS-4`)**: certificates with less than 1/6 of their lifetime left (Traefik renews at 1/3) show on the dashboard and in `dermaga doctor`.
- Migration `0006`: DNS and certificate state per domain; domains that exist before the upgrade stay routed.
- `DERMAGA_DNS_RESOLVER` (test only, set by a drop-in on `dermaga-vm`): DNS checks ask that server; the daemon logs it at start and `doctor` warns while it is set (DEC-44).

### Changed
- Every host of an app now has its own Traefik router and certificate (DEC-39); one domain with wrong DNS or a failed order no longer blocks the certificate of the others. The first sync after an upgrade rewrites existing route files this way, proven through Traefik.
- The switch, the startup reconciler and panel edits build the route with one function (`deploy.RouteSpecFor`, DEC-40).
- The new-app wizard checks the domain's syntax on the server.

### Fixed
- A path prefix shared by several domains appeared once per domain in the strip-prefix middleware.

## [0.7.0] - 2026-11-27

### Added
- **Zero-downtime redeploy (`DEP-1`, `DEP-2`)**: a new release starts next to the live one, is health-checked, and the route moves through three atomic writes, each proven through Traefik with a token the verify router echoes (DEC-33); the old release drains 10 s and stops. Any failure before the switch completes restores the live-only route and removes the candidate.
- **Rollback (`DEP-4`)** to the last 5 releases that were live: no rebuild, the release's own image and immutable encrypted env version (DEC-32); refused while the app builds ("Cancel the running build first").
- **Memory check** before the build and before the start, and up front in the deploy and rollback dialogs.
- **Restart recovery (`DEP-10`)**: after a crash mid-switch the route file decides which release is live; leftover verify routers are removed in place.
- Migration `0005`: env versions, `rolled_back`, per-release runtime config, watch paths; pre-0005 release snapshots are converted at start.
- Panel: Deployments tab with Redeploy (latest or a specific commit) and Roll back, rollback confirmation listing env variable names that differ, live status of the started release.
- App containers run with `RunInit=true` (DEC-34).
- **Push-to-deploy (`APP-8`, `DEP-5`, `UI-7`)**: a verified GitHub push to an app's branch queues a release of that commit; pushes to other branches and tags, deleted branches, pushes with deploy on push off, pushes whose changed files match no watch path, and repeats of a commit that is already queued, building or live are ignored with the plain reason in Activity (DEC-37). A push that lists no files deploys anyway.
- **Watch paths**: one pattern per line in Settings → Deploy on push (a trailing `/` means the whole folder, `*` stays within one folder), validated by the server.
- **Live deployment status**: the app page refreshes every 3 s while the tab is visible, so push-started deployments appear without a reload.
- Settings → GitHub shows the last ignored push and why; the rollback dialog says that the next push will deploy over the rollback while deploy on push is on.
- `test/vm/smoke_ui.mjs`: UI smoke test of a freshly installed VM through an SSH tunnel (first-run setup, Dashboard at 1280/1920/375 in both themes, values compared with the API, no requirement codes).
- `test/vm/m7_invariants.py` (the `seed.sh` M7 check) with a local negative control, `test/vm/m7_invariants_control.sh`.
- `test/vm/seed.sh` for M7: starts Pebble when missing and checks one live release per app, no leftover verify router and containers only for live releases.
- `oha` 1.17.0 and `gosmee` 0.32.1 pinned with checksums for the M7 VM journey (`docs/DEPENDENCIES.md`).

### Changed
- `ui/e2e/panel.test.ts` checks URLs by path, so the browser suite also runs on another port.
- The Dashboard's Version card truncates long development versions on one line; the full value is in the tooltip.
- `test/vm/m3_check.sh access` schedules the fail2ban unban on the VM before the denied logins (`systemd-run` timers at +60 s and +120 s), waits for a new login, and proves the ban from `/var/log/fail2ban.log`. The earlier ControlMaster unban could not work: the nftables ban also rejects packets on the already-open connection.

### Fixed
- Removing an app left its empty folder under `~/.cache/dermaga/builds`, and a daemon killed during a deploy left that deploy's Quadlet dry-run folder there; app removal now deletes the folder and the daemon clears the build scratch folder on start (found on `dermaga-vm` with v0.7.0-rc.4).
- After a crash the startup reconciler left the stopped release's env file (runtime secrets) on disk; orphan units now go with the same cleanup as after a normal switch (`deploy.RemoveReleaseUnit`), stray env files are removed, and the image of a release interrupted by the restart is removed.
- A release adopted after a crash had no log: the log is now saved at every stage line and every 2 s while the release builds, starts and switches.
- A release that lost its traffic just before a restart was stopped at once; it now drains for the same 10 s as after a normal switch.
- Secret values shorter than 4 characters were masked inside unrelated log text (a commit SHA showed `[MASKED]`); they are no longer masked, and the env editor says so under such a value (DEC-36).
- The startup reconciler re-rendered missing app units with an image that never existed and without limits or env file; it now renders exactly what the deploy engine deployed.
- Switch verification failed for every domain whose certificate was still pending (rc.1): Traefik's `sniStrict` refuses the TLS handshake on 443. The verify router now listens on an internal plain-HTTP entrypoint (`:8099` inside the Traefik container, never published) that dermaga reaches through the rootless netns (DEC-33). A binary upgrade applies the new `traefik.yml` itself and restarts Traefik once.
- `test/vm/smoke_ui.mjs` expects the version the VM's binary reports instead of a fixed one.
- `test/vm/setup_pebble.sh` removes dermaga's marker from `traefik.yml`, so a daemon restart can no longer re-render it back to Let's Encrypt during tests.
- `docs/SETUP.md` §4.2 pointed gosmee at the panel, which never serves `/hooks/github-app`; §7 explains why `on_reboot=restart` needs a cold start after reverting a running snapshot.

## [0.6.2] - 2026-11-26

### Changed
- **Full-width panel**: the main content area no longer has a maximum width; it fills the space next to the sidebar with `px-4 md:px-6 lg:px-8` padding, and the app grid gains columns on wide screens (`xl:grid-cols-3 2xl:grid-cols-4`). Login, setup, the GitHub callbacks, the new-app wizard and single forms keep a readable width.
- **Server overview** replaces the Dashboard's "Host Health" strip, showing only values the API returns: status with uptime and "Database: healthy" (schema version in a tooltip), dermaga's own memory (amber above the 30 MB idle budget), the number of apps, and the version. The earlier strip labelled the Go runtime's `Sys`/`Alloc` figures in a way that could be read as host memory; host CPU, RAM and disk move to the M9 System page.
- **No requirement codes in visible panel text** ("Footprint (NFR-2)", "Target < 30 MB", "(AUD-1, AUD-2)", "(APP-5)"–"(APP-9)", "(SRV-1)"); codes stay in comments, CLI output and raw HTTP error bodies.
- The tagline footer is removed; the version stays in the sidebar.
- **Narrow header**: the theme toggle and `Log out` move to the top row so the page links fit at 375 px without clipping.

### Fixed
- Playwright axe checks wait for running CSS transitions before measuring contrast (`ui/e2e/helpers.ts`), which removes intermittent mid-transition contrast failures after a theme toggle.

## [0.6.1] - 2026-11-26

### Changed
- **UI redesign (Coolify-inspired)**: replaced the desktop top bar with a persistent left sidebar (bottom-pinned theme toggle and `Log out`), switched the Dashboard to an apps-first layout above a compact 4-stat Host Health strip, added a dedicated `/apps` page, renamed **Audit Log** to **Activity** (`/activity`, with `/audit-logs` redirecting to `/activity`), and adopted vertical sub-navigation (`SubNav`) for App Detail and Settings with `PageHeader` and `StatusBadge`.
- **Frontend hygiene**: added `ui/.prettierrc.json` (`prettier-plugin-svelte` + `prettier-plugin-tailwindcss`), enforced Tailwind v4 canonical classes and `cssConflict` checks via `@tailwindcss/node`'s `canonicalizeCandidates` + `candidatesToCss` in `make check`, and enabled `svelte-check --fail-on-warnings`.
- **License**: switched the repository license from Apache-2.0 to MIT (`LICENSE`, `ui/package.json`), attached `LICENSE` to GitHub Releases and `checksums.txt`, and rewrote `README.md` for `v0.6.0`+ with new UI screenshots.

## [0.6.0] - 2026-11-26

### Added
- **M6a build pipeline & first deploy backend (`WIZ-2`–`WIZ-3`, `APP-1`, `APP-5`–`APP-7`, `APP-9`, `DEP-3`, `DEP-5`–`DEP-9`, `LOG-1`–`LOG-2`, `UI-5`, `UI-6`)**:
  - `internal/github`: branch listing (`WIZ-2`), commit resolution, and repo tree/Containerfile/Dockerfile/`EXPOSE`/`.gitmodules`/`filter=lfs` inspection (`WIZ-3`) using repo-scoped installation tokens (`GHA-5`).
  - `internal/store`: migration `0004_m6_apps_deploy.sql` (health consecutive threshold, resource/capability limits, deployment `error_message` and `image_ref`), `FailInFlightDeployments` crash/restart recovery, `DeploymentStatusLabel` (`DEP-9`), and `0003 -> 0004` upgrade test.
  - `internal/builder`: bare Git mirrors in `~/.cache/dermaga/mirrors` with `GIT_CONFIG_*` auth (no token in URLs, `.git/config`, or `ps`), `git archive` export, `podman build` with `--memory`/`--cpus`/timeout limits (`DEP-7`), chunk-boundary-safe `SEC-9` secret masking, and bounded `LogHub` (`DEP-8`, `LOG-2`).
  - `internal/deploy`: single global build queue (`DEP-5` superseding queued triggers), first-deploy Quadlet + `podman-system-generator --dryrun` + netns/command/stability-window health checks (`APP-5`) + Traefik dynamic route, process-group cancel (`DEP-6`), failure/cancel cleanup leaving no unit/container/image/route (`DEP-3`), and app removal (`APP-9`).
  - `internal/server`: REST API routes for repo inspection, DNS check, app CRUD, encrypted runtime/build-time env (`APP-1`) with audited reveal (`UI-5`, `AUD-1`), deploy/cancel, typed-confirmation delete (`UI-6`), and SSE streams for build logs (`LOG-2`) and runtime logs (`LOG-1`).

## [0.5.0] - 2026-11-25

### Added
- **GitHub App connection (`GHA-1`–`GHA-6`, `SEC-9`, `DEC-09`, `DEC-29`)**:
  - `internal/github`: GitHub App manifest builder (`Contents: read`, `Metadata: read`, `push` event, private App), RS256 App JWT signing (`crypto/rsa`), `POST /app-manifests/{code}/conversions`, `PATCH /app/hook/config` with freshly generated 32-byte webhook secret (`content_type: "json"`), installation/repository listing, and per-fetch repo-scoped installation tokens kept in memory only, all pinned to `X-GitHub-Api-Version: 2026-03-10`.
  - Migration `0003_github.sql` (`github_installations`, `github_repositories`, `webhook_deliveries`) with `0002 -> 0003` upgrade coverage, AES-256-GCM encryption at rest for App PEM, client secret, and webhook secret, and `doctor` GitHub status check.
  - **Settings → GitHub** UI (`ui/src/lib/components/GitHubSettings.svelte`, `/settings/github/callback`, `/settings/github/install-callback`): hook-domain prerequisite blocker, guided 3-step connect card, single-use session-bound `state` CSRF protection + `SEC-3` Host/Origin checks, installations/repositories view (stacked rows on narrow screens), and typed-confirmation Disconnect with GitHub deletion instructions.
- **GitHub webhook intake & `hook-relay` (`GHA-7`, `GHA-8`, `SEC-11`–`SEC-14`, `DEC-30`)**:
  - `internal/quadlet/hook_relay.go` & `internal/reconciler`: rootless `hook-relay.container` (`Rootfs=` empty directory, no base image, `ReadOnly=true`, `NoNewPrivileges=true`, `DropCapability=ALL`, `Network=web.network`, 25 MiB+64 KiB byte cap, `DefaultDependencies=false` per `DEC-06`, `After=dermaga.service`), wired into `01-hook-domain.yml` on connect and cleanly removed on disconnect.
  - `internal/webhook`: `POST /hooks/github-app` on `$XDG_RUNTIME_DIR/dermaga/hooks.sock` (`0600`), 25 MiB `http.MaxBytesReader` (`413`), constant-time HMAC-SHA256 verification before JSON parsing (`401` + oracle-safe logging per `SEC-11`), `X-GitHub-Delivery` deduplication (`202`, `SEC-13`), per-repo rate limiting (`429`), event filtering (`push`, `installation`, `installation_repositories` per `SEC-12`), `GHA-7` installation/repo sync, and uniform `202` for unknown repos (`SEC-14`).

## [0.4.0] - 2026-11-25

### Added
- **First-run setup mode (`SETUP-1`, `SEC-8`)**: server-enforced redirect to `/setup` while no admin password exists, API refusal in setup mode, single-use 32-byte setup tokens stored as SHA-256 hashes with 24 h expiry, constant-time verification, rate limiting, and exchange for a short-lived `HttpOnly; SameSite=Strict` setup session cookie (with URL token stripped via `history.replaceState`).
- **Setup wizard & TOTP enrolment (`SETUP-2`, `ADM-1`)**: `/setup` UI with token paste fallback + PRD guidance, admin password step with strength meter and server-side policy enforcement, optional TOTP enrolment (`crypto/hmac` RFC 6238 + `github.com/skip2/go-qrcode` inline SVG, ±1 step skew, replay protection, copyable Base32 secret and `otpauth://` URI), persistent TOTP-disabled warning banner in the panel and `dermaga doctor` (`DOC-2`), and TOTP verification on `/login`.
- **Setup link in CLI & installer (`SETUP-3`, `INST-12`)**: `dermaga panel` requests a fresh setup token over `daemon.sock` when uninitialized and prints `http://localhost:7070/setup?token=...`; `install/install.sh` prints the first-run link with `dermaga admin set-password` as fallback.
- **Panel access & security settings (`LAP-3`, `ADM-4`, `ADM-5`, `SEC-10`)**: added `/settings` with **Access** (laptop command, Windows/mobile hints, active sessions list with `user_agent`/`last_seen_at` via migration `0002_sessions_metadata.sql` and "Log out everywhere else"), **Security** (re-authenticated password change ending other sessions; TOTP enable/disable requiring password + current code), and **Hook Domain** (`SETUP-2` DNS A/AAAA check, shared with `/setup` Step 3).
- **Hook domain Traefik router (`APP-2`, `TLS-5`)**: added `01-hook-domain.yml` dynamic config routing only `POST /hooks/github-app` with `tls.certResolver: le` and reconciler integration.
- **Narrow-screen Audit Log (`CONVENTIONS.md` §5.2)**: stacked card rows on viewports `< md` with no horizontal overflow.

### Changed
- **Toolchain upgrade (DEC-25)**: upgraded Go to `1.27.4` (`go.mod`, CI/Release workflows), Bun to `1.4.4` (CI/Release workflows), and `prettier-plugin-svelte` to `4.2.0` (`ui/package.json`, `ui/bun.lock`).

## [0.3.0] - 2026-11-25

### Added
- **Interactive installer** (`install/install.sh`): turns a fresh Debian 13 (x86_64/arm64) server into a hardened dermaga host (`INST-2`–`INST-12`, `SEC-22`). Collects authorized SSH keys (or guides pasting a key), detects private vs public IP without external calls, shows a plan before modifying the host, verifies release binary SHA-256 checksums, sets up `server` with cgroup v2 delegation and lingering, installs `dermaga.service` and Traefik via startup reconciliation, and runs `dermaga doctor`.
- **Host hardening** (`HOST-1`–`HOST-7`): single `server` login user, hardened `sshd` (`AllowUsers server`, key-only, `LogLevel VERBOSE`, local TCP forwarding only), `nftables` default-deny (`inet dermaga` table reloaded without flushing `fail2ban`), `fail2ban` (`sshd` aggressive + `recidive`), `sysctl`, `zram`, `unattended-upgrades`, `systemd-timesyncd`, and `network-online.target` wiring (`DEC-06`, `DEC-22`).
- **Two-phase SSH lockout guard** (`LOCK-1`–`LOCK-5`, `DEC-19`): opens the new SSH port alongside the old port with `Match LocalPort`, detects successful key login from `journalctl`, and closes the old port/password/root access only after proof; restores on failure/timeout/interrupt.
- **Laptop SSH command & host checks** (`internal/access`, `internal/doctor`, `LAP-1`–`LAP-2`): cross-shell `sh -c '...'` command (bash, zsh, fish) that adds a marked `Host dermaga` (or `dermaga-<hostname>`) block with `LocalForward 7070` and matching `IdentityFile`, plus `dermaga doctor` and `dermaga install-settings`.
- **VM verification harness** (`test/vm/drive_install.py`, `test/vm/m3_check.sh`, `test/vm/port_scan.sh`, updated `test/vm/seed.sh`): automated M3b scenarios a–h from `fresh` snapshot.

### Fixed
- **Installer `sshd -T` context**: passed `-C` connection specification to `sshd -T` in `effective_ssh_ports` so Debian 13's `sshd -T` accepts `Match LocalPort` during the open phase.
- **Live-state lockout & firewall verification**: verified live listening `sshd` sockets (`ss -Hltnp`) and the full `nft list ruleset` before skipping or accepting reloads so interrupted reruns never leave old SSH ports open.
- **M3b harness & docs**: closed pty master fd with `SIGHUP -> SIGTERM -> SIGKILL` escalation in `drive_install.py --at-wait hangup`, ordered `m3_check.sh access` to run `server` checks before `denied` checks (preventing premature `fail2ban` bans), and documented passworded `sudo` / `virsh` reboot in `docs/SETUP.md` §3.2.

## [0.2.1] - 2026-11-17

### Fixed
- **Explicit ownership markers for DEP-10**: Added machine-readable ownership markers (`# managed by dermaga; app=<name>`, `# managed by dermaga; network=<name>`, `# managed by dermaga; global`) to all generated Quadlet units and Traefik dynamic configurations. Reconciler strictly ignores and reports files matching naming patterns without the marker as foreign, preventing accidental deletion of user-created units in `~/.config/containers/systemd/`.
- **Safe default against empty database**: Reconciler refuses to delete existing managed units and configs if active app count is 0, protecting against unmounted or incorrectly specified databases; added `--prune` CLI flag to override when empty-database pruning is explicitly intended (`DEC-17`).
- **Traefik negative controls**: Added separate negative test cases for missing middleware and missing service in `test/vm/test_traefik_configs.sh`, explicitly asserting disabled router status and error messages via Python JSON parsing.
- **Allowed-source IP allowlist simulation**: Added allowed source IP simulation in `test/vm/test_traefik_configs.sh` verifying that clients originating from within `192.168.1.0/24` receive HTTP 200 while outside clients receive HTTP 403.
- **Reconciliation negative controls**: Replaced masked `|| true` exit codes with explicit exit code assertions in `test/vm/test_reconciliation.sh`, verified that assertions fail on un-reconciled states, and verified non-zero exit on inaccessible database.
- **Architectural decision references**: Documented `DEC-17` in `docs/ARCHITECTURE.md` and verified all decision citations across documentation.

## [0.2.0] - 2026-11-17

### Added
- **Quadlet generators** (`internal/quadlet`): typed generator producing systemd `.container` and `.network` units with deterministic key ordering, memory and CPU limits, stop timeout, health checks, environment files, and container networking (`APP-5`, `APP-7`, `DEP-5`, `DEP-6`).
- **Traefik generators** (`internal/traefik`): typed generator producing Traefik v3 static (`traefik.yml`), global dynamic (`00-global.yml`), and per-app dynamic (`app-<name>.yml`, `app-<name>-verify.yml`) YAML configurations. Implements the full middleware model (redirects, basic auth, IP allowlist, body limit, headers, compress, strip prefix, rate limit), ACME certificate resolver, and verify router token isolation (`APP-2`–`APP-4`, `APP-10`, `APP-11`, `TLS-1`, `TLS-4`).
- **Startup reconciliation** (`internal/reconciler`, `DEP-10`): daemon startup hook and `dermaga reconcile` CLI subcommand. Removes orphaned container units and Traefik configs, re-renders missing units and configs for active releases, starts stopped live units, cleans up leftover verify routers, and preserves foreign unmanaged files.
- **Store models and methods** (`internal/store/apps.go`): CRUD operations and queries for applications, domains, deployments, and active app records with nullable timestamp scanning.
- **Automated VM test suites**:
  - `test/vm/test_quadlet_dryrun.sh`: dry-run validation of generated Quadlet units against Podman 5's `podman-user-generator` and `systemd-analyze --user verify` on `dermaga-vm`, including negative controls with malformed syntax.
  - `test/vm/test_traefik_configs.sh`: dynamic validation using a throwaway Traefik container pinned to `docker.io/library/traefik:v3.7@sha256:44b61e76a251fe586d17d881fc900452d0a7b4a4b08c97ce6eac86d45b4c8b9d` on non-colliding ports (`18080`, `18443`, `18088`), asserting API router/middleware status and behavioral curl responses (host routing, strip prefix, 301 redirects, basic auth 401/200, IP allowlist 403, body size 200/413, security headers, verify router isolation), with negative controls.
  - `test/vm/test_reconciliation.sh`: scenario test on `dermaga-vm` verifying real orphan removal, missing file re-rendering, stopped unit activation, leftover verify router cleanup, foreign file preservation, and database access failure negative control.
- **Architectural decisions**: recorded `DEC-15` (systemd unit syntax injection escaping and validation), `DEC-16` (switch routing with priority 10000 and universal ACME), and `DEC-17` (explicit ownership markers and safe startup reconciliation) in `docs/ARCHITECTURE.md`.

### Changed
- Hardened Quadlet INI rendering against systemd unit syntax injection: escapes `%` to `%%` in free-text fields (such as `HealthCmd`) to prevent specifier expansion, and strictly rejects trailing backslashes `\` across all keys to prevent line continuation. Rejects newlines, carriage returns, and null bytes across all fields (`SEC-16`–`SEC-19`).
- Pinned reference security headers for `TestDefaultHeadersMatchVPSSetup` directly from `vps-setup.sh` (commit `8fe4cb1aa647a7d7c44710a72bc85000e4d80ef6`) in `internal/traefik/testdata/vps-setup-headers.golden` (`REL-8`).


## [0.1.4] - 2026-11-16

### Fixed
- Fixed dark mode in web panel by configuring `@custom-variant dark (&:where(.dark, .dark *));` in `ui/src/app.css` to support class-based theme toggling alongside Tailwind CSS v4's default media-query variant.
- Prevented flash of wrong theme on page load by adding a synchronous inline `<head>` script in `ui/src/app.html` that checks `localStorage.theme` and falls back to system preference.
- Fixed WCAG AA contrast compliance across light and dark modes: adjusted metric green text to `text-emerald-700` (`#047857`, 4.93:1) on light and `dark:text-emerald-400` (`#34d399`, 8.08:1) on dark; added `dark:text-stone-400` for secondary text on dark surfaces (5.9:1).
- Fixed accessibility violation on Audit Log table horizontal scroll container by adding `role="region" aria-label="Audit log table" tabindex="0"` (`scrollable-region-focusable`).

### Added
- Pinned and integrated `@axe-core/playwright@4.14.0` in `ui/package.json` for automated WCAG 2.1 Level AA testing on live rendered DOM across all views, themes, and viewports.
- Added computed background verification in Playwright E2E asserting rendered sRGB colors (`rgb(12, 10, 9)` body, `rgb(28, 25, 23)` cards) via canvas 2D context resolution.
- Recorded architectural decision `DEC-14` in `docs/ARCHITECTURE.md`.
- Updated deployment and capture scripts to verify real VM dark mode and live Axe contrast on `dermaga-vm`.

### Removed
- Removed static Python contrast calculation script `scripts/check_contrast.py` in favor of live browser DOM analysis with Axe.

## [0.1.3] - 2026-11-16

### Added
- Official Dermaga logo (`ui/static/logo.svg`) incorporated from the project's earlier design assets.
- High-resolution favicons (`favicon.png` 32x32, `favicon-16x16.png` 16x16, and `favicon.svg`) generated reproducibly via ImageMagick (`docs/SETUP.md` §6.7).
- Brand-derived design tokens in `ui/src/app.css` (`--brand-primary`, `--brand-hover`, `--brand-active`, `--brand-ring`, `--brand-accent`, `--brand-navy`, `--brand-text`, `--brand-badge-*`, `--brand-btn-text`) mapped into Tailwind v4 `@theme`.
- Automated contrast verification script `scripts/check_contrast.py` verifying all 17 text/background and focus-ring color pairings meet or exceed WCAG 2.1 Level AA (`NFR-7`).
- Comprehensive browser journey screenshots in light and dark modes across desktop (1280x800) and narrow (375x667) viewports in `docs/evidence/M1/brand/`.
- Architectural decision `DEC-13` recorded in `docs/ARCHITECTURE.md`.

### Changed
- Replaced panel header "D" placeholder with official SVG logo and updated alt text.
- Replaced login card icon with official SVG logo, and replaced default button colors with brand primary tokens.
- Replaced teal accent classes across Dashboard and Audit Log views with brand tokens (`text-brand-text`, `bg-brand-badge-bg`, `text-brand-badge-text`, `focus:ring-brand-ring`).
- Updated `make test-browser` target in `Makefile` to depend on `build` to guarantee tested binaries reflect current UI builds.

## [0.1.2] - 2026-11-16

### Added
- Added comprehensive unit tests in `cmd/dermaga/main_test.go` covering CLI recovery commands, non-TTY and TTY input paths, short and empty password rejections, and dynamic SSH connection detection.
- Added automated measurement script `test/vm/measure_nfr2.sh` to measure fresh-start idle RSS, post-login peak RSS, and 10-minute settled idle RSS on `dermaga-vm`.
- Documented architectural decision `DEC-12` in `docs/ARCHITECTURE.md` establishing Option C for NFR-2 idle RSS budget and RFC 9106 Option 2 Argon2id parameters.

### Changed
- Updated Argon2id parameters to RFC 9106 §4 Second Recommended Option (`t=3, m=64 MiB, p=4`, 128-bit salt, 256-bit tag size) in `internal/auth/auth.go`.
- Pinned `actions/checkout` in GitHub Actions CI workflow to `actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1` (`v7.0.1`), eliminating Node.js 20 deprecation warnings.
- Removed `--password` flag from `dermaga admin set-password` in favor of interactive prompt with hidden input and confirmation on TTY, or single-line stdin on non-TTY.
- Updated `dermaga panel` to dynamically derive SSH connection details (`$SSH_CONNECTION`, daemon configuration, or port 22 fallback) and detect private/loopback/CGNAT addresses.
- Updated non-release version fallback default to `"dev"` when built without `-ldflags` (`main.Version`).

### Fixed
- Included and verified `cmd/dermaga/main_test.go` across clean builds and tests.
- Updated `test/vm/seed.sh` to pipe demo password to stdin and store it in `~/.config/dermaga/demo_password` (mode 0600) without exposing password strings in script logs.

## [0.1.1] - 2026-11-16

### Fixed
- Fixed build failure from a clean clone where unanchored `dermaga` in `.gitignore` omitted `cmd/dermaga/main.go`.
- Fixed `make check` failure on fresh clone by ordering static UI dist generation before Go linting, vet, and testing.
- Fixed broken CI workflow by pinning the Debian Trixie Slim container image multi-arch index digest (`sha256:a72b4433de1cf7d6bae0c4cee74d0547f58601a643b3375f070c51600d63b6b2`), aligning Go with Go 1.26.11, pinning Bun 1.4.2 sha256, and installing missing `file` utility.
- Fixed `SEC-3` Origin validation on mutating requests by removing fallback to `Referer` header.
- Added missing time filtering (`since`, `until`) and app filtering to Audit Logs API and UI (`AUD-2`).
- Removed hard-coded placeholder UI text ("WAL mode", "0600 mode encrypted", speculative "Configured Applications" card) to ensure the UI renders only server-backed state.
- Fixed version display in web panel to show exact server version without duplicate "v" prefix.
- Hardened `make vulncheck` by strictly validating `bun audit` against documented accepted advisories (`GHSA-gcqx-w48m-w2h4`) instead of swallowing errors.
- Documented atomic binary replacement procedure in `docs/SETUP.md` to prevent Linux `ETXTBSY` ("Text file busy") errors during updates.
- Added `test/vm/seed.sh` and `test/vm/m1_check.sh` for idempotent VM demo seeding and verification.
- Corrected documentation discrepancies in `docs/evidence/M1/EVIDENCE.md` regarding Go version (Go 1.26) and session idle timeout (12h).

## [0.1.0] - 2026-11-16

### Added
- Core daemon listening on `127.0.0.1:7070` with root refusal (`SEC-1`).
- DNS rebinding protection (421 Misdirected Request) and foreign Origin protection (403 Forbidden) (`SEC-3`).
- Strict HttpOnly session cookies (`SEC-7`) and timeout headers (`SEC-15`).
- Admin authentication with Argon2id password hashing, progressive rate limiting, and idle/absolute session expiration (`ADM-1`, `ADM-2`, `ADM-3`).
- Recovery commands `dermaga admin set-password` and `dermaga admin totp disable` communicating over a 0600 Unix socket with direct SQLite fallback (`SRV-1`, `SRV-2`).
- `dermaga panel` CLI helper to print SSH local port-forwarding instructions (`SRV-1`).
- AES-256-GCM encryption at rest for secrets under master key with column-bound AAD.
- SQLite database store with WAL mode, foreign keys, embedded schema migrations, and audit logging (`AUD-1`, `AUD-2`).
- Embedded SvelteKit 3 and Tailwind v4 single-page application within the static Go binary.
- Web panel pages: Login, Dashboard showing real daemon status and system telemetry, and Audit Log viewer.
- Ported `hook-relay` and `probe` tools from M0 spikes into the binary.
- Multi-architecture build targets (`linux/amd64` and `linux/arm64`) with automated CI pipeline.
