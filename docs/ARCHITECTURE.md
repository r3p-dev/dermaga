# dermaga architecture

Status: **after M0, D1 and D7 approved** (2026-11-07). [`BRIEF.md`](BRIEF.md) is the source of truth for goals and
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
| D7 | **Switch verification uses a per-deploy token router on an internal `verify` entrypoint** (`:8081`, plain HTTP, inside the Traefik container, never published). **Only** `app-<name>-verify` (`Host(<primary>) && Header(X-Dermaga-Verify, <random token>)`) is attached to `verify`. It uses the same service and the same router-level middlewares as the public router (but see the open point below). The public `app-<name>` router stays on `websecure` only. After the atomic write, the probe from D2 polls `http://traefik:8081<health path>` with that Host and token until it succeeds (deadline 10 s). A best-effort `--resolve <domain>:443:127.0.0.1` check follows; it only warns, and only once a certificate exists. *Approved 2026-11-07, with the public router kept off `verify`.* | The token router only exists in the new file, so a 200 proves Traefik loaded *this* file and that the candidate answers through the app's service. The brief's `--resolve` check alone fails under `sniStrict: true` until ACME has issued a cert (every new domain, and the VM always) **[M0-3b]**. `verify` is reachable from every container on `web` and skips the entrypoint-level `secure@file` chain, so attaching public routers to it would expose every app without that chain. Behind a random token, the token router adds nothing beyond the direct container access that `web` already allows (see the residual risk in §1). |
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

### Open point for M2/M4: access-control middlewares on the token router

"Same router-level middlewares" works for headers, compression, buffering, path-prefix stripping
and rate limiting. It doesn't work for `basicAuth` and `ipAllowList`: the probe has no basic-auth
password (dermaga stores only bcrypt hashes) and connects from the rootless netns
(`10.89.0.1`), so Traefik answers 401/403 before reaching the service. Verification would then
always fail for protected apps. Proposal: the token router gets the public router's middlewares
**minus** `basicAuth` and `ipAllowList`. This doesn't widen access, for the reason given in D7:
the token router is reachable only from `web` and only with the per-deploy token, while the app
container is directly reachable from `web` anyway. To be confirmed before M2 writes the
generator and its golden tests.

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
6. **Switch**: snapshot `app-<name>.yml`, write the new one atomically (temp file in the same dir, `fsync`, `rename`, `fsync` dir) with a fresh verify token, then verify (D7). On failure, restore and verify the old file (D8).
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
| `sessions` | id (hash of cookie), created, last_seen, expires; deleted on logout |
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
| tygo (dev tool) | v0.2.21 | |
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
