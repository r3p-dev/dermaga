# Build `dermaga`: a single-user, Litepod-style PaaS for one hardened Debian VPS

You are building a new open-source project from an empty repository. Read this whole brief before doing anything. It defines the goal, the decisions already made, the hard constraints, the milestones, and how to report back.

The working name is **`dermaga`** (Indonesian for "pier"). It is used for the binary, the systemd unit, paths and the repo. Keep the name in one constant/config place so it can be renamed later.

---

## 1. Goal

One person (me) owns one VPS and wants a Coolify/Litepod-like experience without their weaknesses:

- **Connect a GitHub repo, push a commit, and it is live**: built on the VPS, deployed with zero downtime, rolled back automatically if the new release is unhealthy.
- **A web UI** to see apps, deployments, live logs, resource usage, databases and backups, and to change settings. **A CLI with the same capabilities**, because I prefer working in the terminal.
- **One install command** turns a fresh Debian 13 server into a hardened, ready-to-use host.
- **Security first**: rootless everything, **the admin panel is never reachable from the internet** (only through an SSH tunnel), minimal attack surface.
- **Smallest practical memory footprint**: the VPS has 4 cores / 8 GB and must keep its RAM for the apps.

It is for **personal use only**. Prefer the simplest design that is safe and correct over generality.

### Reference material (read, do not assume)

1. **`https://github.com/r3p-dev/dotfiles`, file `vps-setup.sh`**: my existing, tested Bash script (~2700 lines). It already does server hardening, rootless Podman + Quadlet, Traefik v3 with socket activation, a release/rollback model, and a GitHub Actions → GHCR → SSH deploy flow. **Reuse its hardening and Traefik knowledge**, specifically these functions: `setup_base_system`, `setup_user`, `setup_sudo`, `ensure_subids`, `setup_memory`, `setup_kernel`, `setup_auto_updates`, `setup_podman`, `setup_firewall`, `setup_fail2ban`, `setup_ssh`, `render_traefik_static`, `render_traefik_dynamic`, `render_traefik_container`, `setup_traefik`, plus its validators (`valid_app_name`, `valid_image_ref`, …) and the rules enforced by its embedded Python (`vps_py`). Read them fully. Do **not** copy its deploy flow (Podlet, compose files, GitHub Actions, GHCR, forced SSH commands); that is what `dermaga` replaces.
2. **`https://github.com/litepodsh/litepod`** (Apache-2.0, except `ee/` which you must not read or copy): use it only as inspiration for UX and for two mechanisms: rollout (`apps/api/src/deployment/rollout.rs`) and per-database backups (`apps/api/src/database_backup/`). If you adapt any logic, keep attribution as Apache-2.0 requires. Do not fork it.
3. **`https://github.com/r3p-dev/portfolio`**: the first real app to deploy. **Discover** its build, port, health check and runtime from the repo itself (its `Containerfile`, `compose.yaml`, `server.ts`, `healthcheck.ts`, `astro.config.ts`). Don't guess.
4. **Kapturae**: the second real app. **Ask me for its repo URL** when you reach the milestone that needs it, and discover its stack from the repo.

Always check the current stable versions and current documentation of every tool and library you use (Go, SvelteKit, Svelte, Tailwind, Bun, Traefik, Podman/Quadlet, restic, Debian 13 packages) instead of relying on memory. Pin exact versions.

---

## 2. Decisions already made (do not re-litigate; flag only real blockers)

