# dermaga v2 dependencies

An earlier, uncommitted proposal listed `tailscale.com`, `compose-go`, `go-github`, `go-gitlab`,
`yaml.v3` and "Latest" versions; none of those are adopted. v2 adds **no new Go module** for its core phases.
The v1 list and its justifications stay in `docs/DEPENDENCIES.md`; changes are recorded there.

Rule (v1 BRIEF §4.8, restated): every dependency is pinned to an exact version, justified, and
re-checked for the current stable release and advisories at the start of every phase. Versions
below marked "pinned in code" are read from the repository, not from this file; this file never
becomes a second source of truth for version numbers.

---

## 1. Host (Debian 13 packages, installed by `install.sh`)

| Package                                                                                      | Why                             | v2 change                                                                     |
| -------------------------------------------------------------------------------------------- | ------------------------------- | ----------------------------------------------------------------------------- |
| podman, passt, crun, netavark, aardvark-dns, catatonit, uidmap                               | rootless containers             | —                                                                             |
| nftables, fail2ban, openssh-server, unattended-upgrades, needrestart, systemd-zram-generator | hardening (v1)                  | —                                                                             |
| systemd-timesyncd (if no chrony)                                                             | clock for TOTP and certificates | —                                                                             |
| curl, ca-certificates, jq                                                                    | installer and probes            | —                                                                             |
| **restic**                                                                                   | backups                         | **added (gap G3)**                                                            |
| **git**                                                                                      | mirrors and builds              | **added (gap G3)**                                                            |
| tailscale                                                                                    | optional panel access           | Phase 6 only, opt-in, from Tailscale's Debian repository with its signing key |

Not installed: Docker, Compose, Railpack CLI on the host, Node/Bun at runtime.

## 2. Go modules linked into the binary

| Module                            | Status                 | Why                           |
| --------------------------------- | ---------------------- | ----------------------------- |
| `modernc.org/sqlite`              | v1, pinned in `go.mod` | cgo-free SQLite               |
| `golang.org/x/crypto`             | v1, pinned in `go.mod` | argon2id, bcrypt (basic auth) |
| `github.com/pelletier/go-toml/v2` | v1, pinned in `go.mod` | strict `config.toml` decoding |

Considered for v2 and **not added**:

| Candidate                     | Why not                                                                                                                                                                                                                          |
| ----------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `github.com/google/go-github` | dermaga needs about six GitHub endpoints (manifest conversion, installations, access tokens, repos, branches, contents). Typed request/response structs on `net/http` are smaller and easier to audit.                           |
| a JWT library                 | One RS256 token type: `crypto/rsa.SignPKCS1v15` + `crypto/sha256` + base64url, with tests against a known vector.                                                                                                                |
| `gopkg.in/yaml.v3`            | Templates are embedded JSON; Traefik files are JSON (D6).                                                                                                                                                                        |
| `compose-go`, `go-gitlab`     | Features rejected (v2 BRIEF §2.3).                                                                                                                                                                                               |
| `tailscale.com`               | Phase 6 decides. Preferred: drive the `tailscale` CLI with argument slices and read `tailscale status --json`, rather than linking the large client module. If linking is ever proposed, measure binary size and idle RSS first. |

## 3. Development tools (`tool` directives in `go.mod`, not linked)

staticcheck, govulncheck, tygo — v1, pinned in `go.mod`.

## 4. UI (`ui/package.json`, built with Bun, embedded)

Current dev dependencies: `@sveltejs/kit`, `@sveltejs/adapter-static`,
`@sveltejs/vite-plugin-svelte`, `svelte`, `svelte-check`, `tailwindcss`, `@tailwindcss/vite`,
`typescript`, `vite`.

- **Gap G7, closed 2026-11-12:** exact versions, `bun.lock` committed, `--frozen-lockfile` in
  `make ui`; `release.yml` pins Bun and every action to an exact tag. Versions and the accepted
  `bun audit` finding are listed in `docs/DEPENDENCIES.md`.
- v2 adds a QR code for TOTP enrolment. First check whether the existing UI already renders one;
  if a library is needed, choose the smallest maintained one, pin it, and justify it here.
  Generating the QR server-side in Go without a dependency is acceptable if it stays small.
- Not added: `lucide-svelte` (inline SVG icons are enough), `@xterm/xterm` (the existing
  `Terminal.svelte` renders logs; revisit only if ANSI colour support is needed, and measure the
  bundle).

## 5. Container images

| Image                       | Status                             | Rule                                           |
| --------------------------- | ---------------------------------- | ---------------------------------------------- |
| Traefik                     | v1, pinned in code                 | auto-update within the pinned major, as v1     |
| PostgreSQL, MariaDB, Valkey | v1, pinned in `internal/databases` | re-check current majors at Phase 0             |
| Template images             | new (Phase 4)                      | exact tag per template, verified on the VM     |
| BuildKit (rootless)         | Phase 5, only if the spike passes  | exact tag, on-demand container, never resident |

restic is a host package, not an image.

## 6. External services

| Service                    | Used for                               | When                    |
| -------------------------- | -------------------------------------- | ----------------------- |
| api.github.com, github.com | GitHub App, fetches, release downloads | outbound only           |
| ACME (Let's Encrypt)       | certificates                           | via Traefik             |
| S3-compatible storage      | backups                                | user-provided           |
| ntfy / Telegram / Discord  | notifications                          | optional, user-provided |
| Tailscale coordination     | panel access                           | Phase 6, opt-in         |

No telemetry, no update checks except `dermaga self-update`.
