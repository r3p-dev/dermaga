# Server: health, care and updates

dermaga keeps the server healthy on its own and tells the user in the panel when something needs them.

---

## 1. Dashboard (`DASH`)

- **DASH-1** Server overview: CPU, RAM (available, not free), swap, disk, uptime, pending security
  updates, reboot-required flag.
- **DASH-2** App cards: status (`RUN-1`), live release, last deployment, **current** CPU and memory
  (read when the page is open; no history in v1).
- **DASH-3** Warnings in one place: failing `doctor` checks, certificates about to expire (`TLS-4`),
  low disk (`DISK-3`), crashing apps (`RUN-2`).

## 2. Running apps (`RUN`)

- **RUN-1** Each app shows whether its live release is running, starting, restarting or stopped, with
  uptime and restart count.
- **RUN-2** A live release that exits is restarted automatically with backoff. Repeated restarts are
  shown as **crash loop** with the last log lines and a link to the full log.
- **RUN-3** **Restart** button: restarts the live release (brief interruption, stated in the
  confirmation). Use Redeploy for a zero-downtime restart.

## 3. Disk protection (`DISK`)

- **DISK-1** After every deploy and daily, dermaga removes what it no longer needs: images beyond the
  last 5 releases per app, build cache and dangling layers, build directories, mirrors of removed apps.
  It never removes the live or a rollback-able image.
- **DISK-2** Logs have limits: container and daemon logs (journald), build logs (last 20 deployments
  per app), Traefik access log rotated.
- **DISK-3** Disk usage above 85 % shows a warning on the dashboard with what uses the space, measured
  on the filesystem holding Podman storage across five categories (each with size and reclaimable
  estimate): (1) Podman images, containers and build cache (`podman system df`); (2) old releases beyond
  retention; (3) Git mirrors and leftover build workspaces; (4) build logs and the dermaga database;
  (5) other space outside dermaga (total used minus known categories). Cleanup actions are explicit and
  safe (never deleting live or rollback releases). Above 95 % disk usage, new builds are refused with a
  clear message, while running apps keep serving.

## 4. Doctor (`DOC`)

- **DOC-1** One check in the panel (Settings → Health), every failed check with a fix hint: lingering;
  subuids/subgids; sockets; Podman version ≥ the minimum dermaga supports; Traefik running and its
  file provider loaded without errors; `network-online.target` reached at boot; DNS of every domain and
  of the hook domain; GitHub reachable and the App's key valid; clock synchronised; disk usage.
- **DOC-2** Warnings (not failures): TOTP not enabled; a certificate failing to renew.

## 5. Updating dermaga (`UPD`)

- **UPD-1** **Settings → Updates** checks for a new release **only when the user clicks "Check for
  updates"** (`NFR-8`), and shows the version, the release notes and what will happen.
- **UPD-2** "Update" downloads the release for the server's architecture, **refuses it if its SHA-256
  checksum cannot be verified**, replaces dermaga atomically, applies any database migration, and
  restarts the daemon. Every app keeps serving during the update (`REL-11`).
- **UPD-3** If the new version fails to start, the previous version is restored automatically and the
  panel says so.
- **UPD-4** Running the installer of a newer release on an existing server updates it the same way
  (the fallback when the panel can't be reached).
