package store_test

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	"dermaga/internal/store"
)

// TestStoreMigration0004To0005_DEP4 upgrades a populated 0004 database: every app's current env
// becomes env version 1, deployments keep their data, legacy snapshots are listed for conversion,
// and the rebuilt CHECK accepts rolled_back.
func TestStoreMigration0004To0005_DEP4(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "v4.db")
	rawDB, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	for _, name := range []string{"0001_init.sql", "0002_sessions_metadata.sql", "0003_github.sql", "0004_m6_apps_deploy.sql"} {
		b, err := os.ReadFile(filepath.Join("migrations", name))
		if err != nil {
			t.Fatalf("reading %s: %v", name, err)
		}
		if _, err := rawDB.Exec(string(b)); err != nil {
			t.Fatalf("exec %s: %v", name, err)
		}
	}
	for _, q := range []string{
		`INSERT INTO schema_migrations (version) VALUES (1), (2), (3), (4)`,
		`INSERT INTO apps (id, name, repo_owner, repo_name, port, health_type, env_encrypted) VALUES ('app_1', 'web', 'octo', 'web', 8080, 'http', 'sealed-current')`,
		`INSERT INTO deployments (id, app_id, release_number, status, trigger, commit_hash, commit_message, env_snapshot_encrypted, image_ref) VALUES ('dep_1', 'app_1', 1, 'active', 'manual', 'abc', 'one', 'sealed-snap-1', 'localhost/dermaga/web:r1')`,
		`INSERT INTO deployments (id, app_id, release_number, status, trigger, commit_hash, commit_message, env_snapshot_encrypted) VALUES ('dep_2', 'app_1', 2, 'superseded', 'push', 'def', 'two', '')`,
		`UPDATE apps SET active_release_id = 'dep_1' WHERE id = 'app_1'`,
	} {
		if _, err := rawDB.Exec(q); err != nil {
			t.Fatalf("seeding v4: %v\n%s", err, q)
		}
	}
	_ = rawDB.Close()

	s, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("store.Open upgrade 0004->0005: %v", err)
	}
	defer s.Close()
	ctx := context.Background()

	if ver, err := s.CurrentVersion(ctx); err != nil || ver != 5 {
		t.Fatalf("expected version 5, got %d (err=%v)", ver, err)
	}
	app, err := s.GetApp(ctx, "app_1")
	if err != nil || app.EnvVersionID == nil || app.WatchPaths != "" {
		t.Fatalf("upgraded app: %+v (err=%v)", app, err)
	}
	v, err := s.GetEnvVersion(ctx, *app.EnvVersionID)
	if err != nil || v.Version != 1 || v.EnvEncrypted != "sealed-current" {
		t.Fatalf("env version 1: %+v (err=%v)", v, err)
	}
	dep, err := s.GetDeployment(ctx, "dep_1")
	if err != nil || dep.Status != "active" || dep.ImageRef != "localhost/dermaga/web:r1" || dep.EnvSnapshotEncrypted != "sealed-snap-1" || dep.EnvVersionID != nil {
		t.Fatalf("migrated deployment: %+v (err=%v)", dep, err)
	}
	legacy, err := s.ListLegacySnapshotDeployments(ctx)
	if err != nil || len(legacy) != 1 || legacy[0].ID != "dep_1" {
		t.Fatalf("legacy snapshots: %+v (err=%v)", legacy, err)
	}
	if err := s.UpdateDeploymentStatus(ctx, "dep_2", "rolled_back", "", nil); err != nil {
		t.Fatalf("rolled_back must be accepted: %v", err)
	}
	if err := s.UpdateDeploymentStatus(ctx, "dep_2", "bogus", "", nil); err == nil {
		t.Fatalf("negative control: an unknown status must violate the CHECK constraint")
	}

	// The daemon converts the legacy snapshot into an env version of its own.
	if err := s.AddLegacyEnvVersion(ctx, "dep_1", "app_1", "sealed-converted", `{"port":8080}`); err != nil {
		t.Fatalf("AddLegacyEnvVersion: %v", err)
	}
	dep, _ = s.GetDeployment(ctx, "dep_1")
	if dep.EnvVersionID == nil || dep.EnvSnapshotEncrypted != "" || dep.RuntimeConfig != `{"port":8080}` {
		t.Fatalf("converted deployment: %+v", dep)
	}
	if v, _ := s.GetEnvVersion(ctx, *dep.EnvVersionID); v.Version != 2 || v.EnvEncrypted != "sealed-converted" {
		t.Fatalf("converted env version: %+v", v)
	}
	if app2, _ := s.GetApp(ctx, "app_1"); *app2.EnvVersionID != *app.EnvVersionID {
		t.Fatalf("legacy conversion must not change the app's current env version")
	}
	if legacy, _ := s.ListLegacySnapshotDeployments(ctx); len(legacy) != 0 {
		t.Fatalf("expected no legacy snapshots left, got %d", len(legacy))
	}
}

