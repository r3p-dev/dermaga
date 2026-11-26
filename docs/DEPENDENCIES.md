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
- **Pinned values from real queries:** all pinned values (container image digests, GitHub Actions
  SHAs, tool versions, checksums, download URLs) must be copied directly from a registry or source
  query, never written from memory. Each entry here records the exact query command and the exact
  result line (digest, checksum or version) with the date; the full query output stays in a local
  log under `tmp/evidence/<milestone>/logs/` (`scripts/evidence-run`) and is never committed.
  `scripts/check-policy` (run by `make check`) fails on a container image without an `@sha256:`
  digest.

## 1. Installed

_Filled in as dependencies are added (M1 onward)._

| Dependency | Kind | Purpose | Added in |
| ---------- | ---- | ------- | -------- |
| `modernc.org/sqlite` v1.62.0 | Go module | SQLite driver without cgo; static binary | M1 |
| `golang.org/x/crypto` v0.59.0 | Go module | argon2id password hashing, bcrypt for Traefik basic auth | M1 (v0.59.0 since M3, DEC-23) |
| `golang.org/x/term` v0.48.0 | Go module | Terminal detection and password prompt without echo | M1 |
| `github.com/skip2/go-qrcode` v0.0.0-20200617195104-da1b6568686e | Go module | QR code matrix generation for TOTP enrolment (`SETUP-2`) | M4a |
| `gopkg.in/yaml.v2` v2.4.0 | Go module, tests only | Parses `internal/traefik/testdata/vps-setup-headers.golden` in `injection_test.go`; not linked into the binary | M2 (listed in M3) |
| `honnef.co/go/tools` v0.8.2 (staticcheck) | Go tool | Lint, deprecated API detection | M1 |
| `golang.org/x/vuln` v1.9.0 (govulncheck) | Go tool | Known Go vulnerability scanning | M1 |
| `github.com/gzuidhof/tygo` v0.2.21 | Go tool | TypeScript types from Go structs | M1 |
| `@sveltejs/kit` 3.2.0 | UI (build) | SvelteKit 3 framework | M1 |
| `@sveltejs/adapter-static` 4.0.1 | UI (build) | Static SPA adapter | M1 |
| `@sveltejs/vite-plugin-svelte` 7.3.1 | UI (build) | Svelte integration for Vite | M1 |
| `svelte` 5.57.3 | UI (build) | Svelte 5 compiler | M1 |
| `vite` 8.3.8 | UI (build) | Build tool | M1 |
| `tailwindcss` 4.3.5, `@tailwindcss/vite` 4.3.5 (`@tailwindcss/node` 4.3.5, `@tailwindcss/oxide` 4.3.5) | UI (build & lint) | Utility-first CSS; `ui/scripts/check-tailwind.js` pins and uses `@tailwindcss/node` (`__unstable__loadDesignSystem`, `canonicalizeCandidates`, `candidatesToCss`) + `@tailwindcss/oxide` (`Scanner`) | M1 (hygiene script v0.6.1) |
| `typescript` 6.0.3 | UI (build) | Type checking; 6.x because `@sveltejs/kit` 3.2.0 requires `^6.0.0` and `svelte-check` 4.7.8 accepts `^5 \|\| ^6` | M1 (6.0.3 since M3) |
| `svelte-check` 4.7.8 | UI (build) | Svelte type checking | M1 |
| `prettier` 3.10.1, `prettier-plugin-svelte` 4.2.0, `prettier-plugin-tailwindcss` 0.8.1 | UI (build) | Code formatting and Tailwind class sorting | M1 (4.2.0 since DEC-25; tailwindcss plugin 0.8.1 in v0.6.1) |
| `@playwright/test` 1.65.1 | UI (test) | Browser end-to-end tests | M1 |
| `@axe-core/playwright` 4.14.0 | UI (test) | WCAG 2.1 AA checks on the rendered page | M1 |

