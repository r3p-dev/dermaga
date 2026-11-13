# Phase 0 audit — what v1 really contains

Status: **audit only, no code changed.** Written 2026-11-10 against `antigravity-m3` @ `3571b12`
(working tree has uncommitted doc changes, see §0).

"works" in the tables means: the code exists and is wired end to end (UI/CLI → API → host). It is
**not** runtime proof; runtime proof is Step 3 (`docs/v2/P0-EVIDENCE.md`). Nothing in
`docs/M3-EVIDENCE.md` … `docs/M10-EVIDENCE.md` or the "done" marks of `docs/PLAN.md` was used as
proof. Line numbers refer to the working tree at the time of writing.

## 0. Source documents and an inconsistency to resolve first

- `docs/v2/` did not exist. In the working tree, `docs/BRIEF.md`, `PLAN.md`, `ARCHITECTURE.md`,
  `THREAT-MODEL.md`, `DEPENDENCIES.md` and `OPERATIONS.md` contain the **v2** texts (uncommitted
  changes; `git diff --stat -- docs`: 6 files, +845/−1402). The v1 texts exist only in `HEAD`.
- For this audit: **v1** = `git show HEAD:docs/BRIEF.md` (and the other five files from `HEAD`);
  **v2** = the working-tree copies. Every "v1 BRIEF §x" below refers to the `HEAD` version.
- This needs your decision (gap **G28**): the rules and the v2 texts themselves expect
  `docs/BRIEF.md` = v1 and `docs/v2/*.md` = v2.
- **Resolved 2026-11-12 (decision D-a).** The v1 texts were restored into `docs/*.md` from `HEAD`
  (`git show HEAD:docs/<file>`), and the v2 texts were copied to `docs/v2/*.md` (plain copies).
  From now on **"v1 BRIEF" = `docs/BRIEF.md`** and **"v2 BRIEF" = `docs/v2/BRIEF.md`**; the same
  holds for PLAN, ARCHITECTURE, THREAT-MODEL, DEPENDENCIES and OPERATIONS. Every "v1 …" reference
  in this audit therefore points at `docs/<file>`, and every "v2 …" reference at `docs/v2/<file>`.
  The text of the two sets is unchanged by the move, so section numbers still match. The other
  decisions (D-b … D-e) and the final fix order are recorded in `docs/v2/PLAN.md` §0.1.

## 1. Session header and checks run for this audit

```
$ date -Is; hostname; git -C ~/Projects/dermaga rev-parse --short HEAD; timeout 30 ssh -o BatchMode=yes -o ConnectTimeout=15 dermaga-vm "date -Is; hostname; dermaga version || true; command -v restic git oha podman || true; systemctl --user is-active dermaga traefik 2>&1 | head -3"; echo ssh-exit=$?
2026-11-10T11:52:50+07:00
fedora
3571b12
2026-11-10T04:52:50+00:00
dermaga-vm
dermaga a8cf2fd-dirty
/usr/bin/restic
/usr/bin/git
/usr/local/bin/oha
/usr/bin/podman
active
active
ssh-exit=0
```

(The VM was shut off at session start; I started it with `virsh --connect qemu:///system start
dermaga-vm`, which v1 BRIEF §10 allows without asking. No snapshot was touched. `oha` is already
installed on the VM, so it is the load tool for Step 3 unless you say otherwise.)

The VM runs an older, dirty build (`a8cf2fd-dirty`), not `HEAD`.

```
$ virsh --connect qemu:///system snapshot-list dermaga-vm; virsh --connect qemu:///system snapshot-current --name dermaga-vm
 Name        Creation Time               State
--------------------------------------------------
 fresh       2026-11-07 08:27:27 +0700   running
 vps-setup   2026-11-07 08:41:23 +0700   running

vps-setup
```

`make check` (local, this session): exit 0. `go test ./...` all `ok`. govulncheck: "Your code is
affected by 0 vulnerabilities"; it lists GO-2026-5932 (`golang.org/x/crypto/openpgp`, not imported;
fixed only in a pseudo-version). `cd ui && bun run check`: "svelte-check found 0 errors and 0
warnings" (plus a SvelteKit `config.alias` deprecation notice). Local tool versions: `go1.27.3`,
`bun 1.4.2`.

Observations on the VM used below (raw, this session; the command ran with `set -x`, and I removed
the `+ command` trace lines and the output of commands not relevant here; marked where removed):

```
$ timeout 60 ssh -o BatchMode=yes dermaga-vm "set -x; dermaga apps list; dermaga db list; dermaga doctor; ls ~/.config/traefik/dynamic; dermaga backup settings; systemctl --user list-timers --no-pager | head -20; sysctl net.ipv4.ip_unprivileged_port_start; ps -o pid,rss,comm -C dermaga,traefik; cat /etc/dermaga/host.conf 2>&1 | head -3; ls /etc/sysctl.d"
[… set -x trace lines and the output of ls dynamic, list-timers, sysctl (not in PATH), host.conf (permission denied), ls sysctl.d removed …]
NAME       SOURCE                                         DOMAIN  STRATEGY    LIVE
kapturae   r3p-dev/kapturae@main                                  blue-green  143abc28 217d925805be 2026-11-09 03:24:51
kuma       docker.io/louislam/uptime-kuma:2.5.9-rootless          recreate    dde3709e  2026-11-07 04:35:10
portfolio  r3p-dev/portfolio@dermaga-vm-test                      blue-green  4ecb8170 ca9ef68590b3 2026-11-09 07:34:12
NAME  ENGINE  VERSION  STATUS  VOLUME  LINKED
OK    lingering       enabled for server
OK    subuid          65536 ids
OK    subgid          65536 ids
OK    daemon          a8cf2fd-dirty, pid 788, RSS 24.9 MB, socket /run/user/1001/dermaga/dermaga.sock (0600)
OK    admin password  set
WARN  totp            off: dermaga admin totp enable (strongly recommended)
OK    sockets         traefik-http.socket, traefik-https.socket active
OK    network-online  network-online.target reached
OK    clock           NTP-synchronised
OK    disk            46% used, 10.6 GiB free (/home/server/.local/share/dermaga)
Repository: /home/server/.local/share/dermaga/restic
Restic Pass: •••••••• (pass --reveal-password to show)
    PID   RSS COMMAND
    788 25520 dermaga
   1022 13900 dermaga
   1058 97720 traefik
   1521 15340 dermaga
```

