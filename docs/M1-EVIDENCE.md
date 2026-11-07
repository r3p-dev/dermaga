# M1 evidence: daemon core

Ran on 2026-11-07. Builds and unit tests ran on the laptop (Fedora, Go 1.27.3); VM checks on
`dermaga-vm` (Debian 13, state `vps-setup`, user `server`, uid 1001). Output copied from the
terminal, trimmed only where marked `…`. Session cookies and the TOTP secret are redacted. The VM
admin password used here is random and not recorded.

## 1. `make check` (gofmt, vet, staticcheck, govulncheck, tests)

```
$ make check
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
ok  	github.com/r3p-dev/dermaga/internal/probe
ok  	github.com/r3p-dev/dermaga/internal/relay
ok  	github.com/r3p-dev/dermaga/internal/secrets
ok  	github.com/r3p-dev/dermaga/internal/store
```

The module-level finding is GO-2026-5932 (`x/crypto/openpgp`, not imported); see
[`DEPENDENCIES.md`](DEPENDENCIES.md). Acceptance items covered by unit tests:

| Acceptance (PLAN M1) | Test |
|---|---|
| daemon refuses uid 0 | `daemon.TestRefusesRoot` (also on the VM, §3) |
| socket mode 0600; stale socket replaced; second daemon refused | `daemon.TestListenUnixModeStaleAndRunning` |
| migrations fresh → latest, re-run no-op, failed migration rolled back, numbering gaps rejected; WAL, foreign keys, DB files 0600 | `store.TestOpenMigratesAndRerunIsNoop`, `store.TestMigrateRollsBackAndRejectsBadNumbering` |
| secrets round-trip; tampered, wrong key, wrong context rejected; key file 0600, never overwritten | `secrets.TestRoundTripTamperWrongKeyWrongContext`, `secrets.TestLoadOrCreateKey` |
| argon2id; TOTP RFC 6238 vectors; TOTP required, skew ±1, replay refused; backoff 1 s, 2 s … 15 min cap | `auth.TestPassword`, `auth.TestHOTPMatchesRFC6238Vectors`, `auth.TestTOTPRequiredAndNotReplayable`, `auth.TestLoginBackoffAndReset` |
| session 12 h idle / 7 d absolute, logout deletes server-side, password change ends sessions | `auth.TestSessionTimeoutsLogoutAndPasswordChange` |
| Host other than `127.0.0.1:<port>`/`localhost:<port>` → 421 | `api.TestHostCheck` |
| mutating request with foreign or missing Origin → 403 | `api.TestOriginCheckOnMutatingRequests` |
| cookie `HttpOnly; SameSite=Strict; Path=/`; audit records login, failure, logout | `api.TestLoginSessionLogoutAndAudit` |
| 429 + `Retry-After` during backoff; body limit and strict JSON | `api.TestLoginBackoffReturns429`, `api.TestBadBodies` |
| admin routes only on the socket | `api.TestSocketAdminRoutesAreNotOnPanel` |
| every listener has the four timeouts (both listeners use `api.NewHTTPServer`) | `api.TestServerTimeouts` |
| ported relay and probe | `relay.TestRelayForwardsHTTPToUnixSocket`, `probe.Test*` (3 tests) |

`go list ./...` lists only `cmd/dermaga` and `internal/*`. `spikes/m0/` has been deleted.

## 2. Build

```
$ make build
GOARCH=amd64 go build -trimpath -ldflags='-s -w' -o bin/dermaga-linux-amd64 ./cmd/dermaga
GOARCH=arm64 go build -trimpath -ldflags='-s -w' -o bin/dermaga-linux-arm64 ./cmd/dermaga
12415136 bin/dermaga-linux-amd64   ELF 64-bit LSB executable, x86-64, statically linked, stripped
11731104 bin/dermaga-linux-arm64   ELF 64-bit LSB executable, ARM aarch64, statically linked, stripped
```

## 3. systemd user service on the VM

Installed by hand (the installer takes this over in M8):