// TestEnvVersions_DEC32: creating an app stores version 1; saving a new env adds a version; saving
// other settings with the same ciphertext does not; cleanup keeps current and retained versions.
func TestEnvVersions_DEC32(t *testing.T) {
	s, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()
	ctx := context.Background()

	app := &store.App{ID: "app_1", Name: "web", RepoOwner: "o", RepoName: "r", Port: 80, HealthType: "none", EnvEncrypted: "env-a"}
	if err := s.CreateApp(ctx, app); err != nil {
		t.Fatalf("CreateApp: %v", err)
	}
	v1 := *app.EnvVersionID

	app.MemoryLimit = "256m" // settings-only save: same ciphertext
	if err := s.UpdateApp(ctx, app); err != nil {
		t.Fatalf("UpdateApp settings: %v", err)
	}
	if got, _ := s.GetApp(ctx, "app_1"); *got.EnvVersionID != v1 {
		t.Fatalf("a settings-only save must not create an env version")
	}

	app.EnvEncrypted = "env-b"
	if err := s.UpdateApp(ctx, app); err != nil {
		t.Fatalf("UpdateApp env: %v", err)
	}
	got, _ := s.GetApp(ctx, "app_1")
	v2 := *got.EnvVersionID
	if v2 == v1 {
		t.Fatalf("an env save must create a new version")
	}
	if v, _ := s.GetEnvVersion(ctx, v2); v.Version != 2 || v.EnvEncrypted != "env-b" {
		t.Fatalf("version 2: %+v", v)
	}
	if v, _ := s.GetEnvVersion(ctx, v1); v.EnvEncrypted != "env-a" {
		t.Fatalf("version 1 must stay unchanged: %+v", v)
	}

	app.EnvEncrypted = "env-c"
	_ = s.UpdateApp(ctx, app)
	got, _ = s.GetApp(ctx, "app_1")
	v3 := *got.EnvVersionID

	// Keep v1 (a retained release uses it); v2 is unused; v3 is current.
	n, err := s.DeleteEnvVersionsExcept(ctx, "app_1", []string{v1})
	if err != nil || n != 1 {
		t.Fatalf("DeleteEnvVersionsExcept: n=%d err=%v", n, err)
	}
	if _, err := s.GetEnvVersion(ctx, v2); err == nil {
		t.Fatalf("unused v2 should be deleted")
	}
	for _, id := range []string{v1, v3} {
		if _, err := s.GetEnvVersion(ctx, id); err != nil {
			t.Fatalf("version %s must be kept: %v", id, err)
		}
	}
}

// TestFinishSwitch_DEP1 records a switch atomically: new release live, old one previous or rolled back.
func TestFinishSwitch_DEP1(t *testing.T) {
	s, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()
	ctx := context.Background()
	_ = s.CreateApp(ctx, &store.App{ID: "app_1", Name: "web", RepoOwner: "o", RepoName: "r", Port: 80, HealthType: "none"})
	for i, st := range []string{"active", "switching"} {
		_ = s.CreateDeployment(ctx, &store.Deployment{ID: []string{"dep_1", "dep_2"}[i], AppID: "app_1", ReleaseNumber: i + 1, Status: st, Trigger: "manual"})
	}
	if err := s.FinishSwitch(ctx, "app_1", "dep_2", "dep_1", "rolled_back"); err != nil {
		t.Fatalf("FinishSwitch: %v", err)
	}
	app, _ := s.GetApp(ctx, "app_1")
	d1, _ := s.GetDeployment(ctx, "dep_1")
	d2, _ := s.GetDeployment(ctx, "dep_2")
	if *app.ActiveReleaseID != "dep_2" || d2.Status != "active" || d2.FinishedAt == nil || d1.Status != "rolled_back" {
		t.Fatalf("after switch: app=%v d1=%s d2=%s", *app.ActiveReleaseID, d1.Status, d2.Status)
	}
	if store.DeploymentStatusLabel("rolled_back") != "Rolled back" ||
		store.DeploymentStatusLabel("inactive") != "Previous release" ||
		store.DeploymentStatusLabel("switching") != "Switching traffic" {
		t.Fatalf("labels must match the owner's wording")
	}
	if found, err := s.FindDeploymentByRelease(ctx, "app_1", 2); err != nil || found.ID != "dep_2" {
		t.Fatalf("FindDeploymentByRelease: %+v %v", found, err)
	}
}