(PID 1022 is the `dg-hooks` relay, 1521 a transient `backup run system`.) No database exists, the
restic repository is **local**, not a bucket, and `doctor` has no Traefik/DNS/restic rows.

```
$ timeout 30 ssh -o BatchMode=yes dermaga-vm "grep -rn unprivileged /etc/sysctl.d/; cat ~/.config/systemd/user/dg-backup-system.service"
[… 5 grep lines removed; the two relevant ones are repeated in the sysctl block below …]
# Generated by dermaga. Don't edit: change the app with dermaga instead.
[Unit]
Description=dermaga backup system
OnFailure=dg-backup-failed@%%n.service

[Service]
Type=oneshot
ExecStart=/usr/local/bin/dermaga backup run system
$ timeout 60 ssh -o BatchMode=yes dermaga-vm "systemctl --user cat dg-backup-system.timer; systemctl --user status dg-backup-system.timer dg-backup-system.service --no-pager 2>&1 | head -30; journalctl --user -u dg-backup-system.service --no-pager -n 15; /usr/sbin/sysctl net.ipv4.ip_unprivileged_port_start; dermaga apps show portfolio | head -6"
[… removed: the timer unit; the status output (including its own 3-line journal tail); from journalctl: 8 identical "Failed to add dependency" lines dated Nov 09, the "-- Boot … --" separator, and 3 of the 4 identical lines of the current boot …]
Nov 10 04:48:14 dermaga-vm systemd[711]: /home/server/.config/systemd/user/dg-backup-system.service:4: Failed to add dependency on dg-backup-failed@%n.service, ignoring: Invalid argument
Nov 10 05:01:59 dermaga-vm systemd[711]: Starting dg-backup-system.service - dermaga backup system...
Nov 10 05:02:02 dermaga-vm dermaga[1521]: Backup completed for system (snapshot 053f3ceb, 530.0 KiB)
Nov 10 05:02:02 dermaga-vm systemd[711]: Finished dg-backup-system.service - dermaga backup system.
net.ipv4.ip_unprivileged_port_start = 80
[… apps show output removed …]
```

From the grep in the second command (two of its seven lines):

```
/etc/sysctl.d/90-vps.conf:38:net.ipv4.ip_unprivileged_port_start = 80
/etc/sysctl.d/90-dermaga.conf:43:net.ipv4.ip_unprivileged_port_start = 80
```

## 2. Requirements of v1 BRIEF §3–§9

Status: **works** = code exists and is wired end to end · **partial** = part of it exists, or it is
wired with a defect · **missing** = not found · **unclear** = cannot be decided from the code alone.

### 2.1 §3.1–§3.3 Architecture, host access, files

| Requirement | Status | Where | Note |
| --- | --- | --- | --- |
| §3.1 daemon is a systemd user service, refuses root | works | `internal/daemon/daemon.go:46-50`; `install/install.sh:865-884` | |
| §3.1 panel binds `127.0.0.1:<port>` only | works | `internal/daemon/daemon.go:102` | |
| §3.1 `dermaga panel` prints `Host` snippet (`LocalForward`, `SessionType none`, `ExitOnForwardFailure`), phone note | works | `internal/cli/panel.go:15-90` | reads SSH port from sshd config |
| §3.1 hook listener `hooks/hooks.sock` (0600, dir 0700), only `POST /hooks/github/{appId}` + `GET /healthz`, relay `dg-hooks` | works | `internal/daemon/daemon.go:97,313-360`; `internal/webhook/webhook.go:67-68`; `internal/quadlet/units.go:256-283` | |
| §3.1 Traefik routes only `Host(<hook>) && PathPrefix(/hooks/)` to the relay | works | `internal/traefik/dynamic.go:384-393` | |
| §3.1 CLI socket 0600 | works | `internal/daemon/daemon.go:213-230` | |
| §3.1 no container gets the Podman socket; `podman.socket` disabled | works | `install/install.sh:538-539` | |
| §3.2 host CLIs via `exec.CommandContext`, argument slices, timeouts, no `sh -c` | partial | `internal/host/host.go`; exceptions `internal/backup/database.go:67,75,143,171,182-183,308-314` | `sh -c` scripts **inside** DB containers, some built with `fmt.Sprintf` from the validated DB name → **G38** |
| §3.2 name validation (`valid_app_name` + reserved) | works | `internal/spec/spec.go:181-205` | |
| §3.2 atomic writes (temp, fsync, rename, dir fsync) | works | `internal/host/host.go:26-56` | |
| §3.2 serializers only | works | `internal/quadlet/ini.go`; `internal/traefik/dynamic.go` (JSON, D6) | |
| §3.2 `quadlet -dryrun -user` on a staged copy | works | `internal/quadlet/dryrun.go:17-71` | |
| §3.3 XDG layout | works | `internal/config/config.go:27-81` | |

### 2.2 §3.4 Data model

| Requirement | Status | Where | Note |
| --- | --- | --- | --- |
| settings: password hash, TOTP, hook host, S3/restic, notification target | works | `internal/store/migrations/001_init.sql:3-9`, `003_git_apps.sql:28`, `005_backups.sql:2-8`, `006_observability.sql` | |
| settings: **global defaults** (limits, rate limit, security headers) | missing | Go constants only: `internal/spec/spec.go:84,143-154` | → **G23** |
| apps, domains, env (encrypted), releases state machine, webhook_deliveries, metrics, audit | works | `002_apps.sql`, `003_git_apps.sql:22`, `internal/apps/apps.go:235,258,405-408`, `006_observability.sql` | |
| databases: engine, version, volume, encrypted creds, links, backup policy | works | `004_databases.sql`; `internal/databases/databases.go:81-82,178-186` | policy not editable → **G37** |
| backups: snapshot, size, **status, duration, error** | partial | `005_backups.sql:10-18`; rows only on success `internal/backup/database.go:47`, `system.go:143` | → **G18** |
| AES-256-GCM, `master.key` 0600 | works | `internal/secrets/secrets.go:22-60` | |

### 2.3 §3.5 Deploy pipeline