```
$ scp bin/dermaga-linux-amd64 install/dermaga.service dermaga-vm:/tmp/
$ ssh dermaga-vm 'sudo install -m 0755 /tmp/dermaga-linux-amd64 /usr/local/bin/dermaga
    install -m 0644 /tmp/dermaga.service ~/.config/systemd/user/dermaga.service
    systemctl --user daemon-reload; systemctl --user enable --now dermaga.service'
● dermaga.service - dermaga daemon (single-user PaaS)
     Loaded: loaded (/home/server/.config/systemd/user/dermaga.service; enabled; preset: enabled)
     Active: active (running) since Sat 2026-11-07 02:46:34 UTC; 1s ago
   Main PID: 28160 (dermaga)
     Memory: 7M (peak: 7.5M)
```

Files, modes and listeners:

```
drwx------ server /run/user/1001/dermaga
srw------- server /run/user/1001/dermaga/dermaga.sock
drwx------ server /home/server/.config/dermaga
-rw------- server /home/server/.config/dermaga/master.key
drwx------ server /home/server/.local/share/dermaga
-rw------- /home/server/.local/share/dermaga/dermaga.db
-rw------- /home/server/.local/share/dermaga/dermaga.db-shm
-rw------- /home/server/.local/share/dermaga/dermaga.db-wal
$ ss -Hltnp | grep dermaga
LISTEN 0 4096 127.0.0.1:7070 0.0.0.0:* users:(("dermaga",pid=28160,fd=10))
$ journalctl --user -u dermaga -o cat
INFO started version=v0.0.0-20261107022912-18d515635b42+dirty panel=127.0.0.1:7070 socket=/run/user/1001/dermaga/dermaga.sock
```

The first run created `dermaga.db` as 0644 (inside the 0700 directory). Fixed: `store.Open` now
creates the file 0600, and SQLite copies that mode to `-wal`/`-shm` (output above is after the fix).

Root and a second instance are refused:

```
$ sudo /usr/local/bin/dermaga daemon
dermaga daemon: refusing to run as root: run dermaga as the single unprivileged user (systemctl --user)
exit=1
$ dermaga daemon        # while the service runs
dermaga daemon: /run/user/1001/dermaga/dermaga.sock: another dermaga daemon is running
exit=1
```

## 4. CLI: `status`, `doctor`, `panel`, `admin`

Before a password was set:

```
$ dermaga status
dermaga v0.0.0-20261107022912-18d515635b42+dirty (go1.27.3), pid 28160, up 8s, RSS 16.8 MB
admin password: NOT SET (dermaga admin set-password), TOTP: off
$ dermaga doctor
OK    lingering       enabled for server
OK    subuid          65536 ids
OK    subgid          65536 ids
OK    daemon          v0.0.0-20261107022912-18d515635b42+dirty, pid 28160, RSS 16.9 MB, socket /run/user/1001/dermaga/dermaga.sock (0600)
WARN  admin password  not set: dermaga admin set-password
WARN  totp            off: dermaga admin totp enable (strongly recommended)
OK    sockets         traefik-http.socket, traefik-https.socket active
OK    clock           NTP-synchronised
OK    disk            23% used, 15.0 GiB free (/home/server/.local/share/dermaga)
exit=0
```

```
$ dermaga panel --host vps.example
The panel listens on 127.0.0.1:7070 only. Reach it through an SSH tunnel.

Add this to ~/.ssh/config on your laptop (fix HostName if it isn't how you reach this server):

Host dermaga
    HostName vps.example
    Port 4222
    User server
    LocalForward 7070 127.0.0.1:7070
    SessionType none
    ExitOnForwardFailure yes

Then run "ssh -f dermaga" and open http://localhost:7070
(use exactly this URL: the panel rejects any other Host, and the local port must stay 7070).

Phone: in an SSH app with port forwarding (e.g. Termius), add a local forward from port 7070
to 127.0.0.1:7070 on this host, connect, then open http://localhost:7070 in the phone's browser.
```

(`Port 4222` is read from `/etc/ssh/sshd_config.d/00-vps-hardening.conf`.)

```
$ dermaga admin set-password < password-file        # stdin is not a terminal: one line, no repeat
New admin password: Admin password set. Existing panel sessions were logged out.
exit=0
$ echo short | dermaga admin set-password
dermaga admin: password must be 12 to 1024 characters
exit=1
```

## 5. Panel through the SSH tunnel (from the laptop)

