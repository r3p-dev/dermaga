# Architecture

**§1 is set by the project owner.** Do not change it without asking; if you believe a decision must
change, say so in your report and why. Decisions marked **(verify in M0)** rest on host behavior that
M0 confirms on `dermaga-vm` before code depends on it. **§2 onward is written and kept current by the
implementer** (first in M0, then whenever packages, routes, schema, state machines, generated files or
decisions change).

---

## 1. Fixed decisions

### 1.1 Technology

| Area               | Decision |
| ------------------ | -------- |
| Language and build | **Go**, standard library first (`net/http` routing, `database/sql`, `flag`, `log/slog`, `crypto/*`, `embed`, `os/exec`). **One static binary** (`CGO_ENABLED=0`) for amd64 and arm64 that is the daemon, the three server commands (`SRV-1`), and the small helpers that run elsewhere (`hook-relay` inside the hook relay container, `probe` inside the rootless network namespace). Linters and code generators as `tool` directives in `go.mod`. `make check` runs everything. |
| Web UI             | **SvelteKit** with `adapter-static` (SPA), **Svelte 5 runes**, **Tailwind CSS v4**, TypeScript strict, built with **Bun** (`bun.lock` committed), embedded in the binary. No Node or Bun at runtime. API types generated from Go structs (tygo). The panel is the only user interface. |
| Host               | **Debian 13 only.** One Linux user `server` owns everything. dermaga runs as a **systemd user service** with lingering; it refuses to run as root. |
| Containers         | **Rootless Podman with Quadlet** under the systemd user manager. No Docker, no Compose, no Podman API socket in any container. Every generated unit set is validated with Quadlet's dry run before it is installed. Apps are stateless: no volumes in v1. |
| Reverse proxy      | **Traefik v3** in a container, socket-activated on :80/:443 (TCP 80 via `traefik-http.socket`, and TCP + UDP 443 via `traefik-https.socket` with shared `FileDescriptorName=websecure` for HTTP/3, verified in M0 on production and `dermaga-vm` per HOST-6) by user-manager sockets, **file provider only**. dermaga writes every dynamic file from typed structs (as JSON, which Traefik accepts in `.yml`). Default middleware reproduces `vps-setup.sh`'s headers, rate limit and compression; middleware chains are per router. |
| HTTPS              | **Traefik's ACME** with Let's Encrypt issues and renews certificates; dermaga only configures the resolver and reads the result to show certificate status (`TLS-3`). Certificate storage at `~/.local/share/dermaga/traefik/acme.json` owned by `server`, 0600. Development uses Pebble (local ACME test server) on `dermaga-vm` behind NAT; staging/production on real hosts. **(verify in M0: challenge type with socket activation)** |
| Panel access       | The panel listens on **`127.0.0.1:7070` only, in every state** (including install and setup), reached through SSH local forwarding. No public panel, no inbound listener for GitHub. |
| Server commands    | The three commands of `SRV-1` talk to the daemon over a **Unix socket (0600)**; its file mode is the authentication. If the daemon is inactive, recovery commands (`dermaga admin set-password` and `dermaga admin totp disable`) fall back to direct SQLite access via the shared store package (WAL + `busy_timeout`), revoking all active sessions. |
| State              | **SQLite** (`modernc.org/sqlite`, no cgo), one database file at `~/.local/share/dermaga/dermaga.db` (0600), numbered SQL migrations. Until v1.0.0, schema changes may **edit the original migration** (the dev database is reset); from v1.0.0 on, new migrations only. Settings live in SQLite; there is no config file to edit. |
| Secrets            | Encrypted at rest with **AES-256-GCM** under a master key file at `~/.config/dermaga/master.key` (0600), bound to their column. Env files for Podman are plaintext 0600 (stated honestly in the docs). |
| GitHub             | **GitHub App only.** Created by the manifest flow from the panel with redirects to `localhost:7070` in the browser; webhook URL and freshly generated 32-byte secret configured post-conversion via `PATCH /app/hook/config` (App JWT); per-fetch installation tokens scoped to one repo, passed to git through environment config only (HTTP Basic auth `x-access-token`); the App JWT on `crypto/rsa`; a handful of REST endpoints on `net/http`. |
| Webhooks           | One server-wide endpoint, served on a **Unix socket** (`$XDG_RUNTIME_DIR/dermaga/hooks.sock`) and reached by Traefik through a tiny relay container on network `web` that forwards bytes and parses nothing. Reached via canonical path `/hooks/github-app` on the reserved hook domain. No new host TCP port. All validation in the daemon: HMAC `X-Hub-Signature-256` constant-time compare, reject non-POST, bounded read (`http.MaxBytesReader`) with 25 MiB body cap, unknown events 2xx ignored. **(verify in M0)** |
| Deploys            | One global build queue. Bare mirrors and `git archive` exports in cache; `podman build` with limits; build-time variables passed as `--build-arg`, with clear notice that values can be embedded in image history (secrets must not be set as build variables; `--secret` mount is a candidate, **verify in M0**). **Blue-green only.** Health checks run from the rootless network namespace (5 s stability window if health check is "none") **(verify in M0)**. Every route change is **verified through Traefik** (a temporary per-switch token router on an internal entry point) and restored on failure. Images and runtime configuration snapshots of the last 5 releases kept for rollback. |
| Installer          | **Bash**, interactive, from a GitHub release asset; checksum-verified binary download. |
| Updates            | In the panel on request: checksum-verified download, atomic replace, automatic restore of the previous version if the new one fails to start. |
| Language           | UI, server command output, code, comments, docs and commit messages English. |
| Delivery           | GitHub releases: static binaries for both architectures, `checksums.txt`, `install.sh`. |

