# Evidence: M8 (0.8.0) — Domains, HTTPS, headers, middleware

Commit under review: `29a935e` = `v0.8.0-rc.1` (local part and VM run); the rc.2 fixes are local, in the
working tree on top of it (dirty), see § Findings. Checkpoint: **M8a** (domains and HTTPS).

## Spikes (before any code, owner request)

Throwaway Traefik (same image digest as the live one) socket-activated by a transient user socket on
`:18080`, throwaway Pebble 2.10.3 with real validation and `pebble-challtestsrv` 2.10.3 on podman network
`spk-m8`, files in `~/spike-m8`. Scripts: `tmp/evidence/M8/spike-*.sh`. Nothing touched the live Traefik,
its dynamic directory, the live Pebble or the Quadlet directory.

| Question | Status | Log file | Result |
| --- | --- | --- | --- |
| Traefik sees the real client IP through socket activation (owner gate for rate limit and allowlist) | PASS | `tmp/evidence/M8/logs/spike-clientip.log`, `spike-clientip-live.log` | see below |
| `redirectRegex` permanent status | PASS | `spike-misc-a.log` | GET 301, POST and HEAD 308 |
| `noop@internal` as a verify target in a file | PASS | `spike-misc-a.log` | 418 with the token; wrong token 404 (negative control) |
| Buffering with Traefik's read-only root | PASS | `spike-misc-a.log` | 1.5 MB passes (spills to the container tmpfs), 3 MB over a 2 MB limit gets 413 |
| Headers as `customResponseHeaders` equal `stsSeconds`/`contentTypeNosniff` | PASS | `spike-headers.log` | `IDENTICAL`; without the middleware: differs (negative control) |
| Pebble with real validation | PASS | `spike-acme-observe-2.log` | 90-day certificates when the A record is `host.containers.internal` (169.254.1.2); the VM's own IP is local inside the rootless netns (`connection refused`, `spike-up-b2.log`) |
| Traefik's ACME error log line and `acme.json` layout | PASS | `spike-acme-observe-2.log` | `"message":"Unable to obtain ACME certificate for domains"` with `domains` and `error`; `le.Certificates[].domain.main` + base64 PEM |
| ACME retry behaviour | PASS | `spike-acme-retry.log` | see below |
| bcrypt basic-auth cost (oha 50 req/s, 20 s, on the 2-vCPU VM) | PASS | `spike-bcrypt.log`, `spike-bcrypt-678.log` | Traefik CPU per request: none 1.3–1.5 ms, cost 5 3.8, 6 5.3, 7 8.9, 8 15.6, 10 19.0 ms (oha shares the VM, so rough); owner chose cost 5 + 12-character passwords (M8b) |
| Zero residue | PASS | `spike-down.log` | `leftovers: containers=0 networks=0 units=0 dir=no`; live Traefik, sockets and testapp (HTTPS 200) unchanged |

### spike-clientip.log / spike-clientip-live.log

The workstation cannot reach `:18080` (the VM firewall opens only SSH, 80 and 443), so the throwaway Traefik
was tested from the VM's own address, and the live Traefik's socket was inspected while the workstation held a
TLS connection to `:443`:

```text
== from the VM itself (192.168.122.217)
allow: 200
deny: 403
== Traefik access log: ClientHost per router
allow@file 192.168.122.217 200
deny@file 192.168.122.217 403
== ss on the VM (no sudo: only processes of user server are named)
0      0      [::ffff:192.168.122.217]:443 [::ffff:192.168.122.1]:40448 users:(("traefik",pid=5746,fd=12))
== negative control: same query with no client connected
0
```

### spike-acme-retry.log

```text
t0 04:32:27 {'bad.spike.test': 1, 'blocked.spike.test': 4, 'nodns.spike.test': 2}
t0+120s, no change 04:34:27 {'bad.spike.test': 1, 'blocked.spike.test': 4, 'nodns.spike.test': 2}
after writing an unrelated file (no certResolver) 04:34:47 {'bad.spike.test': 1, 'blocked.spike.test': 5, 'nodns.spike.test': 3}
after a second unrelated change 04:35:07 {'bad.spike.test': 2, 'blocked.spike.test': 7, 'nodns.spike.test': 4}
```