```
## direct from laptop to 192.168.122.217:7070 (must fail)
curl: (28) Connection timed out after 4002 milliseconds
## tunnel with the `dermaga panel` snippet options
$ ssh -f -o SessionType=none -o ExitOnForwardFailure=yes -o LocalForward="7070 127.0.0.1:7070" dermaga-vm
ssh exit=0
## GET /
dermaga v0.0.0-20261107022912-18d515635b42+dirty: the web UI arrives in M7; the JSON API is under /api/.
## GET /api/status, no session -> 401:
401 {"error":"not logged in"}
## Host: evil.example -> 421:
421 {"error":"unexpected Host header"}
## Host: 127.0.0.1:7070 accepted (401 = past the Host check):
401 {"error":"not logged in"}
## POST login without Origin -> 403:
403 {"error":"missing or foreign Origin"}
## POST login, foreign Origin -> 403:
403 {"error":"missing or foreign Origin"}
## POST login, wrong password -> 401:
401 {"error":"invalid credentials"}
## POST login, right password -> 204 + cookie:
HTTP/1.1 204 No Content
Set-Cookie: dermaga_session=<redacted>; Path=/; Max-Age=604800; HttpOnly; SameSite=Strict
## GET /api/status with the session -> 200:
200 {"name":"dermaga","version":"v0.0.0-20261107022912-18d515635b42+dirty","goVersion":"go1.27.3","pid":28237,…,"adminPasswordSet":true,"totpEnabled":false}
## GET /api/audit?limit=3 via the panel:
200 [{"time":"2026-11-07T02:47:34Z","actor":"panel","action":"auth.login"},{"time":"2026-11-07T02:47:34Z","actor":"panel","action":"auth.login_failed"},{"time":"2026-11-07T02:47:16Z","actor":"cli","action":"admin.set_password"}]
## same log via the CLI on the VM:
$ dermaga audit --limit 5
TIME                 ACTOR  ACTION              TARGET  DETAILS
2026-11-07 02:47:34  panel  auth.login
2026-11-07 02:47:34  panel  auth.login_failed
2026-11-07 02:47:16  cli    admin.set_password
```

Logout and backoff (after the argon2 memory fix, §6):

```
## login -> 204
204
## logout -> 204
204
## old cookie after logout -> 401
401 {"error":"not logged in"}
## backoff: 6 wrong passwords
401 retry-after=
401 retry-after=
401 retry-after=
401 retry-after=
401 retry-after=
429 retry-after=1
## right password during the block -> 429
429 {"error":"too many failed logins, retry in 1s"}
## after the block (3 s later) -> 204
204
```

TOTP: `dermaga admin totp enable` driven by a script that computes the code from the printed
secret, then login through the tunnel:

```
Add this to your authenticator app (paste the URI, or enter the secret by hand):

  otpauth://totp/dermaga:server@dermaga-vm?algorithm=SHA1&digits=6&issuer=dermaga&period=30&secret=<secret>

  secret: <secret>
TOTP enabled. The panel now asks for a code at login.
exit=0

## password only, TOTP on -> 401
401 {"error":"invalid credentials"}
## password + wrong code -> 401
401 {"error":"invalid credentials"}
## password + current code -> 204
204
## same code again (replay) -> 401
401 {"error":"invalid credentials"}
## status with the TOTP session -> 200
200 {…,"adminPasswordSet":true,"totpEnabled":true}
## sessions before logout: 3
204
## sessions after logout: 2          # the row is deleted server-side; the other 2 are older live sessions
$ dermaga audit --limit 8
TIME                 ACTOR  ACTION             TARGET  DETAILS
2026-11-07 02:51:01  panel  auth.logout
2026-11-07 02:51:01  panel  auth.login_failed
2026-11-07 02:51:01  panel  auth.login
2026-11-07 02:51:01  panel  auth.login_failed
2026-11-07 02:51:01  panel  auth.login_failed
2026-11-07 02:50:29  cli    admin.totp_enable
2026-11-07 02:50:15  panel  auth.login
2026-11-07 02:50:12  panel  auth.login_failed
```

Afterwards TOTP was disabled again (`dermaga admin totp disable`), because its secret existed only
in the test script. The tunnel was closed.

## 6. Memory (BRIEF §9: idle RSS < 30 MB)

The first build kept argon2's 19 MiB work area after login:

```
## RSS after set-password + 2 logins, then every 30 s while idle
VmRSS:	   57352 kB
VmRSS:	   57352 kB
VmRSS:	   57352 kB
VmRSS:	   57352 kB
```

Fixed with `debug.FreeOSMemory()` after each argon2 computation (ARCHITECTURE D18):

```
after start:                                 RSS 16.7 MB
right after ~10 logins (incl. failures):     VmRSS: 16916 kB
after 60 s idle, then status/doctor:         VmRSS: 18660 kB
cgroup:                                      MemoryCurrent=9863168 MemoryPeak=29179904
```

## 7. Ported `hook-relay` and `probe` on rootless Podman

The relay runs as in M0 (bare rootfs, read-only, no capabilities). For this test it targets
`dermaga.sock`, because `hooks.sock` arrives in M3. The probe runs in the rootless netns and
resolves the container by name:

```
$ podman run -d --name dg-m1-relay --network web --read-only --cap-drop=ALL --security-opt=no-new-privileges \
    --pids-limit=32 --memory=32m -v $XDG_RUNTIME_DIR/dermaga/dermaga.sock:/run/dermaga/hooks.sock \
    --rootfs ~/m1-relayfs:O /dermaga hook-relay -listen :8080
$ podman unshare --rootless-netns /usr/local/bin/dermaga probe -url http://dg-m1-relay:8080/api/status -dns 10.89.0.1:53 -interval 300ms -timeout 10s
{"t":0,"ok":true,"status":200,"n":1}
{"t":0.301,"ok":true,"status":200,"n":2}
{"t":0.604,"ok":true,"status":200,"n":3}
exit=0
$ podman unshare --rootless-netns /usr/local/bin/dermaga probe -url http://dg-m1-relay:8080/api/nope -dns 10.89.0.1:53 -interval 1s -timeout 3s
{"t":0,"ok":false,"status":404,"n":0,"error":"status 404"}
{"t":1.001,"ok":false,"status":404,"n":0,"error":"status 404"}
{"t":2.002,"ok":false,"status":404,"n":0,"error":"status 404"}
dermaga probe: unhealthy: no 3 consecutive successes within 3s
exit=1
$ podman stats --no-stream --format "{{.Name}} mem={{.MemUsage}}" dg-m1-relay
dg-m1-relay mem=6.799MB / 33.55MB
```

The relay uses 6.8 MB (2.4 MB in M0), because the rootfs now holds the full 12 MB binary rather
than the 2 MB spike relay. The container and `~/m1-relayfs` were removed afterwards
(`podman ps -a` shows only `traefik`).

## 8. Final VM state

```
$ dermaga doctor
OK    lingering       enabled for server
OK    subuid          65536 ids
OK    subgid          65536 ids
OK    daemon          v0.0.0-20261107022912-18d515635b42+dirty, pid 28377, RSS 19.4 MB, socket /run/user/1001/dermaga/dermaga.sock (0600)
OK    admin password  set
WARN  totp            off: dermaga admin totp enable (strongly recommended)
OK    sockets         traefik-http.socket, traefik-https.socket active
OK    clock           NTP-synchronised
OK    disk            23% used, 15.0 GiB free (/home/server/.local/share/dermaga)
exit=0
```

`dermaga.service` stays installed and enabled. Set your own password with
`dermaga admin set-password` (the test password is random and unrecorded) and, optionally,
`dermaga admin totp enable` with your phone.

## 9. Reboot survival (checked before M2)

Run on 2026-11-07, after M1 was reported.

**First attempt: the VM powered off instead of rebooting.** `virsh domstate --reason` showed
`shut off (shutdown)`. The cause is in libvirt, not dermaga: both snapshots (`fresh`,
`vps-setup`) store a live domain config with `<on_reboot>destroy</on_reboot>`, left over from
virt-install's cloud-init first boot. A VM reverted to either snapshot therefore powers off on
every guest reboot. The persistent config has `restart`:

```
$ virsh --connect qemu:///system snapshot-dumpxml dermaga-vm vps-setup | grep on_reboot
    <on_reboot>destroy</on_reboot>        # live config saved in the snapshot
    <on_reboot>restart</on_reboot>        # persistent config
$ virsh --connect qemu:///system start dermaga-vm      # boots with the persistent config
Domain 'dermaga-vm' started
```

