# dermaga v2 threat model (delta)

The v1 threat model (`docs/THREAT-MODEL.md`) stays in force; this
file covers what v2 adds or changes and records why ideas from an earlier, uncommitted proposal were rejected. Re-check every claim in the v1
model during Phase 0 against the code, since it was written on `antigravity-m3`.

---

## 1. Assets added in v2

| Asset                        | Where                                            | Impact if stolen                                                                                                             |
| ---------------------------- | ------------------------------------------------ | ---------------------------------------------------------------------------------------------------------------------------- |
| GitHub App private key       | DB, AES-256-GCM with master key                  | Mint installation tokens for every repo the app is installed on (read-only contents) until the key is revoked on GitHub      |
| GitHub App webhook secret    | DB, encrypted                                    | Forge push events → trigger deploys of already-allowed repos/branches (no code injection: the commit is fetched from GitHub) |
| Installation tokens          | memory only, ≤ 1 h, scoped to one repo           | Read that repo for the token lifetime                                                                                        |
| Setup token                  | DB, hashed; printed once in the installer output | Set the first admin password before the owner does                                                                           |
| Laptop `~/.ssh/config` entry | user's laptop                                    | Reveals server IP and port only (no secret)                                                                                  |

The SSH private key never leaves the laptop and is not handled by dermaga.

## 2. Entry points (unchanged count)

| Entry point                          | v2 change                                                                            |
| ------------------------------------ | ------------------------------------------------------------------------------------ |
| Public :80/:443 (Traefik)            | New route `/hooks/github-app` on the hook host                                       |
| SSH :<port>                          | Installer transition period: old access path stays open until the confirmation login |
| Panel 127.0.0.1:7070 via SSH forward | New `needs-setup` state and `/setup` route                                           |
| Outbound to api.github.com           | New                                                                                  |

No new listening port, no public panel, no temporary public bind (the earlier proposal's onboarding design is
rejected for exactly this reason).

## 3. Threats and mitigations

### 3.1 Installer and access

