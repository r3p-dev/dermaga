package deploy

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/r3p-dev/dermaga/internal/apps"
	"github.com/r3p-dev/dermaga/internal/config"
	_ "github.com/r3p-dev/dermaga/internal/golden" // accepts go test ./... -update
	"github.com/r3p-dev/dermaga/internal/reconcile"
	"github.com/r3p-dev/dermaga/internal/secrets"
	"github.com/r3p-dev/dermaga/internal/spec"
	"github.com/r3p-dev/dermaga/internal/store"
	"github.com/r3p-dev/dermaga/internal/traefik"
)

// fake records what a deploy does and can fail one step.
type fake struct {
	calls   []string
	failAt  string // call prefix that fails
	routes  map[string][]byte
	env     []byte
	running map[string]bool
}

func (f *fake) do(c string) error {
	f.calls = append(f.calls, c)
	if f.failAt != "" && strings.HasPrefix(c, f.failAt) {
		return errors.New("injected failure: " + c)
	}
	return nil
}

func (f *fake) Prepare(_ context.Context, a apps.App, ref, rel8 string, log io.Writer) (Prepared, error) {
	fmt.Fprintf(log, "building with token s3cr3t-value\n")
	return Prepared{Image: "localhost/dermaga/" + a.Spec.Name + ":0123456789ab", SHA: strings.Repeat("a", 40), Subject: "msg"}, f.do("prepare")
}
func (f *fake) InstallUnits(_ context.Context, files map[string][]byte) error {
	var n []string
	for k := range files {
		n = append(n, k)
	}
	slices.Sort(n)
	return f.do("install " + strings.Join(n, ","))
}
func (f *fake) WriteEnv(_ string, data []byte) ([]byte, error) {
	prev := f.env
	f.env = data
	return prev, f.do("env")
}
func (f *fake) Stop(_ context.Context, u string) error {
	delete(f.running, strings.TrimSuffix(u, ".service"))
	return f.do("stop " + u)
}
func (f *fake) Start(_ context.Context, u string) error {
	f.running[strings.TrimSuffix(u, ".service")] = true
	return f.do("start " + u)
}
func (f *fake) RemoveContainer(_ context.Context, c string, _ int) error {
	delete(f.running, c)
	return f.do("remove " + c)
}
func (f *fake) DaemonReload(context.Context) error { return nil }
func (f *fake) Healthy(_ context.Context, c Check, _ io.Writer) error {
	return f.do("healthy " + c.Container)
}
func (f *fake) RemoveAppFile(n string) error {
	delete(f.routes, n)
	return f.do("rm route " + n)
}
func (f *fake) ReadAppFile(n string) ([]byte, error) {
	return f.routes[n], nil
}
func (f *fake) ImageHealthcheck(context.Context, string) (bool, error) {
	return false, nil
}
func (f *fake) PruneImages(_ context.Context, app string, keep []string) error {
	return f.do("prune " + app + " keep=" + strings.Join(keep, ","))
}
func (f *fake) Verify(_ context.Context, host, path, token string, any bool, _ io.Writer) error {
	// like Traefik: only a loaded file with this token's router answers
	if !strings.Contains(string(f.routes["app-shop.yml"]), token) {
		return f.do("verify-missing-token")
	}
	return f.do(fmt.Sprintf("verify %s %s any=%v", host, path, any))
}
func (f *fake) WriteAppFile(n string, b []byte) error {
	if err := f.do("route " + n); err != nil {
		return err
	}
	f.routes[n] = b
	return nil
}

func setup(t *testing.T, a spec.App) (*Engine, *fake) {
	t.Helper()
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	box, _ := secrets.New(make([]byte, 32))
	st := apps.Store{DB: db, Box: box}
	id, _, err := st.Create(context.Background(), a)
	if err != nil {
		t.Fatal(err)
	}
	st.SetEnv(context.Background(), id, map[string]string{"TOKEN": "s3cr3t-value"}, nil)
	f := &fake{routes: map[string][]byte{}, running: map[string]bool{}}
	return &Engine{
		Apps: st, Host: f, Paths: config.Paths{StateDir: dir, ConfigDir: "/home/server/.config/dermaga"},
		DrainGrace: 5 * time.Millisecond,
	}, f
}

