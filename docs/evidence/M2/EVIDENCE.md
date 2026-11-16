# Evidence: Milestone M2 (0.2.0) — Generators and Reconciliation

This document records the verification evidence for milestone **M2 (0.2.0)** of **Dermaga**, specifically **Checkpoint M2a: Generators and Dry-Run Validation**.

All tests and measurements were executed on the development workstation and the Debian 13 virtual machine (`dermaga-vm`).

---

## 1. Summary of Acceptance Criteria

| Criteria | Status | Details |
| -------- | ------ | ------- |
| **Golden tests for every generator** | **PASS** | Golden tests in `internal/quadlet/testdata/` (minimal app, custom limits, readonly root, added capabilities, env file, health cmd, network unit) and `internal/traefik/testdata/` (static config, global dynamic, minimal app, middleware toggles, aliases + www, path prefix with/without strip, basic auth, allowlist, body size, custom/removed headers, switch verify). |
| **SEC-16–SEC-19 (Injection Hardening)** | **PASS** | Injection tests reject newlines, null bytes, backticks, dollar expansions, quotes, percent characters, semicolons, and traversal paths across all user-controlled fields. |
| **Quadlet Dry-Run on VM** | **PASS** | All generated Quadlet golden units pass `/usr/lib/systemd/system-generators/podman-system-generator --dryrun --user` on `dermaga-vm` (Debian 13, Podman 5.4.2) with exit code 0. |
| **Traefik Configuration Validation on VM** | **PASS** | Dynamic configuration files validated against live Traefik v3.7.19 on `dermaga-vm` without syntax or watcher callback errors. |
| **REL-8 Default Security Headers** | **PASS** | App dynamic generator defaults equal `vps-setup.sh`'s: HSTS (`max-age=31536000; includeSubDomains`), `X-Content-Type-Options: nosniff`, `X-Frame-Options: SAMEORIGIN`, `Referrer-Policy: strict-origin-when-cross-origin`, `Permissions-Policy: camera=(), microphone=(), geolocation=(), payment=()`, and strip `Server` / `X-Powered-By`. |
| **ACME on Every Router** | **PASS** | Every router (main, aliases, redirects, and switch verify) declares `tls.certResolver: "le"`. |
| **Verify Router in Switch Rendering Only** | **PASS** | Router `app-<name>-verify` with `X-Dermaga-Verify` header and candidate service is rendered only when `Switch` spec is provided, and omitted in standard live configs. |
| **DEP-10 (Reconciliation)** | **Deferred to M2b** | Scheduled for Checkpoint M2b (startup reconciler, fake host tests, orphan cleanup, service restart). |

---

## 2. Quadlet Generator & Dry-Run Validation

All generated Quadlet unit test cases were verified on `dermaga-vm` using `/usr/lib/systemd/system-generators/podman-system-generator --dryrun --user`.

Log file: [`docs/evidence/M2/logs/vm_quadlet_dryrun.log`](logs/vm_quadlet_dryrun.log)

```text
=== Quadlet Generator Dry-Run Verification on dermaga-vm ===
Timestamp: 2026-11-16T21:08:14+07:00
Git commit: 4274867
Working tree status: dirty
Host: dermaga-vm

Created remote directory: /tmp/quadlet-verify-zDcGqn
Staging caps-added -> app-caps-added.container
Staging custom-limits -> app-custom-limits.container
Staging env-file -> app-env-file.container
Staging health-cmd -> app-health-cmd.container
Staging minimal -> app-minimal.container
Staging network-unit -> web.network
Staging readonly-root -> app-readonly-root.container

Executing /usr/lib/systemd/system-generators/podman-system-generator --dryrun --user...
quadlet-generator[3596]: Loading source unit file /tmp/quadlet-verify-zDcGqn/app-caps-added.container
quadlet-generator[3596]: Loading source unit file /tmp/quadlet-verify-zDcGqn/app-custom-limits.container
quadlet-generator[3596]: Loading source unit file /tmp/quadlet-verify-zDcGqn/app-env-file.container
quadlet-generator[3596]: Loading source unit file /tmp/quadlet-verify-zDcGqn/app-health-cmd.container
quadlet-generator[3596]: Loading source unit file /tmp/quadlet-verify-zDcGqn/app-minimal.container
quadlet-generator[3596]: Loading source unit file /tmp/quadlet-verify-zDcGqn/app-readonly-root.container
quadlet-generator[3596]: Loading source unit file /tmp/quadlet-verify-zDcGqn/web.network

=== Quadlet Dry-Run Verification: PASS ===
```

