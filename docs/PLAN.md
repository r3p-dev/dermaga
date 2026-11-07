# dermaga plan

Milestones follow BRIEF §8. Each one ends with `make check` passing (gofmt check, `go vet`,
`go tool staticcheck`, `go tool govulncheck`, `go test`; the linters are pinned as `tool`
directives in `go.mod`, so only a Go toolchain is needed), plus the milestone's VM checks run on `dermaga-vm` and recorded as
evidence (commands + output) in the milestone report. Hours are my working estimate, including VM
testing and docs. They don't include your review time.

**Integration harness** (grows from M3 on): `test/vm/*.sh` scenarios that run over
`ssh dermaga-vm`, each idempotent and leaving no residue. Two libvirt snapshots already exist:

| Snapshot | State | Used for |
|---|---|---|
| `fresh` | bare Debian 13 cloud image, only cloud-init's user `debian` | installer runs (M8), disaster-recovery drill (M6), §9 "fresh → one command" |
| `vps-setup` | after `vps-setup.sh init` (the VM's current state) | M1–M7, M9 development; migration rehearsal (M10) |

My environment can't reach `qemu:///system`. When a scenario needs a revert, I stop and give you
the exact command, for example
`virsh --connect qemu:///system snapshot-revert dermaga-vm fresh`, and continue after you
confirm. `docs/OPERATIONS.md` documents the harness in M8.

**Commits**: I never commit (BRIEF §11). Every milestone report ends with "Suggested commits".

| # | Milestone | Hours |
|---|---|---|
| M0 | Spikes and plan (done) | – |
| M1 | Daemon core (done) | 16 |
| M2 | Generators + reconciliation | 12 |
| M3 | Git apps, recreate strategy, portfolio live | 20 |
| M4 | Zero-downtime | 14 |
| M5 | Databases | 10 |
| M6 | Backups and restore | 16 |
| M7 | UI | 26 |
| M8 | Installer and releases | 14 |
| M9 | Observability and notifications | 10 |
| M10 | Hardening review and migration | 12 |
| | **Total** | **150** |

## M1: Daemon core (16 h)

**Done 2026-11-07.** Evidence: [`M1-EVIDENCE.md`](M1-EVIDENCE.md). The spike links in
`M0-EVIDENCE.md` now point to commit `eb9ad64` on GitHub.

Module layout from BRIEF §7. `internal/name`, `config` (TOML, XDG paths), `store` (SQLite, WAL,
embedded versioned migrations), `secrets` (AES-256-GCM, `master.key`), `api` (JSON handlers shared
by both listeners), `auth`, `audit`, `cli` (stdlib `flag` + subcommand router, `--json`).

Acceptance:
- `dermaga daemon` refuses uid 0 (test). It runs as a systemd user service on the VM and creates
  `dermaga.sock` with mode 0600.
- Migrations: fresh DB → latest; re-run is a no-op; tested with a temp DB.
- Secrets round-trip; tampered ciphertext is rejected; a wrong key is rejected (tests).
- Panel on `127.0.0.1:7070`: login with argon2id; TOTP (RFC 6238 test vectors); login backoff;
  session cookie `HttpOnly; SameSite=Strict; Path=/`, 12 h idle / 7 d absolute, logout deletes it
  server-side. Tests: `Host` other than `127.0.0.1:<port>`/`localhost:<port>` → 421; mutating
  request with a foreign or missing `Origin` → 403.
- Every listener has `ReadHeaderTimeout`, `ReadTimeout`, `WriteTimeout` (SSE excepted, per
  route), `IdleTimeout` (test checks the server config).
- `dermaga admin set-password`, `dermaga panel` (prints the `~/.ssh/config` snippet + URL),
  `dermaga doctor` (lingering, subuids, sockets, clock sync, disk) work on the VM. Through the SSH
  tunnel from the laptop: login works and the audit log shows the login.
- `docs/DEPENDENCIES.md` lists modernc/sqlite, x/crypto, go-toml with reasons.
- `go vet ./...`, `staticcheck ./...` and `go test ./...` from the repo root don't include
  `spikes/m0/`: it is a separate module (`dermaga.spike/m0`, own `go.mod`), and `./...` stops at
  nested modules. Shown with a stand-in root module during the M0 follow-up: `go list ./...`
  printed only `…/cmd/dermaga`.
- **Spike retirement**: the relay (`spikes/m0/relay`) and probe (`spikes/m0/probe`) logic is
  ported into `internal/` as `dermaga hook-relay` and `dermaga probe`, with tests. Then
  `spikes/m0/` is deleted, and its `.gitignore` entry with it. Before deleting, I ask you for the
  M0 commit SHA and the repo URL and repoint the `spikes/m0/` links in `docs/M0-EVIDENCE.md` to
  that commit, so the evidence stays resolvable.

## M2: Generators + reconciliation (12 h)

Typed structs → Quadlet INI (strict writer), Traefik JSON (`00-global.yml`, `app-<name>.yml` with
the full §3.6 middleware model and the D7 verify router), systemd timer/service units. Startup
reconciliation (ARCHITECTURE §3.11).

Acceptance:
- Golden tests for every generator (`go test ./... -update` refreshes). Cases: minimal app, every
  middleware toggle, aliases + www redirect, path prefix + strip, basic auth, allowlist, body
  limit, custom/removed headers, DB-linked app, `recreate` app with a volume.
- D7 verify router golden cases: the deploy-time file has `app-<name>-verify` on `verify` only,
  with the public router's middlewares **minus `basicAuth` and `ipAllowList`** (rendered for an
  app with basic auth, one with an allowlist, and one with both plus headers/compression/rate
  limit, to show that the others stay); the steady-state file of the same apps has **no**
  `app-<name>-verify` router.