func app() spec.App {
	return spec.App{Name: "shop", Port: 4321, Domains: []spec.Domain{{Host: "shop.example.com", Primary: true}},
		Source: spec.Source{Type: "git", Repo: "git@github.com:r3p-dev/shop.git", Branch: "main"}, Health: spec.Health{Path: "/robots.txt"}}
}

func deploy(t *testing.T, e *Engine) apps.Release {
	t.Helper()
	ctx := context.Background()
	r, err := e.Enqueue(ctx, "shop", "manual", "")
	if err != nil {
		t.Fatal(err)
	}
	e.RunOnce(ctx)
	a, _ := e.Apps.Get(ctx, "shop")
	got, _ := e.Apps.Release(ctx, a.ID, r.Rel8)
	return got
}

func live(t *testing.T, e *Engine) string {
	a, _ := e.Apps.Get(context.Background(), "shop")
	if a.Live == nil {
		return ""
	}
	return a.Live.Rel8
}

func TestFirstDeployAndRecreate(t *testing.T) {
	a := app()
	a.Strategy = "recreate"
	e, f := setup(t, a)
	r1 := deploy(t, e)
	c1 := "dg-shop-" + r1.Rel8
	if r1.Status != "live" || live(t, e) != r1.Rel8 || r1.SHA == "" || !f.running[c1] {
		t.Fatalf("first deploy: %+v running %v", r1, f.running)
	}
	want := []string{"prepare", "env", "install " + c1 + ".container", "start " + c1 + ".service", "healthy " + c1,
		"route app-shop.yml", "verify shop.example.com /robots.txt any=false", "route app-shop.yml", "prune shop keep=localhost/dermaga/shop:0123456789ab"}
	if !slices.Equal(f.calls, want) {
		t.Errorf("calls\n %q\nwant\n %q", f.calls, want)
	}
	if !strings.Contains(string(f.routes["app-shop.yml"]), c1+":4321") || strings.Contains(string(f.routes["app-shop.yml"]), "verify") {
		t.Error("route does not point at the release, or has a verify router")
	}
	if string(f.env) != "TOKEN=s3cr3t-value\n" {
		t.Errorf("env file %q", f.env)
	}

	f.calls = nil
	r2 := deploy(t, e)
	c2 := "dg-shop-" + r2.Rel8
	want = []string{"prepare", "env", "install " + c2 + ".container", "stop " + c1 + ".service", "start " + c2 + ".service",
		"healthy " + c2, "route app-shop.yml", "verify shop.example.com /robots.txt any=false", "route app-shop.yml", "remove " + c1, "prune shop keep=localhost/dermaga/shop:0123456789ab"}
	if r2.Status != "live" || live(t, e) != r2.Rel8 || !slices.Equal(f.calls, want) {
		t.Errorf("recreate: %s\n %q\nwant\n %q", r2.Status, f.calls, want)
	}
	appItem, _ := e.Apps.Get(context.Background(), "shop")
	if old, _ := e.Apps.Release(context.Background(), appItem.ID, r1.Rel8); old.Status != "superseded" {
		t.Errorf("previous release %s", old.Status)
	}
}

