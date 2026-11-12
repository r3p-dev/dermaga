# dermaga v2 brief: a ready, secure VPS with GUI-first deploys

This brief sets the direction for v2. An earlier proposal (never committed to this repository)
explored a Tailscale-native panel, five ingestion channels, Compose and Railpack; §2.3 records which
of those ideas are rejected and why. This brief keeps what v1 got right, finishes what v1 left
undone, and sets the direction for v2. Read it fully, together with `docs/BRIEF.md` (v1), before
doing anything. Where the two disagree, this file wins; where this file is silent, v1 still holds.

---

## 1. What I want

One person installs dermaga on a **fresh VPS** and gets, without reading a manual:

1. **A server that is ready and secure from the first minute.** Hardening, firewall, rootless
   containers, reverse proxy, backups: done by the installer, with safe defaults.
2. **Easy access to that server.** No config files to fill in, no long commands to memorise.
   One copy-paste on the laptop, once. After that: `ssh dermaga`, then open
   `http://localhost:7070`.
3. **GUI-first deploys, again and again.** Pick a GitHub repo from a list, choose a domain, press
   Deploy. Every later change (env vars, domains, headers, basic auth, rollbacks, backups) is a
   form in the web UI. Typing in a form is not the same as opening `nano` and pasting a header
   block; the UI exists so I never have to do the latter.

Think "Vercel, but on my own VPS". The CLI stays, with the same capabilities, for people who like
terminals, but **no task may require it**.

Audience: me first, then other people who are not experts. **Never assume the user already knows
what to do** (SSH keys, DNS, GitHub settings). Every step that needs something from the user
explains what, why, and how, in the place where it is needed.

---

## 2. Decisions

### 2.1 Kept from v1 (do not re-litigate)

Debian 13 only (x86_64, aarch64) · exactly one Linux user (`server`) · rootless Podman + Quadlet
under the systemd user manager · Traefik v3, file provider only, socket activation for :80/:443 ·
Go daemon + CLI in one static binary (`CGO_ENABLED=0`, stdlib first) · SQLite via
`modernc.org/sqlite` · SvelteKit (adapter-static, Svelte 5 runes, Tailwind 4) built with Bun and
embedded · CLI over a 0600 Unix socket · **panel bound to `127.0.0.1:7070`, reached only through
SSH local forwarding** · one app = one container on one HTTP port · PostgreSQL, MariaDB, Valkey ·
restic to S3-compatible storage via systemd user timers · no telemetry · Apache-2.0 · blue-green
default, `recreate` for apps with named volumes · per-app Traefik middleware from typed structs.

### 2.2 Changed in v2

| Area                          | v1                                                                       | v2                                                                                                                                          | Why                                                            |
| ----------------------------- | ------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------- |
| Product principle             | CLI first, UI with parity                                                | **GUI-first**: every task doable in the UI; CLI keeps parity                                                                                | The target user does not want to memorise commands             |
| Installer input               | `install.sh config-example` → edit `dermaga-host.conf` → `init --config` | **Interactive** with defaults; `--config` stays for automation only                                                                         | No file to fill in                                             |
| SSH key for the `server` user | Collected from config                                                    | **Reused from the key the user just logged in with** (`authorized_keys` of root / the sudo user); otherwise guided paste                    | No question about GitHub at install time; works without GitHub |
| Lockout protection            | Checks before disabling passwords                                        | Same checks **plus** the installer waits until it sees a successful key login as `server` on the new SSH port before closing anything       | A beginner must not lock themselves out                        |
| Laptop setup                  | Print a `~/.ssh/config` snippet                                          | Print **one copy-paste command** that writes a `Host dermaga` block (with `LocalForward`) into `~/.ssh/config`                              | Daily use becomes `ssh dermaga`                                |
| First admin password          | `dermaga admin set-password` over SSH                                    | **First-run setup page** in the panel, unlocked by a single-use token printed by the installer; TOTP offered right there                    | No command needed; the CLI command stays for recovery          |
| GitHub integration            | Per-app deploy key + per-app webhook added by hand                       | **GitHub App** created from the panel with the manifest flow, repo picker in the wizard. Deploy key + webhook stays as an "Advanced" source | No key to add to each project                                  |
| Non-GitHub users              | `image` source only                                                      | `image` source **plus a small curated template catalog** (forms, pinned images)                                                             | Most non-experts want to run ready-made apps                   |
| Repos without a Containerfile | Not supported                                                            | **Railpack**, only after a spike proves it works rootless without raising idle memory                                                       | Vercel-like "just deploy"                                      |

