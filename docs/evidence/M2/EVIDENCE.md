# Evidence: Milestone M2 (0.2.0) — Generators and Reconciliation

This document records the verification evidence for milestone **M2 (0.2.0)** of **Dermaga**: Quadlet unit generator, Traefik dynamic and static file generator, dry-run validations on Debian 13 VM with Podman 5.4.2 and Traefik v3.7.19, negative controls, systemd specifier injection hardening, and DEP-10 startup reconciliation.

All VM verifications were executed on `dermaga-vm` (`Debian GNU/Linux 13 (trixie)`, Podman 5.4.2, Traefik v3.7.19).

---

## 1. Summary of Acceptance Criteria

| Criteria | Status | Details |
| -------- | ------ | ------- |
| **Golden tests for every generator** | **PASS** | Golden tests in `internal/quadlet/testdata/` (minimal app, custom limits, readonly root, added capabilities, env file, health cmd, url-escaped-health, network unit) and `internal/traefik/testdata/` (static config, global dynamic, minimal app, middleware toggles, aliases + www, path prefix with/without strip, basic auth, allowlist, body size, custom/removed headers, switch verify). |
| **SEC-16–SEC-19 (Injection Hardening & Specifiers)** | **PASS** | `internal/quadlet/ini.go` escapes `%` to `%%` in `HealthCmd` (APP-5 free text) to prevent systemd specifier expansion (`%h`, `%u`, `%20`), and rejects trailing backslash (`\`) to prevent line continuation swallowing directives. All user-controlled fields reject newlines, null bytes, backticks, dollar expansions, quotes, and traversal sequences. Documented in `DEC-15`. |
| **Quadlet Dry-Run on VM with Negative Control** | **PASS** | Verified on `dermaga-vm` via `/usr/lib/systemd/system-generators/podman-system-generator --dryrun --user` with exit code 0. Specifier resolution verified with `systemd-analyze --user verify`. Negative control with unsupported key `ThisIsAnUnsupportedKey` rejected with non-zero exit code 1. |
| **Traefik Isolated Verification on VM with Negative Control** | **PASS** | Verified in an isolated throwaway Traefik container pinned to `docker.io/library/traefik:v3.7@sha256:44b61e76a251fe586d17d881fc900452d0a7b4a4b08c97ce6eac86d45b4c8b9d` on non-colliding ports (`18080`, `18443`, `18088`). All 13 dynamic routers enabled with 0 errors via API polling. Real curl requests assert: Host routing, path prefix with/without strip, 301 redirects, basic auth (401/200), IP allowlist (403), body size limit (200/413), default/custom headers, and switch verify token. Negative control flags broken router with error. Live Traefik directory untouched. |
| **REL-8 Default Security Headers** | **PASS** | `TestDefaultHeadersMatchVPSSetup` compares generator defaults against `internal/traefik/testdata/vps-setup-headers.golden` extracted directly from `vps-setup.sh` (commit `8fe4cb1aa647a7d7c44710a72bc85000e4d80ef6`). All 5 headers match field-by-field. |
| **ACME on Every Router** | **PASS** | Every router (main, aliases, redirects, and switch verify) declares `tls.certResolver: "le"`. |
| **Verify Router in Switch Rendering Only** | **PASS** | Router `app-<name>-verify` with `X-Dermaga-Verify` header and candidate service is rendered only when `Switch` spec is provided, and omitted in standard live configs. |
| **DEP-10 Startup Reconciliation** | **PASS** | `internal/reconciler` repairs system on start: orphan removed, missing file re-rendered, stopped live unit started, leftover verify router removed, foreign files untouched. Verified via unit tests with `FakeHost` (`reconciler_test.go`) and real-host scenario script on `dermaga-vm` (`test/vm/test_reconciliation.sh`) with negative control on inaccessible database. |

---

## 2. Quadlet Generator & Dry-Run Validation on VM

Verified with `test/vm/test_quadlet_dryrun.sh`.

Log file: [`docs/evidence/M2/logs/vm_quadlet_dryrun.log`](logs/vm_quadlet_dryrun.log)

```text
=== Quadlet Generator Dry-Run Verification on dermaga-vm ===
Timestamp: 2026-11-17T05:36:15+07:00
Git commit: 46b94b5
Working tree status: dirty
Host: dermaga-vm

Created remote directory: /tmp/quadlet-verify-bUSQoC
Staging caps-added -> app-caps-added.container
Staging custom-limits -> app-custom-limits.container
Staging env-file -> app-env-file.container
Staging health-cmd -> app-health-cmd.container
Staging minimal -> app-minimal.container
Staging network-unit -> web.network
Staging readonly-root -> app-readonly-root.container
Staging url-escaped-health -> app-url-escaped-health.container

Executing /usr/lib/systemd/system-generators/podman-system-generator --dryrun --user...
quadlet-generator[15619]: Loading source unit file /tmp/quadlet-verify-bUSQoC/app-caps-added.container
quadlet-generator[15619]: Loading source unit file /tmp/quadlet-verify-bUSQoC/app-custom-limits.container
quadlet-generator[15619]: Loading source unit file /tmp/quadlet-verify-bUSQoC/app-env-file.container
quadlet-generator[15619]: Loading source unit file /tmp/quadlet-verify-bUSQoC/app-health-cmd.container
quadlet-generator[15619]: Loading source unit file /tmp/quadlet-verify-bUSQoC/app-minimal.container
quadlet-generator[15619]: Loading source unit file /tmp/quadlet-verify-bUSQoC/app-readonly-root.container
quadlet-generator[15619]: Loading source unit file /tmp/quadlet-verify-bUSQoC/app-url-escaped-health.container
quadlet-generator[15619]: Loading source unit file /tmp/quadlet-verify-bUSQoC/web.network

Verifying systemd unit specifier resolution for url-escaped-health...
Specifier resolution passed: systemd-analyze reported no errors on escaped %20.

--- Negative Control: Deliberately Broken Quadlet Unit ---
Running podman-system-generator against deliberately broken unit...
quadlet-generator[15669]: Loading source unit file /tmp/quadlet-neg-dm7gd9/broken.container
quadlet-generator[15669]: converting "broken.container": unsupported key 'ThisIsAnUnsupportedKey' in group 'Container' in /tmp/quadlet-neg-dm7gd9/broken.container
Negative control successfully rejected as expected (exit code: 1).

=== Quadlet Dry-Run Verification: PASS ===
```

---

## 3. Traefik Generator & Isolated Verification on VM

Verified with `test/vm/test_traefik_configs.sh` running in a throwaway container on `dermaga-vm`. Never touches live Traefik's dynamic directory (§8: idempotent, zero residue).

Log file: [`docs/evidence/M2/logs/vm_traefik_configs.log`](logs/vm_traefik_configs.log)

```text
=== Traefik Golden Configurations Verification on dermaga-vm ===
Timestamp: 2026-11-17T05:35:52+07:00
Git commit: 46b94b5
Working tree status: dirty
Host: dermaga-vm

Discovered live Traefik container image: docker.io/library/traefik:v3.7
Discovered live Traefik image digest: sha256:44b61e76a251fe586d17d881fc900452d0a7b4a4b08c97ce6eac86d45b4c8b9d
Using pinned Traefik image: docker.io/library/traefik:v3.7@sha256:44b61e76a251fe586d17d881fc900452d0a7b4a4b08c97ce6eac86d45b4c8b9d

Creating isolated test directory: /tmp/traefik-verify-lX3zr1
Staging dynamic golden configuration files...
  Staging aliases-www.golden -> aliases-www.yml
  Staging allowlist.golden -> allowlist.yml
  Staging basic-auth.golden -> basic-auth.yml
  Staging body-size.golden -> body-size.yml
  Staging custom-removed-headers.golden -> custom-removed-headers.yml
  Staging global-dynamic.golden -> global-dynamic.yml
  Staging middleware-toggles.golden -> middleware-toggles.yml
  Staging minimal.golden -> minimal.yml
  Staging path-prefix.golden -> path-prefix.yml
  Staging path-prefix-no-strip.golden -> path-prefix-no-strip.yml
  Staging switch-verify.golden -> switch-verify.yml
Creating isolated bridge network test-traefik-net-628221...
Starting throwaway backend echo responders on test-traefik-net-628221...
Starting throwaway Traefik container test-traefik-628221...

Polling Traefik API (http://127.0.0.1:18088/api/http/routers) until dynamic configs load...
Traefik loaded all dynamic routers after 200 ms.

--- Asserting API Router Status ---
Router app-portfolio@file: status=enabled, errors=0 -> OK
Router app-landing@file: status=enabled, errors=0 -> OK
Router app-landing-redirect-2@file: status=enabled, errors=0 -> OK
Router app-landing-redirect-3@file: status=enabled, errors=0 -> OK
Router app-secure-admin@file: status=enabled, errors=0 -> OK
Router app-internal-portal@file: status=enabled, errors=0 -> OK
Router app-uploader@file: status=enabled, errors=0 -> OK
Router app-custom-headers-app@file: status=enabled, errors=0 -> OK
Router app-blog@file: status=enabled, errors=0 -> OK
Router app-api-service@file: status=enabled, errors=0 -> OK
Router app-docs-service@file: status=enabled, errors=0 -> OK
Router app-web-app@file: status=enabled, errors=0 -> OK
Router app-web-app-verify@file: status=enabled, errors=0 -> OK

--- Asserting Real HTTP Behavior via curl ---
1. Routing by Host (portfolio.example.com): PASS (HTTP 200 OK)
2. Path prefix with strip (/api/v1/users -> backend sees /users): PASS (X-Echo-Path: /users received)
3. Path prefix without strip (/docs/guide -> backend sees /docs/guide): PASS (X-Echo-Path: /docs/guide received)
4. Alias www redirect (www.landing.com/about -> 301 to https://landing.com/about): PASS (301 https://landing.com/about)
5a. Basic Auth without credentials: PASS (HTTP 401 Unauthorized)
5b. Basic Auth with valid credentials (admin:secret): PASS (HTTP 200 OK)
6. IP Allowlist rejection from non-allowed client (127.0.0.1): PASS (HTTP 403 Forbidden)
7a. Request buffering with allowed body size (1 KiB <= 10 MiB): PASS (HTTP 200 OK)
7b. Request buffering exceeding max request body size (11 MiB > 10 MiB limit): PASS (HTTP 413 Payload Too Large)
8a. Default security headers verification (portfolio.example.com): PASS (all 5 default security headers present, Server and X-Powered-By stripped)
8b. Custom and removed headers verification (headers.example.com): PASS (CSP and custom header added, X-Frame-Options successfully removed)
9a. Standard request routes to production release 1: PASS (production release 1 served)
9b. Request with X-Dermaga-Verify token routes to candidate release 2: PASS (candidate release 2 served)

--- Negative Control: Deliberately Broken Traefik Configuration ---
Polling API for broken router error detection...
Negative control router successfully flagged with error:
{"entryPoints":["websecure"],"middlewares":["nonexistent-middleware@file"],"service":"nonexistent-service","rule":"Host(`broken.example.com`)","priority":26,"tls":{"options":"default","certResolver":"le"},"observability":{"accessLogs":true,"metrics":true,"tracing":true,"traceVerbosity":"minimal"},"error":["the service \"nonexistent-service@file\" does not exist"],"status":"disabled","using":["websecure"],"name":"app-broken-router@file","provider":"file","priorityStr":"26"}
Negative control PASSED: router referencing missing middleware was flagged by Traefik API.

=== All Traefik Golden Configurations Verified: PASS ===

Cleaning up throwaway test containers and network on dermaga-vm...
Cleanup complete (§8: idempotent, no residue).
```

---

## 4. DEP-10 Startup Reconciliation on VM

Verified with `test/vm/test_reconciliation.sh` running against dermaga's directories on `dermaga-vm`.

Log file: [`docs/evidence/M2/logs/vm_reconciliation.log`](logs/vm_reconciliation.log)

```text
=== DEP-10 Reconciliation Verification on dermaga-vm ===
Timestamp: 2026-11-17T05:54:28+07:00
Git commit: 46b94b5
Working tree status: dirty
Host: dermaga-vm

Building dermaga binary for remote VM...
Build complete.
Created remote test directory: /tmp/reconcile-test-qXely6
Seeding remote SQLite database (/tmp/reconcile-test-qXely6/dermaga.db)...
Preparing local test image localhost/demo:r1 on dermaga-vm...
Staging scenario files in dermaga's directories...

--- Running dermaga reconcile on dermaga-vm ---
=== Reconciliation Report (DEP-10) ===
Orphans removed:        2 [app-orphan-r1.container app-orphan.yml]
Files re-rendered:      2 [app-demo-r1.container app-demo.yml]
Units started:          1 [app-demo-r1.service]
Verify routers cleaned: 1 [app-demo-verify.yml]
Foreign files reported: 4 [custom-vpn.container traefik-acme.volume traefik.container custom-metrics.yml]

--- Asserting Acceptance Cases ---
1. Orphan files removed: PASS (app-orphan-r1.container and app-orphan.yml deleted)
2a. Missing Quadlet infrastructure verified (web.network): PASS (web.network present)
2b. Missing Quadlet app container re-rendered (app-demo-r1.container): PASS (app-demo-r1.container re-rendered)
2c. Missing Traefik global dynamic config verified (00-global.yml): PASS (00-global.yml present)
2d. Missing Traefik app dynamic config re-rendered (app-demo.yml): PASS (app-demo.yml re-rendered)
3. Live unit started by systemd: PASS (app-demo-r1.service is active)
4. Leftover verify router removed (app-demo-verify.yml): PASS (app-demo-verify.yml deleted)
5a. Foreign Quadlet file untouched (custom-vpn.container): PASS (custom-vpn.container untouched)
5b. Foreign Traefik file untouched (custom-metrics.yml): PASS (custom-metrics.yml untouched)
5c. Existing system units untouched (traefik.container, traefik-acme.volume): PASS (system units preserved)
5d. Foreign files reported in output: PASS (All foreign files accurately reported)

--- Negative Control: Deliberately Broken Inaccessible Database ---
Negative control execution output:
Error: opening database: creating database directory: mkdir /nonexistent: permission denied
Negative control PASSED: reconciler failed with non-zero exit on inaccessible database.

=== All DEP-10 Reconciliation Acceptance Cases Verified: PASS ===

Cleaning up reconciliation test environment on dermaga-vm...
Untagged: localhost/demo:r1
Cleanup complete (§8: idempotent, no residue).
```

---

## 5. Golden Tests Inventory

Golden test files committed in `internal/quadlet/testdata/` and `internal/traefik/testdata/`:

| Package | Test Name | File | Description |
| ------- | --------- | ---- | ----------- |
| `quadlet` | `minimal` | `minimal.golden` | Minimal app container with default cgroups, drops all caps, default dependencies false |
| `quadlet` | `custom-limits` | `custom-limits.golden` | Resource limits mapped to `PodmanArgs=--memory=1g --cpus=2.0 --pids-limit 256` |
| `quadlet` | `readonly-root` | `readonly-root.golden` | ReadOnly rootfs with tmpfs mounts (`ReadOnly=true`, `ReadOnlyTmpfs=true`) |
| `quadlet` | `caps-added` | `caps-added.golden` | Drops all capabilities; explicitly adds `NET_BIND_SERVICE` |
| `quadlet` | `env-file` | `env-file.golden` | Environment file mapping (`EnvironmentFile=...`) |
| `quadlet` | `health-cmd` | `health-cmd.golden` | Health check command with `Notify=healthy` and start timeout |
| `quadlet` | `url-escaped-health` | `url-escaped-health.golden` | Legitimate health command containing `%` (e.g. `%20`) rendered as `%%20` to prevent specifier expansion |
| `quadlet` | `network-unit` | `network-unit.golden` | Network unit `web.network` with `DefaultDependencies=false` |
| `traefik` | `static-config` | `static-config.golden` | Static Traefik config with ACME resolver, HTTP/3 on :443, and file provider |
| `traefik` | `global-dynamic` | `global-dynamic.golden` | Server-wide default security headers, rate limits, compression, and TLS options |
| `traefik` | `minimal` | `minimal.golden` | Single-domain app router, default security headers, ACME resolver |
| `traefik` | `middleware-toggles` | `middleware-toggles.golden` | Compression enabled, custom rate limit, custom permissions policy |
| `traefik` | `aliases-www` | `aliases-www.golden` | Primary domain + alias, 301 regex redirects for www subdomains |
| `traefik` | `path-prefix` | `path-prefix.golden` | Path prefix `/api/v1` with `StripPrefix: true` |
| `traefik` | `path-prefix-no-strip` | `path-prefix-no-strip.golden` | Path prefix `/docs` with `StripPrefix: false` |
| `traefik` | `basic-auth` | `basic-auth.golden` | HTTP basic authentication with bcrypt-hashed credentials |
| `traefik` | `allowlist` | `allowlist.golden` | IP allowlist CIDR filtering |
| `traefik` | `body-size` | `body-size.golden` | Request buffering with max request body size limit |
| `traefik` | `custom-removed-headers` | `custom-removed-headers.golden` | Custom CSP header and removal of default `X-Frame-Options` |
| `traefik` | `switch-verify` | `switch-verify.golden` | Blue-green switch rendering with priority 10000 verify router and candidate service |
| `traefik` | `vps-setup-headers` | `vps-setup-headers.golden` | Reference header block extracted from `vps-setup.sh` (commit `8fe4cb1aa647a7d7c44710a72bc85000e4d80ef6`) for REL-8 comparison |

---

## 6. Verification Logs Reference

All logs are committed in `docs/evidence/M2/logs/`:

| Log File | Command | Exit Code | Purpose |
| -------- | ------- | --------- | ------- |
| `vm_quadlet_dryrun.log` | `./test/vm/test_quadlet_dryrun.sh` | 0 | Quadlet dry-run on Debian 13 VM, `%` escaping, and negative control rejection |
| `vm_traefik_configs.log` | `./test/vm/test_traefik_configs.sh` | 0 | Isolated throwaway Traefik container, API verification, real curl requests, negative control |
| `vm_reconciliation.log` | `./test/vm/test_reconciliation.sh` | 0 | DEP-10 startup reconciliation on dermaga-vm, all 5 cases verified, negative control |
| `make_check.log` | `make check` | 0 | Complete static analysis, gofmt, SvelteKit build, test suite, and govulncheck |
| `commit1_check.log` | `make check` (worktree) | 0 | Verification of Commit 1 (Quadlet specifier escaping and line continuation rejection) |
| `commit2_check.log` | `make check` (worktree) | 0 | Verification of Commit 2 (Traefik isolated container tests and VPS setup headers) |
| `commit3_check.log` | `make check` (worktree) | 0 | Verification of Commit 3 (Host ReadDir, paths, and store app models) |
| `commit4_check.log` | `make check` (worktree) | 0 | Verification of Commit 4 (Reconciler DEP-10 implementation and VM tests) |
| `commit5_check.log` | `make check` (worktree) | 0 | Verification of Commit 5 (M2 evidence, documentation, and completion) |
| `commit6_check.log` | `make check` (worktree) | 0 | Verification of Commit 6 (Changelog 0.2.0 release notes) |
