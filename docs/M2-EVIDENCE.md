# M2 evidence: generators and reconciliation

Ran on 2026-11-07. Unit tests ran on the laptop (Fedora, Go 1.27.3, Podman 5.8.9). VM checks ran on
`dermaga-vm` (Debian 13, Podman 5.4.2, Traefik 3.7.17, state `vps-setup` plus the M1 service).
Output is copied from the terminal and trimmed only where marked `…`. Every VM check cleaned up
after itself. The final VM state is in §7.

## 1. `make check`

```
$ make check          # gofmt check first (prints nothing when clean)
go vet ./...
go tool staticcheck ./...
go tool govulncheck ./...
=== Symbol Results ===
No vulnerabilities found.
Your code is affected by 0 vulnerabilities.
This scan also found 0 vulnerabilities in packages you import and 1
vulnerability in modules you require, but your code doesn't appear to call these
vulnerabilities.
go test ./...
ok  	github.com/r3p-dev/dermaga/internal/api
ok  	github.com/r3p-dev/dermaga/internal/auth
ok  	github.com/r3p-dev/dermaga/internal/cli
ok  	github.com/r3p-dev/dermaga/internal/config
ok  	github.com/r3p-dev/dermaga/internal/daemon
ok  	github.com/r3p-dev/dermaga/internal/host
ok  	github.com/r3p-dev/dermaga/internal/probe
ok  	github.com/r3p-dev/dermaga/internal/quadlet
ok  	github.com/r3p-dev/dermaga/internal/reconcile
ok  	github.com/r3p-dev/dermaga/internal/relay
ok  	github.com/r3p-dev/dermaga/internal/secrets
ok  	github.com/r3p-dev/dermaga/internal/spec
ok  	github.com/r3p-dev/dermaga/internal/store
ok  	github.com/r3p-dev/dermaga/internal/traefik
make check exit=0
```

The module-level advisory is still GO-2026-5932 (x/crypto, no fixed tag yet, see
[`DEPENDENCIES.md`](DEPENDENCIES.md)). `go test ./... -update` rewrites every golden file. Every
test package imports `internal/golden`, because `go test` passes `-update` to all test binaries.
Binary size: 12 566 688 bytes (amd64), 11 927 712 bytes (arm64).

| Acceptance (PLAN M2) | Test / golden files |
|---|---|
| Golden: minimal app, every middleware toggle, aliases + www redirect, path prefix + strip, basic auth, allowlist, body limit, custom/removed headers | `traefik.TestAppGolden`: `internal/traefik/testdata/app-{minimal,every-toggle,everything-off,aliases-www,apex-to-www,path-prefix-strip,path-prefix-keep,basic-auth,allowlist,auth-allowlist-headers,body-limit,headers-custom-removed}.json` |
| Golden: DB-linked app, `recreate` app with a volume | `quadlet.TestContainerGolden`, `TestOtherUnitsGolden`: `internal/quadlet/testdata/units/{minimal,db-linked,recreate-volume,hardened,health-escaping,image-healthcheck,hooks}/` |
| Golden: systemd timer + service | `internal/quadlet/testdata/systemd/dg-backup-db-shopdb.{service,timer}` |
| Golden: `00-global.yml`, static `traefik.yml` | `traefik.TestGlobalAndStaticGolden`: `00-global-*.json`, `traefik-*.json` |
| D7: deploy-time file has `app-<name>-verify` on `verify` only, with the public middlewares minus `basicAuth`/`ipAllowList`; steady-state file has none | `app-*.verify.json` next to each `app-*.json`; `traefik.TestVerifyRouter` checks this for every case, and that `auth-allowlist-headers` keeps headers, rate limit and compression |
| Injection: newline, `%`, `$`, backtick and quotes in every user-controlled field are rejected or escaped; a domain can't break the rule | `traefik.TestInjectionRejectedOrEscaped`, `quadlet.TestInjectionRejected`, `quadlet.TestHealthCmdEscaping`, `spec.TestValidate` |
| `00-global.yml` reproduces vps-setup's `00-vps.yml` | `00-global-default.json`: same `secure` chain, `security-headers`, `rate-limit` (100/200), `compress` (1024), TLS options; app defaults add `Permissions-Policy` (`spec.DefaultHeaders`). The `curl -sI` comparison is M3 |
| Generated units pass `quadlet -dryrun` | `quadlet.TestDryRun` (laptop Podman 5.8.9 and VM Podman 5.4.2, §2) |
| Reconciliation with a fake host: orphan removed, missing file re-rendered, stopped live unit started, foreign files untouched, leftover verify router re-rendered | `reconcile.TestRepairs`, `TestMissingFileAndNoLiveRelease`, `TestLiveUnitMissingIsDriftAndErrorsContinue`; real host in §5 |
| Schema: one live release and one primary domain per app | `reconcile.TestLoadApps` |

