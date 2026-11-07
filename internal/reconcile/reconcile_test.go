package reconcile

import (
	"context"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	_ "github.com/r3p-dev/dermaga/internal/golden" // accepts go test ./... -update
	"github.com/r3p-dev/dermaga/internal/spec"
	"github.com/r3p-dev/dermaga/internal/store"
	"github.com/r3p-dev/dermaga/internal/traefik"
)

// fakeHost is a machine with unit files, their systemd states and Traefik files.
type fakeHost struct {
	units  map[string]string // dg-*.container → ActiveState of its service
	files  map[string][]byte // app-*.yml (and foreign files, which must stay)
	calls  []string
	failOn string // a call prefix that fails
}

func (f *fakeHost) call(s string) error {
	f.calls = append(f.calls, s)
	if f.failOn != "" && strings.HasPrefix(s, f.failOn) {
		return context.DeadlineExceeded
	}
	return nil
}

func (f *fakeHost) Units() ([]string, error) {
	var out []string
	for u := range f.units {
		if strings.HasPrefix(u, "dg-") && strings.HasSuffix(u, ".container") {
			out = append(out, u)
		}
	}
	slices.Sort(out)
	return out, nil
}

func (f *fakeHost) AppFiles() ([]string, error) {
	var out []string
	for n := range f.files {
		if m, _ := filepath.Match("app-*.yml", n); m {
			out = append(out, n)
		}
	}
	slices.Sort(out)
	return out, nil
}

func (f *fakeHost) ActiveState(_ context.Context, unit string) (string, error) {
	return f.units[strings.TrimSuffix(unit, ".service")+".container"], f.call("state " + unit)
}

func (f *fakeHost) Start(_ context.Context, unit string) error {
	if err := f.call("start " + unit); err != nil {
		return err
	}
	f.units[strings.TrimSuffix(unit, ".service")+".container"] = "activating"
	return nil
}

func (f *fakeHost) DaemonReload(context.Context) error { return f.call("daemon-reload") }

func (f *fakeHost) RemoveContainer(_ context.Context, c string, _ int) error {
	if err := f.call("remove " + c); err != nil {
		return err
	}
	delete(f.units, c+".container")
	return nil
}

func (f *fakeHost) ReadAppFile(n string) ([]byte, error) { return f.files[n], nil }

func (f *fakeHost) WriteAppFile(n string, b []byte) error {
	if err := f.call("write " + n); err != nil {
		return err
	}
	f.files[n] = b
	return nil
}

func (f *fakeHost) RemoveAppFile(n string) error {
	if err := f.call("rm " + n); err != nil {
		return err
	}
	delete(f.files, n)
	return nil
}

func shop(live string) App {
	return App{Live: live, Spec: spec.App{Name: "shop", Port: 8080,
		Domains:    []spec.Domain{{Host: "shop.example.com", Primary: true}},
		Middleware: spec.Middleware{BasicAuth: []spec.BasicAuthUser{{User: "a", Hash: "$2a$10$XHrAnsxjPHLuFmCNWUIDdeat./VrrO97Hsg2DjDInBZAYmb2bWA5y"}}},
	}}
}