Versions above equal `go.mod` and `ui/package.json`, which pins every UI package exactly (no `^`
ranges); `ui/bun.lock` resolves them to the same versions. Queried 2026-11-17, before the
`golang.org/x/crypto` update and the exact UI pins (log removed 2026-11-17, see git history before this change). Command: query
`proxy.golang.org/<module>/@latest` for every direct Go module and tool, and
`registry.npmjs.org/<pkg>/latest` for every UI package; compare with `go.mod` and `bun.lock`.
Result lines:

```text
## Go (module | go.mod | proxy latest | latest time)
golang.org/x/crypto | v0.0.0-20210817164053-32db794688a5 | v0.59.0 | 2026-11-11T07:23:42Z
golang.org/x/term | v0.48.0 | v0.48.0 | 2026-11-09T16:29:12Z
modernc.org/sqlite | v1.62.0 | v1.62.0 | 2026-11-07T09:13:04Z
github.com/gzuidhof/tygo | v0.2.21 | v0.2.21 | 2026-02-03T10:16:48Z
golang.org/x/vuln | v1.9.0 | v1.9.0 | 2026-10-11T03:39:53Z
honnef.co/go/tools | v0.8.2 | v0.8.2 | 2026-10-12T13:34:44Z
## Go toolchain: go.mod go line and https://go.dev/dl/?mode=json (stable, queried 2026-11-25, dec25-go-query.log)
go 1.27.4
go1.27.4.linux-amd64.tar.gz sha256=7226736c2339719cda28fbf3a54d71aa63bcefe376e98cef00034268dab24840
## Bun packages (package | package.json | bun.lock resolved | npm latest, queried 2026-11-25, dec25-prettier-svelte-query.log)
prettier-plugin-svelte | 4.2.0 | 4.2.0 | 4.2.0
## Bun itself: CI pin and GitHub release SHASUMS256.txt (queried 2026-11-25, dec25-bun-query.log)
bun-v1.4.4
bun-linux-x64.zip sha256=bf8ccbfb2b0b80159e0f9a6d15c55317add8077534dbc6130a3edb52fccb395c
```

## 2. Approved to add

Add each in the milestone where it is first needed, after checking its current version and docs.

### Go modules (linked into the binary)

| Module                              | Purpose                                              | Why not the standard library                             |
| ----------------------------------- | ---------------------------------------------------- | -------------------------------------------------------- |
| `modernc.org/sqlite`                | SQLite driver without cgo                            | No SQLite in the standard library; cgo drivers break the static binary |
| `golang.org/x/crypto`               | argon2id (admin password), bcrypt (Traefik basic auth) | Neither algorithm is in the standard library           |
| `github.com/skip2/go-qrcode`        | QR code matrix for TOTP enrolment (`SETUP-2`)        | Reed-Solomon error correction and QR mask selection are non-trivial without a library; `skip2/go-qrcode` (MIT, zero transitive deps, `govulncheck` clean) was chosen over `rsc.io/qr` (v0.2.0, unmaintained since 2018). Note: `go-qrcode` is unmaintained (module `2020-06-17`, repo last pushed `2024-03-01`), which is acceptable here because it is **encode-only** on trusted internal `otpauth://` URIs with no network calls or parsing of untrusted input. Queried 2026-11-25 (`tmp/evidence/M4/logs/qr-library-query.log`). |

Deliberately **not** added: a web framework, an ORM, a CLI framework, a YAML library in the binary (Traefik
files are JSON; one test reads a YAML golden file, see §1), a TOML library (no config file), a TOTP library (RFC 6238 on `crypto/hmac`), a JWT library (RS256 on `crypto/rsa`), `go-github`
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
| socat (`docker.io/alpine/socat:1.8.1.3`) | Throwaway backends in `test/vm/test_reconciliation.sh` and `test/vm/test_traefik_configs.sh` (VM only) | Test only, never shipped; index digest |
| curl (`docker.io/curlimages/curl:8.12.1`) | Allowlist probe in `test/vm/test_traefik_configs.sh` (VM only) | Test only, never shipped; index digest |
| Pebble (`ghcr.io/letsencrypt/pebble:2.10.3`) | Local ACME test CA for `TLS-5` and `DEC-20` on `dermaga-vm` | Test only, never shipped; index digest |

