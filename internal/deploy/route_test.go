package deploy

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"dermaga/internal/host"
	"dermaga/internal/store"
)

func routeFile(t *testing.T, fh *host.FakeHost, e *Engine, appName string) string {
	t.Helper()
	data, err := fh.ReadFile(filepath.Join(e.cfg.TraefikDynamicDir, "app-"+appName+".yml"))
	if err != nil {
		t.Fatalf("route file: %v", err)
	}
	return string(data)
}

func hostRouted(file, host string) bool {
	return strings.Contains(file, "\"rule\": \"Host(`"+host+"`)")
}

// liveApp deploys an app whose primary domain passed the DNS gate.
func liveApp(t *testing.T, s *store.Store, e *Engine, key []byte, appName string) *store.App {
	t.Helper()
	app := newTestApp(t, s, key, appName, nil)
	ds, _ := s.ListDomainsByApp(context.Background(), app.ID)
	when := time.Now().UTC().Format(time.RFC3339)
	ds[0].DNSVerifiedAt, ds[0].CertRequestedAt = &when, &when
	if err := s.SetDomainState(context.Background(), ds[0]); err != nil {
		t.Fatal(err)
	}
	deployAndWait(t, s, e, app.ID, "c1", "active")
	app, _ = s.GetApp(context.Background(), app.ID)
	return app
}

func withDomain(t *testing.T, s *store.Store, appID string, d store.Domain) []store.Domain {
	t.Helper()
	ds, _ := s.ListDomainsByApp(context.Background(), appID)
	return append(ds, d)
}

func journalMock(lines *string) func(string, ...string) ([]byte, error) {
	return func(name string, args ...string) ([]byte, error) {
		if name == "journalctl" {
			return []byte(*lines), nil
		}
		return nil, nil
	}
}

