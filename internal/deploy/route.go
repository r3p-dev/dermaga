package deploy

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strconv"
	"time"

	"dermaga/internal/name"
	"dermaga/internal/store"
	"dermaga/internal/traefik"
)

// DNSRecheckInterval is how often a domain whose DNS check has not passed yet is checked again (DEC-38).
const DNSRecheckInterval = 5 * time.Minute

// CertRetryInterval is how long a domain without a certificate stays out of the route after its
// order failed (DEC-38): Traefik retries every failed order on any config change, and Let's
// Encrypt allows 5 failed validations per hour per hostname.
const CertRetryInterval = time.Hour

// RouteSpecFor builds an app's route from its domains, for the switch, the startup reconciler and
// panel edits alike (DEC-40). Domains still waiting for DNS or after a failed order get no router.
func RouteSpecFor(app *store.App, domains []store.Domain, live, port int, sw *traefik.SwitchSpec) traefik.AppSpec {
	specs := make([]traefik.DomainSpec, 0, len(domains))
	for _, d := range domains {
		var redir, pfx string
		if d.RedirectTo != nil {
			redir = *d.RedirectTo
		}
		if d.PathPrefix != nil {
			pfx = *d.PathPrefix
		}
		specs = append(specs, traefik.DomainSpec{Domain: d.Domain, IsPrimary: d.IsPrimary, RedirectTo: redir,
			PathPrefix: pfx, StripPrefix: d.StripPrefix, Pending: !d.Routed()})
	}
	return traefik.AppSpec{AppName: app.Name, ReleaseNumber: live, Port: port, Domains: specs, Switch: sw}
}

func (e *Engine) routePath(appName string) string {
	return filepath.Join(e.cfg.TraefikDynamicDir, name.AppDynamicFilename(appName))
}

// writeRoute renders the app's route from its stored domains (see writeRouteWith).
func (e *Engine) writeRoute(ctx context.Context, app *store.App, live, port int, sw *traefik.SwitchSpec) (primary, prefix string, err error) {
	domains, err := e.store.ListDomainsByApp(ctx, app.ID)
	if err != nil {
		return "", "", fmt.Errorf("loading domains: %w", err)
	}
	return e.writeRouteWith(ctx, app, domains, live, port, sw)
}

// writeRouteWith renders the app's route with the main service on release `live` (port) and, when
// sw is set, a verify router on sw's release (DEC-16, DEC-33), and writes it atomically. It returns
// the host and path a verify request uses.
func (e *Engine) writeRouteWith(ctx context.Context, app *store.App, domains []store.Domain, live, port int, sw *traefik.SwitchSpec) (primary, prefix string, err error) {
	spec := RouteSpecFor(app, domains, live, port, sw)
	data, err := traefik.RenderApp(spec)
	if err != nil {
		return "", "", fmt.Errorf("rendering traefik route: %w", err)
	}
	if err := e.host.WriteFileAtomic(e.routePath(app.Name), data, 0600); err != nil {
		return "", "", fmt.Errorf("writing traefik route: %w", err)
	}
	for _, d := range spec.Domains {
		if d.IsPrimary {
			primary, prefix = d.Domain, d.PathPrefix
		}
	}
	return primary, prefix, nil
}

// ApplyRoute puts a changed route live with proof (APP-10, DEC-40) and only then calls commit,
// which saves the change in the database. It refuses with ErrSwitchRunning while a release of the
// app is starting or switching (owner decision: 409). An app without a live release has no route:
// commit runs at once.
func (e *Engine) ApplyRoute(ctx context.Context, appID string, domains []store.Domain, commit func(context.Context) error) error {
	lock := e.appLock(appID)
	select {
	case lock <- struct{}{}:
	default:
		return ErrSwitchRunning
	}
	defer func() { <-lock }()
	return e.applyRouteLocked(context.WithoutCancel(ctx), appID, domains, commit)
}