| Area          | Decision                                                                                                                                                                                                                                                                                                                                                                                                          |
| ------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Host OS       | Debian 13 (trixie) only, x86_64 and aarch64                                                                                                                                                                                                                                                                                                                                                                       |
| Users         | Exactly one Linux user (default name `server`), who owns every container. **No multi-user, teams, orgs, roles.**                                                                                                                                                                                                                                                                                                  |
| Containers    | Rootless Podman + **Quadlet** units managed by the systemd **user** manager (linger enabled)                                                                                                                                                                                                                                                                                                                      |
| Reverse proxy | **Traefik v3** with the **file provider only**. `dermaga` writes one dynamic YAML file per app, generated from settings I change in the UI/CLI. I never edit these files by hand. Traefik **no longer gets the Podman socket**: remove the docker provider and the socket mount that `vps-setup.sh` uses. Keep its socket activation for :80/:443, HTTP/3, ACME HTTP challenge, TLS options and global hardening. |
| Daemon + CLI  | **Go**, one statically linked binary (`CGO_ENABLED=0`). Prefer the standard library: `net/http` with Go 1.22+ routing patterns (no web framework), `os/exec`, `log/slog`, `crypto/*`, `embed`.                                                                                                                                                                                                                    |
| Persistence   | **SQLite via `modernc.org/sqlite`** (pure Go, no cgo), plain SQL with versioned migrations (no ORM), WAL mode.                                                                                                                                                                                                                                                                                                    |
| UI            | **SvelteKit with `adapter-static`, Svelte 5 runes, Tailwind CSS 4**, built with **Bun**, then **embedded into the Go binary with `embed`**. No Node/Bun at runtime.                                                                                                                                                                                                                                               |
| CLI           | The same binary: `dermaga <command>`. It talks to the daemon over a **Unix socket** whose filesystem permissions are the authentication.                                                                                                                                                                                                                                                                          |
| Panel access  | **SSH local port forwarding only** (see §3.1). No public hostname, no mTLS, no Tailscale, no Cloudflare Access.                                                                                                                                                                                                                                                                                                   |
| Source types  | **`git`** (GitHub repo, built on the VPS from a Containerfile/Dockerfile) and **`image`** (pull a public image, for third-party apps such as Uptime Kuma). No Compose support, no buildpacks/Railpack.                                                                                                                                                                                                            |
| App shape     | **One app = one container** listening on one HTTP port. Databases are separate first-class resources that apps link to.                                                                                                                                                                                                                                                                                           |
| Databases     | PostgreSQL, MariaDB, Valkey (Redis-compatible). Pinned major versions.                                                                                                                                                                                                                                                                                                                                            |
| Backups       | **restic** to an S3-compatible bucket (Cloudflare R2, Backblaze B2, …), scheduled by **systemd user timers** that call the CLI.                                                                                                                                                                                                                                                                                   |
| Git provider  | GitHub only. Private repos via a **per-app read-only deploy key** generated by `dermaga`. Auto-deploy via a **per-app GitHub webhook** with its own HMAC secret. No GitHub App, no OAuth.                                                                                                                                                                                                                         |
| Telemetry     | None. No phone-home, no update checks unless I run `dermaga self-update`.                                                                                                                                                                                                                                                                                                                                         |
| License       | Apache-2.0 for this project.                                                                                                                                                                                                                                                                                                                                                                                      |

### Explicit non-goals for v1

Multi-user/teams/SSO, multi-server/fleets, Docker Compose, Kubernetes, preview environments, AI assistant/MCP, billing, buildpacks, non-GitHub providers, non-Debian hosts, any public admin panel.

---

## 3. Architecture

### 3.1 Processes and network exposure

```
Internet ──► :80/:443 (systemd user sockets) ──► Traefik container (network "web")
                                                   ├──► app containers (by container name, network "web")
                                                   └──► dermaga hook listener: ONLY POST /hooks/github/<appId>

Laptop/phone ──ssh -L 7070:127.0.0.1:7070 (existing SSH key, custom port)──► dermaga panel 127.0.0.1:7070 (UI + JSON API)
Shell on server ──► dermaga CLI ──► Unix socket $XDG_RUNTIME_DIR/dermaga/dermaga.sock
```

- `dermaga` runs as a **systemd user service** of the single user. Never as root; it refuses to start as root.
- **Panel listener**: binds `127.0.0.1:7070` only (port configurable). It is reached exclusively through SSH local forwarding with my existing key. `vps-setup.sh` already sets `AllowTcpForwarding local` in sshd: verify this and keep it. The panel still requires login (defense in depth). `dermaga` must print a ready-to-paste `~/.ssh/config` snippet (a `Host` entry with `LocalForward 7070 127.0.0.1:7070`, `SessionType none`, `ExitOnForwardFailure yes`) and document phone access (an SSH app with port forwarding, e.g. Termius).
- **Hook listener** (decided after M0, see ARCHITECTURE D1): a separate listener in the same process on the Unix socket `$XDG_RUNTIME_DIR/dermaga/hooks.sock` (0600) that serves only `POST /hooks/github/{appId}` and `GET /healthz`. A relay container `dg-hooks` on network `web` runs `dermaga hook-relay` (the same static binary as a bare rootfs, read-only, all capabilities dropped, small memory/pids limits) and forwards TCP :8080 to the bind-mounted socket byte for byte. Traefik routes exactly `Host(<hook host>) && PathPrefix(/hooks/)` (e.g. `hooks.r3p.dev`) to `http://dg-hooks:8080`. No new host TCP port. All validation stays in the daemon; the relay is a dumb pipe.
- **CLI ↔ daemon**: Unix socket, mode `0600`, owned by the user. No password needed.
- No container, including Traefik, gets access to the Podman API socket. `podman.socket` stays disabled unless something proven needs it.

