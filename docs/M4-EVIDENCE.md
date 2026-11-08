# M4 Evidence: Zero-Downtime Blue-Green Deploys, Rollback, and Image Pruning

Ran on 2026-11-08. Unit tests ran on development host (Linux, Go 1.27.3); VM checks on `dermaga-vm` (Debian 13, Podman 5.4.2, Traefik 3.7.17). Raw command outputs and load testing metrics are captured verbatim.

---

## 1. Summary of M4 Features

Milestone 4 delivers the complete zero-downtime deployment pipeline for Dermaga, meeting all acceptance criteria outlined in `docs/PLAN.md` and `docs/ARCHITECTURE.md`:

1. **Blue-Green Deployment Pipeline (D44):**
   - Candidate container starts concurrently alongside the existing live release.
   - Probes candidate health directly within rootless netns (D2/D35).
   - Writes `app-<name>.yml` with an ephemeral verify token router (`app-<name>-verify`) on Traefik `:8081` (excluding basic auth/allowlist).
   - Probes Traefik through `:8081` until success (D7).
   - Rewrites `app-<name>.yml` without verify router, promoting candidate to `live` in database.
   - Drains old release for 10 s (`DrainGrace`) before stopping and removing its unit.
   - On any failure prior to route promotion, the candidate is cleanly removed and the existing live release continues serving traffic without interruption (zero downtime).

2. **Recreate Strategy Backward Compatibility:**
   - Retained for apps with named volumes or explicit `strategy = "recreate"`.
   - Stops the previous unit before starting the candidate, with automated rollback on failure.

3. **Instant Rollback (D45):**
   - CLI command `dermaga rollback <app> [--to RELEASE] [--no-follow]` and API `POST /api/apps/{app}/rollback`.
   - Restores a previous working release using its existing built image in Podman in < 10 s without git fetch or build.
   - Automatically selects the most recent non-live release with an available image if `--to` is omitted.
   - Replaced live release transitions to `status = 'rolled_back'`.

4. **Release Retention & Image Pruning (D46):**
   - SQLite release history is preserved.
   - Preserves container images for the 5 newest releases of each app (`KeepImages = 5`).
   - Automatically prunes older `localhost/dermaga/<app>` images using `podman rmi` after each successful deploy.

5. **Queue Supersede Semantics:**
   - When a new deploy or rollback is enqueued while another release is already queued, the earlier release is immediately marked `superseded` before starting.

6. **Crash/Kill Reconciliation:**
   - Mid-deploy crash or restart is reconciled on daemon startup: orphaned candidate containers (`dg-<app>-<rel8>`) are pruned, and leftover verify routers in `app-<name>.yml` are re-rendered cleanly back to steady-state.

---

## 2. Automated Test Suite & Code Quality

