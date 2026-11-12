# Dependencies

BRIEF §4.8: minimal dependencies, each one justified here. `go.sum` is committed. Versions are
pinned in `go.mod` and listed in ARCHITECTURE §6. The standard library comes first: `net/http`
(routing, servers, client), `database/sql`, `crypto/*` (AES-GCM, HMAC, SHA-*), `encoding/json`,
`flag`, `log/slog`, `embed`, `os/exec`.

## Linked into the binary

| Module | Version | Why | Alternative considered |
|---|---|---|---|
| `modernc.org/sqlite` | v1.61.1 | SQLite without cgo, so the binary stays static (`CGO_ENABLED=0`, BRIEF §2). | `mattn/go-sqlite3` needs cgo. |
| `golang.org/x/crypto` | v0.58.0 | `argon2` for the admin password (BRIEF §4.3). `bcrypt` for Traefik basic-auth hashes (`dermaga middleware <app> basic-auth add`, cost 10, ARCHITECTURE D38). Maintained by the Go team. | No argon2 in the standard library. |
| `github.com/pelletier/go-toml/v2` | v2.4.3 | Reads `config.toml` with typed decoding and rejects unknown keys (BRIEF §3.2: typed serialization). | `BurntSushi/toml` works too. go-toml v2 has strict mode (`DisallowUnknownFields`) and no further dependencies. |

These come in indirectly through `modernc.org/sqlite`, its transpiled-C runtime:
`modernc.org/libc`, `modernc.org/mathutil`, `modernc.org/memory`, `golang.org/x/sys`,
`github.com/dustin/go-humanize`, `github.com/google/uuid`, `github.com/remyoudompheng/bigfft`.

Not added on purpose:

- **TOTP library**: RFC 6238 is about 40 lines on `crypto/hmac`, tested against the RFC's vectors (`internal/auth/totp.go`).
- **`golang.org/x/term`** (no-echo password prompt): two `ioctl` calls via `syscall` (`internal/cli/admin.go`). dermaga is Linux-only.
- **CLI framework**: the standard `flag` package and a map of subcommands (`internal/cli`).
- **YAML library**: Traefik dynamic files are written as JSON (ARCHITECTURE D6).

## Development tools (`tool` directives in go.mod, not linked)

| Tool | Module | Run as |
|---|---|---|
| staticcheck | `honnef.co/go/tools` v0.8.2 | `go tool staticcheck ./...` |
| govulncheck | `golang.org/x/vuln` v1.9.0 | `go tool govulncheck ./...` |

Their dependencies (`golang.org/x/tools`, `x/mod`, `x/sync`, `x/telemetry`, `x/exp/typeparams`,
`BurntSushi/toml`) appear in `go.mod` as indirect requirements, but none of them is linked into the
binary. `tygo` (Go → TypeScript types) joins this list in M7.

## UI build (`ui/package.json`, dev dependencies only, nothing ships at runtime)

Exact versions, no ranges (`bun add -d -E`); `ui/bun.lock` is committed and `make ui` installs with
`--frozen-lockfile`. Pinned 2026-11-12 at the versions the lockfile already resolved, so the pin
changes no installed package. Built and checked with **Bun 1.4.2** (local and `release.yml`).

| Package | Version | Why |
|---|---|---|
| `svelte` | 5.57.3 | UI framework (BRIEF §2). |
| `@sveltejs/kit` | 3.1.1 | Routing and the static SPA build embedded in the binary. |
| `@sveltejs/adapter-static` | 4.0.1 | Emits `ui/dist` for `embed`. |
| `@sveltejs/vite-plugin-svelte` | 7.3.1 | Svelte compiler plugin, required by SvelteKit. |
| `vite` | 8.3.6 | Bundler, required by SvelteKit. |
| `tailwindcss`, `@tailwindcss/vite` | 4.3.5 | Styling. |
| `typescript` | 5.9.3 | Type checking of the UI and of the tygo-generated types. |
| `svelte-check` | 4.7.8 | `bun run check`, part of `make check` (`ui-check`). |
| `prettier` | 3.10.1 | One formatter for the UI (`bun run format`, `format:check` in `make check`). Added 2026-11-12 (decision D-d, gap G31); registry `latest` on that date. No alternative considered: it is the formatter SvelteKit projects use and Go already has `gofmt`. |
| `prettier-plugin-svelte` | 4.1.1 | Lets Prettier parse `.svelte` files; registry `latest` on 2026-11-12, peer range `prettier ^3`, `svelte ^5`. |

Prettier settings (`ui/.prettierrc`) follow the style the code already had: tabs, single quotes, no
trailing commas, width 100. `ui/src/lib/types/` is ignored because tygo generates it.

Newer releases seen on 2026-11-12 (`bun outdated`), **not** taken in the pinning change:
`@sveltejs/kit` 3.2.0, `vite` 8.3.8, `typescript` 7.0.2 (new major).

## CI (`.github/workflows/release.yml`)

Every action is pinned to the full commit SHA of its release tag, with the tag as a trailing
comment, because tags can be moved and this workflow builds the binaries and `checksums.txt` that
the installer verifies. Tags are the newest within the major already in use; SHAs resolved with
`git ls-remote https://github.com/<owner>/<repo> refs/tags/<tag>` on 2026-11-12 (all four are
lightweight tags, so the SHA is the commit). Go comes from `go.mod` (`go-version-file`).

| Action / runtime | Pin | Newer major exists |
|---|---|---|
| `actions/checkout` | `11d5960a326750d5838078e36cf38b85af677262` (v4.4.0) | v7.0.1 |
| `actions/setup-go` | `40f1582b2485089dde7abd97c1529aa768e1baff` (v5.6.0) | v7.0.0 |
| `oven-sh/setup-bun` | `0c5077e51419868618aeaa5fe8019c62421857d6` (v2.2.0) | — |
| Bun (`bun-version`) | 1.4.2 | — |
| `softprops/action-gh-release` | `3bb12739c298aeb8a4eeaf626c5b8d85266b0e65` (v2.6.2) | v3.0.4 |

## Known advisories

- **GO-2026-6859** (`net/http`, HTTP/1 server crash through a `MaxBytesReader` header race):
  reported by govulncheck on 2026-11-12 with reachable call paths (the daemon serves HTTP). Found
  in go1.27.3, fixed in go1.27.4. `go.mod` now says `go 1.27.4` (no `toolchain` line is needed;
  the `go` line is the minimum and `GOTOOLCHAIN=auto` fetches it). CI reads the same file through
  `go-version-file: go.mod`, so there is no second Go version to keep in step.
- **GHSA-gcqx-w48m-w2h4** (`deepmerge` 4.3.1, low, prototype poisoning in `mergeObject()`):
  reported by `bun audit` on 2026-11-12, reached through `@sveltejs/vite-plugin-svelte`. Build-time
  only; nothing from `node_modules` runs on the server. Accepted for now; re-check at each phase.

- **GO-2026-5932** (`golang.org/x/crypto/openpgp` is unmaintained and unsafe by design): reported
  by govulncheck at module level. dermaga doesn't import `openpgp` (govulncheck: "your code
  doesn't appear to call these vulnerabilities"). The fix exists only as a pseudo-version
  (`v0.58.1-0.20261009181718-2f1f834330b2`). Move to the next tagged x/crypto release.
  Accepted for now (2026-11-07). Re-checked at the start of M2: `go list -m -versions` still ends
  at v0.58.0, so there is no fixed tag yet; same result at the start of M3. Check again at every milestone.