### 3.2 Talking to the host

- Use the `podman`, `systemctl --user`, `journalctl --user`, `git` and `restic` CLIs through `exec.CommandContext` with **argument slices only** and timeouts. Never `sh -c`, never interpolate input into a shell string.
- Validate every name that reaches a command or a file path (reuse `valid_app_name` semantics from `vps-setup.sh`: `^[a-z][a-z0-9-]{0,30}[a-z0-9]$`, plus a reserved-names list).
- Write config files atomically (temp file in the same directory, `fsync`, `rename`). Generate YAML/INI/TOML with a serializer, never by concatenating user input.
- Validate generated Quadlet units with `/usr/libexec/podman/quadlet -dryrun -user` against a staged copy of the unit directory before activating them (same technique as `deploy_release` in `vps-setup.sh`).

### 3.3 Files and directories (XDG, all owned by the user)

```
~/.config/dermaga/                config.toml (non-secret settings), master.key (0600)
~/.config/dermaga/env/<app>.env   runtime env file per app (0600), referenced by EnvironmentFile=
~/.local/share/dermaga/           dermaga.db (SQLite), repos/<app>/ (bare mirrors), deploy-keys/<app> (0600), known_hosts
~/.local/state/dermaga/           build logs, Traefik access log, backup logs
~/.config/containers/systemd/     generated Quadlet units (prefix "dg-")
~/.config/traefik/                traefik.yml (static), dynamic/00-global.yml, dynamic/app-<name>.yml, dynamic/99-custom.yml (never touched by dermaga)
```

### 3.4 Data model (SQLite)

Design the schema yourself from these needs, with migrations from day one:

- **settings**: admin password hash, TOTP secret, hook hostname, global defaults (memory/CPU/pids limits, rate limit, security headers), S3/restic settings, notification target.
- **apps**: name, source type, repo URL, branch, build context, Containerfile path, image ref (for `image` apps), container port, health check, resource limits, deploy strategy, auto-deploy on/off, watch paths, middleware config, deploy-key fingerprint, webhook secret (encrypted).
- **domains**: per app; primary domain plus aliases and a redirect-to-primary flag.
- **env_vars**: per app; values encrypted at rest.
- **releases / deployments**: id, app, trigger (webhook, manual, rollback), commit SHA and message, image ID, status state machine (`queued → building → starting → healthy → live | failed | rolled_back | superseded`), timestamps, log file path.
- **databases**: engine, version, name, volume, credentials (encrypted), linked apps, backup policy.
- **backups**: database or system, restic snapshot id, size, status, duration, error.
- **webhook_deliveries**: GitHub delivery id (dedupe), app, result.
- **metrics**: periodic samples per container (CPU %, memory), with retention.
- **audit_log**: every mutating action, by panel/CLI/webhook.

Secrets: AES-256-GCM (`crypto/aes` + `crypto/cipher`) with a random 256-bit key in `~/.config/dermaga/master.key` (0600). Be honest in the docs: env files must exist in plaintext (0600) for Podman; encryption at rest protects the database file and its backups.

### 3.5 Deploy pipeline

1. **Trigger**: webhook, UI button, CLI `dermaga deploy <app> [--ref <sha>]`, or rollback.
2. **Queue**: one global build queue, concurrency 1. Per app, a newer trigger supersedes a queued (not yet running) one.
3. **Fetch**: update the bare mirror over SSH with the app's deploy key (`GIT_SSH_COMMAND` with `-i <key> -o IdentitiesOnly=yes -o StrictHostKeyChecking=yes -o UserKnownHostsFile=<dermaga known_hosts>`), then export the exact commit into a temporary build directory.
4. **Build**: `podman build` with the app's Containerfile, tag `localhost/dermaga/<app>:<sha12>`, OCI labels (source, revision). Cap build resources with `podman build` memory/CPU flags (defaults: 2 GiB, 2 CPUs; configurable) and a timeout (default 20 min). Stream the build log to a file and to subscribers (SSE). **Mask env values** in logs.
5. **Start the candidate** as a new Quadlet unit `dg-<app>-<rel8>.container` on network `web` (plus linked database networks), with the app's `EnvironmentFile=`, memory/CPU/pids limits, `NoNewPrivileges=true`, `DropCapability=ALL` by default (per-app override list), optional `ReadOnly=true`, logs to journald, `Notify=healthy` when a health check exists.
6. **Health**, in priority order:
   - the app's configured HTTP health path, probed by the daemon from inside the rootless network namespace (candidate: `podman unshare --rootless-netns curl -fsS http://<container-ip>:<port><path>`; verify in M0);
   - otherwise the image/Quadlet `HealthCmd`;
   - otherwise "running for N seconds without restarting" (warn in the UI that this is weak).

   Require K consecutive successes within a start timeout (defaults: 3 successes, 120 s).

