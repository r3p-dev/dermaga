package reconciler_test

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
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

	_ = fh.WriteFileAtomic(activeContainer, []byte("# managed by dermaga; app=active-app\n[Container]\n"), 0644)
	_ = fh.WriteFileAtomic(activeTraefik, []byte("# managed by dermaga; app=active-app\n{}\n"), 0644)
	_ = fh.WriteFileAtomic(netUnit, []byte("# managed by dermaga; network=web\n"), 0644)
	_ = fh.WriteFileAtomic(globalTraefik, []byte("# managed by dermaga; global\n"), 0644)

	// Stage orphan files (app-orphan-r1.container, app-orphan.yml, app-active-app-r2.container)
	orphanContainer := filepath.Join(cfg.QuadletDir, "app-orphan-r1.container")
	oldReleaseContainer := filepath.Join(cfg.QuadletDir, "app-active-app-r2.container")
	orphanTraefik := filepath.Join(cfg.TraefikDynamicDir, "app-orphan.yml")

	_ = fh.WriteFileAtomic(orphanContainer, []byte("# managed by dermaga; app=orphan\n[Container]\n"), 0644)
	_ = fh.WriteFileAtomic(oldReleaseContainer, []byte("# managed by dermaga; app=active-app\n[Container]\n"), 0644)
	_ = fh.WriteFileAtomic(orphanTraefik, []byte("# managed by dermaga; app=orphan\n{}\n"), 0644)

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
	_ = fh.WriteFileAtomic(filepath.Join(cfg.QuadletDir, "web.network"), []byte("# managed by dermaga; network=web\n"), 0644)
	_ = fh.WriteFileAtomic(filepath.Join(cfg.QuadletDir, "app-api-r1.container"), []byte("# managed by dermaga; app=api\n[Container]\n"), 0644)
	_ = fh.WriteFileAtomic(filepath.Join(cfg.TraefikDynamicDir, "00-global.yml"), []byte("# managed by dermaga; global\n"), 0644)
	_ = fh.WriteFileAtomic(filepath.Join(cfg.TraefikDynamicDir, "app-api.yml"), []byte("# managed by dermaga; app=api\n{}\n"), 0644)

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

	_ = fh.WriteFileAtomic(filepath.Join(cfg.QuadletDir, "web.network"), []byte("# managed by dermaga; network=web\n"), 0644)
	_ = fh.WriteFileAtomic(filepath.Join(cfg.QuadletDir, "app-webapp-r1.container"), []byte("# managed by dermaga; app=webapp\n[Container]\n"), 0644)
	_ = fh.WriteFileAtomic(filepath.Join(cfg.TraefikDynamicDir, "00-global.yml"), []byte("# managed by dermaga; global\n"), 0644)
	_ = fh.WriteFileAtomic(filepath.Join(cfg.TraefikDynamicDir, "app-webapp.yml"), []byte("# managed by dermaga; app=webapp\n{}\n"), 0644)

	// Stage leftover verify router from crashed deployment
	leftoverVerify := filepath.Join(cfg.TraefikDynamicDir, "app-webapp-verify.yml")
	_ = fh.WriteFileAtomic(leftoverVerify, []byte("# managed by dermaga; app=webapp\nleftover-verify-router"), 0644)

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

	_ = fh.WriteFileAtomic(filepath.Join(cfg.QuadletDir, "web.network"), []byte("# managed by dermaga; network=web\n"), 0644)
	_ = fh.WriteFileAtomic(filepath.Join(cfg.QuadletDir, "app-portal-r1.container"), []byte("# managed by dermaga; app=portal\n[Container]\n"), 0644)
	_ = fh.WriteFileAtomic(filepath.Join(cfg.TraefikDynamicDir, "00-global.yml"), []byte("# managed by dermaga; global\n"), 0644)
	_ = fh.WriteFileAtomic(filepath.Join(cfg.TraefikDynamicDir, "app-portal.yml"), []byte("# managed by dermaga; app=portal\n{}\n"), 0644)

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
	_ = fh.WriteFileAtomic(filepath.Join(cfg.QuadletDir, "app-oldapp-r1.container"), []byte("# managed by dermaga; app=oldapp\n[Container]\n"), 0644)
	_ = fh.WriteFileAtomic(filepath.Join(cfg.TraefikDynamicDir, "app-oldapp.yml"), []byte("# managed by dermaga; app=oldapp\n{}\n"), 0644)

	// 2. Missing file: app-demo-r2.container and app-demo.yml NOT staged

	// 3. Stopped live unit: systemctl is-active returns inactive
	fh.CommandMock = func(name string, args ...string) ([]byte, error) {
		if len(args) >= 3 && args[0] == "--user" && args[1] == "is-active" {
			return []byte("inactive\n"), fmt.Errorf("exit 3")
		}
		return []byte("ok"), nil
	}

	// 4. Leftover verify router
	_ = fh.WriteFileAtomic(filepath.Join(cfg.TraefikDynamicDir, "app-demo-verify.yml"), []byte("# managed by dermaga; app=demo\nverify"), 0644)

	// 5. Foreign files (one matches naming pattern but lacks marker; other is non-pattern)
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