Traefik never retries a failed order by itself, but retries every failed domain on any dynamic config change:
the reason for the extended DNS gate (DEC-38).

## Acceptance criteria (M8a, local)

| Criterion (req ID) | Status | Log file | Excerpt |
| --- | --- | --- | --- |
| APP-2, TLS-2, TLS-3: domain set API (roles, prefix, DNS gate for new domains, statuses, Check again), authentication on the 3 routes | PASS | `tmp/evidence/M8/logs/m8a-go-tests.log` | `TestDomainsAPI_APP2_TLS2_TLS3` |
| APP-2: refused sets (hook domain, another app's domain, wildcard, non-ASCII, injection, no dot, duplicate, two/no primary, bad role, bad prefix) leave the domains unchanged | PASS | same | `TestDomainsAPI_Validation_APP2` |
| APP-10: a route Traefik does not load restores the previous file, keeps the rows and returns Traefik's error (engine and API) | PASS | same | `TestApplyRoute_TraefikRejects_RestoresAndKeepsDB_APP10`, `TestDomainsAPI_TraefikRejects_APP10` |
| APP-10: a change is proven through the verify router before it is committed; refused while a release starts or switches | PASS | same | `TestApplyRoute_ProvenThenCommitted_APP10`, `TestApplyRoute_RefusedWhileSwitching` |
| TLS-2 (DEC-38): a domain with wrong DNS gets no router, rechecked only every 5 minutes or on Check again, routed once DNS passes | PASS | same | `TestSyncApp_DNSGate_DEC38` |
| DEC-38: failed order without certificate leaves the route, back after an hour or Check again; a domain with a certificate stays; older errors do not count again | PASS | same | `TestSyncApp_FailedOrderLeavesRoute_DEC38` |
| Upgrade: an outdated (0.7.0) route file is rewritten with proof; an up-to-date one is not | PASS | same | `TestSyncApp_RewritesOutdatedRoute` |
| A route that keeps failing is one Activity entry, logged again after a success | PASS | same | `TestSyncDomains_FailureLoggedOnce` |
| Migration 0006 from a 0005 database: existing domains stay routed | PASS | same | `TestStoreMigration0005To0006_DEC38`, `TestReplaceDomains_APP2` |
| TLS-3: `acme.json` and Traefik's real error lines (captured in the spike) read; plain reasons | PASS | same | `TestReadCertificates_TLS3`, `TestACMEErrors_TLS3` |
| TLS-4: below 1/6 of the lifetime is a warning (90 and 45 days), dashboard and doctor | PASS | same | `TestExpiringSoon_TLS4`, `TestDoctorCertificates_TLS4` |
| DEC-44: the resolver override asks only the configured server; doctor warns while it is set | PASS | same | `TestLookupFor_DEC44`, `TestDoctorDNSResolverOverride_DEC44` |
| DEC-39: one router per host; pending domains get no router, the pending primary still names the verify router; golden files | PASS | same | `TestTraefikGolden` (new `pending-domains.golden`, 10 updated), `TestStripPrefixOncePerPrefix` |
| M7 switch, rollback and recovery unchanged with the shared route builder | PASS | same | `TestRedeploy*`, `TestRollback*`, `TestReconcile*` (46 tests PASS, 0 FAIL) |
| Negative controls: 15 code mutations (12 for rc.1, 3 for the rc.2 fixes), each must make its test fail | PASS | `tmp/evidence/M8/logs/m8a-negative-controls.log` | see below |
| Browser: Domains tab (rows, record to create, failed reason and next try, add with www redirect and a field error, make primary, Traefik refusal, Check again, prefix error), dashboard warning, hook domain certificate; axe WCAG AA at 1280 and 375 in both themes | PASS | `tmp/evidence/M8/logs/m8a-browser-domains.log`, `m8a-browser-all.log` | `2 passed`, full suite `15 passed` |
| `make check` | PASS | `.check-logs/` (latest run) | all steps `ok` |
| VM (`v0.8.0-rc.1`): upgrade, certificate cases, Check again, redirects, rejected route, doctor, demo state, denied logins | see § M8a on dermaga-vm | `tmp/evidence/M8/logs/vm-*.log` | |

### m8a-negative-controls.log

```text
detected (test fails as it must): TestSyncApp_DNSGate_DEC38
detected (test fails as it must): TestSyncApp_FailedOrderLeavesRoute_DEC38
detected (test fails as it must): TestSyncApp_FailedOrderLeavesRoute_DEC38
detected (test fails as it must): TestApplyRoute_TraefikRejects_RestoresAndKeepsDB_APP10
detected (test fails as it must): TestApplyRoute_TraefikRejects_RestoresAndKeepsDB_APP10
detected (test fails as it must): TestApplyRoute_RefusedWhileSwitching
detected (test fails as it must): TestSyncApp_RewritesOutdatedRoute
detected (test fails as it must): TestDomainsAPI_Validation_APP2
detected (test fails as it must): TestDomainsAPI_APP2_TLS2_TLS3
detected (test fails as it must): TestExpiringSoon_TLS4
detected (test fails as it must): TestStoreMigration0005To0006_DEC38
detected (test fails as it must): TestSyncDomains_FailureLoggedOnce
detected (test fails as it must): TestApplyRoute_UsesRouteTimeout_APP10
detected (test fails as it must): TestExpiringSoon_TLS4
detected (test fails as it must): TestServerStatusCertificateWarningsArray_TLS4
undetected mutations: 0
```

The first run found one gap: `TestExpiringSoon_TLS4` only checked the exact 1/3 point, so a wrong 1/3
threshold passed; the test now checks 1/4 left (no warning) and the mutation is detected.

## M8a on dermaga-vm (`v0.8.0-rc.1`)

Release binary from GitHub (`sha256sum -c` OK, `dermaga version v0.8.0-rc.1 (commit 29a935e)`,
`vm-release-download.log`), installed per SETUP §6.3. Baseline before any change in `vm-baseline.log`
(v0.7.0-rc.5, Pebble with `PEBBLE_VA_ALWAYS_VALID=1`, certificates for `hooks` and `testapp` until 25 Feb 2027).
Scripts: `test/vm/setup_pebble.sh`, `test/vm/test_dns.sh`, `test/vm/seed.sh` (committed) and
`tmp/evidence/M8/vm-*.sh` (run scripts). The panel API was called through `tmp/evidence/M8/vmapi.sh` (login on the
VM, password never printed).

| Check | Status | Log file | Result |
| --- | --- | --- | --- |
| New Pebble setup (real validation, `pebble-dns`, `dermaga-test-dns`, drop-in), existing domains registered, testapp HTTPS 200, second run changes nothing | PASS | `vm-pebble-setup.log` | second run: every container "up to date", "drop-in unchanged" |
| Upgrade 0.7.0-rc.5 → 0.8.0-rc.1 under `oha` 50 req/s for 120 s | PASS | `vm-upgrade.log` | see below |
| Certificate cases: good → active, wrong → waiting with the record, fail → failed with the real reason, out of the route, next try | PASS (after the account reset) | `vm-cert-cases.log`, `vm-check-again.log` | first attempt failed for all three: see Finding 2 |
| "Check again" brings failed domains back into the route | PASS | `vm-check-again.log` | good and www active ~20 s later; fail back in the route, fails again, out again |
| No verify router left after the watcher's applies | PASS | `vm-verify-router-check.log` | 3 reads 5 s apart: `app-testapp-host-0 app-testapp-host-1 app-testapp-redirect-0` |
| HTTP → HTTPS and www → primary | PASS | `vm-redirects.log` | see below |
| A route change Traefik does not load: route file byte-identical, domains unchanged, testapp keeps serving, change succeeds after the restore | PASS | `vm-route-rejected.log` | |
| ... and the panel shows Traefik's error | **FAIL** | `vm-route-rejected.log` | `curl: (52) Empty reply from server` after ~30 s: Finding 1, fixed for rc.2 |
| doctor: test resolver warning; a real 5-minute certificate warned by doctor, the dashboard API and the domain status; ok again after the restore | PASS | `vm-doctor.log` | see below |
| Demo state (`seed.sh`) and M7/M8 invariants | PASS | `vm-seed.log`, `m8-invariants-control.log` | routes = good, testapp, www; not routed = fail, wrong; control 7/7 |
| Real panel, Domains tab | PASS | `vm-domains-shot.log` | screenshots below |
| Denied logins last, VM-side unban, new `ssh dermaga` ≥ 30 s later | PASS | `vm-m3-access.log`, `vm-final-ssh.log` | bans 4 → 5; new login 12:57:19; `ssh dermaga` 12:57:27 (89 s after the last denied attempt) |

### vm-upgrade.log

```text
=== upgrade 12:33:00
.local/bin/dermaga.new: OK
restarted 12:33:01
=== after
dermaga version v0.8.0-rc.1 (commit 29a935e)
schema version: 6
Traefik restarted: no
--- route of testapp: routers before -> after
app-testapp
app-testapp-host-0
Warning: DNS checks use the test resolver 127.0.0.1:8053 (DERMAGA_DNS_RESOLVER)
traefik error lines: 0
{"domain":"testapp.dermaga.test","role":"primary","dns_verified":true,"cert":"active","expires":"2027-02-25T00:55:59Z"}
status codes: {"200":6001}
errors: {}
RESULT upgrade: oha non-2xx=0 connection-errors=0
```

### vm-check-again.log (certificate cases after the account reset)

```text
12:42:31 testapp=active fail=requesting good=requesting www=requesting
12:42:42 testapp=active fail=requesting good=requesting www=active
12:42:52 testapp=active fail=requesting good=active www=active
12:43:02 testapp=active fail=failed good=active www=active
{"domain":"fail.dermaga.test","cert":"failed","reason":"The certificate authority could not reach this server on port 80 in time.","fix":"Check that the A record points to this server and that no firewall or proxy blocks port 80, then press Check again.","next_retry":"2026-11-27T06:43:01Z","detail":"unable to generate a certificate for the domains [fail.dermaga.test]: resolver: one or more domains had a problem: [fail.dermaga.test: invalid authorization: ac"}
{"domain":"wrong.dermaga.test","cert":"waiting_dns"}
--- routers (fail.dermaga.test out again, wrong.dermaga.test never in)
app-testapp-host-0: Host(`good.dermaga.test`)
app-testapp-host-1: Host(`testapp.dermaga.test`)
app-testapp-redirect-0: Host(`www.testapp.dermaga.test`)
app-testapp-verify: Host(`testapp.dermaga.test`) && Header(`X-Dermaga-Verify`, `69980be9070bfb04379d7abe386da6b7`)
--- HTTPS
good.dermaga.test https 200
testapp.dermaga.test https 200
```

The `app-testapp-verify` router is the watcher's apply caught between its two writes (`vm-verify-router-check.log`:
gone in three later reads). `vm-cert-cases.log` shows `wrong.dermaga.test` with `"record":"A wrong.dermaga.test -> 192.168.122.217"` and
never in the route file; its negative control (`wrong https 000`, no TLS for a domain without a router).