7. **Switch traffic** (decided after M0, see ARCHITECTURE D7/D8): atomically rewrite `dynamic/app-<name>.yml` so the Traefik service points to the candidate container. The file also contains a per-deploy token router `app-<name>-verify` (`Host(<primary>) && Header(X-Dermaga-Verify, <random token>)`) on an internal plain-HTTP entrypoint `verify` (`:8081`, inside the Traefik container, never published). **Only the token router is attached to `verify`**; the public `app-<name>` router stays on `websecure` only. The token router uses the same service as the public router and the public router's router-level middlewares **minus `basicAuth` and `ipAllowList`** (the probe has no basic-auth password and connects from the rootless netns). Verify by polling the health path through `verify` with the token (from the rootless netns, via `dermaga probe`) until it succeeds, deadline 10 s. Once verification succeeds, rewrite the file atomically without the token router, so it exists only while a deploy is in progress. If it fails, treat it as "Traefik did not load the config": restore the previous file, verify that, remove the candidate, and point the error at `journalctl --user -u traefik` and `99-custom.yml`. A `--resolve <domain>:443:127.0.0.1` request afterwards is a warning-only check, and only once a certificate exists.
8. **Drain and stop the old release** after a grace period (default 10 s). Remove its unit file. Keep the last N releases' images (default 5) for instant rollback; prune older images.
9. **Failure at any step**: the candidate is stopped and removed, the live release is untouched, the deployment is marked `failed` with the reason, and a notification is sent. **No visitor may see an error caused by a failed deploy.**
10. **Strategy `recreate`** (stop old, then start new) exists for apps that cannot run twice at once. It is the **default for apps with named volumes** (two writers on one volume is unsafe, e.g. SQLite). Blue-green is the default for stateless apps.
11. **Rollback** = deploy a previous release's existing image through the same pipeline (no rebuild).
12. **First deploy**: if it fails, nothing is left running and no route exists.

Concurrency safety: per-app lock; deterministic, idempotent reconciliation on daemon start (compare DB state vs. units vs. Traefik files; repair or report drift, never touch units or files without the `dg-`/`app-` prefix).

### 3.6 Per-app middleware (Traefik dynamic file per app, edited through UI/CLI)

Each app file contains its routers (HTTPS + redirect handling), its service and its middleware chain. Defaults come from the global settings and must reproduce what `render_traefik_dynamic` in `vps-setup.sh` applies today (read it: HSTS, nosniff, frame options, referrer policy, `Permissions-Policy`, stripping `Server`/`X-Powered-By`, rate limit, compression, TLS options). Per app, the UI/CLI can:

- toggle and edit security headers; add custom response headers (e.g. a CSP); remove a default header;
- toggle compression; set rate limit average/burst or disable it;
- basic auth (bcrypt hashes generated by `dermaga`);
- IP allowlist (`ipAllowList`);
- max request body size (`buffering`);
- redirects: aliases → primary domain (301), `www` ↔ apex;
- optional path-prefix routing with prefix stripping.

Model middleware as typed Go structs and serialize them. Never let user input inject arbitrary YAML. `99-custom.yml` is the documented escape hatch for anything the UI doesn't cover.

### 3.7 Databases

- Create from the UI/CLI: engine + version + name → Quadlet `.container` + named `.volume` + a **dedicated network `dg-db-<name>`**. Databases are **never** attached to `web` and are never routed by Traefik.
- Random credentials (≥ 32 bytes). Linking a database to an app attaches the app to that network and injects env vars (`DATABASE_URL` and engine-specific `PG*` / `MYSQL_*` / `REDIS_URL`), with editable names.
- Databases are always restarted in place (no blue-green). Version upgrades within a major are a manual "pull and restart" action. Major upgrades are out of scope (document dump/restore instead).
- Engine-appropriate capabilities: start from the minimum each official image needs under rootless Podman and test it. Do not blanket-drop capabilities that break the image.
- SSH-tunnel connection instructions in the UI, so I can connect a desktop DB client without exposing ports.

