# Apps and deploys

An **app** is one **stateless** container built from a GitHub repo, listening on one HTTP port,
reachable on one or more domains. Apps have no volumes in v1 (data belongs in an outside service until
databases and backups arrive in v1.1).

---

## 1. GitHub App (`GHA`)

- **GHA-1** **Settings → GitHub → Connect GitHub** creates one private GitHub App for this server with
  GitHub's manifest flow. Every redirect lands on `http://localhost:7070` in the user's browser; the
  panel is never exposed for it. The panel explains each GitHub screen before the user sees it.
- **GHA-2** Minimum permissions: Contents read, Metadata read. Events: push, plus the installation
  events.
- **GHA-3** After creating the App, the user installs it on their account or organisation and picks the
  repositories it may read. The panel can send them back to GitHub at any time to change that choice.
- **GHA-4** The App's private key and webhook secret are stored encrypted.
- **GHA-5** Access tokens are requested per fetch, scoped to one repository, kept in memory only;
  never in a URL, a process argument, a file, a log or the database.
- **GHA-6** Settings → GitHub shows the App, its installations and the repos it can see, and offers
  **Disconnect** (deletes the stored key and explains how to delete the App on GitHub too). Apps keep
  serving their live release; new deploys are refused with a clear message until GitHub is connected
  again.
- **GHA-7** Uninstalling, suspending or removing a repo from the App on GitHub is reflected in the
  panel; affected apps show that they can no longer deploy.
- **GHA-8** Pushes arrive through **one webhook for the whole server** at the hook domain
  (`SETUP-2`). The panel shows whether the last delivery arrived and when.

## 2. New app wizard (`WIZ`)

- **WIZ-1** Pick a repository from the ones the App can see. If the list is empty, explain how to
  grant access on GitHub and link there.
- **WIZ-2** Pick a branch (default branch preselected).
- **WIZ-3** dermaga reads the repo and **suggests** the Containerfile/Dockerfile path, build context and
  the first `EXPOSE`d port, shown as suggestions to confirm or edit. No Containerfile → say so and link
  to a short guide with examples for common frameworks.
- **WIZ-4** Port, health check path (recommended, with why), environment variables (`APP-1`), domain
  with a DNS check showing the exact record to create (`APP-2`).
- **WIZ-5** Deploy → live build log → success with the live URL, or a clear error with what to do next.
- **WIZ-6** The wizard sends only values the server accepts, and shows server errors next to their
  field.

## 3. App settings (`APP`)

App page tabs: **Overview · Deployments · Logs · Environment · Domains · Headers & Middleware ·
Settings · Danger zone**.

- **APP-1 Environment**: add, edit, remove variables. Each variable is available at **runtime**,
  at **build time**, or both (frameworks such as Astro, SvelteKit and Vite read `PUBLIC_*` / `VITE_*`
  variables during the build). Values hidden by default (`UI-5`). Changes take effect on the next
  deploy, and the panel says so and offers to redeploy.
- **APP-2 Domains**: primary domain, aliases, redirect aliases → primary (301), `www` ↔ apex redirect,
  optional path prefix with prefix stripping. A host belongs to at most one app. Each domain shows its
  DNS check and its certificate status (`TLS-3`).
- **APP-3 Security headers**: defaults equal `vps-setup.sh`'s (HSTS, nosniff, frame options, referrer
  policy, `Permissions-Policy`, strip `Server`/`X-Powered-By`). Per app: turn all off, remove single
  defaults, add or replace custom headers (e.g. a CSP) as key/value rows. A preview of the effective
  header set.
- **APP-4 Middleware**: compression on/off; rate limit (average, burst, off); basic auth users
  (password typed in the form, hashed by the server); IP allowlist (CIDR rows, validated); max request
  body size.
- **APP-5 Health**: HTTP path (recommended), or the image's own health command, or none (with a
  visible warning). Start timeout and required consecutive successes.
- **APP-6 Resources**: memory, CPU and pids limits (global defaults prefilled), optional read-only root
  filesystem, extra capabilities (default: all dropped).
- **APP-7 Build**: branch, Containerfile path, build context.
- **APP-8 Auto-deploy**: on/off; watch paths (deploy only if the push touched matching files; if the
  push lists no files, deploy anyway).
- **APP-9 Danger zone**: remove the app (containers, images, route, certificates' routers, mirror),
  with typed confirmation.
- **APP-10 Route changes are safe**: saving domains, headers or middleware applies the new route and
  checks through Traefik that it is live; if not, the previous route and settings are restored and the
  error is shown. The app is not restarted.
- **APP-11 Global defaults** (Settings → Defaults): memory/CPU/pids limits, security headers and rate
  limit used for new apps and for apps that don't override them.

## 4. HTTPS (`TLS`)

Traefik issues and renews certificates (ACME, Let's Encrypt); dermaga configures it and shows the
result.

- **TLS-1** Every domain of every app gets a certificate automatically, with no setting to fill in. HTTP
  redirects to HTTPS.
- **TLS-2** Before a domain is saved, dermaga checks that its DNS points to this server, and explains
  the exact record when it doesn't. A domain can still be saved with a warning (DNS may be
  propagating); the certificate is then requested once DNS is right.
- **TLS-3** Each domain shows its certificate status: active (with expiry date), waiting for DNS,
  or failed (with Let's Encrypt's reason in plain words and what to do).
- **TLS-4** Renewal is automatic; a certificate close to expiry that failed to renew shows as a warning
  on the dashboard and in `doctor`.
- **TLS-5** The hook domain gets its certificate the same way.

## 5. Deploys (`DEP`)

Triggers: push (auto-deploy), **Redeploy** button (latest commit of the branch, or a chosen commit),
**Rollback** button.

- **DEP-1** **Zero downtime**: the new release starts next to the old one, must be healthy, the route
  switch is verified, then the old one drains (grace 10 s) and stops.
- **DEP-2** **A failed deploy changes nothing visible**: the candidate is removed, the live release
  keeps serving, the deployment shows `failed` with the reason.
- **DEP-3** A **first deploy** that fails leaves nothing running and no route.
- **DEP-4** **Rollback** to any of the last 5 releases uses the existing image (no rebuild), with zero
  downtime, in < 10 s.
- **DEP-5** One build at a time server-wide. A newer trigger replaces a queued (not yet running) one for
  the same app (`superseded`).
- **DEP-6** **Cancel** a queued or running build from the panel; the live release is untouched.
- **DEP-7** Builds have limits: default 2 GiB memory, 2 CPUs, 20 min timeout, all configurable.
- **DEP-8** Build logs stream live; secrets (env values, tokens) are masked.
- **DEP-9** Deployment statuses: `queued → building → starting → healthy → live`, or `failed`,
  `cancelled`, `rolled_back`, `superseded`. The list shows trigger, commit SHA and message, duration,
  and the log.
- **DEP-10** After a daemon crash or reboot, dermaga repairs itself on start: exactly one live release
  and one route per app; anything it does not own is reported, never touched.

## 6. Logs (`LOG`)

- **LOG-1** Runtime logs per app, live (follow) and the last N lines.
- **LOG-2** Build logs per deployment, live while building, kept with the deployment.
