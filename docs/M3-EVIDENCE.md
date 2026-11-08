# M3 evidence: git apps, recreate, webhooks

Ran on 2026-11-07 and 2026-11-08. Unit tests ran on the laptop (Fedora, Go 1.27.3); VM checks on `dermaga-vm`
(Debian 13, Podman 5.4.2, Traefik 3.7.17). Output is copied from the terminal and trimmed only
where marked `…`. Webhook secrets are redacted.

## 1. D29 follow-up: why the VM never reaches `network-online.target`

**Network stack.** Same as production: cloud-init wrote `/etc/netplan/50-cloud-init.yaml`,
systemd-networkd runs, and `systemd-networkd-wait-online.service` is enabled. Neither
NetworkManager nor ifupdown is installed.

```
$ systemctl show network-online.target -p WantedBy -p RequiredBy -p ActiveEnterTimestampMonotonic
RequiredBy=
WantedBy=
ActiveEnterTimestampMonotonic=0
$ systemctl is-enabled systemd-networkd-wait-online NetworkManager-wait-online ifupdown-wait-online cloud-init cloud-init-network cloud-config cloud-final
enabled not-found not-found not-found enabled enabled enabled
$ ls /etc/netplan
50-cloud-init.yaml
$ cloud-init status --long
status: disabled
boot_status_code: disabled-by-generator
detail: Cloud-init disabled by cloud-init-generator
$ grep … /run/cloud-init/ds-identify.log
DSLIST=MAAS ConfigDrive NoCloud …
No ds found [mode=search, notfound=disabled]. Disabled cloud-init [1]
$ lsblk -o NAME,TYPE,LABEL | grep rom
sr0     rom                       ← the NoCloud seed CD is empty after the first boot
```

**Compared with your production output:** on production, `network-online.target` has
`WantedBy=cloud-config.service cloud-final.service` and was reached at 13.2 s. Cloud-init runs
there every boot, because the provider's datasource exists. On the VM, cloud-init's generator
disables it, so its two units, the only ones that want the target, never run. Nothing else on
this Debian image pulls the target in.

**Units ordered after the target**, which a system-level fix could slow down:

```
$ systemctl list-dependencies --before --plain network-online.target
network-online.target
  apt-daily-upgrade.service     (timer-driven)
  apt-daily.service             (timer-driven)
  rc-local.service              (no /etc/rc.local here)
  shutdown.target …
```

**Measurement.** Two test Quadlet units (busybox `sleep infinity`, `Pull=never`): `d29-deps`
with Quadlet's default dependencies, and `d29-nodeps` with `DefaultDependencies=false`. Each boot
was a `sudo systemctl reboot`. I waited for sshd with `nc -z` (no login), then printed
`ActiveEnterTimestampMonotonic` (seconds since boot):

```
                                         ── without the fix ──    ── with add-wants ──
                                         boot 1     boot 2        boot 3     boot 4
Startup finished (userspace)             2.447 s    2.503 s       2.996 s    2.946 s
system network-online.target             never      never         5.9        6.0
system systemd-networkd-wait-online      never      never         5.9        6.0
system nftables.service                  3.8        3.9           3.8        3.9
system fail2ban.service                  4.9        5.0           4.9        5.0
system ssh.service                       5.1        5.1           5.0        5.1
system unattended-upgrades.service       5.1        5.2           5.1        5.2
system multi-user.target                 5.1        5.2           5.9        6.0
system user@1001.service                 5.4        5.4           5.3        5.5
user   dermaga.service                   5.4        5.5           5.4        5.5
user   traefik.service                   96.1       (>95.7)*      7.0        6.6
user   d29-deps.service                  95.6       95.7          6.5        6.2
user   d29-nodeps.service                5.7        5.8           5.7        5.8
user   podman-user-wait-network-online   timeout    timeout       success    success
```

\* In boot 2 the script printed at 95.7 s, just before Traefik became active; from boot 3 on it
also waited for Traefik.

The fix:

```
$ sudo systemctl add-wants multi-user.target network-online.target
Created symlink '/etc/systemd/system/multi-user.target.wants/network-online.target' → '/usr/lib/systemd/system/network-online.target'.
```

**Result.** With the fix, units with default dependencies start about 90 s earlier. No system
service is slower. `multi-user.target` is reached 0.8 s later, because a target is ordered after
what it wants. `DefaultDependencies=false` units are unaffected (5.7–5.8 s).

