# Migrating from `vps-setup.sh` to `dermaga`

This guide details the step-by-step procedure for migrating an existing production VPS provisioned
with `vps-setup.sh` (using Docker-provider label routing, GHCR-based deployments, and `podman.socket`)
to `dermaga` (file-provider routing, rootless daemon builds, Quadlet units, and automated zero-downtime deploys)
with near-zero downtime and a tested rollback plan.

---

## 1. Architectural Differences & Migration Strategy

| Component | `vps-setup.sh` (Legacy) | `dermaga` (Target) |
|---|---|---|
| **Traefik Providers** | `docker` (watches `/var/run/docker.sock`) + `file` | `file` only (`~/.config/traefik/dynamic/`) |
| **Podman Socket** | `podman.socket` active and mounted into Traefik | `podman.socket` **disabled**; zero socket mounts |
| **Traefik Config** | `00-vps.yml` with entrypoint-level `secure@file` | `00-global.yml` with router-level chains (`spec.DefaultHeaders`) |
| **App Units** | `vps-<app>.container` generated from Compose | `dg-<app>-<rel8>.container` managed by Dermaga daemon |
| **App Routing** | Traefik labels on containers (`traefik.http.*`) | Dynamic files `~/.config/traefik/dynamic/app-<name>.yml` |
| **Build Flow** | GitHub Actions $\to$ GHCR $\to$ SSH deploy hook | Local rootless `podman build` on VPS or pinned image |
| **Databases** | Unmanaged compose services or host packages | Managed Quadlet DBs on isolated bridge networks (`dg-db-*`) |

### Critical Danger & Sequencing Strategy
**The Trap:** If you switch Traefik to file-provider-only before migrating applications, Traefik immediately discards all label-based routes, causing **complete downtime** for all running applications.

**The Solution:**
1. Keep the legacy Traefik setup running (`docker` + `file` providers both active).
2. Re-create and deploy all target applications inside Dermaga. Dermaga generates `app-<name>.yml` in `~/.config/traefik/dynamic/`. Traefik's active file provider loads these routes immediately.
3. Once Dermaga containers are serving traffic, switch Traefik to file-provider-only mode and disable `podman.socket`.
4. Stop and remove the legacy `vps-*` units.

---

## 2. Pre-Migration Checklist & Backup

Execute these commands as the unprivileged user (`server`) on the target VPS before making changes:

### Step 2.1: Create Migration Backup Directory
```bash
mkdir -p ~/migration-backup/{traefik,systemd,apps}
```

### Step 2.2: Backup Traefik Configuration & ACME Certificates
```bash
cp ~/.config/traefik/traefik.yml ~/migration-backup/traefik/traefik.yml.bak
cp -r ~/.config/traefik/dynamic/ ~/migration-backup/traefik/
# Verify Let's Encrypt certificate volume exists
podman volume inspect traefik-acme > ~/migration-backup/traefik/acme-volume-meta.json
```

### Step 2.3: Backup Quadlet Unit Files & Active Container State
```bash
cp ~/.config/containers/systemd/* ~/migration-backup/systemd/ 2>/dev/null || true
podman ps --format "{{.Names}} {{.Image}} {{.Ports}}" > ~/migration-backup/running-containers.txt
```

### Step 2.4: Snapshot Application Databases
If any legacy application uses an attached database, take a manual dump:
```bash
# Example for PostgreSQL
podman exec -t <pg-container> pg_dumpall -U postgres > ~/migration-backup/apps/postgres-all.sql
# Example for MariaDB
podman exec -t <mariadb-container> mariadb-dump --all-databases -u root -p > ~/migration-backup/apps/mariadb-all.sql
```

---

## 3. Step-by-Step Migration Procedure

### Phase 1: Install and Initialize Dermaga
1. Download or compile the `dermaga` static binary to `/usr/local/bin/dermaga` (mode 0755).
2. Install the systemd user service `~/.config/systemd/user/dermaga.service` (or run `sudo install.sh init` if performing full host setup).
3. Enable lingering and start the daemon:
   ```bash
   loginctl enable-linger server
   systemctl --user daemon-reload
   systemctl --user enable --now dermaga.service
   ```
4. Configure administrative credentials and verify doctor:
   ```bash
   dermaga admin set-password
   dermaga doctor
   ```

### Phase 2: Create Applications in Dermaga
For each application currently running under `vps-setup.sh`:

1. **Inspect Legacy Specs:**
   Examine `~/.config/containers/systemd/vps-<app>.container` or `~/apps/<app>/compose.yaml` to identify:
   - Primary domain and aliases (`vps.domain` or `traefik.http.routers...`)
   - Internal port (`vps.port` or container port)
   - Health check path (e.g. `/robots.txt`, `/healthz`)
   - Named volumes and environment variables

