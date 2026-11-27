package deploy

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"dermaga/internal/crypto"
	"dermaga/internal/host"
	"dermaga/internal/store"
	"dermaga/internal/traefik"
)

// newTestApp creates an app with one primary domain and the given runtime env.
func newTestApp(t *testing.T, s *store.Store, key []byte, appName string, env map[string]string) *store.App {
	t.Helper()
	ctx := context.Background()
	sealed, err := SealEnvMap(key, "env_encrypted", env)
	if err != nil {
		t.Fatalf("SealEnvMap: %v", err)
	}
	app := &store.App{
		ID: "app_" + appName, Name: appName, RepoOwner: "octo", RepoName: "web", Branch: "main",
		Port: 8080, HealthType: "none", MemoryLimit: "512m", EnvEncrypted: sealed,
	}
	if err := s.CreateApp(ctx, app); err != nil {
		t.Fatalf("CreateApp: %v", err)
	}
	if err := s.CreateDomain(ctx, &store.Domain{ID: "dom_" + appName, AppID: app.ID, Domain: appName + ".example.com", IsPrimary: true}); err != nil {
		t.Fatalf("CreateDomain: %v", err)
	}
	return app
}

// waitIdle waits until no build runs or waits and no release of appID starts or switches.
func waitIdle(t *testing.T, e *Engine, appID string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		e.mu.Lock()
		idle := e.activeDepID == "" && len(e.pending) == 0
		e.mu.Unlock()
		if idle && len(e.appLock(appID)) == 0 {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("engine did not become idle")
}

func deployAndWait(t *testing.T, s *store.Store, e *Engine, appID, commit, want string) *store.Deployment {
	t.Helper()
	dep, err := e.Enqueue(context.Background(), appID, "manual", commit, "msg "+commit)
	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	got := waitForStatus(t, s, dep.ID, want)
	waitIdle(t, e, appID)
	got, _ = s.GetDeployment(context.Background(), got.ID)
	return got
}

func routeOf(t *testing.T, fh *host.FakeHost, e *Engine, appName string) (live int, verify bool) {
	t.Helper()
	data, err := fh.ReadFile(filepath.Join(e.cfg.TraefikDynamicDir, "app-"+appName+".yml"))
	if err != nil {
		t.Fatalf("route file: %v", err)
	}
	live, verify, ok := traefik.LiveRelease(data, appName)
	if !ok {
		t.Fatalf("route file does not parse:\n%s", data)
	}
	return live, verify
}

func ran(fh *host.FakeHost, name string, args ...string) bool {
	want := strings.Join(append([]string{name}, args...), " ")
	for _, c := range fh.Commands {
		if strings.Join(append([]string{c.Name}, c.Args...), " ") == want {
			return true
		}
	}
	return false
}

// TestRedeploy_SwitchesWithProof_DEP1: r2 starts next to r1, both verify steps are proven through
// (fake) Traefik, the route moves to r2 without a leftover verify router, then r1 is stopped and
// its unit and env file removed while its image stays for rollback.
func TestRedeploy_SwitchesWithProof_DEP1(t *testing.T) {
	s, fh, eng, key := setupTestEngine(t)
	var verified atomic.Int32
	inner := eng.cfg.VerifyRoute
	eng.cfg.VerifyRoute = func(ctx context.Context, h, p, tok string) (bool, error) {
		ok, err := inner(ctx, h, p, tok)
		if ok {
			verified.Add(1)
		}
		return ok, err
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	_ = eng.Start(ctx)
	app := newTestApp(t, s, key, "shop", map[string]string{"K": "v1"})

	r1 := deployAndWait(t, s, eng, app.ID, "c1", "active")
	if verified.Load() != 0 {
		t.Fatalf("a first deploy has nothing to switch from and must not verify")
	}
	r2 := deployAndWait(t, s, eng, app.ID, "c2", "active")

	if verified.Load() != 2 {
		t.Fatalf("expected 2 proven verify steps, got %d", verified.Load())
	}
	if live, verify := routeOf(t, fh, eng, "shop"); live != 2 || verify {
		t.Fatalf("route: live=r%d verify=%v, want r2 without verify router", live, verify)
	}
	r1, _ = s.GetDeployment(ctx, r1.ID)
	a, _ := s.GetApp(ctx, app.ID)
	if r1.Status != "inactive" || r2.Status != "active" || *a.ActiveReleaseID != r2.ID {
		t.Fatalf("statuses: r1=%s r2=%s active=%s", r1.Status, r2.Status, *a.ActiveReleaseID)
	}
	if !ran(fh, "systemctl", "--user", "stop", "app-shop-r1.service") {
		t.Fatalf("r1 must be stopped after the drain")
	}
	if _, err := fh.Stat(filepath.Join(eng.cfg.QuadletDir, "app-shop-r1.container")); err == nil {
		t.Fatalf("r1 unit must be removed")
	}
	if _, err := fh.Stat(EnvFilePath(eng.cfg.EnvDir, "shop", 1)); err == nil {
		t.Fatalf("r1 env file must be removed")
	}
	if ran(fh, "podman", "rmi", "--ignore", r1.ImageRef) || ran(fh, "podman", "rmi", "-f", r1.ImageRef) {
		t.Fatalf("r1 image must stay for rollback")
	}
}

// TestRedeploy_HealthFails_OldStaysLive_DEP2: a candidate that never gets healthy is removed and
// the live release, its route and its unit are untouched.
func TestRedeploy_HealthFails_OldStaysLive_DEP2(t *testing.T) {
	s, fh, eng, key := setupTestEngine(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	_ = eng.Start(ctx)
	app := newTestApp(t, s, key, "shop", nil)
	r1 := deployAndWait(t, s, eng, app.ID, "c1", "active")

	fh.CommandMock = func(name string, args ...string) ([]byte, error) {
		if name == "systemctl" && len(args) == 3 && args[1] == "is-active" && args[2] == "app-shop-r2.service" {
			return []byte("failed"), errors.New("exit status 3")
		}
		return []byte("ok"), nil
	}
	r2 := deployAndWait(t, s, eng, app.ID, "c2", "failed")

	if !strings.Contains(r2.ErrorMessage, "failed to stay running") {
		t.Fatalf("failure reason: %q", r2.ErrorMessage)
	}
	if live, verify := routeOf(t, fh, eng, "shop"); live != 1 || verify {
		t.Fatalf("route must stay on r1: live=r%d verify=%v", live, verify)
	}
	a, _ := s.GetApp(ctx, app.ID)
	if *a.ActiveReleaseID != r1.ID || ran(fh, "systemctl", "--user", "stop", "app-shop-r1.service") {
		t.Fatalf("r1 must stay live and running")
	}
	if _, err := fh.Stat(filepath.Join(eng.cfg.QuadletDir, "app-shop-r2.container")); err == nil {
		t.Fatalf("the failed candidate's unit must be removed")
	}
	if !ran(fh, "podman", "rmi", "-f", "localhost/dermaga/shop:r2") {
		t.Fatalf("the failed candidate's image must be removed")
	}
}

// TestRedeploy_VerifyFails_RestoresRoute_DEP2 fails each verify step in turn: the route goes back
// to r1 only, r1 keeps running, the candidate is removed.
func TestRedeploy_VerifyFails_RestoresRoute_DEP2(t *testing.T) {
	for _, failStep := range []int32{1, 2} {
		t.Run(fmt.Sprintf("step%d", failStep), func(t *testing.T) {
			s, fh, eng, key := setupTestEngine(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			_ = eng.Start(ctx)
			app := newTestApp(t, s, key, "shop", nil)
			r1 := deployAndWait(t, s, eng, app.ID, "c1", "active")

			var step atomic.Int32
			var lastTok string
			inner := eng.cfg.VerifyRoute
			eng.cfg.VerifyRoute = func(ctx context.Context, h, p, tok string) (bool, error) {
				if tok != lastTok {
					lastTok = tok
					step.Add(1)
				}
				if step.Load() == failStep {
					return false, errors.New("verify router not loaded yet")
				}
				return inner(ctx, h, p, tok)
			}
			r2 := deployAndWait(t, s, eng, app.ID, "c2", "failed")
			if !strings.Contains(r2.ErrorMessage, "Traefik did not answer through the verify router") {
				t.Fatalf("failure reason: %q", r2.ErrorMessage)
			}
			if live, verify := routeOf(t, fh, eng, "shop"); live != 1 || verify {
				t.Fatalf("route must be restored to r1: live=r%d verify=%v", live, verify)
			}
			a, _ := s.GetApp(ctx, app.ID)
			if *a.ActiveReleaseID != r1.ID || ran(fh, "systemctl", "--user", "stop", "app-shop-r1.service") {
				t.Fatalf("r1 must stay live and running")
			}
			if !ran(fh, "systemctl", "--user", "stop", "app-shop-r2.service") {
				t.Fatalf("the candidate must be stopped")
			}
		})
	}
}

// TestMemoryCheck_RefusesDeploy: with less MemAvailable than the app's memory limit the deploy is
// refused before the build, with a reason, and the live release is untouched.
func TestMemoryCheck_RefusesDeploy(t *testing.T) {
	s, fh, eng, key := setupTestEngine(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	_ = eng.Start(ctx)
	app := newTestApp(t, s, key, "shop", nil)
	r1 := deployAndWait(t, s, eng, app.ID, "c1", "active")

	_ = fh.WriteFileAtomic("/proc/meminfo", []byte("MemTotal: 2000000 kB\nMemAvailable:  102400 kB\n"), 0444)
	builds := len(fh.Commands)
	r2 := deployAndWait(t, s, eng, app.ID, "c2", "failed")
	if !strings.Contains(r2.ErrorMessage, "not enough free memory") || !strings.Contains(r2.ErrorMessage, "100 MiB available") {
		t.Fatalf("failure reason: %q", r2.ErrorMessage)
	}
	for _, c := range fh.Commands[builds:] {
		if c.Name == "podman" && len(c.Args) > 0 && c.Args[0] == "build" {
			t.Fatalf("no build may start when memory is refused")
		}
	}
	a, _ := s.GetApp(ctx, app.ID)
	if *a.ActiveReleaseID != r1.ID {
		t.Fatalf("r1 must stay live")
	}
	mc, err := eng.CheckMemory("512m")
	if err != nil || mc.OK || mc.AvailableBytes != 100<<20 || mc.NeededBytes != 512<<20 {
		t.Fatalf("CheckMemory: %+v %v", mc, err)
	}
}

// TestRollback_RestoresReleaseEnv_DEP4: rolling back to r1 starts r1's image with r1's env version
// (not the env saved since), without a build; r2 becomes "rolled_back"; the app's env is unchanged.
func TestRollback_RestoresReleaseEnv_DEP4(t *testing.T) {
	s, fh, eng, key := setupTestEngine(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	_ = eng.Start(ctx)
	app := newTestApp(t, s, key, "shop", map[string]string{"API_KEY": "old-key"})
	r1 := deployAndWait(t, s, eng, app.ID, "c1", "active")

	cur, _ := s.GetApp(ctx, app.ID)
	cur.EnvEncrypted, _ = SealEnvMap(key, "env_encrypted", map[string]string{"API_KEY": "new-key"})
	if err := s.UpdateApp(ctx, cur); err != nil {
		t.Fatalf("UpdateApp: %v", err)
	}
	r2 := deployAndWait(t, s, eng, app.ID, "c2", "active")
	before, _ := s.GetApp(ctx, app.ID)

	builds := 0
	for _, c := range fh.Commands {
		if c.Name == "podman" && len(c.Args) > 0 && c.Args[0] == "build" {
			builds++
		}
	}
	rb, err := eng.Rollback(ctx, app.ID, r1.ID)
	if err != nil {
		t.Fatalf("Rollback: %v", err)
	}
	got := waitForStatus(t, s, rb.ID, "active")
	waitIdle(t, eng, app.ID)

	if got.Trigger != "rollback" || got.ImageRef != r1.ImageRef || got.SourceDeploymentID == nil || *got.SourceDeploymentID != r1.ID {
		t.Fatalf("rollback release: %+v", got)
	}
	env, err := fh.ReadFile(EnvFilePath(eng.cfg.EnvDir, "shop", got.ReleaseNumber))
	if err != nil || !strings.Contains(string(env), "API_KEY=old-key") {
		t.Fatalf("rollback must run with r1's env, got %q (%v)", env, err)
	}
	n := 0
	for _, c := range fh.Commands {
		if c.Name == "podman" && len(c.Args) > 0 && c.Args[0] == "build" {
			n++
		}
	}
	if n != builds {
		t.Fatalf("a rollback must not build")
	}
	r2, _ = s.GetDeployment(ctx, r2.ID)
	after, _ := s.GetApp(ctx, app.ID)
	if r2.Status != "rolled_back" || *after.EnvVersionID != *before.EnvVersionID {
		t.Fatalf("r2=%s; the panel env must stay the current version", r2.Status)
	}
	if live, verify := routeOf(t, fh, eng, "shop"); live != got.ReleaseNumber || verify {
		t.Fatalf("route: r%d verify=%v", live, verify)
	}
	// The image r1 and the rollback release share is kept: removing it would break both.
	if ran(fh, "podman", "rmi", "--ignore", r1.ImageRef) {
		t.Fatalf("the shared image must stay")
	}
}

// TestRollback_RefusedWhileBuilding: owner decision, "Cancel the running build first".
func TestRollback_RefusedWhileBuilding(t *testing.T) {
	s, fh, eng, key := setupTestEngine(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	_ = eng.Start(ctx)
	app := newTestApp(t, s, key, "shop", nil)
	r1 := deployAndWait(t, s, eng, app.ID, "c1", "active")
	_ = deployAndWait(t, s, eng, app.ID, "c2", "active")

	started := make(chan struct{})
	fh.StreamMock = func(ctx context.Context, env []string, onLine func(string), name string, args ...string) error {
		close(started)
		<-ctx.Done()
		return ctx.Err()
	}
	r3, err := eng.Enqueue(ctx, app.ID, "manual", "c3", "slow build")
	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	<-started
	if _, err := eng.Rollback(ctx, app.ID, r1.ID); !errors.Is(err, ErrBuildRunning) {
		t.Fatalf("expected ErrBuildRunning, got %v", err)
	}
	_ = eng.Cancel(ctx, r3.ID)
	waitForStatus(t, s, r3.ID, "cancelled")
}

// TestRetention_KeepsLiveAndLastFive_DEP4: after 8 releases the images of r1 and r2 are removed,
// r3–r7 stay as rollback targets; rolling back to r2 is refused, to r3 allowed.
func TestRetention_KeepsLiveAndLastFive_DEP4(t *testing.T) {
	s, fh, eng, key := setupTestEngine(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	_ = eng.Start(ctx)
	app := newTestApp(t, s, key, "shop", nil)
	var deps []*store.Deployment
	for i := 1; i <= 8; i++ {
		deps = append(deps, deployAndWait(t, s, eng, app.ID, fmt.Sprintf("c%d", i), "active"))
	}
	for i := 1; i <= 8; i++ {
		img := fmt.Sprintf("localhost/dermaga/shop:r%d", i)
		removed := ran(fh, "podman", "rmi", "--ignore", img)
		if removed != (i <= 2) {
			t.Fatalf("r%d image removed=%v", i, removed)
		}
	}
	if _, err := eng.Rollback(ctx, app.ID, deps[1].ID); err == nil || !strings.Contains(err.Error(), "only the last 5") {
		t.Fatalf("rollback to r2 must be refused, got %v", err)
	}
	rb, err := eng.Rollback(ctx, app.ID, deps[2].ID)
	if err != nil {
		t.Fatalf("rollback to r3: %v", err)
	}
	waitForStatus(t, s, rb.ID, "active")
	waitIdle(t, eng, app.ID)
}

// TestLegacySnapshotConversion: a release from before migration 0005 gets its environment as an
// env version and a plain runtime config when the engine starts, and can be rolled back to.
func TestLegacySnapshotConversion(t *testing.T) {
	s, _, eng, key := setupTestEngine(t)
	ctx := context.Background()
	app := newTestApp(t, s, key, "shop", map[string]string{"K": "current"})
	snap, err := sealJSONForTest(key, map[string]any{"env": map[string]string{"K": "legacy"}, "port": 3000, "health_type": "none"})
	if err != nil {
		t.Fatalf("seal: %v", err)
	}
	if _, err := s.DB().Exec(`INSERT INTO deployments (id, app_id, release_number, status, trigger, commit_hash, commit_message, env_snapshot_encrypted, image_ref)
		VALUES ('dep_legacy', ?, 1, 'inactive', 'manual', 'abc', 'old', ?, 'localhost/dermaga/shop:r1')`, app.ID, snap); err != nil {
		t.Fatalf("insert legacy: %v", err)
	}
	if err := eng.convertLegacySnapshots(ctx); err != nil {
		t.Fatalf("convert: %v", err)
	}
	dep, _ := s.GetDeployment(ctx, "dep_legacy")
	env, err := eng.releaseEnv(ctx, dep)
	if err != nil || env["K"] != "legacy" {
		t.Fatalf("legacy env: %v %v", env, err)
	}
	rc, err := ReleaseRuntimeConfig(app, dep)
	if err != nil || rc.Port != 3000 || rc.MemoryLimit != "512m" {
		t.Fatalf("legacy config: %+v %v", rc, err)
	}
}

func sealJSONForTest(key []byte, v any) (string, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	return crypto.Encrypt(key, "env_snapshot_encrypted", raw)
}

// The default VerifyRoute probes Traefik's internal plain-HTTP verify entrypoint through the
// rootless netns (not 443: sniStrict refuses the handshake while a certificate is pending), and
// reports the probe's own reason on failure (DEP-1, DEC-33).
func TestVerifyThroughTraefik_NetnsProbe_DEP1(t *testing.T) {
	h := host.NewFakeHost()
	e := NewEngine(nil, h, nil, nil, Config{BinaryPath: "/home/server/.local/bin/dermaga"})
	ok, err := e.cfg.VerifyRoute(context.Background(), "shop.example.com", "/api", "tok123")
	if !ok || err != nil {
		t.Fatalf("verify = %v, %v; want true, nil", ok, err)
	}
	want := "podman unshare --rootless-netns /home/server/.local/bin/dermaga probe -host shop.example.com -verify-token tok123 http://traefik:8099/api"
	if got := h.Commands[0].Name + " " + strings.Join(h.Commands[0].Args, " "); got != want {
		t.Fatalf("command:\n got %s\nwant %s", got, want)
	}

	h.CommandMock = func(string, ...string) ([]byte, error) {
		return []byte("HTTP 404 (404 Not Found) in 1ms\nBody: 404 page not found\nError: verify router not loaded yet\n"), errors.New("exit status 1")
	}
	ok, err = e.cfg.VerifyRoute(context.Background(), "shop.example.com", "/", "tok123")
	if ok || err == nil || err.Error() != "verify router not loaded yet" {
		t.Fatalf("failed probe = %v, %v; want false, \"verify router not loaded yet\"", ok, err)
	}
}

func TestCheckVerifyResponse_DEP1(t *testing.T) {
	resp := func(status int, echoed string) *http.Response {
		r := &http.Response{StatusCode: status, Header: http.Header{}}
		if echoed != "" {
			r.Header.Set(traefik.VerifyHeader, echoed)
		}
		return r
	}
	for _, tc := range []struct {
		name string
		resp *http.Response
		want string
	}{
		{"echoed, app answered", resp(200, "tok"), ""},
		{"echoed, app answered 401", resp(401, "tok"), ""},
		{"no echo: old file still loaded", resp(404, ""), "verify router not loaded yet"},
		{"other token: earlier file", resp(200, "old"), "verify router not loaded yet"},
		{"echoed, candidate unreachable", resp(502, "tok"), "Traefik could not reach the new release (HTTP 502)"},
		{"echoed, timeout", resp(504, "tok"), "Traefik could not reach the new release (HTTP 504)"},
	} {
		err := CheckVerifyResponse(tc.resp, "tok")
		if got := fmt.Sprint(err); (tc.want == "" && err != nil) || (tc.want != "" && got != tc.want) {
			t.Errorf("%s: got %v, want %q", tc.name, err, tc.want)
		}
	}
}

// TestReleaseLog_SavedUpToSwitch_DEP10: the log in the DB already holds every stage line when
// Traefik is asked to verify, so a release adopted after a crash at that point keeps its log.
func TestReleaseLog_SavedUpToSwitch_DEP10(t *testing.T) {
	s, _, eng, key := setupTestEngine(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	_ = eng.Start(ctx)
	app := newTestApp(t, s, key, "shop", map[string]string{"K": "v1"})
	deployAndWait(t, s, eng, app.ID, "c1", "active")

	var atVerify []string
	inner := eng.cfg.VerifyRoute
	eng.cfg.VerifyRoute = func(ctx context.Context, h, p, tok string) (bool, error) {
		deps, _ := s.ListDeploymentsByApp(ctx, app.ID)
		atVerify = append(atVerify, deps[0].BuildLogs) // newest first: the candidate
		return inner(ctx, h, p, tok)
	}
	deployAndWait(t, s, eng, app.ID, "c2", "active")
	if len(atVerify) < 2 {
		t.Fatalf("verify called %d times", len(atVerify))
	}
	if !strings.Contains(atVerify[0], "==> Starting release r2") {
		t.Fatalf("log in the DB at the first verify step lacks the start line:\n%q", atVerify[0])
	}
	if last := atVerify[len(atVerify)-1]; !strings.Contains(last, "==> Traefik reaches r2 through the verify router") {
		t.Fatalf("log in the DB at the last verify step lacks the verify line:\n%q", last)
	}
}

// TestStart_RemovesImagesOfInterruptedReleases: after a restart the image of a release that was
// building or starting is removed; a rollback's image belongs to the release it restores and stays.
func TestStart_RemovesImagesOfInterruptedReleases(t *testing.T) {
	s, fh, eng, key := setupTestEngine(t)
	app := newTestApp(t, s, key, "shop", map[string]string{"K": "v1"})
	ctx := context.Background()
	for _, d := range []store.Deployment{
		{ID: "dep_b", AppID: app.ID, ReleaseNumber: 2, Status: "starting", Trigger: "manual", CommitHash: "c2", CommitMessage: "m"},
		{ID: "dep_r", AppID: app.ID, ReleaseNumber: 3, Status: "switching", Trigger: "rollback", CommitHash: "c1", CommitMessage: "m", ImageRef: "localhost/dermaga/shop:r1"},
	} {
		if err := s.CreateDeployment(ctx, &d); err != nil {
			t.Fatal(err)
		}
	}
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	if err := eng.Start(runCtx); err != nil {
		t.Fatal(err)
	}
	if !ran(fh, "podman", "rmi", "--ignore", "localhost/dermaga/shop:r2") {
		t.Fatalf("image of the interrupted r2 must be removed")
	}
	for _, c := range fh.Commands {
		if c.Name == "podman" && strings.Contains(strings.Join(c.Args, " "), "shop:r1") {
			t.Fatalf("rollback image must stay: %v", c.Args)
		}
	}
}

// APP-9 / DEP-10: a crash skips the build's own cleanup; Start clears the scratch folder, and removing
// an app leaves nothing of it under BuildsDir or MirrorsDir.
func TestStartAndRemoveApp_LeaveNoBuildScratch_APP9(t *testing.T) {
	s, fh, eng, key := setupTestEngine(t)
	app := newTestApp(t, s, key, "shop", nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	other := filepath.Join(eng.cfg.MirrorsDir, "other.git", "HEAD")
	for _, p := range []string{
		filepath.Join(eng.cfg.BuildsDir, ".dryrun-dep_x", "app-shop-r7.container"),
		filepath.Join(eng.cfg.BuildsDir, "shop", "dep_y", "Containerfile"),
		other,
	} {
		fh.Files[p] = []byte("x")
	}
	if err := eng.Start(ctx); err != nil {
		t.Fatal(err)
	}
	for p := range fh.Files {
		if strings.HasPrefix(p, eng.cfg.BuildsDir+"/") {
			t.Fatalf("scratch left after Start: %s", p)
		}
	}
	if _, ok := fh.Files[other]; !ok {
		t.Fatal("Start must not touch the mirrors") // negative control
	}

	_ = deployAndWait(t, s, eng, app.ID, "c1", "active")
	fh.Files[filepath.Join(eng.cfg.BuildsDir, "shop", ".keep")] = []byte("x") // the folder the builder leaves
	fh.Files[filepath.Join(eng.cfg.MirrorsDir, "shop.git", "HEAD")] = []byte("x")
	if err := eng.RemoveApp(ctx, app.ID); err != nil {
		t.Fatal(err)
	}
	for p := range fh.Files {
		if strings.Contains(p, "shop") {
			t.Fatalf("left after RemoveApp: %s", p)
		}
	}
	if _, ok := fh.Files[other]; !ok {
		t.Fatal("RemoveApp must not touch another app's mirror") // negative control
	}
}
