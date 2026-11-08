package apps

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	_ "github.com/r3p-dev/dermaga/internal/golden" // accepts go test ./... -update
	"github.com/r3p-dev/dermaga/internal/secrets"
	"github.com/r3p-dev/dermaga/internal/spec"
	"github.com/r3p-dev/dermaga/internal/store"
)

func testStore(t *testing.T) Store {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	box, _ := secrets.New(make([]byte, 32))
	return Store{DB: db, Box: box}
}

func portfolio() spec.App {
	return spec.App{Name: "portfolio", Port: 4321,
		Domains: []spec.Domain{{Host: "r3p.dev", Primary: true}, {Host: "www.r3p.dev", Redirect: true}},
		Source:  spec.Source{Type: "git", Repo: "git@github.com:r3p-dev/portfolio.git", Branch: "main", AutoDeploy: true},
		Health:  spec.Health{Path: "/robots.txt"}}
}

func TestCreateGetSaveAndSchema(t *testing.T) {
	s, ctx := testStore(t), context.Background()
	id, secret, err := s.Create(ctx, portfolio())
	if err != nil || len(id) != 16 || len(secret) != 64 {
		t.Fatal(id, secret, err)
	}
	a, err := s.Get(ctx, "portfolio")
	if err != nil || a.ID != id || a.Spec.Primary() != "r3p.dev" || len(a.Spec.Domains) != 2 || a.Spec.Health.Path != "/robots.txt" || a.Live != nil {
		t.Fatalf("%+v %v", a, err)
	}
	_, got, err := s.ByID(ctx, id)
	if err != nil || got != secret {
		t.Fatal("webhook secret round trip", err)
	}
	var sealed string
	s.DB.QueryRow(`SELECT webhook_secret FROM apps`).Scan(&sealed)
	if strings.Contains(sealed, secret) {
		t.Fatal("webhook secret stored in plaintext")
	}
	if _, err := s.Get(ctx, "nope"); err == nil {
		t.Fatal("missing app found")
	}

	// a host belongs to one app
	other := portfolio()
	other.Name, other.Domains = "blog", []spec.Domain{{Host: "www.r3p.dev", Primary: true}}
	if _, _, err := s.Create(ctx, other); err == nil || !strings.Contains(err.Error(), "already belongs") {
		t.Fatal("duplicate host accepted:", err)
	}
	if _, err := s.Get(ctx, "blog"); err == nil {
		t.Fatal("failed create left the app behind")
	}
	a.Spec.Domains = a.Spec.Domains[:1]
	a.Spec.Port = 8080
	if err := s.Save(ctx, id, a.Spec); err != nil {
		t.Fatal(err)
	}
	if a, _ = s.Get(ctx, "portfolio"); len(a.Spec.Domains) != 1 || a.Spec.Port != 8080 {
		t.Fatalf("save: %+v", a.Spec)
	}
	a.Spec.Port = 0
	if err := s.Save(ctx, id, a.Spec); err == nil {
		t.Fatal("invalid spec saved")
	}
}

func TestReleasesPromoteAndDeliveries(t *testing.T) {
	s, ctx := testStore(t), context.Background()
	id, _, _ := s.Create(ctx, portfolio())
	r1, _ := s.NewRelease(ctx, id, "manual", "")
	r2, _ := s.NewRelease(ctx, id, "webhook", "abc1234")
	r1.Image = "localhost/dermaga/portfolio:1"
	r2.Image = "localhost/dermaga/portfolio:2"
	if err := s.PromoteLive(ctx, id, r1); err != nil {
		t.Fatal(err)
	}
	if err := s.PromoteLive(ctx, id, r2); err != nil {
		t.Fatal(err)
	}
	a, _ := s.Get(ctx, "portfolio")
	if a.Live == nil || a.Live.Rel8 != r2.Rel8 {
		t.Fatalf("live: %+v", a.Live)
	}
	if got, _ := s.Release(ctx, id, r1.Rel8); got.Status != "superseded" {
		t.Fatalf("previous live: %s", got.Status)
	}
	if _, err := s.DB.Exec(`UPDATE releases SET status = 'live' WHERE id = ?`, r1.ID); err == nil {
		t.Fatal("second live release accepted by the schema")
	}
	r3, _ := s.NewRelease(ctx, id, "manual", "")
	if u, _ := s.Unfinished(ctx); len(u) != 1 || u[0].Rel8 != r3.Rel8 {
		t.Fatalf("unfinished: %+v", u)
	}

	if dup, err := s.RecordDelivery(ctx, "d1", id, "queued"); dup || err != nil {
		t.Fatal(dup, err)
	}
	if dup, err := s.RecordDelivery(ctx, "d1", id, "queued"); !dup || err != nil {
		t.Fatal("duplicate delivery not detected", err)
	}

	// Rollback target selection and rollback promotion status
	tgt, err := s.RollbackTarget(ctx, id, "")
	if err != nil || tgt.Rel8 != r1.Rel8 {
		t.Fatalf("target %v err %v", tgt, err)
	}
	rRollback, _ := s.NewRelease(ctx, id, "rollback", tgt.Rel8)
	rRollback.Image = tgt.Image
	if err := s.PromoteLive(ctx, id, rRollback); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.Release(ctx, id, r2.Rel8); got.Status != "rolled_back" {
		t.Fatalf("expected rolled_back for r2, got %s", got.Status)
	}
	if _, err := s.RollbackTarget(ctx, id, rRollback.Rel8); err == nil {
		t.Fatal("expected error targeting current live release")
	}
	if tgt2, err := s.RollbackTarget(ctx, id, r2.Rel8); err != nil || tgt2.Rel8 != r2.Rel8 {
		t.Fatalf("target r2: %v, err %v", tgt2, err)
	}
}

func TestEnv(t *testing.T) {
	s, ctx := testStore(t), context.Background()
	id, _, _ := s.Create(ctx, portfolio())
	if err := s.SetEnv(ctx, id, map[string]string{"DATABASE_URL": "postgres://u:p@db/x", "EMPTY": ""}, nil); err != nil {
		t.Fatal(err)
	}
	if err := s.SetEnv(ctx, id, map[string]string{"DATABASE_URL": "postgres://u:p2@db/x"}, []string{"EMPTY"}); err != nil {
		t.Fatal(err)
	}
	env, err := s.Env(ctx, id)
	if err != nil || len(env) != 1 || env["DATABASE_URL"] != "postgres://u:p2@db/x" {
		t.Fatal(env, err)
	}
	if string(EnvFile(map[string]string{"B": "2=x", "A": "1"})) != "A=1\nB=2=x\n" {
		t.Fatal("env file format")
	}
	for k, v := range map[string]string{"1X": "v", "A-B": "v", "OK": "line\nINJECTED=1", "OK2": "a\rb", "OK3": "a\x00"} {
		if err := s.SetEnv(ctx, id, map[string]string{k: v}, nil); err == nil {
			t.Errorf("env %q=%q accepted", k, v)
		}
	}
}