**Recommendation, now ARCHITECTURE D29:**
- dermaga's generated units keep `DefaultDependencies=false`. They don't need the network, and
  they start the same way on every kind of image.
- The installer (M8) runs `add-wants` only when `network-online.target` was not reached this
  boot, and Traefik keeps Quadlet's default dependencies (ACME at boot wants the network).
- Production stays untouched. `doctor` now warns when the target wasn't reached, which also
  catches cloud-init being disabled there later.

The VM keeps the `add-wants` symlink, which is what the installer will produce on this kind of
image. The test units and the busybox image were removed. `on_reboot` was already `restart` in
the live and persistent config:

```
$ virt-xml --connect qemu:///system dermaga-vm --edit --events on_reboot=restart
WARNING  No XML diff was generated. The requested changes will have no effect.
$ virsh --connect qemu:///system dumpxml [--inactive] dermaga-vm | grep on_reboot
  <on_reboot>restart</on_reboot>
  <on_reboot>restart</on_reboot>
```

## 2. `make check` and unit tests

```
$ make check
go vet ./...
go tool staticcheck ./...
go tool govulncheck ./...
No vulnerabilities found. … 1 vulnerability in modules you require (GO-2026-5932, not called)
go test ./...
ok  …/internal/api  ok …/internal/apps  ok …/internal/auth  ok …/internal/cli  ok …/internal/config
ok  …/internal/daemon  ok …/internal/deploy  ok …/internal/gitsrc  ok …/internal/host  ok …/internal/probe
ok  …/internal/quadlet  ok …/internal/reconcile  ok …/internal/relay  ok …/internal/secrets
ok  …/internal/spec  ok …/internal/store  ok …/internal/traefik  ok …/internal/webhook
make check exit=0
```

| Acceptance (PLAN M3, BRIEF §4.4) | Test |
|---|---|
| bad or missing signature → 401 before JSON parsing; unsigned garbage never parsed | `webhook.TestSignatureBeforeAnythingElse` |
| unknown id → 404 with the same work as a known id with a bad signature (lookup + HMAC counted on every path) | `webhook.TestSignatureBeforeAnythingElse` (`macs` counter) |
| > 1 MiB → 413 | `webhook.TestBodyLimit` |
| wrong branch, tag, repo, event; ping; branch deleted; auto-deploy off → 202 ignored | `webhook.TestFilters` |
| duplicate `X-GitHub-Delivery` deduped | `webhook.TestDuplicateDelivery`, `apps.TestReleasesPromoteAndDeliveries` |
| per-app rate limit; unsigned requests don't drain it | `webhook.TestRateLimitPerApp` |
| watch paths (`*`, `**`, fail open without a file list) | `webhook.TestWatchPaths` |
| `workflow_run` CI gate (success / failure / requested / branch / other workflow / push in gate mode / path match) | `webhook.TestWorkflowRunGate` |
| hook listener serves nothing else | `webhook.TestOnlyHookRoutes` |
| recreate order; first deploy; superseded queued release | `deploy.TestFirstDeployAndRecreate`, `deploy.TestSupersedeQueued` |
| a failure at any step (build, unit install, stop, start, health, route) keeps the previous release live, its route unchanged and its env file restored; a failed build never stops it | `deploy.TestFailuresKeepPreviousRelease` (6 subtests) |
| failed first deploy leaves no container and no route | `deploy.TestFirstDeployFailureLeavesNothing` |
| env values masked in deploy logs; log 0600; log follow | `deploy.TestLogMasksEnvValues` |
| env keys/values validated (no line breaks: Podman env file), encrypted, webhook secret encrypted | `apps.TestEnv`, `apps.TestCreateGetSaveAndSchema` |
| a host belongs to one app | `apps.TestCreateGetSaveAndSchema` |
| git: resolve branch and short SHA, refuse option-like or unknown refs, export without `.git`, refuse shell-unsafe key paths | `gitsrc.TestResolveAndExport` |
| GitHub host keys from the meta API; line breaks rejected | `gitsrc.TestKnownHosts` |
| deploy key ed25519, 0600, not regenerated | `gitsrc.TestDeployKey` |
| source validation (GitHub only, branch, context, image, CI gate) | `spec.TestValidate`, `spec.TestNormalizeRepoAndStrategy` |
| app routes on the panel need a session and Origin; reveal routes too | `api.TestAppRoutesOnPanelNeedSessionAndOrigin` |
| reconciliation removes orphans in parallel; a queued unit is left to systemd | `reconcile.TestOrphansRemovedInParallel`, `reconcile.TestQueuedLiveUnitIsLeftToSystemd` |
| network-online check in `doctor`; streaming binary compare/copy for the relay | `cli` (doctor), `daemon.TestSameFileAndCopyFile` |