### Key Quadlet Verifications:
1. **DEC-06 (`DefaultDependencies=false`)**: Every generated unit includes `[Quadlet] DefaultDependencies=false`. The generator output contains no implicit dependency on `podman-user-wait-network-online.service`, eliminating the 90-second boot hang.
2. **DEC-05 (Resource Limits via `PodmanArgs=`)**: Generated container units map memory and CPU limits directly to `podman run --memory=... --cpus=...` while keeping `conmon` unconstrained.
3. **Restart Backoff (`[Service]`)**: Configured with `Restart=on-failure`, `RestartSec=1s`, `RestartSteps=3`, `RestartMaxDelaySec=8s`, `TimeoutStartSec=30`, `TimeoutStopSec=20`.
4. **SEC-19 (Capabilities & Privileges)**: Enforces `NoNewPrivileges=true` and `DropCapability=ALL` by default. Additional capabilities (e.g. `NET_BIND_SERVICE`) map cleanly to `--cap-add`.
5. **Health Checks**: Containers with health commands map to `--sdnotify=healthy` and `--health-cmd "..."`.

---

## 3. Traefik Generator & Configuration Validation

All Traefik dynamic configurations were tested against live Traefik v3.7.19 running in rootless Podman on `dermaga-vm`.

Log file: [`docs/evidence/M2/logs/vm_traefik_configs.log`](logs/vm_traefik_configs.log)

```text
=== Traefik Golden Configurations Verification on dermaga-vm ===
Timestamp: 2026-11-16T21:09:56+07:00
Git commit: 4274867
Working tree status: dirty
Host: dermaga-vm

Testing dynamic config aliases-www... OK (loaded by Traefik without errors)
Testing dynamic config allowlist... OK (loaded by Traefik without errors)
Testing dynamic config basic-auth... OK (loaded by Traefik without errors)
Testing dynamic config body-size... OK (loaded by Traefik without errors)
Testing dynamic config custom-removed-headers... OK (loaded by Traefik without errors)
Testing dynamic config global-dynamic... OK (loaded by Traefik without errors)
Testing dynamic config middleware-toggles... OK (loaded by Traefik without errors)
Testing dynamic config minimal... OK (loaded by Traefik without errors)
Testing dynamic config path-prefix... OK (loaded by Traefik without errors)
Testing dynamic config path-prefix-no-strip... OK (loaded by Traefik without errors)
Validating static config static-config (JSON syntax)... OK
Testing dynamic config switch-verify... OK (loaded by Traefik without errors)

=== All Traefik Golden Configurations Verified: PASS ===
```

### Key Traefik Verifications:
1. **JSON in `.yml` (DEC-01)**: All dynamic configurations are serialized as standard JSON and parsed natively by Traefik's file provider.
2. **REL-8 Default Security Headers**: Matches `vps-setup.sh`:
   - `stsSeconds: 31536000`
   - `stsIncludeSubdomains: true`
   - `contentTypeNosniff: true`
   - `customFrameOptionsValue: SAMEORIGIN`
   - `referrerPolicy: strict-origin-when-cross-origin`
   - `customResponseHeaders: {"Permissions-Policy": "camera=(), microphone=(), geolocation=(), payment=()", "Server": "", "X-Powered-By": ""}`
3. **ACME on Every Router**: All HTTP routers declare `tls: { "certResolver": "le" }`.
4. **Switch Verify Router (DEC-16)**: Temporary router `app-<name>-verify` with `rule: "Host(...) && Header('X-Dermaga-Verify', '...')"` and `priority: 10000` points traffic to candidate service `app-<name>-candidate` during deployments.
5. **301 Redirects**: Dedicated redirect routers enforce permanent regex redirects for aliases and www-to-apex domains.

---

## 4. SEC-16–SEC-19 Injection Hardening Evidence

Unit tests in `internal/quadlet/injection_test.go` and `internal/traefik/injection_test.go` verify that invalid input is rejected at the boundary without shell execution:

```text
=== RUN   TestQuadletInjectionRejection
--- PASS: TestQuadletInjectionRejection (0.00s)
=== RUN   TestDefaultDependenciesGuaranteed
--- PASS: TestDefaultDependenciesGuaranteed (0.00s)
=== RUN   TestTraefikInjectionRejection
--- PASS: TestTraefikInjectionRejection (0.00s)
=== RUN   TestACMEOnEveryRouter
--- PASS: TestACMEOnEveryRouter (0.00s)
=== RUN   TestDefaultHeadersMatchVPSSetup
--- PASS: TestDefaultHeadersMatchVPSSetup (0.00s)
```

Tested input vectors:
- Newlines (`\n`, `\r\n`) and null bytes (`\x00`)
- Shell command substitution (backticks `` `...` ``, `$()`, `${VAR}`)
- Semicolons, pipes, redirects (`;`, `|`, `&`, `>`, `<`)
- Quotes (single `'` and double `"`)
- Traversal sequences (`../`, `/`)
- Invalid memory formats (`512mb`, `100%`) and CPU limits (`2 CPUs`)
- Invalid capabilities and basic auth usernames containing spaces or colons

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

---

## 6. Next Steps: Checkpoint M2b (Reconciliation)

Checkpoint M2a is complete and verified. Next checkpoint (M2b) will implement:
1. `internal/reconciler`: Startup reconciliation per `DEP-10`.
2. Unit tests with `FakeHost` (orphan unit removal, missing file re-rendering, stopped live unit restart, leftover verify router removal, foreign file safety).
3. VM verification of reconciliation routines.
4. Version bump to `v0.2.0` in `CHANGELOG.md`.