### 2.3 Rejected ideas from the earlier proposal (do not implement)

| Idea                                                      | Why it is rejected                                                                                                       |
| --------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------ |
| Panel on the public/LAN IP during onboarding with a token | On a VPS that is the public internet. Zero public panel exposure is a hard rule, also during setup.                      |
| Panel without password/TOTP ("Tailscale Whois only")      | A single compromised tailnet device or account would open the panel. Network access never replaces login.                |
| GitLab App, Public Git URL, archive upload                | Not needed for one GitHub user; each adds code and attack surface. Public Git URL may come back later as a small option. |
| Docker Compose → multi-unit Quadlet                       | Breaks "one app = one container" and the blue-green pipeline; a Compose allowlist is a large attack surface.             |
| Debian 12 / Ubuntu / Fedora / Arch hosts                  | The hardening is written and tested for Debian 13 only.                                                                  |
| `curl https://get.dermaga.dev`                            | That domain does not exist. Use the GitHub release URL.                                                                  |
| "Latest" versions                                         | Every dependency and image is pinned to an exact, verified version.                                                      |

**Correction (decision D-b, 2026-11-12, gap G26).** An earlier version of this table rejected
`net.ipv4.ip_unprivileged_port_start=80` with the reason "socket activation already solves this".
That reason is wrong for dermaga: `traefik-http.socket` and `traefik-https.socket` are units of the
`server` user's systemd **user manager**, and a user manager binds sockets as that unprivileged
user. Without the sysctl it cannot bind :80/:443. The installer therefore **keeps**
`net.ipv4.ip_unprivileged_port_start = 80` as a deliberate deviation. The cost (any local user can
bind ports 80–1023) is recorded as an accepted residual risk in `docs/v2/THREAT-MODEL.md` §5 and
`docs/THREAT-MODEL.md` §7.4. The alternative (user sockets on high ports plus an nftables redirect)
is not built.

### 2.4 Optional, decided later (Phase 6 gate)

**Tailscale** as an _additional_ way to reach the panel. Allowed only if all of these hold: opt-in
at install, panel login (password + TOTP) still required, port 7070 never bound to a public
interface, and the memory budget in §5 still holds. If any of these fails, drop it.

### 2.5 Non-goals for v2

Multi-user/teams/SSO, multiple servers, Compose, Kubernetes, non-GitHub VCS apps, archive upload,
preview environments, AI assistant, billing, non-Debian hosts, any public admin panel, a laptop
client binary (dermaga runs on the server only; the laptop uses plain `ssh`).

---

## 3. The user journey (acceptance target)

This is the experience v2 must deliver. Every step is tested on a fresh Debian 13 VM.

1. **Before install** (README): "When you create the VPS at your provider, add your laptop's SSH
   key." Linked guide for people who have no key yet (`ssh-keygen -t ed25519`,
   `cat ~/.ssh/id_ed25519.pub`). "Created it with a root password only? Fine, the installer will
   guide you."
2. **Install**: log in with what the provider gave you, then
   `curl -fsSL https://github.com/r3p-dev/dermaga/releases/download/<tag>/install.sh | sudo bash`.
3. The installer finds the key used for this login, shows its fingerprint and comment
   (`ED25519 SHA256:… rep@fedora`) and asks "Use this key for user `server`?" (Enter = yes). No
   key found → explains how to create one and asks the user to paste the public key.
4. It asks for the SSH port (default 4222) and nothing else that has a safe default.
5. It hardens and installs everything. Before disabling password login or closing the old SSH
   port it says: "Open a **new** terminal on your laptop and run `ssh -p 4222 server@<ip>`. Keep
   this window open." It detects the successful key login itself and continues (typing `yes` is
   the fallback).
6. It prints, ready to paste on the laptop, **one command** that saves the connection as
   `Host dermaga`, and the first-run URL `http://localhost:7070/setup?token=…`.