| Requirement | Status | Where | Note |
| --- | --- | --- | --- |
| triggers: webhook, UI, CLI `deploy [--ref]`, rollback | works | `internal/api/apps.go:260-300`; `internal/cli/apps.go:370-428` | UI has no ref input (§5) |
| one queue, concurrency 1, supersede queued | works | `internal/deploy/deploy.go:119-208` | |
| fetch: mirror over SSH, deploy key, `IdentitiesOnly`, `StrictHostKeyChecking=yes`, own known_hosts; export | works | `internal/gitsrc/gitsrc.go:121,137-180` | |
| build: `<app>:<sha12>`, OCI labels, 2 GiB/2 CPU/20 min, streamed log, env masked | works | `internal/deploy/system.go:39,69-110`; `internal/deploy/deploy.go:589-652` | |
| candidate unit: networks, EnvironmentFile, limits, NoNewPrivileges, DropCapability=ALL, ReadOnly, Notify=healthy | works | `internal/quadlet/units.go:51-124` | |
| health: path via rootless netns → HealthCmd → weak "running" with warning; K successes/timeout | works | `internal/deploy/deploy.go:540-560`; `internal/deploy/system.go:237-260` | |
| switch with token router, verify, drop token router; restore on failure (D7/D8) | works | `internal/deploy/deploy.go:397,431,512-538` | |
| drain grace, keep 5 images, prune | works | `internal/deploy/deploy.go:445-495` | |
| failure: candidate removed, live untouched, `failed`, notification | works | `internal/deploy/deploy.go:268-285` | |
| recreate default with volumes | works | `internal/spec/spec.go:54-62`; `internal/deploy/deploy.go:405` | |
| rollback = old image, no rebuild | works | `internal/apps/apps.go:338`; `internal/api/apps.go:279-300` | |
| first failed deploy leaves nothing | works | `internal/deploy/deploy.go:274-276` | |
| per-app lock; reconciliation only on `dg-`/`app-` | works | `internal/deploy/deploy.go:110-115`; `internal/reconcile/reconcile.go:2,29-30,61` | |

### 2.4 §3.6 Per-app middleware

| Requirement | Status | Where | Note |
| --- | --- | --- | --- |
| typed structs, JSON output, router-level chains (D27) | works | `internal/spec/spec.go:107-139`; `internal/traefik/dynamic.go:248-292` | |
| defaults reproduce vps-setup headers, rate limit, compression | works | `internal/spec/spec.go:141-154` | `curl -sI` comparison is Step 3 |
| per app: headers on/off/edit/remove/custom; compression; rate limit avg/burst/off; basic auth; IP allowlist; max body; aliases/redirect; path prefix + strip | partial | model + validation `internal/spec/spec.go:402-444`; CLI only via `apps edit` JSON and `middleware basic-auth` (`internal/cli/apps.go:266-311,651-713`) | no UI (**G1**); bcrypt only client-side (**G12**) |
| header CRLF / control characters rejected | works | `internal/spec/spec.go:186,237-250,403-415`; `internal/traefik/traefik_test.go:155` | no test through the HTTP API yet |
| save re-renders the route, no restart | partial | `internal/api/apps.go:175-203` → `internal/deploy/deploy.go:564-573` | writes the file only: no Traefik verification, no restore on rejection, DB saved before render → **G11** |

### 2.5 §3.7 Databases

| Requirement | Status | Where | Note |
| --- | --- | --- | --- |
| create → container + volume + own network `dg-db-<name>`, never `web` | works | `internal/quadlet/units.go:147-240` | |
| random credentials ≥ 32 bytes | works | `internal/databases/databases.go:150-151` | |
| link: join network, inject `DATABASE_URL` + engine vars, editable names | works | `internal/databases/databases.go:445-462`; `internal/deploy/deploy.go:225-260` | UI renames only `DATABASE_URL` |
| restart in place | works | `internal/api/databases.go:63` | |
| "pull and restart" within the major | missing | not found | → **G32** |
| engine-appropriate capabilities | works | `internal/databases/databases.go:55,62,69` | runtime proof not re-run |
| SSH-tunnel instructions in UI | partial | `internal/databases/databases.go:510-530`; `internal/api/databases.go:208,258` | SSH port hard-coded `"22"` → **G22** |

### 2.6 §3.8 Backups and restore

| Requirement | Status | Where | Note |
| --- | --- | --- | --- |
| S3 settings | works | `internal/backup/settings.go:84-112`; UI `ui/src/routes/backups/+page.svelte:61-80` | CLI cannot set them → **G14** |
| restic password generated, **shown once**, offline warning | missing | silently generated on first GET `internal/backup/settings.go:45-50` | → **G6**; plaintext in every GET → **G13** |
| per-DB timer 03:00 Asia/Jakarta, random delay, retention 7/4/6 | works | `internal/backup/timers.go:45-88`; `internal/quadlet/timer.go:33-41`; `internal/backup/database.go:41-43` | |
| streaming dumps via `--stdin-from-command` | works | `internal/backup/restic.go:61-79`; `internal/backup/database.go:61-83` | |
| system backup daily (`VACUUM INTO`, config, env, keys, Traefik), excludes `master.key`; `keys export` | partial | `internal/backup/system.go:21-150,224-255`; `timers.go:100-136` | UI export broken → **G19** |
| per-app volume backups, off by default, warning | partial | `internal/backup/volume.go`; `internal/api/backup.go:128-134` | manual only; no toggle, schedule, warning → **G37** |
| weekly `restic check`, periodic `--read-data-subset`; forget --prune | partial | `internal/backup/restic.go:167-200` | no check timer → **G16** |
| restore same DB (safety dump, typed confirm) / new DB; test restore | works | `internal/backup/database.go:108-360`; `internal/api/databases.go:453-501` | no UI restore (**G21**); UI test-restore text fabricated (**G20**) |
| DR: fresh → install → `restore-system` → apps redeployed, DBs restored | partial | `internal/backup/system.go:152-222`; `internal/cli/backup.go:181-208` | files only, no redeploy, no DB restore, live DB overwritten, no key import → **G17** |
| failing backup fails the run and notifies | partial | `restic.go:79,125`; `OnFailure` broken on VM (§1); API runs don't notify (`internal/api/backup.go:144-148`, `databases.go:465-468`) | → **G15** |

### 2.7 §3.9 Observability

| Requirement | Status | Where | Note |
| --- | --- | --- | --- |
| live runtime logs (journalctl JSON, SSE); build logs streamed | works | `internal/api/apps.go:303-376` | |
| `podman stats` every 30 s, kept 7 d, charted | partial | `internal/telemetry/collector.go:181,201` | current value only, no chart → **G33** |
| access-log analytics per minute, 14 d, self-rotated | works | `internal/telemetry/accesslog.go:296-329`; `collector.go:205,209` | |
| server overview incl. security updates, reboot-required | works | `internal/telemetry/host.go:18-34,175-244` | |
| notifications ntfy/Telegram/Discord: deploy failed/succeeded, backup failed, flapping, disk > 85 % | partial | `internal/notify/notify.go:23-25,107-123`; `deploy.go:281-289`; `collector.go:164-173` | no flapping; `deploy.success` not in the default event list; backup-failed broken → **G15**, **G34** |

