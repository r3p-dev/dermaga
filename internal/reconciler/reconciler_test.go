package reconciler_test

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"

	"dermaga/internal/host"
	"dermaga/internal/reconciler"
	"dermaga/internal/store"
)

func setupTestEnvironment(t *testing.T) (*store.Store, *host.FakeHost, reconciler.Config) {
	t.Helper()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "dermaga.db")

	st, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("opening test store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	fh := host.NewFakeHost()
	cfg := reconciler.Config{
		QuadletDir:        "/home/server/.config/containers/systemd",
		TraefikDynamicDir: "/home/server/.local/share/dermaga/traefik/dynamic",
	}

	return st, fh, cfg
}

func seedActiveApp(t *testing.T, st *store.Store, appName string, relNum int, port int, domain string) {
	t.Helper()
	ctx := context.Background()

	appID := fmt.Sprintf("app_%s", appName)
	depID := fmt.Sprintf("dep_%s_r%d", appName, relNum)
	domID := fmt.Sprintf("dom_%s", appName)

	app := &store.App{
		ID:              appID,
		Name:            appName,
		RepoOwner:       "testorg",
		RepoName:        appName,
		Port:            port,
		HealthType:      "none",
		ActiveReleaseID: &depID,
	}
	if err := st.CreateApp(ctx, app); err != nil {
		t.Fatalf("seeding app: %v", err)
	}

	dep := &store.Deployment{
		ID:                   depID,
		AppID:                appID,
		ReleaseNumber:        relNum,
		Status:               "active",
		Trigger:              "manual",
		CommitHash:           "abcd123",
		CommitMessage:        "Active deployment",
		EnvSnapshotEncrypted: "",
	}
	if err := st.CreateDeployment(ctx, dep); err != nil {
		t.Fatalf("seeding deployment: %v", err)
	}

	dom := &store.Domain{
		ID:        domID,
		AppID:     appID,
		Domain:    domain,
		IsPrimary: true,
	}
	if err := st.CreateDomain(ctx, dom); err != nil {
		t.Fatalf("seeding domain: %v", err)
	}
}

// 1. Orphan removed
func TestReconcile_OrphanRemoved(t *testing.T) {
	st, fh, cfg := setupTestEnvironment(t)
	ctx := context.Background()

	// Seed active app 'active-app' at release 1
	seedActiveApp(t, st, "active-app", 1, 8080, "active.example.com")

	// Set up expected active files
	activeContainer := filepath.Join(cfg.QuadletDir, "app-active-app-r1.container")
	activeTraefik := filepath.Join(cfg.TraefikDynamicDir, "app-active-app.yml")
	netUnit := filepath.Join(cfg.QuadletDir, "web.network")
	globalTraefik := filepath.Join(cfg.TraefikDynamicDir, "00-global.yml")

	_ = fh.WriteFileAtomic(activeContainer, []byte("active-container"), 0644)
	_ = fh.WriteFileAtomic(activeTraefik, []byte("active-traefik"), 0644)
	_ = fh.WriteFileAtomic(netUnit, []byte("web-network"), 0644)
	_ = fh.WriteFileAtomic(globalTraefik, []byte("00-global"), 0644)

	// Stage orphan files (app-orphan-r1.container, app-orphan.yml, app-active-app-r0.container)
	orphanContainer := filepath.Join(cfg.QuadletDir, "app-orphan-r1.container")
	oldReleaseContainer := filepath.Join(cfg.QuadletDir, "app-active-app-r0.container")
	orphanTraefik := filepath.Join(cfg.TraefikDynamicDir, "app-orphan.yml")

	_ = fh.WriteFileAtomic(orphanContainer, []byte("orphan"), 0644)
	_ = fh.WriteFileAtomic(oldReleaseContainer, []byte("old-rel"), 0644)
	_ = fh.WriteFileAtomic(orphanTraefik, []byte("orphan-traefik"), 0644)

	// Mock systemctl is-active for active app
	fh.CommandMock = func(name string, args ...string) ([]byte, error) {
		if len(args) >= 3 && args[0] == "--user" && args[1] == "is-active" && args[2] == "app-active-app-r1.service" {
			return []byte("active\n"), nil
		}
		return []byte("ok"), nil
	}

	rec := reconciler.New(fh, st, cfg)
	report, err := rec.Reconcile(ctx)
	if err != nil {
		t.Fatalf("Reconcile failed: %v", err)
	}

	// Verify orphans removed
	if len(report.OrphansRemoved) != 3 {
		t.Fatalf("expected 3 orphans removed, got %d: %v", len(report.OrphansRemoved), report.OrphansRemoved)
	}

	// Verify files deleted from host
	if _, err := fh.ReadFile(orphanContainer); err == nil {
		t.Errorf("orphanContainer still exists on host")
	}
	if _, err := fh.ReadFile(oldReleaseContainer); err == nil {
		t.Errorf("oldReleaseContainer still exists on host")
	}
	if _, err := fh.ReadFile(orphanTraefik); err == nil {
		t.Errorf("orphanTraefik still exists on host")
	}

	// Verify active files remain
	if _, err := fh.ReadFile(activeContainer); err != nil {
		t.Errorf("activeContainer was improperly deleted: %v", err)
	}
	if _, err := fh.ReadFile(activeTraefik); err != nil {
		t.Errorf("activeTraefik was improperly deleted: %v", err)
	}
}

