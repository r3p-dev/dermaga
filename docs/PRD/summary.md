# dermaga: Product Requirements (summary)

The product requirements define **what** dermaga does. They are split by area:

| File                     | Covers                                                                                                   |
| ------------------------ | -------------------------------------------------------------------------------------------------------- |
| `summary.md` (this file) | Product, goals, principles, the user, glossary, the v1.0.0 journey, scope                                |
| `shared.md`              | Rules every area depends on: panel behavior, server commands, audit log, non-functional requirements, release criteria |
| `install-and-access.md`  | Before install, the installer, host hardening, lockout guard, laptop access, first-run setup, admin login, moving from `vps-setup.sh` |
| `apps.md`                | GitHub App, the new-app wizard, app settings, domains and HTTPS, headers and middleware, deploys, logs   |
| `server.md`              | Dashboard, app status and restart, disk protection, `doctor`, updating dermaga                           |

Requirement IDs (`DEP-3`, `TLS-2`, …) are unique across all files. `docs/PLAN.md`, tests and evidence
files refer to them. Security rules are in `docs/ARCHITECTURE.md` §1.2 (`NFR-1`).

---

## 1. Product

**dermaga** (Indonesian for "pier") turns **one fresh Debian 13 VPS** into a hardened, ready-to-use
host, and then lets its owner deploy web apps from GitHub **from a web panel**, again and again,
without memorising commands or editing files. Think "Vercel, but on my own VPS": connect GitHub, pick
a repo, choose a domain, press Deploy. `git push` does the rest.

dermaga runs **only on the server**. The laptop needs nothing but plain `ssh` and a browser.

## 2. Goals for v1.0.0

1. A fresh VPS is secure and ready after one command, with no file to fill in and no way to lock
   yourself out.
2. Connecting GitHub, creating an app and every change after it (env, domains, headers, middleware,
   redeploy, rollback) are done in the panel. **There is no CLI to learn.**
3. `git push` → live, with zero failed requests during the switch; a broken commit never takes a site
   down.
4. dermaga keeps the server healthy on its own (certificates, disk, crashed apps) and tells the user
   in the panel when something needs them.
5. dermaga stays small: the VPS's memory belongs to the apps.

## 3. Principles

These decide every trade-off. No requirement elsewhere overrides them.

1. **Web UI only.** Every task is done in the panel. The only shell commands are the installer and
   three recovery commands (`SRV-1`).
2. **Never assume the user knows what to do.** Every step that needs something from the user (an SSH
   key, a DNS record, a click on GitHub) explains what, why and how, where it is needed, and checks the
   result when it can.
3. **Secure from the first minute, by default.** The panel is never reachable from the internet, not
   even during setup.
4. **A broken change never takes a site down.**
5. **Honest.** The panel shows only what the server returned.
6. **Small.** As little memory at idle as possible.
7. **No files to fill in.** Safe defaults everywhere; questions only where no safe default exists.

## 4. The user

One person per server. No teams, no roles.

1. **The owner first**: one developer with one VPS who wants a Vercel-like experience on rootless
   Podman, without a public admin panel.
2. **Then people who are not experts**: they may not know what an SSH key, a DNS record or a
   Containerfile is, and need every step explained.

## 5. Glossary

Use these words consistently in code, UI and docs.

| Term         | Meaning                                                                                          |
| ------------ | ------------------------------------------------------------------------------------------------ |
| server       | The VPS, and the one Linux user `server` that owns everything on it                              |
| panel        | The web UI, served by the daemon on `127.0.0.1:7070`, reached through `ssh dermaga`              |
| daemon       | The dermaga systemd user service                                                                 |
| GitHub App   | The private GitHub App dermaga creates for this server; it can read the repos the user picks      |
| app          | One stateless container built from a GitHub repo, listening on one HTTP port, on one or more domains |
| release      | One built image of an app, identified by its commit                                              |
| deployment   | One attempt to make a release live, with a status and a log                                      |
| live release | The release currently serving an app's domains                                                   |
| route        | Traefik's configuration for an app: domains, certificates, headers, middleware                   |
| hook domain  | The public domain GitHub sends push events to (`/hooks/github-app`)                              |
| master key   | The key that encrypts dermaga's secrets at rest                                                  |
| setup token  | The single-use token in the installer's first-run link                                           |

## 6. The v1.0.0 journey

Tested end to end on a fresh Debian 13 VM.

1. **Before install**: the README tells the user to add their laptop's SSH key when creating the VPS,
   with a guide for people without a key (`INST-1`).
2. **Install**: log in as the provider says, run one command.
3. The installer reuses the SSH key just used to log in (Enter to accept) or guides the user to paste
   one; asks for the SSH port; nothing about GitHub.
4. It hardens the server and installs everything. It closes the old way in only after a successful
   login with the new key on the new port.
5. It prints one command to paste on the laptop and a first-run link.
6. On the laptop: paste the command once; from then on `ssh dermaga` and `http://localhost:7070`.
7. **First run**: admin password, TOTP, hook domain with a DNS check.
8. **Connect GitHub** with one button: a private GitHub App for this server; the user picks the repos.
9. **New app**: repo → branch → suggested Containerfile and port → env (runtime and build time) →
   domain with a DNS check → Deploy, with a live build log. HTTPS is issued automatically.
10. Every later change is a form.
11. `git push` → live, zero failed requests. A bad push leaves the old release serving; one click
    rolls back.

## 7. Scope

**After v1.0.0** (not committed; each gets its own PRD section first). Roughly in this order:

- **v1.1: databases together with backups** (PostgreSQL, MariaDB, Valkey; scheduled restic backups to
  S3-compatible storage; restore and disaster recovery). Never one without the other.
- **v1.1: build secrets** (`--secret` mounts for `RUN --mount=type=secret` during container builds; deferred from v1.0.0).
- Notifications (ntfy, Telegram, Discord); metrics history and traffic charts; exporting and importing
  the configuration; public images as a source; a template catalog; deploy keys as an alternative to
  the GitHub App; a CI gate (deploy after a GitHub Actions workflow succeeds); apps with volumes and
  the recreate strategy; building without a Containerfile; Tailscale as an extra way to the panel;
  per-app networks.

**Out of scope for v1.0.0**: everything above, plus Git submodules and Git LFS (detected in the wizard with a warning, `WIZ-3`); a user-facing CLI; multi-user, teams, SSO; more
than one server; Docker Compose; Kubernetes; preview environments; non-GitHub Git providers; hosts
other than Debian 13; any public admin panel; a dermaga client on the laptop; telemetry or automatic
update checks; a UI language other than English.