### 2.8 §3.10 UI pages

| Requirement | Status | Where | Note |
| --- | --- | --- | --- |
| minimal, dark/light, keyboard-friendly, no component framework | works | `ui/src/lib/stores/theme.ts`; `ui/src/routes/+layout.svelte:34-73`; `ui/package.json` | |
| Dashboard: server overview + app cards (status, live release, last deploy, CPU/RAM sparkline) | partial | `ui/src/routes/+page.svelte:30-36,360-420` | numbers, no sparkline → **G33** |
| App → Overview · Deployments (list, live log, rollback, redeploy) · Logs · Environment | works | `ui/src/routes/apps/[name]/+page.svelte:337-584` | no deploy-by-ref; rollback via row button |
| App → **Domains & Middleware** | missing | tab declared `:15,317`, never rendered (blocks `:337-633`) | **G1** |
| App → Database links | works | `ui/src/routes/apps/[name]/+page.svelte:586-631` | |
| App → **Settings** (source, build, health, resources, strategy, auto-deploy, watch paths) | missing | tab declared `:15,319`, never rendered | **G1** |
| App → Danger zone (keep or purge data) | works | `ui/src/routes/apps/[name]/+page.svelte:633-663` | |
| New app wizard: repo → deploy key + link → webhook + link → port, domain, health → first deploy | partial | `ui/src/routes/apps/new/+page.svelte` | sends `strategy: "zero-downtime"`, which the server rejects → **G10**; hard-coded "portfolio" defaults → **G5** |
| wizard detects Containerfile/Dockerfile + `EXPOSE` | missing | name guess only `ui/src/routes/apps/new/+page.svelte:27-37` | **G5** |
| wizard supports `image` apps | missing | git only `:48-62` | → **G29** |
| Databases: list, create, connection info, link, backups, restore, test restore | partial | `ui/src/routes/databases/+page.svelte`, `ui/src/routes/databases/[name]/+page.svelte` | no restore (**G21**); test-restore result fabricated (**G20**) |
| Backups: S3 settings, schedules, history, system backup, key export | partial | `ui/src/routes/backups/+page.svelte` | no bucket field; key export broken (**G19**); schedule is static text `:184`; history = snapshots only (**G18**); no restore-system (**G21**) |
| Settings: password, TOTP, global defaults, hook hostname, notifications, SSH snippet | partial | `ui/src/routes/settings/+page.svelte` | password/TOTP only as copyable CLI text (D54, see **G24**); no global defaults (**G23**); snippet hard-codes port 4222 (`:414-432`, **G22**) |
| Audit log | works | `ui/src/routes/audit/+page.svelte` | |
| SSE; same JSON API; TS types generated from Go (tygo) | partial | `tygo.yaml`; `Makefile:23-25`; `ui/src/lib/api/client.ts` | several client types are hand-written and wrong (`exportKeys`, `syncKnownHosts`) → **G19** |

### 2.9 §3.11 CLI

| Requirement | Status | Where | Note |
| --- | --- | --- | --- |
| `status`, `apps list/show/create/edit/remove`, `deploy`, `rollback`, `logs [-f] [--build]`, `env list/set/unset/edit`, `domains`, `middleware` | works | `internal/cli/cli.go:35-64`; `internal/cli/apps.go` | `middleware` only shows + basic-auth; the rest via `apps edit` JSON (D41) |
| `db list/create/link/unlink/shell/remove` | works | `internal/cli/db.go:27-60` | |
| `backup run/list/restore/test` | partial | `internal/cli/backup.go:30-53`; DB restore/test in `internal/cli/db.go:54-57` | no `backup restore`/`backup test`; they exist as `db restore`/`db restore-test` |
| `restore-system`, `keys export`, `admin set-password`, `panel`, `self-update` | works | `internal/cli/cli.go:41-59` | |
| `doctor`: lingering, subuids, sockets, **Traefik health**, **DNS**, disk, **restic reachability**, clock | partial | `internal/doctor/doctor.go:26-38` | **G4** |
| human output, `--json` | partial | `--json` on status, audit, doctor, apps list/show, db list/create/show, backup snapshots | not on deploy, env, domains, middleware, settings, backup settings |
| stdlib `flag`, no framework | works | `internal/cli/cli.go:1-33` | |

### 2.10 §4 Security requirements

| Requirement | Status | Where | Note |
| --- | --- | --- | --- |
| §4.1 nothing as root after install; daemon refuses root | works | `internal/daemon/daemon.go:46-50` | |
| §4.2 panel on 127.0.0.1; Host check (421); strict Origin on mutating requests (403) | works | `internal/api/api.go:151-176`; tests `internal/api/api_test.go:91,105` | |
| §4.3 argon2id, optional TOTP, backoff, cookie `HttpOnly; SameSite=Strict; Path=/`, 12 h / 7 d, server-side logout | works | `internal/auth/password.go:11-58`; `internal/auth/auth.go:19-23,62,102,122`; `internal/api/api.go:211-228` | |
| §4.4 webhook: HMAC before parse, 1 MiB, push/workflow_run, repo+branch, dedupe, 202, rate limit, unknown id 404 w/o timing leak, timeouts | works | `internal/webhook/webhook.go:27,37-47,82-159,187-233`; `internal/apps/apps.go:405-408`; `internal/api/api.go:63-72` | |
| §4.5 no shell interpolation; strict validation; typed serialization | partial | see §3.2 row | **G38** |
| §4.6 secrets never logged, never returned after creation except explicit reveal, masked in logs | partial | env masked `deploy.go:589-652`; env reveal audited `api/apps.go:388-391` | restic password returned by plain GET → **G13** |
| §4.7 NoNewPrivileges, cap drop, limits 512m/1/512, no ports, no privileged, named volumes only | works | `internal/quadlet/units.go:84-105`; `internal/spec/spec.go:84,174-177` | |
| §4.8 `go.sum` + `bun.lock`, justified deps, govulncheck in CI, checksums, verified self-update, pinned images | partial | `go.sum`, `ui/bun.lock` tracked; `.github/workflows/release.yml:36-57`; `internal/cli/self_update.go:131-229` | `^` ranges, `bun-version: latest`, `traefik:v3.7`, `postgres:18`, `valkey:9`, `DERMAGA_VERSION="latest"`, actions on major tags → **G7**, **G30** |
| §4.9 every vps-setup hardening guarantee kept | partial | `install/install.sh:427-487,714-863` | modern SSH algorithms + `LogLevel VERBOSE` (vps-setup `1180-1193`) dropped → **G27**; `ip_unprivileged_port_start = 80` (vps-setup sets it too, v2 BRIEF §2.3 rejects it) → **G26** |
| §4.10 `THREAT-MODEL.md` incl. sudo risk | partial | `git show HEAD:docs/THREAT-MODEL.md` | v1 copy only in HEAD (§0) |

