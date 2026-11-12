# dermaga v2 operations guide (target)

This is the user guide v2 must make true. Sections marked with a phase describe behaviour that does
not exist yet; remove the marker when that phase's evidence is accepted. Day-2 runbooks from v1
(`docs/OPERATIONS.md`) stay valid unless replaced here.

---

## 1. Before you start

You need:

1. **A new VPS with Debian 13** (x86_64 or ARM). 2 GB RAM is the practical minimum; 4 GB or more is
   comfortable.
2. **An SSH key on your laptop, added to the VPS when you create it.** Most providers ask for a key
   on the "create server" page. This is how the installer knows it is you.
3. **A domain** you can add DNS records to (for your apps and for GitHub's push notifications).

### Don't have an SSH key yet?

On your **laptop** (Linux, macOS, or Windows 10+ in PowerShell):

1. Check: `ls ~/.ssh/id_ed25519.pub`. If the file exists, you already have one; skip to step 3.
2. Create one: `ssh-keygen -t ed25519`, then press Enter at every question.
3. Show it: `cat ~/.ssh/id_ed25519.pub`. Copy the whole line (it starts with `ssh-ed25519`).
4. Paste it into your provider's "SSH keys" field when creating the VPS.

Already created the VPS with a root password only? That's fine. The installer will ask you to paste
the line from step 3.

The key that lets you log in is the private file `~/.ssh/id_ed25519`. It never leaves your laptop.
Back it up somewhere safe.

---

## 2. Install (Phase 1)

Log in to the VPS the way your provider tells you (for example `ssh root@203.0.113.10`), then run:

```
curl -fsSL https://github.com/r3p-dev/dermaga/releases/download/<version>/install.sh | sudo bash
```

The installer will:

1. **Show the SSH key you used to log in** and ask whether to use it for the `server` user. Press
   Enter. (No key found? It explains how to create one and asks you to paste it.)
2. **Ask for the SSH port.** Press Enter for 4222.
3. Harden the server, install containers, the reverse proxy and dermaga. This takes a few minutes.
4. **Ask you to test the new login** before it closes the old one:

   > Open a NEW terminal on your laptop and run: `ssh -p 4222 server@203.0.113.10`
   > Keep this window open.

   When you are in, the installer notices by itself and continues. If your provider has its own
   firewall in the web console, allow port 4222 there first.

5. Print two things you need next: **the laptop command** and **the setup link**.

Nothing to edit, nothing to memorise.

---

## 3. Connect your laptop (once) (Phase 1)

Copy the laptop command the installer printed and paste it into a terminal **on your laptop**. It
looks like this (yours has your IP and port filled in):

```
sh -c '…saves a "dermaga" entry in ~/.ssh/config…'
```

It saves the connection under the name `dermaga`. Pasting it again is harmless.

From now on, every time:

1. `ssh dermaga` — you are on your server, and the panel is available while this window is open.
2. Open **http://localhost:7070** in your browser.

Close the terminal and the panel is gone again. That is the security model: only your SSH key opens
the door.

Lost the command? On the server run `dermaga panel`, or open **Settings → Access** in the panel.

---

## 4. First run (Phase 1)

Open the setup link the installer printed (`http://localhost:7070/setup?token=…`) while `ssh dermaga`
is open.

1. **Choose an admin password.**
2. **Turn on two-factor login (TOTP).** Scan the QR code with an authenticator app and type the
   6-digit code. Strongly recommended; you can skip it, but dermaga will keep reminding you.
3. **Set the hook domain**, for example `hooks.example.com`. dermaga shows the DNS record to create
   (`A hooks.example.com → <your server IP>`) and checks it. GitHub uses this address to tell your
   server about new pushes; nothing else is served there.

---

## 5. Connect GitHub (Phase 3)

**Settings → GitHub → Connect GitHub.**

1. GitHub opens and shows an app named like `dermaga-<server>-ab12`, already filled in. Click
   **Create GitHub App**. It is private and belongs to you.
2. Back in dermaga, click **Install on repositories**, choose "Only select repositories" and pick
   the repos you want to deploy. You can change this later on GitHub.
3. You land back in dermaga with your repos listed.

The app can only **read** the repositories you selected. You don't add keys to your projects.

Prefer not to use a GitHub App? **New app → Advanced → Deploy key** still works: dermaga shows a key
and a webhook to add to that one repo, with direct links to the right GitHub pages.

---

## 6. Deploy an app

**Apps → New app** (Phase 3 for the repo picker; Phase 0 for detection).

1. **Pick a repository** and branch.
2. dermaga reads the repo and **suggests** the Containerfile/Dockerfile and the port from `EXPOSE`.
   Check them and change them if needed. No Containerfile? See §6.1.
3. **Domain**: enter `app.example.com`. dermaga shows the DNS record and checks it.
4. **Deploy.** Watch the build log live.

From then on, every `git push` to that branch deploys automatically, with zero downtime. If the new
version fails its health check, the old one keeps serving and you get a notification.

### 6.1 Repos without a Containerfile

Until Phase 5: add a Containerfile to the repo (the panel links a short guide). After Phase 5, if
accepted: choose **Build automatically (Railpack)**.

### 6.2 Ready-made apps (Phase 4)

**Apps → New app → Ready-made app**: pick Uptime Kuma, Vaultwarden, …, fill in the domain and the
few required fields, Deploy. No GitHub needed.

---

## 7. Everyday changes (all in the app page)

| I want to…                                       | Where                                |
| ------------------------------------------------ | ------------------------------------ |
| Change environment variables                     | Environment                          |
| Add a domain, redirect `www`                     | Domains & Middleware → Domains       |
| Add a header (e.g. a CSP), remove `X-Powered-By` | Domains & Middleware → Headers       |
| Turn compression or rate limiting on/off         | Domains & Middleware → Traffic       |
| Protect with a password                          | Domains & Middleware → Basic auth    |
| Allow only certain IPs                           | Domains & Middleware → IP allowlist  |
| Allow bigger uploads                             | Domains & Middleware → Body size     |
| Go back to a previous version                    | Deployments → Roll back              |
| See logs                                         | Logs                                 |
| Add a database                                   | Databases tab, or the Databases page |
| Change memory/CPU, health check, strategy        | Settings                             |

Saving a header or middleware change applies within seconds, without restarting the app.

---

## 8. Backups

**Backups → Settings**: enter your S3-compatible bucket (Cloudflare R2, Backblaze B2, …).

dermaga then shows the **backup password once**. Write it down and store it offline, together with
the master key (**Backups → Export master key**). Without both, backups cannot be restored on a new
server. dermaga asks you to confirm before continuing.

Each database gets a daily backup (03:00 Asia/Jakarta by default). Use **Test restore** to prove a
backup works without touching live data.

### Disaster recovery

New VPS → install (§2) → panel → **Backups → Restore system** with the bucket details, backup
password and master key. Apps are redeployed from their last good versions and databases restored.

---

## 9. Health and help

- **Dashboard**: server CPU, RAM, disk, pending updates, app status.
- **`dermaga doctor`** (or **Settings → Health**): checks lingering, sockets, Traefik, DNS for every
  domain, backups reachability, clock, disk, TOTP.
- Logs on the server: `journalctl --user -u dermaga -f` (daemon), app logs in the panel.
- Notifications: **Settings → Notifications** (ntfy, Telegram or Discord) for failed deploys, failed
  backups and a nearly full disk.

---

## 10. Optional: Tailscale (Phase 6, only if approved)

If enabled at install time, the panel is also reachable at `http://<server>:7070` from your
tailnet, still with password and TOTP. SSH (`ssh dermaga`) keeps working as before.