That cold boot brought `dermaga.service` up by itself, but `dermaga doctor`, run 6 s after
boot, failed:

```
FAIL  clock           not NTP-synchronised (TOTP and certificates need a correct clock): timedatectl status
exit=1
## 40 s later, systemd-timesyncd:
03:24:12 Contacted time server 103.134.77.146:123 (2.debian.pool.ntp.org).
03:24:12 Initial clock synchronization to Sat 2026-11-07 03:24:12.887613 UTC.
```

**Fix:** during the first 5 minutes after boot, an unsynchronised clock is a `WARN` ("not
NTP-synchronised yet (booted Ns ago)"). After that it is still a `FAIL`, so a clock that never
syncs is still reported (`cli.TestClockCheckToleratesEarlyBoot`).

**Second attempt, with the fixed binary and a real `sudo reboot`.** After the reboot I waited
for port 4222 with `nc -z` (that doesn't log in), then waited 35 s more before the first SSH
login. The journal then shows whether linger started the user manager:

```
$ ssh dermaga-vm '…; sudo reboot'
boot_id=ac28b23a-1740-432e-b4f8-3cf79cb23b85
## first login after the reboot:
boot_id=092b1f37-1187-4659-8ac0-c13d1af19d19
booted=2026-11-07 03:25:29
== ordering (monotonic seconds since boot)
[    5.168321] dermaga-vm systemd[1]: Starting user@1001.service - User Manager for UID 1001...
[    5.372337] dermaga-vm systemd[698]: Starting dermaga.service - dermaga daemon (single-user PaaS)...
[    5.385553] dermaga-vm systemd[698]: Started dermaga.service - dermaga daemon (single-user PaaS).
[    5.399444] dermaga-vm dermaga[740]: 2026/11/07 03:25:35 INFO started version=v0.0.0-20261107030203-3cff5df416d8+dirty panel=127.0.0.1:7070 socket=/run/user/1001/dermaga/dermaga.sock
[   54.870695] dermaga-vm sshd-session[950]: Accepted publickey for server from 192.168.122.1 port 59548 ssh2: ED25519 SHA256:V+KZKBpZYT+xiu/DXAsTjf2LTerWPN+DYUlih/Z1U9A
== loginctl
State=active
Linger=yes
1 1001 server - 698 manager - no -
2 1001 server - 950 user    - no -
== unit
NRestarts=0
ActiveState=active
SubState=running
UnitFileState=enabled
== sockets
drwx------ server:server /run/user/1001/dermaga
srw------- server:server /run/user/1001/dermaga/dermaga.sock
active
active
LISTEN 0      4096       127.0.0.1:7070 0.0.0.0:* users:(("dermaga",pid=740,fd=10))
$ dermaga doctor          # at the first login, 55 s after boot
OK  lingering       enabled for server
OK  subuid          65536 ids
OK  subgid          65536 ids
OK  daemon          v0.0.0-20261107030203-3cff5df416d8+dirty, pid 740, RSS 16.3 MB, socket /run/user/1001/dermaga/dermaga.sock (0600)
OK  admin password  set
OK  totp            enabled
OK  sockets         traefik-http.socket, traefik-https.socket active
OK  clock           NTP-synchronised
OK  disk            23% used, 15.0 GiB free (/home/server/.local/share/dermaga)
exit=0
```

The user manager (manager session 1, started by linger) and dermaga were up 5.4 s after boot,
49 s before the first login (session 2). No manual step was needed.

Traefik (vps-setup's unit) came back too, but only 95.6 s after boot: Quadlet's wait for the
system `network-online.target` timed out first. Found and fixed for dermaga's own units in M2
([`M2-EVIDENCE.md`](M2-EVIDENCE.md) §2b, ARCHITECTURE D29). The installer fixes Traefik's unit in M8.

**For future snapshot reverts:** the VM keeps the persistent `restart` behaviour until the next
revert. After a revert, run `virsh --connect qemu:///system shutdown dermaga-vm`, then
`virsh --connect qemu:///system start dermaga-vm` before any reboot test, or retake the
snapshots from a VM started with the persistent config.

## Not verified in M1

- arm64 runtime (cross-compiled only, as in M0).
