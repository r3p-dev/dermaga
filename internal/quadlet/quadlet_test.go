package quadlet

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/r3p-dev/dermaga/internal/golden"
	"github.com/r3p-dev/dermaga/internal/spec"
)

func release(a spec.App) Release {
	a.Name, a.Port = "shop", 8080
	a.Source = spec.Source{Type: "image", Image: "docker.io/library/nginx:1"}
	a.Domains = []spec.Domain{{Host: "shop.example.com", Primary: true}}
	return Release{App: a, Rel8: "1a2b3c4d", Image: "localhost/dermaga/shop:0123456789ab", EnvFile: "/home/server/.config/dermaga/env/shop.env"}
}

// containerCases are the PLAN M2 unit cases. Their goldens are also the input
// of TestDryRun.
func containerCases() map[string]Release {
	db := release(spec.App{})
	db.Databases = []string{"shopdb", "cache"}
	img := release(spec.App{})
	img.Image = "docker.io/louislam/uptime-kuma:2.0.2"
	img.ImageHealthcheck = true
	return map[string]Release{
		"minimal":   release(spec.App{}),
		"db-linked": db,
		// recreate is the default for apps with named volumes; the unit is the same shape
		"recreate-volume": release(spec.App{Volumes: []spec.Volume{{Name: "data", Path: "/app/data"}, {Name: "uploads", Path: "/srv/uploads"}}}),
		"hardened": release(spec.App{
			ReadOnly: true, AddCaps: []string{"CHOWN", "SETUID", "SETGID"},
			Limits: spec.Limits{Memory: "1g", CPUs: "0.5", Pids: 128},
			Health: spec.Health{Cmd: "wget -q -O /dev/null http://127.0.0.1:8080/healthz", TimeoutSec: 60},
		}),
		// every character that systemd or Quadlet could interpret
		"health-escaping":   release(spec.App{Health: spec.Health{Cmd: `sh -c 'test "$(cat /tmp/x)" = ok' && echo 100% ` + "`date`" + ` \n done`}}),
		"image-healthcheck": img,
	}
}

func TestContainerGolden(t *testing.T) {
	for n, r := range containerCases() {
		fn, b, err := Container(r)
		if err != nil {
			t.Fatalf("%s: %v", n, err)
		}
		if fn != "dg-shop-1a2b3c4d.container" {
			t.Errorf("%s: file name %s", n, fn)
		}
		golden.Check(t, filepath.Join("units", n, fn), b)
	}
}

func TestOtherUnitsGolden(t *testing.T) {
	check := func(dir, fn string, b []byte, err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
		golden.Check(t, filepath.Join("units", dir, fn), b)
	}
	for _, v := range []spec.Volume{{Name: "data", Path: "/app/data"}, {Name: "uploads", Path: "/srv/uploads"}} {
		fn, b, err := Volume("shop", v)
		check("recreate-volume", fn, b, err)
	}
	for _, db := range []string{"shopdb", "cache"} {
		fn, b, err := Network(db)
		check("db-linked", fn, b, err)
	}
	fn, b, err := Hooks("/home/server/.local/share/dermaga/relay", "/run/user/1001/dermaga/hooks")
	check("hooks", fn, b, err)

	tm := Timer{Name: "backup-db-shopdb", Description: "backup db:shopdb", Binary: "/usr/local/bin/dermaga",
		Args: []string{"backup", "run", "--scheduled", "db:shopdb"}, OnCalendar: "*-*-* 03:00:00 Asia/Jakarta", RandomDelay: 1800}
	svc, tmr, err := tm.Units()
	if err != nil {
		t.Fatal(err)
	}
	golden.Check(t, "systemd/"+tm.UnitName()+".service", svc)
	golden.Check(t, "systemd/"+tm.UnitName()+".timer", tmr)
}