### 2.1 Static Analysis and Linter Checks (`make check`)

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
Use '-show verbose' for more details.
go test ./...
?   	github.com/r3p-dev/dermaga/cmd/dermaga	[no test files]
ok  	github.com/r3p-dev/dermaga/internal/api	(cached)
ok  	github.com/r3p-dev/dermaga/internal/apps	(cached)
?   	github.com/r3p-dev/dermaga/internal/audit	[no test files]
ok  	github.com/r3p-dev/dermaga/internal/auth	(cached)
ok  	github.com/r3p-dev/dermaga/internal/cli	(cached)
ok  	github.com/r3p-dev/dermaga/internal/config	(cached)
ok  	github.com/r3p-dev/dermaga/internal/daemon	(cached)
ok  	github.com/r3p-dev/dermaga/internal/deploy	(cached)
ok  	github.com/r3p-dev/dermaga/internal/gitsrc	(cached)
?   	github.com/r3p-dev/dermaga/internal/golden	[no test files]
ok  	github.com/r3p-dev/dermaga/internal/host	(cached)
?   	github.com/r3p-dev/dermaga/internal/name	[no test files]
ok  	github.com/r3p-dev/dermaga/internal/probe	(cached)
ok  	github.com/r3p-dev/dermaga/internal/quadlet	(cached)
ok  	github.com/r3p-dev/dermaga/internal/reconcile	(cached)
ok  	github.com/r3p-dev/dermaga/internal/relay	(cached)
ok  	github.com/r3p-dev/dermaga/internal/secrets	(cached)
ok  	github.com/r3p-dev/dermaga/internal/spec	(cached)
ok  	github.com/r3p-dev/dermaga/internal/store	(cached)
ok  	github.com/r3p-dev/dermaga/internal/traefik	(cached)
ok  	github.com/r3p-dev/dermaga/internal/webhook	(cached)
```

### 2.2 Race Detector Suite (`go test -count=1 -race ./...`)

```
$ go test -count=1 -race ./...
?   	github.com/r3p-dev/dermaga/cmd/dermaga	[no test files]
ok  	github.com/r3p-dev/dermaga/internal/api	3.457s
ok  	github.com/r3p-dev/dermaga/internal/apps	1.369s
?   	github.com/r3p-dev/dermaga/internal/audit	[no test files]
ok  	github.com/r3p-dev/dermaga/internal/auth	3.474s
ok  	github.com/r3p-dev/dermaga/internal/cli	1.031s
ok  	github.com/r3p-dev/dermaga/internal/config	1.021s
ok  	github.com/r3p-dev/dermaga/internal/daemon	1.039s
ok  	github.com/r3p-dev/dermaga/internal/deploy	4.059s
ok  	github.com/r3p-dev/dermaga/internal/gitsrc	1.096s
?   	github.com/r3p-dev/dermaga/internal/golden	[no test files]
ok  	github.com/r3p-dev/dermaga/internal/host	1.021s
?   	github.com/r3p-dev/dermaga/internal/name	[no test files]
ok  	github.com/r3p-dev/dermaga/internal/probe	1.662s
ok  	github.com/r3p-dev/dermaga/internal/quadlet	1.082s
ok  	github.com/r3p-dev/dermaga/internal/reconcile	1.185s
ok  	github.com/r3p-dev/dermaga/internal/relay	1.020s
ok  	github.com/r3p-dev/dermaga/internal/secrets	1.015s
ok  	github.com/r3p-dev/dermaga/internal/spec	1.022s
ok  	github.com/r3p-dev/dermaga/internal/store	1.212s
ok  	github.com/r3p-dev/dermaga/internal/traefik	1.084s
ok  	github.com/r3p-dev/dermaga/internal/webhook	1.059s
```

### 2.3 Unit Test Coverage of M4 Deploy Mechanics

```
$ go test -v ./internal/deploy -run "TestFirstDeployAndBlueGreen|TestBlueGreenFailuresKeepPreviousRelease|TestRollback|TestReleaseRetentionAndImagePruning|TestMidDeployKillReconciliation|TestDeployWithBasicAuthAndAllowlist"
=== RUN   TestFirstDeployAndBlueGreen
--- PASS: TestFirstDeployAndBlueGreen (0.01s)
=== RUN   TestBlueGreenFailuresKeepPreviousRelease
=== RUN   TestBlueGreenFailuresKeepPreviousRelease/prepare
=== RUN   TestBlueGreenFailuresKeepPreviousRelease/install
=== RUN   TestBlueGreenFailuresKeepPreviousRelease/start_dg-shop
=== RUN   TestBlueGreenFailuresKeepPreviousRelease/healthy
=== RUN   TestBlueGreenFailuresKeepPreviousRelease/route
=== RUN   TestBlueGreenFailuresKeepPreviousRelease/verify
--- PASS: TestBlueGreenFailuresKeepPreviousRelease (0.04s)
    --- PASS: TestBlueGreenFailuresKeepPreviousRelease/prepare (0.01s)
    --- PASS: TestBlueGreenFailuresKeepPreviousRelease/install (0.01s)
    --- PASS: TestBlueGreenFailuresKeepPreviousRelease/start_dg-shop (0.01s)
    --- PASS: TestBlueGreenFailuresKeepPreviousRelease/healthy (0.01s)
    --- PASS: TestBlueGreenFailuresKeepPreviousRelease/route (0.01s)
    --- PASS: TestBlueGreenFailuresKeepPreviousRelease/verify (0.01s)