## 2. `quadlet -dryrun` on the VM (Podman 5.4.2)

The quadlet test binary was cross-compiled (`go test -c`), copied to the VM with its `testdata/`,
and run there:

```
$ /usr/libexec/podman/quadlet -version
5.4.2
$ ./quadlet.test -test.v -test.run "DryRun|Golden|Injection|Escaping"
--- PASS: TestContainerGolden (0.00s)
--- PASS: TestOtherUnitsGolden (0.00s)
--- PASS: TestHealthCmdEscaping (0.00s)
--- PASS: TestInjectionRejected (0.00s)
--- PASS: TestDryRun (0.02s)
PASS
```

Raw output for the DB-linked app, its networks and the hook relay, staged with the VM's real
`web.network`. Lines are wrapped for reading, and the `dg-db-shopdb` and `web` network services,
which look like `dg-db-cache`, are left out (`…`):

```
## staged: dg-db-cache.network dg-db-shopdb.network dg-hooks.container dg-shop-1a2b3c4d.container web.network
exit=0
---dg-db-cache-network.service---
ExecStart=/usr/bin/podman network create --ignore --label dermaga.database=cache dg-db-cache
…
---dg-hooks.service---
ExecStart=/usr/bin/podman run --name dg-hooks --cidfile=%t/%N.cid --replace --rm --cgroups=split
    --stop-timeout 5 --pids-limit 32 --network web --sdnotify=conmon -d
    --security-opt=no-new-privileges --cap-drop all
    --read-only -v /run/user/1001/dermaga/hooks:/run/dermaga --memory=32m
    --rootfs /home/server/.local/share/dermaga/relay:O /dermaga hook-relay -listen :8080 -target /run/dermaga/hooks.sock
---dg-shop-1a2b3c4d.service---
ExecStart=/usr/bin/podman run --name dg-shop-1a2b3c4d --cidfile=%t/%N.cid --replace --rm --cgroups=split
    --pids-limit 512 --stop-timeout 10 --pull never
    --network web --network dg-db-shopdb --network dg-db-cache
    --sdnotify=conmon -d --security-opt=no-new-privileges --cap-drop all
    --label dermaga.app=shop --label dermaga.release=1a2b3c4d
    --env-file /home/server/.config/dermaga/env/shop.env --memory=512m --cpus=1 localhost/dermaga/shop:0123456789ab
## Memory= unit: exit=1         (D4 still holds on 5.4.2: "unsupported key 'Memory' in group 'Container'")
```

The timer and service pass systemd's own check:

```
$ systemd-analyze --user verify dg-backup-db-shopdb.timer dg-backup-db-shopdb.service
exit=0
$ systemd-analyze calendar "*-*-* 03:00:00 Asia/Jakarta"
Normalized form: *-*-* 03:00:00 Asia/Jakarta
    Next elapse: Sat 2026-11-07 20:00:00 UTC
```

### 2a. The free-text `HealthCmd` reaches Podman unchanged

The `health-escaping` golden was installed as a real unit. Test-only edits: busybox image,
`Exec=sleep 3600` and a temporary env file. Then `podman inspect`:

```
## input (spec.Health.Cmd):
sh -c 'test "$(cat /tmp/x)" = ok' && echo 100% `date` \n done
## unit line written by the INI writer (% and $ doubled, rest verbatim):
HealthCmd=sh -c 'test "$$(cat /tmp/x)" = ok' && echo 100%% `date` \n done
container exists after 0.2 s
## podman inspect .Config.Healthcheck.Test:
["CMD-SHELL","sh -c 'test \"$(cat /tmp/x)\" = ok' && echo 100% `date` \\n done"]
```

Byte for byte the input (JSON-escaped by `inspect`). Line breaks, a trailing backslash and
surrounding spaces are rejected before writing (`spec.Text`, `quadlet.checkValue`).

### 2b. Found: every Quadlet start waited 90 s for `network-online.target` (fixed, D29)

The first attempt at 2a hung. The start job was queued behind Quadlet's automatic
`podman-user-wait-network-online.service`:

```
$ systemctl --user list-jobs
102 dg-esc-1a2b3c4d.service                 start waiting
124 podman-user-wait-network-online.service start running
$ systemctl is-active network-online.target
inactive
$ systemctl --user cat podman-user-wait-network-online.service | grep -E "Exec|Timeout"
TimeoutStartSec=90s
ExecStart=/bin/sh -c 'until systemctl is-active network-online.target; do sleep 0.5; done'
```

