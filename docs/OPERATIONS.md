# dermaga Operations and Runbook Guide

This document is the operational guide and day-2 runbook for `dermaga`. It covers fresh host provisioning, reverse proxy management, firewall and security configurations, backup orchestration, disaster recovery drills, self-updates, troubleshooting, and the integration test harness.

---

## 1. Architecture and Security Topology

Dermaga is designed for a single-operator Debian 13 host. All application workloads and the reverse proxy run as rootless Podman containers under systemd user manager supervision.

```
Internet ──► :80/:443 (systemd sockets: web, websecure)
               │
               ▼
         Traefik v3 (rootless, file-provider only)
               │
               ├──► App Containers (`dg-<app>-<rel8>`, network "web")
               │
               ├──► Hook Relay Container (`dg-hooks:8080`, network "web")
               │      │ (bind-mount /run/dermaga)
               │      ▼
               │    Unix Socket (`hooks.sock`, 0600)
               │      │
               │      ▼
               └──► dermaga daemon (user "server")
                      ▲
                      │ Unix Socket (`dermaga.sock`, 0600)
                      ├── dermaga CLI (operator on host)
                      │
                      │ TCP 127.0.0.1:7070 (loopback only)
                      └── SSH Local Forwarding (operator laptop)
```

### Port and Exposure Matrix

| Port / Socket | Scope | Bound To | Authentication / Authorization |
| :--- | :--- | :--- | :--- |
| **TCP 22 / 4222** | Host WAN | OpenSSH | ED25519 / RSA Public Keys only; root login disabled |
| **TCP 80** | Host WAN | `traefik-http.socket` | Redirects 301 to HTTPS :443; ACME HTTP-01 challenges |
| **TCP/UDP 443** | Host WAN | `traefik-https.socket` | TLS 1.2+ / HTTP/3; per-app basic auth, IP allowlists |
| **TCP 8081** | Traefik Container | `verify` entrypoint | Internal only; random per-deploy verify token routers (ADR D7) |
| **TCP 7070** | Host Loopback | `127.0.0.1:7070` | Web Dashboard; argon2id admin password + TOTP (RFC 6238) |
| **`dermaga.sock`** | Host Filesystem | `$XDG_RUNTIME_DIR/dermaga/` | Unix domain socket (mode `0600`, user `server`) |
| **`hooks.sock`** | Host Filesystem | `$XDG_RUNTIME_DIR/dermaga/hooks/` | Unix domain socket (mode `0600`, dir `0700`); HMAC-SHA256 |
| **Database Ports** | Isolated Bridges | `dg-db-<name>.network` | Private container network; accessible only by linked apps |

---

## 2. Host Provisioning (Day 0 & Day 1)

### 2.1 Prerequisites
- Clean installation of **Debian 13 (trixie)** (cloud image, VPS, or bare metal).
- One non-root user with passwordless or password sudo (e.g. `server` or `debian`).
- Public SSH key available on the provisioning client.

### 2.2 Configuration Template

Generate a customized configuration file using the installer:

```bash
# Generate template
./install/install.sh config-example > dermaga-host.conf

# Review and edit parameters
nano dermaga-host.conf
```

Key variables to configure in `dermaga-host.conf`:
- `USER_NAME`: Non-root container owner (default: `server`).
- `SSH_PUBLIC_KEY`: Your client SSH public key (`cat ~/.ssh/id_ed25519.pub`).
- `SSH_PORT`: Custom SSH port (e.g. `4222` or `22`).
- `ACME_EMAIL`: Your email address for Let's Encrypt certificates.
- `ACME_STAGING`: Set to `"yes"` during initial testing, `"no"` for production.
- `TIMEZONE`: Host timezone (e.g. `Asia/Jakarta`, `UTC`).

### 2.3 Automated Installation

Run the installer as root or with sudo:

```bash
sudo bash ./install/install.sh init --config dermaga-host.conf
```

Or execute via single-command curl-to-bash:

```bash
curl -fsSL https://raw.githubusercontent.com/r3p-dev/dermaga/main/install/install.sh | sudo bash
```

The installer idempotently executes:
1. Base system package installation (`podman`, `crun`, `passt`, `nftables`, `fail2ban`, `unattended-upgrades`, `restic`).
2. Dedicated user creation, subuid/subgid mapping (65536 IDs), and user lingering (`loginctl enable-linger`).
3. Memory and kernel hardening (`zram`, sysctl security defaults, rootless unprivileged port start at 80).
4. `network-online.target` boot synchronization (ADR D29).
5. Architecture detection (`amd64` or `arm64`), download, SHA-256 verification, and installation of `/usr/local/bin/dermaga`.
6. Traefik file-provider configuration with socket activation (:80/:443) and `DefaultDependencies=false`.
7. Host firewall setup (`nftables` default-deny) and fail2ban jails (`sshd` + `recidive`).
8. Systemd user service installation (`dermaga.service`).
9. `dermaga doctor` pre-flight validation.

### 2.4 First Login & Admin Setup

1. Add the SSH tunnel snippet to your client laptop's `~/.ssh/config`:
   ```ssh
   Host dermaga-panel
       HostName <YOUR_SERVER_IP>
       Port <SSH_PORT>
       User server
       LocalForward 7070 127.0.0.1:7070
       SessionType none
       ExitOnForwardFailure yes
   ```

2. Establish the SSH tunnel:
   ```bash
   ssh -N dermaga-panel
   ```

3. Set your initial administrator password (execute on server via SSH):
   ```bash
   dermaga admin set-password
   ```

4. Enable TOTP two-factor authentication (RFC 6238):
   ```bash
   dermaga admin totp enable
   ```

5. Open `http://localhost:7070` in your browser and sign in.

---

## 3. Reverse Proxy & Traefik File Provider

Dermaga manages Traefik strictly through the **file provider** (ADR D27). Traefik has no access to the Podman API socket, and `podman.socket` is permanently disabled.

### Directory Structure
- `~/.config/traefik/traefik.yml`: Static configuration (entrypoints, certificatesResolvers, log formatting).
- `~/.config/traefik/dynamic/00-global.yml`: Global security chains, TLS minimum version, rate limiters.
- `~/.config/traefik/dynamic/app-<name>.yml`: Per-app dynamic routes and middleware (generated by Dermaga).
- `~/.config/traefik/dynamic/99-custom.yml`: User-managed custom routes (hot-reloaded; never overwritten by Dermaga).

### Manual Traefik Operations
```bash
# View Traefik container logs
journalctl --user -u traefik -f

# Verify Quadlet generated units
/usr/libexec/podman/quadlet -dryrun -user

# Re-render static traefik.yml
dermaga traefik static --acme-email user@example.com > ~/.config/traefik/traefik.yml
systemctl --user restart traefik
```

---

## 4. Application Deployments & Lifecycle

### 4.1 Creating Apps
```bash
# Git repository app
dermaga apps create myapp --source git --repo git@github.com:user/repo.git --branch main --port 3000

# Container image app
dermaga apps create uptime --source image --image docker.io/louislam/uptime-kuma:1 --port 3001
```

### 4.2 Domains & Middleware
```bash
# Add domain
dermaga domains myapp add app.example.com --primary

# Add redirect domain
dermaga domains myapp add www.example.com --redirect

# Add basic authentication
dermaga middleware myapp basic-auth add developer
```

### 4.3 Environment Variables
```bash
# Set secrets
dermaga env myapp set DATABASE_URL=postgres://... API_KEY=secret123

# List environment variables (secrets masked by default)
dermaga env myapp list

# Reveal secret values (audited)
dermaga env myapp list --reveal
```

### 4.4 Deployment & Rollback
```bash
# Trigger immediate deployment
dermaga deploy myapp

# Stream deployment logs
dermaga logs myapp -f --build latest

# Instant rollback to previous release (under 10 s, skips rebuild)
dermaga rollback myapp

# Rollback to specific historical release
dermaga rollback myapp --to a1b2c3d4
```