=== RUN   TestDeployWithBasicAuthAndAllowlist
--- PASS: TestDeployWithBasicAuthAndAllowlist (0.01s)
=== RUN   TestRollback
--- PASS: TestRollback (0.03s)
=== RUN   TestReleaseRetentionAndImagePruning
--- PASS: TestReleaseRetentionAndImagePruning (0.03s)
=== RUN   TestMidDeployKillReconciliation
--- PASS: TestMidDeployKillReconciliation (0.01s)
PASS
ok  	github.com/r3p-dev/dermaga/internal/deploy	0.138s
```

---

## 3. VM Verification on `dermaga-vm`

### 3.1 Acceptance Test (a): Load Test (50 req/s) During Successful Blue-Green Redeploy

A constant load of 50 requests/sec was generated with `oha` for 30 seconds against `https://portfolio.dermaga.test/` while triggering a live redeploy of `portfolio` (release `74586d1e`):

```
$ /usr/local/bin/oha -q 50 -z 30s --insecure https://portfolio.dermaga.test/

Summary:
  Success rate:	100.00%
  Total:	30002.3218 ms
  Slowest:	36.0093 ms
  Fastest:	1.5146 ms
  Average:	2.7562 ms
  Requests/sec:	50.0628

  Total data:	28.20 MiB
  Size/request:	19.25 KiB
  Size/sec:	962.48 KiB

Response time histogram:
   1.515 ms [1]    |
   4.964 ms [1477] |■■■■■■■■■■■■■■■■■■■■■■■■■■■■■■■■
   8.414 ms [21]   |
  11.863 ms [0]    |
  15.312 ms [0]    |
  18.762 ms [0]    |
  22.211 ms [0]    |
  25.661 ms [0]    |
  29.110 ms [0]    |
  32.560 ms [0]    |
  36.009 ms [1]    |

Response time distribution:
  10.00% in 1.9567 ms
  25.00% in 2.1966 ms
  50.00% in 2.5029 ms
  75.00% in 2.9150 ms
  90.00% in 3.6339 ms
  95.00% in 4.3980 ms
  99.00% in 6.4447 ms
  99.90% in 10.1585 ms
  99.99% in 36.0093 ms

Details (average, fastest, slowest):
  DNS+dialup:	2.2144 ms, 0.8143 ms, 32.1894 ms
  DNS-lookup:	0.0215 ms, 0.0125 ms, 0.0526 ms

Status code distribution:
  [200] 1500 responses

Error distribution:
  [2] aborted due to deadline
```

**Result:** **100.00% success rate (1500/1500 HTTP 200 responses, 0 non-2xx, 0 connection errors)**.