### 3.8 Backups and restore

- Settings: S3 endpoint, bucket, region, access key/secret, restic repository password. **Generate the restic password, show it once, and tell me to store it offline**; losing it means losing every backup.
- Per database: schedule (default daily 03:00 Asia/Jakarta with randomized delay), retention (default `--keep-daily 7 --keep-weekly 4 --keep-monthly 6`), generated as a systemd user timer + service calling `dermaga backup run db:<name>`.
- Dumps stream straight into restic without temporary files (prefer `restic backup --stdin-from-command` if the installed restic supports it, so a failing dump fails the backup): PostgreSQL `pg_dump -Fc`, MariaDB `mariadb-dump --single-transaction --routines --triggers --events`, Valkey via `BGSAVE` then the RDB file.
- **System backup** (also scheduled, daily): a consistent SQLite snapshot (`VACUUM INTO`), `config.toml`, env files, deploy keys, Traefik config. Excludes `master.key`, which I must export once (`dermaga keys export`) and keep offline. Document this clearly.
- Optional per-app named-volume backups (`podman volume export`), off by default, with a warning about consistency for live writers.
- Weekly `restic check` (and periodic `--read-data-subset`); `restic forget --prune` after backups.
- Restore: into the same database (takes an automatic safety dump first, requires typed confirmation) or into a new database. **"Test restore"** action: restore the latest snapshot into a throwaway container, run a sanity query, report, and clean up.
- Disaster recovery doc + command: fresh server → install → `dermaga restore-system` from the bucket → apps redeployed from their last good commits, databases restored.

### 3.9 Observability

- Live runtime logs per app via `journalctl --user` (JSON output, follow) streamed over SSE; build logs as files streamed the same way.
- Container CPU/memory sampled every 30 s from `podman stats --no-stream --format json`, kept 7 days, charted in the UI.
- Request analytics per app (requests, 4xx/5xx, p50/p95 latency) from Traefik's JSON access log written to a bind-mounted state directory, parsed incrementally by the daemon, aggregated per minute, kept 14 days. Rotate/truncate the log file yourself.
- Server overview: CPU, RAM (available, not free), swap, disk, uptime, pending security updates, reboot-required flag.
- Notifications (optional, one target): ntfy, a Telegram bot or a Discord webhook, for deploy failed/succeeded, backup failed, health check flapping, disk > 85 %.

### 3.10 UI

Minimal, fast, dark and light themes, keyboard-friendly, no heavy component framework. Pages:

- **Dashboard**: server overview + app cards (status, live release, last deploy, CPU/RAM sparkline).
- **App**: Overview · Deployments (list, live log, rollback, redeploy) · Logs · Environment · Domains & Middleware · Database links · Settings (source, build, health, resources, strategy, auto-deploy, watch paths) · Danger zone (remove; keep or purge data).
- **New app** wizard: repo URL → deploy key shown with a link to the repo's GitHub "Deploy keys" page → webhook URL + secret shown with a link to its "Webhooks" page → port, domain, health path → first deploy. Detect `Containerfile`/`Dockerfile` and an `EXPOSE`d port from the repo to prefill fields as suggestions I confirm, never silently.
- **Databases**: list, create, connection info, link to apps, backups, restore, test restore.
- **Backups**: S3 settings, schedules, history, system backup, key export instructions.
- **Settings**: password, TOTP, global defaults (limits, headers, rate limit), hook hostname, notifications, SSH-tunnel snippet.
- **Audit log**.

Use SSE for streams. The UI calls the same JSON API the CLI uses. Keep API types in Go as the single source of truth and generate the TypeScript types from them (evaluate a generator such as `tygo`, or emit JSON Schema/OpenAPI from Go). Never hand-maintain two copies.

### 3.11 CLI (parity with the UI)

At least: `status`, `apps [list|show|create|edit|remove]`, `deploy`, `rollback`, `logs [-f] [--build]`, `env [list|set|unset|edit]`, `domains`, `middleware`, `db [list|create|link|unlink|shell|remove]`, `backup [run|list|restore|test]`, `restore-system`, `keys export`, `admin set-password`, `panel` (prints the SSH tunnel snippet and URL), `self-update`, `doctor` (checks: lingering, subuids, sockets, Traefik health, DNS of configured domains, disk, restic reachability, clock sync). Human-readable output by default, `--json` for scripting. Start with the standard library `flag` package and a small subcommand router; justify any CLI framework in `docs/DEPENDENCIES.md` if you add one.