### 1.2 Security requirements (all mandatory)

Exposure

- **SEC-1** Nothing runs as root after installation.
- **SEC-2** The panel binds `127.0.0.1` only and is reachable only through SSH local forwarding, at
  every moment, including install and first-run setup.
- **SEC-3** DNS-rebinding protection: requests whose `Host` is not `127.0.0.1:<port>` or
  `localhost:<port>` are rejected; mutating requests need a matching `Origin`.
- **SEC-4** The only public entry points are Traefik on 80/443 and the canonical `/hooks/github-app`
  path on the hook domain. No new host TCP port; no published container ports.
- **SEC-5** No container, including Traefik, gets the Podman API socket.
- **SEC-6** Network access never replaces the panel login.

Authentication and secrets

- **SEC-7** argon2id password, optional TOTP, login backoff. Session cookie `HttpOnly;
  SameSite=Strict; Path=/`, only its hash stored; idle and absolute limits enforced on the server.
- **SEC-8** Setup token: 32 random bytes, stored hashed, single use, expires after 24 h, accepted only
  on the loopback listener, invalid once an admin password exists.
- **SEC-9** Every secret (env values, the GitHub App key and webhook secret) encrypted at rest; never
  logged; never returned after creation except through an explicit, audited reveal; masked in build
  logs.
- **SEC-10** Password and TOTP changes on the panel require re-authentication.

Webhooks

- **SEC-11** Verify `X-Hub-Signature-256` (HMAC-SHA256, constant-time compare) **before** parsing JSON;
  bounded read (`http.MaxBytesReader`) with body limit 25 MiB. On verification failure, log only the
  delivery ID, event, source and "signature mismatch" — never log the expected signature, received
  signature, secret or payload (avoids turning logs into a signature oracle).
- **SEC-12** Accept only `push` and the installation events; deploy only for a repo and branch that
  belong to an app.
- **SEC-13** Dedupe on `X-GitHub-Delivery`; respond `202` immediately and enqueue; rate-limit per app
  after the signature is valid.
- **SEC-14** Responses don't reveal whether a repo is known: an unknown repo gets the same `202` as a
  known one.
- **SEC-15** Every listener has read-header, read, write and idle timeouts.

Host commands and generated files

- **SEC-16** No shell interpolation anywhere: commands run as argument lists with timeouts, also inside
  containers.
