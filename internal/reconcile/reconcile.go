// Package reconcile compares the database (each app's live release) with the
// dg-* units and app-*.yml files on disk at daemon start, repairs what is
// deterministic and reports the rest (BRIEF §3.5, ARCHITECTURE §3.11). It
// never looks at a unit or file without dermaga's prefixes.
package reconcile

import (
	"bytes"
	"context"
	"fmt"
	"regexp"
	"strings"
	"sync"

	"github.com/r3p-dev/dermaga/internal/name"
	"github.com/r3p-dev/dermaga/internal/quadlet"
	"github.com/r3p-dev/dermaga/internal/spec"
	"github.com/r3p-dev/dermaga/internal/traefik"
)

// App is an app as stored, with its live release ("" if it has none).
type App struct {
	Spec spec.App
	Live string // rel8
}

// Host is what reconciliation needs from the machine (host.Host in production).
type Host interface {
	Units() ([]string, error)    // dg-*.container file names
	AppFiles() ([]string, error) // app-*.yml file names
	ActiveState(ctx context.Context, unit string) (string, error)
	Start(ctx context.Context, unit string) error
	DaemonReload(ctx context.Context) error
	RemoveContainer(ctx context.Context, container string, stopTimeout int) error // called concurrently
	ReadAppFile(name string) ([]byte, error)                                      // nil, nil if missing
	WriteAppFile(name string, data []byte) error
	RemoveAppFile(name string) error
}

// Report lists what was repaired, what was left alone, and what failed.
type Report struct {
	Repaired []string
	Drift    []string // needs a person; nothing was changed
	Errors   []string
}

// StopTimeout bounds how long an orphaned container gets to stop (D3).
const StopTimeout = 10

var candidateRE = regexp.MustCompile(`^` + name.Unit + `(.+)-([0-9a-f]{8})\.container$`)

// Run reconciles once. It goes on after a failed step and reports it.
//
//   - dg-<app>-<rel8>.container of a known app that isn't its live release
//     (a crash mid-deploy): removed.
//   - the live unit is not running: started.
//   - app-<name>.yml differs from what the database renders (missing, still
//     has the deploy-time verify router, stale backend): rewritten atomically.
//     A known app without a live release gets no file.
//   - units and files of unknown apps: reported, never touched.
func Run(ctx context.Context, h Host, apps []App, site traefik.Site) Report {
	var r Report
	fail := func(format string, a ...any) { r.Errors = append(r.Errors, fmt.Sprintf(format, a...)) }
	byName := map[string]App{}
	for _, a := range apps {
		byName[a.Spec.Name] = a
	}

	units, err := h.Units()
	if err != nil {
		fail("listing units: %v", err)
	}
	liveSeen := map[string]bool{}
	var orphans []string
	for _, u := range units {
		base := strings.TrimSuffix(strings.TrimPrefix(u, name.Unit), ".container")
		if base == "hooks" || strings.HasPrefix(base, "db-") {
			continue // owned by the hook relay and the database code
		}
		m := candidateRE.FindStringSubmatch(u)
		var a App
		known := false
		if m != nil {
			a, known = byName[m[1]]
		}
		switch {
		case !known:
			r.Drift = append(r.Drift, fmt.Sprintf("%s: no such app in the database (left alone)", u))
		case m[2] == a.Live:
			liveSeen[a.Spec.Name] = true
		default:
			orphans = append(orphans, strings.TrimSuffix(u, ".container"))
		}
	}
	// in parallel, so start waits one StopTimeout, not one per orphan
	errs := make([]error, len(orphans))
	var wg sync.WaitGroup
	for i, c := range orphans {
		wg.Add(1)
		go func() { defer wg.Done(); errs[i] = h.RemoveContainer(ctx, c, StopTimeout) }()
	}
	wg.Wait()
	removed := false
	for i, c := range orphans {
		if errs[i] != nil {
			fail("removing orphaned release %s: %v", c, errs[i])
			continue
		}
		removed = true
		r.Repaired = append(r.Repaired, fmt.Sprintf("%s: removed orphaned release (not live)", c))
	}
	if removed {
		if err := h.DaemonReload(ctx); err != nil {
			fail("daemon-reload: %v", err)
		}
	}

	for _, a := range apps {
		if a.Live == "" {
			continue
		}
		c := quadlet.ContainerName(a.Spec.Name, a.Live)
		if !liveSeen[a.Spec.Name] {
			r.Drift = append(r.Drift, fmt.Sprintf("%s.container: live release of %s has no unit file (redeploy the app)", c, a.Spec.Name))
			continue
		}
		state, err := h.ActiveState(ctx, c+".service")
		if err != nil {
			fail("state of %s: %v", c, err)
			continue
		}
		if state == "active" || state == "activating" || state == "reloading" || state == "queued" {
			continue
		}
		if err := h.Start(ctx, c+".service"); err != nil {
			fail("starting %s: %v", c, err)
			continue
		}
		r.Repaired = append(r.Repaired, fmt.Sprintf("%s: live release was %s, started", c, state))
	}

	files, err := h.AppFiles()
	if err != nil {
		fail("listing Traefik files: %v", err)
	}
	for _, f := range files {
		if _, ok := byName[strings.TrimSuffix(strings.TrimPrefix(f, name.AppFile), ".yml")]; !ok {
			r.Drift = append(r.Drift, fmt.Sprintf("%s: no such app in the database (left alone)", f))
		}
	}
	for _, a := range apps {
		f := traefik.AppFileName(a.Spec.Name)
		have, err := h.ReadAppFile(f)
		if err != nil {
			fail("reading %s: %v", f, err)
			continue
		}
		if a.Live == "" {
			if have != nil {
				if err := h.RemoveAppFile(f); err != nil {
					fail("removing %s: %v", f, err)
					continue
				}
				r.Repaired = append(r.Repaired, fmt.Sprintf("%s: removed (the app has no live release)", f))
			}
			continue
		}
		want, err := traefik.App(a.Spec, quadlet.ContainerName(a.Spec.Name, a.Live), "", site)
		if err != nil {
			fail("rendering %s: %v", f, err)
			continue
		}
		if bytes.Equal(have, want) {
			continue
		}
		why := "differed from the database"
		switch {
		case have == nil:
			why = "was missing"
		case bytes.Contains(have, []byte(`"`+traefik.VerifyRouter(a.Spec.Name)+`"`)):
			why = "still had the deploy-time verify router"
		}
		if err := h.WriteAppFile(f, want); err != nil {
			fail("writing %s: %v", f, err)
			continue
		}
		r.Repaired = append(r.Repaired, fmt.Sprintf("%s: %s, re-rendered", f, why))
	}
	return r
}
