# dermaga architecture

Status: **after M1** (2026-11-07). D1 and D7 approved, including the token router's middlewares. [`BRIEF.md`](BRIEF.md) is the source of truth for goals and
constraints. This file records *how* dermaga meets them, and every decision taken along the way with
its reason. Evidence for the claims marked **[M0-n]** is in [`M0-EVIDENCE.md`](M0-EVIDENCE.md).

## 1. Processes and exposure

```
Internet ──► :80/:443  systemd user sockets (traefik-http/https.socket)
               └─► Traefik container ─── network "web" ───┬──► dg-<app>-<rel8>   app containers
                     │  web, websecure: public routers    └──► dg-hooks:8080      hook relay container
                     │  verify :8081: token routers only              │ bind-mounted Unix socket
                     │  (dermaga probe via rootless netns)            │
                     ▼                                                ▼
              routes PathPrefix(/hooks/) on the hook host ──► $XDG_RUNTIME_DIR/dermaga/hooks.sock
                                                                      │
Laptop ──ssh -L 7070:127.0.0.1:7070──► sshd :4222 ──► 127.0.0.1:7070 ─┤  dermaga daemon
                                                                      │  (systemd user service,
Shell on server ──► dermaga CLI ──► $XDG_RUNTIME_DIR/dermaga/dermaga.sock   user "server")
                                                                      │
              dermaga ──exec──► podman · systemctl --user · journalctl --user · git · restic
```

