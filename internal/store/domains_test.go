package store_test

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	"dermaga/internal/store"
)

// TestStoreMigration0005To0006_DEC38 upgrades a 0005 database: existing domains stay routed
// (DNS verified and certificate requested at upgrade time), so 0.8.0 does not drop a live site.
func TestStoreMigration0005To0006_DEC38(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "v5.db")
	rawDB, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"0001_init.sql", "0002_sessions_metadata.sql", "0003_github.sql", "0004_m6_apps_deploy.sql", "0005_m7_releases.sql"} {
		b, err := os.ReadFile(filepath.Join("migrations", name))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := rawDB.Exec(string(b)); err != nil {
			t.Fatalf("exec %s: %v", name, err)
		}
	}
	for _, q := range []string{
		`INSERT INTO schema_migrations (version) VALUES (1), (2), (3), (4), (5)`,
		`INSERT INTO apps (id, name, repo_owner, repo_name, port, health_type) VALUES ('app_1', 'web', 'octo', 'web', 8080, 'http')`,
		`INSERT INTO domains (id, app_id, domain, is_primary) VALUES ('dom_1', 'app_1', 'web.example.com', 1)`,
	} {
		if _, err := rawDB.Exec(q); err != nil {
			t.Fatalf("seeding v5: %v\n%s", err, q)
		}
	}
	_ = rawDB.Close()

	s, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("upgrade 0005->0006: %v", err)
	}
	defer s.Close()
	ctx := context.Background()
	if ver, err := s.CurrentVersion(ctx); err != nil || ver != 6 {
		t.Fatalf("expected version 6, got %d (err=%v)", ver, err)
	}
	ds, err := s.ListDomainsByApp(ctx, "app_1")
	if err != nil || len(ds) != 1 {
		t.Fatalf("domains after upgrade: %+v (err=%v)", ds, err)
	}
	if d := ds[0]; !d.Routed() || d.CertRequestedAt == nil || d.CertFailedAt != nil || d.CertError != "" {
		t.Fatalf("an existing domain must stay routed: %+v", d)
	}
}

// TestReplaceDomains_APP2: the set is replaced as a whole, state columns survive, a host used by
// another app is refused by the database and leaves the old set in place.
func TestReplaceDomains_APP2(t *testing.T) {
	s, err := store.Open(filepath.Join(t.TempDir(), "d.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	for _, id := range []string{"app_a", "app_b"} {
		if err := s.CreateApp(ctx, &store.App{ID: id, Name: id[4:] + "x", RepoOwner: "o", RepoName: "r", Port: 80, HealthType: "none"}); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.CreateDomain(ctx, &store.Domain{ID: "dom_b", AppID: "app_b", Domain: "b.example.com", IsPrimary: true}); err != nil {
		t.Fatal(err)
	}
	when := "2026-11-27T00:00:00Z"
	primary := "a.example.com"
	set := []store.Domain{
		{ID: "dom_1", Domain: "a.example.com", IsPrimary: true, DNSVerifiedAt: &when, CertRequestedAt: &when},
		{ID: "dom_2", Domain: "www.a.example.com", RedirectTo: &primary, CertFailedAt: &when, CertError: "boom"},
	}
	if err := s.ReplaceDomains(ctx, "app_a", set); err != nil {
		t.Fatal(err)
	}
	got, _ := s.ListDomainsByApp(ctx, "app_a")
	if len(got) != 2 || !got[0].Routed() || got[1].Routed() || got[1].CertError != "boom" || got[1].RedirectTo == nil {
		t.Fatalf("replaced set: %+v", got)
	}
	if owner, _ := s.DomainOwner(ctx, "b.example.com"); owner != "app_b" {
		t.Fatalf("owner of b.example.com: %q", owner)
	}
	if err := s.ReplaceDomains(ctx, "app_a", []store.Domain{{ID: "dom_3", Domain: "b.example.com", IsPrimary: true}}); err == nil {
		t.Fatal("negative control: a host of another app must be refused (UNIQUE)")
	}
	if got, _ := s.ListDomainsByApp(ctx, "app_a"); len(got) != 2 {
		t.Fatalf("a refused replace must keep the old set, got %+v", got)
	}
	now := "2026-11-27T01:00:00Z"
	got[1].CertFailedAt, got[1].CertError, got[1].CertRequestedAt = nil, "", &now
	if err := s.SetDomainState(ctx, got[1]); err != nil {
		t.Fatal(err)
	}
	if again, _ := s.ListDomainsByApp(ctx, "app_a"); again[1].CertFailedAt != nil || deref(again[1].CertRequestedAt) != now {
		t.Fatalf("state not saved: %+v", again[1])
	}
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