func TestFirstDeployAndBlueGreen(t *testing.T) {
	e, f := setup(t, app()) // default strategy is blue-green
	r1 := deploy(t, e)
	c1 := "dg-shop-" + r1.Rel8
	if r1.Status != "live" || live(t, e) != r1.Rel8 || r1.SHA == "" || !f.running[c1] {
		t.Fatalf("first deploy: %+v running %v", r1, f.running)
	}
	want := []string{"prepare", "env", "install " + c1 + ".container", "start " + c1 + ".service", "healthy " + c1,
		"route app-shop.yml", "verify shop.example.com /robots.txt any=false", "route app-shop.yml", "prune shop keep=localhost/dermaga/shop:0123456789ab"}
	if !slices.Equal(f.calls, want) {
		t.Errorf("calls\n %q\nwant\n %q", f.calls, want)
	}

	f.calls = nil
	r2 := deploy(t, e)
	c2 := "dg-shop-" + r2.Rel8
	// In blue-green: start c2 BEFORE stop c1!
	want = []string{"prepare", "env", "install " + c2 + ".container", "start " + c2 + ".service",
		"healthy " + c2, "route app-shop.yml", "verify shop.example.com /robots.txt any=false", "route app-shop.yml",
		"stop " + c1 + ".service", "remove " + c1, "prune shop keep=localhost/dermaga/shop:0123456789ab"}
	if r2.Status != "live" || live(t, e) != r2.Rel8 || !slices.Equal(f.calls, want) {
		t.Errorf("blue-green: %s\n %q\nwant\n %q", r2.Status, f.calls, want)
	}
	if !f.running[c2] || f.running[c1] {
		t.Errorf("running %v: want %s only", f.running, c2)
	}
	appItem, _ := e.Apps.Get(context.Background(), "shop")
	if old, _ := e.Apps.Release(context.Background(), appItem.ID, r1.Rel8); old.Status != "superseded" {
		t.Errorf("previous release %s", old.Status)
	}
}

func TestBlueGreenFailuresKeepPreviousRelease(t *testing.T) {
	for _, step := range []string{"prepare", "install", "start dg-shop", "healthy", "route", "verify"} {
		t.Run(step, func(t *testing.T) {
			e, f := setup(t, app())
			r1 := deploy(t, e)
			c1 := "dg-shop-" + r1.Rel8
			route := string(f.routes["app-shop.yml"])
			f.calls, f.failAt = nil, step
			f.env = []byte("OLD=1\n")
			r2 := deploy(t, e)
			c2 := "dg-shop-" + r2.Rel8
			if r2.Status != "failed" || r2.Error == "" || live(t, e) != r1.Rel8 {
				t.Fatalf("release %s (%s), live %s", r2.Status, r2.Error, live(t, e))
			}
			if string(f.routes["app-shop.yml"]) != route {
				t.Errorf("route not back to the previous release's steady file:\n%s", f.routes["app-shop.yml"])
			}
			// In blue-green, c1 was NEVER stopped!
			if !f.running[c1] || f.running[c2] {
				t.Errorf("running %v: want only %s", f.running, c1)
			}
			if slices.ContainsFunc(f.calls, func(c string) bool { return strings.HasPrefix(c, "stop") }) {
				t.Errorf("old release was stopped during blue-green failure: %q", f.calls)
			}
		})
	}
}

func TestFailuresKeepPreviousRelease(t *testing.T) {
	for _, step := range []string{"prepare", "install", "stop", "start dg-shop", "healthy", "route", "verify"} {
		t.Run(step, func(t *testing.T) {
			a := app()
			a.Strategy = "recreate"
			e, f := setup(t, a)
			r1 := deploy(t, e)
			c1 := "dg-shop-" + r1.Rel8
			route := string(f.routes["app-shop.yml"])
			f.calls, f.failAt = nil, step
			f.env = []byte("OLD=1\n")
			r2 := deploy(t, e)
			c2 := "dg-shop-" + r2.Rel8
			if step == "start dg-shop" && r2.Status == "live" {
				t.Skip() // "start dg-shop" also matches restarting the old release
			}
			if r2.Status != "failed" || r2.Error == "" || live(t, e) != r1.Rel8 {
				t.Fatalf("release %s (%s), live %s", r2.Status, r2.Error, live(t, e))
			}
			if string(f.routes["app-shop.yml"]) != route {
				t.Errorf("route not back to the previous release's steady file:\n%s", f.routes["app-shop.yml"])
			}
			if !f.running[c1] || f.running[c2] {
				t.Errorf("running %v: want only %s", f.running, c1)
			}
			if step != "prepare" && string(f.env) != "OLD=1\n" {
				t.Errorf("env file not restored: %q", f.env)
			}
			if step == "prepare" && slices.ContainsFunc(f.calls, func(c string) bool { return strings.HasPrefix(c, "stop") }) {
				t.Error("a failed build stopped the live release")
			}
			log, _ := os.ReadFile(r2.LogPath)
			if !strings.Contains(string(log), "keeps serving") {
				t.Errorf("log:\n%s", log)
			}
		})
	}
}