### 2.11 §5 GitHub integration

| Requirement | Status | Where | Note |
| --- | --- | --- | --- |
| ed25519 deploy key per app, 0600, read-only instructions | works | `internal/gitsrc/gitsrc.go:33-55`; `internal/cli/apps.go:234-248` | |
| webhook secret ≥ 32 bytes, exact URL | works | `internal/apps/apps.go:64-86`; `internal/api/apps.go:101-122` | |
| optional CI gate | works | `internal/spec/spec.go:353-364`; `internal/webhook/webhook.go:213-229` | not editable in UI (G1) |
| watch paths, fail open | works | `internal/webhook/webhook.go:244-260` | |
| GitHub host keys pinned **at install time**, fingerprints shown | partial | command `internal/api/apps.go:446-462`; installer: not found | → **G25** |

### 2.12 §6 Installer

| Requirement | Status | Where | Note |
| --- | --- | --- | --- |
| Bash, `set -Eeuo pipefail`, idempotent | partial | `install/install.sh:12,45-58` | unproven; restarts journald/fail2ban/traefik every run (`:280,804,710`) |
| `config-example` + `init --config` | works | `install/install.sh:60-89,963-1001` | |
| safety: key before password lock, `sshd -t`, never restart nftables, **detect other :80/:443 listener** | partial | `install/install.sh:812-848,757-763` | port-busy check missing (vps-setup `990`) → **G27** |
| installs Podman, Traefik (Quadlet, sockets, file provider), **restic, git**, curl | partial | `install/install.sh:245-251,598-712` | **G3** |
| binary for arch, SHA-256 verified | partial | `install/install.sh:543-596` | missing `checksums.txt` only warns (`:590-592`); empty match passes (`:586`) → **G9** |
| user service, linger, start, `doctor` | works | `install/install.sh:378-389,865-901` | doctor result ignored (`:900`) |
| next steps (snippet, URL, set-password, TOTP) | works | `install/install.sh:927-961` | |
| `MIGRATING-FROM-VPS-SETUP.md` with rollback | unclear | `docs/MIGRATING-FROM-VPS-SETUP.md` | not exercised |

### 2.13 §7 Repository and quality bar

| Requirement | Status | Where | Note |
| --- | --- | --- | --- |
| layout `cmd/ internal/ ui/ install/ docs/` | works | tree | |
| gofmt, vet, staticcheck, govulncheck | works | `Makefile:13-18` | passes this session |
| TypeScript strict + **Prettier** | partial | `ui/tsconfig.json:4` | no Prettier → **G31** |
| golden tests (Quadlet, Traefik, timers), webhook tests, state machine with fake host | works | `internal/quadlet/quadlet_test.go`, `internal/traefik/traefik_test.go`, `internal/webhook/webhook_test.go`, `internal/deploy/deploy_test.go` | timer test asserts the `%%n` bug as correct (`internal/backup/backup_test.go:364`) → **G15** |
| integration harness `test/vm/*.sh` | missing | `test/` not found | → **G35** |
| UI check part of the full check | missing | `Makefile:13` has no `ui-check` | → **G31** |

### 2.14 §8 milestone acceptance and §9 global acceptance (code readiness only)

| Item | Status | Note |
| --- | --- | --- |
| M4: 50 req/s, 0 non-2xx / 0 conn errors, good and failing deploy | unclear | code path exists; run in Step 3 |
| M6: rebuild from the bucket, everything back | missing | blocked by **G17**; the VM has never used a bucket (repo is local, §1) |
| M8: fresh → one command → doctor green; re-run no-op; port scan | unclear | blocked by **G3**, **G4**, **G9**; run in Step 3 |
| M10: Kapturae from its repo | unclear | the URL was never recorded; the VM has `r3p-dev/kapturae` — you must confirm |
| §9 headers match vps-setup, compression | unclear | Step 3 |
| §9 daemon idle < 30 MB; daemon + Traefik < 100 MB | unclear | §1: daemon 25.5 MB, **Traefik 97.7 MB** (not idle-measured) → **G36** |
| §9 backups on schedule, test restore, DR once | missing | **G15**, **G17** |
| §9 reboot survival | unclear | Step 3 |

## 3. Known gaps G1–G8 from v2 PLAN Phase 0

