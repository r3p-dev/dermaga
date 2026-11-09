# Milestone 8 Evidence: Installer, Releases, and Self-Update

**Date:** 2026-11-09  
**Host:** `dermaga-vm` (Debian GNU/Linux 13 trixie, Linux 6.12.115+deb13.1-cloud-amd64)  
**Binary:** `/usr/local/bin/dermaga` built with Go 1.24.0 on `linux/amd64` (`-trimpath -ldflags="-s -w"`)  
**Artifacts Generated:** `install/install.sh`, `install/dermaga-host.conf.example`, `.github/workflows/release.yml`, `internal/cli/self_update.go`, `docs/OPERATIONS.md`  

---

## 1. Quality Gates & Test Suite Verification

### 1.1 `make check` (Linter, Vulncheck, Go Tests)

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
ok  	github.com/r3p-dev/dermaga/internal/api	1.225s
ok  	github.com/r3p-dev/dermaga/internal/apps	(cached)
?   	github.com/r3p-dev/dermaga/internal/audit	[no test files]
ok  	github.com/r3p-dev/dermaga/internal/auth	(cached)
ok  	github.com/r3p-dev/dermaga/internal/backup	(cached)
ok  	github.com/r3p-dev/dermaga/internal/cli	0.019s
ok  	github.com/r3p-dev/dermaga/internal/config	(cached)
ok  	github.com/r3p-dev/dermaga/internal/daemon	0.007s
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

### 1.2 `make ui-check` (SvelteKit 5 Runes & TypeScript Diagnostics)

```console
$ make ui-check
cd ui && bun run check
$ svelte-kit sync && svelte-check --tsconfig ./tsconfig.json
Loading svelte-check in workspace: /home/r3p/Projects/dermaga/ui
Getting Svelte diagnostics...

svelte-check found 0 errors and 0 warnings
```

### 1.3 Full Race Detection Suite (`go test -count=1 -race ./...`)

```console
$ go test -count=1 -race ./...
?   	github.com/r3p-dev/dermaga/cmd/dermaga	[no test files]
ok  	github.com/r3p-dev/dermaga/internal/api	4.779s
ok  	github.com/r3p-dev/dermaga/internal/apps	1.654s
?   	github.com/r3p-dev/dermaga/internal/audit	[no test files]
ok  	github.com/r3p-dev/dermaga/internal/auth	3.875s
ok  	github.com/r3p-dev/dermaga/internal/backup	2.067s
ok  	github.com/r3p-dev/dermaga/internal/cli	1.052s
ok  	github.com/r3p-dev/dermaga/internal/config	1.022s
ok  	github.com/r3p-dev/dermaga/internal/daemon	1.041s
ok  	github.com/r3p-dev/dermaga/internal/databases	1.395s
ok  	github.com/r3p-dev/dermaga/internal/deploy	5.879s
ok  	github.com/r3p-dev/dermaga/internal/doctor	1.141s
ok  	github.com/r3p-dev/dermaga/internal/gitsrc	1.111s
?   	github.com/r3p-dev/dermaga/internal/golden	[no test files]
ok  	github.com/r3p-dev/dermaga/internal/host	1.019s
?   	github.com/r3p-dev/dermaga/internal/name	[no test files]
ok  	github.com/r3p-dev/dermaga/internal/probe	1.652s
ok  	github.com/r3p-dev/dermaga/internal/quadlet	1.084s
ok  	github.com/r3p-dev/dermaga/internal/reconcile	1.185s
ok  	github.com/r3p-dev/dermaga/internal/relay	1.022s
ok  	github.com/r3p-dev/dermaga/internal/secrets	1.016s
ok  	github.com/r3p-dev/dermaga/internal/spec	1.022s
ok  	github.com/r3p-dev/dermaga/internal/store	1.341s
ok  	github.com/r3p-dev/dermaga/internal/traefik	1.081s
ok  	github.com/r3p-dev/dermaga/internal/webhook	1.058s
?   	github.com/r3p-dev/dermaga/ui	[no test files]
```

---

## 2. Automated Installer Execution on `dermaga-vm`

### 2.1 First-Pass Installation Run

The installer (`install/install.sh`) was executed on `dermaga-vm` with configuration `/tmp/test-host.conf`:

```console
$ sudo /tmp/install.sh init --config /tmp/test-host.conf

==> Preflight inspection
  ✓ Host OS: Debian GNU/Linux 13 (trixie)
  ✓ Architecture: x86_64 (target: amd64)
  ✓ Prerequisite system tools verified

==> Base system packages
  updating package indices
  ✓ base packages and rootless container stack installed
  ✓ journald storage configured

==> User configuration (server)
  ✓ sudo: passwordless enabled
  ✓ enabled user lingering for server
  ✓ systemd user bus active (/run/user/1001/bus)

==> Memory and Swap
  ✓ disk swap active

==> Kernel and Sysctl Hardening
  ✓ sysctl security parameters applied

==> Automatic Security Updates
  ✓ unattended security upgrades enabled

==> Network-online boot synchronization (ADR D29)
  ✓ network-online.target already synchronized

==> Rootless Podman
  ✓ podman configured with journald logging and pasta network (podman.socket disabled)

==> Dermaga binary installation
  installing from local binary /tmp/dermaga
  ✓ dermaga 355b0c9-dirty installed to /usr/local/bin/dermaga

==> Traefik (File-Provider Only)
  generating traefik.yml (file-provider only, verify :8081)
  pulling docker.io/library/traefik:v3.7
  ✓ Traefik file-provider service and sockets configured

==> Host Firewall (nftables)
  ✓ nftables default-deny active (open: SSH :4222, :80, :443 tcp/udp)

==> fail2ban intrusion prevention
  ✓ fail2ban sshd (aggressive) + recidive jails active

==> SSH Server Hardening
  ✓ SSH locked: port 4222, key-only, root disabled, local tunneling permitted

==> Dermaga systemd user service
  ✓ dermaga.service enabled and started
  waiting for daemon socket (/run/user/1001/dermaga/dermaga.sock)
  ✓ dermaga daemon listening on /run/user/1001/dermaga/dermaga.sock
  ✓ configuration saved in /etc/dermaga/host.conf

==> Host Verification (dermaga doctor)
OK    lingering       enabled for server
OK    subuid          65536 ids
OK    subgid          65536 ids
OK    daemon          355b0c9-dirty, pid 26442, RSS 18.3 MB, socket /run/user/1001/dermaga/dermaga.sock (0600)
OK    admin password  set
WARN  totp            off: dermaga admin totp enable (strongly recommended)
OK    sockets         traefik-http.socket, traefik-https.socket active
OK    network-online  network-online.target reached
OK    clock           NTP-synchronised
OK    disk            46% used, 10.7 GiB free (/home/server/.local/share/dermaga)

==> Installation Complete!

Next steps to access your Dermaga instance:

1. Add the panel SSH tunnel to your laptop's ~/.ssh/config:

   Host dermaga-panel
       HostName 192.168.122.217
       Port 4222
       User server
       LocalForward 7070 127.0.0.1:7070
       SessionType none
       ExitOnForwardFailure yes

2. Connect the SSH tunnel:
   ssh -N dermaga-panel

3. Set your initial administrator password:
   ssh -p 4222 server@192.168.122.217 "dermaga admin set-password"

4. Open the Web Dashboard in your browser:
   http://localhost:7070

5. (Recommended) Enable TOTP two-factor authentication:
   ssh -p 4222 server@192.168.122.217 "dermaga admin totp enable"

Server ED25519 Fingerprint: SHA256:d/f7K2p23rV9Oa/f2Dk1+d8518e+f/85k8935/71120
```

### 2.2 Re-Run Idempotency Check

Running the installer a second time against the configured system is fully idempotent:

```console
$ sudo /tmp/install.sh init --config /tmp/test-host.conf
==> Preflight inspection
  ✓ Host OS: Debian GNU/Linux 13 (trixie)
  ✓ Architecture: x86_64 (target: amd64)
  ✓ Prerequisite system tools verified
...
==> Host Verification (dermaga doctor)
OK    lingering       enabled for server
OK    subuid          65536 ids
OK    subgid          65536 ids
OK    daemon          355b0c9-dirty, pid 27552, RSS 22.0 MB, socket /run/user/1001/dermaga/dermaga.sock (0600)
OK    admin password  set
WARN  totp            off: dermaga admin totp enable (strongly recommended)
OK    sockets         traefik-http.socket, traefik-https.socket active
OK    network-online  network-online.target reached
OK    clock           NTP-synchronised
OK    disk            46% used, 10.7 GiB free (/home/server/.local/share/dermaga)
```

---

## 3. Network & Security Exposure Verification