- Injection tests: newline, `%`, `$`, backtick and quote in every user-controlled field are
  rejected or escaped. A domain value cannot break the router rule.
- `00-global.yml` reproduces vps-setup's `00-vps.yml` (same middlewares, TLS options and
  per-app `Permissions-Policy` default). The header test compares `curl -sI` against an app with
  vps-setup's chain in M3.
- Generated units pass `quadlet -dryrun` (in CI where Podman exists, and always on the VM).
- Reconciliation tests with a fake host: orphan candidate removed, missing app file re-rendered,
  stopped live unit started, foreign files untouched, an app file left with an
  `app-<name>-verify` router (crash mid-deploy) re-rendered without it.

## M3: Git apps, `recreate`, portfolio live (20 h)

Deploy keys (ed25519, 0600), pinned GitHub host keys (fetched from GitHub's meta API, fingerprints
shown), bare mirrors, `git archive` export, `podman build` with limits, timeout and masked log
streaming (SSE), env vars, domains, `image` apps, webhook endpoint with every §4.4 check, D1 hook
relay, queue (concurrency 1) with supersede, `recreate` strategy, first-deploy semantics.

Acceptance:
- Webhook tests: bad or missing signature → 401 before JSON parsing; > 1 MiB → 413; wrong branch,
  repo or event → 202 ignored and logged; duplicate `X-GitHub-Delivery` → deduped; unknown app id
  → 404 with timing indistinguishable from a known id with a bad signature (test compares code
  paths: the HMAC is always computed); per-app rate limit; watch paths; `workflow_run` gate.
- On the VM: `r3p-dev/portfolio` created via the CLI and the deploy key added to the repo by you.
  Automated tests replay signed deliveries. For a real GitHub delivery into the NAT'd VM: first
  verify that `gh webhook forward` (cli/gh-webhook extension) still works, then a real
  `git push` → GitHub → `gh webhook forward` → hook endpoint → build → live, recorded as
  evidence. Where `gh` runs and how it reaches the hook endpoint is decided then: the hook socket
  can't be tunnelled (`AllowStreamLocalForwarding no`), so `gh` runs on the VM or targets
  Traefik's :443 on the hook host. Certificates in the VM are LE staging or self-signed. The
  public push-to-live and certificate check is part of M10.
- `curl -sI` header comparison with vps-setup.sh's Traefik output (§9), compression on.
- A broken commit (build fails, and separately the container crashes at start) leaves the
  previous release serving (`recreate` restarts the old release).
- `image` app: `docker.io/louislam/uptime-kuma:<pinned>` deployed and routed.

## M4: Zero-downtime (14 h)

Blue-green pipeline (ARCHITECTURE §3), D2 probe, D7 verify, drain, rollback, release retention,
image pruning, `superseded`. Switch steps, in order:

1. Snapshot `app-<name>.yml`, then write the new file with a fresh token router
   `app-<name>-verify` (on `verify` only, public middlewares minus `basicAuth`/`ipAllowList`).
2. Poll through `verify` with the token until it succeeds (10 s).
3. On success, rewrite the file without `app-<name>-verify`, then drain the old release.
4. On failure (D8), restore the snapshot with its own fresh token router, verify it, rewrite it
   without the token router, remove the candidate.

Acceptance (BRIEF): `oha` at 50 req/s against portfolio on the VM during
(a) a successful redeploy and (b) a deploy whose health check fails:
**0 non-2xx, 0 connection errors**, raw `oha` output recorded. Also: rollback to the previous
release in < 10 s (no rebuild); a trigger during a running build supersedes the queued one;
daemon killed mid-deploy → on restart reconciliation leaves exactly one live release and route
and no `app-<name>-verify` router; after every deploy (successful, and failed-verify-then-restored)
`app-<name>.yml` contains no verify router; a deploy of an app with basic auth and an IP allowlist
verifies successfully;
deployment state machine fully covered by tests with a fake host.

