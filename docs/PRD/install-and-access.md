# Install, host and access

Everything from "I have a fresh VPS" to "I am logged in to the panel".

---

## 1. Before install (`INST`)

- **INST-1** The README has a short **"Before you start"** section:
  - a new VPS with **Debian 13** (x86_64 or ARM, 2 GB RAM minimum) and a domain the user can add DNS
    records to;
  - **add your laptop's SSH key when you create the VPS** (most providers ask on the create page):
    that is how the installer knows it is you;
  - no key yet: step by step for Linux, macOS and Windows 10+ (check for `~/.ssh/id_ed25519.pub`,
    `ssh-keygen -t ed25519`, show it with `cat`, paste it at the provider), explaining that the
    private file never leaves the laptop and should be backed up;
  - a VPS created with a root password only is fine too: the installer asks for the key.

## 2. Installer (`INST`)

One command, from a GitHub release asset:
`curl -fsSL https://github.com/r3p-dev/dermaga/releases/download/<tag>/install.sh | sudo bash`.

- **INST-2** Debian 13 only; refuses other systems with a clear message. Idempotent: re-running is
  safe and a no-op when nothing changed.
- **INST-3** **Interactive, no config file.** Prompts read from the terminal even when the script is
  piped. Every prompt shows its default; Enter accepts it. Questions only where no safe default
  exists. Without a terminal and without the automation flags, it stops with a clear message.
- **INST-4** A non-interactive path (a config file plus "yes to defaults") exists for the VM test
  harness. Users are never told to use it.
- **INST-5** **SSH key for user `server`**: collects the keys already authorised for root or the sudo
  user, marks the one used for the current login, shows type, fingerprint and comment, and asks "Use
  this key?" (one key) or a numbered choice (several; default = the current one). No key → explains
  how to create one **on the laptop** and accepts a pasted public key, validated, until valid or
  aborted.
- **INST-6** Asks for the SSH port (default 4222). Detects the server's public IP from the default-route
  interface (global IPv4 and IPv6) without calling third-party services (`NFR-8`); if private (RFC 1918,
  100.64.0.0/10) or if no public IPv4 is detected, it explains the situation and prompts the user for the
  public IP. Stored in settings and editable in Settings. No question about GitHub.
- **INST-7** Shows a short plan of what it will change before changing anything.
- **INST-8** Refuses to continue when something already listens on :80 or :443, and says what.
- **INST-9** Installs Podman, Traefik, git and curl; downloads the dermaga binary for the
  architecture and **aborts if its SHA-256 checksum cannot be verified**.
- **INST-10** Pins GitHub's SSH host keys (from GitHub's published metadata, fingerprints shown) for
  dermaga's own use.
- **INST-11** Creates the dermaga user service, enables lingering, starts it, runs the `doctor`
  checks (`DOC-1`) and shows the result.
- **INST-12** Prints at the end: the laptop command (`LAP-1`), the first-run link (`SETUP-1`), and
  "Lost this? Run `dermaga panel` on the server."

## 3. Host hardening (`HOST`)

The installer gives at least every guarantee `vps-setup.sh` gives, and weakens none.

- **HOST-1** Exactly one Linux user, `server`, who owns every container.
- **HOST-2** SSH: key-only on the chosen port, modern algorithms, `AllowUsers server`,
  `LogLevel VERBOSE`, root login off, TCP forwarding `local` only, no stream-local forwarding.
- **HOST-3** nftables default-deny with only the SSH port, 80/tcp, 443/tcp and 443/udp open; never
  restarted in a way that drops the current session.
- **HOST-4** fail2ban for sshd plus recidive.
- **HOST-5** sysctl hardening, unattended security upgrades, zram swap, synchronised clock.
- **HOST-6** Rootless Podman with Quadlet under the systemd user manager; Traefik v3 with socket
  activation for :80/:443 (tcp, and udp for HTTP/3, **verify in M0**; if UDP socket activation is
  unsupported by Traefik, HTTP/3 is disabled and Traefik serves TCP 80/443 via socket activation only),
  file provider only; no container gets the Podman socket.
- **HOST-7** Recommends `SUDO_NOPASSWD=no` for `server` and explains why.

## 4. Lockout guard and laptop access (`LOCK`, `LAP`)

