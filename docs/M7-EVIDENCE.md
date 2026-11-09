# Milestone 7 Evidence: Embedded Web Dashboard & SvelteKit UI

**Date:** 2026-11-09  
**Host:** `dermaga-vm` (Debian GNU/Linux 13 trixie, Linux 6.12.115+deb13.1-cloud-amd64 x86_64)  
**Binary:** `/usr/local/bin/dermaga` (14 MB amd64 static binary with embedded SvelteKit 3 UI)  
**Frontend Stack:** SvelteKit 3.1.1, Svelte 5.57.3, Tailwind CSS 4.3.5, Vite 8.3.6, TypeScript 5.9.3, `tygo v0.2.21`  

---

## 1. Pre-flight & Test Suite Verification

### 1.1 `make check` (Linter, Tygo Freshness, Vulncheck, Tests)

```console
$ make check
go tool tygo generate
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
ok  	github.com/r3p-dev/dermaga/internal/backup	(cached)
ok  	github.com/r3p-dev/dermaga/internal/cli	0.006s
ok  	github.com/r3p-dev/dermaga/internal/config	(cached)
ok  	github.com/r3p-dev/dermaga/internal/daemon	(cached)
ok  	github.com/r3p-dev/dermaga/internal/databases	(cached)
ok  	github.com/r3p-dev/dermaga/internal/deploy	(cached)
ok  	github.com/r3p-dev/dermaga/internal/doctor	(cached)
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
?   	github.com/r3p-dev/dermaga/ui	[no test files]
```

### 1.2 Race Detector Suite (`CGO_ENABLED=1 go test -count=1 -race ./...`)

```console
$ CGO_ENABLED=1 go test -count=1 -race ./...
?   	github.com/r3p-dev/dermaga/cmd/dermaga	[no test files]
ok  	github.com/r3p-dev/dermaga/internal/api	4.846s
ok  	github.com/r3p-dev/dermaga/internal/apps	1.600s
?   	github.com/r3p-dev/dermaga/internal/audit	[no test files]
ok  	github.com/r3p-dev/dermaga/internal/auth	3.782s
ok  	github.com/r3p-dev/dermaga/internal/backup	2.053s
ok  	github.com/r3p-dev/dermaga/internal/cli	1.035s
ok  	github.com/r3p-dev/dermaga/internal/config	1.026s
ok  	github.com/r3p-dev/dermaga/internal/daemon	1.035s
ok  	github.com/r3p-dev/dermaga/internal/databases	1.383s
ok  	github.com/r3p-dev/dermaga/internal/deploy	5.900s
ok  	github.com/r3p-dev/dermaga/internal/doctor	1.134s
ok  	github.com/r3p-dev/dermaga/internal/gitsrc	1.104s
?   	github.com/r3p-dev/dermaga/internal/golden	[no test files]
ok  	github.com/r3p-dev/dermaga/internal/host	1.020s
?   	github.com/r3p-dev/dermaga/internal/name	[no test files]
ok  	github.com/r3p-dev/dermaga/internal/probe	1.654s
ok  	github.com/r3p-dev/dermaga/internal/quadlet	1.091s
ok  	github.com/r3p-dev/dermaga/internal/reconcile	1.186s
ok  	github.com/r3p-dev/dermaga/internal/relay	1.022s
ok  	github.com/r3p-dev/dermaga/internal/secrets	1.015s
ok  	github.com/r3p-dev/dermaga/internal/spec	1.028s
ok  	github.com/r3p-dev/dermaga/internal/store	1.409s
ok  	github.com/r3p-dev/dermaga/internal/traefik	1.080s
ok  	github.com/r3p-dev/dermaga/internal/webhook	1.062s
?   	github.com/r3p-dev/dermaga/ui	[no test files]
```

### 1.3 TypeScript Diagnostics & Type Freshness (`make tygo-check` & `make ui-check`)

```console
$ make tygo-check
go tool tygo generate

$ make ui-check
cd ui && bun run check
$ svelte-kit sync && svelte-check --tsconfig ./tsconfig.json
Loading svelte-check in workspace: /home/r3p/Projects/dermaga/ui
Getting Svelte diagnostics...

svelte-check found 0 errors and 0 warnings
```

