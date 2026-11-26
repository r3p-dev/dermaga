package reconciler_test

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"dermaga/internal/deploy"
	"dermaga/internal/host"
	"dermaga/internal/quadlet"
	"dermaga/internal/reconciler"
	"dermaga/internal/store"
	"dermaga/internal/traefik"
)

// TestReconcile_ReRenderedUnitMatchesDeploy_DEP10: a missing live unit comes back with the
// release's image, limits, init and env file, so it can actually start.
func TestReconcile_ReRenderedUnitMatchesDeploy_DEP10(t *testing.T) {
	st, fh, cfg := setupTestEnvironment(t)
	cfg.EnvDir = "/home/server/.local/share/dermaga/env"
	ctx := context.Background()
	seedActiveApp(t, st, "blog", 2, 3000, "blog.example.com")
	dep, _ := st.GetDeployment(ctx, "dep_blog_r2")
	_ = st.SetDeploymentImageRef(ctx, dep.ID, "localhost/dermaga/blog:r2")
	envFile := deploy.EnvFilePath(cfg.EnvDir, "blog", 2)
	_ = fh.WriteFileAtomic(envFile, []byte("K=v\n"), 0600)

	if _, err := reconciler.New(fh, st, cfg).Reconcile(ctx); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	unit, err := fh.ReadFile(filepath.Join(cfg.QuadletDir, "app-blog-r2.container"))
	if err != nil {
		t.Fatalf("unit not re-rendered: %v", err)
	}
	for _, want := range []string{"Image=localhost/dermaga/blog:r2", "RunInit=true", "--memory=512m", "EnvironmentFile=" + envFile} {
		if !strings.Contains(string(unit), want) {
			t.Fatalf("re-rendered unit lacks %q:\n%s", want, unit)
		}
	}
}

// seedSwitch sets up app "shop" with r1 live and r2 in "switching" (trigger given), both units on
// disk, and the route file as it was when the daemon stopped.
func seedSwitch(t *testing.T, st *store.Store, fh *host.FakeHost, cfg reconciler.Config, trigger string, main int, token string) {
	t.Helper()
	ctx := context.Background()
	seedActiveApp(t, st, "shop", 1, 8080, "shop.example.com")
	if err := st.CreateDeployment(ctx, &store.Deployment{ID: "dep_shop_r2", AppID: "app_shop", ReleaseNumber: 2, Status: "switching", Trigger: trigger, CommitHash: "c2", CommitMessage: "two"}); err != nil {
		t.Fatalf("seed r2: %v", err)
	}
	app, _ := st.GetApp(ctx, "app_shop")
	for _, rel := range []int{1, 2} {
		unit, err := quadlet.RenderContainer(deploy.ContainerSpecFor("shop", rel, "localhost/dermaga/shop:r1", deploy.RuntimeConfigOf(app), ""))
		if err != nil {
			t.Fatalf("render unit: %v", err)
		}
		_ = fh.WriteFileAtomic(filepath.Join(cfg.QuadletDir, fmt.Sprintf("app-shop-r%d.container", rel)), unit, 0644)
	}
	route, err := traefik.RenderApp(traefik.AppSpec{
		AppName: "shop", ReleaseNumber: main, Port: 8080,
		Domains: []traefik.DomainSpec{{Domain: "shop.example.com", IsPrimary: true}},
		Switch:  &traefik.SwitchSpec{CandidateReleaseNumber: 2, CandidatePort: 8080, VerifyToken: token},
	})
	if err != nil {
		t.Fatalf("render route: %v", err)
	}
	_ = fh.WriteFileAtomic(filepath.Join(cfg.TraefikDynamicDir, "app-shop.yml"), route, 0600)
}

func liveRoute(t *testing.T, fh *host.FakeHost, cfg reconciler.Config) (int, bool) {
	t.Helper()
	data, _ := fh.ReadFile(filepath.Join(cfg.TraefikDynamicDir, "app-shop.yml"))
	rel, verify, ok := traefik.LiveRelease(data, "shop")
	if !ok {
		t.Fatalf("route file missing or unreadable")
	}
	return rel, verify
}

func stopped(fh *host.FakeHost, svc string) bool {
	for _, c := range fh.Commands {
		if c.Name == "systemctl" && strings.Join(c.Args, " ") == "--user stop "+svc {
			return true
		}
	}
	return false
}

// TestReconcile_CrashAfterMainSwitched_DEP10: the file already sends traffic to r2 (step 2 of the
// switch was written): the DB adopts r2, the old release becomes previous (rolled back after a
// rollback), the verify router goes, r1 is stopped. Exactly one live release, one route.
func TestReconcile_CrashAfterMainSwitched_DEP10(t *testing.T) {
	for trigger, oldStatus := range map[string]string{"push": "inactive", "rollback": "rolled_back"} {
		t.Run(trigger, func(t *testing.T) {
			st, fh, cfg := setupTestEnvironment(t)
			ctx := context.Background()
			seedSwitch(t, st, fh, cfg, trigger, 2, "tok-b-123")

			report, err := reconciler.New(fh, st, cfg).Reconcile(ctx)
			if err != nil {
				t.Fatalf("Reconcile: %v", err)
			}
			app, _ := st.GetApp(ctx, "app_shop")
			r1, _ := st.GetDeployment(ctx, "dep_shop_r1")
			r2, _ := st.GetDeployment(ctx, "dep_shop_r2")
			if *app.ActiveReleaseID != "dep_shop_r2" || r2.Status != "active" || r1.Status != oldStatus {
				t.Fatalf("after recovery: live=%s r1=%s r2=%s", *app.ActiveReleaseID, r1.Status, r2.Status)
			}
			if rel, verify := liveRoute(t, fh, cfg); rel != 2 || verify {
				t.Fatalf("route: r%d verify=%v", rel, verify)
			}
			if !stopped(fh, "app-shop-r1.service") || stopped(fh, "app-shop-r2.service") {
				t.Fatalf("r1 must be stopped, r2 kept")
			}
			if len(report.SwitchesRecovered) != 1 || !strings.Contains(report.SwitchesRecovered[0], "switch completed") {
				t.Fatalf("report: %v", report.SwitchesRecovered)
			}
		})
	}
}

// TestReconcile_CrashBeforeMainSwitched_DEP10: the file still sends traffic to r1 with a verify
// router to r2 (step 1): r1 stays live, the verify router goes, the r2 candidate is stopped.
func TestReconcile_CrashBeforeMainSwitched_DEP10(t *testing.T) {
	st, fh, cfg := setupTestEnvironment(t)
	ctx := context.Background()
	seedSwitch(t, st, fh, cfg, "push", 1, "tok-a-123")

	report, err := reconciler.New(fh, st, cfg).Reconcile(ctx)
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	app, _ := st.GetApp(ctx, "app_shop")
	if *app.ActiveReleaseID != "dep_shop_r1" {
		t.Fatalf("r1 must stay live, got %s", *app.ActiveReleaseID)
	}
	if rel, verify := liveRoute(t, fh, cfg); rel != 1 || verify {
		t.Fatalf("route: r%d verify=%v", rel, verify)
	}
	if !stopped(fh, "app-shop-r2.service") || stopped(fh, "app-shop-r1.service") {
		t.Fatalf("the r2 candidate must be stopped, r1 kept")
	}
	if len(report.VerifyRoutersCleaned) != 1 {
		t.Fatalf("verify router cleanup not reported: %+v", report)
	}
}