| Threat                                                                  | Mitigation                                                                                                                                                                                                                 |
| ----------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| User locks themselves out (wrong key, closed window, provider firewall) | Dual access during transition; continue only after a journal-confirmed key login as `server` on the new port; abort/timeout leaves old access intact; installer re-runnable                                                |
| Installer piped from the network is tampered                            | Downloaded from a GitHub release over HTTPS; the binary it fetches is verified against `checksums.txt`. Residual: the script itself is trusted on first use. Docs show how to download, read and run it instead of piping. |
| Wrong key adopted (stale key in root's `authorized_keys`)               | Fingerprint and comment shown; session key highlighted; explicit choice when several                                                                                                                                       |
| Attacker races the owner to `/setup`                                    | Setup route only on loopback (reachable only through the owner's SSH), token required, single use, 24 h expiry, disabled once a password exists                                                                            |
| Setup token leaks via terminal scrollback or shell history              | Not passed as a command argument; printed only; expires; useless after first use                                                                                                                                           |

### 3.2 GitHub App

| Threat                                           | Mitigation                                                                                                                                                              |
| ------------------------------------------------ | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Manifest callback CSRF / code injection          | `state` nonce bound to the admin session, single use, short expiry; code exchanged by the daemon, never trusted from the browser beyond that                            |
| Over-broad permissions                           | `contents: read` + `metadata: read` only; `actions: read` only with a CI gate; user chooses "Only select repositories" (UI recommends it)                               |
| Token exposure in process list, logs, git config | Token passed through `GIT_CONFIG_*` environment variables; never in argv, URL or `.git/config`; masked in build logs (raw and base64); grep tests in Phase 3 acceptance |
| Private key theft from DB backups                | Encrypted with master key, which is excluded from system backups (v1 rule)                                                                                              |
| Forged webhooks                                  | HMAC-SHA256 with the app secret verified before parsing, 1 MiB limit, event allowlist, dedupe, per-repo rate limit                                                      |
| Webhook reveals which repos are deployed         | Unknown repo/branch → same 202 as an ignored event                                                                                                                      |
| Malicious code in an installed repo              | Out of scope by trust model: only repos the owner selects are built. Builds stay rootless with v1 limits and timeouts.                                                  |
| Revocation                                       | "Disconnect" deletes the key locally and tells the user to delete the app on GitHub; the docs say to delete the app on GitHub after suspected compromise                |

### 3.3 UI-editable middleware

| Threat                                                    | Mitigation                                                                                                                                                   |
| --------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| Header CRLF / response splitting                          | Header names: RFC 9110 token chars; values: no control characters; enforced server-side; golden and injection tests (v1 M2 test set extended to the new API) |
| Breaking Traefik with bad input                           | Typed structs → JSON (D6); config validated before atomic write; on Traefik rejection, the previous file is restored (v1 D8 behaviour)                       |
| Locking yourself out of an app (allowlist without own IP) | UI warning; change is reversible from the panel, which does not go through the app's middleware                                                              |
| Weak basic-auth passwords                                 | bcrypt by the daemon; UI strength hint                                                                                                                       |

### 3.4 Templates

| Threat                          | Mitigation                                                                                       |
| ------------------------------- | ------------------------------------------------------------------------------------------------ |
| Malicious or moved image tag    | Exact tags; each template verified on the VM before shipping; upstream link shown                |
| Templates asking for privileges | Same container rules as apps: capability drop, no privileged, named volumes only, no host mounts |

### 3.5 Railpack (Phase 5, only if adopted)

| Threat                                        | Mitigation                                                                         |
| --------------------------------------------- | ---------------------------------------------------------------------------------- |
| Build-time code execution                     | Same as Containerfile builds: rootless, resource limits, timeout, no Podman socket |
| BuildKit as a long-running privileged service | Not allowed: on-demand rootless container only, stopped after each build           |

### 3.6 Tailscale (Phase 6, only if adopted)

| Threat                                                     | Mitigation                                                                               |
| ---------------------------------------------------------- | ---------------------------------------------------------------------------------------- |
| Compromised tailnet device or account                      | Panel login (password + TOTP) still required; Tailscale is transport, not authentication |
| Panel bound before the node is up / on the wrong interface | Bind to the tailnet IP only after `tailscale status` reports it; never `0.0.0.0`         |

## 4. Rejected claims from the earlier proposal

| Claim                                                 | Correction                                                                      |
| ----------------------------------------------------- | ------------------------------------------------------------------------------- |
| Panel exposed on the machine IP during onboarding     | On a VPS this is the public internet. Rejected.                                 |
| "Sessions do not rely on password cookies" with Whois | Network identity is not authentication. Password + TOTP + session cookie stay.  |
| Compose parser "drops" dangerous directives           | Feature rejected; an allowlist of Compose is a large, error-prone surface.      |
| Archive upload with zip-slip protection               | Feature rejected for v2.                                                        |
| Paths under `/var/lib/dermaga`                        | dermaga uses XDG paths under the `server` user's home (v1 BRIEF §3.3).          |
| Host UID 1000                                         | The `server` user's UID is whatever the installer created; do not hard-code it. |

## 5. Residual risks (accepted, documented)

- All apps share network `web` (v1 residual, unchanged).
- The sudo-capable `server` user (v1 residual; recommend `SUDO_NOPASSWD=no`).
- Trust on first use of `install.sh`.
- `net.ipv4.ip_unprivileged_port_start = 80` (decision D-b, gap G26): any local user or process can
  bind ports 80–1023, for example to impersonate a service on an unused low port, or to take :80/:443
  while the Traefik socket units are stopped. Needed because the Traefik sockets are systemd **user**
  units and bind as the unprivileged `server` user (v2 BRIEF §2.3). Accepted because the host is
  single-user (sshd `AllowUsers` admits only `server`, which already owns the Traefik sockets) and no
  container publishes ports: containers run in their own network namespaces and dermaga writes no
  `PublishPort=`, so a compromised container cannot bind a host port. Details: `docs/THREAT-MODEL.md` §7.4.
- A GitHub App private key compromise is bounded by the selected repositories and read-only scope,
  but lasts until the owner deletes the app on GitHub.