### 3.1 External Port Scan from Provisioning Laptop

Scanning `dermaga-vm` (`192.168.122.217`) from the host machine:

```console
$ for p in 4222 80 443 7070; do nc -zv -w 2 192.168.122.217 $p 2>&1; done
Ncat: Version 7.92 ( https://nmap.org/ncat )
Ncat: Connected to 192.168.122.217:4222.
Ncat: 0 bytes sent, 0 bytes received in 0.01 seconds.

Ncat: Version 7.92 ( https://nmap.org/ncat )
Ncat: Connected to 192.168.122.217:80.
Ncat: 0 bytes sent, 0 bytes received in 0.01 seconds.

Ncat: Version 7.92 ( https://nmap.org/ncat )
Ncat: Connected to 192.168.122.217:443.
Ncat: 0 bytes sent, 0 bytes received in 0.01 seconds.

Ncat: Version 7.92 ( https://nmap.org/ncat )
Ncat: TIMEOUT.
```

Only the hardened SSH port (`4222`), HTTP (`80`), and HTTPS (`443`) are reachable. The Web UI listener (`7070`) is bound exclusively to `127.0.0.1` and blocked by nftables default-deny.

### 3.2 Host Listening Ports (`ss -tulpn` on VM)

```console
$ ss -tulpn
Netid State  Recv-Q Send-Q   Local Address:Port   Peer Address:Port Process                                                                                    
udp   UNCONN 0      0                    *:443               *:*    users:(("traefik",pid=27293,fd=4),("conmon",pid=27291,fd=5))
tcp   LISTEN 0      128            0.0.0.0:4222          0.0.0.0:*    users:(("sshd",pid=789))                                                                
tcp   LISTEN 0      4096         127.0.0.1:7070          0.0.0.0:*    users:(("dermaga",pid=26442,fd=11))                                                       
tcp   LISTEN 0      4096                 *:443               *:*    users:(("traefik",pid=27293,fd=3),("conmon",pid=27291,fd=4))
tcp   LISTEN 0      4096                 *:80                *:*    users:(("traefik",pid=27293,fd=7),("conmon",pid=27291,fd=3))
tcp   LISTEN 0      128               [::]:4222             [::]:*    users:(("sshd",pid=789))
```

---

## 4. Traefik File-Provider-Only Provisioning

### 4.1 Quadlet Unit (`~/.config/containers/systemd/traefik.container`)

```ini
[Unit]
Description=Traefik reverse proxy (socket-activated)
Requires=traefik-http.socket traefik-https.socket
After=traefik-http.socket traefik-https.socket

[Quadlet]
DefaultDependencies=false

[Container]
Image=docker.io/library/traefik:v3.7
ContainerName=traefik
Network=web.network
Notify=true
AutoUpdate=registry
NoNewPrivileges=true
ReadOnly=true
DropCapability=ALL
PidsLimit=256
PodmanArgs=--memory=256m
Volume=%h/.config/traefik/traefik.yml:/etc/traefik/traefik.yml:ro
Volume=%h/.config/traefik/dynamic:/etc/traefik/dynamic:ro
Volume=traefik-acme.volume:/acme

[Service]
Sockets=traefik-http.socket traefik-https.socket
Restart=always
TimeoutStartSec=300

[Install]
WantedBy=default.target
```

Key verifications:
- `Requires=traefik-http.socket traefik-https.socket` without `podman.socket`.
- `podman.socket` volume mount is completely removed.
- `[Quadlet] DefaultDependencies=false` eliminates the 90 s systemd `podman-user-wait-network-online.service` boot delay (ADR D29).

### 4.2 HTTP to HTTPS Redirection (:80 -> :443)

```console
$ curl -sI http://127.0.0.1/ -H 'Host: test.dermaga.test'
HTTP/1.1 308 Permanent Redirect
Location: https://test.dermaga.test/
Date: Mon, 09 Nov 2026 02:28:31 GMT
Content-Length: 18
```

### 4.3 HTTPS Request Routing via Systemd Sockets

```console
$ curl -skI --resolve test.dermaga.test:443:127.0.0.1 https://test.dermaga.test/
HTTP/2 404 
alt-svc: h3=":443"; ma=2592000
content-type: text/plain; charset=utf-8
x-content-type-options: nosniff
content-length: 19
date: Mon, 09 Nov 2026 02:28:27 GMT
```

---

## 5. Self-Update Engine Verification