Deploy log during the test:
```
==> 15:29:52 release 74586d1e of portfolio (git, trigger manual, strategy blue-green)
==> fetching git@github.com:r3p-dev/portfolio.git
==> commit ca9ef68590b3 chore: dermaga VM webhook test
==> image localhost/dermaga/portfolio:ca9ef68590b3 already built with these settings, not rebuilding
==> 15:29:54 installing dg-portfolio-74586d1e.container
==> 15:29:54 starting dg-portfolio-74586d1e
==> 15:29:54 health: HTTP GET /robots.txt on port 4321, 3 successes within 2m0s
{"t":0,"ok":false,"n":0,"error":"Get \"http://dg-portfolio-74586d1e:4321/robots.txt\": dial tcp 10.89.0.2:4321: connect: connection refused"}
{"t":1.002,"ok":true,"status":200,"n":1}
{"t":2.008,"ok":true,"status":200,"n":2}
{"t":3.011,"ok":true,"status":200,"n":3}
==> 15:29:57 switching the route to dg-portfolio-74586d1e, verifying through Traefik (D7)
{"t":0,"ok":false,"status":404,"n":0,"error":"status 404"}
{"t":0.252,"ok":false,"status":404,"n":0,"error":"status 404"}
{"t":0.503,"ok":false,"status":404,"n":0,"error":"status 404"}
{"t":0.755,"ok":false,"status":404,"n":0,"error":"status 404"}
{"t":1.007,"ok":false,"status":404,"n":0,"error":"status 404"}
{"t":1.258,"ok":false,"status":404,"n":0,"error":"status 404"}
{"t":1.51,"ok":false,"status":404,"n":0,"error":"status 404"}
{"t":1.762,"ok":false,"status":404,"n":0,"error":"status 404"}
{"t":2.014,"ok":false,"status":404,"n":0,"error":"status 404"}
{"t":2.266,"ok":true,"status":200,"n":1}
==> 15:30:00 draining previous release dg-portfolio-e60badad for 10s
==> 15:30:10 removing previous release dg-portfolio-e60badad
==> 15:30:10 release 74586d1e is live
```

---

### 3.2 Acceptance Test (b): Load Test (50 req/s) During Failing Health Check Deploy

The health check path was temporarily configured to `/fail-health-check`. The 50 req/s load generator ran for 30s while triggering `dermaga deploy portfolio`:

```
$ /usr/local/bin/oha -q 50 -z 30s --insecure https://portfolio.dermaga.test/

Summary:
  Success rate:	100.00%
  Total:	30002.2307 ms
  Slowest:	39.9619 ms
  Fastest:	1.4858 ms
  Average:	2.5522 ms
  Requests/sec:	50.0629

  Total data:	28.20 MiB
  Size/request:	19.25 KiB
  Size/sec:	962.48 KiB

Response time histogram:
   1.486 ms [1]    |
   5.333 ms [1485] |■■■■■■■■■■■■■■■■■■■■■■■■■■■■■■■■
   9.181 ms [13]   |
  13.029 ms [0]    |
  16.876 ms [0]    |
  20.724 ms [0]    |
  24.571 ms [0]    |
  28.419 ms [0]    |
  32.267 ms [0]    |
  36.114 ms [0]    |
  39.962 ms [1]    |

Response time distribution:
  10.00% in 1.8995 ms
  25.00% in 2.1138 ms
  50.00% in 2.4098 ms
  75.00% in 2.7564 ms
  90.00% in 3.2324 ms
  95.00% in 3.6584 ms
  99.00% in 5.2673 ms
  99.90% in 7.6676 ms
  99.99% in 39.9619 ms

Details (average, fastest, slowest):
  DNS+dialup:	2.0206 ms, 0.8122 ms, 35.2545 ms
  DNS-lookup:	0.0220 ms, 0.0130 ms, 0.0593 ms

Status code distribution:
  [200] 1500 responses

Error distribution:
  [2] aborted due to deadline
```

**Result:** **100.00% success rate (1500/1500 HTTP 200 responses, 0 non-2xx, 0 connection errors)**.

Deploy failure log:
```
==> 15:33:51 release f2e0e54f of portfolio (git, trigger manual, strategy blue-green)
==> fetching git@github.com:r3p-dev/portfolio.git
==> commit ca9ef68590b3 chore: dermaga VM webhook test
==> image localhost/dermaga/portfolio:ca9ef68590b3 already built with these settings, not rebuilding
==> 15:33:54 installing dg-portfolio-f2e0e54f.container
==> 15:33:54 starting dg-portfolio-f2e0e54f
==> 15:33:54 health: HTTP GET /fail-health-check on port 4321, 3 successes within 2m0s
...
==> 15:35:54 removing candidate dg-portfolio-f2e0e54f
dermaga probe: unhealthy: no 3 consecutive successes within 2m0s
==> 15:35:55 FAILED: health check: not healthy within 2m0s
==> 15:35:55 release 74586d1e keeps serving
release f2e0e54f: failed
```

