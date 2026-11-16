# Changelog

All notable changes to this project are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and versions follow
[Semantic Versioning](https://semver.org/).

## [Unreleased]

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
