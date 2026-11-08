// Package deploy runs releases (BRIEF §3.5): one global queue with
// concurrency 1, where a newer trigger supersedes a queued one per app; then
// build or pull, start the candidate, check its health and switch the route.
// M3 implements the recreate strategy for every app; blue-green follows in M4.
// Host access is behind the Host interface so tests can fake it.
package deploy

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/r3p-dev/dermaga/internal/apps"
	"github.com/r3p-dev/dermaga/internal/audit"
	"github.com/r3p-dev/dermaga/internal/config"
	"github.com/r3p-dev/dermaga/internal/quadlet"
	"github.com/r3p-dev/dermaga/internal/spec"
	"github.com/r3p-dev/dermaga/internal/traefik"
)

// Prepared is a release's image, ready to run.
type Prepared struct {
	Image       string
	SHA         string // git apps
	Subject     string // commit subject
	Healthcheck bool   // the image defines a HEALTHCHECK
}

// Check describes how to decide that a started release is healthy (BRIEF §3.5 step 6).
type Check struct {
	Container string
	Unit      string // <container>.service
	Port      int
	Path      string // HTTP health path, probed through the rootless netns (D2)
	Notify    bool   // HealthCmd or image HEALTHCHECK: the unit turns active when healthy (Notify=healthy)
	Successes int
	Timeout   time.Duration
}

// Host is everything a deploy does to the machine.
type Host interface {
	// Prepare fetches, exports and builds a git app's commit, or pulls an image app's image.
	Prepare(ctx context.Context, a apps.App, ref, rel8 string, log io.Writer) (Prepared, error)
	// InstallUnits dry-runs the unit files with Quadlet (D5), writes them and reloads systemd.
	InstallUnits(ctx context.Context, files map[string][]byte) error
	// WriteEnv replaces the app's env file and returns the previous content (nil if none).
	WriteEnv(app string, data []byte) (prev []byte, err error)
	Stop(ctx context.Context, unit string) error
	Start(ctx context.Context, unit string) error
	RemoveContainer(ctx context.Context, container string, stopTimeout int) error
	DaemonReload(ctx context.Context) error
	Healthy(ctx context.Context, c Check, log io.Writer) error
	WriteAppFile(name string, data []byte) error
	RemoveAppFile(name string) error
	// Verify polls path through Traefik's verify entrypoint with host and the
	// token header until it succeeds or the deadline passes (D7). anyStatus:
	// the app has no health path, so any app answer counts.
	Verify(ctx context.Context, host, path, token string, anyStatus bool, log io.Writer) error
	// ReadAppFile reads a Traefik dynamic file (nil, nil if missing).
	ReadAppFile(name string) ([]byte, error)
	// ImageHealthcheck inspects whether an existing image defines a HEALTHCHECK.
	ImageHealthcheck(ctx context.Context, image string) (bool, error)
	// PruneImages keeps the given images and removes older localhost/dermaga/<app> images.
	PruneImages(ctx context.Context, app string, keep []string) error
}

// VerifyTimeout is D7's deadline for Traefik to load a new route.
const VerifyTimeout = 10 * time.Second

type Engine struct {
	Apps       apps.Store
	Host       Host
	Paths      config.Paths
	Site       traefik.Site
	DrainGrace time.Duration // default: 10 * time.Second (BRIEF §3.5 step 8)
	KeepImages int           // default: 5 (BRIEF §3.5 step 8)

	mu      sync.Mutex
	order   []string        // app names in queue order
	pending map[string]*job // queued, not yet running, per app
	wake    chan struct{}
	locks   sync.Map // app name → *sync.Mutex
}

type job struct {
	app string
	rel apps.Release
}

const (
	defaultSuccesses = 3
	defaultTimeout   = 120 * time.Second
	stopTimeout      = 10
)

// Lock serialises every mutating action on one app (deploys, edits, removal).
func (e *Engine) Lock(app string) func() {
	m, _ := e.locks.LoadOrStore(app, &sync.Mutex{})
	m.(*sync.Mutex).Lock()
	return m.(*sync.Mutex).Unlock
}