// TestSpecifierEscapingGolden pins the one place a systemd specifier passes
// through (OnFailure=…@%n.service) next to literal % and $ in Exec and
// Environment values, which must stay doubled.
func TestSpecifierEscapingGolden(t *testing.T) {
	tm := Timer{Name: "backup-system", Description: "backup system", Binary: "/usr/local/bin/dermaga",
		Args: []string{"backup", "run", "--scheduled", "system"}, OnCalendar: "*-*-* 02:00:00 Asia/Jakarta", RandomDelay: 1800,
		OnFailure: "dg-backup-failed@%n.service"}
	svc, _, err := tm.Units()
	if err != nil {
		t.Fatal(err)
	}
	golden.Check(t, "systemd/"+tm.UnitName()+".service", svc)
	if !bytes.Contains(svc, []byte("\nOnFailure=dg-backup-failed@%n.service\n")) {
		t.Errorf("OnFailure lost its specifier:\n%s", svc)
	}

	b, err := unit{
		{"Unit", [][2]string{{"OnFailure", "dg-backup-failed@%n.service"}}},
		{"Container", [][2]string{{"Exec", "sh -c 'date +%s; echo 100% $HOME'"}}},
		{"Service", [][2]string{{"Environment", "DISCOUNT=50% PRICE=$5"}, {"ExecStartPre", "/bin/echo %n 100%"}}},
	}.render()
	if err != nil {
		t.Fatal(err)
	}
	golden.Check(t, "systemd/specifier-escaping.service", b)
	for _, want := range []string{
		"OnFailure=dg-backup-failed@%n.service\n",
		"Exec=sh -c 'date +%%s; echo 100%% $$HOME'\n",
		"Environment=DISCOUNT=50%% PRICE=$$5\n",
		"ExecStartPre=/bin/echo %%n 100%%\n",
	} {
		if !bytes.Contains(b, []byte(want)) {
			t.Errorf("missing %q in:\n%s", want, b)
		}
	}
	for _, bad := range []string{"other.service", "dg-backup-failed@%i.service", "dg-x@%n.service extra", "%n", "dg-x@%%n.service"} {
		if _, err := (unit{{"Unit", [][2]string{{"OnFailure", bad}}}}).render(); err == nil {
			t.Errorf("OnFailure=%q accepted", bad)
		}
	}
}

func TestDatabaseUnitsGolden(t *testing.T) {
	check := func(dir, fn string, b []byte, err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
		golden.Check(t, filepath.Join("units", dir, fn), b)
	}

	// Postgres unit set
	fn, b, err := DBVolume("shopdb")
	check("db-postgres", fn, b, err)
	fn, b, err = Network("shopdb")
	check("db-postgres", fn, b, err)
	fn, b, err = DBContainer(DBRelease{
		Name:       "shopdb",
		Engine:     "postgres",
		Image:      "docker.io/library/postgres:18",
		EnvFile:    "/home/server/.config/dermaga/env/db-shopdb.env",
		VolumePath: "/var/lib/postgresql",
		Caps:       []string{"CHOWN", "DAC_OVERRIDE", "FOWNER", "SETGID", "SETUID", "KILL"},
	})
	check("db-postgres", fn, b, err)

	// MariaDB unit set
	fn, b, err = DBVolume("mariadb")
	check("db-mariadb", fn, b, err)
	fn, b, err = Network("mariadb")
	check("db-mariadb", fn, b, err)
	fn, b, err = DBContainer(DBRelease{
		Name:       "mariadb",
		Engine:     "mariadb",
		Image:      "docker.io/library/mariadb:11.8",
		EnvFile:    "/home/server/.config/dermaga/env/db-mariadb.env",
		VolumePath: "/var/lib/mysql",
		Caps:       []string{"CHOWN", "DAC_OVERRIDE", "SETGID", "SETUID", "KILL"},
	})
	check("db-mariadb", fn, b, err)

	// Valkey unit set
	fn, b, err = DBVolume("cache")
	check("db-valkey", fn, b, err)
	fn, b, err = Network("cache")
	check("db-valkey", fn, b, err)
	fn, b, err = DBContainer(DBRelease{
		Name:       "cache",
		Engine:     "valkey",
		Image:      "docker.io/valkey/valkey:9",
		EnvFile:    "/home/server/.config/dermaga/env/db-cache.env",
		VolumePath: "/data",
		Caps:       []string{"SETGID", "SETUID", "KILL"},
		Exec:       `/bin/sh -c 'exec valkey-server --requirepass "$VALKEY_PASSWORD"'`,
	})
	check("db-valkey", fn, b, err)
}

