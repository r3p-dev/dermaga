# M0 evidence: spikes on Debian 13

All spikes ran on 2026-11-07 on the test VM `dermaga-vm` (libvirt, 2 vCPU, 4 GB RAM),
provisioned with `vps-setup.sh` 1.2.5. Everything ran as user `server` (uid 1001, rootless Podman)
unless a line says `sudo`. The output below is copied from the terminal, trimmed only where
marked `…`. Spike source code is in [`spikes/m0/`](https://github.com/r3p-dev/dermaga/tree/eb9ad64e0b9a81e08ab5e9bbf9def381d477099f/spikes/m0) at commit
`eb9ad64` (removed from the tree at the end of M1, after porting relay and probe into `internal/`).

## Environment

```
$ id; . /etc/os-release; echo $PRETTY_NAME; uname -rm; podman --version
uid=1001(server) gid=1001(server) groups=1001(server),4(adm),27(sudo),999(systemd-journal)
Debian GNU/Linux 13 (trixie)
6.12.115+deb13.1-cloud-amd64 x86_64
podman version 5.4.2
$ dpkg-query -W podman passt crun netavark aardvark-dns openssh-server restic
podman          5.4.2+ds1-2+b2
passt           0.0~git20250503.587980c-2+deb13u1
crun            1.21-1
netavark        1.14.0-2
aardvark-dns    1.14.0-3
openssh-server  1:10.0p1-7+deb13u4
restic          0.18.0-1+b4        (installed during spike 6; git candidate is 1:2.47.3-0+deb13u1)
$ podman ps --format '{{.Names}} {{.Image}}'
traefik docker.io/library/traefik:v3.7     # v3.7.17, from vps-setup.sh
```

Podman starts the rootless network namespace (it holds every bridge network) with this pasta
command line:

```
/usr/bin/pasta --config-net --pid …/rootless-netns-conn.pid --dns-forward 169.254.1.1 -t none -u none
  -T none -U none --no-map-gw --quiet --netns …/rootless-netns --map-guest-addr 169.254.1.2
$ podman network inspect web → 10.89.0.0/24 gw=10.89.0.1
$ podman exec traefik cat /etc/hosts | grep host.containers
169.254.1.2	host.containers.internal host.docker.internal
```

---

## 1. Traefik reaching the hook listener without new exposure

**Result: a Unix socket, plus a 2.4 MB relay container on `web`. No new TCP port on the host.**

### 1a. Host TCP listeners, as seen from the Traefik container

Two test HTTP servers ran on the host, one on `127.0.0.1:7071` and one on `0.0.0.0:7072`.

```
## loopback listener 127.0.0.1:7071 via host.containers.internal:
wget: can't connect to remote host (169.254.1.2): Connection refused
## loopback listener via gateway 10.89.0.1:7071:
wget: can't connect to remote host (10.89.0.1): Connection refused
## 0.0.0.0:7072 listener via host.containers.internal:
hello from host 0.0.0.0:7072 peer=192.168.122.217
## from laptop (outside the VM) to 192.168.122.217:7072:
curl: (28) Connection timed out after 4002 milliseconds      # nftables default-deny
```

Because of `--no-map-gw`, host loopback cannot be reached from containers, and that protects the
panel. `host.containers.internal` reaches only listeners bound to a non-loopback host address,
which on a VPS is the public interface. With that option, only nftables would keep the hook port
off the internet. **Rejected** (it widens exposure).

### 1b. pasta `-T` (forward one namespace port to host loopback)

```
# containers.conf: pasta_options = ["-T", "7071"]
$ podman unshare --rootless-netns ss -Hltn
LISTEN 0 128 127.0.0.1:7071 0.0.0.0:*        # bound to the netns loopback only
## Traefik -> gateway 10.89.0.1:7071: Connection refused

# containers.conf: pasta_options = ["-T", "0.0.0.0/7071"]
Error: setting up Pasta: pasta failed with exit code 1:
Invalid port specifier 0.0.0.0/7071                         # Traefik failed to start
```

**Rejected.** The `-T` listener exists only on the netns loopback, which containers can't reach,
and Debian's pasta rejects an address on `-T`. The config was reverted and Traefik restored.

### 1c. Unix socket and relay container (chosen)

The stand-in daemon listens on `$XDG_RUNTIME_DIR/dermaga/hooks.sock` (0600). A container on `web`
runs only the static relay binary ([`spikes/m0/relay`](https://github.com/r3p-dev/dermaga/blob/eb9ad64e0b9a81e08ab5e9bbf9def381d477099f/spikes/m0/relay/main.go), which becomes `dermaga hook-relay`) from a
bare rootfs directory, with no image, and forwards TCP :8080 to the bind-mounted socket.

```
$ podman run -d --name dg-hooks-spike --network web --read-only --cap-drop=ALL \
    --security-opt=no-new-privileges --pids-limit=32 --memory=32m \
    -v /run/user/1001/dermaga/hooks.sock:/run/dermaga/hooks.sock \
    --rootfs /home/server/spike/relayfs:O /relay -listen :8080 -target /run/dermaga/hooks.sock
## Traefik -> dg-hooks-spike:8080 (relay -> unix socket -> daemon):
daemon got GET /hooks/github/abc123 host=dg-hooks-spike:8080 xff=""
dg-hooks-spike mem=2.408MB / 33.55MB
```

End to end through Traefik's public :443, with a file-provider router
`Host(hooks.spike.test) && PathPrefix(/hooks/)`:

```
## POST via Traefik :443 to /hooks/github/abc123
daemon got POST /hooks/github/abc123 host=hooks.spike.test xff="127.0.0.1"
HTTP 200
## GET / on the hook host (must NOT reach the daemon)
404 page not found
## GET /api/... on the hook host
404 page not found
## response headers (secure@file chain applied):
HTTP/2 200
referrer-policy: strict-origin-when-cross-origin
strict-transport-security: max-age=31536000; includeSubDomains
x-content-type-options: nosniff
x-frame-options: SAMEORIGIN
## host listeners added by this design (expect none on 8080/7071):
0
```

The same relay also runs as a Quadlet unit with `Rootfs=` (see spike 4):
`daemon got GET /hooks/github/viaquadlet host=dg-hooks:8080`.

## 2. Daemon-side HTTP health probe into a container on `web`

**Result: `podman unshare --rootless-netns <probe>` works. Plan: re-exec `dermaga probe` once per deploy.**

```
candidate IP on web: 10.89.0.12
## from the host directly (expected to fail: bridge lives in the rootless netns)
curl: (28) Connection timed out after 2002 milliseconds
## podman unshare --rootless-netns curl
app-a
real	0m0.055s
## same, unhealthy path (404) -> non-zero exit
curl: (22) The requested URL returned error: 404
## by container name via aardvark DNS inside the netns
curl: option --dns-servers: the installed libcurl version does not support this
```

The Go probe ([`spikes/m0/probe`](https://github.com/r3p-dev/dermaga/blob/eb9ad64e0b9a81e08ab5e9bbf9def381d477099f/spikes/m0/probe/main.go)) runs inside the netns, resolves container names through
aardvark-dns at the network gateway, and requires K consecutive successes before a deadline:

```
$ podman unshare --rootless-netns ~/spike/probe -url http://spike-app-a:8080/ -dns 10.89.0.1:53 -interval 300ms
  0.00s ok 200 (1/3)
  0.30s ok 200 (2/3)
  0.60s ok 200 (3/3)
exit=0          real 0m0.650s
## unhealthy path, 3 s deadline
  0.00s fail: status 404
  1.00s fail: status 404
  2.00s fail: status 404
unhealthy: deadline exceeded
exit=1
## container that is not up yet (starts 2 s later) -> fails, then 3 consecutive ok
  2.00s fail: Get "http://spike-late:8080/": dial tcp: lookup spike-late: i/o timeout
  2.50s ok 200 (1/3)
  3.00s ok 200 (2/3)
  3.51s ok 200 (3/3)
exit=0
```

## 3. Traefik file-provider hot reload with atomic rename

**Result: reload works through rename in the bind-mounted directory. Latency is 2.04–2.12 s, with
zero non-200 responses during a switch.**

`flip.sh` writes `app-spike.yml` (temp file in the same directory, `sync`, `mv`) pointing at
backend A or B, then polls Traefik every 20 ms until the new body appears.

```
spike-app-b live after 1061 ms (requests=30, non-200=0)
spike-app-a live after 2058 ms (requests=58, non-200=0)
spike-app-b live after 2100 ms (requests=59, non-200=0)
spike-app-a live after 2055 ms (requests=58, non-200=0)
spike-app-b live after 2114 ms (requests=60, non-200=0)
spike-app-a live after 2044 ms (requests=58, non-200=0)
spike-app-b live after 2116 ms (requests=60, non-200=0)
spike-app-a live after 2064 ms (requests=58, non-200=0)
spike-app-b live after 2112 ms (requests=60, non-200=0)
spike-app-a live after 2065 ms (requests=59, non-200=0)
## mounts inside Traefik:
/etc/traefik/dynamic bind rw=false
```

(The first, 1061 ms, value landed partway through a throttle window.) The steady state is
Traefik's default `providersThrottleDuration` of 2 s plus about 100 ms.

**A broken file freezes all updates.** With an invalid `app-spike-broken.yml` next to it, a
change to `app-spike.yml` was **not** applied until the broken file was removed:

```
## with a broken sibling file, app-spike still served?   app-a 200
## change app-spike to app-b while broken file present:   app-a 200      ← change ignored
{"level":"error","providerName":"file","error":"collecting file configs: /etc/traefik/dynamic/app-spike-broken.yml: yaml: line 1: did not find expected ',' or ']'", …}
## after removing broken file:                            app-b 200
```

**JSON is accepted in a `.yml` file** (JSON is a subset of YAML), so dermaga can write
dynamic config with `encoding/json` alone:

```
$ cat app-jsonspike.yml   # {"http":{"routers":{"app-jsonspike":{"rule":"Host(`json.spike.test`)", … redirectRegex …}}}}
HTTP 301 -> https://example.org/some/path?q=1
errors in Traefik log: 0
```

### 3b. Verifying the switch through Traefik on an internal entrypoint (extra)

Brief §3.5.7 suggests `curl --resolve <domain>:443:127.0.0.1`. The default TLS options set
`sniStrict: true`, so that request fails whenever the domain has no certificate yet: on every
first deploy of a new domain, and always on this VM, which ACME cannot reach. Alternative tested:
a plain-HTTP entrypoint `verify` on `:8081` inside the Traefik container (not socket-activated,
not published). A router guarded by a per-deploy token proves that the new file is loaded *and*
that the request goes through the app's router and service:

```
# traefik.yml: entryPoints.verify.address ":8081"; app file:
#   app-demo         Host(`demo.example`)                                  entryPoints [websecure, verify]
#   app-demo-verify  Host(`demo.example`) && Header(`X-Dermaga-Verify`, `tok123`)   entryPoints [verify]
## verify entrypoint with token (from rootless netns):
$ podman unshare --rootless-netns curl -H "Host: demo.example" -H "X-Dermaga-Verify: tok123" http://10.89.0.2:8081/
candidate
 HTTP 200
## unknown host -> 404
## :8081 from the host:   curl: (7) Failed to connect to 127.0.0.1 port 8081   (0 host listeners)
## :8081 from the laptop: curl: (28) Connection timed out
## socket-activated :80/:443 still work:   http :80 -> 301, :443 held by the systemd-passed fd
```

Reverted afterwards. In this test the public router was also attached to `verify`. The approved
design (ARCHITECTURE D7) attaches **only** the token router to `verify`. The test request above
went through the token router, so the result holds unchanged.

## 4. Quadlet `Notify=healthy` and `quadlet -dryrun`

**Dry-run against a staged copy of the unit directory catches bad units:**

```
$ QUADLET_UNIT_DIRS=$STAGE /usr/libexec/podman/quadlet -dryrun -user
exit=1
converting "dg-spike-good.container": unsupported key 'Memory' in group 'Container' …
converting "dg-spike-invalid.container": unsupported key 'BogusKey' in group 'Container' …
converting "dg-spike-noimage.container": no Image or Rootfs key specified
```

`Memory=` doesn't exist in Podman 5.4.2's Quadlet; memory limits must go through
`PodmanArgs=--memory=…`, as in `vps-setup.sh`. A unit that fails conversion gets **no**
`.service` (`Unit dg-spike-good.service not found`). After the fix, dry-run passes and shows the
generated command:

```
exit=0
ExecStart=/usr/bin/podman run --name dg-hooks … --pids-limit 32 --network web --sdnotify=conmon -d
  --security-opt=no-new-privileges --cap-drop all --read-only -v %t/dermaga/hooks.sock:/run/dermaga/hooks.sock
  --memory=32m --rootfs %h/spike/relayfs:O /relay -listen :8080 -target /run/dermaga/hooks.sock
```

**`Notify=healthy` makes `systemctl start` wait for the healthcheck** (test app: healthy after about 6 s):

```
## start good unit: systemctl blocks until the healthcheck passes
exit=0 after 6554 ms
active running
health=healthy
```

**A unit that never becomes healthy is slow to fail and leaks its container:**

```
## start bad unit (HealthCmd=false, TimeoutStartSec=15)
Job for dg-spike-bad.service failed because a timeout was exceeded.
exit=1 after 105395 ms
timeout failed
container left: dg-spike-bad Exited (-1) Less than a second ago (unhealthy)

journal:
01:55:15.857 dg-spike-bad.service: start operation timed out. Terminating.
01:56:45.858 dg-spike-bad.service: State 'stop-sigterm' timed out. Killing.     ← default TimeoutStopSec=90s
01:56:46.188 Error: cannot remove container … as it could not be stopped
```

A `podman run --sdnotify=healthy` that is waiting ignores SIGTERM. With `TimeoutStopSec=20` and
`systemctl stop`, it still takes the full timeout and leaves the container behind
(`stop exit=0 after 20357 ms`, `dg-spike-bad Exited (-1)`). The fast, clean order is to remove the
container first, then stop the unit:

```
$ systemctl --user start --no-block dg-spike-bad.service    # returns 0 immediately; unit is "activating"
$ podman rm -f -t 5 dg-spike-bad                             # 5196 ms (busybox sh ignores SIGTERM → SIGKILL after 5 s)
$ systemctl --user stop dg-spike-bad.service                 # total 5247 ms
exit-code failed
no container left behind
```

## 5. Go binary: modernc.org/sqlite and an embedded SvelteKit build

[`spikes/m0/minibin`](https://github.com/r3p-dev/dermaga/tree/eb9ad64e0b9a81e08ab5e9bbf9def381d477099f/spikes/m0/minibin): Go 1.27.3, `modernc.org/sqlite` v1.61.1 (WAL), `net/http` with 1.22+
patterns, and an embedded SvelteKit 3.1.1 / Svelte 5.57 / Tailwind 4.3 / adapter-static 4.0.1 SPA
built with Bun 1.4.2 (144 KB, 17 files). `CGO_ENABLED=0 go build -trimpath -ldflags='-s -w'`.

```
minibin-amd64  11,169,952 bytes  ELF 64-bit LSB executable, x86-64, statically linked, stripped
minibin-arm64  10,682,528 bytes
```

Running on the VM as a transient systemd user service:

```
idle RSS after start: 12396 KB
2026/11/07 02:00:34 sqlite 3.54.1 journal_mode=wal
{"apps":1,"goVersion":"go1.27.3","rssKB":13744,"sqlite":"3.54.1 (wal)"}
/apps/portfolio -> 200 text/html; charset=utf-8              # SPA fallback for a client-side route
RSS right after 600 requests: 16976 KB
RSS after 60 s idle: 16976 KB
MemoryCurrent=8208384 MemoryPeak=8744960                     # cgroup view (anon + page cache charged)
```

Budget (§9): idle RSS < 30 MB. **Measured: 12.4 MB at idle, 17.0 MB after load.**

## 6. restic `--stdin-from-command` on Debian 13

```
$ restic version
restic 0.18.0 compiled with go1.24.4 on linux/amd64
$ restic backup --help | grep stdin-from-command
      --stdin-from-command   interpret arguments as command to execute and store its stdout
```

Tested against a local repository (no S3 bucket is available here; S3 is not tested), with
`postgres:18` holding a 100,000-row table:

```
## 1. success: pg_dump -Fc streamed into restic
$ restic backup -q --tag db:app --stdin-filename app.dump --stdin-from-command -- podman exec spike-pg pg_dump -Fc -U postgres app
restic exit=0
## 2. dump fails at start (unknown database)
subprocess podman: pg_dump: error: … FATAL:  database "nope" does not exist
Fatal: unable to save snapshot: failed to save /nope.dump: Fatal: command failed: exit status 1
restic exit=1
## 3. dump fails mid-stream (writes 5 MB then exits 3)
Fatal: unable to save snapshot: failed to save /mid.dump: Fatal: command failed: exit status 3
restic exit=1
## 4. dump killed by a signal
Fatal: unable to save snapshot: failed to save /kill.dump: Fatal: command failed: signal: killed
restic exit=1
## snapshots (only #1 must exist):
ID        Time                 Host        Tags    Size
442d4426  2026-11-07 02:02:56  dermaga-vm  db:app  209.331 KiB
1 snapshots
## restore check: dump is a valid pg custom archive
$ restic dump -q latest app.dump | podman exec -i spike-pg pg_restore --list | grep -c "TABLE DATA"
1
$ restic check
no errors were found
```

## 7. SSH local forwarding to `127.0.0.1:7070`

The effective sshd config produced by `vps-setup.sh` is unchanged:

```
$ sudo sshd -T -C user=server,host=localhost,addr=127.0.0.1 | grep …
port 4222
permitrootlogin no
passwordauthentication no
allowtcpforwarding local
allowstreamlocalforwarding no
gatewayports no
permittunnel no
allowagentforwarding no
x11forwarding no
allowusers server
```

From the laptop (the spike 5 binary is listening on the VM at `127.0.0.1:7070`):

```
## direct from laptop to VM:7070 (must fail)
curl: (28) Connection timed out after 4002 milliseconds
## tunnel with the snippet options
$ ssh -f -o SessionType=none -o ExitOnForwardFailure=yes -o LocalForward="7070 127.0.0.1:7070" dermaga-vm
$ curl http://localhost:7070/api/stats
{"apps":1,"goVersion":"go1.27.3","rssKB":18028,"sqlite":"3.54.1 (wal)"}
http://localhost:7070/apps/portfolio -> 200
## second tunnel on the same local port (ExitOnForwardFailure must make it exit non-zero)
bind [127.0.0.1]:7070: Address already in use
Could not request local forwarding.
exit=255
## remote forward -R (must be refused: AllowTcpForwarding local)
Error: remote port forwarding failed for listen port 9999
exit=255
## unix-socket forward to a dermaga socket (must be refused: AllowStreamLocalForwarding no)
curl: (56) Recv failure: Connection reset by peer
sshd: refused streamlocal port forward: originator  port 0, target /run/user/1001/dermaga/hooks.sock
```

## Cleanup

All spike containers, units, dynamic files, images and `~/spike` were removed afterwards.
`containers.conf` was restored, and Traefik is running unchanged. restic 0.18.0 stays installed
(dermaga's installer installs it anyway).