The hook relay (`hook-relay.container`) runs the static `dermaga` binary on an empty root filesystem (`Rootfs=`, no base image, no digest, `DEC-30`).

Queries for these pins:

- Traefik, 2026-11-17, Docker Hub registry v2 API (DEC-20; log removed 2026-11-17, see git history before this change):

  ```text
  $ HEAD /v2/library/traefik/manifests/v3.7.19 (Accept: OCI index, Docker manifest list)
  content-type: application/vnd.oci.image.index.v1+json
  docker-content-digest: sha256:aa942a00bce033c909bf1f7e861d93761be4b31a035f10d2410d33e38a8f41cf
  ```

- socat and curl, 2026-11-17, skopeo 1.22.3 on the dev machine (`socat` had no tag; `1.8.1.3` is the
  tag `latest` pointed to). Full output: `tmp/evidence/M3/logs/process2-test-image-digests.log`.

  ```text
  == docker.io/alpine/socat: which version tag latest points to; digest = skopeo inspect --raw docker://IMAGE | skopeo manifest-digest /dev/stdin
  latest sha256:71131702f671a88b3808a70b7330f9679cd9b9933adcafadc191508edfe55bba
  1.8.1.3 sha256:71131702f671a88b3808a70b7330f9679cd9b9933adcafadc191508edfe55bba
  == docker.io/curlimages/curl:8.12.1
  8.12.1 sha256:94e9e444bcba979c2ea12e27ae39bee4cd10bc7041a472c4727a558e213744e6
  ```

- Pebble, 2026-11-25, `skopeo` (`tmp/evidence/M4/logs/pebble-image-query.log`):

  ```text
  ghcr.io/letsencrypt/pebble:2.10.3@sha256:0c81f05db371f436c97fb894bd6d2ab8fc9c8e2ce40f8bef8a8e15941bcd75ef
  ```

## 4. Host packages (Debian 13, installed by the installer)