// Enqueue records a queued release. A release of the same app that is still
// queued is marked superseded and replaced (BRIEF §3.5 step 2).
func (e *Engine) Enqueue(ctx context.Context, appName, trigger, ref string) (apps.Release, error) {
	a, err := e.Apps.Get(ctx, appName)
	if err != nil {
		return apps.Release{}, err
	}
	r, err := e.Apps.NewRelease(ctx, a.ID, trigger, ref)
	if err != nil {
		return apps.Release{}, err
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.pending == nil {
		e.pending, e.wake = map[string]*job{}, make(chan struct{}, 1)
	}
	if old := e.pending[appName]; old != nil {
		old.rel.Status, old.rel.Error = "superseded", "superseded by "+r.Rel8+" before it started"
		if err := e.Apps.Update(ctx, old.rel); err != nil {
			slog.Error("marking release superseded", "app", appName, "err", err)
		}
	} else {
		e.order = append(e.order, appName)
	}
	e.pending[appName] = &job{app: appName, rel: r}
	select {
	case e.wake <- struct{}{}:
	default:
	}
	return r, nil
}

// Rollback queues a rollback to a target release (or the previous release with an image).
func (e *Engine) Rollback(ctx context.Context, appName, targetRel8 string) (apps.Release, error) {
	a, err := e.Apps.Get(ctx, appName)
	if err != nil {
		return apps.Release{}, err
	}
	target, err := e.Apps.RollbackTarget(ctx, a.ID, targetRel8)
	if err != nil {
		return apps.Release{}, err
	}
	return e.Enqueue(ctx, appName, "rollback", target.Rel8)
}

// Drop forgets a queued release of app (the app is being removed).
func (e *Engine) Drop(app string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	delete(e.pending, app)
}

func (e *Engine) next() *job {
	e.mu.Lock()
	defer e.mu.Unlock()
	for len(e.order) > 0 {
		app := e.order[0]
		e.order = e.order[1:]
		if j := e.pending[app]; j != nil {
			delete(e.pending, app)
			return j
		}
	}
	return nil
}

// Run is the single deploy worker (concurrency 1). It returns when ctx ends.
func (e *Engine) Run(ctx context.Context) {
	e.mu.Lock()
	if e.pending == nil {
		e.pending, e.wake = map[string]*job{}, make(chan struct{}, 1)
	}
	wake := e.wake
	e.mu.Unlock()
	for {
		for j := e.next(); j != nil; j = e.next() {
			e.deploy(ctx, j)
		}
		select {
		case <-ctx.Done():
			return
		case <-wake:
		}
	}
}

// RunOnce deploys everything queued, then returns (tests).
func (e *Engine) RunOnce(ctx context.Context) {
	for j := e.next(); j != nil; j = e.next() {
		e.deploy(ctx, j)
	}
}

func (e *Engine) deploy(ctx context.Context, j *job) {
	defer e.Lock(j.app)()
	r := j.rel
	a, err := e.Apps.Get(ctx, j.app)
	if err != nil {
		r.Status, r.Error = "failed", err.Error()
		e.Apps.Update(ctx, r)
		return
	}
	env, err := e.Apps.Env(ctx, a.ID)
	if err != nil {
		r.Status, r.Error = "failed", err.Error()
		e.Apps.Update(ctx, r)
		return
	}
	r.LogPath = e.Paths.BuildLog(a.Spec.Name, r.Rel8)
	logw, closeLog := openLog(r.LogPath, env)
	defer closeLog()
	logf := func(format string, args ...any) {
		fmt.Fprintf(logw, "==> %s %s\n", time.Now().Format("15:04:05"), fmt.Sprintf(format, args...))
	}

	err = e.run(ctx, a, env, &r, logw, logf)
	if err != nil {
		r.Status, r.Error = "failed", err.Error()
		logf("FAILED: %v", err)
		if a.Live != nil {
			logf("release %s keeps serving", a.Live.Rel8)
		} else {
			logf("first deploy failed: nothing is left running and no route exists")
		}
		if uerr := e.Apps.Update(ctx, r); uerr != nil {
			slog.Error("recording failed release", "app", a.Spec.Name, "err", uerr)
		}
		e.audit(actor(r), "deploy.failed", a.Spec.Name, r.Rel8+": "+err.Error())
		return
	}
	logf("release %s is live", r.Rel8)
	e.audit(actor(r), "deploy.live", a.Spec.Name, r.Rel8)
}

// run executes the deployment pipeline: blue-green for stateless apps,
// recreate for apps with volumes (BRIEF §3.5). Every error return leaves the previous
// live release serving with its route (or, on a first deploy, nothing at all).
func (e *Engine) run(ctx context.Context, a apps.App, env map[string]string, r *apps.Release, log io.Writer, logf func(string, ...any)) error {
	name := a.Spec.Name
	strat := a.Spec.EffectiveStrategy()
	r.Status = "building"
	if err := e.Apps.Update(ctx, *r); err != nil {
		return err
	}
	logf("release %s of %s (%s, trigger %s, strategy %s)", r.Rel8, name, a.Spec.Source.Type, r.Trigger, strat)

	var p Prepared
	var err error
	if r.Trigger == "rollback" {
		target, terr := e.Apps.Release(ctx, a.ID, r.Ref)
		if terr != nil {
			return fmt.Errorf("target release %s: %w", r.Ref, terr)
		}
		logf("rollback to release %s (image %s)", target.Rel8, target.Image)
		p = Prepared{Image: target.Image, SHA: target.SHA, Subject: "rollback to " + target.Rel8}
		hc, herr := e.Host.ImageHealthcheck(ctx, p.Image)
		if herr != nil {
			logf("inspecting image healthcheck: %v", herr)
		}
		p.Healthcheck = hc
	} else {
		p, err = e.Host.Prepare(ctx, a, r.Ref, r.Rel8, log)
		if err != nil {
			return fmt.Errorf("preparing the image: %w", err)
		}
	}
	r.Image, r.SHA, r.Message, r.Status = p.Image, p.SHA, p.Subject, "starting"
	if err := e.Apps.Update(ctx, *r); err != nil {
		return err
	}

	cname := quadlet.ContainerName(name, r.Rel8)
	files := map[string][]byte{}
	fn, unit, err := quadlet.Container(quadlet.Release{
		App: a.Spec, Rel8: r.Rel8, Image: p.Image, EnvFile: e.Paths.EnvFile(name), ImageHealthcheck: p.Healthcheck,
	})
	if err != nil {
		return err
	}
	files[fn] = unit
	for _, v := range a.Spec.Volumes {
		vf, vu, err := quadlet.Volume(name, v)
		if err != nil {
			return err
		}
		files[vf] = vu
	}
	route, err := traefik.App(a.Spec, cname, "", e.Site)
	if err != nil {
		return err
	}
	token := randToken()
	verifyRoute, err := traefik.App(a.Spec, cname, token, e.Site)
	if err != nil {
		return err
	}
	prevEnv, err := e.Host.WriteEnv(name, apps.EnvFile(env))
	if err != nil {
		return fmt.Errorf("writing the env file: %w", err)
	}

	// From here on every failure removes the candidate, restores the env file
	// and, if it was stopped, starts the previous release again.
	var old string
	if a.Live != nil {
		old = quadlet.ContainerName(name, a.Live.Rel8)
	}
	stopped, routed := false, false
	fail := func(err error) error {
		bg := context.WithoutCancel(ctx)
		if routed && old == "" {
			logf("first deploy: removing the route again")
			if rerr := e.Host.RemoveAppFile(traefik.AppFileName(name)); rerr != nil {
				logf("removing the route: %v", rerr)
			}
		}
		logf("removing candidate %s", cname)
		if rerr := e.Host.RemoveContainer(bg, cname, stopTimeout); rerr != nil {
			logf("removing candidate: %v", rerr)
		}
		e.Host.DaemonReload(bg)
		if prevEnv != nil {
			if _, werr := e.Host.WriteEnv(name, prevEnv); werr != nil {
				logf("restoring the env file: %v", werr)
			}
		}
		if stopped {
			logf("starting previous release %s again", old)
			if serr := e.Host.Start(bg, old+".service"); serr != nil {
				logf("starting %s: %v", old, serr)
			} else if herr := e.Host.Healthy(bg, check(a.Spec, old, false), log); herr != nil {
				logf("previous release %s is NOT healthy again: %v", old, herr)
			} else {
				logf("previous release %s is healthy again", old)
			}
		}
		if routed && old != "" {
			e.restoreRoute(bg, a.Spec, old, log, logf) // D8
		}
		return err
	}
	logf("installing %s", fn)
	if err := e.Host.InstallUnits(ctx, files); err != nil {
		return fail(err)
	}
	if strat == "recreate" && old != "" {
		logf("recreate: stopping %s", old)
		stopped = true // even a failed stop may have stopped it: start it again on failure
		if err := e.Host.Stop(ctx, old+".service"); err != nil {
			return fail(fmt.Errorf("stopping the previous release: %w", err))
		}
	}
	logf("starting %s", cname)
	if err := e.Host.Start(ctx, cname+".service"); err != nil {
		return fail(fmt.Errorf("starting the candidate: %w", err))
	}
	c := check(a.Spec, cname, p.Healthcheck)
	logf("health: %s", describe(c))
	if err := e.Host.Healthy(ctx, c, log); err != nil {
		return fail(fmt.Errorf("health check: %w", err))
	}
	r.Status = "healthy"
	e.Apps.Update(ctx, *r)
	logf("switching the route to %s, verifying through Traefik (D7)", cname)
	routed = true
	// Snapshot the current route (D8, BRIEF §3.5 step 7)
	_, _ = e.Host.ReadAppFile(traefik.AppFileName(name))
	if err := e.Host.WriteAppFile(traefik.AppFileName(name), verifyRoute); err != nil {
		return fail(fmt.Errorf("writing the route: %w", err))
	}
	vpath, anyStatus := verifyPath(a.Spec)
	if err := e.Host.Verify(ctx, a.Spec.Primary(), vpath, token, anyStatus, log); err != nil {
		return fail(fmt.Errorf("the new route was not served through Traefik within %s, so Traefik probably did not load the config: "+
			"look for an invalid file in Traefik's dynamic directory (often %s) with journalctl --user -u traefik (%w)", VerifyTimeout, traefik.CustomFile, err))
	}
	if err := e.Host.WriteAppFile(traefik.AppFileName(name), route); err != nil {
		// the verified route serves the candidate; only the token router is left over until the next start
		logf("removing the verify router: %v (startup reconciliation removes it)", err)
	}
	if err := e.Apps.PromoteLive(ctx, a.ID, *r); err != nil {
		return err // route already points at the candidate; startup reconciliation repairs from the DB
	}
	r.Status = "live"
	if old != "" {
		if strat == "blue-green" {
			grace := e.DrainGrace
			if grace <= 0 {
				grace = 10 * time.Second
			}
			logf("draining previous release %s for %s", old, grace)
			select {
			case <-time.After(grace):
			case <-ctx.Done():
			}
			logf("stopping previous release %s", old)
			if err := e.Host.Stop(ctx, old+".service"); err != nil {
				logf("stopping %s: %v", old, err)
			}
		}
		logf("removing previous release %s", old)
		if err := e.Host.RemoveContainer(ctx, old, stopTimeout); err != nil {
			logf("removing %s: %v (startup reconciliation retries)", old, err)
		}
		e.Host.DaemonReload(ctx)
	}
	e.pruneOldImages(ctx, a, logf)
	return nil
}

func (e *Engine) pruneOldImages(ctx context.Context, a apps.App, logf func(string, ...any)) {
	keepCount := e.KeepImages
	if keepCount <= 0 {
		keepCount = 5
	}
	rels, err := e.Apps.Releases(ctx, a.ID, 50)
	if err != nil {
		logf("pruning images: listing releases: %v", err)
		return
	}
	var keep []string
	seen := map[string]bool{}
	for _, rel := range rels {
		if rel.Image != "" && !seen[rel.Image] {
			seen[rel.Image] = true
			keep = append(keep, rel.Image)
			if len(keep) >= keepCount {
				break
			}
		}
	}
	if len(keep) > 0 {
		if err := e.Host.PruneImages(ctx, a.Spec.Name, keep); err != nil {
			logf("pruning images: %v", err)
		}
	}
}

func randToken() string {
	b := make([]byte, 16)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// verifyPath is what D7 requests through Traefik: the health path, or "/"
// where any app answer counts.
func verifyPath(a spec.App) (string, bool) {
	if a.Health.Path != "" {
		return a.Health.Path, false
	}
	return "/", true
}

// restoreRoute points the app's route back at the previous release and
// verifies it with its own token before dropping the token router (D8).
func (e *Engine) restoreRoute(ctx context.Context, a spec.App, old string, log io.Writer, logf func(string, ...any)) {
	f := traefik.AppFileName(a.Name)
	token := randToken()
	withToken, err1 := traefik.App(a, old, token, e.Site)
	steady, err2 := traefik.App(a, old, "", e.Site)
	if err := errors.Join(err1, err2); err != nil {
		logf("restoring the route: %v", err)
		return
	}
	logf("restoring the route to %s", old)
	if err := e.Host.WriteAppFile(f, withToken); err != nil {
		logf("restoring the route: %v", err)
		return
	}
	vpath, anyStatus := verifyPath(a)
	if err := e.Host.Verify(ctx, a.Primary(), vpath, token, anyStatus, log); err != nil {
		logf("the restored route did not verify either: Traefik is not loading dynamic config. Its last loaded "+
			"config keeps serving; look for an invalid file in its dynamic directory (often %s) with journalctl --user -u traefik (%v)", traefik.CustomFile, err)
	} else {
		logf("restored route verified")
	}
	if err := e.Host.WriteAppFile(f, steady); err != nil {
		logf("removing the verify router: %v", err)
	}
}

func check(a spec.App, cname string, imageHC bool) Check {
	c := Check{Container: cname, Unit: cname + ".service", Port: a.Port, Path: a.Health.Path,
		Notify: a.Health.Cmd != "" || imageHC, Successes: a.Health.Successes, Timeout: time.Duration(a.Health.TimeoutSec) * time.Second}
	if c.Successes == 0 {
		c.Successes = defaultSuccesses
	}
	if c.Timeout == 0 {
		c.Timeout = defaultTimeout
	}
	return c
}

func describe(c Check) string {
	switch {
	case c.Path != "":
		return fmt.Sprintf("HTTP GET %s on port %d, %d successes within %s", c.Path, c.Port, c.Successes, c.Timeout)
	case c.Notify:
		return fmt.Sprintf("container health check (Notify=healthy) within %s", c.Timeout)
	}
	return "WEAK: no health check configured, only 'running without restarts' (set a health path)"
}

// RenderRoute rewrites an app's route after a domain or middleware change.
// The caller holds the app's lock. Apps without a live release have no route.
func (e *Engine) RenderRoute(ctx context.Context, a apps.App) error {
	if a.Live == nil {
		return nil
	}
	b, err := traefik.App(a.Spec, quadlet.ContainerName(a.Spec.Name, a.Live.Rel8), "", e.Site)
	if err != nil {
		return err
	}
	return e.Host.WriteAppFile(traefik.AppFileName(a.Spec.Name), b)
}

// actor: deploys from a webhook are logged as such; manual ones come through the CLI or panel.
func actor(r apps.Release) string {
	if r.Trigger == "webhook" {
		return "webhook"
	}
	return "cli"
}

func (e *Engine) audit(actor, action, target, details string) {
	if err := audit.Log(e.Apps.DB, actor, action, target, details); err != nil {
		slog.Error("audit log write failed", "action", action, "err", err)
	}
}

// openLog opens a release's log file. Everything written is masked: env
// values of 4+ characters are replaced with *** line by line (BRIEF §3.5 step 4).
func openLog(path string, env map[string]string) (io.Writer, func()) {
	var secrets []string
	for _, v := range env {
		if len(v) >= 4 {
			secrets = append(secrets, v)
		}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		slog.Error("build log", "err", err)
		return io.Discard, func() {}
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		slog.Error("build log", "err", err)
		return io.Discard, func() {}
	}
	m := &masker{w: f, secrets: secrets}
	return m, func() { m.flush(); f.Close() }
}

type masker struct {
	mu      sync.Mutex
	w       io.Writer
	secrets []string
	buf     []byte
}

func (m *masker) Write(p []byte) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.buf = append(m.buf, p...)
	for {
		i := bytes.IndexByte(m.buf, '\n')
		if i < 0 {
			break
		}
		m.emit(m.buf[:i+1])
		m.buf = m.buf[i+1:]
	}
	if len(m.buf) > 64<<10 { // a huge line without newline: emit it masked rather than hold it
		m.emit(m.buf)
		m.buf = nil
	}
	return len(p), nil
}

func (m *masker) emit(line []byte) {
	s := string(line)
	for _, sec := range m.secrets {
		s = strings.ReplaceAll(s, sec, "***")
	}
	m.w.Write([]byte(s))
}

func (m *masker) flush() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.buf) > 0 {
		m.emit(m.buf)
		m.buf = nil
	}
}

// Follow copies a release's log to w line by line as it grows, until the
// release is finished (done reports that) and the file is drained, or ctx ends.
func Follow(ctx context.Context, path string, done func() bool, w func(line string) error) error {
	f, err := os.Open(path)
	for errors.Is(err, os.ErrNotExist) && !done() {
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(300 * time.Millisecond):
		}
		f, err = os.Open(path)
	}
	if err != nil {
		return err
	}
	defer f.Close()
	rd := bufio.NewReader(f)
	var buf string
	finished := false
	for {
		line, err := rd.ReadString('\n')
		buf += line
		switch {
		case err == nil:
			if werr := w(strings.TrimSuffix(buf, "\n")); werr != nil {
				return werr
			}
			buf = ""
			continue
		case err != io.EOF:
			return err
		case finished:
			if buf != "" {
				return w(buf)
			}
			return nil
		case done():
			finished = true // read once more: lines written before done() turned true
			continue
		}
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(300 * time.Millisecond):
		}
	}
}