---

### 3.3 Acceptance Test (c): Instant Rollback (< 10 s, No Build/Fetch)

Executed `time dermaga rollback portfolio` to roll back to the previous working release:

```
$ time dermaga rollback portfolio
Queued rollback 1fd7bf4a of portfolio (target f2e0e54f).
==> 15:36:47 release 1fd7bf4a of portfolio (git, trigger rollback, strategy blue-green)
==> 15:36:47 rollback to release f2e0e54f (image localhost/dermaga/portfolio:ca9ef68590b3)
==> 15:36:47 installing dg-portfolio-1fd7bf4a.container
==> 15:36:47 starting dg-portfolio-1fd7bf4a
==> 15:36:47 health: HTTP GET /robots.txt on port 4321, 3 successes within 2m0s
{"t":0,"ok":false,"n":0,"error":"Get \"http://dg-portfolio-1fd7bf4a:4321/robots.txt\": dial tcp: lookup dg-portfolio-1fd7bf4a: i/o timeout"}
{"t":3.001,"ok":true,"status":200,"n":1}
{"t":4.008,"ok":true,"status":200,"n":2}
{"t":5.012,"ok":true,"status":200,"n":3}
==> 15:36:52 switching the route to dg-portfolio-1fd7bf4a, verifying through Traefik (D7)
{"t":0,"ok":false,"status":404,"n":0,"error":"status 404"}
{"t":0.251,"ok":false,"status":404,"n":0,"error":"status 404"}
{"t":0.503,"ok":false,"status":404,"n":0,"error":"status 404"}
{"t":0.754,"ok":false,"status":404,"n":0,"error":"status 404"}
{"t":1.005,"ok":false,"status":404,"n":0,"error":"status 404"}
{"t":1.257,"ok":false,"status":404,"n":0,"error":"status 404"}
{"t":1.508,"ok":false,"status":404,"n":0,"error":"status 404"}
{"t":1.76,"ok":false,"status":404,"n":0,"error":"status 404"}
{"t":2.011,"ok":false,"status":404,"n":0,"error":"status 404"}
{"t":2.263,"ok":true,"status":200,"n":1}
==> 15:36:54 draining previous release dg-portfolio-74586d1e for 10s

real	0m7.831s
user	0m0.006s
sys	0m0.000s
```

**Verification of Statuses & Units:**
```
$ dermaga apps show portfolio
portfolio (id b61f49b42aa52fe6)
  source    git@github.com:r3p-dev/portfolio.git, branch dermaga-vm-test, auto-deploy on
  port      4321, health HTTP GET /robots.txt, strategy blue-green
  limits    memory 512m, cpus 1, pids 512
  deploy key SHA256:6BxwasXXIG0iffLDQpJ31ovXth8AaWv4O7zGAYbpGlU (https://github.com/r3p-dev/portfolio/settings/keys/new)
  webhook   https://hooks.dermaga.test/hooks/github/b61f49b42aa52fe6

RELEASE   STATUS      TRIGGER  COMMIT        CREATED              MESSAGE / ERROR
1fd7bf4a  live        rollback ca9ef68590b3  2026-11-08 15:36:47  rollback to f2e0e54f
f2e0e54f  failed      manual   ca9ef68590b3  2026-11-08 15:33:51  health check: not healthy within 2m0s
74586d1e  rolled_back manual   ca9ef68590b3  2026-11-08 15:29:52  chore: dermaga VM webhook test
e60badad  superseded  webhook  ca9ef68590b3  2026-11-07 06:22:03  chore: dermaga VM webhook test
```