---

## 4. Security requirements (all mandatory)

1. Nothing runs as root after installation; the daemon refuses to start as root.
2. Panel bound to `127.0.0.1` only, reachable only through SSH forwarding. Reject requests whose `Host` header is not `127.0.0.1:<port>` or `localhost:<port>` (DNS-rebinding protection). Strict `Origin` check on mutating requests.
3. Auth: single admin password hashed with argon2id (`golang.org/x/crypto/argon2`), optional TOTP (RFC 6238) strongly encouraged at setup, login rate limiting with backoff, session cookie `HttpOnly; SameSite=Strict; Path=/`, idle timeout 12 h, absolute 7 d, logout invalidates server-side.
4. Webhooks: verify `X-Hub-Signature-256` HMAC-SHA256 with `hmac.Equal` **before** parsing JSON, 1 MiB body limit (`http.MaxBytesReader`), accept only `push` (and optionally `workflow_run` completed+success, see §5), only the configured repo and branch, dedupe on `X-GitHub-Delivery`, respond `202` immediately and enqueue, rate-limit per app. Unknown app id → `404` with no timing difference that reveals existence. Server timeouts (`ReadHeaderTimeout`, `ReadTimeout`, `WriteTimeout`, `IdleTimeout`) on every listener.
5. No shell interpolation anywhere; strict input validation; typed serialization for YAML/INI/TOML.
6. Secrets never logged, never returned by the API after creation except where explicitly revealed by a user action, and masked in build logs.
7. Containers: `NoNewPrivileges`, capability drop by default, memory/CPU/pids limits by default (read the defaults from `vps-setup.sh`'s `DEFAULT_MEMORY`, `DEFAULT_CPUS`, `DEFAULT_PIDS`), no published ports, no privileged mode, no host bind mounts for apps (named volumes only).
8. Supply chain: `go.sum` and `bun.lock` committed, minimal dependencies (justify each in `docs/DEPENDENCIES.md`), `govulncheck` in CI, release binaries with SHA-256 checksums; `self-update` verifies the checksum (and a signature if you add minisign/cosign). Traefik and database images pinned to explicit tags, auto-updated only within the pinned major as `vps-setup.sh` does.
9. Keep every host-hardening guarantee `vps-setup.sh` gives today (nftables default-deny with only SSH/80/443 open, fail2ban sshd + recidive, key-only SSH on a custom port with modern algorithms, `AllowUsers` the single user, local-only TCP forwarding, sysctl hardening, unattended-upgrades, zram). Do not weaken any of them.
10. Write `docs/THREAT-MODEL.md`: assets, entry points (public 80/443, hook endpoint, SSH + tunnel), what an attacker gains at each layer, mitigations, residual risks. Include the "sudo user" risk: recommend `SUDO_NOPASSWD=no`.

---

## 5. GitHub integration details

- Deploy key: ed25519, generated per app, private key 0600, public key shown in the UI with exact instructions (read-only, never "allow write access").
- Webhook: content type `application/json`, secret generated per app (≥ 32 bytes), events "Just the push event". Show the exact URL `https://<hook-host>/hooks/github/<appId>`.
- **Optional CI gate per app**: instead of deploying on `push`, deploy on `workflow_run` with `conclusion == success` for a named workflow on the configured branch (so an existing `ci.yml` still gates deploys). Document which webhook events to tick for this mode.
- Watch paths (optional glob list): deploy only if the push touched matching files; if the payload doesn't list files, deploy anyway (fail open).
- Pin GitHub's SSH host keys: fetch them from GitHub's published metadata at install time, show the fingerprints, and store them in a known_hosts file used only by `dermaga`.

---

## 6. Installer

`install.sh` (Bash, idempotent, `set -Eeuo pipefail`, re-runnable) in this repo:

1. Port the hardening parts of `vps-setup.sh` listed in §1 (keep its config-file approach: `install.sh config-example > dermaga-host.conf`, then `sudo bash install.sh init --config dermaga-host.conf`). Keep its safety checks (refuse to disable password login if the user has no key, validate sshd config before restart, never `systemctl restart nftables`, detect something else on :80/:443, and so on).
2. Install Podman, Traefik (as a Quadlet unit with socket activation, **file provider only**), restic, git, curl.
3. Download the `dermaga` release binary for the architecture, verify SHA-256, install to `/usr/local/bin/dermaga`.
4. Create the user service, enable linger, start it, run `dermaga doctor`.
5. Print next steps: the `~/.ssh/config` snippet for the panel tunnel, the panel URL (`http://localhost:7070`), how to set the admin password (`dermaga admin set-password`), and the TOTP setup.

Also provide `docs/MIGRATING-FROM-VPS-SETUP.md`: how to move a server already set up with `vps-setup.sh` (apps deployed via its label-based Traefik routing) to `dermaga` with minimal downtime. Read `vps-setup.sh` to get the real paths, unit names and Traefik layout. The key risk is that switching Traefik to file-provider-only removes label-based routes, so recreate each app in `dermaga` first, then switch, then remove the old `vps` units. Include a rollback path.

---

## 7. Repository layout and quality bar

```
cmd/dermaga/          main: daemon + CLI entry
internal/             api, auth, config, store (SQLite + migrations), deploy, build, gitsrc,
                      podman, quadlet, traefik, backup, metrics, webhook, notify, cli, ...
ui/                   SvelteKit (adapter-static, Svelte 5, Tailwind 4), built with Bun
install/              install.sh + config example
docs/                 ARCHITECTURE, PLAN, THREAT-MODEL, OPERATIONS, DEPENDENCIES, MIGRATING-FROM-VPS-SETUP
Makefile              ui build → embed → go build (both architectures), test, lint
```

- `gofmt`, `go vet`, `staticcheck`, `govulncheck`. TypeScript strict + Prettier for the UI.
- **Golden-file tests** (`go test ./... -update` to refresh) for every generator: Quadlet units, Traefik dynamic files, systemd timers. Webhook signature/branch/dedupe tests. Deployment state-machine tests with host commands behind an interface that tests can fake.
- **Integration test harness** for a real Debian 13 VM (document how I run it on my Fedora laptop with libvirt/GNOME Boxes, or how CI can run it): install from scratch, deploy a sample app, blue-green redeploy under load, failed deploy under load, rollback, DB create + backup + test restore, reboot survival.
- Each milestone ends with all tests passing and a "Suggested commits" list (§11). I make the commits.

---

## 8. Milestones

**M0: Spikes and plan. Stop and report after this.**
Validate on Debian 13 with rootless Podman, then report what works, with evidence (commands + output):

1. The Traefik container (rootless, pasta, network `web`) reaching a listener on the host for `/hooks/` without exposing anything new.
2. Daemon-side HTTP health probe into a container on network `web` (e.g. `podman unshare --rootless-netns …`), or the best alternative.
3. Traefik file-provider hot reload with atomic rename in a bind-mounted directory; measure reload latency.
4. Quadlet `Notify=healthy` behaviour and `quadlet -dryrun` validation for generated units.
5. A minimal Go binary with `modernc.org/sqlite` and an embedded static SvelteKit build: binary size and idle RSS.
6. `restic backup --stdin-from-command` availability in Debian 13's restic, and failure propagation.
7. SSH local forwarding to `127.0.0.1:7070` works with the sshd config produced by `vps-setup.sh`.

Then write `docs/ARCHITECTURE.md` and `docs/PLAN.md` (milestones with acceptance criteria and an hour estimate per milestone), listing any decision in §2 you believe must change and why. **Wait for my confirmation before M1.**

**M1: Daemon core.** Config, SQLite + migrations, encrypted secrets, Unix-socket API + CLI skeleton, panel listener with auth/TOTP/sessions/CSRF/host checks, audit log, `doctor`, `panel`.

**M2: Generators.** Quadlet units, Traefik static + global + per-app dynamic files with the full middleware model, systemd timers. Golden tests. Reconciliation on startup.

**M3: Git apps, recreate strategy.** Deploy keys, mirrors, builds with limits and live logs, env vars, domains, webhook endpoint with all §4.4 checks, `image` apps. Deploy `r3p-dev/portfolio` end to end in the VM.

**M4: Zero-downtime.** Blue-green pipeline (§3.5), health probing, verification through Traefik, drain, rollback, release retention, image pruning, supersede/queue semantics. Acceptance: with a load generator (e.g. `oha`) at 50 req/s against the app during (a) a successful redeploy and (b) a deploy whose health check fails, **zero non-2xx responses and zero connection errors**.

**M5: Databases.** Engines, per-database networks, linking + env injection, in-place restarts, SSH-tunnel instructions.

**M6: Backups.** restic settings, DB dumps, system backup, timers, retention, check, restore, test restore, `restore-system`. Acceptance: destroy the VM, rebuild from the bucket, and everything comes back.

**M7: UI.** All pages in §3.10 on top of the existing API, with SSE streams and generated TypeScript types.

**M8: Installer and releases.** `install.sh` (§6), release workflow building binaries + checksums for both architectures, `self-update`.

**M9: Observability and notifications.** Metrics sampling, access-log analytics, charts, notifications.

**M10: Hardening review and migration.** Threat model, security self-review against §4 with findings fixed, `MIGRATING-FROM-VPS-SETUP.md`, then migrate portfolio and Kapturae in the VM following the guide.

## 9. Global acceptance criteria

- Fresh Debian 13 → one install command → `dermaga doctor` all green.
- From outside, a port scan shows only SSH (custom port), 80 and 443. The panel is unreachable from the public IP and reachable at `http://localhost:7070` through the SSH tunnel.
- `git push` to the portfolio's `main` → live on its domain without manual steps, zero failed requests during the switch.
- A broken commit never takes the site down.
- Response headers on the deployed portfolio match what `vps-setup.sh`'s Traefik produces today (compare `curl -sI` before/after), and responses are compressed.
- **Daemon idle RSS < 30 MB**; Traefik + daemon together add < 100 MB.
- Backups run on schedule; a test restore succeeds; full disaster recovery is documented and was exercised once.
- Everything survives a reboot without intervention.

## 10. How to work with me

- Ask before any decision that is hard to reverse or that contradicts §2. Otherwise decide, write the decision and its reason in `docs/ARCHITECTURE.md`, and continue.
- After each milestone, report briefly: what was built, how it was tested (with evidence), what deviated from this brief, open questions, the suggested commits (§11), and the next milestone.
- Never claim something works without having run it. If something can't be tested in your environment, say so explicitly and give me the exact commands to run it on my machine or VM.

### Test environment

- All VM work runs on `dermaga-vm` (`ssh dermaga-vm`: user `server`, port 4222, passwordless sudo). **`server` in my SSH config is my production VPS. Never connect to it or run anything on it.**
- libvirt snapshots exist: `fresh` (bare Debian 13 cloud image, only cloud-init's user `debian`) and `vps-setup` (after `vps-setup.sh init`). You cannot reach `qemu:///system`; when a scenario needs a revert, stop and give me the exact command (`virsh --connect qemu:///system snapshot-revert dermaga-vm <name>`), then continue after I confirm.
- The VM is behind NAT: no inbound webhooks and no real ACME certificates. Use the Let's Encrypt staging resolver or self-signed certificates in the VM. For real GitHub deliveries without inbound exposure, use GitHub's webhook forwarding (`gh webhook forward`, from the `cli/gh-webhook` extension; verify it still works) into the local hook endpoint, plus replayed signed deliveries in automated tests. The real public push-to-live and certificate check happens during the actual migration in M10.
- For backups before M6 has a bucket, use a local restic repository. I will provide a test R2 or B2 bucket when M6 starts.

## 11. Git workflow: I commit, you suggest

- **Never create commits, push, tag, rebase, reset, stash, or create/switch branches. Don't stage files with `git add` either.** Read-only git (`status`, `diff`, `log`, `show`) is fine. I review and commit everything myself.
- At the end of every milestone (and at any natural checkpoint inside a long milestone), add a **"Suggested commits"** section to your report: an ordered list of small, focused commits. For each one give:
  - the Conventional Commits message: subject ≤ 72 characters, imperative mood, a scope where it helps (`feat(deploy): …`, `test(traefik): …`, `docs: …`), and a short body explaining **why**;
  - the exact paths it includes;
  - a copy-pasteable command for the **fish** shell (no heredocs, no `$'…'`), in this form:
    `git add path/a path/b; and git commit -m "type(scope): subject" -m "Body paragraph."`
- Every path that changed must appear in exactly one suggested commit. Run `git status --short` at the end and confirm that nothing is left out and nothing is listed twice.
- Commit messages contain **no AI attribution**: no `Co-Authored-By`, no "Generated with", no mention of Claude or any assistant.
- Never put secrets, tokens, `master.key`, `.env` files or VM-specific credentials in a suggested commit. Keep `.gitignore` up to date and mention every change to it.
