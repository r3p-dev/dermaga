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

## Known advisories

- **GO-2026-6859** (`net/http`, HTTP/1 server crash through a `MaxBytesReader` header race):
  reported by govulncheck on 2026-11-12 with reachable call paths (the daemon serves HTTP). Found
  in go1.27.3, fixed in go1.27.4. `go.mod` now says `go 1.27.4` (no `toolchain` line is needed;
  the `go` line is the minimum and `GOTOOLCHAIN=auto` fetches it). CI reads the same file through
  `go-version-file: go.mod`, so there is no second Go version to keep in step.
- **GO-2026-5932** (`golang.org/x/crypto/openpgp` is unmaintained and unsafe by design): reported
  by govulncheck at module level. dermaga doesn't import `openpgp` (govulncheck: "your code
  doesn't appear to call these vulnerabilities"). The fix exists only as a pseudo-version
  (`v0.58.1-0.20261009181718-2f1f834330b2`). Move to the next tagged x/crypto release.
  Accepted for now (2026-11-07). Re-checked at the start of M2: `go list -m -versions` still ends
  at v0.58.0, so there is no fixed tag yet; same result at the start of M3. Check again at every milestone.
