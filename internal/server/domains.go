package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"

	"dermaga/internal/access"
	"dermaga/internal/auth"
	"dermaga/internal/deploy"
	"dermaga/internal/store"
	"dermaga/internal/traefik"
)

// MaxDomainsPerApp keeps one app's route and its certificate requests reviewable.
const MaxDomainsPerApp = 20

var pathPrefixRegex = regexp.MustCompile(`^(/[A-Za-z0-9._~-]+)+$`)

func (s *Server) handleListDomains(w http.ResponseWriter, r *http.Request) {
	app, ok := s.appFromPath(w, r)
	if !ok {
		return
	}
	s.writeDomains(w, r.Context(), app.ID, http.StatusOK)
}

// handleUpdateDomains replaces the app's domain set and applies the route with proof before it is
// saved (APP-2, APP-10, DEC-40).
func (s *Server) handleUpdateDomains(w http.ResponseWriter, r *http.Request) {
	app, ok := s.appFromPath(w, r)
	if !ok {
		return
	}
	var req UpdateDomainsRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64*1024)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	set, fields := s.buildDomainSet(r.Context(), app.ID, req)
	if len(fields) > 0 {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "validation failed", "fields": fields})
		return
	}
	err := s.cfg.Deploy.ApplyRoute(r.Context(), app.ID, set, func(ctx context.Context) error {
		return s.cfg.Store.ReplaceDomains(ctx, app.ID, set)
	})
	if s.writeRouteError(w, err) {
		return
	}
	names := make([]string, 0, len(set))
	for _, d := range set {
		names = append(names, d.Domain)
	}
	_ = s.cfg.Store.RecordAudit(r.Context(), "panel", "admin", "domains_update",
		fmt.Sprintf("Domains of %s: %s", app.Name, strings.Join(names, ", ")), &app.ID, nil)
	s.writeDomains(w, r.Context(), app.ID, http.StatusOK)
}

// handleCheckDomainsAgain checks DNS of waiting domains and retries failed certificates now (DEC-38).
func (s *Server) handleCheckDomainsAgain(w http.ResponseWriter, r *http.Request) {
	app, ok := s.appFromPath(w, r)
	if !ok {
		return
	}
	if s.writeRouteError(w, s.cfg.Deploy.SyncApp(r.Context(), app.ID, true)) {
		return
	}
	s.writeDomains(w, r.Context(), app.ID, http.StatusOK)
}

func (s *Server) writeRouteError(w http.ResponseWriter, err error) bool {
	switch {
	case err == nil:
		return false
	case errors.Is(err, deploy.ErrSwitchRunning):
		writeError(w, http.StatusConflict, "A release of this app is starting or switching traffic. Save again when it is live.")
	default:
		writeError(w, http.StatusBadGateway, err.Error())
	}
	return true
}

func (s *Server) appFromPath(w http.ResponseWriter, r *http.Request) (*store.App, bool) {
	if s.cfg.Deploy == nil {
		writeError(w, http.StatusInternalServerError, "deployment engine is not configured")
		return nil, false
	}
	app, err := s.cfg.Store.GetApp(r.Context(), r.PathValue("id"))
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "app not found")
		return nil, false
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load app")
		return nil, false
	}
	return app, true
}

