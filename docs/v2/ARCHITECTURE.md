# dermaga v2 architecture (target design)

This describes the **target** for v2. It is not a status report. The v1 design and its decision
log (D1–D61) stay in `docs/ARCHITECTURE.md`; new v2 decisions are appended there as D62 onward,
each with its reason and evidence, as they are taken. Status labels below:

- **v1** — exists in code on `antigravity-m3`; must be re-verified in Phase 0 before relying on it.
- **gap** — required by v1, missing or incomplete; fixed in Phase 0.
- **new** — v2 work.

---

## 1. Processes and exposure

```
Internet ──► :80/:443 (systemd user sockets) ──► Traefik ── network "web" ──┬──► dg-<app>-<rel8>
                                                                            └──► dg-hooks:8080 ──► hooks.sock ──► daemon
                                                  routes on the hook host:
                                                    /hooks/github/{appId}   (v1, per-app webhook, "Advanced" source)
                                                    /hooks/github-app       (new, one GitHub App webhook)

Laptop ── ssh dermaga (LocalForward 7070) ──► sshd :<port> ──► 127.0.0.1:7070 ──► daemon (panel + API)
Laptop browser ◄──► github.com (manifest + install redirects land on http://localhost:7070/…)
Daemon ── outbound HTTPS ──► api.github.com (manifest conversion, installation tokens, repo reads)
Shell on server ──► dermaga CLI ──► $XDG_RUNTIME_DIR/dermaga/dermaga.sock
```

Unchanged from v1: no new host TCP port; the panel listens on loopback only; the hook path is the
D1 relay; no container gets the Podman socket. v2 adds **no inbound listener**. The GitHub App
flow uses browser redirects to `localhost` and outbound calls from the daemon only.

---

## 2. Installer (`install/install.sh`) — new behaviour

Bash, idempotent, `set -Eeuo pipefail`, Debian 13 only. Distributed as a release asset next to the
binaries and `checksums.txt`.

### 2.1 Interactive by default

- `curl … | sudo bash` means stdin is the script. **All prompts read from `/dev/tty`.** If there is
  no TTY and no `--config`/`--yes`, stop with a clear message.
- Questions only where no safe default exists. Every prompt shows its default; Enter accepts it.
- `--config <file>` and `--yes` keep a fully non-interactive path (used by the VM harness). The
  `config-example` subcommand stays.

### 2.2 SSH key for `server`

1. Collect candidate public keys from `/root/.ssh/authorized_keys` and from `$SUDO_USER`'s
   `authorized_keys` (existing `collect_admin_keys`).
2. If the current session's key can be identified (sshd's "Accepted publickey … SHA256:<fp>" in the
   journal for this `SSH_CONNECTION`), mark it as "the key you used to log in".
3. Show each candidate with `ssh-keygen -lf` output (type, fingerprint, comment). One key → "Use it?
   [Y/n]". Several → numbered choice, default = the session key.
4. None found → print the short "create a key" guide (`ssh-keygen -t ed25519`,
   `cat ~/.ssh/id_ed25519.pub`, run **on the laptop**), then accept a pasted key, validated with
   `ssh-keygen -lf` before use. Loop until valid or the user aborts.

### 2.3 Lockout guard

1. Configure sshd for the new port and key-only login for `server`, **but keep the old port and the
   current auth method open** (nftables allows both) until confirmation.