// applyRouteLocked writes the new route plus a verify router to the live release, waits until
// Traefik answers with the token (the whole file is loaded), then writes it without the verify
// router. If Traefik never loads it, the previous file is written back and Traefik's own error is
// returned; the database is not touched.
func (e *Engine) applyRouteLocked(ctx context.Context, appID string, domains []store.Domain, commit func(context.Context) error) error {
	app, err := e.store.GetApp(ctx, appID)
	if err != nil {
		return err
	}
	if app.ActiveReleaseID == nil {
		if commit != nil {
			return commit(ctx)
		}
		return nil
	}
	live, err := e.store.GetDeployment(ctx, *app.ActiveReleaseID)
	if err != nil {
		return fmt.Errorf("loading the live release: %w", err)
	}
	rc, err := ReleaseRuntimeConfig(app, live)
	if err != nil {
		return err
	}
	path := e.routePath(app.Name)
	previous, readErr := e.host.ReadFile(path)
	since := e.now()

	tok := randomHex(16)
	sw := &traefik.SwitchSpec{CandidateReleaseNumber: live.ReleaseNumber, CandidatePort: rc.Port, VerifyToken: tok}
	primary, prefix, err := e.writeRouteWith(ctx, app, domains, live.ReleaseNumber, rc.Port, sw)
	if err == nil {
		err = e.waitVerified(ctx, primary, prefix, tok)
	}
	if err != nil {
		if readErr == nil {
			_ = e.host.WriteFileAtomic(path, previous, 0600)
		} else {
			_, _, _ = e.writeRoute(ctx, app, live.ReleaseNumber, rc.Port, nil)
		}
		if reason := e.traefikConfigError(ctx, since); reason != "" {
			err = errors.New(reason)
		}
		//lint:ignore ST1005 Traefik is a proper noun; the panel shows this text
		return fmt.Errorf("Traefik did not load the new route, so the previous route and settings stay in place: %w", err)
	}
	if _, _, err := e.writeRouteWith(ctx, app, domains, live.ReleaseNumber, rc.Port, nil); err != nil {
		_ = e.host.WriteFileAtomic(path, previous, 0600)
		return err
	}
	if commit != nil {
		return commit(ctx)
	}
	return nil
}

// traefikConfigError returns the newest configuration error Traefik logged since `since`, if any.
func (e *Engine) traefikConfigError(ctx context.Context, since time.Time) string {
	out, err := e.host.RunCommand(ctx, "journalctl", "--user", "-u", "traefik.service", "-o", "cat", "--no-pager",
		"--since", "@"+strconv.FormatInt(since.Unix(), 10))
	if err != nil {
		return ""
	}
	return traefik.LastConfigError(out)
}

// ---- domain watcher (DEC-38) ----