### vm-redirects.log

```text
--- http://testapp.dermaga.test/a?b=1 (expect a permanent redirect to https)
301 -> https://testapp.dermaga.test/a?b=1
--- https://www.testapp.dermaga.test/a?b=1 (expect 301 to the primary, path and query kept)
301 -> https://testapp.dermaga.test/a?b=1
--- http://www.testapp.dermaga.test/ followed to the end (http -> https -> primary)
final 200 https://testapp.dermaga.test/ after 2 redirects
--- HEAD on the www alias (Traefik answers 308 for non-GET, spike)
HTTP/2 308
--- negative control: the primary over https is served, no redirect
200 ->
```

### vm-route-rejected.log

Traefik's file provider was frozen by making dermaga's own `00-global.yml` invalid for 34 s (its bytes saved
first and written back exactly, `00-global.yml restored exactly: yes`); no foreign file was placed in the live
directory. Traefik logged `collecting file configs: /etc/traefik/dynamic/00-global.yml: yaml: line 1: did not
find expected ',' or ']'` and kept serving the last good configuration.

```text
=== PUT with edit.dermaga.test added
curl: (52) Empty reply from server
HTTP 000
testapp https 200
route file unchanged: yes
domains after: ["testapp.dermaga.test","fail.dermaga.test","good.dermaga.test","wrong.dermaga.test","www.testapp.dermaga.test"]
=== positive control: the same PUT now
HTTP 200
```

