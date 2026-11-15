# Shared rules

Rules every area depends on.

---

## 1. Panel behavior (`UI`)

- **UI-1** Every task in this PRD is done in the panel. A feature that exists only in the API is not
  done.
- **UI-2** The panel shows only what the server returned: no placeholder results, no hard-coded
  success text, no guessed hosts, ports or values.
- **UI-3** Every view has loading, empty and error states. A server error appears next to the field
  it belongs to, with what to do next.
- **UI-4** Every step that needs something outside the server (an SSH key, a DNS record, a GitHub
  page) explains what, why and how in place, links to the exact page when one exists, and checks the
  result when the server can.
- **UI-5** Secrets are hidden by default; revealing one is an explicit action, recorded in the audit
  log (`AUD-1`).
- **UI-6** Destructive actions need typed confirmation (the name of the thing).
- **UI-7** Live streams (build logs, runtime logs, deployment status) update without reloading.
- **UI-8** Dark and light themes; every action reachable with the keyboard; usable on a narrow screen
  (a phone with an SSH app and port forwarding).

## 2. Server commands (`SRV`)

- **SRV-1** Besides the installer, the user ever needs exactly three commands on the server, all
  documented in the panel and the README: `dermaga panel` (prints the laptop command and a first-run
  link, `SETUP-3`), `dermaga admin set-password` and `dermaga admin totp disable` (recovery,
  `ADM-6`). There is no other user-facing CLI.
- **SRV-2** These commands communicate with the daemon over a local Unix socket (0600), print plain
  English, and exit non-zero on failure.
  - For recovery (`dermaga admin set-password` and `dermaga admin totp disable`): if the daemon
    cannot be reached, they fall back to accessing SQLite directly through the shared store package
    (WAL + `busy_timeout`), revoking all active sessions. Only user `server` can run this as the
    database file is 0600.
  - For `dermaga panel`: requires the daemon. If the daemon is inactive, it prints the service status,
    the last few log lines, and the exact command to start it (`systemctl --user start dermaga`), or
    offers to start it after confirmation.

## 3. Audit log (`AUD`)

- **AUD-1** Every mutating action is recorded with time, origin (panel, webhook, server command,
  daemon) and a short description: logins, settings changes, GitHub connection changes, deploys,
  rollbacks, restarts, env changes, secret reveals, updates. Never a secret value.
- **AUD-2** Viewable and filterable (by origin, by app, by time) in the panel.

## 4. Non-functional requirements (`NFR`)

- **NFR-1 Security**: see `docs/ARCHITECTURE.md` §1.2. Mandatory, overrides convenience.
- **NFR-2 Footprint**: daemon idle RSS < 30 MB; Traefik + daemon < 100 MB, measured after a fresh
  restart. 2 GB RAM is the practical minimum server.
- **NFR-3 Reliability**: everything survives a reboot without intervention. After a daemon crash or
  reboot, dermaga repairs itself on start (`DEP-10`).
- **NFR-4 Outside services can fail**: GitHub and Let's Encrypt each degrade one feature with a clear
  message when they fail; never unrelated pages or actions, and never a running app.
- **NFR-5 Auditability**: `AUD-1`.
- **NFR-6 Language**: the UI and every user-facing message are English, plain, and say what happened
  and what to do next.
- **NFR-7 Accessibility**: keyboard reachable, visible focus, a label on every field, native
  headings, WCAG 2.1 AA contrast.
- **NFR-8 Privacy**: no telemetry, no phone-home; dermaga contacts its release page only when the
  user asks to check for updates (`UPD-1`).

## 5. v1.0.0 release criteria (`REL`)

v1.0.0 is tagged when **all** of these are demonstrated on `dermaga-vm` with evidence:

- **REL-1** Fresh Debian 13 → one install command → `doctor` all green in the panel, **zero files
  edited by hand**, no question that has a safe default.
- **REL-2** The installer cannot lock the user out: tested with a wrong key, with no key, and with the
  confirmation window closed mid-way.
- **REL-3** A port scan from outside shows only the SSH port, 80 and 443 (tcp) and 443 (udp), at every
  moment of install and setup. The panel is reachable only through `ssh dermaga`.
- **REL-4** Laptop: one pasted command, then `ssh dermaga` and the browser are all that is needed.
  After install, no command is typed on the server for any v1 task.
- **REL-5** Connecting GitHub, creating an app and pushing to it work end to end without the panel
  being publicly reachable.
- **REL-6** `git push` → live with **zero non-2xx responses and zero connection errors** at 50 req/s,
  for a successful deploy and for a deploy whose health check fails.
- **REL-7** Certificates are issued and renewed by themselves; a domain whose DNS is wrong shows why
  in the panel.
- **REL-8** Default response headers match what `vps-setup.sh`'s Traefik produces; a header changed in
  the panel shows up in `curl -sI` within seconds.
- **REL-9** `NFR-2` measured.
- **REL-10** Everything survives a reboot without intervention; a crashing app is shown as such and
  can be restarted from the panel.
- **REL-11** Updating dermaga from the previous version keeps every app serving.