- **LOCK-1** The new SSH port and key-only login for `server` are configured while the old port and
  the current login method **stay open**. sshd config is validated before reload.
- **LOCK-2** The installer says: "Open a NEW terminal on your laptop and run
  `ssh -p <port> server@<ip>`. Keep this window open." It warns that a provider-level firewall may
  need the port opened.
- **LOCK-3** It waits (default 10 minutes) until it sees a successful key login as `server` on the
  new port, and continues by itself. Typing `yes` is the manual fallback.
- **LOCK-4** Only then are the old port, password login and root login closed.
- **LOCK-5** Timeout, Ctrl+C or a closed window leave the old access path intact; the installer
  explains how to resume (re-running is safe).
- **LAP-1** **Laptop command**: one command, pasteable in bash, zsh and fish, that creates `~/.ssh`
  if needed, backs up an existing `~/.ssh/config` once, adds a marked `Host dermaga` block (host,
  port, user `server`, `LocalForward 7070 127.0.0.1:7070`, keep-alive) only if the marker is not
  there yet (uses `dermaga-<hostname>` if a `Host dermaga` already exists), and prints "Saved. From
  now on: `ssh dermaga`, then open http://localhost:7070".
- **LAP-2** `ssh dermaga` gives a shell **and** the panel tunnel; a second session still opens a shell
  and only warns that 7070 is in use.
- **LAP-3** **Settings → Access** shows the same command and block at any time, with the server's real
  SSH port and address, plus how to connect from Windows and from a phone SSH app with port
  forwarding.

## 5. First-run setup (`SETUP`)

- **SETUP-1** While no admin password exists, the panel is in **setup mode**: every page except
  `/setup` redirects there. `/setup` requires the single-use token from the installer's link. Visiting
  `/setup` without a token shows a form to paste the token and clear guidance: "Open the link from the
  installer, or run `dermaga panel` on the server for a fresh link." (not a dead-end error page). The
  token is compared in constant time, attempts are rate-limited, and it expires after 24 h.
- **SETUP-2** Steps: admin password with a strength meter → TOTP enrolment with a QR code and a
  verification code (skippable, with a persistent warning in the panel and in `doctor`, `DOC-2`) → hook
  domain: reserved dedicated host (`hooks.example.com`, `APP-2`), the exact DNS record to create
  compared against the detected/configured server IP (A and AAAA records; an AAAA pointing elsewhere is
  flagged as it breaks ACME; optional nonce self-check via Traefik that warns but never blocks on
  failure), with why GitHub needs it. The hook domain must be set before connecting GitHub, but DNS
  verification is informative and does not block setup or connecting GitHub; deliveries begin arriving
  once DNS and certificates are in place.
- **SETUP-3** `dermaga panel` on the server prints the laptop command and the link again, issuing a
  new token if the old one expired and no admin exists yet.

## 6. Admin login and sessions (`ADM`)

- **ADM-1** One admin password (argon2id). Optional TOTP (RFC 6238), strongly encouraged.
- **ADM-2** Login rate limiting with backoff.
- **ADM-3** Sessions: idle timeout 12 h, absolute 7 d; logout ends the session on the server.
- **ADM-4** **Settings → Security**: change the password, enable or disable TOTP, each requiring
  re-authentication (current password, plus a current TOTP code when TOTP is on). Changing the
  password ends every other session.
- **ADM-5** **Settings → Access** lists active sessions and offers "log out everywhere".
- **ADM-6** Recovery from a shell on the server: `dermaga admin set-password`,
  `dermaga admin totp disable`.

## 7. Moving from `vps-setup.sh` (`MIG`)

- **MIG-1** A guide (`docs/MIGRATING-FROM-VPS-SETUP.md`) moves a server set up with `vps-setup.sh`
  (apps routed by Traefik labels) to dermaga, using the real paths, unit names and Traefik layout of
  `vps-setup.sh`: recreate each app in dermaga first, switch Traefik to file-provider-only, then
  remove the old units.
- **MIG-2** Measured downtime per app, and a tested rollback path back to the label-based setup.
- **MIG-3** Rehearsed on `dermaga-vm` (snapshot `vps-setup`) with portfolio and Kapturae before the
  owner runs it on the production VPS.