// RunDomainWatcher syncs every app's domains at once (this also brings route files written by an
// older version up to date) and then every minute, until ctx ends.
func (e *Engine) RunDomainWatcher(ctx context.Context) {
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	for {
		e.SyncDomains(ctx)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// SyncDomains runs SyncApp(…, false) for every app.
func (e *Engine) SyncDomains(ctx context.Context) {
	apps, err := e.store.ListApps(ctx)
	if err != nil {
		return
	}
	for _, a := range apps {
		err := e.SyncApp(ctx, a.ID, false)
		if errors.Is(err, ErrSwitchRunning) {
			continue
		}
		msg := ""
		if err != nil {
			msg = fmt.Sprintf("Could not update the route of %s: %v", a.Name, err)
		}
		// One Activity entry per new failure, not one per minute while it lasts.
		e.mu.Lock()
		repeated := e.lastSyncErr[a.ID] == msg
		e.lastSyncErr[a.ID] = msg
		e.mu.Unlock()
		if msg != "" && !repeated {
			e.logDomainEvent(ctx, a.ID, "route_sync_failed", msg)
		}
	}
}

// SyncApp updates the DNS gate and certificate state of an app's domains and applies the route
// when it changed (DEC-38):
//   - a domain whose DNS check has not passed is checked again every 5 minutes (now = "Check again");
//   - a routed domain without a certificate whose order failed since it entered the route leaves it;
//   - a failed domain comes back after an hour (now = "Check again");
//   - the route on disk is rewritten, with proof, whenever it differs from what the domains say.
//
// While a release of the app is starting or switching it returns ErrSwitchRunning; the switch
// renders the current domains itself.
func (e *Engine) SyncApp(ctx context.Context, appID string, now bool) error {
	app, err := e.store.GetApp(ctx, appID)
	if err != nil || app.ActiveReleaseID == nil {
		return err
	}
	domains, err := e.store.ListDomainsByApp(ctx, appID)
	if err != nil {
		return err
	}
	certs := e.readCertificates()
	var acmeErrs map[string]traefik.ACMEError
	t := e.now()
	stamp := t.UTC().Format(time.RFC3339)

	for i := range domains {
		d := &domains[i]
		changed := false
		switch {
		case d.DNSVerifiedAt == nil:
			if (now || e.dnsDue(d.ID, t)) && e.cfg.DNSMatches != nil && e.cfg.DNSMatches(ctx, d.Domain) {
				d.DNSVerifiedAt, d.CertRequestedAt = &stamp, &stamp
				changed = true
				e.logDomainEvent(ctx, appID, "domain_dns_verified", fmt.Sprintf("DNS of %s points to this server; requesting its certificate", d.Domain))
			}
		case d.CertFailedAt != nil:
			failed, _ := time.Parse(time.RFC3339, *d.CertFailedAt)
			if now || !t.Before(failed.Add(CertRetryInterval)) {
				d.CertFailedAt, d.CertError, d.CertRequestedAt = nil, "", &stamp
				changed = true
			}
		default:
			if _, ok := certs[d.Domain]; ok {
				continue
			}
			if acmeErrs == nil {
				acmeErrs = e.readACMEErrors(ctx, domains)
			}
			ae, ok := acmeErrs[d.Domain]
			requested, _ := time.Parse(time.RFC3339, deref(d.CertRequestedAt))
			if ok && ae.Time.After(requested) {
				d.CertFailedAt, d.CertError = &stamp, ae.Detail
				changed = true
				e.logDomainEvent(ctx, appID, "domain_cert_failed", fmt.Sprintf("Certificate for %s failed; it leaves the route until %s or Check again", d.Domain, t.Add(CertRetryInterval).UTC().Format("15:04 UTC")))
			}
		}
		if changed {
			if err := e.store.SetDomainState(ctx, *d); err != nil {
				return err
			}
		}
	}
	return e.syncRoute(ctx, app, domains)
}

// syncRoute applies the route with proof when the file differs from what the domains say.
func (e *Engine) syncRoute(ctx context.Context, app *store.App, domains []store.Domain) error {
	live, err := e.store.GetDeployment(ctx, *app.ActiveReleaseID)
	if err != nil {
		return err
	}
	rc, err := ReleaseRuntimeConfig(app, live)
	if err != nil {
		return err
	}
	want, err := traefik.RenderApp(RouteSpecFor(app, domains, live.ReleaseNumber, rc.Port, nil))
	if err != nil {
		return err
	}
	if have, err := e.host.ReadFile(e.routePath(app.Name)); err == nil && bytes.Equal(have, want) {
		return nil
	}
	return e.ApplyRoute(ctx, app.ID, domains, nil)
}

// dnsDue reports whether a domain's DNS check is due, and records the check.
func (e *Engine) dnsDue(domainID string, t time.Time) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	if last, ok := e.dnsChecked[domainID]; ok && t.Sub(last) < DNSRecheckInterval {
		return false
	}
	e.dnsChecked[domainID] = t
	return true
}

func (e *Engine) readCertificates() map[string]traefik.Certificate {
	if e.cfg.ACMEPath == "" {
		return nil
	}
	data, err := e.host.ReadFile(e.cfg.ACMEPath)
	if err != nil {
		return nil
	}
	certs, _ := traefik.ReadCertificates(data)
	return certs
}

// readACMEErrors reads Traefik's log since the oldest time one of the domains entered the route.
func (e *Engine) readACMEErrors(ctx context.Context, domains []store.Domain) map[string]traefik.ACMEError {
	var since time.Time
	for _, d := range domains {
		if r, err := time.Parse(time.RFC3339, deref(d.CertRequestedAt)); err == nil && (since.IsZero() || r.Before(since)) {
			since = r
		}
	}
	if since.IsZero() {
		return map[string]traefik.ACMEError{}
	}
	out, err := e.host.RunCommand(ctx, "journalctl", "--user", "-u", "traefik.service", "-o", "cat", "--no-pager",
		"--since", "@"+strconv.FormatInt(since.Unix(), 10))
	if err != nil {
		return map[string]traefik.ACMEError{}
	}
	return traefik.ACMEErrors(out)
}

func (e *Engine) logDomainEvent(ctx context.Context, appID, event, msg string) {
	_ = e.store.RecordAudit(ctx, "daemon", "system", event, msg, &appID, nil)
}

func (e *Engine) now() time.Time {
	if e.cfg.Now != nil {
		return e.cfg.Now()
	}
	return time.Now()
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// DomainStatus is a domain's DNS and certificate state as the panel shows it (TLS-2, TLS-3).
type DomainStatus struct {
	Certificate  string // "active", "waiting_dns", "requesting" or "failed"
	ExpiresAt    time.Time
	ExpiringSoon bool
	Reason, Fix  string // failed only
	NextRetryAt  time.Time
}

// DomainStatusOf derives a domain's status from its stored state and Traefik's certificates.
func DomainStatusOf(d store.Domain, certs map[string]traefik.Certificate, now time.Time) DomainStatus {
	if c, ok := certs[d.Domain]; ok {
		return DomainStatus{Certificate: "active", ExpiresAt: c.NotAfter, ExpiringSoon: c.ExpiringSoon(now)}
	}
	switch {
	case d.DNSVerifiedAt == nil:
		return DomainStatus{Certificate: "waiting_dns"}
	case d.CertFailedAt != nil:
		failed, _ := time.Parse(time.RFC3339, *d.CertFailedAt)
		reason, fix := traefik.PlainReason(d.CertError)
		return DomainStatus{Certificate: "failed", Reason: reason, Fix: fix, NextRetryAt: failed.Add(CertRetryInterval)}
	default:
		return DomainStatus{Certificate: "requesting"}
	}
}

// Certificates returns what Traefik stored in acme.json (nil when it cannot be read).
func (e *Engine) Certificates() map[string]traefik.Certificate {
	return e.readCertificates()
}

// RouteDir is the Traefik dynamic directory route files are written to.
func (e *Engine) RouteDir() string {
	return e.cfg.TraefikDynamicDir
}