// 2. Missing file re-rendered
func TestReconcile_MissingFilesReRendered(t *testing.T) {
	st, fh, cfg := setupTestEnvironment(t)
	ctx := context.Background()

	// Seed active app 'blog' at release 2
	seedActiveApp(t, st, "blog", 2, 3000, "blog.example.com")

	// Host has NO files (all missing!)
	fh.CommandMock = func(name string, args ...string) ([]byte, error) {
		if len(args) >= 3 && args[0] == "--user" && args[1] == "is-active" {
			return []byte("active\n"), nil
		}
		return []byte("ok"), nil
	}

	rec := reconciler.New(fh, st, cfg)
	report, err := rec.Reconcile(ctx)
	if err != nil {
		t.Fatalf("Reconcile failed: %v", err)
	}

	// Should re-render: web.network, app-blog-r2.container, 00-global.yml, app-blog.yml
	if len(report.FilesReRendered) != 4 {
		t.Fatalf("expected 4 files re-rendered, got %d: %v", len(report.FilesReRendered), report.FilesReRendered)
	}

	// Verify contents written to host
	netData, err := fh.ReadFile(filepath.Join(cfg.QuadletDir, "web.network"))
	if err != nil || len(netData) == 0 {
		t.Fatalf("web.network missing or empty: %v", err)
	}
	cData, err := fh.ReadFile(filepath.Join(cfg.QuadletDir, "app-blog-r2.container"))
	if err != nil || len(cData) == 0 {
		t.Fatalf("app-blog-r2.container missing or empty: %v", err)
	}
	globData, err := fh.ReadFile(filepath.Join(cfg.TraefikDynamicDir, "00-global.yml"))
	if err != nil || len(globData) == 0 {
		t.Fatalf("00-global.yml missing or empty: %v", err)
	}
	appData, err := fh.ReadFile(filepath.Join(cfg.TraefikDynamicDir, "app-blog.yml"))
	if err != nil || len(appData) == 0 {
		t.Fatalf("app-blog.yml missing or empty: %v", err)
	}

	// Verify daemon-reload was called
	foundReload := false
	for _, cmd := range fh.Commands {
		if cmd.Name == "systemctl" && len(cmd.Args) >= 2 && cmd.Args[0] == "--user" && cmd.Args[1] == "daemon-reload" {
			foundReload = true
			break
		}
	}
	if !foundReload {
		t.Errorf("systemctl --user daemon-reload was not called")
	}
}