---

## 5. Database Management & Tunnels

Each database engine runs in an isolated rootless container on a private bridge network (`dg-db-<name>.network`). Database ports are never published to host interfaces or the internet.

### 5.1 Supported Engines
- PostgreSQL 18 (`--engine postgres`)
- MariaDB 11.8 LTS (`--engine mariadb`)
- Valkey 9 (`--engine valkey`)

### 5.2 Provisioning & Linking
```bash
# Create database
dermaga db create mydb --engine postgres

# Link database to application (injects DATABASE_URL and triggers zero-downtime redeploy)
dermaga db link mydb myapp

# Interactive shell inside container
dermaga db shell mydb
```

### 5.3 Desktop GUI Database Proxy (ADR D49)
To connect GUI tools (DBeaver, TablePlus, DataGrip, pgAdmin) securely over SSH without opening firewall ports:

```bash
# On server: start rootless bridge proxy on local port
dermaga db proxy mydb --port 5432

# On laptop: port-forward over SSH
ssh -L 5432:127.0.0.1:5432 -p <SSH_PORT> server@<HOST>

# Connect your desktop client to localhost:5432 with credentials from:
dermaga db show mydb --reveal
```

---

## 6. Backup, Verification & Restore

Backups utilize `restic` streaming directly over stdin without buffering intermediate dumps to the host filesystem (ADR D10).

### 6.1 Configuring S3 Storage
```bash
dermaga backup settings \
  --repo s3:https://<account-id>.r2.cloudflarestorage.com/dermaga-backup \
  --access-key <KEY> \
  --secret-key <SECRET> \
  --password <RESTIC_PASSWORD>
```

### 6.2 Backup Timers & Schedules
- Automatic schedules run daily at `03:00` Jakarta time with randomized delay (`RandomizedDelaySec=1800`) to prevent load spikes.
- Check timer status:
  ```bash
  systemctl --user list-timers "dg-backup-*"
  ```

### 6.3 Manual Backup & Verification
```bash
# Run manual database backup
dermaga backup run db:mydb

# Run full system configuration backup
dermaga backup system

# Verify backup repository integrity
dermaga backup check

# Sandboxed test restore (creates isolated throwaway container and verifies data)
dermaga db restore-test mydb
```

### 6.4 Encryption Key Export
The master secret key (`~/.config/dermaga/master.key`) decrypts database credentials and application secrets. It is strictly excluded from backups.
```bash
# Export key for safekeeping in password manager / vault
dermaga keys export
```

---

## 7. Disaster Recovery Runbook

In the event of complete server loss, follow this procedure to restore full service on a fresh Debian 13 host.

### Step 1: Provision Clean Server
Run the installer on the new host:
```bash
sudo bash ./install/install.sh init --config dermaga-host.conf
```

### Step 2: Restore Master Encryption Key
Create the master key file before restoring secrets:
```bash
install -d -m 0700 ~/.config/dermaga
printf '%s' "<HEX_ENCODED_32_BYTE_KEY>" | xxd -r -p > ~/.config/dermaga/master.key
chmod 0600 ~/.config/dermaga/master.key
```

### Step 3: Configure Restic Credentials
Configure restic repository environment variables or run:
```bash
dermaga backup settings --repo s3:... --access-key ... --secret-key ... --password ...
```

### Step 4: Restore System State
Unpack the latest consistent system state archive from restic:
```bash
dermaga restore-system -y
```

### Step 5: Restore Databases
Restore database engines and data volumes:
```bash
dermaga db restore mydb -y
```

### Step 6: Redeploy Applications
Rebuild and launch applications from their bare git mirrors:
```bash
dermaga deploy myapp
```

### Step 7: Verify Recovery
```bash
dermaga doctor
curl -sI https://app.example.com
```

---

## 8. Self-Update & Release Management