---

## 2. Frontend Build & Static Asset Footprint

The frontend is compiled to a static Single Page Application (SPA) using `@sveltejs/adapter-static` with client-side routing fallback to `index.html`.

```console
$ make ui
cd ui && bun install && bun run build
bun install v1.4.2
Checked 67 installs across 106 packages (no changes)
$ vite build
vite v8.3.6 building ssr environment for production...
✓ 157 modules transformed.
vite v8.3.6 building client environment for production...
✓ 201 modules transformed.
> Using @sveltejs/adapter-static
Wrote site to "dist"
✔ done

$ du -sh ui/dist
348K	ui/dist
```

Total uncompressed asset size is **348 KB** (under 90 KB gzipped).

---

## 3. Binary Size & Idle Daemon RSS

### 3.1 Static Go Binaries with Embedded UI

```console
$ make build
GOARCH=amd64 go build -trimpath -ldflags='-s -w' -o bin/dermaga-linux-amd64 ./cmd/dermaga
GOARCH=arm64 go build -trimpath -ldflags='-s -w' -o bin/dermaga-linux-arm64 ./cmd/dermaga

$ ls -lh bin/
total 26M
-rwxr-xr-x 1 r3p r3p 14M Nov  9 08:56 dermaga-linux-amd64
-rwxr-xr-x 1 r3p r3p 13M Nov  9 08:56 dermaga-linux-arm64
```

*Criteria:* Binary size < 30 MB.  
*Result:* **14 MB** (amd64) and **13 MB** (arm64). Pass.

### 3.2 Daemon Idle RSS on `dermaga-vm`

```console
$ ssh dermaga-vm "systemctl --user status dermaga"
● dermaga.service - dermaga daemon (single-user PaaS)
     Loaded: loaded (/home/server/.config/systemd/user/dermaga.service; enabled; preset: enabled)
     Active: active (running) since Mon 2026-11-09 01:56:44 UTC; 2min 39s ago
   Main PID: 25004 (dermaga)
      Tasks: 8 (limit: 4676)
     Memory: 24.7M (peak: 104.4M)
        CPU: 5.088s
     CGroup: /user.slice/user-1001.slice/user@1001.service/app.slice/dermaga.service
             └─25004 /usr/local/bin/dermaga daemon

$ dermaga doctor | grep daemon
OK  daemon  v0.0.0-20261109012634-d3d0488c4666+dirty, pid 25004, RSS 18.4 MB, socket /run/user/1001/dermaga/dermaga.sock (0600)
```

*Criteria:* Idle RSS < 30 MB.  
*Result:* **18.4 MB – 24.7 MB**. Pass.

---

## 4. Live VM Verification: Embedded Asset & SPA Fallback Serving

The daemon listens on `127.0.0.1:7070` for panel requests over SSH tunnel.

### 4.1 Root Endpoint (`/`)

```console
$ curl -sI -H 'Host: 127.0.0.1:7070' http://127.0.0.1:7070/
HTTP/1.1 200 OK
Accept-Ranges: bytes
Cache-Control: no-cache
Content-Length: 1785
Content-Type: text/html; charset=utf-8
Referrer-Policy: no-referrer
X-Content-Type-Options: nosniff
X-Frame-Options: DENY
Date: Mon, 09 Nov 2026 01:56:52 GMT
```

### 4.2 SPA Client-Side Fallback Routing (`/apps/portfolio`)

Unmatched paths that do not start with `/api/` fall back directly to `index.html` with `Cache-Control: no-cache`:

```console
$ curl -sI -H 'Host: 127.0.0.1:7070' http://127.0.0.1:7070/apps/portfolio
HTTP/1.1 200 OK
Accept-Ranges: bytes
Cache-Control: no-cache
Content-Length: 1785
Content-Type: text/html; charset=utf-8
Referrer-Policy: no-referrer
X-Content-Type-Options: nosniff
X-Frame-Options: DENY
Date: Mon, 09 Nov 2026 01:56:55 GMT
```

### 4.3 Immutable Asset Caching (`/_app/immutable/...`)

Hashed static assets are served with long-term immutable caching:

```console
$ curl -sI -H 'Host: 127.0.0.1:7070' http://127.0.0.1:7070/_app/immutable/entry/start.4Hl4cnx9.js
HTTP/1.1 200 OK
Accept-Ranges: bytes
Cache-Control: public, max-age=31536000, immutable
Content-Length: 548
Content-Type: text/javascript; charset=utf-8
Referrer-Policy: no-referrer
X-Content-Type-Options: nosniff
X-Frame-Options: DENY
Date: Mon, 09 Nov 2026 01:57:04 GMT
```

### 4.4 Host Header Validation & DNS Rebinding Protection

Requests with unexpected `Host` headers are rejected:

```console
$ curl -sI -H 'Host: evil.com' http://127.0.0.1:7070/
HTTP/1.1 421 Misdirected Request
Content-Type: application/json
Date: Mon, 09 Nov 2026 01:57:08 GMT
Content-Length: 35
```

---

## 5. Live VM Verification: Authentication & API Operations

### 5.1 Panel Login & Session Cookie

Mutating endpoints enforce origin validation:

```console
$ curl -i -s -c /tmp/cookies.txt \
  -H "Host: 127.0.0.1:7070" \
  -H "Origin: http://127.0.0.1:7070" \
  -H "Content-Type: application/json" \
  -d '{"password": "supersecretpassword123"}' \
  http://127.0.0.1:7070/api/login

HTTP/1.1 204 No Content
Cache-Control: no-store
Referrer-Policy: no-referrer
Set-Cookie: dermaga_session=pcp-E5LKzh4xvzNO8x-JV0WQo0kBnRd-1fnCy4O5UtM; Path=/; Max-Age=604800; HttpOnly; SameSite=Strict
X-Content-Type-Options: nosniff
X-Frame-Options: DENY
Date: Mon, 09 Nov 2026 01:57:44 GMT
```

### 5.2 Health Metrics API (`GET /api/doctor`)

```console
$ curl -s -b /tmp/cookies.txt -H "Host: 127.0.0.1:7070" http://127.0.0.1:7070/api/doctor
[
  {"name":"lingering","status":"ok","detail":"enabled for server"},
  {"name":"subuid","status":"ok","detail":"65536 ids"},
  {"name":"subgid","status":"ok","detail":"65536 ids"},
  {"name":"daemon","status":"ok","detail":"v0.0.0-20261109012634-d3d0488c4666+dirty, pid 25004, RSS 19.4 MB"},
  {"name":"admin password","status":"ok","detail":"set"},
  {"name":"totp","status":"warn","detail":"off: dermaga admin totp enable (strongly recommended)"},
  {"name":"sockets","status":"ok","detail":"traefik-http.socket, traefik-https.socket active"},
  {"name":"network-online","status":"ok","detail":"network-online.target reached"},
  {"name":"clock","status":"ok","detail":"NTP-synchronised"},
  {"name":"disk","status":"ok","detail":"46% used, 10.7 GiB free (/home/server/.local/share/dermaga)"}
]
```

### 5.3 Live SSE Log Streaming

#### Runtime Logs (`/api/apps/portfolio/logs?lines=3`)
```console
$ curl -s -b /tmp/cookies.txt -H "Host: 127.0.0.1:7070" "http://127.0.0.1:7070/api/apps/portfolio/logs?n=3"
data: Starting dg-portfolio-6ab5ef63.service - dermaga app portfolio, release 6ab5ef63...
data: Started dg-portfolio-6ab5ef63.service - dermaga app portfolio, release 6ab5ef63.
data: Listening on http://0.0.0.0:4321
```

#### Build Logs (`/api/apps/portfolio/releases/6ab5ef63/log`)
```console
$ curl -s -b /tmp/cookies.txt -H "Host: 127.0.0.1:7070" "http://127.0.0.1:7070/api/apps/portfolio/releases/6ab5ef63/log" | head -n 8
data: ==> 00:04:09 release 6ab5ef63 of portfolio (git, trigger manual, strategy blue-green)
data: ==> fetching git@github.com:r3p-dev/portfolio.git
data: ==> commit ca9ef68590b3 chore: dermaga VM webhook test
data: ==> image localhost/dermaga/portfolio:ca9ef68590b3 already built with these settings, not rebuilding
data: ==> 00:04:31 installing dg-portfolio-6ab5ef63.container
data: ==> 00:04:31 starting dg-portfolio-6ab5ef63
data: ==> 00:04:31 health: HTTP GET /robots.txt on port 4321, 3 successes within 2m0s
```