func TestFirstDeployFailureLeavesNothing(t *testing.T) {
	e, f := setup(t, app())
	f.failAt = "healthy"
	r := deploy(t, e)
	if r.Status != "failed" || live(t, e) != "" || len(f.routes) != 0 || len(f.running) != 0 {
		t.Fatalf("status %s routes %v running %v", r.Status, f.routes, f.running)
	}
	if !slices.Contains(f.calls, "remove dg-shop-"+r.Rel8) {
		t.Errorf("candidate not removed: %q", f.calls)
	}
}

func TestSupersedeQueued(t *testing.T) {
	e, f := setup(t, app())
	ctx := context.Background()
	r1, _ := e.Enqueue(ctx, "shop", "webhook", "")
	r2, _ := e.Enqueue(ctx, "shop", "webhook", "")
	e.RunOnce(ctx)
	a, _ := e.Apps.Get(ctx, "shop")
	g1, _ := e.Apps.Release(ctx, a.ID, r1.Rel8)
	g2, _ := e.Apps.Release(ctx, a.ID, r2.Rel8)
	if g1.Status != "superseded" || g2.Status != "live" || strings.Count(strings.Join(f.calls, " "), "prepare") != 1 {
		t.Fatalf("%s %s %q", g1.Status, g2.Status, f.calls)
	}
}

func TestLogMasksEnvValues(t *testing.T) {
	e, _ := setup(t, app())
	r := deploy(t, e)
	log, err := os.ReadFile(r.LogPath)
	if err != nil || strings.Contains(string(log), "s3cr3t-value") || !strings.Contains(string(log), "building with token ***") {
		t.Fatalf("log not masked:\n%s", log)
	}
	if fi, _ := os.Stat(r.LogPath); fi.Mode().Perm() != 0o600 {
		t.Errorf("log mode %v", fi.Mode().Perm())
	}
	var lines []string
	Follow(context.Background(), r.LogPath, func() bool { return true }, func(l string) error { lines = append(lines, l); return nil })
	if len(lines) < 5 || !strings.Contains(lines[len(lines)-1], "is live") {
		t.Errorf("follow: %q", lines)
	}
}

// D7/D8: a route Traefik doesn't serve is never reported live. On a first
// deploy the route is removed again; otherwise the previous route comes back.
func TestVerifyFailure(t *testing.T) {
	e, f := setup(t, app())
	f.failAt = "verify"
	r := deploy(t, e)
	if r.Status != "failed" || !strings.Contains(r.Error, "did not load the config") || len(f.routes) != 0 || len(f.running) != 0 {
		t.Fatalf("first deploy: %s %q routes %v running %v", r.Status, r.Error, f.routes, f.running)
	}
	f.failAt = ""
	r1 := deploy(t, e)
	f.failAt, f.calls = "verify", nil
	r2 := deploy(t, e)
	route := string(f.routes["app-shop.yml"])
	if r2.Status != "failed" || live(t, e) != r1.Rel8 || !strings.Contains(route, "dg-shop-"+r1.Rel8) || strings.Contains(route, "-verify") {
		t.Fatalf("redeploy: %s live %s route:\n%s", r2.Status, live(t, e), route)
	}
	log, _ := os.ReadFile(r2.LogPath)
	if !strings.Contains(string(log), "restoring the route to dg-shop-"+r1.Rel8) || !strings.Contains(string(log), "journalctl --user -u traefik") {
		t.Errorf("log:\n%s", log)
	}
}