| # | Verdict | Evidence |
| --- | --- | --- |
| **G1** App page lacks Domains & Middleware and Settings tabs | **confirmed** | `ui/src/routes/apps/[name]/+page.svelte:15` declares `'domains'` and `'settings'`; `:312-321` renders the tab buttons ("Domains & Proxy", "Settings"); the `{#if}` chain `:337,423,482,506,586,633` has no branch for either, so both show an empty page. |
| **G2** no API to edit middleware/settings from the UI | **partly refuted, real defect found** | `PUT /api/apps/{app}` exists on the panel (`internal/api/apps.go:61`, panel wiring `internal/api/api.go:83`) and the client has `updateApp` (`ui/src/lib/api/client.ts:122-130`), never called by any page. The API is not fit for G2: (a) basic-auth hashes must arrive pre-hashed (bcrypt only in the CLI, `internal/cli/apps.go:672`), so the UI cannot add a user without hashing in the browser (**G12**); (b) saving writes the route without Traefik verification and without restoring the previous file, and the DB is updated before the render (`internal/api/apps.go:192-200`, `internal/deploy/deploy.go:564-573`) (**G11**); (c) no endpoint returns the effective header set for a preview. |
| **G3** installer lacks `restic` and `git` | **confirmed** | `install/install.sh:245-251`: package lists contain neither. (They exist on the VM because vps-setup installed them; §1 `command -v` output.) |
| **G4** doctor lacks Traefik health, DNS, restic | **confirmed** | `internal/doctor/doctor.go:31-36` (host checks), `internal/cli/doctor.go:53-80`, `internal/api/api.go:255-282`; VM output in §1 has none of them. The API and CLI also build the list twice (duplicate code). |
| **G5** wizard does not detect Containerfile/Dockerfile + `EXPOSE` | **confirmed** | `ui/src/routes/apps/new/+page.svelte:27-37` guesses from the repo **name** ("portfolio" → port 3000, `/healthz`, domain `portfolio.vm.local`); no API reads the repository before the first deploy. |
| **G6** restic password not shown once / no offline confirmation | **confirmed** | `internal/backup/settings.go:45-50` generates it silently on the first `GET /api/backup/settings` (and writes on a GET); the UI never shows it; the CLI shows it on demand with `--reveal-password` (`internal/cli/backup.go:146,163-164`). No master-key + password recovery statement in the UI. |
| **G7** unpinned versions | **confirmed, wider** | `.github/workflows/release.yml:24` `bun-version: latest`; `ui/package.json:14-22` all `^`. Also `actions/*@v4/v5`, `softprops/action-gh-release@v2` (major tags) and the items in **G30**. |
| **G8** evidence claims parity that does not exist | **confirmed** | `docs/M7-EVIDENCE.md:357-396` marks every row "Verified", including `domains add/remove/primary` (`:374`) and `middleware` (`:375`) on `/apps/[name]` (not rendered, G1), `db restore` (`:383`, no UI, G21), `keys export` (`:390`, broken, G19), `backup check … subset verification` (`:388`, UI sends no subset). Other evidence files: `docs/M6-EVIDENCE.md:320-411` is a volume wipe labelled "Disaster Recovery Drill"; `docs/M10-EVIDENCE.md:249-267` describes a Kapturae migration, but no document records you giving the repo URL (BRIEF §1.4 requires asking). |

## 4. Additional gaps found (G9 …)

Severity: **S** = security or data loss · **B** = broken feature · **M** = missing v1 requirement ·
**H** = hygiene.