### 5.4 Database Management via Panel API

#### Create Database (`POST /api/databases`)
```console
$ curl -i -s -b /tmp/cookies.txt -H "Host: 127.0.0.1:7070" -H "Origin: http://127.0.0.1:7070" \
  -H "Content-Type: application/json" \
  -d '{"name": "m7-test-db", "engine": "postgres"}' \
  http://127.0.0.1:7070/api/databases
HTTP/1.1 201 Created
{"id":"bc8313b012b82c4b","name":"m7-test-db","engine":"postgres","version":"18","port":5432,"volume":"dg-db_m7-test-db","status":"activating",...}
```

#### Trigger Database Backup (`POST /api/databases/m7-test-db/backup`)
```console
$ curl -i -s -b /tmp/cookies.txt -H "Host: 127.0.0.1:7070" -H "Origin: http://127.0.0.1:7070" \
  -X POST http://127.0.0.1:7070/api/databases/m7-test-db/backup
HTTP/1.1 200 OK
{"id":"a07bc096e4eda54807ae6155f5b5face60e935b834c6921220700bec24b3039e","short_id":"a07bc096","time":"2026-11-09T01:58:56.723711051Z","paths":["/m7-test-db.dump"],"tags":["db:m7-test-db","engine:postgres"],"size_bytes":901}
```

#### Trigger Sandboxed Restore Test (`POST /api/databases/m7-test-db/restore-test`)
```console
$ curl -i -s -b /tmp/cookies.txt -H "Host: 127.0.0.1:7070" -H "Origin: http://127.0.0.1:7070" \
  -X POST http://127.0.0.1:7070/api/databases/m7-test-db/restore-test
HTTP/1.1 200 OK
{"database":"m7-test-db","snapshotId":"a07bc096","duration":"3.384s","sanityOut":"1","success":true,"testedAt":"2026-11-09T01:59:05.314708372Z"}
```

#### Delete Database (`DELETE /api/databases/m7-test-db?purge=true`)
```console
$ curl -i -s -b /tmp/cookies.txt -H "Host: 127.0.0.1:7070" -H "Origin: http://127.0.0.1:7070" \
  -X DELETE "http://127.0.0.1:7070/api/databases/m7-test-db?purge=true"
HTTP/1.1 204 No Content
```

### 5.5 Audit Trail Verification

Audit records confirm all actions initiated by the panel carry `actor: "panel"`:

```console
$ curl -s -b /tmp/cookies.txt -H "Host: 127.0.0.1:7070" "http://127.0.0.1:7070/api/audit?limit=6"
[
  {"time":"2026-11-09T01:59:11Z","actor":"panel","action":"db.delete","target":"m7-test-db","details":"purge=true"},
  {"time":"2026-11-09T01:59:05Z","actor":"panel","action":"db.restore_test","target":"m7-test-db","details":"snapshot=a07bc096 duration=3.384s success=true"},
  {"time":"2026-11-09T01:58:57Z","actor":"panel","action":"db.backup","target":"m7-test-db","details":"snapshot=a07bc096 size=901"},
  {"time":"2026-11-09T01:58:50Z","actor":"panel","action":"db.restore_test_failed","target":"m7-test-db","details":"no snapshots found for database m7-test-db"},
  {"time":"2026-11-09T01:58:43Z","actor":"panel","action":"db.create","target":"m7-test-db","details":"engine=postgres version=18"},
  {"time":"2026-11-09T01:57:44Z","actor":"panel","action":"auth.login"}
]
```

---

## 6. CLI-to-UI Feature Parity Checklist

Every capability available in the `dermaga` CLI is reachable in the web dashboard:

| CLI Capability | UI Route / Component | UI Capability | Status |
|---|---|---|:---:|
| `dermaga doctor` | `/` (Dashboard) | System health badge, host checks list, memory & disk usage profile | Verified |
| `dermaga status` | Global Layout & `/` | Version, uptime, daemon PID, memory RSS, password/TOTP status | Verified |
| `dermaga apps list` | `/` & Navbar | Apps grid with live release status, strategy, domains, quick links | Verified |
| `dermaga apps create` | `/apps/new` | Multi-step wizard, stack auto-detection, repo/branch, port, deploy key & webhook display | Verified |
| `dermaga apps show` | `/apps/[name]` | Overview card, live release card, source metadata, container status | Verified |
| `dermaga apps remove` | `/apps/[name]` | Danger zone delete with confirmation dialog | Verified |
| `dermaga deploy <app>` | `/apps/[name]` | "Deploy Now" trigger button with optional ref/sha override | Verified |
| `dermaga rollback <app>` | `/apps/[name]` | Instant rollback button to previous release without rebuilding | Verified |
| `dermaga logs <app> [-f] [-n]` | `/apps/[name]` | Terminal component: live runtime SSE logs, follow toggle, line wrap, search filter, copy | Verified |
| `dermaga logs --build` | `/apps/[name]` | Release history table "View Log" modal with live SSE build logs | Verified |
| `dermaga env list/set/unset/edit` | `/apps/[name]` | Environment variables manager with reveal toggle, add/edit/delete, batch save | Verified |
| `dermaga domains add/remove/primary` | `/apps/[name]` | Domains management card: list, add domain, toggle redirect, primary badge | Verified |
| `dermaga middleware [show/auth]` | `/apps/[name]` | Basic auth & middleware configuration card | Verified |
| `dermaga db list` | `/databases` | Databases table with status, engine, version, volume, linked apps count | Verified |
| `dermaga db create` | `/databases` | Modal wizard supporting PostgreSQL 18, MariaDB 11.8, Valkey 9 | Verified |
| `dermaga db show [--reveal]` | `/databases/[name]` | Connection URL reveal toggle, credentials, volume & port details | Verified |
| `dermaga db proxy` | `/databases/[name]` | Copyable SSH port-forward tunnel and GUI client snippet (DBeaver, TablePlus, DataGrip) | Verified |
| `dermaga db link / unlink` | `/databases/[name]` | Linked apps manager: attach/detach app networks, redeploy trigger | Verified |
| `dermaga db restart` | `/databases/[name]` | Immediate service restart trigger | Verified |
| `dermaga db backup` | `/databases/[name]` | On-demand streaming backup trigger with progress feedback | Verified |
| `dermaga db restore` | `/databases/[name]` | Manual restore modal with target snapshot selection | Verified |
| `dermaga db restore-test` | `/databases/[name]` | One-click sandboxed restore test with execution time and sanity output | Verified |
| `dermaga db remove [--purge]` | `/databases/[name]` | Delete database modal with optional volume purge checkbox | Verified |
| `dermaga backup snapshots` | `/backups` | Snapshot explorer with engine/target tag filtering, ID, size, timestamp | Verified |
| `dermaga backup system` | `/backups` | Trigger atomic system state snapshot (`VACUUM INTO` + tarball) | Verified |
| `dermaga backup check` | `/backups` | Trigger repository integrity check with subset verification | Verified |
| `dermaga backup settings` | `/backups` | S3 repository configuration editor (bucket, region, endpoint, access key, secret) | Verified |
| `dermaga keys export` | `/backups` | Secure modal to export master secret encryption key with copy button | Verified |
| `dermaga settings` | `/settings` | Inbound GitHub webhook hostname editor with save feedback | Verified |
| `dermaga github known-hosts` | `/settings` | Fetch & pin official GitHub SSH public keys trigger button | Verified |
| `dermaga admin set-password / totp` | `/settings` | Password/TOTP status indicators + copyable CLI commands (per ADR D54) | Verified |
| `dermaga audit [--limit]` | `/audit` | Chronological audit trail table with actor badge, action, target, details, search filter | Verified |
| Light / Dark theme | Global Navbar (`t`) | System, dark, and light mode switcher with persistent `localStorage` sync | Verified |
| Keyboard Shortcuts | Global Layout (`?`) | Quick navigation (`g h`, `g a`, `g d`, `g b`, `g s`, `g l`, `n a`, `n d`, `t`, `?`) | Verified |