### 8.1 Checking for Updates
```bash
dermaga self-update --check
```
Output:
```
Current version: v0.8.0
Latest version:  v0.8.1
Update available: v0.8.0 -> v0.8.1
Run 'dermaga self-update' to install.
```

### 8.2 Applying Updates
```bash
# Update to latest official release
sudo dermaga self-update

# Update to a specific tagged version
sudo dermaga self-update --version v0.8.1

# Force re-installation
sudo dermaga self-update --force
```

### 8.3 Rollback Safety
The `self-update` engine:
1. Downloads the architecture-matching binary (`dermaga-linux-amd64` or `dermaga-linux-arm64`) and `checksums.txt`.
2. Computes and strictly verifies the SHA-256 checksum. If corrupted or tampered, it halts immediately without touching the live system.
3. Performs a dry-run execution (`tempBinary version`).
4. Atomically replaces `/usr/local/bin/dermaga` with automatic fallback to `/usr/local/bin/dermaga.old` if replacement fails.
5. Synchronizes the hook relay binary (`~/.local/share/dermaga/relay/dermaga`).
6. Restarts `dermaga.service` systemd user unit.

---

## 9. Troubleshooting & Diagnostics Matrix

| Symptom | Diagnostic Command | Likely Cause | Recommended Resolution |
| :--- | :--- | :--- | :--- |
| **`dermaga doctor` reports lingering off** | `loginctl show-user $USER` | User lingering disabled | Run `sudo loginctl enable-linger server` |
| **Subuid/subgid count < 65536** | `cat /etc/subuid` | Missing range mapping | Run installer or add `server:100000:65536` to `/etc/subuid` and `/etc/subgid` |
| **Traefik starts 90 seconds late at boot** | `systemctl show -p ActiveEnterTimestampMonotonic network-online.target` | `network-online.target` unsynchronized | Run `sudo systemctl add-wants multi-user.target network-online.target` (ADR D29) |
| **Ports 80/443 connection refused** | `systemctl --user status traefik-*.socket` | Socket units not active | Run `systemctl --user enable --now traefik-http.socket traefik-https.socket` |
| **HTTP 502 Bad Gateway on app domain** | `podman logs dg-<app>-<rel8>` | Application crashed or wrong port | Verify `PORT` setting in app spec (`dermaga apps show <app>`) and container logs |
| **Deploy verification timeout (ADR D7)** | `journalctl --user -u traefik -n 50` | Invalid custom dynamic YAML syntax | Check `journalctl --user -u traefik` and fix syntax errors in `~/.config/traefik/dynamic/99-custom.yml` |
| **Webhook returns 401 Unauthorized** | `dermaga audit --limit 10` | Signature HMAC mismatch | Ensure GitHub webhook secret matches `dermaga apps webhook <app>` |
| **Database connection refused by linked app** | `podman inspect dg-db-<name>` | Containers not on same network | Check `dermaga db show <name>` and verify `dg-db-<name>.network` is attached to app |
| **Backup failure alert in audit log** | `journalctl --user -u dg-backup-*` | S3 network timeout or wrong credentials | Verify bucket reachability via `dermaga backup check` and inspect restic credentials |

---

## 10. Integration Test Harness

The Dermaga integration harness runs against `dermaga-vm` (`ssh dermaga-vm`).

### Libvirt Snapshot Management
Snapshots maintained on the host (`qemu:///system`):
- `fresh`: Bare Debian 13 cloud image, only cloud-init user `debian`. Used for full installer and disaster recovery tests.
- `vps-setup`: Post-installation baseline. Used for feature regression tests.

To revert the VM for an installer drill:
```bash
# 1. Revert to fresh snapshot (confirm before executing)
virsh --connect qemu:///system snapshot-revert dermaga-vm fresh

# 2. Ensure VM restarts on reboot (snapshots carry on_reboot=destroy)
virt-xml --connect qemu:///system dermaga-vm --edit --events on_reboot=restart

# 3. Verify VM connectivity
ssh -p 4222 debian@dermaga-vm "uname -a"
```