// 3. Stopped live unit started
func TestReconcile_StoppedLiveUnitStarted(t *testing.T) {
	st, fh, cfg := setupTestEnvironment(t)
	ctx := context.Background()

	// Seed active app 'api' at release 1
	seedActiveApp(t, st, "api", 1, 8080, "api.example.com")

	// Pre-create all files
	_ = fh.WriteFileAtomic(filepath.Join(cfg.QuadletDir, "web.network"), []byte("web"), 0644)
	_ = fh.WriteFileAtomic(filepath.Join(cfg.QuadletDir, "app-api-r1.container"), []byte("container"), 0644)
	_ = fh.WriteFileAtomic(filepath.Join(cfg.TraefikDynamicDir, "00-global.yml"), []byte("global"), 0644)
	_ = fh.WriteFileAtomic(filepath.Join(cfg.TraefikDynamicDir, "app-api.yml"), []byte("traefik"), 0644)

	// Mock systemctl is-active -> returns "inactive\n"
	startedUnits := []string{}
	fh.CommandMock = func(name string, args ...string) ([]byte, error) {
		if len(args) >= 3 && args[0] == "--user" && args[1] == "is-active" {
			return []byte("inactive\n"), fmt.Errorf("exit 3")
		}
		if len(args) >= 3 && args[0] == "--user" && args[1] == "start" {
			startedUnits = append(startedUnits, args[2])
			return []byte("ok"), nil
		}
		return []byte("ok"), nil
	}

	rec := reconciler.New(fh, st, cfg)
	report, err := rec.Reconcile(ctx)
	if err != nil {
		t.Fatalf("Reconcile failed: %v", err)
	}

	if len(report.UnitsStarted) != 1 || report.UnitsStarted[0] != "app-api-r1.service" {
		t.Fatalf("expected app-api-r1.service in UnitsStarted, got: %v", report.UnitsStarted)
	}
	if len(startedUnits) != 1 || startedUnits[0] != "app-api-r1.service" {
		t.Fatalf("systemctl --user start was not called for app-api-r1.service: %v", startedUnits)
	}
}

// 4. Leftover verify router removed
func TestReconcile_LeftoverVerifyRouterRemoved(t *testing.T) {
	st, fh, cfg := setupTestEnvironment(t)
	ctx := context.Background()

	seedActiveApp(t, st, "webapp", 1, 8080, "webapp.example.com")

	_ = fh.WriteFileAtomic(filepath.Join(cfg.QuadletDir, "web.network"), []byte("web"), 0644)
	_ = fh.WriteFileAtomic(filepath.Join(cfg.QuadletDir, "app-webapp-r1.container"), []byte("container"), 0644)
	_ = fh.WriteFileAtomic(filepath.Join(cfg.TraefikDynamicDir, "00-global.yml"), []byte("global"), 0644)
	_ = fh.WriteFileAtomic(filepath.Join(cfg.TraefikDynamicDir, "app-webapp.yml"), []byte("traefik"), 0644)

	// Stage leftover verify router from crashed deployment
	leftoverVerify := filepath.Join(cfg.TraefikDynamicDir, "app-webapp-verify.yml")
	_ = fh.WriteFileAtomic(leftoverVerify, []byte("leftover-verify-router"), 0644)

	fh.CommandMock = func(name string, args ...string) ([]byte, error) {
		return []byte("active\n"), nil
	}

	rec := reconciler.New(fh, st, cfg)
	report, err := rec.Reconcile(ctx)
	if err != nil {
		t.Fatalf("Reconcile failed: %v", err)
	}

	if len(report.VerifyRoutersCleaned) != 1 || report.VerifyRoutersCleaned[0] != "app-webapp-verify.yml" {
		t.Fatalf("expected app-webapp-verify.yml in VerifyRoutersCleaned, got: %v", report.VerifyRoutersCleaned)
	}

	if _, err := fh.ReadFile(leftoverVerify); err == nil {
		t.Errorf("leftover verify router file was not deleted from host")
	}
}