func TestVerifyWithoutHealthPath(t *testing.T) {
	a := app()
	a.Health.Path = ""
	e, f := setup(t, a)
	if r := deploy(t, e); r.Status != "live" || !slices.Contains(f.calls, "verify shop.example.com / any=true") {
		t.Fatalf("%s %q", r.Status, f.calls)
	}
}

func TestRollback(t *testing.T) {
	e, f := setup(t, app())
	r1 := deploy(t, e)
	c1 := "dg-shop-" + r1.Rel8
	_ = c1
	r2 := deploy(t, e)
	c2 := "dg-shop-" + r2.Rel8
	if live(t, e) != r2.Rel8 {
		t.Fatalf("r2 not live: %s", live(t, e))
	}

	f.calls = nil
	// Default rollback to previous release (r1)
	r3, err := e.Rollback(context.Background(), "shop", "")
	if err != nil {
		t.Fatal(err)
	}
	e.RunOnce(context.Background())
	a, _ := e.Apps.Get(context.Background(), "shop")
	g3, _ := e.Apps.Release(context.Background(), a.ID, r3.Rel8)
	c3 := "dg-shop-" + r3.Rel8

	if g3.Status != "live" || live(t, e) != r3.Rel8 {
		t.Fatalf("rollback release status %s live %s", g3.Status, live(t, e))
	}
	if g3.Trigger != "rollback" || g3.Ref != r1.Rel8 || g3.Image != r1.Image {
		t.Fatalf("rollback release fields: %+v", g3)
	}
	// Verify previous live release (r2) is marked rolled_back
	oldR2, _ := e.Apps.Release(context.Background(), a.ID, r2.Rel8)
	if oldR2.Status != "rolled_back" {
		t.Errorf("r2 status = %s, want rolled_back", oldR2.Status)
	}
	// Verify rollback does NOT call prepare/build!
	if slices.Contains(f.calls, "prepare") {
		t.Errorf("rollback called prepare/rebuild: %q", f.calls)
	}
	want := []string{"env", "install " + c3 + ".container", "start " + c3 + ".service", "healthy " + c3,
		"route app-shop.yml", "verify shop.example.com /robots.txt any=false", "route app-shop.yml",
		"stop " + c2 + ".service", "remove " + c2, "prune shop keep=localhost/dermaga/shop:0123456789ab"}
	if !slices.Equal(f.calls, want) {
		t.Errorf("rollback calls\n %q\nwant\n %q", f.calls, want)
	}

	// Rollback explicitly targeting r2
	f.calls = nil
	r4, err := e.Rollback(context.Background(), "shop", r2.Rel8)
	if err != nil {
		t.Fatal(err)
	}
	e.RunOnce(context.Background())
	if live(t, e) != r4.Rel8 {
		t.Fatalf("r4 live: %s", live(t, e))
	}

	// Rolling back to already live release fails
	if _, err := e.Rollback(context.Background(), "shop", r4.Rel8); err == nil {
		t.Error("expected error rolling back to already-live release")
	}

	// Rolling back non-existent app fails
	if _, err := e.Rollback(context.Background(), "nonexistent", ""); err == nil {
		t.Error("expected error on nonexistent app")
	}
}

func TestDeployWithBasicAuthAndAllowlist(t *testing.T) {
	a := app()
	a.Middleware.BasicAuth = []spec.BasicAuthUser{{User: "admin", Hash: "$2a$10$XHrAnsxjPHLuFmCNWUIDdeat./VrrO97Hsg2DjDInBZAYmb2bWA5y"}}
	a.Middleware.IPAllowList = []string{"192.168.1.0/24"}
	e, f := setup(t, a)
	r := deploy(t, e)
	if r.Status != "live" {
		t.Fatalf("deploy failed: %s (%s)", r.Status, r.Error)
	}
	if !slices.Contains(f.calls, "verify shop.example.com /robots.txt any=false") {
		t.Errorf("verify not in calls: %q", f.calls)
	}
	// Steady route contains basicAuth and allowlist
	steady := string(f.routes["app-shop.yml"])
	if !strings.Contains(steady, "app-shop-auth") || !strings.Contains(steady, "app-shop-allowlist") {
		t.Errorf("steady route missing middlewares:\n%s", steady)
	}
	if strings.Contains(steady, "verify") {
		t.Errorf("steady route contains verify router:\n%s", steady)
	}
}