### vm-doctor.log

```text
[WARN] dns resolver   DNS checks use the test resolver 127.0.0.1:8053
12:52:22 {"status":"active","expires_at":"2026-11-27T05:53:06Z","expiring_soon":true}
[WARN] certificates   The certificate for expiring.dermaga.test expires in 0 days (27 Nov 2026) and has not been renewed.
--- dashboard API: certificate_warnings
["The certificate for expiring.dermaga.test expires in 0 days (27 Nov 2026) and has not been renewed."]
--- doctor after the restore (certificates ok; resolver warning stays while the drop-in is set)
[OK  ] certificates   no certificate close to expiry (7 domains)
null
```

("0 days" and `null` are Findings 3 and 4.)

### Findings

1. **The panel got no answer when Traefik did not load a route change** (APP-10 "shows the error"). The engine
   waited the 30 s switch timeout for the proof; the panel's HTTP write timeout is 15 s (`SEC-15`), so the reply
   was cut. Route and rows were kept correctly. Fix (local, for rc.2): `RouteTimeout` (default 10 s) for route
   changes and the watcher, the switch keeps 30 s; `TestApplyRoute_UsesRouteTimeout_APP10`,
   `TestRouteProofFitsWriteTimeout_APP10`, mutation detected. The verify-timeout wording is now neutral ("Traefik
   did not answer through the verify router within …") for both uses.
2. **A new Pebble forgets Traefik's ACME account**: every order failed with `accountDoesNotExist` after the new
   Pebble started, and Traefik did not register again. Test-environment cause (Let's Encrypt keeps accounts):
   `setup_pebble.sh` now drops the `le` account from `acme.json` for each new Pebble container and restarts
   Traefik. The reason was the generic "refused"; it now has plain words (rc.2).
3. "expires in 0 days" for a certificate with less than a day left → "expires in less than a day" / "expired on"
   with the time (rc.2).
4. `certificate_warnings` was `null` when empty → always a list (rc.2).
5. **A Traefik restart is not free of errors**: the account-reset restart under `oha` (40 s) gave
   `{"200":1950}` and `{"invalid peer certificate: UnknownIssuer":5,"tls handshake eof":46}`
   (`vm-check-again.log`). SETUP §6.3 claimed requests wait in the backlog; corrected. M8a's upgrade does not
   restart Traefik; releases that change Traefik's static config or image do (open item for M10).

### Screens (real panel on dermaga-vm)

| Local file | Page | Width | Theme | Shows |
| --- | --- | --- | --- | --- |
| `tmp/evidence/M8/screenshots/m8a-vm-domains-1280-light.png` | testapp → Domains (real VM) | 1280 | light | testapp active until 25 Feb 2027; fail.dermaga.test failed "could not reach this server on port 80 in time", next try 13:43; good active; wrong waiting with `A wrong.dermaga.test → 192.168.122.217`; www redirect active |
| `tmp/evidence/M8/screenshots/m8a-vm-domains-375-dark.png` | testapp → Domains (real VM) | 375 | dark | same rows stacked, readable after the colour transitions settled (a first capture taken mid-transition showed faded buttons) |

## Run & view

Locally: `make check`; `cd ui && EVIDENCE_MILESTONE=M8 bunx playwright test e2e/domains.test.ts`.

On `dermaga-vm`: `test/vm/seed.sh` (Pebble with real validation, test DNS, demo domains, invariants), then
`ssh -N -L 7070:127.0.0.1:7070 dermaga-vm` and http://localhost:7070 → Apps → testapp → **Domains**
(good active, wrong waiting for DNS, fail failed with the next try, www redirecting); change a case with
`test/vm/test_dns.sh dermaga-vm good wrong.dermaga.test` and press **Check again**; Settings → **Hook Domain**;
Dashboard. HTTPS from the workstation: copy `~/.local/share/dermaga/traefik/pebble-minica.pem` and use
`curl --cacert <it> --connect-to <domain>:443:192.168.122.217:443 https://<domain>/`. Teardown:
`test/vm/setup_pebble.sh dermaga-vm down` (SETUP §7.5).

## Screens

Captured by `ui/e2e/domains.test.ts` (API mocked), opened and checked one by one.

| Local file | Page | Width | Theme | Shows |
| --- | --- | --- | --- | --- |
| `tmp/evidence/M8/screenshots/m8a-domains-1280-light.png` | App → Domains | 1280 | light | 4 rows: primary active until 25 Feb 2027, www redirect requesting, alias waiting with the A record and Check again, redirect failed with reason, fix, next try 12:31 and details; path prefix form |
| `tmp/evidence/M8/screenshots/m8a-domains-1280-dark.png` | App → Domains | 1280 | dark | same rows, amber and red boxes readable on dark |
| `tmp/evidence/M8/screenshots/m8a-domains-375-light.png` | App → Domains | 375 | light | rows stacked, Actions under each domain, Check again buttons full row, prefix form stacked |
| `tmp/evidence/M8/screenshots/m8a-domains-375-dark.png` | App → Domains | 375 | dark | same, dark |
| `tmp/evidence/M8/screenshots/m8a-add-domain-1280-light.png` | Add a domain dialog | 1280 | light | api.example.com, DNS not pointing here with the record and "you can save it now", Alias selected, www.api.example.com redirect checked |
| `tmp/evidence/M8/screenshots/m8a-add-domain-1280-dark.png` | Add a domain dialog | 1280 | dark | same, dark |
| `tmp/evidence/M8/screenshots/m8a-add-domain-375-light.png` | Add a domain dialog | 375 | light | stacked input and Check DNS, the www checkbox label wraps as one text block (fixed after a first capture split it into three columns) |
| `tmp/evidence/M8/screenshots/m8a-add-domain-375-dark.png` | Add a domain dialog | 375 | dark | same, dark; the www checkbox label wraps as one text block |
| `tmp/evidence/M8/screenshots/m8a-domains-refused-1280-light.png` | App → Domains | 1280 | light | red alert "Traefik did not load the new route, so the previous route and settings stay in place: middleware "x@file" does not exist", new.example.com primary, api and www.api added waiting for DNS |
| `tmp/evidence/M8/screenshots/m8a-dashboard-cert-warning-1280-light.png` | Dashboard | 1280 | light | amber alert: certificate for shop.example.com expires in 6 days (3 Dec 2026) and has not been renewed, with what to check |
| `tmp/evidence/M8/screenshots/m8a-dashboard-cert-warning-1280-dark.png` | Dashboard | 1280 | dark | same, dark |
| `tmp/evidence/M8/screenshots/m8a-dashboard-cert-warning-375-light.png` | Dashboard | 375 | light | alert above the server cards, wrapped |
| `tmp/evidence/M8/screenshots/m8a-dashboard-cert-warning-375-dark.png` | Dashboard | 375 | dark | same, dark |
| `tmp/evidence/M8/screenshots/m8a-hook-domain-cert-1280-light.png` | Settings → Hook Domain | 1280 | light | DNS matches, "HTTPS certificate active until 25 Feb 2027." above Save |

## How it works

See ARCHITECTURE §5.7 (flow, DNS gate, failed orders, watcher, route edits with the sequence diagram).
In short: Domains tab → `PUT /api/apps/{id}/domains` → `server.buildDomainSet` (validation, DNS check for new
domains) → `deploy.Engine.ApplyRoute` (app lock, route + verify router to the live release, token through
`:8099`, final route, then `store.ReplaceDomains`). The watcher (`RunDomainWatcher`, every minute) moves domains
through the DNS gate, takes failed orders out of the route and back after an hour, and re-applies routes that
differ from what the domains say (also after an upgrade).

## Code tour

1. `internal/store/migrations/0006_m8_domains.sql`, `internal/store/apps.go`: state columns, `Domain.Routed`, `ReplaceDomains`, `SetDomainState`, `DomainOwner`, `ListDomainHosts`.
2. `internal/traefik/render.go`: one router per host, `Pending` domains skipped, strip prefix once.
3. `internal/traefik/certs.go`: `ReadCertificates`, `ExpiringSoon` (1/6), `ACMEErrors`, `PlainReason`, `LastConfigError`.
4. `internal/deploy/route.go`: `RouteSpecFor`, `writeRouteWith`, `ApplyRoute`, `SyncApp`, `DomainStatusOf`.
5. `internal/deploy/switch.go`, `internal/reconciler/switch.go`: the old route copies removed.
6. `internal/access/dns.go`: `PointsHere`, `LookupFor` (DEC-44); `cmd/dermaga/main.go`: wiring and the watcher.
7. `internal/server/domains.go`: the three routes, validation, status view, hook domain certificate, warnings.
8. `internal/doctor/doctor.go`: `certificates` and `dns resolver` checks.
9. `ui/src/lib/components/DomainsTab.svelte`, `ui/src/lib/api.ts`: the tab (mockup A) and its client calls; `HookDomainForm.svelte`, `routes/+page.svelte`: certificate line and warnings.

## Decisions & deviations

- DEC-38–DEC-44 accepted by the owner (2026-11-27), DEC-38 extended with the failed-order rule after the retry spike.
- Router names changed from `app-<name>` (one router) to `app-<name>-host-<n>` / `app-<name>-redirect-<n>`; `LiveRelease` reads the service, so recovery is unaffected.
- The reconciler still writes only missing route files; outdated ones are rewritten by the watcher with proof, so a broken render can never freeze Traefik at start.
- No static Traefik config change in M8a, so the VM's `traefik.yml` (made foreign by `setup_pebble.sh`) does not block the upgrade.
- Nothing done as root.

## Suggested commits (M8a)

`scripts/check-commits tmp/evidence/M8/commits-m8a.plan`:

```text
PASS static checks: 7 commits, 51 paths
PASS commit 1: feat(traefik): give every host its own router and read cert status
PASS commit 2: feat(store): store DNS and certificate state per domain
PASS commit 3: feat(deploy): apply route changes with proof and gate domains on DNS
PASS commit 4: feat(doctor): warn about expiring certificates and the test resolver
PASS commit 5: feat(api): add domain routes and certificate status
PASS commit 6: feat(ui): add the Domains tab and certificate warnings
PASS commit 7: docs: record the M8a design and local evidence
```

## Suggested commits (rc.1 VM run and rc.2 fixes)

`scripts/check-commits tmp/evidence/M8/commits-m8a-rc2.plan`:

```text
PASS static checks: 4 commits, 21 paths
PASS commit 1: fix(traefik): name short expiries and unknown ACME accounts plainly
PASS commit 2: fix(api): answer route changes before the panel's write timeout
PASS commit 3: test(vm): run Pebble with real validation and a test DNS
PASS commit 4: docs: record the M8a VM run on v0.8.0-rc.1
```