// 7. Foreign file named like managed one (e.g. app-custom-r1.container without marker) is untouched
func TestReconcile_ForeignFileNamedLikeManagedUntouched(t *testing.T) {
	st, fh, cfg := setupTestEnvironment(t)
	ctx := context.Background()

	// Seed active app
	seedActiveApp(t, st, "active-app", 1, 8080, "active.example.com")

	// Pre-create active files with proper ownership markers
	_ = fh.WriteFileAtomic(filepath.Join(cfg.QuadletDir, "web.network"), []byte("# managed by dermaga; network=web\n"), 0644)
	_ = fh.WriteFileAtomic(filepath.Join(cfg.QuadletDir, "app-active-app-r1.container"), []byte("# managed by dermaga; app=active-app\n"), 0644)
	_ = fh.WriteFileAtomic(filepath.Join(cfg.TraefikDynamicDir, "00-global.yml"), []byte("# managed by dermaga; global\n"), 0644)
	_ = fh.WriteFileAtomic(filepath.Join(cfg.TraefikDynamicDir, "app-active-app.yml"), []byte("# managed by dermaga; app=active-app\n"), 0644)

	// Stage files that match Dermaga naming patterns (app-*.container, app-*.yml) but DO NOT have Dermaga marker
	foreignNamedContainer := filepath.Join(cfg.QuadletDir, "app-custom-r1.container")
	foreignNamedTraefik := filepath.Join(cfg.TraefikDynamicDir, "app-custom.yml")
	foreignNamedVerify := filepath.Join(cfg.TraefikDynamicDir, "app-custom-verify.yml")

	unmarkedContainerContent := "[Unit]\nDescription=User unmanaged container unit\n[Container]\nImage=user/custom:latest\n"
	unmarkedTraefikContent := "{\"http\":{\"routers\":{\"user-custom\":{}}}}\n"
	unmarkedVerifyContent := "{\"http\":{\"routers\":{\"user-verify\":{}}}}\n"

	_ = fh.WriteFileAtomic(foreignNamedContainer, []byte(unmarkedContainerContent), 0644)
	_ = fh.WriteFileAtomic(foreignNamedTraefik, []byte(unmarkedTraefikContent), 0644)
	_ = fh.WriteFileAtomic(foreignNamedVerify, []byte(unmarkedVerifyContent), 0644)

	fh.CommandMock = func(name string, args ...string) ([]byte, error) {
		return []byte("active\n"), nil
	}

	rec := reconciler.New(fh, st, cfg)
	report, err := rec.Reconcile(ctx)
	if err != nil {
		t.Fatalf("Reconcile failed: %v", err)
	}

	// OrphansRemoved must be EMPTY!
	if len(report.OrphansRemoved) != 0 {
		t.Fatalf("expected 0 orphans removed, got %d: %v", len(report.OrphansRemoved), report.OrphansRemoved)
	}
	if len(report.VerifyRoutersCleaned) != 0 {
		t.Fatalf("expected 0 verify routers cleaned, got %d: %v", len(report.VerifyRoutersCleaned), report.VerifyRoutersCleaned)
	}

	// All 3 foreign-named files must be reported in ForeignFilesReported
	for _, expectedFile := range []string{"app-custom-r1.container", "app-custom.yml", "app-custom-verify.yml"} {
		found := false
		for _, f := range report.ForeignFilesReported {
			if f == expectedFile {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected %s in ForeignFilesReported, got: %v", expectedFile, report.ForeignFilesReported)
		}
	}

	// Verify byte-for-byte untouched on disk
	cContent, err := fh.ReadFile(foreignNamedContainer)
	if err != nil || string(cContent) != unmarkedContainerContent {
		t.Errorf("foreignNamedContainer was modified or deleted: %v", err)
	}
	tContent, err := fh.ReadFile(foreignNamedTraefik)
	if err != nil || string(tContent) != unmarkedTraefikContent {
		t.Errorf("foreignNamedTraefik was modified or deleted: %v", err)
	}
	vContent, err := fh.ReadFile(foreignNamedVerify)
	if err != nil || string(vContent) != unmarkedVerifyContent {
		t.Errorf("foreignNamedVerify was modified or deleted: %v", err)
	}
}

// 8. Empty DB safe default: existing managed units are preserved when DB has 0 active apps
func TestReconcile_EmptyDBSafeDefault(t *testing.T) {
	st, fh, cfg := setupTestEnvironment(t)
	ctx := context.Background()

	// Notice: 0 apps seeded in store (empty database!)

	// Pre-create managed files with valid Dermaga ownership markers
	managedContainer := filepath.Join(cfg.QuadletDir, "app-legacy-r1.container")
	managedTraefik := filepath.Join(cfg.TraefikDynamicDir, "app-legacy.yml")

	_ = fh.WriteFileAtomic(managedContainer, []byte("# managed by dermaga; app=legacy\n[Container]\n"), 0644)
	_ = fh.WriteFileAtomic(managedTraefik, []byte("# managed by dermaga; app=legacy\n{}\n"), 0644)

	rec := reconciler.New(fh, st, cfg) // AllowEmptyDBPrune is false by default
	report, err := rec.Reconcile(ctx)
	if err != nil {
		t.Fatalf("Reconcile failed: %v", err)
	}

	// Safe default must be triggered!
	if !report.SafeDefaultTriggered {
		t.Errorf("expected SafeDefaultTriggered to be true, got false")
	}

	// Zero orphans removed!
	if len(report.OrphansRemoved) != 0 {
		t.Fatalf("expected 0 orphans removed under safe default, got %d: %v", len(report.OrphansRemoved), report.OrphansRemoved)
	}

	// Both files must remain on disk untouched
	if _, err := fh.ReadFile(managedContainer); err != nil {
		t.Errorf("managedContainer was deleted under safe default: %v", err)
	}
	if _, err := fh.ReadFile(managedTraefik); err != nil {
		t.Errorf("managedTraefik was deleted under safe default: %v", err)
	}
}

// 9. Empty DB with explicit AllowEmptyDBPrune flag deletes managed units
func TestReconcile_EmptyDBWithPruneFlag(t *testing.T) {
	st, fh, cfg := setupTestEnvironment(t)
	ctx := context.Background()

	cfg.AllowEmptyDBPrune = true // explicit override

	managedContainer := filepath.Join(cfg.QuadletDir, "app-legacy-r1.container")
	managedTraefik := filepath.Join(cfg.TraefikDynamicDir, "app-legacy.yml")

	_ = fh.WriteFileAtomic(managedContainer, []byte("# managed by dermaga; app=legacy\n[Container]\n"), 0644)
	_ = fh.WriteFileAtomic(managedTraefik, []byte("# managed by dermaga; app=legacy\n{}\n"), 0644)

	rec := reconciler.New(fh, st, cfg)
	report, err := rec.Reconcile(ctx)
	if err != nil {
		t.Fatalf("Reconcile failed: %v", err)
	}

	if len(report.OrphansRemoved) != 2 {
		t.Fatalf("expected 2 orphans removed with prune flag, got %d: %v", len(report.OrphansRemoved), report.OrphansRemoved)
	}

	if _, err := fh.ReadFile(managedContainer); err == nil {
		t.Errorf("managedContainer still exists on disk")
	}
	if _, err := fh.ReadFile(managedTraefik); err == nil {
		t.Errorf("managedTraefik still exists on disk")
	}
}

func traefikTestConfig(t *testing.T) (*store.Store, *host.FakeHost, reconciler.Config) {
	st, fh, cfg := setupTestEnvironment(t)
	cfg.UserUnitDir = "/home/server/.config/systemd/user"
	cfg.TraefikDir = "/home/server/.local/share/dermaga/traefik"
	return st, fh, cfg
}

func systemctlCalls(fh *host.FakeHost) []string {
	var calls []string
	for _, c := range fh.Commands {
		if c.Name == "systemctl" {
			calls = append(calls, strings.Join(c.Args, " "))
		}
	}
	return calls
}

// HOST-6 / NFR-3: a fresh host gets Traefik's units and static config, and Traefik is started
// without blocking the daemon.
func TestReconcile_TraefikInstalledAndStarted(t *testing.T) {
	st, fh, cfg := traefikTestConfig(t)
	report, err := reconciler.New(fh, st, cfg).Reconcile(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{
		filepath.Join(cfg.QuadletDir, "traefik.container"),
		filepath.Join(cfg.UserUnitDir, "traefik-http.socket"),
		filepath.Join(cfg.UserUnitDir, "traefik-https.socket"),
		filepath.Join(cfg.TraefikDir, "traefik.yml"),
	} {
		if _, err := fh.ReadFile(p); err != nil {
			t.Errorf("%s not written: %v", p, err)
		}
	}
	static, _ := fh.ReadFile(filepath.Join(cfg.TraefikDir, "traefik.yml"))
	if !strings.Contains(string(static), `"storage": "/etc/traefik/acme.json"`) || strings.Contains(string(static), `"email"`) {
		t.Errorf("unexpected static config:\n%s", static)
	}
	want := []string{
		"--user daemon-reload",
		"--user enable --now traefik-http.socket traefik-https.socket",
		"--user restart --no-block traefik.service",
	}
	if got := systemctlCalls(fh); strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("systemctl calls = %q, want %q", got, want)
	}
	if len(report.ForeignFilesReported) != 0 {
		t.Errorf("unexpected foreign files: %v", report.ForeignFilesReported)
	}

	// A second pass changes nothing and only makes sure Traefik is started.
	fh.Commands = nil
	report, err = reconciler.New(fh, st, cfg).Reconcile(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(report.FilesReRendered) != 0 {
		t.Errorf("second pass re-rendered %v", report.FilesReRendered)
	}
	want = []string{
		"--user enable --now traefik-http.socket traefik-https.socket",
		"--user start --no-block traefik.service",
	}
	if got := systemctlCalls(fh); strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("second pass systemctl calls = %q, want %q", got, want)
	}
}

// An owned but outdated unit (e.g. an older pinned image) is replaced and Traefik restarted.
func TestReconcile_TraefikOutdatedUnitReplaced(t *testing.T) {
	st, fh, cfg := traefikTestConfig(t)
	if _, err := reconciler.New(fh, st, cfg).Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	unit := filepath.Join(cfg.QuadletDir, "traefik.container")
	old, _ := fh.ReadFile(unit)
	_ = fh.WriteFileAtomic(unit, []byte(strings.Replace(string(old), "v3.7.19", "v3.7.18", 1)), 0644)
	fh.Commands = nil

	report, err := reconciler.New(fh, st, cfg).Reconcile(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(report.FilesReRendered, ",") != "traefik.container" {
		t.Errorf("FilesReRendered = %v", report.FilesReRendered)
	}
	if got := systemctlCalls(fh); len(got) != 3 || got[2] != "--user restart --no-block traefik.service" {
		t.Errorf("systemctl calls = %q", got)
	}
}

// DEP-10: vps-setup.sh's own Traefik unit has no marker; dermaga reports it, never touches it,
// and does not start its own Traefik next to it.
func TestReconcile_ForeignTraefikUntouched(t *testing.T) {
	st, fh, cfg := traefikTestConfig(t)
	unit := filepath.Join(cfg.QuadletDir, "traefik.container")
	foreign := []byte("# Managed by vps-setup.sh\n[Container]\nImage=docker.io/library/traefik:v3.7\n")
	_ = fh.WriteFileAtomic(unit, foreign, 0644)

	report, err := reconciler.New(fh, st, cfg).Reconcile(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := fh.ReadFile(unit); string(got) != string(foreign) {
		t.Error("foreign traefik.container was modified")
	}
	if !strings.Contains(strings.Join(report.ForeignFilesReported, ","), "traefik.container") {
		t.Errorf("foreign unit not reported: %v", report.ForeignFilesReported)
	}
	for _, c := range systemctlCalls(fh) {
		if strings.Contains(c, "traefik") {
			t.Errorf("traefik must not be started next to a foreign one: %q", c)
		}
	}
}

func TestReconcile_HookRelayLifecycle_DEC30_GHA6(t *testing.T) {
	st, fh, cfg := traefikTestConfig(t)
	cfg.HookRelayRootfsDir = "/home/server/.local/share/dermaga/hook-relay-rootfs"
	cfg.BinaryPath = "/home/server/.local/bin/dermaga"
	cfg.SocketDir = "/run/user/1001/dermaga"

	ctx := context.Background()
	_ = st.SetSetting(ctx, "hook_domain", "hooks.example.com")
	_ = st.SetSetting(ctx, "github_app_id", "12345")

	rec := reconciler.New(fh, st, cfg)
	if err := rec.EnsureGitHubRelay(ctx, "hooks.example.com"); err != nil {
		t.Fatalf("EnsureGitHubRelay failed: %v", err)
	}

	relayUnit := filepath.Join(cfg.QuadletDir, "hook-relay.container")
	unitData, err := fh.ReadFile(relayUnit)
	if err != nil {
		t.Fatalf("expected hook-relay.container written: %v", err)
	}
	if !strings.Contains(string(unitData), "Rootfs="+cfg.HookRelayRootfsDir) {
		t.Errorf("expected Rootfs in hook-relay.container, got:\n%s", unitData)
	}

	hookDyn := filepath.Join(cfg.TraefikDynamicDir, "01-hook-domain.yml")
	dynData, err := fh.ReadFile(hookDyn)
	if err != nil {
		t.Fatalf("expected 01-hook-domain.yml written: %v", err)
	}
	if !strings.Contains(string(dynData), "http://hook-relay:8080") {
		t.Errorf("expected http://hook-relay:8080 backend in 01-hook-domain.yml, got:\n%s", dynData)
	}

	// Disconnect removes both relay unit and hook router (GHA-6)
	if err := rec.RemoveGitHubRelay(ctx); err != nil {
		t.Fatalf("RemoveGitHubRelay failed: %v", err)
	}
	if _, err := fh.ReadFile(relayUnit); err == nil {
		t.Error("expected hook-relay.container removed on disconnect")
	}
	if _, err := fh.ReadFile(hookDyn); err == nil {
		t.Error("expected 01-hook-domain.yml removed on disconnect")
	}
}