func TestReleaseRetentionAndImagePruning(t *testing.T) {
	e, f := setup(t, app())
	e.KeepImages = 3
	for i := 0; i < 5; i++ {
		deploy(t, e)
	}
	var lastPrune string
	for _, c := range f.calls {
		if strings.HasPrefix(c, "prune shop") {
			lastPrune = c
		}
	}
	if lastPrune == "" {
		t.Fatal("prune was never called")
	}
}

type recFake struct {
	units   []string
	files   []string
	routes  map[string][]byte
	running map[string]bool
}

func (r *recFake) Units() ([]string, error)    { return r.units, nil }
func (r *recFake) AppFiles() ([]string, error) { return r.files, nil }
func (r *recFake) ActiveState(_ context.Context, u string) (string, error) {
	c := strings.TrimSuffix(u, ".service")
	if r.running[c] {
		return "active", nil
	}
	return "inactive", nil
}
func (r *recFake) Start(_ context.Context, u string) error {
	r.running[strings.TrimSuffix(u, ".service")] = true
	return nil
}
func (r *recFake) DaemonReload(context.Context) error { return nil }
func (r *recFake) RemoveContainer(_ context.Context, c string, _ int) error {
	delete(r.running, c)
	var remaining []string
	for _, u := range r.units {
		if u != c+".container" {
			remaining = append(remaining, u)
		}
	}
	r.units = remaining
	return nil
}
func (r *recFake) ReadAppFile(n string) ([]byte, error) { return r.routes[n], nil }
func (r *recFake) WriteAppFile(n string, b []byte) error {
	r.routes[n] = b
	return nil
}
func (r *recFake) RemoveAppFile(n string) error {
	delete(r.routes, n)
	return nil
}

func TestMidDeployKillReconciliation(t *testing.T) {
	a := app()
	e, f := setup(t, a)
	r1 := deploy(t, e)
	c1 := "dg-shop-" + r1.Rel8

	c2 := "dg-shop-99999999"
	f.running[c2] = true
	token := "tokentokentoken"
	verifyRoute, _ := traefik.App(a, c2, token, e.Site)
	f.routes["app-shop.yml"] = verifyRoute

	ctx := context.Background()
	r2, _ := e.Apps.NewRelease(ctx, "shop", "manual", "")
	r2.Status = "starting"
	e.Apps.Update(ctx, r2)

	// Mark unfinished releases failed (like daemon startup)
	if rs, err := e.Apps.Unfinished(ctx); err == nil {
		for _, ur := range rs {
			ur.Status, ur.Error = "failed", "daemon stopped mid-deploy"
			e.Apps.Update(ctx, ur)
		}
	}

	fakeHost := &recFake{
		units:   []string{c1 + ".container", c2 + ".container"},
		files:   []string{"app-shop.yml"},
		routes:  f.routes,
		running: f.running,
	}
	appsList, _ := e.Apps.List(ctx)
	var desired []reconcile.App
	for _, appItem := range appsList {
		d := reconcile.App{Spec: appItem.Spec}
		if appItem.Live != nil {
			d.Live = appItem.Live.Rel8
		}
		desired = append(desired, d)
	}

	rep := reconcile.Run(ctx, fakeHost, desired, e.Site)
	if len(rep.Errors) > 0 {
		t.Fatalf("reconcile errors: %v", rep.Errors)
	}
	if fakeHost.running[c2] {
		t.Errorf("candidate container %s not removed", c2)
	}
	routeContent := string(fakeHost.routes["app-shop.yml"])
	if strings.Contains(routeContent, "verify") || !strings.Contains(routeContent, c1) {
		t.Errorf("route not properly reconciled:\n%s", routeContent)
	}
}