// TestApplyRoute_ProvenThenCommitted_APP10: the new route goes live through the verify router,
// the final file has no verify router, and only then is the database change committed.
func TestApplyRoute_ProvenThenCommitted_APP10(t *testing.T) {
	s, fh, eng, key := setupTestEngine(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	_ = eng.Start(ctx)
	app := liveApp(t, s, eng, key, "shop")
	now := time.Now().UTC().Format(time.RFC3339)
	set := withDomain(t, s, app.ID, store.Domain{ID: "dom_alias", Domain: "store.example.com", DNSVerifiedAt: &now, CertRequestedAt: &now})

	var sawVerify bool
	inner := eng.cfg.VerifyRoute
	eng.cfg.VerifyRoute = func(ctx context.Context, h, p, tok string) (bool, error) {
		sawVerify = strings.Contains(routeFile(t, fh, eng, "shop"), "app-shop-verify")
		return inner(ctx, h, p, tok)
	}
	committed := false
	if err := eng.ApplyRoute(ctx, app.ID, set, func(ctx context.Context) error {
		committed = true
		return s.ReplaceDomains(ctx, app.ID, set)
	}); err != nil {
		t.Fatalf("ApplyRoute: %v", err)
	}
	f := routeFile(t, fh, eng, "shop")
	if !sawVerify || !committed || !hostRouted(f, "store.example.com") || strings.Contains(f, "verify") {
		t.Fatalf("verify seen=%v committed=%v file:\n%s", sawVerify, committed, f)
	}
}

// TestApplyRoute_TraefikRejects_RestoresAndKeepsDB_APP10: when Traefik never loads the file, the
// previous file is written back, the database is not touched and Traefik's error is returned.
func TestApplyRoute_TraefikRejects_RestoresAndKeepsDB_APP10(t *testing.T) {
	s, fh, eng, key := setupTestEngine(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	_ = eng.Start(ctx)
	app := liveApp(t, s, eng, key, "shop")
	before := routeFile(t, fh, eng, "shop")

	logLines := `{"level":"error","error":"middleware \"x@file\" does not exist","message":"Error while building configuration"}`
	fh.CommandMock = journalMock(&logLines)
	eng.cfg.VerifyRoute = func(context.Context, string, string, string) (bool, error) { return false, nil }
	now := time.Now().UTC().Format(time.RFC3339)
	set := withDomain(t, s, app.ID, store.Domain{ID: "dom_alias", Domain: "store.example.com", DNSVerifiedAt: &now, CertRequestedAt: &now})
	err := eng.ApplyRoute(ctx, app.ID, set, func(context.Context) error {
		t.Fatal("commit must not run when Traefik did not load the route")
		return nil
	})
	if err == nil || !strings.Contains(err.Error(), "does not exist") || !strings.Contains(err.Error(), "previous route and settings stay") {
		t.Fatalf("want Traefik's error, got %v", err)
	}
	if after := routeFile(t, fh, eng, "shop"); after != before {
		t.Fatalf("previous route not restored:\n%s", after)
	}
	if ds, _ := s.ListDomainsByApp(ctx, app.ID); len(ds) != 1 {
		t.Fatalf("database changed: %+v", ds)
	}
}

// TestApplyRoute_RefusedWhileSwitching: a route edit gets ErrSwitchRunning (409) while a release
// of the app holds the app lock (owner decision).
func TestApplyRoute_RefusedWhileSwitching(t *testing.T) {
	s, _, eng, key := setupTestEngine(t)
	app := newTestApp(t, s, key, "shop", nil)
	lock := eng.appLock(app.ID)
	lock <- struct{}{}
	defer func() { <-lock }()
	if err := eng.ApplyRoute(context.Background(), app.ID, nil, nil); !errors.Is(err, ErrSwitchRunning) {
		t.Fatalf("want ErrSwitchRunning, got %v", err)
	}
}

// TestSyncApp_DNSGate_DEC38: a domain whose DNS check has not passed gets no router; it is checked
// again only after 5 minutes (or at once with "Check again"); once it passes it enters the route.
func TestSyncApp_DNSGate_DEC38(t *testing.T) {
	s, fh, eng, key := setupTestEngine(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	_ = eng.Start(ctx)
	app := liveApp(t, s, eng, key, "shop")
	if err := s.ReplaceDomains(ctx, app.ID, withDomain(t, s, app.ID, store.Domain{ID: "dom_new", Domain: "new.example.com"})); err != nil {
		t.Fatal(err)
	}
	clock := time.Date(2026, 11, 27, 10, 0, 0, 0, time.UTC)
	eng.cfg.Now = func() time.Time { return clock }
	dnsOK, checks := false, 0
	eng.cfg.DNSMatches = func(_ context.Context, d string) bool { checks++; return dnsOK }

	if err := eng.SyncApp(ctx, app.ID, false); err != nil {
		t.Fatal(err)
	}
	if f := routeFile(t, fh, eng, "shop"); hostRouted(f, "new.example.com") || !hostRouted(f, "shop.example.com") {
		t.Fatalf("negative control: a domain with wrong DNS must not be routed:\n%s", f)
	}
	dnsOK = true
	clock = clock.Add(4 * time.Minute)
	_ = eng.SyncApp(ctx, app.ID, false)
	if checks != 1 {
		t.Fatalf("DNS must be rechecked only every 5 minutes, got %d checks", checks)
	}
	if err := eng.SyncApp(ctx, app.ID, true); err != nil {
		t.Fatal(err)
	}
	if f := routeFile(t, fh, eng, "shop"); !hostRouted(f, "new.example.com") || strings.Contains(f, "verify") {
		t.Fatalf("a domain whose DNS passed must be routed:\n%s", f)
	}
	ds, _ := s.ListDomainsByApp(ctx, app.ID)
	if ds[1].DNSVerifiedAt == nil || ds[1].CertRequestedAt == nil {
		t.Fatalf("state not saved: %+v", ds[1])
	}
}

// TestSyncApp_FailedOrderLeavesRoute_DEC38: a routed domain without a certificate whose order
// failed after it entered the route leaves it, comes back after an hour or on "Check again"; a
// domain with a certificate stays even with an error in the log.
func TestSyncApp_FailedOrderLeavesRoute_DEC38(t *testing.T) {
	s, fh, eng, key := setupTestEngine(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	_ = eng.Start(ctx)
	app := liveApp(t, s, eng, key, "shop")
	clock := time.Date(2026, 11, 27, 10, 0, 0, 0, time.UTC)
	eng.cfg.Now = func() time.Time { return clock }
	req := clock.Add(-time.Minute).Format(time.RFC3339)
	set := withDomain(t, s, app.ID, store.Domain{ID: "dom_bad", Domain: "bad.example.com", DNSVerifiedAt: &req, CertRequestedAt: &req})
	if err := s.ReplaceDomains(ctx, app.ID, set); err != nil {
		t.Fatal(err)
	}
	eng.cfg.ACMEPath = "/acme.json"
	fh.Files["/acme.json"] = acmeWith(t, "shop.example.com")
	logLines := acmeErrorLine("bad.example.com", clock.Add(-30*time.Second), "urn:ietf:params:acme:error:connection :: timeout") + "\n" +
		acmeErrorLine("shop.example.com", clock.Add(-30*time.Second), "urn:ietf:params:acme:error:connection :: refused")
	fh.CommandMock = journalMock(&logLines)

	if err := eng.SyncApp(ctx, app.ID, false); err != nil {
		t.Fatal(err)
	}
	f := routeFile(t, fh, eng, "shop")
	if hostRouted(f, "bad.example.com") || !hostRouted(f, "shop.example.com") {
		t.Fatalf("failed domain must leave the route, the one with a certificate stays:\n%s", f)
	}
	ds, _ := s.ListDomainsByApp(ctx, app.ID)
	st := DomainStatusOf(ds[1], eng.Certificates(), clock) // primary first, then by name
	if ds[1].Domain != "bad.example.com" || st.Certificate != "failed" || !st.NextRetryAt.Equal(clock.Add(time.Hour)) || !strings.Contains(st.Reason, "in time") {
		t.Fatalf("status of the failed domain: %+v %+v", ds[1], st)
	}
	if st := DomainStatusOf(ds[0], eng.Certificates(), clock); st.Certificate != "active" {
		t.Fatalf("primary status: %+v", st)
	}

	clock = clock.Add(59 * time.Minute)
	_ = eng.SyncApp(ctx, app.ID, false)
	if hostRouted(routeFile(t, fh, eng, "shop"), "bad.example.com") {
		t.Fatal("must stay out of the route for an hour")
	}
	clock = clock.Add(time.Minute)
	_ = eng.SyncApp(ctx, app.ID, false)
	if !hostRouted(routeFile(t, fh, eng, "shop"), "bad.example.com") {
		t.Fatal("the hourly retry must put it back")
	}
	// The old error is older than the new request: it does not count again.
	_ = eng.SyncApp(ctx, app.ID, false)
	if !hostRouted(routeFile(t, fh, eng, "shop"), "bad.example.com") {
		t.Fatal("an error from before the retry must not count")
	}
}

// TestSyncApp_RewritesOutdatedRoute: a route file in an older format (0.7.0's single router) is
// rewritten with proof at the first sync after an upgrade; an up-to-date file is left alone.
func TestSyncApp_RewritesOutdatedRoute(t *testing.T) {
	s, fh, eng, key := setupTestEngine(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	_ = eng.Start(ctx)
	app := liveApp(t, s, eng, key, "shop")
	path := filepath.Join(eng.cfg.TraefikDynamicDir, "app-shop.yml")
	current := routeFile(t, fh, eng, "shop")
	_ = fh.WriteFileAtomic(path, []byte(strings.Replace(current, "app-shop-host-0", "app-shop", 1)), 0600)
	verifies := 0
	inner := eng.cfg.VerifyRoute
	eng.cfg.VerifyRoute = func(ctx context.Context, h, p, tok string) (bool, error) { verifies++; return inner(ctx, h, p, tok) }
	if err := eng.SyncApp(ctx, app.ID, false); err != nil {
		t.Fatal(err)
	}
	if routeFile(t, fh, eng, "shop") != current || verifies != 1 {
		t.Fatalf("outdated route not rewritten with proof (verifies=%d)", verifies)
	}
	_ = eng.SyncApp(ctx, app.ID, false)
	if verifies != 1 {
		t.Fatal("negative control: an up-to-date route must not be rewritten")
	}
}

func acmeErrorLine(domain string, at time.Time, detail string) string {
	b, _ := json.Marshal(map[string]any{"level": "error", "domains": []string{domain}, "error": detail, "time": at.Format(time.RFC3339),
		"message": "Unable to obtain ACME certificate for domains"})
	return string(b)
}

func acmeWith(t *testing.T, host string) []byte {
	t.Helper()
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	tmpl := &x509.Certificate{SerialNumber: big.NewInt(1), DNSNames: []string{host}, NotBefore: time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC), NotAfter: time.Date(2027, 1, 30, 0, 0, 0, 0, time.UTC)}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	cert := base64.StdEncoding.EncodeToString(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))
	return []byte(fmt.Sprintf(`{"le":{"Certificates":[{"domain":{"main":%q},"certificate":%q,"key":"x","Store":"default"}]}}`, host, cert))
}

// TestSyncDomains_FailureLoggedOnce: a route that keeps failing is one Activity entry, not one per
// minute; after a success a new failure is logged again.
func TestSyncDomains_FailureLoggedOnce(t *testing.T) {
	s, fh, eng, key := setupTestEngine(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	_ = eng.Start(ctx)
	app := liveApp(t, s, eng, key, "shop")
	path := filepath.Join(eng.cfg.TraefikDynamicDir, "app-shop.yml")
	outdated := func() { _ = fh.WriteFileAtomic(path, []byte("# managed by dermaga; app=shop\n{}\n"), 0600) }
	failing := true
	inner := eng.cfg.VerifyRoute
	eng.cfg.VerifyRoute = func(ctx context.Context, h, p, tok string) (bool, error) {
		if failing {
			return false, nil
		}
		return inner(ctx, h, p, tok)
	}
	count := func() int {
		n := 0
		logs, _ := s.ListAuditLogs(ctx, store.AuditFilter{AppID: &app.ID, Event: "route_sync_failed", Limit: 100})
		for _, l := range logs {
			if l.Event == "route_sync_failed" {
				n++
			}
		}
		return n
	}
	for i := 0; i < 3; i++ {
		outdated()
		eng.SyncDomains(ctx)
	}
	if n := count(); n != 1 {
		t.Fatalf("a lasting failure must be logged once, got %d", n)
	}
	failing = false
	eng.SyncDomains(ctx)
	failing = true
	outdated()
	eng.SyncDomains(ctx)
	if n := count(); n != 2 {
		t.Fatalf("a failure after a success must be logged again, got %d", n)
	}
}

// TestApplyRoute_UsesRouteTimeout_APP10: a route change Traefik does not load gives up after
// RouteTimeout, not the 30 s switch timeout, so the panel gets the answer (found on dermaga-vm with
// v0.8.0-rc.1: the panel's 15 s write timeout cut the reply).
func TestApplyRoute_UsesRouteTimeout_APP10(t *testing.T) {
	s, _, eng, key := setupTestEngine(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	_ = eng.Start(ctx)
	app := liveApp(t, s, eng, key, "shop")
	eng.cfg.SwitchTimeout, eng.cfg.RouteTimeout = 5*time.Second, 100*time.Millisecond
	eng.cfg.VerifyRoute = func(context.Context, string, string, string) (bool, error) { return false, nil }
	start := time.Now()
	if err := eng.ApplyRoute(ctx, app.ID, withDomain(t, s, app.ID, store.Domain{ID: "dom_x", Domain: "x.example.com"}), nil); err == nil {
		t.Fatal("want an error")
	}
	if d := time.Since(start); d > 2*time.Second {
		t.Fatalf("ApplyRoute waited %s; it must use RouteTimeout", d)
	}
}