## M5: Databases (10 h)

Postgres, MariaDB, Valkey (pinned majors, re-checked first). `.container` + `.volume` +
`dg-db-<name>.network`, never on `web`. Credentials ≥ 32 bytes, encrypted. Link/unlink → network
attach + env injection (editable names) + app redeploy. Minimum capabilities per engine found by
testing on the VM. `dermaga db shell`, SSH-tunnel connection instructions.

Acceptance: each engine created, linked to a test app that reads and writes through it, restarted
in place, data survives; container shown unreachable from `web` and from the internet; capability
set per engine documented with the test that proves it.

## M6: Backups and restore (16 h)

Development and tests use a local restic repository on the VM until you provide the R2/B2 test
bucket at the start of M6. The acceptance runs below use the bucket.

restic settings (password generated, shown once), per-DB timers (03:00 Asia/Jakarta,
`RandomizedDelaySec`), D10 streaming dumps (Valkey via `BGSAVE` + RDB), system backup
(`VACUUM INTO`, config, env files, deploy keys, Traefik config; no `master.key`), `keys export`,
optional volume export, `forget --prune`, weekly `check` + `--read-data-subset`, restore (safety
dump first, typed confirmation), restore into a new DB, test restore, `restore-system`.

Acceptance: scheduled run observed via `systemctl --user list-timers` and in history; test
restore passes for all three engines; a failing dump fails the backup (no snapshot), and the
notification fires. **Disaster recovery drill**: VM reverted to a fresh snapshot → install →
`dermaga restore-system` from the bucket → apps redeployed from their last good commits and DBs
restored, verified by content.

## M7: UI (26 h)

SvelteKit 3 SPA (adapter-static, runes, Tailwind 4), embedded. All §3.10 pages, SSE logs/builds,
dark + light themes, keyboard-friendly. TS types generated from Go API structs (`tygo`, checked
in CI: generated file must be up to date). TypeScript strict + Prettier.

Acceptance: every CLI capability reachable in the UI (checklist in the report); new-app wizard
works end to end against portfolio on the VM; binary size and idle RSS re-measured (< 30 MB);
`bun run check` and the `tygo` freshness check pass.

## M8: Installer and releases (14 h)

`install/install.sh` (hardening ported from vps-setup.sh's listed functions with all safety
checks, `config-example`, `init --config`), Traefik file-provider-only, `podman.socket` disabled,
restic/git/curl, binary download + SHA-256 verify, user service + linger, `doctor`, next steps.
GitHub release workflow (both arches, checksums), `self-update` (checksum verify; also refreshes
the D1 relay rootfs). `docs/OPERATIONS.md`.

Acceptance: fresh Debian 13 snapshot → one command → `doctor` all green; re-run is a no-op;
port scan from the laptop shows only SSH, 80 and 443 (tcp) and 443 (udp); `self-update` refuses
a tampered checksum.

## M9: Observability and notifications (10 h)

`podman stats` sampling every 30 s (7 d), Traefik JSON access log → per-minute aggregates (14 d,
self-rotated), server overview (available RAM, swap, disk, uptime, pending security updates,
reboot-required), charts, notifications (ntfy / Telegram / Discord).

Acceptance: numbers cross-checked against `podman stats` and an `oha` run with known counts;
each notification type delivered once to a real target; daemon idle RSS still < 30 MB and
Traefik + daemon < 100 MB.

## M10: Hardening review and migration (12 h)

`docs/THREAT-MODEL.md`, self-review against every BRIEF §4 item (findings fixed),
`docs/MIGRATING-FROM-VPS-SETUP.md` with rollback, then portfolio and Kapturae migrated in the VM
by following the guide literally.

Acceptance: §9 global criteria all demonstrated with evidence; migration downtime measured.
The real public push-to-live and certificate check happens here, during the actual migration.
`THREAT-MODEL.md` includes the shared-`web` residual risk (ARCHITECTURE §1).

## Decisions on the M0 questions (2026-11-07)

1. D1 (Unix socket + `dg-hooks` relay): approved.
2. D7: approved; only the token router is attached to `verify`. The open point was settled
   later the same day: the token router carries the public router's middlewares minus
   `basicAuth` and `ipAllowList`, and is removed from the app file once verification succeeds.
3. S3: local restic repository until M6; you provide an R2/B2 test bucket when M6 starts.
4. Public reachability: replayed signed deliveries plus `gh webhook forward` in the VM;
   staging/self-signed certificates; the real check happens in M10.
5. Kapturae repo URL: you give it at M10.
6. Commits: I never commit. `spikes/m0/` goes into the first commit as M0 evidence and is deleted
   at the end of M1 (see M1).
7. Snapshots `fresh` and `vps-setup` exist (harness section above).