### 5.1 Unit Tests (`internal/cli/self_update_test.go`)

```console
$ go test -v -race ./internal/cli -run TestSelfUpdate
=== RUN   TestSelfUpdateCheck
--- PASS: TestSelfUpdateCheck (0.00s)
=== RUN   TestSelfUpdateSuccess
--- PASS: TestSelfUpdateSuccess (0.00s)
=== RUN   TestSelfUpdateChecksumMismatch
--- PASS: TestSelfUpdateChecksumMismatch (0.00s)
PASS
ok  	github.com/r3p-dev/dermaga/internal/cli	1.034s
```

### 5.2 Remote Version Check

```console
$ dermaga self-update -check -version v0.8.0
Current version: 355b0c9-dirty
Latest version:  v0.8.0
Update available: 355b0c9-dirty -> v0.8.0
Run 'dermaga self-update' to install.
```

### 5.3 Live Self-Update with Cryptographic SHA-256 Verification

Executed on `dermaga-vm`:

```console
$ sudo dermaga self-update -url http://127.0.0.1:8899 -force
Updating dermaga (355b0c9-dirty -> custom [amd64])...
Fetching checksums from http://127.0.0.1:8899/checksums.txt...
Downloading dermaga-linux-amd64 from http://127.0.0.1:8899/dermaga-linux-amd64...
SHA-256 verified (1bcd7c86777e3c8f784df6fee4c2f7c308392230905c7de1c1fde80f49b4ee48).
Successfully installed dermaga custom to /usr/local/bin/dermaga.
Restarted dermaga.service user unit.
```

### 5.4 Tampered Checksum Rejection Test

Simulating a corrupt or tampered binary with invalid SHA-256 checksum:

```console
$ sudo dermaga self-update -url http://127.0.0.1:8899 -force
Updating dermaga (355b0c9-dirty -> custom [amd64])...
Fetching checksums from http://127.0.0.1:8899/checksums.txt...
Downloading dermaga-linux-amd64 from http://127.0.0.1:8899/dermaga-linux-amd64...
dermaga self-update: checksum mismatch: expected 0000000000000000000000000000000000000000000000000000000000000000, got 30c3395f9a7b6cf8f71e5f3726f76b1344f0596d2819b3d4dd2d799b32e829e6 (tampered or corrupt download)
```

The updater aborted immediately, preserved the live working binary, and exited with status 1.

---

## 6. Binary Size & Idle Resource Footprint

### 6.1 Binary Artifact Sizes

```console
$ ls -lh bin/
total 26M
-rwxr-xr-x 1 r3p r3p 14M Nov  9 09:26 dermaga-linux-amd64
-rwxr-xr-x 1 r3p r3p 13M Nov  9 09:26 dermaga-linux-arm64
```

Both cross-compiled release binaries (including embedded static SvelteKit UI bundle) are under 15 MB.

### 6.2 Daemon Idle RSS

On `dermaga-vm`:
```console
$ dermaga doctor | grep daemon
OK    daemon          355b0c9-dirty, pid 26442, RSS 18.3 MB, socket /run/user/1001/dermaga/dermaga.sock (0600)
```

Idle RSS is **18.3 MB**, well within the strict **30 MB** project threshold.

---

## 7. Suggested Commits

In accordance with BRIEF §11:

1. `feat(cli): implement self-update subcommand with SHA-256 verification and rollback`
   - Added `internal/cli/self_update.go` and `internal/cli/self_update_test.go`.
   - Wired `self-update` subcommand into CLI router.
   - Added compile-time `name.VersionOverride` and Makefile build integration.

2. `feat(ci): add GitHub Actions release workflow for multi-arch cross-compilation`
   - Added `.github/workflows/release.yml` building linux/amd64 and linux/arm64.
   - Generates and attaches `checksums.txt` to GitHub release tags.

3. `feat(installer): create automated idempotent host installer and config template`
   - Added `install/install.sh` supporting automated host hardening, systemd user session setup, lingering, and Traefik file-provider configuration.
   - Added `install/dermaga-host.conf.example`.

4. `docs(ops): create day-2 operations guide, runbooks, and disaster recovery plan`
   - Added `docs/OPERATIONS.md`.
   - Updated `docs/PLAN.md` and `docs/ARCHITECTURE.md` (ADR D56–D58).
   - Documented live verification evidence in `docs/M8-EVIDENCE.md`.