func render(t *testing.T, a App, rel8, token string) []byte {
	t.Helper()
	b, err := traefik.App(a.Spec, "dg-"+a.Spec.Name+"-"+rel8, token, traefik.Site{})
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestRepairs(t *testing.T) {
	a := shop("aaaaaaaa")
	steady := render(t, a, "aaaaaaaa", "")
	h := &fakeHost{
		units: map[string]string{
			"dg-shop-aaaaaaaa.container": "failed",   // live release, stopped
			"dg-shop-bbbbbbbb.container": "inactive", // orphaned candidate (crash mid-deploy)
			"dg-blog-cccccccc.container": "active",   // unknown app: report only
			"dg-hooks.container":         "active",   // owned elsewhere: silent
			"dg-db-shopdb.container":     "active",
			"dg-shop_data.volume":        "",       // volumes are never touched
			"traefik.container":          "active", // no prefix
		},
		files: map[string][]byte{
			"app-shop.yml":  render(t, a, "bbbbbbbb", "0123456789abcdef0123456789abcdef"), // verify router left over
			"app-blog.yml":  []byte("{}"),
			"99-custom.yml": []byte("custom"),
			"00-global.yml": []byte("global"),
		},
	}
	r := Run(context.Background(), h, []App{a}, traefik.Site{})

	if len(r.Errors) > 0 {
		t.Fatal(r.Errors)
	}
	want := []string{
		"remove dg-shop-bbbbbbbb", "daemon-reload",
		"state dg-shop-aaaaaaaa.service", "start dg-shop-aaaaaaaa.service",
		"write app-shop.yml",
	}
	if !slices.Equal(h.calls, want) {
		t.Errorf("calls\n %q\nwant\n %q", h.calls, want)
	}
	if string(h.files["app-shop.yml"]) != string(steady) {
		t.Error("app-shop.yml is not the steady-state file")
	}
	if strings.Contains(string(h.files["app-shop.yml"]), "app-shop-verify") {
		t.Error("verify router still there")
	}
	for _, f := range []string{"app-blog.yml", "99-custom.yml", "00-global.yml"} {
		if h.files[f] == nil {
			t.Errorf("%s was touched", f)
		}
	}
	for _, u := range []string{"dg-blog-cccccccc.container", "dg-hooks.container", "dg-shop_data.volume", "traefik.container"} {
		if _, ok := h.units[u]; !ok {
			t.Errorf("%s was removed", u)
		}
	}
	if len(r.Repaired) != 3 || len(r.Drift) != 2 {
		t.Errorf("repaired %q\ndrift %q", r.Repaired, r.Drift)
	}
	if !strings.Contains(strings.Join(r.Repaired, "\n"), "still had the deploy-time verify router") {
		t.Errorf("reason missing: %q", r.Repaired)
	}

	// second run: nothing left to do, nothing touched (idempotent)
	h.calls = nil
	r = Run(context.Background(), h, []App{a}, traefik.Site{})
	if len(r.Repaired) != 0 || !slices.Equal(h.calls, []string{"state dg-shop-aaaaaaaa.service"}) {
		t.Errorf("second run: repaired %q calls %q", r.Repaired, h.calls)
	}
}

func TestMissingFileAndNoLiveRelease(t *testing.T) {
	a, b := shop("aaaaaaaa"), shop("")
	b.Spec.Name = "blog"
	b.Spec.Domains = []spec.Domain{{Host: "blog.example.com", Primary: true}}
	h := &fakeHost{
		units: map[string]string{
			"dg-shop-aaaaaaaa.container": "active",
			"dg-blog-dddddddd.container": "activating", // failed first deploy left a candidate
		},
		files: map[string][]byte{"app-blog.yml": []byte("{}")},
	}
	r := Run(context.Background(), h, []App{a, b}, traefik.Site{})
	if len(r.Errors) > 0 {
		t.Fatal(r.Errors)
	}
	if string(h.files["app-shop.yml"]) != string(render(t, a, "aaaaaaaa", "")) {
		t.Error("missing app-shop.yml not re-rendered")
	}
	if _, ok := h.files["app-blog.yml"]; ok {
		t.Error("app-blog.yml kept although blog has no live release")
	}
	if _, ok := h.units["dg-blog-dddddddd.container"]; ok {
		t.Error("candidate of an app without a live release kept")
	}
	if !strings.Contains(strings.Join(r.Repaired, "\n"), "app-shop.yml: was missing") {
		t.Errorf("%q", r.Repaired)
	}
}

func TestLiveUnitMissingIsDriftAndErrorsContinue(t *testing.T) {
	a := shop("aaaaaaaa")
	h := &fakeHost{
		units:  map[string]string{"dg-shop-bbbbbbbb.container": "active"},
		files:  map[string][]byte{},
		failOn: "remove",
	}
	r := Run(context.Background(), h, []App{a}, traefik.Site{})
	if len(r.Errors) != 1 || len(r.Drift) != 1 || !strings.Contains(r.Drift[0], "has no unit file") {
		t.Errorf("errors %q drift %q", r.Errors, r.Drift)
	}
	if h.files["app-shop.yml"] == nil {
		t.Error("a failed removal stopped the run")
	}
}

func TestLoadApps(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	now := time.Now().Unix()
	for _, q := range []string{
		`INSERT INTO apps (id, name, spec, created_at) VALUES ('id1', 'shop', '{"port":8080,"readOnly":true}', ?)`,
		`INSERT INTO apps (id, name, spec, created_at) VALUES ('id2', 'blog', '{"port":3000}', ?)`,
		`INSERT INTO domains (host, app_id, is_primary, redirect) VALUES ('www.shop.example.com', 'id1', 0, 1), ('shop.example.com', 'id1', 1, 0), ('blog.example.com', 'id2', 1, 0)`,
		`INSERT INTO releases (app_id, rel8, image, status, created_at) VALUES ('id1', 'aaaaaaaa', 'localhost/dermaga/shop:1', 'superseded', ?), ('id1', 'bbbbbbbb', 'localhost/dermaga/shop:2', 'live', ?)`,
	} {
		if _, err := db.Exec(q, now, now); err != nil {
			t.Fatal(q, err)
		}
	}
	apps, err := LoadApps(context.Background(), db)
	if err != nil {
		t.Fatal(err)
	}
	if len(apps) != 2 || apps[0].Spec.Name != "blog" || apps[0].Live != "" || apps[1].Live != "bbbbbbbb" ||
		!apps[1].Spec.ReadOnly || apps[1].Spec.Primary() != "shop.example.com" || len(apps[1].Spec.Domains) != 2 {
		t.Fatalf("%+v", apps)
	}
	if err := apps[1].Spec.Validate(); err != nil {
		t.Error(err)
	}
	// the schema keeps one live release and one primary domain per app
	if _, err := db.Exec(`UPDATE releases SET status = 'live' WHERE rel8 = 'aaaaaaaa'`); err == nil {
		t.Error("second live release accepted")
	}
	if _, err := db.Exec(`UPDATE domains SET is_primary = 1, redirect = 0 WHERE host = 'www.shop.example.com'`); err == nil {
		t.Error("second primary domain accepted")
	}
}
