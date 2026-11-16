# Changelog

All notable changes to this project are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and versions follow
[Semantic Versioning](https://semver.org/).

## [Unreleased]

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
- Official Dermaga logo (`ui/static/logo.svg`) incorporated from `dermaga-archive/ui/static/logo.svg`.
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