func TestHealthCmdEscaping(t *testing.T) {
	_, b, err := Container(containerCases()["health-escaping"])
	if err != nil {
		t.Fatal(err)
	}
	want := "HealthCmd=sh -c 'test \"$$(cat /tmp/x)\" = ok' && echo 100%% `date` \\n done\n"
	if !strings.Contains(string(b), want) {
		t.Errorf("want line %q in\n%s", want, b)
	}
}

// Injection: newline, %, $, backtick and quotes in every user-controlled field
// are rejected, or escaped where the field is free text (HealthCmd, above).
func TestInjectionRejected(t *testing.T) {
	bad := []string{"a\nb", "a\rExecStartPre=/bin/evil", "100%", "$x", "a`b", `a"b`, "a'b", "a b", "a\\"}
	for _, s := range bad {
		for field, r := range map[string]func(*Release){
			"app name":      func(r *Release) { r.App.Name += s },
			"rel8":          func(r *Release) { r.Rel8 = "1a2b3c4" + s },
			"image":         func(r *Release) { r.Image += s },
			"env file":      func(r *Release) { r.EnvFile += s },
			"database":      func(r *Release) { r.Databases = []string{"db" + s} },
			"volume name":   func(r *Release) { r.App.Volumes = []spec.Volume{{Name: "data" + s, Path: "/data"}} },
			"volume path":   func(r *Release) { r.App.Volumes = []spec.Volume{{Name: "data", Path: "/data" + s}} },
			"capability":    func(r *Release) { r.App.AddCaps = []string{"CHOWN" + s} },
			"memory":        func(r *Release) { r.App.Limits.Memory = "512m" + s },
			"cpus":          func(r *Release) { r.App.Limits.CPUs = "1" + s },
			"health cmd nl": func(r *Release) { r.App.Health.Cmd = "true\n[Service]\nExecStartPre=/bin/evil" },
		} {
			rel := release(spec.App{})
			r(&rel)
			if _, _, err := Container(rel); err == nil {
				t.Errorf("%s %q: accepted", field, s)
			}
		}
		if _, _, err := Hooks("/relay"+s, "/run/hooks"); err == nil {
			t.Errorf("hooks rootfs %q: accepted", s)
		}
		if _, _, err := DBVolume("shopdb" + s); err == nil {
			t.Errorf("db volume %q: accepted", s)
		}
		for field, f := range map[string]func(*DBRelease){
			"db name":     func(d *DBRelease) { d.Name += s },
			"db image":    func(d *DBRelease) { d.Image += s },
			"db env":      func(d *DBRelease) { d.EnvFile += s },
			"db vol path": func(d *DBRelease) { d.VolumePath += s },
		} {
			dbr := DBRelease{
				Name: "shopdb", Engine: "postgres", Image: "docker.io/library/postgres:18",
				EnvFile: "/home/server/.config/dermaga/env/db-shopdb.env", VolumePath: "/var/lib/postgresql",
			}
			f(&dbr)
			if _, _, err := DBContainer(dbr); err == nil {
				t.Errorf("%s %q: accepted", field, s)
			}
		}
		tm := Timer{Name: "backup-system", Description: "backup system", Binary: "/usr/local/bin/dermaga", Args: []string{"backup", "run", "system"}, OnCalendar: "daily"}
		for field, f := range map[string]func(*Timer){
			"timer arg":      func(t *Timer) { t.Args = append(t.Args, s) },
			"timer calendar": func(t *Timer) { t.OnCalendar += s },
			"timer binary":   func(t *Timer) { t.Binary += s },
		} {
			if field == "timer calendar" && s == "a b" {
				continue // spaces are part of calendar specs
			}
			bt := tm
			f(&bt)
			if _, _, err := bt.Units(); err == nil {
				t.Errorf("%s %q: accepted", field, s)
			}
		}
	}
	for _, v := range []string{"", " x", "x ", "a\x00b", "a\\"} {
		if _, err := (unit{{"Container", [][2]string{{"Image", v}}}}).render(); err == nil {
			t.Errorf("render accepted value %q", v)
		}
	}
}