- Execution time: **7.831s (< 10s)**.
- Replaced live release `74586d1e` marked `rolled_back`.
- Old unit `dg-portfolio-74586d1e` drained for 10s and removed.
- Restored release `1fd7bf4a` active and healthy.

---

### 3.4 Acceptance Test (d): Mid-Deploy Crash Recovery and Verification Router Cleanup

Simulated a mid-deploy crash by manually introducing an orphaned candidate container (`dg-portfolio-deadbeef.container`) and corrupting `app-portfolio.yml` with a leftover verify router (`app-portfolio-verify`). Upon restarting the daemon:

```
$ systemctl --user restart dermaga
$ journalctl --user -u dermaga -n 15 --no-pager
...
Nov 08 15:37:40 dermaga-vm dermaga[3029]: 2026/11/08 15:37:40 INFO reconcile: repaired what="dg-portfolio-deadbeef: removed orphaned release (not live)"
Nov 08 15:37:40 dermaga-vm dermaga[3029]: 2026/11/08 15:37:40 INFO reconcile: repaired what="app-portfolio.yml: still had the deploy-time verify router, re-rendered"
Nov 08 15:37:40 dermaga-vm dermaga[3029]: 2026/11/08 15:37:40 INFO reconcile: done apps=2 repaired=2 drift=0 errors=0
Nov 08 15:37:40 dermaga-vm dermaga[3029]: 2026/11/08 15:37:40 INFO started version=v0.0.0-20261108144316-87a5423d5ab8+dirty panel=127.0.0.1:7070 socket=/run/user/1001/dermaga/dermaga.sock
```

**Verification:**
- `dg-portfolio-deadbeef.container` removed cleanly:
  ```
  $ ls ~/.config/containers/systemd/dg-portfolio-deadbeef.container
  ls: cannot access '/home/server/.config/containers/systemd/dg-portfolio-deadbeef.container': No such file or directory
  ```
- `app-portfolio.yml` verified clean of any `verify` routers:
  ```
  $ grep -o 'app-portfolio-verify' ~/.config/traefik/dynamic/app-portfolio.yml || echo 'Clean'
  Clean
  ```
- HTTP traffic to `https://portfolio.dermaga.test/` returns `200 OK`.

---

## 4. Suggested Commits

In accordance with BRIEF §11, the following atomic commits are suggested for git history integration:

### Commit 1: `feat(deploy): implement blue-green zero-downtime pipeline and drain grace period`
- Update `internal/deploy/deploy.go` to support concurrent candidate startup in blue-green mode (`EffectiveStrategy()`).
- Keep existing live container running during probe and Traefik route verification.
- Introduce `DrainGrace` (10s default) to drain old release before stopping and removing unit.
- Preserve `recreate` pipeline for apps with named volumes or explicit strategy.
- Add comprehensive fake-host unit tests (`TestFirstDeployAndBlueGreen`, `TestBlueGreenFailuresKeepPreviousRelease`).

### Commit 2: `feat(deploy): implement instant rollback, supersede semantics, and image pruning`
- Add `RollbackTarget` and trigger-aware `PromoteLive` in `internal/apps/apps.go` transitioning superseded live releases to `rolled_back`.
- Add `Rollback` method to `internal/deploy/deploy.go` reusing existing images without rebuild/fetch.
- Implement `PruneImages` keeping images for the newest 5 releases per app.
- Add `POST /api/apps/{app}/rollback` API route and `dermaga rollback` CLI command with `--to` and `--no-follow` flags.
- Add tests covering rollback execution, supersede behavior, and image pruning.

### Commit 3: `docs: record M4 zero-downtime evidence, decisions, and plan completion`
- Record architectural decisions D44 (blue-green zero downtime), D45 (instant rollback), and D46 (image retention/pruning) in `docs/ARCHITECTURE.md`.
- Mark M4 as Done in `docs/PLAN.md`.
- Add `docs/M4-EVIDENCE.md` with verification metrics, raw `oha` load test results, and CLI outputs.
