# Evidence: M8 (0.8.0) — Domains, HTTPS, headers, middleware

Commit under review: working tree on `577350f` (dirty: the M8a changes, before the owner's commits).
Checkpoint: **M8a** (domains and HTTPS), local part. The VM run follows on `v0.8.0-rc.1`.

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
| Negative controls: 12 code mutations, each must make its test fail | PASS | `tmp/evidence/M8/logs/m8a-negative-controls.log` | see below |
| Browser: Domains tab (rows, record to create, failed reason and next try, add with www redirect and a field error, make primary, Traefik refusal, Check again, prefix error), dashboard warning, hook domain certificate; axe WCAG AA at 1280 and 375 in both themes | PASS | `tmp/evidence/M8/logs/m8a-browser-domains.log`, `m8a-browser-all.log` | `2 passed`, full suite `15 passed` |
| `make check` | PASS | `.check-logs/` (latest run) | all steps `ok` |
| VM: correct DNS → active, wrong DNS → waiting with the record, failed challenge → reason; HTTP → HTTPS and 301; upgrade from 0.7.0 keeps testapp serving | NOT RUN | — | needs `v0.8.0-rc.1` (owner tag); plan below |

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
undetected mutations: 0
```

The first run found one gap: `TestExpiringSoon_TLS4` only checked the exact 1/3 point, so a wrong 1/3
threshold passed; the test now checks 1/4 left (no warning) and the mutation is detected.

## Run & view

Locally: `make check`; `cd ui && EVIDENCE_MILESTONE=M8 bunx playwright test e2e/domains.test.ts`.

VM run on `v0.8.0-rc.1` (next session, after the owner's tag): binary upgrade per SETUP §6.3; Pebble with real
validation and two `pebble-challtestsrv` instances (one for Pebble with records to 169.254.1.2, one for
dermaga on `127.0.0.1:8053` with records to the VM's address, set through the `DERMAGA_DNS_RESOLVER` drop-in);
test domains for correct DNS (active), wrong DNS (waiting with the record) and a failed challenge (the dermaga
DNS passes, the Pebble DNS points to a dead IP: reason shown, out of the route, next try); `curl` for
HTTP → HTTPS and the 301 redirect; testapp keeps serving across the upgrade (`oha`); denied logins last.
Panel: Apps → testapp → **Domains**; Settings → **Hook Domain**; Dashboard.

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