7. On the laptop: paste the command once. Then `ssh dermaga` (shell on the VPS, panel tunnel open
   while the session lives) and open the setup URL.
8. **First run** in the browser: set the admin password, enable TOTP (strongly recommended, skippable
   with a warning), set the hook domain (e.g. `hooks.example.com`) with a DNS check.
9. **Connect GitHub**: one button creates a private GitHub App for this server (manifest flow), then
   "Install on repositories" lets the user pick repos. Back in the panel automatically.
10. **New app**: pick repo → branch (default branch preselected) → dermaga reads the repo and
    suggests Containerfile path and port → domain (with DNS check) → Deploy. Live build log.
11. Every later change is a form: env vars, domains and redirects, headers and middleware, health
    check, resources, strategy, auto-deploy, databases, backups, notifications.
12. `git push` → live with zero failed requests. A broken commit never takes the site down.

---

## 4. Hard rules (security and honesty)

1. All v1 security requirements (v1 BRIEF §4) stay. Nothing in v2 may weaken them.
2. The panel is never reachable from any non-loopback interface, at any moment, including setup.
3. The setup token: 32 random bytes, stored hashed, single use, expires after 24 h, only accepted on
   the loopback listener, invalid once an admin password exists.
4. GitHub App: minimum permissions (Contents: read, Metadata: read; Actions: read only if the CI gate
   is used). Private key encrypted with the master key. Installation tokens are requested per fetch,
   scoped to the one repository, never written to disk, never placed in a URL or a process argument.
5. Every step that needs the user (provider SSH key, DNS records, GitHub clicks, S3 bucket, snapshot
   revert, a repo URL) **stops and asks**. Never invent a value, never assume it happened.
6. **Evidence rule.** A claim that something works must come with the raw output of a command that
   was actually run, in the same session, including `date -Is`, `hostname` and `git rev-parse
--short HEAD` at the top. Anything not run is written as **NOT RUN** with the exact commands
   for me. Summaries, reconstructed or "representative" output are forbidden. A milestone with any
   NOT RUN acceptance item is not done.

---

## 5. Global acceptance criteria (v2)

- Fresh Debian 13 → one command → `dermaga doctor` all green, with **zero files edited by hand** and
  no question that has a safe default.
- The installer cannot lock the user out: tested with a wrong key, with no key, and with the user
  closing the confirmation window.
- From outside, a port scan shows only the SSH port, 80 and 443. The panel is unreachable from the
  public IP at every moment of the install and setup, verified by scanning during the install.
- Laptop: one pasted command, then `ssh dermaga` + browser is all that is needed. Re-pasting the
  command does not duplicate the block.
- Every capability of the CLI is reachable in the UI (checklist in the report, each item exercised).
- GitHub App flow works end to end without the panel being publicly reachable.
- `git push` → live with zero non-2xx and zero connection errors at 50 req/s; a failing deploy also
  zero errors.
- Response headers match vps-setup.sh's defaults; changing a header in the UI changes `curl -sI`
  within seconds.
- Backups run on schedule, test restore succeeds, and a full disaster recovery from the bucket onto
  a reverted fresh VM was exercised once.
- Daemon idle RSS < 30 MB; Traefik + daemon < 100 MB. Railpack and Tailscale, if added, add 0 MB
  at idle (Railpack) or are opt-in and measured (Tailscale).
- Everything survives a reboot.

---

## 6. How to work with me

Unchanged from v1 BRIEF §10 and §11, restated because they were broken on `antigravity-m3`:

- Ask before any decision that is hard to reverse or contradicts this brief.
- Report after each phase: what was built, how it was tested (raw evidence), deviations, open
  questions, suggested commits, next phase.
- **I make every commit.** Never commit, push, tag, stage, rebase, reset, stash, or switch
  branches. Suggest small, focused Conventional Commits, each passing `make check` on its own, with
  fish-compatible `git add …; and git commit -m … -m …` commands and no AI attribution.
- VM rules from v1 BRIEF §10 apply unchanged (`dermaga-vm` only, never the host `server`; ask before
  any snapshot revert/create/delete).

The phase plan is in `docs/v2/PLAN.md`, the target design in `docs/v2/ARCHITECTURE.md`.