On this Debian 13 system nothing pulls in `network-online.target`, so the wait runs into its
timeout. The vps-setup Traefik unit shows the effect at every boot:

```
$ journalctl --user -b -u traefik -u podman-user-wait-network-online -o short-monotonic
[    5.373112] Starting podman-user-wait-network-online.service - Wait for system level network-online.target as user....
[   95.371054] podman-user-wait-network-online.service: start operation timed out. Terminating.
[   95.596985] Starting traefik.service - Traefik reverse proxy (socket-activated)...
[   96.124767] Started traefik.service - Traefik reverse proxy (socket-activated).
```

Every generated Quadlet unit now starts with `[Quadlet] DefaultDependencies=false`, which Podman
5.4.2 supports:

```
## dg-a (no [Quadlet] section)        ## dg-b (DefaultDependencies=false)
[Unit]                                 [Unit]
Wants=podman-user-wait-network-online.service
After=podman-user-wait-network-online.service
```

After the fix the same container existed 0.2 s after `start --no-block` (2a). The Traefik unit
belongs to the installer (M8) and still has the delay. It is also present on any server set up
with vps-setup.sh (see the report).

## 3. Real Traefik loads every generated file (extra check)

A throwaway Traefik container (`docker.io/library/traefik:v3.7`, network `web`, no published
ports, `/acme` on tmpfs) ran with the generated `traefik.yml` and `00-global.yml`. Each golden
app file was moved atomically into its dynamic dir as `app-shop.yml`, one at a time, with 2.6 s
between files for the provider throttle (D9). The backend was busybox `httpd` named
`dg-shop-1a2b3c4d`. Requests came from the rootless netns to `verify` (:8081). Column spacing is realigned:

```
traefik 3.7.17 at 10.89.0.5; host listeners on 8081: 0
CASE                          FILE    TOKEN  NOTOK  GZIP  HEADERS
aliases-www                   steady  404    404    0
aliases-www.verify            verify  200    404    1     permissions-policy strict-transport-security x-frame-options
allowlist                     steady  404    404    0
allowlist.verify              verify  200    404    1     permissions-policy strict-transport-security x-frame-options
apex-to-www                   steady  404    404    0
apex-to-www.verify            verify  200    404    1     permissions-policy strict-transport-security x-frame-options
auth-allowlist-headers        steady  404    404    0
auth-allowlist-headers.verify verify  200    404    1     permissions-policy strict-transport-security x-frame-options x-robots-tag
basic-auth                    steady  404    404    0
basic-auth.verify             verify  200    404    1     permissions-policy strict-transport-security x-frame-options
body-limit                    steady  404    404    0
body-limit.verify             verify  200    404    1     permissions-policy strict-transport-security x-frame-options
every-toggle                  steady  404    404    0
every-toggle.verify           verify  200    404    1     content-security-policy permissions-policy strict-transport-security x-frame-options
everything-off                steady  404    404    0
everything-off.verify         verify  200    404    0
headers-custom-removed        steady  404    404    0
headers-custom-removed.verify verify  200    404    1     content-security-policy server x-frame-options
minimal                       steady  404    404    0
minimal.verify                verify  200    404    1     permissions-policy strict-transport-security x-frame-options
path-prefix-keep              steady  404    404    0
path-prefix-keep.verify       verify  200    404    1     permissions-policy strict-transport-security x-frame-options
path-prefix-strip             steady  404    404    0
path-prefix-strip.verify      verify  200    404    1     permissions-policy strict-transport-security x-frame-options
trusted-proxy                 steady  404    404    0
## Traefik log entries at level error/warn (ACME excluded):
(none)
```

What this shows for D7:

- `TOKEN 200` on every `.verify` file, **including basic auth and the allowlist**. The probe
  connects from `10.89.0.1` without a password, so these middlewares must be left off.
- `NOTOK 404`: without the token nothing on `verify` answers, so the public router is not
  attached to it.
- Steady-state files give `TOKEN 404`: the verify router is gone once the file is rewritten.
- Headers and gzip follow the case. `everything-off` has neither. `headers-custom-removed`
  replaced `Server` and dropped `Strict-Transport-Security` and `Permissions-Policy`.

## 4. Hook relay: mount the socket directory, not the socket file (D28)

The generated `dg-hooks` unit ran with the real paths. A Python Unix-socket HTTP server stood in
for the daemon's `hooks.sock` (the hook listener arrives in M3) and was restarted, which creates
a new socket inode, the same as a daemon restart:

```
## A: generated unit, directory mount (Volume=/run/user/1001/dermaga/hooks:/run/dermaga)
socket inode 477 mode 600
{"t":0,"ok":true,"status":200,"n":1}
-- restart the socket server (new socket inode)
socket inode 517 mode 600
{"t":0,"ok":true,"status":200,"n":1}
## B: single-file mount, as in M0/M1
socket inode 520 mode 600
{"t":0,"ok":true,"status":200,"n":1}
-- restart the socket server (new socket inode)
socket inode 564 mode 600
dermaga probe: unhealthy: no 1 consecutive successes within 4s
## relay memory: 6.816MB / 33.55MB
```

With the single-file mount (B), every daemon restart would have cut off webhooks until `dg-hooks`
was restarted too.

## 5. Startup reconciliation on the real host

The M2 binary was installed and started once, which applied migration 002 (`user_version 2`).
Then a broken state was set up: app rows inserted with Python's sqlite3 while the daemon was
stopped, plus units from the `minimal` golden. Test-only edits to the units: busybox image and
`Exec=sleep 3600`.

```
db: app shop, live release aaaaaaaa, failed release bbbbbbbb
## before
dg-shop-aaaaaaaa: unit inactive, container none          ← live release, stopped
dg-shop-bbbbbbbb: unit active, container running         ← orphaned candidate
dg-ghost-0badf00d: unit inactive, container none         ← app not in the database
app-shop.yml: verify router 1, backend dg-shop-1a2b3c4d  ← crash mid-deploy
## daemon start
INFO reconcile: repaired what="dg-shop-bbbbbbbb: removed orphaned release (not live)"
INFO reconcile: repaired what="dg-shop-aaaaaaaa: live release was inactive, started"
INFO reconcile: repaired what="app-shop.yml: still had the deploy-time verify router, re-rendered"
WARN reconcile: drift what="dg-ghost-0badf00d.container: no such app in the database (left alone)"
WARN reconcile: drift what="app-ghost.yml: no such app in the database (left alone)"
INFO reconcile: done apps=1 repaired=3 drift=2 errors=0
INFO started version=v0.0.0-20261107030203-3cff5df416d8+dirty panel=127.0.0.1:7070 socket=/run/user/1001/dermaga/dermaga.sock
(reconcile took about 11 s)
## after
dg-shop-aaaaaaaa: file yes, unit active, container running
dg-shop-bbbbbbbb: file no, unit inactive, container none
dg-ghost-0badf00d: file yes, unit inactive, container none
app-shop.yml: verify router 0, backend dg-shop-aaaaaaaa, mode 600
untouched: 00-vps.yml traefik.container app-ghost.yml dg-ghost-0badf00d.container
## second daemon start (idempotent)
WARN reconcile: drift what="dg-ghost-0badf00d.container: no such app in the database (left alone)"
WARN reconcile: drift what="app-ghost.yml: no such app in the database (left alone)"
INFO reconcile: done apps=1 repaired=0 drift=2 errors=0
## cleanup
db rows left: 0
containers: traefik ; units: traefik-acme.volume traefik.container web.network ; dynamic: 00-vps.yml
```

The 11 s are the orphan's `podman rm --time 10` (busybox `sleep` ignores SIGTERM). Reconciliation
runs before the listeners serve, so the CLI and panel wait that long at startup (D32).

## 6. Not verified in M2

- The `curl -sI` comparison against vps-setup's Traefik over HTTPS (PLAN puts it in M3, with a
  real app and a certificate). §3 checked the headers through `verify` only.
- The `verify` entrypoint inside the *installed* Traefik: the VM still runs vps-setup's static
  config, and the generated `traefik.yml` is installed in M8. §3 used a throwaway Traefik.
- Database engine units, the `dg-hooks` unit with the real daemon socket, and timers driven by
  the daemon come with M5, M3 and M6.
- Quadlet dry-run in CI: there is no CI workflow yet (M8). The test runs wherever
  `/usr/libexec/podman/quadlet` exists and skips elsewhere.

## 7. Final VM state

```
$ dermaga doctor
OK  lingering       enabled for server
OK  subuid          65536 ids
OK  subgid          65536 ids
OK  daemon          v0.0.0-20261107030203-3cff5df416d8+dirty, pid 4862, RSS 17.1 MB, socket /run/user/1001/dermaga/dermaga.sock (0600)
OK  admin password  set
OK  totp            enabled
OK  sockets         traefik-http.socket, traefik-https.socket active
OK  clock           NTP-synchronised
OK  disk            23% used, 15.0 GiB free (/home/server/.local/share/dermaga)
exit=0
```

`/usr/local/bin/dermaga` is the M2 build, and the database is at schema version 2 with no apps.
Only vps-setup's units and `00-vps.yml` are on disk. The busybox test image and `~/m2` were
removed.