| Package                                                                    | Purpose |
| -------------------------------------------------------------------------- | ------- |
| podman, passt, crun, netavark, aardvark-dns, catatonit, uidmap             | Rootless containers and networking |
| dbus-user-session                                                          | The systemd user bus `systemctl --user` talks to |
| nftables, fail2ban, python3-systemd, openssh-server, unattended-upgrades, apt-listchanges, needrestart, systemd-zram-generator | Host hardening (python3-systemd: fail2ban's journal backend) |
| sudo, iproute2                                                             | sudo for `server`; `ss` and `ip` for the installer's checks |
| systemd-timesyncd (if no other time daemon)                                | Correct clock for TOTP and certificates |
| git                                                                        | Mirrors and builds |
| curl, ca-certificates                                                      | Installer downloads |

## 5. Development and runtime requirements

| Requirement                     | Source |
| ------------------------------- | ------ |
| Go                              | The `go` line in `go.mod` |
| Bun                             | Pinned in CI and the `Makefile` |
| Podman (minimum version)        | `doctor.MinPodmanVersion` (5.4.2, Debian 13's); checked by `doctor`, which the installer runs |
| CI job packages                 | `.github/workflows/ci.yml` and `release.yml` install `zsh fish openssh-client openssh-server` (LAP-1 and installer tests, `sshd -T`) and, for releases, `gh` from Debian trixie; `ci.yml` also installs `shellcheck` (DEC-27). Queried 2026-11-17 with the Debian madison API (log removed 2026-11-17, see git history before this change): `gh \| 2.46.0-3 \| trixie`, `zsh \| 5.9-8 \| trixie`, `fish \| 4.0.2-1 \| trixie`, `openssh-server \| 1:10.0p1-7+deb13u4 \| trixie`. shellcheck, 2026-11-17: `curl -fsS --connect-timeout 10 --max-time 30 "https://qa.debian.org/madison.php?package=shellcheck&s=trixie&text=on"` → `shellcheck \| 0.10.0-1 \| trixie` (`tmp/evidence/M3/logs/process2-shellcheck-debian-query.log`) |
| libvirt + a Debian 13 cloud image | The test VM (`docs/SETUP.md`) |
| `shellcheck` 0.10.0 (workstation) | The same binary CI installs (Debian trixie `shellcheck` 0.10.0-1), so `make check` runs shellcheck locally instead of `SKIP` (DEC-27). Upstream publishes no checksums for v0.10.0, so it comes from Debian: `https://deb.debian.org/debian/pool/main/s/shellcheck/shellcheck_0.10.0-1_amd64.deb`, sha256 `8b207f7d4421a46cec9e377c74d450e1717092b58ae3e7151cd5f85f639065cd` from trixie's `main/binary-amd64/Packages.xz`, whose sha256 `be3d7d96a040eda0505c1808cccc67f183bbc05dff0842c50f88e6c396557412` matches trixie's `InRelease` (fetched over HTTPS; the archive signature was not checked because Debian's keyring is not installed here). `/usr/bin/shellcheck` extracted with `ar` + `tar` to `~/.local/bin/shellcheck` (sha256 `03c66fc2bb5ee853c98a9d8759043d1b49a49cd4622fea81ac4bd6fdc8714152`). Query 2026-11-26 (`tmp/evidence/M7/logs/shellcheck-install.log`). |
| `oha` v1.17.0 (workstation, VM tests only) | Load generator for the zero-downtime proof (`REL-6`, M7): `-q 50 -w`, `--cacert` (Pebble root), `--connect-to` (app domain → VM IP), JSON output. `https://github.com/hatoo/oha/releases/download/v1.17.0/oha-linux-amd64`, sha256 `85c6eb98066b8c8bd513b2dd96fafd8f9863249ea653606bae1ca2e3a75c43d2` (GitHub asset digest). Query 2026-11-26: `gh api repos/hatoo/oha/releases/latest` → `v1.17.0`, published 2026-11-14 (`tmp/evidence/M7/logs/tools-query.log`). Installed to the gitignored `tmp/tools/bin/` after `sha256sum -c`. Why not curl in a loop: no fixed request rate, no latency correction, no per-status counts. |
| `gosmee` v0.32.1 (workstation, VM tests only) | Forwards real GitHub deliveries from a smee channel to the VM behind NAT (M7b push-to-deploy). `https://github.com/chmouel/gosmee/releases/download/v0.32.1/gosmee_0.32.1_linux_x86_64.tar.gz`, sha256 `69e5d2593022f0cd30cc38304863b94a39e5cf42631763b9e7969e563be4e389` (release `checksums.txt` and GitHub asset digest agree). Query 2026-11-26: `gh api repos/chmouel/gosmee/releases/latest` → `v0.32.1`, published 2026-10-09 (`tmp/evidence/M7/logs/tools-query.log`). Installed to `tmp/tools/bin/` after `sha256sum -c`. Never used by automated tests (signed replays only). |

## 6. Known advisories

Re-checked at the start of every milestone (`govulncheck`, `bun audit`).

| Advisory | Reached? | Decision | Date |
| -------- | -------- | -------- | ---- |
| [GHSA-gcqx-w48m-w2h4](https://github.com/advisories/GHSA-gcqx-w48m-w2h4) — deepmerge ≥4.2.0 prototype poisoning (low) | No — transitive via `@sveltejs/vite-plugin-svelte`, build-time only, never in the browser bundle | Accepted; waiting for upstream fix in vite-plugin-svelte | 2026-11-16 |