// 5. Foreign files untouched
func TestReconcile_ForeignFilesUntouched(t *testing.T) {
	st, fh, cfg := setupTestEnvironment(t)
	ctx := context.Background()

	seedActiveApp(t, st, "portal", 1, 8080, "portal.example.com")

	_ = fh.WriteFileAtomic(filepath.Join(cfg.QuadletDir, "web.network"), []byte("web"), 0644)
	_ = fh.WriteFileAtomic(filepath.Join(cfg.QuadletDir, "app-portal-r1.container"), []byte("container"), 0644)
	_ = fh.WriteFileAtomic(filepath.Join(cfg.TraefikDynamicDir, "00-global.yml"), []byte("global"), 0644)
	_ = fh.WriteFileAtomic(filepath.Join(cfg.TraefikDynamicDir, "app-portal.yml"), []byte("traefik"), 0644)

	// Stage foreign files
	foreignContainer := filepath.Join(cfg.QuadletDir, "custom-vpn.container")
	foreignNetwork := filepath.Join(cfg.QuadletDir, "db.network")
	foreignTraefik := filepath.Join(cfg.TraefikDynamicDir, "custom-traefik-router.yml")
	foreignToml := filepath.Join(cfg.TraefikDynamicDir, "metrics.toml")

	_ = fh.WriteFileAtomic(foreignContainer, []byte("foreign-vpn"), 0644)
	_ = fh.WriteFileAtomic(foreignNetwork, []byte("foreign-db-net"), 0644)
	_ = fh.WriteFileAtomic(foreignTraefik, []byte("foreign-router"), 0644)
	_ = fh.WriteFileAtomic(foreignToml, []byte("foreign-toml"), 0644)

	fh.CommandMock = func(name string, args ...string) ([]byte, error) {
		return []byte("active\n"), nil
	}

	rec := reconciler.New(fh, st, cfg)
	report, err := rec.Reconcile(ctx)
	if err != nil {
		t.Fatalf("Reconcile failed: %v", err)
	}

	// Verify foreign files reported
	if len(report.ForeignFilesReported) != 4 {
		t.Fatalf("expected 4 foreign files reported, got %d: %v", len(report.ForeignFilesReported), report.ForeignFilesReported)
	}

	// Verify all foreign files are UNTOUCHED byte-for-byte
	for path, expectedContent := range map[string]string{
		foreignContainer: "foreign-vpn",
		foreignNetwork:   "foreign-db-net",
		foreignTraefik:   "foreign-router",
		foreignToml:      "foreign-toml",
	} {
		content, err := fh.ReadFile(path)
		if err != nil {
			t.Fatalf("foreign file %s was deleted or missing: %v", path, err)
		}
		if string(content) != expectedContent {
			t.Fatalf("foreign file %s content modified! got %q, want %q", path, string(content), expectedContent)
		}
	}
}

// 6. Combined scenario covering all five cases simultaneously
func TestReconcile_CombinedScenario(t *testing.T) {
	st, fh, cfg := setupTestEnvironment(t)
	ctx := context.Background()

	// Seed active app
	seedActiveApp(t, st, "demo", 2, 4000, "demo.example.com")

	// 1. Orphan file
	_ = fh.WriteFileAtomic(filepath.Join(cfg.QuadletDir, "app-oldapp-r1.container"), []byte("orphan"), 0644)
	_ = fh.WriteFileAtomic(filepath.Join(cfg.TraefikDynamicDir, "app-oldapp.yml"), []byte("orphan"), 0644)

	// 2. Missing file: app-demo-r2.container and app-demo.yml NOT staged

	// 3. Stopped live unit: systemctl is-active returns inactive
	fh.CommandMock = func(name string, args ...string) ([]byte, error) {
		if len(args) >= 3 && args[0] == "--user" && args[1] == "is-active" {
			return []byte("inactive\n"), fmt.Errorf("exit 3")
		}
		return []byte("ok"), nil
	}

	// 4. Leftover verify router
	_ = fh.WriteFileAtomic(filepath.Join(cfg.TraefikDynamicDir, "app-demo-verify.yml"), []byte("verify"), 0644)

	// 5. Foreign files
	_ = fh.WriteFileAtomic(filepath.Join(cfg.QuadletDir, "unmanaged-app.container"), []byte("vpn"), 0644)
	_ = fh.WriteFileAtomic(filepath.Join(cfg.TraefikDynamicDir, "traefik-custom.yml"), []byte("ext"), 0644)

	rec := reconciler.New(fh, st, cfg)
	report, err := rec.Reconcile(ctx)
	if err != nil {
		t.Fatalf("Reconcile failed: %v", err)
	}

	if len(report.OrphansRemoved) != 2 {
		t.Errorf("OrphansRemoved mismatch: %v", report.OrphansRemoved)
	}
	if len(report.FilesReRendered) != 4 { // web.network, app-demo-r2.container, 00-global.yml, app-demo.yml
		t.Errorf("FilesReRendered mismatch: %v", report.FilesReRendered)
	}
	if len(report.UnitsStarted) != 1 || report.UnitsStarted[0] != "app-demo-r2.service" {
		t.Errorf("UnitsStarted mismatch: %v", report.UnitsStarted)
	}
	if len(report.VerifyRoutersCleaned) != 1 || report.VerifyRoutersCleaned[0] != "app-demo-verify.yml" {
		t.Errorf("VerifyRoutersCleaned mismatch: %v", report.VerifyRoutersCleaned)
	}
	if len(report.ForeignFilesReported) != 2 {
		t.Errorf("ForeignFilesReported mismatch: %v", report.ForeignFilesReported)
	}
}