- **SEC-17** Every name that reaches a command or a path is validated
  (`^[a-z][a-z0-9-]{0,30}[a-z0-9]$` plus reserved names).
- **SEC-18** Generated files come from typed serialization, written atomically; dermaga never touches a
  unit or file without its own prefixes.

Containers

- **SEC-19** `NoNewPrivileges`, all capabilities dropped by default (per-app additions), memory, CPU and
  pids limits by default, no privileged mode, no host bind mounts, no volumes.

Supply chain

- **SEC-20** Minimal dependencies, each justified in `docs/DEPENDENCIES.md`; lockfiles committed; exact
  versions everywhere, including CI actions (by SHA) and images (tag and digest).
- **SEC-21** `govulncheck` in CI; advisories re-checked at every milestone.
- **SEC-22** The installer and the panel's update refuse a binary whose checksum cannot be verified.
- **SEC-23** No telemetry, no phone-home.

Accepted residual risks (each goes into `docs/THREAT-MODEL.md`): all apps share network `web`, so an
app can reach another app directly; `net.ipv4.ip_unprivileged_port_start = 80` for the user-manager
sockets; plaintext env files (0600); a sudo-capable `server` user.

---

## 2. Overview

_Written by the implementer in M0: one diagram of the processes, sockets, networks and exposure, and a
short description of each part._

## 3. Components

_Go packages and UI structure: one line per package with its job._

## 4. Data and files

### 4.1 Directory layout and permissions

dermaga strictly follows the XDG base directory specification for user `server`. All created
directories are mode `0700`, and sensitive files are mode `0600`.

| Path | Purpose | Persistence | Permissions |
| ---- | ------- | ----------- | ----------- |
| `~/.local/share/dermaga/dermaga.db` | Primary SQLite database (WAL mode) | Persistent | 0600 file |
| `~/.config/dermaga/master.key` | Master encryption key (AES-256-GCM), kept separate from DB | Persistent | 0600 file |
| `~/.cache/dermaga/mirrors/<app>.git` | Bare Git mirrors for builds | Rebuildable (cache) | 0700 dir |
| `~/.cache/dermaga/builds/<app>/<deploy-id>` | Ephemeral build contexts (`git archive` export) | Temporary (cleaned post-build and at startup) | 0700 dir |
| `$XDG_RUNTIME_DIR/dermaga/` | Runtime directory (`hooks.sock`, `daemon.sock`); managed by systemd user unit via `RuntimeDirectory=dermaga` | Ephemeral (tmpfs) | 0700 dir, 0600 sockets |
| `~/.local/share/dermaga/traefik/dynamic/` | Generated Traefik dynamic configurations (`.yml` holding JSON) | Managed files | 0700 dir, 0600 files |
| `~/.local/share/dermaga/traefik/acme.json` | ACME certificates stored by Traefik | Persistent | 0600 file |

Mounting note: Traefik mounts `~/.local/share/dermaga/traefik/dynamic/` as a directory mount into
`/etc/traefik/dynamic/` (never bind-mounting individual files, which breaks inotify on atomic renames).
All dynamic configuration updates write to a temporary file in the same directory, `fsync`, and
atomically `rename` into place.

### 4.2 SQLite schema

_Detailed table definitions and migrations written in M0 step 5._

## 5. Flows

_Install, first run, connecting GitHub, deploy (blue-green, rollback, route edit, cancel), webhook,
certificate status, cleanup, update, reconciliation: from the trigger to the host and back._

## 6. State machines

_Deployment status (`DEP-9`), app runtime status (`RUN-1`), setup mode (`SETUP-1`), update
(`UPD-2`–`UPD-3`): states and transitions as tables, each changed in exactly one place._

## 7. Decisions log

Every decision not covered by §1, and **every workaround**, gets an entry: number, date, decision,
reason, alternatives considered, and links.

| #   | Date | Decision | Reason | Alternatives considered |
| --- | ---- | -------- | ------ | ----------------------- |