2. **Register the App in Dermaga:**
   - **For Git Applications (e.g. `portfolio`, `kapturae`):**
     ```bash
     dermaga apps create <app> \
       --repo <owner/repo> \
       --branch main \
       --port <port> \
       --domain <domain> \
       --health-path <health-path>
     ```
     Add the generated public deploy key (`~/.local/share/dermaga/deploy-keys/<app>.pub`) to GitHub repo settings.

   - **For Image Applications (e.g. `kuma`):**
     ```bash
     dermaga apps create <app> \
       --image <image-ref> \
       --port <port> \
       --domain <domain> \
       --volume <vol-name>:<mount-path> \
       --health-path <health-path>
     ```

3. **Migrate Environment Variables:**
   Copy environment variables from legacy `~/apps/<app>/.env` into Dermaga:
   ```bash
   # Set environment variables securely
   dermaga env set <app> KEY=VALUE ...
   ```

4. **Deploy the Application in Dermaga:**
   ```bash
   dermaga deploy <app>
   ```
   *Note:* The Dermaga candidate will build/pull, start as `dg-<app>-<rel8>`, pass health checks, and generate `~/.config/traefik/dynamic/app-<app>.yml`.
   Because Traefik's file provider is active, Traefik immediately hot-reloads and routes traffic to the Dermaga container!

5. **Verify New Route:**
   ```bash
   curl -sI https://<domain>/
   ```
   Verify HTTP 200 and expected response headers.

---

### Phase 3: Traefik Switch to File-Provider-Only
Now that all applications are active under Dermaga file-provider routing, disconnect the legacy Docker provider.

1. **Verify `00-global.yml` Readiness:**
   Ensure Dermaga has generated `00-global.yml` in `~/.config/traefik/dynamic/00-global.yml`. If `00-vps.yml` is present, rename it:
   ```bash
   mv ~/.config/traefik/dynamic/00-vps.yml ~/migration-backup/traefik/
   ```

2. **Generate Dermaga Static Traefik Configuration:**
   Generate the static config:
   ```bash
   dermaga traefik static > ~/.config/traefik/traefik.yml
   ```
   *What changed:* `providers.docker` is removed, `entryPoints.verify` (:8081) is added, entrypoint middleware `secure@file` is removed (router-level chains take precedence).

3. **Update Traefik Quadlet Unit:**
   Update `~/.config/containers/systemd/traefik.container` to remove the Podman socket:
   - Remove `podman.socket` from `Requires=` and `After=`.
   - Remove `Volume=%t/podman/podman.sock:/var/run/docker.sock`.
   - Ensure `[Quadlet] DefaultDependencies=false` is present.

4. **Restart Traefik:**
   ```bash
   systemctl --user daemon-reload
   systemctl --user restart traefik.service
   ```

5. **Verify Traefik Status & Access Logs:**
   ```bash
   systemctl --user status traefik.service
   journalctl --user -u traefik -n 20 --no-pager
   ```

---

### Phase 4: Retire Legacy Containers & Sockets

1. **Stop and Disable Legacy App Units:**
   ```bash
   systemctl --user stop vps-<app>.service
   rm -f ~/.config/containers/systemd/vps-<app>.container
   ```

2. **Disable `podman.socket`:**
   ```bash
   systemctl --user disable --now podman.socket podman.service
   ```

3. **Reload Systemd User Daemon:**
   ```bash
   systemctl --user daemon-reload
   ```

4. **Verify System Health:**
   ```bash
   dermaga doctor
   ```

---

## 4. Rollback Procedure

If unexpected errors occur during migration (e.g. Traefik fails to reload or custom routes fail), follow this exact rollback procedure to restore legacy service within 60 seconds:

### Step R.1: Stop Dermaga Containers
```bash
dermaga apps list
# Stop and remove candidate units if needed
systemctl --user stop dermaga.service
```

### Step R.2: Restore Legacy Traefik Configuration
```bash
cp ~/migration-backup/traefik/traefik.yml.bak ~/.config/traefik/traefik.yml
cp ~/migration-backup/traefik/00-vps.yml ~/.config/traefik/dynamic/00-vps.yml
rm -f ~/.config/traefik/dynamic/00-global.yml
rm -f ~/.config/traefik/dynamic/app-*.yml
```

### Step R.3: Restore Legacy Traefik Container Unit & Podman Socket
```bash
cp ~/migration-backup/systemd/traefik.container ~/.config/containers/systemd/traefik.container
systemctl --user enable --now podman.socket
```

### Step R.4: Restore Legacy App Units
```bash
cp ~/migration-backup/systemd/vps-*.container ~/.config/containers/systemd/ 2>/dev/null || true
systemctl --user daemon-reload
systemctl --user restart traefik.service
```
Restart legacy app units:
```bash
systemctl --user start vps-<app>.service
```

### Step R.5: Verify Recovery
```bash
curl -sI https://<domain>/
podman ps
```
All legacy services will resume handling traffic via Docker provider labels.