| # | Sev | Gap | Evidence |
| --- | --- | --- | --- |
| G9 | S | Installer installs an **unverified binary** when `checksums.txt` can't be fetched, and passes when it has no line for the binary | `install/install.sh:582-592`, `:586` |
| G10 | B | Wizard sends `strategy: "zero-downtime"`; the server accepts only `""`, `recreate`, `blue-green`, so **the wizard's default always fails** | `ui/src/routes/apps/new/+page.svelte:19,61,267`; `internal/spec/spec.go:263-265`; run this session: `Validate(strategy=zero-downtime) = strategy "zero-downtime": recreate or blue-green` |
| G11 | S | Route edits are not verified through Traefik and not rolled back; DB saved before render | `internal/api/apps.go:192-200`; `internal/deploy/deploy.go:562-573` |
| G12 | B | Basic-auth passwords can only be hashed by the CLI; API accepts only hashes | `internal/cli/apps.go:664-677`; `internal/spec/spec.go:424-426` |
| G13 | S | `GET /api/backup/settings` returns the **restic password in plaintext** and **generates + writes** it on a GET | `internal/api/backup.go:45-60`; `internal/backup/settings.go:45-50,73` |
| G14 | M | CLI cannot set S3/restic settings | `internal/cli/backup.go:144-170` |
| G15 | B | Backup failure alerts broken: `OnFailure=dg-backup-failed@%%n.service` rejected by systemd (journal §1), because the INI writer doubles `%`; API backup failures never notify; the test asserts the wrong value | `internal/quadlet/ini.go:48`; `internal/backup/timers.go:59,113`; `internal/backup/backup_test.go:364`; `internal/api/backup.go:144-148`; `internal/api/databases.go:465-468` |
| G16 | M | No weekly `restic check` / `--read-data-subset` timer | `internal/backup/timers.go` (DB + system only) |
| G17 | B | `restore-system` only writes files: overwrites the **open** SQLite DB under the running daemon; no DB restore, no redeploy, no `master.key` import; user told to restart by hand | `internal/backup/system.go:152-222`; `internal/cli/backup.go:207` |
| G18 | M | Backup history lacks failed runs, status, duration, error | `005_backups.sql:10-18` |
| G19 | B | UI master-key export shows nothing (API `key`, client `masterKeyB64`); `syncKnownHosts` typed `string[]`, API returns a map; hand-written types | `internal/api/backup.go:218-221`; `ui/src/lib/api/client.ts:173-174,287-288`; `ui/src/routes/backups/+page.svelte:112` |
| G20 | S | UI prints **fabricated results**: test restore always "Integrity check: PASS"; `restic check` success text hard-coded | `ui/src/routes/databases/[name]/+page.svelte:96`; `ui/src/routes/backups/+page.svelte:99` |
| G21 | M | UI lacks DB restore, `restore-system`, manual `backup run`, check subset | `ui/src/lib/api/client.ts:238-285` (defined, unused) |
| G22 | B | SSH port hard-coded: DB snippet `"22"`, Settings shows `Port 4222` and `<your-server-ip>` | `internal/api/databases.go:208,258`; `ui/src/routes/settings/+page.svelte:414-432` |
| G23 | M | Global defaults (limits, headers, rate limit) not stored or editable | `internal/spec/spec.go:84,143-154` |
| G24 | M / decision | Password and TOTP not changeable in UI (D54); v1 §3.10 lists them on Settings, v2 is GUI-first | `internal/api/api.go:104-106`; `ui/src/routes/settings/+page.svelte:331-375` |
| G25 | M | Installer never pins GitHub SSH host keys | `install/install.sh` (not found) |
| G26 | S / decision | Installer sets `net.ipv4.ip_unprivileged_port_start = 80` (copied from vps-setup.sh `665`); v2 BRIEF §2.3 rejects it; live on VM | `install/install.sh:472` |
| G27 | S | Hardening lost vs vps-setup.sh: SSH algorithm lists, `LogLevel VERBOSE`, busy :80/:443 check, `show_plan` | `install/install.sh:818-843` vs vps-setup.sh `1165-1193`, `990`, `400` |
| G28 | H / decision | v1 docs overwritten by v2 texts; `docs/v2/` missing (§0) | `git diff --stat -- docs` |
| G29 | M | Wizard cannot create `image` apps | `ui/src/routes/apps/new/+page.svelte:48-62` |
| G30 | S | More floating versions: `traefik:v3.7` + `AutoUpdate=registry`; `postgres:18`, `mariadb:11.8`, `valkey:9`; `DERMAGA_VERSION="latest"`, fallback `v0.8.0` | `install/install.sh:14,82,86,128,130,562,655`; `internal/databases/databases.go:52,59,66` |
| G31 | H | Full check omits `bun run check`; no Prettier | `Makefile:13` |
| G32 | M | No DB "pull and restart" | not found |
| G33 | M | No CPU/RAM charts or sparklines | `ui/src/routes/+page.svelte:360-420` |
| G34 | M | No flapping alert; `deploy.success` not in defaults/UI | `internal/notify/notify.go:86`; `internal/deploy/deploy.go:289` |
| G35 | M | No integration harness `test/vm/*.sh` | not found |
| G36 | unclear | Traefik 97.7 MB RSS on VM; budget needs idle measurement | `install/install.sh:660` |
| G37 | M | DB backup schedule/retention and volume backups not configurable; no volume warning | `internal/databases/databases.go:178-179`; `internal/api/backup.go:128-134` |
| G38 | H | `sh -c` + `fmt.Sprintf(name)` inside DB containers (validated name) | `internal/backup/database.go:143,183,309,314` |
| G39 | H | VM runs `a8cf2fd-dirty`, not `HEAD` | §1 |
| G40 | H | `LICENSE` is an empty, untracked file (0 bytes); v1 BRIEF §2 requires Apache-2.0 | `ls -la` in the repo root: `LICENSE` size 0; `git status`: `?? LICENSE`. Adding the Apache-2.0 text is your call (I won't invent a copyright line). **Deferred by owner; LICENSE removed for now** (2026-11-12): the owner removed the file on purpose; do not recreate it |
| G41 | H | No CI on push/PR; `make check` only runs on release tags | `.github/workflows/release.yml:3-7` (`on: push: tags: 'v*.*.*'`) is the only workflow |
| G42 | M | Installer and `doctor` do not check the minimum Podman version; units can fail on older hosts | `quadlet.MinPodman` (5.4.2) is only used by `TestDryRun`; first CI run on `ubuntu-24.04` (2026-11-12): `unsupported key 'StopTimeout' in group 'Container'`. Fix order: `doctor` row in CS6, installer abort in CS7 |
| G43 | S / B | The Backups settings form loads the masked S3 secret (`••••••••`) from `GET /api/backup/settings` and sends it back on save, so saving any setting replaces the real secret key with the mask and S3 backups stop authenticating | `ui/src/routes/backups/+page.svelte` (prefill and save of `s3SecretKey`); `internal/api/backup.go` `putBackupSettings` stored whatever arrived. Fixed 2026-11-13: the server refuses the mask with 400 and keeps the stored key when the field is absent or empty; the form sends the key only when typed. Other masked secrets checked: the notification token is never prefilled and is kept when empty; the env-var mask is display only |

## 5. CLI ↔ UI parity (every command and flag)

Source: `internal/cli/cli.go:35-64` and each subcommand's `flag` set. "missing" = no UI control.
Internal commands (`daemon`, `hook-relay`, `probe`, `db-bridge`, `backup failure-notify`,
`traefik static`, `version`) are machine-only and excluded.

| CLI | Flags | UI control | Status |
| --- | --- | --- | --- |
| `status` | `--json` | Dashboard + layout status (`+page.svelte:30`, `+layout.svelte:42`) | ok |
| `audit` | `--limit`, `--json` | `/audit` limit input (`audit/+page.svelte:93`) | ok |
| `doctor` | `--json` | Dashboard doctor modal (`+page.svelte:530`) | ok (inherits G4) |
| `panel` | `--host` | Settings snippet | **wrong** (hard-coded, G22) |
| `admin set-password` | — | copyable CLI text only | **missing** (G24) |
| `admin totp enable/disable` | — | copyable CLI text only | **missing** (G24) |
| `apps list` | `--json` | Dashboard cards | ok |
| `apps show` | `--json` | App Overview | ok |
| `apps create` (git) | `--repo --branch --port --domain --alias --redirect --health-path --health-cmd --context --containerfile --volume --strategy --no-auto-deploy --workflow --watch --memory --cpus --read-only --cap` | wizard: repo, branch, containerfile, port, health path, domain, strategy, auto-deploy | **partial**: no alias/redirect, health-cmd, context, volume, workflow, watch, memory/cpus, read-only, cap; strategy broken (G10) |
| `apps create --image` | same | — | **missing** (G29) |
| `apps edit` | `--file` | — (`updateApp` unused) | **missing** (G1) |
| `apps remove` | `--purge`, `--yes` | Danger zone with purge checkbox | ok |
| `apps webhook` | — | "reveal webhook" button (`apps/[name]/+page.svelte:253-287`) | ok |
| `deploy` | `--ref`, `--no-follow` | Deploy button + live log | **partial**: no ref |
| `rollback` | `--to`, `--no-follow` | per-release rollback button | ok |
| `logs` | `-f`, `-n`, `--build` | Runtime logs (fixed 100 lines), release log modal | **partial**: no `-n` |
| `env list/set/unset/edit` | `--reveal` | Environment tab with reveal | ok |
| `domains add/remove/primary` | `--redirect` | — | **missing** (G1) |
| `middleware show` | — | — | **missing** (G1) |
| `middleware basic-auth add/remove` | — | — | **missing** (G1, G12) |
| `db list` | `--json` | `/databases` | ok |
| `db create` | `--engine --version --json` | create modal | ok |
| `db show` | `--reveal --json` | detail page with reveal | ok |
| `db link` | `--env KEY=ALIAS` (repeatable) | link modal (only `DATABASE_URL` alias) | **partial** |
| `db unlink` | — | unlink button | ok |
| `db shell` | — | — | n/a: needs a terminal; UI shows connection info instead (decide whether acceptable) |
| `db proxy` | `--port` | tunnel snippet | **partial** (port 22 hard-coded, G22) |
| `db restart` | — | restart button | ok |
| `db backup` | — | backup button | ok |
| `db restore` | `--snapshot --into --yes` | — | **missing** (G21) |
| `db restore-test` | — | test-restore button | **wrong** (fabricated output, G20) |
| `db remove` | `--purge --force` | delete modal | ok |
| `backup list/snapshots` | `--tag --json` | snapshot explorer with tag filter | ok |
| `backup run <target>` | — | DB backup + system backup buttons; no volume target | **partial** (G21) |
| `backup system` | — | system backup button | ok |
| `backup check` | `--read-data --subset` | check button, no subset | **partial**; output text fabricated (G20) |
| `backup settings` | `--reveal-password` | settings modal (edit); no reveal | **partial** (G6, G13, G14) |
| `restore-system` | `--snapshot -y` | — | **missing** (G21) |
| `keys export` | — | export button | **broken** (G19) |
| `settings` | `--hook-host` | Settings hook host | ok |
| `config notifications show/set` | `--provider --target --token --events` | Settings notifications | ok |
| `notify test` | `--provider --target --token` | "Send test notification" | ok |
| `github known-hosts` | — | Settings button | **partial** (fingerprints rendered wrong, G19) |
| `self-update` | `--check --version --force --repo --url` | — | **missing** (needs sudo to write `/usr/local/bin`; decide whether UI should offer `--check` only) |

## 6. Decisions needed before Step 2

| # | Question | Why it matters |
| --- | --- | --- |
| D-a (G28) | Restore v1 texts to `docs/*.md` (from `HEAD`) and move the v2 texts to `docs/v2/*.md`, as the rules and v2 PLAN expect? Plain file copies only; no `git mv`. | Every reference to "v1 BRIEF" is currently ambiguous. |
| D-b (G26) | v2 BRIEF §2.3 says socket activation makes `ip_unprivileged_port_start=80` unnecessary. That is not true for the **user** manager: `traefik-http.socket` is a user unit (`install/install.sh:613-638`), and an unprivileged systemd user manager cannot bind :80 without that sysctl (vps-setup.sh's own comment at `664`). Options: (1) keep the sysctl and record the deviation; (2) user sockets on high ports + nftables `redirect` 80→8080 / 443→8443 (also in `output` for local traffic); (3) system-level socket units handing fds to a user service (not supported across managers without a proxy). This is reasoning from the code and vps-setup's comment, **NOT RUN** on the VM; option 2 needs a VM spike before I recommend it. | Contradicts the brief either way; hard to reverse on installed hosts. |
| D-c (G24) | Keep D54 (password/TOTP only via CLI) for v1.0, or add UI forms with re-authentication (current password + TOTP)? | v1 §3.10 vs D54; v2 Phase 1 adds `/setup`. |
| D-d (G31) | Prettier is a new dev dependency. Add it (pinned, `bun run format:check` in the full check) or record the deviation? | `pin-versions`: no new dependency without approval. |
| D-e | Which v1 items may move to Phase 2 instead of Phase 0? Proposal below in §7 ("deferral candidates"). | Phase 0 is estimated at 30 h; closing every gap is ~55 h. |

Designs that are not obvious and that I will propose and wait on at their change set (as asked in
Step 2): CS6 (how `doctor` checks Traefik health and what "DNS correct" means behind NAT), CS8
(how the wizard reads a private repo before the first deploy), CS9 (restore-system / DR flow).

## 7. Proposed fix order (each change set commits on its own and passes the full check)

Sizes: S ≤ 2 h · M 2–5 h · L > 5 h.

| CS | Closes | Content | Size |
| --- | --- | --- | --- |
| CS1 | G7, G31 (part), G30 (actions) | Pin every `ui/package.json` dev dependency exactly (`bun add -d -E`), refresh `bun.lock`; `bun-version` = local Bun; actions pinned to exact versions; add `ui-check` to `make check`; DEPENDENCIES update | S |
| CS2 | G10, G19, G20, G22 | Wizard strategy values; replace hand-written client types with tygo-generated ones (add the missing Go response structs); show real test-restore / check output; SSH port and host from the server (`panel` logic shared with the API) | M |
| CS3 | G13, G15, G6, G14 | Mask the restic password in GET, no writes on GET; explicit "generate password" call that returns it once + required "stored offline" confirmation (API, UI, CLI); master key + password recovery text; fix `%n` escaping for `OnFailure=` with a golden test, notify on API/manual backup failure; `backup settings set` in the CLI | M |
| CS4 | G11, G12, G2 | Route edits: render → validate → atomic write with a verify token router → Traefik verification → drop token (D7) or restore the previous file and the previous DB row (D8); server-side bcrypt for basic-auth passwords; `GET /api/apps/{app}/headers` (effective set); API tests incl. CRLF/control-character injection | M |
| CS5 | G1 | App → Domains & Middleware tab and Settings tab (everything in v2 ARCHITECTURE §6 for both), server errors shown inline | L |
| CS6 | G4, G42 (doctor row) | `doctor`: Traefik health, DNS for every domain + hook host, restic reachability; one shared implementation for CLI and API; fix hints | M (design first) |
| CS7 | G3, G9, G25, G27, G30 (installer), G26 per D-b, G42 (installer check) | Installer: restic + git, mandatory checksum, pin GitHub host keys, SSH algorithm lists + `LogLevel VERBOSE`, busy-port check, exact Traefik tag, no `latest` default | M |
| CS8 | G5, G29 | Wizard: create → deploy key step → inspect (Containerfile/Dockerfile, first `EXPOSE`) → confirm/edit → first deploy; image-app path | M (design first) |
| CS9 | G17 | Real `restore-system`: master-key import, DB restored offline (not under the running daemon), databases restored from their latest snapshots, apps redeployed from their last good commits; UI + CLI Accepts an existing restic password through `backup password use-existing` (verified with `restic cat config` before saving, lands in `confirmed`; storage already fits, v2 ARCHITECTURE D62) | L (design first) |
| CS10 | G16, G18, G21, G37 | Weekly check timer; backup history with failures/duration/error; UI DB restore, restore-system, backup run, check subset; DB schedule/retention edit; volume backup opt-in with warning | L |
| CS11 | G38, G34, G32 | No `Sprintf` in container shell strings (pass names as env/args); flapping alert + `deploy.success` in defaults; DB "pull and restart" | M |
| CS12 | G23, G33, G35 | Global defaults stored and editable; CPU/RAM charts + sparklines; `test/vm/*.sh` harness used by Step 3 | L |

**Deferral candidates for D-e** (v1 items that overlap Phase 2 "GUI completeness"): G23, G24, G29,
G32, G33 and the parity partials (deploy `--ref`, logs `-n`, extra link aliases, `self-update`
in the UI). If you agree, CS12 shrinks to G35 only, CS8 to G5 only, CS11 to G38 + G34, and the
total drops from about 55 h to about 40 h.

G8, G28 and G39 are handled in Step 4 / before Step 3 (G39: install `HEAD` on the VM before any
evidence is taken). G36 is decided by the Step 3 measurement.