2. Validate with `sshd -t`, reload (never restart nftables; keep v1's safety checks).
3. Print: "Open a NEW terminal on your laptop and run: `ssh -p <port> server@<ip>`. Keep this window
   open." Warn that a provider-level firewall may need the port opened.
4. Wait (default 10 min) for a journal entry "Accepted publickey for server … port <port>". On
   success continue automatically; typing `yes` is accepted as a manual confirmation.
5. Only then: close the old port, disable password and root login, reload. Timeout or Ctrl+C →
   leave the old access path intact and explain how to resume (`install.sh init` is re-runnable).

### 2.4 Final output

- The **laptop command** (§3), already filled in with IP, port and user.
- The **first-run URL** `http://localhost:7070/setup?token=<token>` (token from §4).
- One line: "Lost this? On the server run `dermaga panel`." (`panel` prints both again; the token is
  re-issued if the old one expired and no admin exists yet.)
- `dermaga doctor` output.

---

## 3. Laptop access (no client software)

The installer prints one POSIX `sh -c '…'` command, so it works when pasted into bash, zsh or fish.
It:

1. creates `~/.ssh` (0700) if missing and backs up an existing `~/.ssh/config` once
   (`config.dermaga.bak`);
2. appends, only if the marker is not already present:

```
# >>> dermaga 203.0.113.10 >>>
Host dermaga
  HostName 203.0.113.10
  Port 4222
  User server
  LocalForward 7070 127.0.0.1:7070
  ServerAliveInterval 30
# <<< dermaga 203.0.113.10 <<<
```

3. prints "Saved. From now on: `ssh dermaga`, then open http://localhost:7070".

Notes: the alias is `dermaga`, or `dermaga-<hostname>` if a `Host dermaga` already exists. No
`ExitOnForwardFailure`, so a second `ssh dermaga` still opens a shell (it only warns that 7070 is in
use). No `SessionType none`: logging in to the server and having the panel are the same action.
Windows: the panel's Access page shows the same block and where to put it.

The panel's **Settings → Access** page shows the same command and block at any time.

---

## 4. First-run setup — new

- Daemon state `needs-setup` while no admin password exists. In that state every panel route except
  `/setup` and its API redirects to `/setup`.
- The setup token (BRIEF §4.3) is generated by `install.sh` via `dermaga setup-token` and stored
  hashed. `/setup` asks for: admin password (argon2id, strength meter), TOTP enrolment with QR and a
  verification code (skip allowed, with a persistent warning in the UI and `doctor`), hook domain.
- Hook domain step: shows the exact DNS record to create (`A hooks.example.com → <server IP>`),
  checks resolution from the server, and explains that GitHub needs this to send push events.
- `dermaga admin set-password` stays for recovery.

---

## 5. GitHub App — new (`internal/githubapp`)

### 5.1 Creation (manifest flow)

1. Settings → GitHub → "Connect GitHub". The panel renders a form that POSTs to
   `https://github.com/settings/apps/new?state=<nonce>` (or the organisation variant) with a
   `manifest` JSON field:
   - `name`: `dermaga-<hostname>-<4 random chars>` (app names are global on GitHub);
   - `url`: `http://localhost:7070`;
   - `hook_attributes.url`: `https://<hook domain>/hooks/github-app`;
   - `redirect_url`: `http://localhost:7070/api/github/manifest/callback`;
   - `setup_url`: `http://localhost:7070/settings/github/installed`;
   - `public: false`;
   - `default_permissions`: `contents: read`, `metadata: read` (+ `actions: read` only when a CI
     gate is enabled later — this requires an app permission update the user approves on GitHub);
   - `default_events`: `push` (+ `workflow_run` as above).
2. GitHub redirects the **browser** to `redirect_url?code=…&state=…`. The daemon checks `state`,
   then calls `POST https://api.github.com/app-manifests/{code}/conversions` (outbound). It stores
   the app id, slug, client id, webhook secret and **private key**, all encrypted with the master key.
   Client secret is not needed and is discarded.
3. The panel links to `https://github.com/apps/<slug>/installations/new`. After installing, GitHub
   redirects the browser to `setup_url?installation_id=…`; the daemon verifies the installation
   belongs to its app via the API and stores it.

**Phase 3 starts with a spike** that confirms on real GitHub that `localhost` redirect and setup
URLs are accepted for a manifest-created app, and that the conversion works within its time limit.
If not, stop and report alternatives before building.

### 5.2 Tokens and fetching

- App JWT (RS256, ≤ 10 min, `iat` back-dated 60 s) built with `crypto/rsa` + `encoding/json` +
  base64url; no JWT library.
- Per fetch: `POST /app/installations/{id}/access_tokens` with `repository_ids: [<repo id>]` and
  `permissions: {contents: read}`. Token kept in memory only.
- Git over HTTPS without exposing the token: pass it as an `http.extraHeader` through the
  environment (`GIT_CONFIG_COUNT=1`, `GIT_CONFIG_KEY_0=http.https://github.com/.extraheader`,
  `GIT_CONFIG_VALUE_0=Authorization: Basic <base64(x-access-token:TOKEN)>`). Never in the URL, never
  in argv, never in `.git/config`. Mirrors keep a credential-free remote URL.
- Build log masking covers the token and its base64 form.

### 5.3 Webhooks

- New route `POST /hooks/github-app` on the existing hook listener (relay unchanged; Traefik already
  routes `PathPrefix(/hooks/)` on the hook host).
- Same checks as v1 §4.4, in the same order: body limit 1 MiB, HMAC with the **app** secret before
  parsing, event allowlist (`push`, `workflow_run`, plus `installation*` events to keep the stored
  installation and repository list current), dedupe on `X-GitHub-Delivery`, 202 then enqueue.
- Routing: `repository.id` + `ref` → every app with that repo id and branch. No match → 202 and
  ignored (logged), so the response does not reveal which repos are deployed.
- Per-app watch paths and the `workflow_run` CI gate work as in v1.

### 5.4 Data model additions

`github_app` (single row: app id, slug, client id, webhook secret enc, private key enc, created_at),
`github_installations` (id, account login, account type, suspended), apps gain `source.type = "github-app"`
with `repo_id`, `repo_full_name`, `installation_id`. `git` (deploy key) and `image` sources stay.

### 5.5 Wizard data

`GET /api/github/repos` (from installations), `GET /api/github/repos/{id}/branches`, and
`GET /api/github/repos/{id}/inspect?ref=` which reads, through the contents API, the root listing
and the `Containerfile`/`Dockerfile` (size-capped) to suggest the build file and the first `EXPOSE`
port. Suggestions are shown as suggestions; the user confirms. A repo without either file shows:
"No Containerfile found" with a link to a short guide, and, once Phase 5 lands, the Railpack option.

---

## 6. UI — GUI-first

Every page from v1 BRIEF §3.10 exists **and works**, plus the v2 additions. Phase 0 closes the v1
gaps; Phase 2 makes the UI complete.

| Area                       | Status        | Content                                                                                                                                                                                                                                                                                                                                                                     |
| -------------------------- | ------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| App → Domains & Middleware | **gap**       | primary/aliases/redirect, www↔apex; security headers on/off and edit; custom response headers (key/value rows); removed headers; compression; rate limit (avg/burst/off); basic auth users (bcrypt by daemon); IP allowlist (CIDR rows, validated); max body size; path prefix + strip. Preview of the effective header set. Save → re-render → Traefik reload, no restart. |
| App → Settings             | **gap**       | source, build (Containerfile path, context), health (path / cmd / none with warning), resources, strategy, auto-deploy, watch paths, CI gate                                                                                                                                                                                                                                |
| New app wizard             | **gap → new** | v1: detect Containerfile/EXPOSE (missing). v2: GitHub App repo picker first; "Advanced: deploy key + webhook"; "Ready-made app" (templates)                                                                                                                                                                                                                                 |
| Settings → Access          | **new**       | laptop command, `Host` block, current SSH port, sessions list, logout all                                                                                                                                                                                                                                                                                                   |
| Settings → GitHub          | **new**       | connect, installations, repos visible, disconnect (deletes stored key; tells the user to delete the app on GitHub)                                                                                                                                                                                                                                                          |
| Templates                  | **new**       | catalog cards → form (domain, required env, volume size hint) → deploys an `image` app                                                                                                                                                                                                                                                                                      |
| Backups                    | **gap**       | restic password generated and **shown once** with "I stored it offline" confirmation (§8)                                                                                                                                                                                                                                                                                   |
| Everything else            | **v1**        | re-verified in Phase 0                                                                                                                                                                                                                                                                                                                                                      |

Rules: no feature is "done" if it only exists in the API or CLI. Each form validates on the client
for UX and on the server for safety; server-side validation is the authority.

---

## 7. Templates — new (`internal/templates`)

- Small curated catalog embedded in the binary as JSON (no YAML library): name, description, image
  with **exact pinned tag**, container port, health path, named volumes, env schema (required,
  generated secret, default), resource hints, link to upstream docs.
- Each entry's port, health endpoint, volumes and env must be verified against the image's current
  upstream documentation and by deploying it on the VM. Nothing from memory.
- A template deploy is an ordinary `image` app afterwards (editable like any other).
- Initial set (each must pass the VM test or is dropped): Uptime Kuma, Vaultwarden, plus at most
  three more that run as one container with a named volume.

---

## 8. Backups — gap fixes

- restic repository password: generated (32 bytes), displayed once in the UI with a mandatory
  confirmation, then stored encrypted with the master key (needed for scheduled runs). The UI and
  docs state plainly: master key + restic password must both be kept offline; losing either loses
  the backups or their decryption.
- `install.sh` installs `restic` and `git` (missing on `antigravity-m3`).

Decision (numbering continues the v1 ADR table in `docs/ARCHITECTURE.md`, which ends at D61):

| # | Decision | Why |
| --- | --- | --- |
| D62 | **Restic password: explicit generation, one display, audited reveal, three states.** The password lives in `settings.restic_password` (sealed with the master key); `settings.restic_password_confirmed_at` (unix seconds, 0 = not confirmed) gives the state: `none` (no password), `unconfirmed`, `confirmed`. **Generation** happens only through `POST /api/backup/password` (panel "Generate password", `dermaga backup password init`), never as a side effect: `GET /api/backup/settings` returns `passwordState` and no password and writes nothing, and `PUT` cannot set or replace the password (a new password would lock the existing repository). **States:** with `none`, every restic operation fails with "set up the backup password first" and the normal `backup.failed` notification; with `unconfirmed`, backups run, the panel shows a banner and `doctor` warns; passwords generated silently by earlier versions start as `unconfirmed` and are kept (no re-keying). **Reveal:** `POST /api/backup/password/reveal`. In the panel it requires re-authentication (admin password, plus a TOTP code when TOTP is on); failures answer 403, are audited and count against the same limiter and backoff as login (429 when blocked). On the CLI socket there is no re-authentication, because socket access already is full owner access (the same user can read `master.key` and reset the admin password); `dermaga backup password reveal` requires typing `reveal`. **Confirm:** `POST /api/backup/password/confirm` after the owner ticks "I stored it offline" (UI) or types `stored` (CLI). **Never leaked:** the password appears only in the generate and reveal response bodies, both sent with `Cache-Control: no-store`; never in logs, audit details or error messages (test: `TestBackupPasswordLifecycle` greps the captured log). **Audit events:** `backup.password_generated`, `backup.password_revealed`, `backup.password_confirmed` (each with `source=panel` or `source=cli`) and `backup.password_reveal_denied`. **Recovery statement** shown with the password in UI and CLI (`backup.RecoveryStatement`): "To restore this server you need two things: the master key (`dermaga keys export`) and this restic password. Without the restic password the backups cannot be read by anyone. Without the master key the restored database cannot decrypt app secrets, environment variables and database passwords. Store both offline, in different places from the server." **Later (CS9):** `backup password use-existing` fits without migration: verify the supplied password against the repository (`restic cat config`), store it in the same column and set `restic_password_confirmed_at`, landing in `confirmed`. | Closes G6 and G13: the password used to be generated and stored by a GET and returned in clear by the API, and the owner was never shown it, so a lost server meant unreadable backups. Replaces v1's `backup settings --reveal-password`. |
| D63 | **Route edits use the deploy pipeline's D7/D8, and basic-auth passwords are hashed by the daemon.** `PUT /api/apps/{app}` (domains, middleware) validates the new spec, checks that no domain belongs to another app, then calls `deploy.Engine.ApplyRoute`: the same `switchRoute` a deploy uses writes the route with a one-time token router, waits until Traefik serves it (`VerifyTimeout`), and writes the steady file. Only then is the spec stored. If Traefik does not serve the route, `restoreRoute` puts the previous route back and verifies it, the DB row is untouched, the API answers 502 naming the timeout and the place to look, and `app.edit_failed` is audited (`app.edit` on success). If the save itself fails after a verified route, the previous route is restored too. Edits take `Engine.Lock(app)`, the lock deploys take. **Basic auth:** clients send `{user, password}`; the daemon hashes with bcrypt (cost 10, 12–72 bytes) in `resolveBasicAuth`, clears the password, and never returns a hash (`view` strips it; `spec.Validate` refuses a spec that still carries a password). A user sent without a password keeps the stored hash; a hash sent by a client is ignored. The CLI `middleware … basic-auth add` sends the plaintext over the socket. **`GET /api/apps/{app}/headers`** returns the effective header set (`EffectiveHeadersView`). **Fault injection for D8 on a real host** exists only under the `faultinject` build tag (`internal/deploy/fault_on.go`); no Makefile target or workflow sets it. | Closes G11, G12 and the API part of G2. Before, an edit stored the spec first and wrote the route unverified, so a file Traefik rejected left the DB and the proxy disagreeing; and only the CLI could hash, so the panel could not add a user. Supersedes the CLI-side hashing of v1 D38. |
- `doctor` adds: Traefik health, DNS for every configured domain and the hook domain, restic
  repository reachability (when configured), TOTP warning, GitHub App reachability (when connected).

---

## 9. Railpack — new, gated (Phase 5)

Only after a spike answers, on `dermaga-vm`, with evidence:

1. Current Railpack release and how it builds (it emits a BuildKit build plan; confirm the exact
   mechanism from its current docs).
2. Whether BuildKit can run **rootless under Podman** as an on-demand container (started for a build,
   stopped after), with dermaga's build memory/CPU limits and timeout.
3. Idle memory unchanged (no long-running BuildKit daemon); cache kept in a named volume with a size
   cap and pruning.
4. The resulting image loads into Podman's local storage and runs through the normal pipeline.

If any answer is no, report and stop; Railpack is dropped or redesigned with my approval.

---

## 10. Tailscale — optional, gated (Phase 6)

Conditions in BRIEF §2.4. If approved: `tailscaled` installed and joined from the installer
(auth key or interactive login URL printed in the terminal), the panel additionally binds the
tailnet IP **only after** the node is up, and login stays required. Never a temporary public bind.

---

## 11. Memory budget

Daemon idle RSS < 30 MB; daemon + Traefik < 100 MB. The GitHub App client, templates and setup
flow add no background goroutines beyond a token cache. Measured at the end of every phase with
`ps -o rss` on the VM (raw output in the evidence).