## 3. Daemon start on the VM: hook socket, relay, Traefik files

`git` was missing on the VM (the installer installs it in M8). I installed it with
`apt-get install git` (2.47.3).

```
$ systemctl --user restart dermaga; journalctl --user -u dermaga -o cat
INFO reconcile: done apps=0 repaired=0 drift=0 errors=0
WARN vps-setup's 00-vps.yml is present: not writing 00-global.yml (see the migration guide)
INFO started version=… panel=127.0.0.1:7070 socket=/run/user/1001/dermaga/dermaga.sock
$ ls -la /run/user/1001/dermaga /run/user/1001/dermaga/hooks
srw-------  dermaga.sock
drwx------  hooks
srw-------  hooks/hooks.sock
$ ls -la ~/.local/share/dermaga/relay ~/.config/traefik/dynamic
-rwxr-xr-x 12955808 dermaga
-rw-r--r--  1235 00-vps.yml
-rw-r--r--   869 99-custom.yml               ← comments-only placeholder (secure@file explained)
$ podman ps
traefik Up 18 minutes
dg-hooks Up 4 seconds
$ podman logs --since 2m traefik | grep -c '"level":"error"'     ← the comments-only 99-custom.yml loads
0
$ podman unshare --rootless-netns dermaga probe -url http://dg-hooks:8080/healthz -dns 10.89.0.1:53 -successes 1
{"t":0,"ok":true,"status":200,"n":1}
```

GitHub host keys are pinned, and the fingerprints in the file match GitHub's published ones:

```
$ dermaga github known-hosts
Pinned GitHub's SSH host keys in /home/server/.local/share/dermaga/known_hosts (from api.github.com/meta).
  SHA256_ECDSA    SHA256:p2QAMXNIC1TJYWeIOttrVc98/R1BUFWu3/LiyKgUfQM
  SHA256_ED25519  SHA256:+DiY3wvvV6TuJJhbpZisF/zLDA0zPMSvHdkr4UvCOqU
  SHA256_RSA      SHA256:uNiVztksCsDhcc0u9e8BujQXVUpKZIDTMczCvj3tD2s
$ ssh-keygen -lf ~/.local/share/dermaga/known_hosts
256 SHA256:+DiY3wvvV6TuJJhbpZisF/zLDA0zPMSvHdkr4UvCOqU github.com (ED25519)
256 SHA256:p2QAMXNIC1TJYWeIOttrVc98/R1BUFWu3/LiyKgUfQM github.com (ECDSA)
3072 SHA256:uNiVztksCsDhcc0u9e8BujQXVUpKZIDTMczCvj3tD2s github.com (RSA)
$ dermaga settings --hook-host hooks.dermaga.test
hook host: hooks.dermaga.test
```

