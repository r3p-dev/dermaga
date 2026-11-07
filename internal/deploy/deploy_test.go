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

	"github.com/r3p-dev/dermaga/internal/apps"
	"github.com/r3p-dev/dermaga/internal/config"
	_ "github.com/r3p-dev/dermaga/internal/golden" // accepts go test ./... -update
	"github.com/r3p-dev/dermaga/internal/secrets"
	"github.com/r3p-dev/dermaga/internal/spec"
	"github.com/r3p-dev/dermaga/internal/store"
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
	return &Engine{Apps: st, Host: f, Paths: config.Paths{StateDir: dir, ConfigDir: "/home/server/.config/dermaga"}}, f
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
	e, f := setup(t, app())
	r1 := deploy(t, e)
	c1 := "dg-shop-" + r1.Rel8
	if r1.Status != "live" || live(t, e) != r1.Rel8 || r1.SHA == "" || !f.running[c1] {
		t.Fatalf("first deploy: %+v running %v", r1, f.running)
	}
	want := []string{"prepare", "env", "install " + c1 + ".container", "start " + c1 + ".service", "healthy " + c1, "route app-shop.yml"}
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
		"healthy " + c2, "route app-shop.yml", "remove " + c1}
	if r2.Status != "live" || live(t, e) != r2.Rel8 || !slices.Equal(f.calls, want) {
		t.Errorf("recreate: %s\n %q\nwant\n %q", r2.Status, f.calls, want)
	}
	a, _ := e.Apps.Get(context.Background(), "shop")
	if old, _ := e.Apps.Release(context.Background(), a.ID, r1.Rel8); old.Status != "superseded" {
		t.Errorf("previous release %s", old.Status)
	}
}

func TestFailuresKeepPreviousRelease(t *testing.T) {
	for _, step := range []string{"prepare", "install", "stop", "start dg-shop", "healthy", "route"} {
		t.Run(step, func(t *testing.T) {
			e, f := setup(t, app())
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
				t.Error("route changed although the deploy failed")
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
