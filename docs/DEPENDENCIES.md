# Dependencies

Every Go module, UI package, host package and container image this project uses, and why. **Exact
versions live in `go.mod` / `go.sum`, `ui/package.json` / `bun.lock`, the installer and the code that
names an image**, not here, so this file doesn't go stale.

Rules (`docs/CONVENTIONS.md` §12):

- Add nothing that isn't listed here. To add something, list it with its purpose and **why the
  standard library or the platform is not enough**, in the same change.
- Only stable, actively maintained dependencies. Check before adding that the version you pick isn't
  deprecated.
- When something is removed, remove it here too.

## 1. Installed

_Filled in as dependencies are added (M1 onward)._

| Dependency | Kind | Purpose | Added in |
| ---------- | ---- | ------- | -------- |
| `modernc.org/sqlite` v1.62.0 | Go module | SQLite driver without cgo; static binary | M1 |
| `golang.org/x/crypto` v0.59.0 | Go module | argon2id password hashing | M1 |
| `golang.org/x/term` v0.48.0 | Go module | Terminal detection and password prompt without echo | M1 |
| `honnef.co/go/tools` (staticcheck) | Go tool | Lint, deprecated API detection | M1 |
| `golang.org/x/vuln` (govulncheck) | Go tool | Known Go vulnerability scanning | M1 |
| `github.com/gzuidhof/tygo` | Go tool | TypeScript types from Go structs | M1 |
| `@sveltejs/kit` 3.2.0 | UI (build) | SvelteKit 3 framework | M1 |
| `@sveltejs/adapter-static` 4.0.1 | UI (build) | Static SPA adapter | M1 |
| `@sveltejs/vite-plugin-svelte` 7.3.1 | UI (build) | Svelte integration for Vite | M1 |
| `svelte` 5.57.3 | UI (build) | Svelte 5 compiler | M1 |
| `vite` 8.3.8 | UI (build) | Build tool | M1 |
| `tailwindcss` 4.3.5, `@tailwindcss/vite` | UI (build) | Utility-first CSS | M1 |
| `typescript` 5.x, `svelte-check` | UI (build) | Type checking | M1 |
| `prettier`, `prettier-plugin-svelte` | UI (build) | Code formatting | M1 |
| `@playwright/test` 1.65.1 | UI (test) | Browser end-to-end tests | M1 |

## 2. Approved to add

Add each in the milestone where it is first needed, after checking its current version and docs.

### Go modules (linked into the binary)

| Module                              | Purpose                                              | Why not the standard library                             |
| ----------------------------------- | ---------------------------------------------------- | -------------------------------------------------------- |
| `modernc.org/sqlite`                | SQLite driver without cgo                            | No SQLite in the standard library; cgo drivers break the static binary |
| `golang.org/x/crypto`               | argon2id (admin password), bcrypt (Traefik basic auth) | Neither algorithm is in the standard library           |

Deliberately **not** added: a web framework, an ORM, a CLI framework, a YAML library (Traefik files are
JSON), a TOML library (no config file), a TOTP library (RFC 6238 on `crypto/hmac`), a JWT library (RS256 on `crypto/rsa`), `go-github`
(a handful of endpoints on `net/http`).

### Go tools (`tool` directives, not linked)

| Tool                                | Purpose                         |
| ----------------------------------- | ------------------------------- |
| staticcheck (`honnef.co/go/tools`)  | Lint, including deprecated APIs |
| govulncheck (`golang.org/x/vuln`)   | Known vulnerabilities           |
| tygo (`github.com/gzuidhof/tygo`)   | TypeScript types from Go structs |

### UI (build time only)

| Package                                                                                    | Purpose                    |
| ------------------------------------------------------------------------------------------ | -------------------------- |
| `@sveltejs/kit`, `@sveltejs/adapter-static`, `@sveltejs/vite-plugin-svelte`, `svelte`, `vite` | The SPA and its static build |
| `tailwindcss`, `@tailwindcss/vite`                                                          | Styling                    |
| `typescript`, `svelte-check`                                                                | Strict type checks         |
| `prettier`, `prettier-plugin-svelte`                                                        | Formatting                 |
| `@playwright/test`                                                                         | End-to-end browser testing |
| `@axe-core/playwright`                                                                     | Automated WCAG 2.1 AA accessibility and contrast auditing on live DOM |

**To propose when needed** (with a comparison, before adding): a browser test tool (M1), a lint setup
that reports deprecated APIs in the UI (M1), a QR code renderer for TOTP enrolment (M4; first check
whether it can be done without a library).

## 3. Container images (rootless Podman)

Pinned by tag **and** digest where they are used.

| Image                         | Purpose                                  | Rule |
| ----------------------------- | ---------------------------------------- | ---- |
| Traefik v3 (official)         | Reverse proxy; pinned as `traefik.Image` in `internal/traefik/types.go` (tag + multi-arch index digest, DEC-20) | Updates within the pinned major only, shipped with dermaga releases |
| Debian 13 (official)          | CI job container                         | |

The hook relay runs the dermaga binary on an empty root filesystem (no base image).

## 4. Host packages (Debian 13, installed by the installer)

| Package                                                                    | Purpose |
| -------------------------------------------------------------------------- | ------- |
| podman, passt, crun, netavark, aardvark-dns, catatonit, uidmap             | Rootless containers and networking |
| dbus-user-session                                                          | The systemd user bus `systemctl --user` talks to |
| nftables, fail2ban, python3-systemd, openssh-server, unattended-upgrades, apt-listchanges, needrestart, systemd-zram-generator | Host hardening (python3-systemd: fail2ban's journal backend) |
| sudo, iproute2                                                             | sudo for `server`; `ss` and `ip` for the installer's checks |
| systemd-timesyncd (if no other time daemon)                                | Correct clock for TOTP and certificates |
| git                                                                        | Mirrors and builds |
| curl, ca-certificates, jq                                                  | Installer and checks |

## 5. Development and runtime requirements

| Requirement                     | Source |
| ------------------------------- | ------ |
| Go                              | The `go` line in `go.mod` |
| Bun                             | Pinned in CI and the `Makefile` |
| Podman (minimum version)        | `doctor.MinPodmanVersion` (5.4.2, Debian 13's); checked by `doctor`, which the installer runs |
| CI job packages                 | `.github/workflows/ci.yml` and `release.yml` install `jq zsh fish openssh-client openssh-server` (LAP-1 and installer tests, `sshd -T`) and, for releases, `gh` from Debian trixie (`docs/evidence/M3/logs/debian_packages_query.log`) |
| libvirt + a Debian 13 cloud image | The test VM (`docs/SETUP.md`) |

## 6. Known advisories

Re-checked at the start of every milestone (`govulncheck`, `bun audit`).

| Advisory | Reached? | Decision | Date |
| -------- | -------- | -------- | ---- |
| [GHSA-gcqx-w48m-w2h4](https://github.com/advisories/GHSA-gcqx-w48m-w2h4) — deepmerge ≥4.2.0 prototype poisoning (low) | No — transitive via `@sveltejs/vite-plugin-svelte`, build-time only, never in the browser bundle | Accepted; waiting for upstream fix in vite-plugin-svelte | 2026-11-16 |