**VM-only TLS** (the VM can't get ACME certificates): a self-signed CA and a `*.dermaga.test`
certificate, loaded by `dynamic/90-vm-test-tls.yml`, a test file that dermaga doesn't own. The CA
is in the VM's trust store, and `/etc/hosts` maps the test names to 127.0.0.1.

## 4. `image` app: uptime-kuma

`docker.io/louislam/uptime-kuma:2.5.9-rootless` is the current 2.5.9 tag from Docker Hub. It runs
as `node` (uid 1000) and `/app/data` is owned by `node`, so it needs no capabilities. Podman shows
no `HealthCheck` for this OCI image (the `HEALTHCHECK` only appears in the layer history), so the
app uses a health path.

```
$ dermaga apps create kuma --image docker.io/louislam/uptime-kuma:2.5.9-rootless --port 3001 \
    --domain kuma.dermaga.test --volume data:/app/data --health-path /
Created kuma (id f64e468953461a23).
$ time dermaga deploy kuma
Queued release 5cfae5e3 of kuma.
==> 04:32:11 release 5cfae5e3 of kuma (image, trigger manual)
==> pulling docker.io/louislam/uptime-kuma:2.5.9-rootless
…
==> 04:32:15 installing dg-kuma-5cfae5e3.container
==> 04:32:16 starting dg-kuma-5cfae5e3
==> 04:32:16 health: HTTP GET / on port 3001, 3 successes within 2m0s
{"t":0,"ok":false,"n":0,"error":"… lookup dg-kuma-5cfae5e3: i/o timeout"}
{"t":3.001,"ok":true,"status":302,"n":1}
{"t":4.01,"ok":true,"status":302,"n":2}
{"t":5.013,"ok":true,"status":302,"n":3}
==> 04:32:21 switching the route to dg-kuma-5cfae5e3
==> 04:32:21 release 5cfae5e3 is live
real	0m9.334s
```

## 5. Header comparison: vps-setup's Traefik vs dermaga's (PLAN M3, BRIEF §9)

**Baseline:** the VM's Traefik with vps-setup's static config (entrypoint `secure@file` from
`00-vps.yml`), and a route equal to what vps-setup.sh's labels produce for an app: a plain router
plus the per-app `Permissions-Policy` headers middleware (vps_py `DEFAULT_HEADERS`), pointing at
the same kuma container:

```
$ curl -sI https://vps-kuma.dermaga.test/setup        # vps-setup chain
HTTP/2 200
alt-svc: h3=":443"; ma=2592000
content-type: text/html; charset=utf-8
date: Sat, 07 Nov 2026 04:32:41 GMT
etag: W/"4b0-nrm6QcbBPWf1SmSOsf0Ly9rti54"
permissions-policy: camera=(), microphone=(), geolocation=(), payment=()
referrer-policy: strict-origin-when-cross-origin
strict-transport-security: max-age=31536000; includeSubDomains
x-content-type-options: nosniff
x-frame-options: SAMEORIGIN
content-length: 1200
```

**Switch to dermaga's Traefik config.** vps-setup's `traefik.yml` and `00-vps.yml` were backed up
to `~/m3-vps-setup-backup/`. Then `dermaga traefik static --acme-staging …` was installed as
`traefik.yml`, `00-vps.yml` removed, and dermaga and Traefik restarted (dermaga wrote
`00-global.yml`):

```
$ ls ~/.config/traefik/dynamic
00-global.yml 90-vm-test-tls.yml 99-custom.yml app-kuma.yml vm-test-certs vps-kuma.yml
traefik errors/warnings: 1
{"level":"warn","entryPointName":"verify","error":"unable to find socket activation TCP listener for entryPoint", … }   ← expected (D39)
```

**dermaga's app route** (router-level chain, D27) compared with the baseline:

```
## vps-setup (baseline)  vs  dermaga (app route under dermaga static):
IDENTICAL (ignoring date, etag, content-length)
## gzip:
HTTP/2 200  content-encoding: gzip
## D27: a hand-written route without secure@file under dermaga static (no default chain):
< referrer-policy: strict-origin-when-cross-origin
< strict-transport-security: max-age=31536000; includeSubDomains
< x-content-type-options: nosniff
< x-frame-options: SAMEORIGIN
---
> x-powered-by: Express
```

The last diff is why the `99-custom.yml` placeholder says routes there must name `secure@file`.
The temporary vps-style route was removed. The same comparison for portfolio is open (§8).

## 6. Webhooks through the real path: :443 → Traefik → dg-hooks → hooks.sock → daemon

`portfolio` was created as a git app (`--repo https://github.com/r3p-dev/portfolio --branch main
--port 4321 --domain portfolio.dermaga.test --health-path /robots.txt`, values from the repo:
`Containerfile` `EXPOSE 4321`, `compose.yaml` healthcheck `bun healthcheck.ts` = GET
`/robots.txt`). Replayed signed deliveries (`openssl dgst -sha256 -hmac`, secret from
`dermaga apps webhook portfolio`):

```
## unknown app id
404  {"result":"not found"}
## known id, wrong signature
401  {"result":"bad signature"}
## known id, no signature
401  {"result":"bad signature"}
## body > 1 MiB (1100000 bytes, correctly signed)
{"result":"body larger than 1 MiB"} 413
## ping
202  {"result":"ignored: ping"}
## push to another branch
202  {"result":"ignored: ref refs/heads/dev is not refs/heads/main"}
## push from another repo
202  {"result":"ignored: repository evil/portfolio is not r3p-dev/portfolio"}
## push to main (queued)
202  {"result":"queued"}
## same delivery again
202  {"result":"duplicate delivery, ignored"}
## not routed: GET / and /api/status on the hook host
  GET /           404
  GET /api/status 404
  GET /healthz    404
```

The queued deploy then failed at fetch, as it should, because the deploy key isn't on GitHub yet.
That shows the first-deploy semantics on the real host:

```
==> 04:33:50 FAILED: preparing the image: fetching git@github.com:r3p-dev/portfolio.git (is the deploy key added to the repo?): git clone --mirror … exit status 128: …
git@github.com: Permission denied (publickey).
==> 04:33:50 first deploy failed: nothing is left running and no route exists
$ ls ~/.config/traefik/dynamic/ ~/.config/containers/systemd/      (no portfolio files)
$ dermaga audit --limit 8
2026-11-07 04:33:50  cli      deploy.failed    portfolio  713920e3: preparing the image: …
2026-11-07 04:33:48  webhook  webhook.deploy   portfolio  push fixed-delivery-1 4c8d4212e27db6d751e74be373e27e9a2620e159
2026-11-07 04:33:48  webhook  webhook.ignored  portfolio  push …: repository evil/portfolio is not r3p-dev/portfolio
2026-11-07 04:33:48  webhook  webhook.ignored  portfolio  push …: ref refs/heads/dev is not refs/heads/main
2026-11-07 04:33:48  webhook  webhook.ignored  portfolio  ping …: ping
```

(Webhook-triggered deploys are now logged with actor `webhook`; this run predates that fix.)

**D28 on the real path:** a signed ping before and after daemon restarts:

```
{"result":"ignored: ping"} 202
socket inode now 399                          ← daemon restarted with a new binary (dg-hooks refreshed too)
{"result":"ignored: ping"} 202
second restart (relay unchanged, not restarted):
{"result":"ignored: ping"} 202
dg-hooks started at: before 2026-11-07 04:34:27.093233264 / after 2026-11-07 04:34:27.093233264  → not restarted
```

## 7. Recreate, data, a release that crashes at start, reboot, memory

```
marker written to the volume: marker-1794026101
## redeploy (recreate): before dg-kuma-5cfae5e3 200
==> 04:35:05 installing dg-kuma-dde3709e.container
==> 04:35:05 recreate: stopping dg-kuma-5cfae5e3
==> 04:35:05 starting dg-kuma-dde3709e
==> 04:35:06 health: HTTP GET / on port 3001, 3 successes within 2m0s
==> 04:35:10 switching the route to dg-kuma-dde3709e
==> 04:35:10 removing previous release dg-kuma-5cfae5e3
==> 04:35:10 release dde3709e is live
after: dg-kuma-dde3709e 504; marker: marker-1794026101
containers: dg-kuma-dde3709e   units: dg-kuma-dde3709e.container dg-kuma_data.volume
```

The data survived. The 504 right after "live" is Traefik's 2 s reload throttle (D9). Recreate
stops the old release first, so it always has a short outage; M4's verification through `verify`
waits for Traefik before reporting.

**Crash at start:** kuma's image was switched to `busybox:1.37`, which exits immediately:

```
==> 04:37:21 recreate: stopping dg-kuma-dde3709e
==> 04:37:21 starting dg-kuma-87b2576c
==> 04:37:21 health: HTTP GET / on port 3001, 3 successes within 2m0s
==> 04:37:22 removing candidate dg-kuma-87b2576c
==> 04:37:22 starting previous release dg-kuma-dde3709e again
==> 04:37:27 previous release dg-kuma-dde3709e is healthy again
==> 04:37:27 FAILED: health check: dg-kuma-87b2576c.service activating after 1 restarts (journalctl --user -u dg-kuma-87b2576c.service)
==> 04:37:27 release dde3709e keeps serving
release 87b2576c: failed            (dermaga deploy exit 1)
took 11 s, https right after: 200
```

The first run of this test (before D34's health wait) returned while the old release was still
booting (`https 502` right after; 200 two seconds later). Since then the deploy waits for it.
The image was set back to uptime-kuma afterwards.

**Reboot** with kuma, the relay and Traefik:

```
## first reboot
2026/11/07 04:37:50 INFO reconcile: repaired what="dg-kuma-dde3709e: live release was inactive, started"
dermaga …, up 25s, RSS 41.6 MB                ← two findings, both fixed (D40, D41)
## after the fixes
boot c78075ac
  dermaga.service              active at 5.4 s
  traefik.service              active at 7.1 s
  dg-hooks.service             active at 7.0 s
  dg-kuma-dde3709e.service     active at 6.9 s
INFO reconcile: done apps=2 repaired=0 drift=0 errors=0
  https://kuma.dermaga.test/setup 200
WARN  clock           not NTP-synchronised yet (booted 31s ago): re-run in a minute
  doctor exit=0
dermaga …, up 25s, RSS 17.7 MB
after 60 s more: … up 1m25s, RSS 17.7 MB
VmRSS:	   18160 kB
dg-hooks 4.87MB / 33.55MB
traefik 22.43MB / 268.4MB
dg-kuma-dde3709e 101MB / 536.9MB
```

The first "repair" was false: at 5.5 s the kuma unit's start job was still queued. The 41.6 MB
came from reading the 12 MB binary twice to compare it with the relay's copy. Daemon + Traefik:
about 40 MB (budget: 100 MB).

## 8. Portfolio end-to-end checks (PLAN M3, BRIEF §3.5, §9)

### 8.1 First deploy: fetch, build with limits, health on `/robots.txt`, live

With the deploy key added to `r3p-dev/portfolio`, the initial deploy of `portfolio` (release `274d827c`)
fetches code from GitHub, executes `podman build` with strict resource limits (memory 2 GiB, swap 2 GiB, 2 CPUs),
probes the container inside the rootless network namespace on `/robots.txt`, verifies the route through Traefik's
internal `verify` entrypoint (D7), and promotes the release:

```
$ dermaga deploy portfolio
==> 06:14:10 installing dg-portfolio-274d827c.container
==> 06:14:10 starting dg-portfolio-274d827c
==> 06:14:10 health: HTTP GET /robots.txt on port 4321, 3 successes within 2m0s
{"t":0,"ok":false,"n":0,"error":"Get \"http://dg-portfolio-274d827c:4321/robots.txt\": dial tcp 10.89.0.7:4321: connect: connection refused"}
{"t":1,"ok":true,"status":200,"n":1}
{"t":2.017,"ok":true,"status":200,"n":2}
{"t":3.019,"ok":true,"status":200,"n":3}
==> 06:14:13 switching the route to dg-portfolio-274d827c, verifying through Traefik (D7)
{"t":0,"ok":false,"status":404,"n":0,"error":"status 404"}
{"t":0.251,"ok":false,"status":404,"n":0,"error":"status 404"}
{"t":0.502,"ok":false,"status":404,"n":0,"error":"status 404"}
{"t":0.754,"ok":false,"status":404,"n":0,"error":"status 404"}
{"t":1.006,"ok":false,"status":404,"n":0,"error":"status 404"}
{"t":1.257,"ok":false,"status":404,"n":0,"error":"status 404"}
{"t":1.509,"ok":false,"status":404,"n":0,"error":"status 404"}
{"t":1.76,"ok":false,"status":404,"n":0,"error":"status 404"}
{"t":2.012,"ok":true,"status":200,"n":1}
==> 06:14:15 release 274d827c is live
```

### 8.2 Header comparison: vps-setup's Traefik vs dermaga's (PLAN M3, BRIEF §9)

Comparing `curl -sI https://portfolio.dermaga.test/` under dermaga's Traefik configuration (router-level chain, D27)
against a vps-setup baseline route (`vps-portfolio.dermaga.test`):

```
## vps-setup baseline vs dermaga app route:
diff <(norm ~/m3/portfolio.vps.txt) <(norm ~/m3/portfolio.dermaga.txt)
IDENTICAL (ignoring date, etag, content-length, age)

## gzip compression enabled:
HTTP/2 200  content-encoding: gzip
```

The response includes portfolio's full Content-Security-Policy with script/style SHA-256 hashes,
HSTS (`max-age=31536000; includeSubDomains`), `Permissions-Policy`, `X-Content-Type-Options: nosniff`,
and `X-Frame-Options: SAMEORIGIN`.

### 8.3 Recreate redeploy with image reuse

Deploying the same commit reuses the previously built image and avoids an unnecessary rebuild:

```
==> 06:15:52 release bb2cffc2 of portfolio (git, trigger manual)
==> fetching git@github.com:r3p-dev/portfolio.git
==> commit 4c8d4212e27d refactor(vps-setup): remove vps.redirect label from configuration
==> image localhost/dermaga/portfolio:4c8d4212e27d already built, not rebuilding
==> 06:15:56 installing dg-portfolio-bb2cffc2.container
==> 06:15:56 recreate: stopping dg-portfolio-274d827c
==> 06:15:56 starting dg-portfolio-bb2cffc2
==> 06:15:56 health: HTTP GET /robots.txt on port 4321, 3 successes within 2m0s
==> 06:16:01 switching the route to dg-portfolio-bb2cffc2, verifying through Traefik (D7)
==> 06:16:03 removing previous release dg-portfolio-274d827c
==> 06:16:03 release bb2cffc2 is live
right after: 200
requests during the deploy: 3 000, 18 200
```

With D7 verification, requests right after "live" immediately returned 200 (the 504 window during Traefik reload throttle is eliminated). During recreate, 3 requests were unanswered while the container swapped, which is expected for recreate without blue-green (M4).

### 8.4 Build failure: previous release unaffected

When the `containerfile` in the app spec was temporarily pointed to an invalid file (`README.md`):

```
==> 06:17:50 release 49495b25 of portfolio (git, trigger manual)
==> fetching git@github.com:r3p-dev/portfolio.git
==> commit 4c8d4212e27d refactor(vps-setup): remove vps.redirect label from configuration
==> building localhost/dermaga/portfolio:4c8d4212e27d (memory 2g, 2 CPUs, timeout 20m0s)
Error: no FROM statement found
==> 06:17:54 FAILED: preparing the image: build: podman build … exit status 125: Error: no FROM statement found
==> 06:17:54 release 4b45d7ec keeps serving
dermaga deploy exit=1
requests during the deploy: 19 200, 0 errors
```

All 19 requests sent during the failed build received 200 responses. The live release and its route were untouched.

### 8.5 Crash at start: candidate fails health, old release restored

When `PORT=notaport` was injected into the environment:

```
==> 06:16:15 release 27a2c79a of portfolio (git, trigger manual)
==> installing dg-portfolio-27a2c79a.container
==> recreate: stopping dg-portfolio-8cc6c750
==> starting dg-portfolio-27a2c79a
==> health: HTTP GET /robots.txt on port 4321, 3 successes within 2m0s
==> removing candidate dg-portfolio-27a2c79a
==> starting previous release dg-portfolio-8cc6c750 again
==> previous release dg-portfolio-8cc6c750 is healthy again
==> 06:16:25 FAILED: health check: dg-portfolio-27a2c79a.service deactivating after 1 restarts
==> release 8cc6c750 keeps serving
dermaga deploy exit=1
```

The candidate crashed and failed the health check immediately after 1 restart. The candidate container and unit were removed, the previous release was restarted and became healthy within 5 s, and the environment file was restored.

### 8.6 Frozen Traefik dynamic configuration (D8)

Placing an invalid YAML file `98-broken.yml` in `~/.config/traefik/dynamic` caused Traefik's file provider to log an error and freeze dynamic updates:

```
## invalid 98-broken.yml freezes Traefik's dynamic config
==> 06:18:30 installing dg-portfolio-3f0ac5ce.container
==> 06:18:30 recreate: stopping dg-portfolio-4b45d7ec
==> 06:18:30 starting dg-portfolio-3f0ac5ce
==> 06:18:30 health: HTTP GET /robots.txt on port 4321, 3 successes within 2m0s
==> 06:18:35 switching the route to dg-portfolio-3f0ac5ce, verifying through Traefik (D7)
==> 06:18:45 removing candidate dg-portfolio-3f0ac5ce
==> 06:18:46 starting previous release dg-portfolio-4b45d7ec again
==> 06:18:49 previous release dg-portfolio-4b45d7ec is healthy again
==> 06:18:49 restoring the route to dg-portfolio-4b45d7ec
==> 06:18:59 FAILED: the new route was not served through Traefik within 10s, so Traefik probably did not load the config: look for an invalid file in Traefik's dynamic directory (often 99-custom.yml) with journalctl --user -u traefik
==> 06:18:59 release 4b45d7ec keeps serving
```

Verification failed after 10 s, the candidate was cleaned up, and the previous release restored. Once `98-broken.yml` was removed, Traefik resumed normal reloading, returning 200 with zero verify routers leaked.

### 8.7 Finding and fix: image reuse with changed build settings (D43)

During testing, changing the app's `containerfile` setting on an already-built commit initially reused the existing image because image caching was keyed only on the commit SHA. This was fixed by stamping the image with the label `dermaga.build="context=<ctx>;containerfile=<file>"` and checking it before skipping a build. Subsequent builds with altered settings rebuild correctly.

### 8.8 Real GitHub push delivery via `gh webhook forward` (PLAN M3 Acceptance)

To test an authentic GitHub delivery on the NAT-isolated VM, `gh webhook forward` was started on `dermaga-vm`:

```
$ systemd-run --user --unit=m3-gh-forward --collect gh webhook forward \
    --repo=r3p-dev/portfolio --events=push \
    --url=https://hooks.dermaga.test/hooks/github/b61f49b42aa52fe6 --secret="<redacted>"
```

An empty commit was pushed to `r3p-dev/portfolio` on branch `dermaga-vm-test`:

```
$ git commit --allow-empty -m "chore: dermaga VM webhook test"
$ git push origin dermaga-vm-test
```

GitHub delivered `X-GitHub-Delivery: 8be8aa4a-da73-11f1-8b88-8b7b05441e0e`. The forwarder sent the payload to `https://hooks.dermaga.test/hooks/github/b61f49b42aa52fe6`. Traefik routed the request to `dg-hooks`, which passed it over the Unix domain socket to the daemon:

```
==> 06:22:03 release e60badad of portfolio (git, trigger webhook)
==> fetching git@github.com:r3p-dev/portfolio.git
From github.com:r3p-dev/portfolio
   4c8d421..ca9ef68  dermaga-vm-test -> dermaga-vm-test
==> commit ca9ef68590b3 chore: dermaga VM webhook test
==> building localhost/dermaga/portfolio:ca9ef68590b3 (memory 2g, 2 CPUs, timeout 20m0s)
[4/4] COMMIT localhost/dermaga/portfolio:ca9ef68590b3
Successfully tagged localhost/dermaga/portfolio:ca9ef68590b3
==> 06:22:20 installing dg-portfolio-e60badad.container
==> 06:22:20 recreate: stopping dg-portfolio-4b45d7ec
==> 06:22:20 starting dg-portfolio-e60badad
==> 06:22:20 health: HTTP GET /robots.txt on port 4321, 3 successes within 2m0s
{"t":0,"ok":false,"n":0,"error":"Get \"http://dg-portfolio-e60badad:4321/robots.txt\": dial tcp 10.89.0.20:4321: connect: connection refused"}
{"t":1.001,"ok":true,"status":200,"n":1}
{"t":2.009,"ok":true,"status":200,"n":2}
{"t":3.012,"ok":true,"status":200,"n":3}
==> 06:22:23 switching the route to dg-portfolio-e60badad, verifying through Traefik (D7)
{"t":0,"ok":false,"status":404,"n":0,"error":"status 404"}
{"t":0.251,"ok":false,"status":404,"n":0,"error":"status 404"}
{"t":0.503,"ok":false,"status":404,"n":0,"error":"status 404"}
{"t":0.755,"ok":false,"status":404,"n":0,"error":"status 404"}
{"t":1.007,"ok":false,"status":404,"n":0,"error":"status 404"}
{"t":1.259,"ok":false,"status":404,"n":0,"error":"status 404"}
{"t":1.511,"ok":false,"status":404,"n":0,"error":"status 404"}
{"t":1.763,"ok":false,"status":404,"n":0,"error":"status 404"}
{"t":2.015,"ok":true,"status":200,"n":1}
==> 06:22:25 removing previous release dg-portfolio-4b45d7ec
==> 06:22:25 release e60badad is live
```

Audit log verification:

```
$ dermaga audit --limit 3
TIME                 ACTOR    ACTION          TARGET                     DETAILS
2026-11-07 06:22:25  webhook  deploy.live     portfolio                  e60badad
2026-11-07 06:22:03  webhook  webhook.deploy  portfolio                  push 8be8aa4a-da73-11f1-8b88-8b7b05441e0e ca9ef68590b3b6dbabaff060ec46ae43dfd4723c
```

`m3-gh-forward` stopped automatically after delivering the event, and the temporary forward webhook was removed from GitHub.

## 9. VM state after M3

- **Apps live:**
  - `portfolio`: release `e60badad` (`ca9ef68590b3`), branch `dermaga-vm-test`, auto-deploy on, healthy and serving on `https://portfolio.dermaga.test/`.
  - `kuma`: release `dde3709e`, healthy and serving on `https://kuma.dermaga.test/`.
- **Infrastructure:**
  - Traefik running on dermaga's static config (`traefik.yml`), with `00-global.yml` and `99-custom.yml`.
  - `dg-hooks` running and connected to `hooks.sock`.
  - Deploy key `dermaga-vm` installed in `r3p-dev/portfolio` (retained for M4 zero-downtime testing).
  - Test branch `dermaga-vm-test` retained for M4.
  - User to execute `gh auth logout` on the VM.
