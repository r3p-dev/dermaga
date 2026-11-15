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

**To propose when needed** (with a comparison, before adding): a browser test tool (M1), a lint setup
that reports deprecated APIs in the UI (M1), a QR code renderer for TOTP enrolment (M4; first check
whether it can be done without a library).

## 3. Container images (rootless Podman)

Pinned by tag **and** digest where they are used.

| Image                         | Purpose                                  | Rule |
| ----------------------------- | ---------------------------------------- | ---- |
| Traefik v3 (official)         | Reverse proxy                            | Updates within the pinned major only |
| Debian 13 (official)          | CI job container                         | |

The hook relay runs the dermaga binary on an empty root filesystem (no base image).

## 4. Host packages (Debian 13, installed by the installer)

| Package                                                                    | Purpose |
| -------------------------------------------------------------------------- | ------- |
| podman, passt, crun, netavark, aardvark-dns, catatonit, uidmap             | Rootless containers and networking |
| nftables, fail2ban, openssh-server, unattended-upgrades, needrestart, systemd-zram-generator | Host hardening |
| systemd-timesyncd (if no other time daemon)                                | Correct clock for TOTP and certificates |
| git                                                                        | Mirrors and builds |
| curl, ca-certificates, jq                                                  | Installer and checks |

## 5. Development and runtime requirements

| Requirement                     | Source |
| ------------------------------- | ------ |
| Go                              | The `go` line in `go.mod` |
| Bun                             | Pinned in CI and the `Makefile` |
| Podman (minimum version)        | A constant in code; checked by CI, the installer and `doctor` |
| libvirt + a Debian 13 cloud image | The test VM (`docs/SETUP.md`) |

## 6. Known advisories

Re-checked at the start of every milestone (`govulncheck`, `bun audit`).

| Advisory | Reached? | Decision | Date |
| -------- | -------- | -------- | ---- |