// buildDomainSet validates a requested domain set and turns it into rows: existing domains keep
// their DNS and certificate state, new ones pass the DNS gate if their DNS already points here.
// Field errors are keyed "domains.<index>", "domains" or "path_prefix".
func (s *Server) buildDomainSet(ctx context.Context, appID string, req UpdateDomainsRequest) ([]store.Domain, map[string]string) {
	fields := map[string]string{}
	if len(req.Domains) == 0 || len(req.Domains) > MaxDomainsPerApp {
		fields["domains"] = fmt.Sprintf("An app needs between 1 and %d domains.", MaxDomainsPerApp)
		return nil, fields
	}
	prefix := strings.TrimSpace(req.PathPrefix)
	if prefix == "/" {
		prefix = ""
	}
	if prefix != "" && (len(prefix) > 100 || !pathPrefixRegex.MatchString(prefix)) {
		fields["path_prefix"] = "Use a path like /blog: it starts with /, has no spaces and does not end with /."
	}

	hook, _ := s.cfg.Store.GetSetting(ctx, auth.SettingHookDomain)
	existing, _ := s.cfg.Store.ListDomainsByApp(ctx, appID)
	old := map[string]store.Domain{}
	for _, d := range existing {
		old[d.Domain] = d
	}
	seen := map[string]bool{}
	primary := ""
	for i, in := range req.Domains {
		key := fmt.Sprintf("domains.%d", i)
		host := strings.TrimSuffix(strings.ToLower(strings.TrimSpace(in.Domain)), ".")
		req.Domains[i].Domain = host
		switch {
		case strings.HasPrefix(host, "*."):
			fields[key] = "Wildcard domains are not supported: each domain gets its own certificate. Add the subdomains one by one."
		case strings.IndexFunc(host, func(r rune) bool { return r > 127 }) >= 0:
			fields[key] = "Enter internationalized domains in their xn-- form (your DNS provider shows it)."
		case traefik.ValidateDomain(host) != nil || !strings.Contains(host, "."):
			fields[key] = "This is not a valid domain name, for example app.example.com."
		case seen[host]:
			fields[key] = "This domain is listed twice."
		case hook != "" && host == hook:
			fields[key] = "This is the hook domain GitHub sends pushes to; it cannot serve an app."
		default:
			if owner, err := s.cfg.Store.DomainOwner(ctx, host); err == nil && owner != "" && owner != appID {
				fields[key] = "This domain already belongs to another app."
			}
		}
		seen[host] = true
		switch in.Role {
		case "primary":
			if primary != "" {
				fields[key] = "Only one domain can be the primary domain."
			}
			primary = host
		case "alias", "redirect":
		default:
			fields[key] = "The role must be primary, alias or redirect."
		}
	}
	if primary == "" && fields["domains"] == "" {
		fields["domains"] = "Choose one primary domain."
	}
	if len(fields) > 0 {
		return nil, fields
	}

	now := time.Now().UTC().Format(time.RFC3339)
	serverIP, _ := s.cfg.Store.GetSetting(ctx, access.SettingAddress)
	set := make([]store.Domain, 0, len(req.Domains))
	for _, in := range req.Domains {
		d, ok := old[in.Domain]
		if !ok {
			d = store.Domain{ID: "dom_" + newRandomHex(12), Domain: in.Domain}
			if access.CheckDomainDNS(ctx, in.Domain, serverIP, s.cfg.DNSLookup).PointsHere() {
				d.DNSVerifiedAt, d.CertRequestedAt = &now, &now
			}
		}
		d.IsPrimary, d.RedirectTo, d.PathPrefix, d.StripPrefix = in.Role == "primary", nil, nil, false
		if in.Role == "redirect" {
			target := primary
			d.RedirectTo = &target
		} else if prefix != "" {
			p := prefix
			d.PathPrefix, d.StripPrefix = &p, req.StripPrefix
		}
		set = append(set, d)
	}
	return set, nil
}

func (s *Server) writeDomains(w http.ResponseWriter, ctx context.Context, appID string, status int) {
	app, err := s.cfg.Store.GetApp(ctx, appID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load app")
		return
	}
	domains, err := s.cfg.Store.ListDomainsByApp(ctx, appID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load domains")
		return
	}
	var certs map[string]traefik.Certificate
	if s.cfg.Deploy != nil {
		certs = s.cfg.Deploy.Certificates()
	}
	now := time.Now()
	resp := DomainsResponse{Domains: make([]DomainView, 0, len(domains)), Live: app.ActiveReleaseID != nil}
	resp.ServerIP, _ = s.cfg.Store.GetSetting(ctx, access.SettingAddress)
	for _, d := range domains {
		v := DomainView{Domain: d.Domain, Role: "alias", DNSVerified: d.DNSVerifiedAt != nil,
			Certificate: certificateInfo(deploy.DomainStatusOf(d, certs, now), d.CertError)}
		switch {
		case d.IsPrimary:
			v.Role = "primary"
			if d.PathPrefix != nil {
				resp.PathPrefix, resp.StripPrefix = *d.PathPrefix, d.StripPrefix
			}
		case d.RedirectTo != nil:
			v.Role = "redirect"
		}
		if !v.DNSVerified {
			v.DNS, _ = s.checkHookDomainDNS(ctx, d.Domain)
		}
		resp.Domains = append(resp.Domains, v)
	}
	writeJSON(w, status, resp)
}

func certificateInfo(st deploy.DomainStatus, detail string) CertificateInfo {
	ci := CertificateInfo{Status: st.Certificate, ExpiringSoon: st.ExpiringSoon, Reason: st.Reason, Fix: st.Fix}
	if !st.ExpiresAt.IsZero() {
		ci.ExpiresAt = st.ExpiresAt.UTC().Format(time.RFC3339)
	}
	if st.Certificate == "failed" {
		ci.Detail = detail
		ci.NextRetryAt = st.NextRetryAt.UTC().Format(time.RFC3339)
	}
	return ci
}

// certificateWarnings lists app and hook domain certificates close to expiry (TLS-4).
func (s *Server) certificateWarnings(ctx context.Context) []string {
	if s.cfg.Deploy == nil {
		return nil
	}
	hosts, _ := s.cfg.Store.ListDomainHosts(ctx)
	if hook, _ := s.cfg.Store.GetSetting(ctx, auth.SettingHookDomain); hook != "" {
		hosts = append(hosts, hook)
	}
	return traefik.ExpiringCertificates(s.cfg.Deploy.Certificates(), hosts, time.Now())
}

// hookDomainCertificate is the hook domain's certificate status (TLS-5); it has no DNS gate.
func (s *Server) hookDomainCertificate(domain string) *CertificateInfo {
	if s.cfg.Deploy == nil || domain == "" {
		return nil
	}
	if c, ok := s.cfg.Deploy.Certificates()[domain]; ok {
		return &CertificateInfo{Status: "active", ExpiresAt: c.NotAfter.UTC().Format(time.RFC3339), ExpiringSoon: c.ExpiringSoon(time.Now())}
	}
	return &CertificateInfo{Status: "requesting"}
}