| Endpoint | Bound to | Who can reach it | Auth |
|---|---|---|---|
| Traefik web/websecure | systemd sockets :80, :443 (tcp+udp) | internet | per-app (basic auth, allowlist) |
| Traefik `verify` :8081 | inside the Traefik container only | every container on `web` and the rootless netns (dermaga's probe) | only `app-<name>-verify` routers, each matching a random per-deploy token; no public router is attached, because `verify` skips the entrypoint-level `secure@file` chain |
| Hook relay `dg-hooks:8080` | network `web` only | Traefik (and other `web` containers) | HMAC per app |
| Hook listener | Unix socket `hooks.sock`, 0600 | the relay (container root = uid `server`) | HMAC per app |
| Panel + JSON API | `127.0.0.1:7070` | SSH local forward only | password (+TOTP), session, Host/Origin checks |
| CLI API | Unix socket `dermaga.sock`, 0600 | user `server` | file permissions |
| Databases | `dg-db-<name>` networks | linked app containers | engine credentials |

Nothing new listens on a host TCP port except the panel on loopback. Containers cannot reach host
loopback, because Podman runs pasta with `--no-map-gw` **[M0-1a]**, so the panel can't be reached
from a compromised app container.

**Residual risk (accepted for v1): all apps share network `web`.** One app container can reach
another app container (`dg-<other>-<rel8>:<port>`), the hook relay and Traefik's `verify`
entrypoint directly, bypassing Traefik and with it per-app basic auth, IP allowlists, rate limits
and security headers. Access control in Traefik therefore protects an app from the internet, not
from the other apps on the same host. A possible later improvement is per-app networks
(`dg-app-<name>`, with Traefik attached to each), which isolates apps from each other. That costs
one network per app and a Traefik unit change per new app. This risk also goes into
`THREAT-MODEL.md` (M10).

## 2. Decisions

Each decision gives its reason and the evidence. "Brief" means the decision is already in BRIEF §2.

| # | Decision | Why |
|---|---|---|
| D1 | **Hook path = Unix socket + relay container `dg-hooks`.** The daemon serves `POST /hooks/github/{appId}` and `GET /healthz` on `hooks.sock`. A Quadlet container on `web` runs `dermaga hook-relay` (the same static binary, copied into a bare rootfs dir, `Rootfs=…:O`, read-only, `DropCapability=ALL`, 32 MiB, 32 pids) and forwards TCP :8080 to the socket byte for byte. Traefik routes `Host(<hook host>) && PathPrefix(/hooks/)` to `http://dg-hooks:8080`. | No host TCP port, so exposure doesn't depend on nftables. The alternatives failed: `host.containers.internal` only reaches listeners on the public interface **[M0-1a]**, and pasta `-T` binds the netns loopback only and Debian's pasta rejects an address **[M0-1b]**. Cost: 2.4 MB RSS **[M0-1c]**. No third-party image to pin. All validation (§4.4) stays in the daemon, and the relay is a dumb pipe. *Approved 2026-11-07.* |
| D2 | **Health probe = `podman unshare --rootless-netns dermaga probe …`**, one exec per deploy. The probe resolves `dg-<app>-<rel8>` through aardvark-dns at the network gateway and reports each attempt as a JSON line on stdout (streamed into the deploy log). It exits 0 after K consecutive successes, non-zero at the deadline. | Bridge IPs are only reachable inside the rootless netns **[M0-2]**. Re-executing our own binary avoids depending on host curl (which can't use aardvark DNS) and avoids one fork per probe. ~55 ms overhead. |
| D3 | **The daemon owns every deadline; it never blocks on `systemctl start`.** Candidates start with `systemctl --user start --no-block`. Cleanup order: `podman rm -f -t <StopTimeout>` → `systemctl --user stop` → `reset-failed` → remove unit → `daemon-reload`. Units set `StopTimeout=` (container) and `TimeoutStopSec=` (service) explicitly. `Notify=healthy` stays for units with a `HealthCmd`, because it gives correct ordering at boot. | A waiting `Notify=healthy` start ignores SIGTERM: a never-healthy unit took 105 s to fail and leaked its container. Removing the container first is bounded and clean **[M0-4]**. |
| D4 | **Memory limit via `PodmanArgs=--memory=…`**, not `Memory=`. CPU via `PodmanArgs=--cpus=…`. Pids via `PidsLimit=`. | Podman 5.4.2's Quadlet rejects `Memory=` **[M0-4]**. Same as vps-setup.sh. |
| D5 | **Every generated unit set is dry-run validated** (`QUADLET_UNIT_DIRS=<staged copy> quadlet -dryrun -user`) before anything is installed. | A rejected unit silently has no `.service` **[M0-4]**. |
| D6 | **Traefik dynamic files are written as JSON** (`encoding/json` from typed structs) into `app-<name>.yml`. No YAML library. | Traefik accepts JSON in `.yml` **[M0-3]**. Typed serialization is guaranteed, and it removes a dependency. |
| D7 | **Switch verification uses a per-deploy token router on an internal `verify` entrypoint** (`:8081`, plain HTTP, inside the Traefik container, never published). **Only** `app-<name>-verify` (`Host(<primary>) && Header(X-Dermaga-Verify, <random token>)`) is attached to `verify`. It uses the same service as the public router and the public router's router-level middlewares **minus `basicAuth` and `ipAllowList`**. The public `app-<name>` router stays on `websecure` only. After the atomic write, the probe from D2 polls `http://traefik:8081<health path>` with that Host and token until it succeeds (deadline 10 s). **Once verification succeeds, the app file is rewritten atomically without `app-<name>-verify`**, so verify routers exist only while a deploy is in progress (startup reconciliation re-renders any app file that still has one, e.g. after a crash mid-deploy). A best-effort `--resolve <domain>:443:127.0.0.1` check follows; it only warns, and only once a certificate exists. *Approved 2026-11-07, with the public router kept off `verify`; middleware subset and router removal approved 2026-11-07.* | The token router only exists in the new file, so a 200 proves Traefik loaded *this* file and that the candidate answers through the app's service. The brief's `--resolve` check alone fails under `sniStrict: true` until ACME has issued a cert (every new domain, and the VM always) **[M0-3b]**. `verify` is reachable from every container on `web` and skips the entrypoint-level `secure@file` chain, so attaching public routers to it would expose every app without that chain. Behind a random token, the token router adds nothing beyond the direct container access that `web` already allows (see the residual risk in §1). `basicAuth` and `ipAllowList` are left off because the probe has no basic-auth password (only bcrypt hashes are stored) and connects from the rootless netns (`10.89.0.1`), so Traefik would answer 401/403 and verification would always fail for protected apps. That doesn't widen access, for the same reason. Removing the router after verification shrinks the window in which a token is valid to the deploy itself. |
| D8 | **A failed verification counts as "Traefik did not load the config".** The previous app file is restored (snapshot kept in memory) and verified with its own token, then the candidate is removed. The error points at `journalctl --user -u traefik` and `99-custom.yml`. | One invalid file in `dynamic/` freezes **all** updates while the old config keeps serving **[M0-3]**. D7 detects this without dermaga having to parse hand-written YAML. `doctor` also reports Traefik provider errors. |
| D9 | **Switch timing:** typical switch-to-verified time is about 2.1 s (Traefik's default `providersThrottleDuration` of 2 s). It stays at the default. | **[M0-3]**: 2.04–2.12 s, zero non-200 during 10 switches. |
| D10 | **Backups stream with `restic backup --stdin-from-command --stdin-filename <db>.<ext> -- podman exec <ctr> <dump argv>`.** Credentials are read inside the container from its own environment (idea from Litepod's `database_backup/engine.rs`; no secrets in host argv). | Debian 13's restic 0.18.0 has the flag. A failing, mid-stream-failing or killed dump gives exit 1 and **no snapshot** **[M0-6]**. |
| D11 | **Panel tunnel** as specified, with sshd unchanged (`AllowTcpForwarding local`, `AllowStreamLocalForwarding no`). `dermaga panel` prints the `Host` snippet with `SessionType none`, `ExitOnForwardFailure yes`. | Works; `-R` and Unix-socket forwards are refused **[M0-7]**. Because stream-local forwarding is off, the CLI socket can't be tunnelled, which is intended. |
| D12 | **One binary**: daemon, CLI, `hook-relay` and `probe` are subcommands of `dermaga`. | One artifact to checksum, update and pin. Measured 11.2 MB (amd64) / 10.7 MB (arm64), idle RSS 12.4 MB **[M0-5]**. |
| D13 | **The name lives in one place**: `internal/name` holds `Name = "dermaga"` and the derived prefixes (`dg-` units, `app-` Traefik files, socket and directory names). | BRIEF: keep it renameable. |
| D14 | **Dependencies kept to:** `modernc.org/sqlite`, `golang.org/x/crypto` (argon2id, bcrypt for Traefik basic auth), `github.com/pelletier/go-toml/v2` (`config.toml`). TOTP (RFC 6238) is ~40 lines on `crypto/hmac`, tested against the RFC vectors. INI (Quadlet/systemd) is written by a small strict writer: values containing control characters are rejected, and `%`/`$` are escaped. Dev-only: `tygo` (Go → TS types, via a `tool` directive, not linked). | BRIEF §4.8. Each gets a section in `DEPENDENCIES.md` in M1. |
| D15 | **Litepod ideas reused (concepts, no code):** a candidate-cleanup guard that always runs on early return; the route switch as a short, non-cancellable critical section; snapshotting the old route and restoring it on failure. | Read `apps/api/src/deployment/rollout.rs`. If any code is adapted later, it gets an Apache-2.0 attribution header. |

### Changes to BRIEF §2

None. D1 and D7 changed mechanisms in §3.1 and §3.5.7, not §2. Both were approved and are now in
the brief.

### Settled: access-control middlewares on the token router

The open point from M0 (basic auth and IP allowlists would make verification fail for protected
apps) was settled on 2026-11-07. The token router gets the public router's middlewares minus
`basicAuth` and `ipAllowList`, and it is removed once verification succeeds. Both points are recorded in D7.

### Decisions taken in M1

| # | Decision | Why |
|---|---|---|
| D16 | **Migrations are numbered SQL files; `PRAGMA user_version` holds the last applied number.** Each file runs in one transaction together with the version bump. A gap in the numbering, or a database newer than the binary, is an error. | No migrations table, no library. `user_version` is transactional in SQLite, so a failed migration leaves version and schema unchanged (tested). |
| D17 | **One SQLite connection** (`SetMaxOpenConns(1)`), WAL, `foreign_keys=ON`, `busy_timeout=5000`, `synchronous=NORMAL`. The database file is created 0600, and SQLite gives `-wal`/`-shm` the same mode. | The daemon's write rate is tiny. One connection rules out `SQLITE_BUSY` between our own connections. 0600 keeps copies and backups private even outside the 0700 directory. |
| D18 | **argon2id with m=19 MiB, t=2, p=1** (OWASP profile), parameters stored in the PHC string. `debug.FreeOSMemory()` runs after each hash. Logins are serialised by a mutex. | The idle RSS budget is 30 MB. Without the release, RSS stayed at 57 MB after a login; with it, 18.7 MB after 60 s idle (M1 evidence). Serialising keeps login memory to one hash at a time. |
| D19 | **Sessions:** 256-bit random cookie `dermaga_session` (`HttpOnly; SameSite=Strict; Path=/`, `Max-Age` 7 d, no `Secure` because the panel is plain HTTP on localhost). Only its SHA-256 is stored. Idle 12 h and absolute 7 d are enforced server-side. `last_seen_at` is written at most once a minute. Changing the password deletes every session. | BRIEF §4.3. A stolen database doesn't yield usable cookies. |
| D20 | **Login backoff is global**, not per IP: the 5th consecutive failure blocks login for 1 s, the 6th for 2 s, then 4 s, 8 s … up to 15 min. During a block every attempt gets HTTP 429 + `Retry-After` without the password being checked. A success resets the counter. | Every panel request comes from 127.0.0.1 through the tunnel, so per-IP state would mean nothing. One admin means a global counter locks out only the attacker's guesses. |
| D21 | **TOTP**: RFC 6238 SHA-1, 6 digits, 30 s, ±1 step. A code is accepted only once (the last used step is kept in memory). The secret is stored encrypted with AAD `settings.totp_secret`. It is enabled from the CLI (`dermaga admin totp enable` prints the `otpauth://` URI and secret, then requires a valid code before storing it). | Universal authenticator support. Confirming a code before storing prevents a lockout from a mistyped secret. The panel UI for TOTP comes in M7. |
| D22 | **Secrets are bound to their column**: `Box.Seal(value, aad)` uses the column name as GCM additional data, so a ciphertext copied into another column doesn't decrypt. `master.key` is created via temp file + `link(2)`, so an existing key is never overwritten, and it is refused if it is group- or world-readable. | Losing or replacing the key loses every secret. |
| D23 | **Admin routes exist only on the Unix socket** (`PUT /api/admin/password`, `PUT`/`DELETE /api/admin/totp`). The panel serves login, logout, status and audit in M1. Panel responses carry `Cache-Control: no-store`, `X-Frame-Options: DENY`, `nosniff` and `Referrer-Policy: no-referrer`. | The socket's file mode is the authentication (BRIEF §3.1). Changing the password from the panel needs the current password, and comes with the Settings page in M7. |
| D24 | **The user unit has no `NoNewPrivileges=`** (`install/dermaga.service`). | From M3 on the daemon runs rootless Podman, which needs the setuid/file-capability helpers `newuidmap`/`newgidmap`. |
| D25 | **Linters are pinned as `tool` directives in `go.mod`** (`go tool staticcheck`, `go tool govulncheck`). `make check` runs gofmt, vet, staticcheck, govulncheck and tests, so only a Go toolchain is needed. | Reproducible checks; nothing installed globally. |
| D26 | **`config.toml` is optional and strict**: defaults apply when it is missing, unknown keys are rejected. M1 has one setting, `[panel] port`. | A typo must not silently fall back to a default. Settings that the UI changes live in SQLite, not here. |

## 3. Deploy pipeline (blue-green; mechanics proven in M0)

```
queued → building → starting → healthy → live
   └──────────┴─────────┴─────────┴──► failed | rolled_back | superseded
```

1. **Queue**: one global worker (concurrency 1). Per app, a newer trigger replaces a queued, not yet running, one (`superseded`). Per-app mutex for any mutating action.
2. **Fetch** into the bare mirror with the deploy key (`GIT_SSH_COMMAND`, argv only, `StrictHostKeyChecking=yes`, dermaga's own `known_hosts`). `git archive <sha>` into a temporary build dir.
3. **Build**: `podman build --memory 2g --cpus 2 -f <containerfile> -t localhost/dermaga/<app>:<sha12> --label org.opencontainers.image.{source,revision}=…`, with a timeout (20 min). Output goes to a log file and SSE subscribers, with env values masked.
4. **Candidate unit** `dg-<app>-<rel8>.container`: `Network=web.network` (+ `dg-db-*`), `EnvironmentFile=`, `NoNewPrivileges=true`, `DropCapability=ALL` (+ per-app `AddCapability=`), `PidsLimit=`, `PodmanArgs=--memory=… --cpus=…`, optional `ReadOnly=true`, `StopTimeout=`, `[Service] TimeoutStopSec=`, `Restart=always`, `[Install] WantedBy=default.target`. Staged → dry-run (D5) → atomic install → `daemon-reload` → `start --no-block` (D3).
5. **Health** (priority order, K=3 successes within 120 s):
   a. HTTP health path → `dermaga probe` inside the rootless netns (D2);
   b. image or unit `HealthCmd` → poll `podman inspect` health status (`Notify=healthy` also makes the unit go `active` exactly then);
   c. neither → "running ≥ N s with zero restarts" (`podman inspect .RestartCount`), flagged as weak in the UI.
6. **Switch**: snapshot `app-<name>.yml`, write the new one atomically (temp file in the same dir, `fsync`, `rename`, `fsync` dir) with a fresh verify token, then verify (D7). On failure, restore and verify the old file (D8). After a successful verification (of the new file, or of the restored one), rewrite the file without `app-<name>-verify` (D7).
7. **Drain**: wait the grace period (10 s), stop the old unit, remove its file, `daemon-reload`. Keep the images of the last 5 releases and prune older `localhost/dermaga/<app>` images.
8. **Any failure before the switch**: remove the candidate (D3). The live release and route are never touched. First deploy: no route file is written until the candidate is healthy, so a failed first deploy leaves nothing.
9. **`recreate`** (default when the app has named volumes): stop old → start new → health → switch. On failure, restart the previous release from its kept image and restore its route.
10. **Rollback** = steps 4–8 with an existing image (no fetch or build).
11. **Startup reconciliation**: compare the DB (live release per app) with the `dg-*` units on disk and running, and with the `app-*.yml` files. Repair what is deterministic (re-render a missing file, start a stopped live unit, remove orphaned `dg-*` candidates), report the rest. Never touch names without the `dg-`/`app-` prefix.

## 4. Files dermaga owns

As in BRIEF §3.3, plus:

```
$XDG_RUNTIME_DIR/dermaga/dermaga.sock      CLI API (0600)
$XDG_RUNTIME_DIR/dermaga/hooks.sock        hook listener (0600), bind-mounted into dg-hooks
~/.local/share/dermaga/relay/dermaga       copy of the binary = rootfs of dg-hooks (refreshed by self-update)
~/.config/containers/systemd/dg-hooks.container
~/.config/traefik/dynamic/00-global.yml    replaces vps-setup's 00-vps.yml (same middlewares, D6 format)
~/.config/traefik/dynamic/app-<name>.yml   one per app (JSON content)
~/.config/traefik/dynamic/99-custom.yml    yours; never written by dermaga
~/.config/systemd/user/dg-backup-*.{service,timer}
```

Traefik's Quadlet unit (written by the installer) is vps-setup's unit **minus** the Podman socket
mount and `Requires=podman.socket`; the static config loses the `docker` provider and gains
`entryPoints.verify` (`:8081`) and, in M9, a JSON access log in a bind-mounted state dir.
`podman.socket` is disabled.

## 5. Data model (SQLite, WAL, `foreign_keys=ON`, migrations in `internal/store/migrations/NNN_*.sql`)

| Table | Key columns |
|---|---|
| `settings` | single row: admin password hash (argon2id), TOTP secret (enc), hook host, default limits, header/rate-limit defaults (JSON), S3 + restic settings (secrets enc), notify target (enc) |
| `sessions` | id_hash (SHA-256 of the cookie), created_at, last_seen_at; expiry is computed (12 h idle, 7 d absolute); deleted on logout |
| `apps` | id (random, used in the webhook URL), name UNIQUE, source (`git`/`image`), repo, branch, context, containerfile, image_ref, port, health (path, cmd, K, timeout), limits, strategy, auto_deploy, trigger (`push`/`workflow_run` + workflow name), watch paths (JSON), middleware (JSON of typed struct), deploy key fingerprint, webhook secret (enc) |
| `domains` | app_id, host UNIQUE, is_primary, redirect_to_primary |
| `env_vars` | app_id, key, value (enc); UNIQUE(app_id, key) |
| `releases` | id, app_id, rel8, trigger, ref, sha, message, image_id, status, error, log_path, timestamps |
| `databases` | id, name UNIQUE, engine, version, volume, credentials (enc), backup policy (schedule, retention) |
| `db_links` | database_id, app_id, env var names (JSON) |
| `backups` | target (`db:<name>`/`system`), snapshot_id, bytes, status, duration, error |
| `webhook_deliveries` | delivery_id UNIQUE, app_id, received, result |
| `metrics` | ts, container, cpu, mem (7 d) · `requests_minute` (app, minute, count, 4xx, 5xx, p50, p95; 14 d) |
| `audit_log` | ts, actor (`panel`/`cli`/`webhook`/`timer`), action, target, details (no secrets) |

Encrypted columns: AES-256-GCM, a random 96-bit nonce per value, stored as `v1:<nonce|ciphertext>`.
The key is `~/.config/dermaga/master.key` (32 random bytes, 0600). Env files must be plaintext
(0600) for Podman. Encryption protects the DB file and its backups, nothing more.

## 6. Pinned versions (checked 2026-11-07)

| Component | Version | Source |
|---|---|---|
| Go | 1.27.3 | go.dev |
| modernc.org/sqlite | v1.61.1 (SQLite 3.54.1) | proxy.golang.org |
| golang.org/x/crypto | v0.58.0 | |
| github.com/pelletier/go-toml/v2 | v2.4.3 | |
| tygo (dev tool, added in M7) | v0.2.21 | |
| staticcheck (`go tool`) | honnef.co/go/tools v0.8.2 (2026.2.2) | proxy.golang.org |
| govulncheck (`go tool`) | golang.org/x/vuln v1.9.0 | proxy.golang.org |
| Bun | 1.4.4 (spike used 1.4.2) | |
| SvelteKit / Svelte / adapter-static | 3.1.1 / 5.57.3 / 4.0.1 | npm |
| Tailwind CSS / Vite / vite-plugin-svelte | 4.3.5 / 8.3.6 / 7.3.1 | npm |
| Traefik | `docker.io/library/traefik:v3.7` (now v3.7.17), `AutoUpdate=registry` | as in vps-setup.sh |
| Podman / passt / restic / git / OpenSSH | 5.4.2 / 2025-05-03 / 0.18.0 / 2.47.3 / 10.0p1 | Debian 13 archive |
| Postgres / MariaDB / Valkey | candidates `postgres:18`, `mariadb:11.8` (LTS), `valkey/valkey:9`; re-check in M5 | Docker Hub |

## 7. Not verified in M0 (owned by later milestones)

- S3 backends (R2/B2): a local restic repository is used until M6, when you provide a test bucket.
- Real ACME issuance and inbound webhooks: the VM is behind libvirt NAT. In the VM: Let's Encrypt
  staging or self-signed certificates, replayed signed deliveries in automated tests, and
  `gh webhook forward` (cli/gh-webhook; to be verified in M3) for real GitHub deliveries. The
  public push-to-live and certificate check happens during the migration in M10.
- aarch64: the binary cross-compiles, but nothing has run on arm64 hardware.
- Load behaviour of the switch at 50 req/s (M4 acceptance, with `oha`).
- `podman unshare --rootless-netns` when no container is running: not tested, because during a
  probe the candidate (and Traefik) always keep the netns alive.