// TestDryRun runs Quadlet on every golden unit set, next to a web.network
// like the installer's. Skipped where Podman isn't installed or is older than
// MinPodman; with DERMAGA_REQUIRE_QUADLET=1 (CI) both are failures instead.
func TestDryRun(t *testing.T) {
	skip := t.Skipf
	if os.Getenv("DERMAGA_REQUIRE_QUADLET") == "1" {
		skip = t.Fatalf
	}
	if _, err := os.Stat(Bin); err != nil {
		skip("%s not installed; the dry-run needs Podman >= %s", Bin, MinPodman)
	}
	out, err := exec.Command(Bin, "-version").Output()
	if err != nil {
		t.Fatalf("%s -version: %v", Bin, err)
	}
	found := strings.TrimSpace(string(out))
	if !versionAtLeast(found, MinPodman) {
		skip("Quadlet %s is older than the minimum supported Podman %s", found, MinPodman)
	}
	t.Logf("Quadlet %s (minimum %s)", found, MinPodman)
	dirs, err := os.ReadDir(filepath.Join("testdata", "units"))
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range dirs {
		files := map[string][]byte{"web.network": []byte("[Network]\nNetworkName=web\n")}
		entries, _ := os.ReadDir(filepath.Join("testdata", "units", d.Name()))
		for _, e := range entries {
			b, err := os.ReadFile(filepath.Join("testdata", "units", d.Name(), e.Name()))
			if err != nil {
				t.Fatal(err)
			}
			files[e.Name()] = b
		}
		if err := DryRun(context.Background(), "", files); err != nil {
			t.Errorf("%s: %v", d.Name(), err)
		}
	}
	bad := map[string][]byte{"dg-bad-00000000.container": []byte("[Container]\nImage=docker.io/library/busybox:1\nBogusKey=1\n")}
	if err := DryRun(context.Background(), "", bad); err == nil || !strings.Contains(err.Error(), "BogusKey") {
		t.Errorf("an unknown key must be rejected, got %v", err)
	}
}

// versionAtLeast compares the leading major.minor.patch numbers of v and min.
// A version it can't read counts as too old.
func versionAtLeast(v, min string) bool {
	var a, b [3]int
	if n, _ := fmt.Sscanf(v, "%d.%d.%d", &a[0], &a[1], &a[2]); n != 3 {
		return false
	}
	fmt.Sscanf(min, "%d.%d.%d", &b[0], &b[1], &b[2])
	for i := range a {
		if a[i] != b[i] {
			return a[i] > b[i]
		}
	}
	return true
}

func TestVersionAtLeast(t *testing.T) {
	for v, want := range map[string]bool{
		"5.4.2": true, "5.8.9": true, "6.0.0": true, "5.10.0": true, "5.4.2+ds1": true,
		"5.4.1": false, "4.9.3": false, "5.3.9": false, "": false, "garbage": false,
	} {
		if got := versionAtLeast(v, MinPodman); got != want {
			t.Errorf("versionAtLeast(%q, %q) = %v, want %v", v, MinPodman, got, want)
		}
	}
}
