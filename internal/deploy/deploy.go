package deploy

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"dermaga/internal/builder"
	"dermaga/internal/crypto"
	"dermaga/internal/github"
	"dermaga/internal/host"
	"dermaga/internal/name"
	"dermaga/internal/quadlet"
	"dermaga/internal/store"
)

// Config configures paths and runtime dependencies for the deployment Engine.
type Config struct {
	QuadletDir        string
	TraefikDynamicDir string
	EnvDir            string
	MirrorsDir        string
	BuildsDir         string
	BinaryPath        string
	MasterKey         []byte
	StabilityWindow   time.Duration // default 5s when health_type == "none"
	ProbeInterval     time.Duration // default 1s
	MeminfoPath       string        // default /proc/meminfo (memory check)
	DrainGrace        time.Duration // default 10s: old release keeps finishing requests (DEP-1)
	SwitchTimeout     time.Duration // default 30s per Traefik verification step
	// VerifyRoute asks Traefik for host+path with the verify token (default: the netns probe to Traefik's verify entrypoint).
	VerifyRoute func(ctx context.Context, host, path, token string) (bool, error)
}

// Engine manages the single global build queue and deployment lifecycle (DEP-5–DEP-9).
type Engine struct {
	store   *store.Store
	host    host.Host
	gh      *github.Client
	builder *builder.Builder
	cfg     Config

	mu          sync.Mutex
	logSaveMu   sync.Mutex        // serializes log saves so an older snapshot never overwrites a newer one
	order       []string          // appID queue order
	pending     map[string]string // appID -> queued depID
	activeDepID string
	activeAppID string
	activeStop  context.CancelFunc
	cancelled   map[string]bool
	appLocks    map[string]chan struct{} // one release per app starts or switches at a time
	pushMu      sync.Mutex               // serializes HandlePush's duplicate check and enqueue
	baseCtx     context.Context          // daemon lifetime, for rollbacks that outlive the request
	wake        chan struct{}
	stopCh      chan struct{}
}

// NewEngine creates a deployment Engine.
func NewEngine(s *store.Store, h host.Host, gh *github.Client, b *builder.Builder, cfg Config) *Engine {
	if cfg.StabilityWindow <= 0 {
		cfg.StabilityWindow = 5 * time.Second
	}
	if cfg.ProbeInterval <= 0 {
		cfg.ProbeInterval = 1 * time.Second
	}
	if cfg.MeminfoPath == "" {
		cfg.MeminfoPath = "/proc/meminfo"
	}
	if cfg.DrainGrace <= 0 {
		cfg.DrainGrace = DefaultDrainGrace
	}
	if cfg.SwitchTimeout <= 0 {
		cfg.SwitchTimeout = 30 * time.Second
	}
	e := &Engine{
		store:     s,
		host:      h,
		gh:        gh,
		builder:   b,
		cfg:       cfg,
		pending:   make(map[string]string),
		cancelled: make(map[string]bool),
		appLocks:  make(map[string]chan struct{}),
		baseCtx:   context.Background(),
		wake:      make(chan struct{}, 1),
		stopCh:    make(chan struct{}),
	}
	if e.cfg.VerifyRoute == nil {
		e.cfg.VerifyRoute = e.verifyThroughTraefik
	}
	return e
}

// Logs returns the Builder's LogHub for live SSE streaming.
func (e *Engine) Logs() *builder.LogHub {
	return e.builder.Logs()
}

// Start recovers any in-flight deployments interrupted by a previous daemon crash/restart
// and launches the single-concurrency build worker.
func (e *Engine) Start(ctx context.Context) error {
	e.baseCtx = ctx
	if err := e.convertLegacySnapshots(ctx); err != nil {
		// Serving must not depend on it: those releases just cannot be rolled back to.
		fmt.Fprintf(os.Stderr, "Warning: converting pre-0005 release snapshots: %v\n", err)
	}
	failed, err := e.store.FailInFlightDeployments(ctx, "daemon restarted mid-deployment")
	if err != nil {
		return fmt.Errorf("failing interrupted deployments: %w", err)
	}
	// The reconciler already removed their units and env files; the image their own build tagged is
	// never used again. Not d.ImageRef: a rollback's points at the image of the release it restores.
	for _, d := range failed {
		if app, err := e.store.GetApp(ctx, d.AppID); err == nil {
			_, _ = e.host.RunCommand(ctx, "podman", "rmi", "--ignore", builder.ImageRef(app.Name, d.ReleaseNumber))
		}
	}
	// BuildsDir holds only scratch (source exports, Quadlet dry-runs) that a build removes when it
	// ends; a crash skips that, and nothing builds before the worker starts.
	if e.cfg.BuildsDir != "" {
		_ = e.host.RemoveAll(e.cfg.BuildsDir)
	}
	go e.workerLoop(ctx)
	return nil
}

// Enqueue creates a new queued deployment for appID, superseding any existing queued
// deployment for the same app (DEP-5).
func (e *Engine) Enqueue(ctx context.Context, appID, trigger, commitHash, commitMsg string) (*store.Deployment, error) {
	app, err := e.store.GetApp(ctx, appID)
	if err != nil {
		return nil, err
	}
	relNum, err := e.store.NextReleaseNumber(ctx, appID)
	if err != nil {
		return nil, fmt.Errorf("computing release number: %w", err)
	}

	if app.EnvVersionID == nil {
		return nil, errors.New("the app has no stored environment version")
	}

	depID := "dep_" + randomHex(12)
	dep := &store.Deployment{
		ID:            depID,
		AppID:         appID,
		ReleaseNumber: relNum,
		Status:        "queued",
		Trigger:       trigger,
		CommitHash:    commitHash,
		CommitMessage: commitMsg,
		EnvVersionID:  app.EnvVersionID,
		RuntimeConfig: RuntimeConfigOf(app).json(),
	}
	if err := e.store.CreateDeployment(ctx, dep); err != nil {
		return nil, fmt.Errorf("creating deployment: %w", err)
	}

	e.mu.Lock()
	if oldDepID, exists := e.pending[appID]; exists {
		now := nowUTC()
		_ = e.store.UpdateDeploymentStatus(ctx, oldDepID, "superseded", "superseded by newer deployment", &now)
		e.pending[appID] = depID
	} else {
		e.pending[appID] = depID
		e.order = append(e.order, appID)
	}
	e.mu.Unlock()

	e.notifyWorker()
	return dep, nil
}

// Cancel cancels a queued or currently running deployment (DEP-6).
func (e *Engine) Cancel(ctx context.Context, depID string) error {
	e.mu.Lock()
	for appID, qDepID := range e.pending {
		if qDepID == depID {
			delete(e.pending, appID)
			e.mu.Unlock()
			now := nowUTC()
			if err := e.store.UpdateDeploymentStatus(ctx, depID, "cancelled", "cancelled by operator", &now); err != nil {
				return err
			}
			e.builder.Logs().Append(depID, "==> Deployment cancelled while queued")
			e.saveLogs(ctx, depID)
			e.builder.Logs().Finish(depID)
			return nil
		}
	}
	if e.activeDepID == depID && e.activeStop != nil {
		e.cancelled[depID] = true
		cancel := e.activeStop
		e.mu.Unlock()
		cancel()
		return nil
	}
	e.mu.Unlock()

	dep, err := e.store.GetDeployment(ctx, depID)
	if err != nil {
		return err
	}
	return fmt.Errorf("deployment %s is %s and cannot be cancelled", dep.ID, dep.Status)
}

func (e *Engine) notifyWorker() {
	select {
	case e.wake <- struct{}{}:
	default:
	}
}

func (e *Engine) workerLoop(ctx context.Context) {
	for {
		depID := e.popNext()
		if depID == "" {
			select {
			case <-ctx.Done():
				return
			case <-e.stopCh:
				return
			case <-e.wake:
				continue
			}
		}
		e.runOne(ctx, depID)
	}
}

func (e *Engine) popNext() string {
	e.mu.Lock()
	defer e.mu.Unlock()
	for len(e.order) > 0 {
		appID := e.order[0]
		e.order = e.order[1:]
		if depID, ok := e.pending[appID]; ok {
			delete(e.pending, appID)
			return depID
		}
	}
	return ""
}

func (e *Engine) runOne(parentCtx context.Context, depID string) {
	dep, err := e.store.GetDeployment(parentCtx, depID)
	if err != nil {
		return
	}
	app, err := e.store.GetApp(parentCtx, dep.AppID)
	if err != nil {
		return
	}

	runCtx, cancel := context.WithCancel(parentCtx)
	e.mu.Lock()
	e.activeDepID = depID
	e.activeAppID = app.ID
	e.activeStop = cancel
	e.mu.Unlock()

	defer func() {
		cancel()
		e.mu.Lock()
		e.activeDepID = ""
		e.activeAppID = ""
		e.activeStop = nil
		delete(e.cancelled, depID)
		e.mu.Unlock()
	}()

	isCancelled := func() bool {
		e.mu.Lock()
		c := e.cancelled[depID]
		e.mu.Unlock()
		return c || errors.Is(runCtx.Err(), context.Canceled)
	}
	logf, stopLogs := e.releaseLogger(parentCtx, depID)
	defer stopLogs()
	fail := func(err error, imageRef string) {
		if imageRef == "" {
			imageRef = builder.ImageRef(app.Name, dep.ReleaseNumber)
		}
		e.failRelease(parentCtx, app, dep, err, imageRef, isCancelled())
	}

	// 1. Building
	_ = e.store.UpdateDeploymentStatus(parentCtx, depID, "building", "", nil)
	rc, err := ReleaseRuntimeConfig(app, dep)
	if err != nil {
		fail(err, "")
		return
	}
	// Refuse early instead of after a 20-minute build; checked again right before the start.
	if err := e.requireMemory(rc.MemoryLimit); err != nil {
		fail(err, "")
		return
	}
	token, err := e.mintRepoToken(runCtx, app)
	if err != nil {
		fail(err, "")
		return
	}

	buildEnv, err := OpenEnvMap(e.cfg.MasterKey, "build_env_encrypted", app.BuildEnvEncrypted)
	if err != nil {
		fail(err, "")
		return
	}
	runtimeEnv, err := e.releaseEnv(parentCtx, dep)
	if err != nil {
		fail(err, "")
		return
	}
	runtimeSecrets := make([]string, 0, len(runtimeEnv))
	for _, v := range runtimeEnv {
		runtimeSecrets = append(runtimeSecrets, v)
	}

	commitRef := dep.CommitHash
	if commitRef == "" {
		commitRef = app.Branch
	}

	buildMem, _ := e.store.GetSetting(parentCtx, "build_memory_limit")
	if buildMem != "" {
		if err := builder.ValidateMemoryLimit(buildMem); err != nil {
			fail(err, "")
			return
		}
	}
	buildCPU, _ := e.store.GetSetting(parentCtx, "build_cpu_limit")
	var buildTimeout time.Duration
	if ts, err := e.store.GetSetting(parentCtx, "build_timeout_sec"); err == nil && ts != "" {
		parsed, err := builder.ValidateTimeoutSec(ts)
		if err != nil {
			fail(err, "")
			return
		}
		buildTimeout = parsed
	}

	imageRef, err := e.builder.Build(runCtx, builder.BuildRequest{
		AppName:           app.Name,
		RepoOwner:         app.RepoOwner,
		RepoName:          app.RepoName,
		CommitRef:         commitRef,
		ReleaseNumber:     dep.ReleaseNumber,
		DeploymentID:      depID,
		ContainerfilePath: app.ContainerfilePath,
		BuildContext:      app.BuildContext,
		InstallToken:      token,
		BuildEnv:          buildEnv,
		RuntimeSecrets:    runtimeSecrets,
		Memory:            buildMem,
		CPUs:              buildCPU,
		Timeout:           buildTimeout,
	})
	if err != nil {
		fail(err, "")
		return
	}
	_ = e.store.SetDeploymentImageRef(parentCtx, depID, imageRef)

	// 2.–4. Start next to the live release, health check, switch (one release per app at a time).
	lock := e.appLock(app.ID)
	select {
	case lock <- struct{}{}:
	case <-runCtx.Done():
		fail(runCtx.Err(), imageRef)
		return
	}
	err = e.startAndSwitch(runCtx, app, dep, imageRef, rc, runtimeEnv, "inactive", logf)
	<-lock
	if err != nil {
		fail(err, imageRef)
		return
	}
	e.finishLive(parentCtx, depID)
}

// startAndSwitch starts a release next to the live one, waits until it is healthy and switches
// the route to it with proof through Traefik, then drains and stops the old release (DEP-1). Any
// error before the switch completes leaves the live release serving (DEP-2). The caller holds the
// app's lock. oldStatus is what the replaced release becomes ("inactive" or "rolled_back").
func (e *Engine) startAndSwitch(ctx context.Context, app *store.App, dep *store.Deployment, imageRef string, rc RuntimeConfig, env map[string]string, oldStatus string, logf func(string)) error {
	if err := e.requireMemory(rc.MemoryLimit); err != nil {
		return err
	}
	_ = e.store.UpdateDeploymentStatus(ctx, dep.ID, "starting", "", nil)
	logf(fmt.Sprintf("==> Starting release r%d", dep.ReleaseNumber))

	envFile := ""
	if len(env) > 0 {
		envData, err := FormatEnvFile(env)
		if err != nil {
			return err
		}
		envFile = EnvFilePath(e.cfg.EnvDir, app.Name, dep.ReleaseNumber)
		if err := e.host.WriteFileAtomic(envFile, envData, 0600); err != nil {
			return fmt.Errorf("writing env file: %w", err)
		}
	}

	unitBytes, err := quadlet.RenderContainer(ContainerSpecFor(app.Name, dep.ReleaseNumber, imageRef, rc, envFile))
	if err != nil {
		return fmt.Errorf("rendering quadlet unit: %w", err)
	}
	unitName := name.AppQuadletFilename(app.Name, dep.ReleaseNumber)
	dryDir := filepath.Join(e.cfg.BuildsDir, ".dryrun-"+dep.ID)
	defer func() { _ = e.host.RemoveAll(dryDir) }()
	if _, err := quadlet.NewDryRunValidator(e.host, "").ValidateUnit(ctx, unitName, unitBytes, dryDir); err != nil {
		return fmt.Errorf("quadlet dry-run failed: %w", err)
	}
	if err := e.host.WriteFileAtomic(filepath.Join(e.cfg.QuadletDir, unitName), unitBytes, 0644); err != nil {
		return fmt.Errorf("writing quadlet unit: %w", err)
	}
	if _, err := e.host.RunCommand(ctx, "systemctl", "--user", "daemon-reload"); err != nil {
		return fmt.Errorf("systemd daemon-reload: %w", err)
	}
	serviceName := name.AppServiceName(app.Name, dep.ReleaseNumber)
	if _, err := e.host.RunCommand(ctx, "systemctl", "--user", "start", serviceName); err != nil {
		return fmt.Errorf("starting container service: %w", err)
	}

	_ = e.store.UpdateDeploymentStatus(ctx, dep.ID, "probing", "", nil)
	if err := e.waitHealthy(ctx, app.Name, rc, dep.ReleaseNumber, serviceName); err != nil {
		return err
	}

	fresh, err := e.store.GetApp(ctx, app.ID)
	if err != nil {
		return err
	}
	if fresh.ActiveReleaseID == nil { // first deploy: nothing to switch from (DEP-3)
		if _, _, err := e.writeRoute(ctx, fresh, dep.ReleaseNumber, rc.Port, nil); err != nil {
			return err
		}
		return e.store.FinishSwitch(ctx, app.ID, dep.ID, "", "")
	}
	live, err := e.store.GetDeployment(ctx, *fresh.ActiveReleaseID)
	if err != nil {
		return fmt.Errorf("loading the live release: %w", err)
	}
	liveRC, err := ReleaseRuntimeConfig(fresh, live)
	if err != nil {
		return err
	}

	// From here on a cancel or a closed request must not cut the switch halfway.
	sctx := context.WithoutCancel(ctx)
	_ = e.store.UpdateDeploymentStatus(sctx, dep.ID, "switching", "", nil)
	if err := e.switchTraffic(sctx, fresh, live.ReleaseNumber, liveRC.Port, dep.ReleaseNumber, rc.Port, logf); err != nil {
		return err
	}
	if err := e.store.FinishSwitch(sctx, app.ID, dep.ID, live.ID, oldStatus); err != nil {
		return err
	}
	logf(fmt.Sprintf("==> Draining r%d for %s before stopping it", live.ReleaseNumber, e.cfg.DrainGrace))
	time.Sleep(e.cfg.DrainGrace)
	e.stopRelease(sctx, app.Name, live.ReleaseNumber)
	e.applyRetention(sctx, fresh, dep.ID)
	return nil
}

// saveLogs writes a release's log so far to the DB.
func (e *Engine) saveLogs(ctx context.Context, depID string) {
	e.logSaveMu.Lock()
	defer e.logSaveMu.Unlock()
	_ = e.store.SetDeploymentBuildLogs(context.WithoutCancel(ctx), depID, e.builder.Logs().Snapshot(depID))
}

// releaseLogger returns the stage logger of a running release: each "==>" line is saved at once
// and build output in between every 2 s, so a release adopted after a crash keeps its log up to
// the crash point (DEP-10). stop ends the periodic save.
func (e *Engine) releaseLogger(ctx context.Context, depID string) (logf func(string), stop func()) {
	done := make(chan struct{})
	go func() {
		t := time.NewTicker(2 * time.Second)
		defer t.Stop()
		last := ""
		for {
			select {
			case <-done:
				return
			case <-t.C:
				if cur := e.builder.Logs().Snapshot(depID); cur != last {
					last = cur
					e.saveLogs(ctx, depID)
				}
			}
		}
	}()
	return func(line string) {
		e.builder.Logs().Append(depID, line)
		e.saveLogs(ctx, depID)
	}, func() { close(done) }
}

func (e *Engine) finishLive(ctx context.Context, depID string) {
	e.builder.Logs().Append(depID, "==> Deployment is live")
	e.saveLogs(ctx, depID)
	e.builder.Logs().Finish(depID)
}

// failRelease removes a candidate that never went live and records why (DEP-2, DEP-3, DEP-6).
// imageRef "" keeps the image (a rollback reuses an image that stays retained).
func (e *Engine) failRelease(ctx context.Context, app *store.App, dep *store.Deployment, err error, imageRef string, cancelled bool) {
	e.cleanupCandidate(app, dep.ReleaseNumber, imageRef)
	if fresh, gErr := e.store.GetApp(ctx, app.ID); gErr == nil && fresh.ActiveReleaseID == nil {
		_ = e.host.Remove(filepath.Join(e.cfg.TraefikDynamicDir, name.AppDynamicFilename(app.Name)))
	}
	now := nowUTC()
	status, msg := "failed", err.Error()
	if cancelled {
		status, msg = "cancelled", "cancelled by operator"
		e.builder.Logs().Append(dep.ID, "==> Deployment cancelled; candidate resources cleaned up")
	} else {
		e.builder.Logs().Append(dep.ID, "==> Deployment failed: "+msg)
	}
	e.saveLogs(ctx, dep.ID)
	_ = e.store.UpdateDeploymentStatus(ctx, dep.ID, status, msg, &now)
	e.builder.Logs().Finish(dep.ID)
}

// Rollback starts the retained release targetID again from its image and environment version,
// next to the live release, and switches to it (DEP-4). No rebuild; refused while the app builds.
func (e *Engine) Rollback(ctx context.Context, appID, targetID string) (*store.Deployment, error) {
	e.mu.Lock()
	_, queued := e.pending[appID]
	building := e.activeAppID == appID
	e.mu.Unlock()
	if queued || building {
		return nil, ErrBuildRunning
	}
	app, err := e.store.GetApp(ctx, appID)
	if err != nil {
		return nil, err
	}
	if app.ActiveReleaseID == nil {
		return nil, errors.New("this app has no live release to roll back from")
	}
	deps, err := e.store.ListDeploymentsByApp(ctx, appID)
	if err != nil {
		return nil, err
	}
	var target *store.Deployment
	for _, d := range RetainedSet(deps, *app.ActiveReleaseID) {
		if d.ID == targetID && d.ID != *app.ActiveReleaseID {
			target = &d
		}
	}
	if target == nil {
		return nil, fmt.Errorf("only the last %d releases that were live can be rolled back to", RetainedReleases)
	}
	if target.EnvVersionID == nil || target.ImageRef == "" {
		return nil, fmt.Errorf("r%d has no stored environment or image to roll back to", target.ReleaseNumber)
	}
	rc, err := ReleaseRuntimeConfig(app, target)
	if err != nil {
		return nil, err
	}

	lock := e.appLock(appID)
	select {
	case lock <- struct{}{}:
	default:
		return nil, ErrSwitchRunning
	}
	relNum, err := e.store.NextReleaseNumber(ctx, appID)
	if err != nil {
		<-lock
		return nil, err
	}
	dep := &store.Deployment{
		ID:                 "dep_" + randomHex(12),
		AppID:              appID,
		ReleaseNumber:      relNum,
		Status:             "starting",
		Trigger:            "rollback",
		CommitHash:         target.CommitHash,
		CommitMessage:      target.CommitMessage,
		EnvVersionID:       target.EnvVersionID,
		RuntimeConfig:      rc.json(),
		SourceDeploymentID: &target.ID,
		ImageRef:           target.ImageRef,
	}
	if err := e.store.CreateDeployment(ctx, dep); err != nil {
		<-lock
		return nil, fmt.Errorf("creating deployment: %w", err)
	}
	go func() {
		defer func() { <-lock }()
		bctx := e.baseCtx
		logf, stopLogs := e.releaseLogger(bctx, dep.ID)
		defer stopLogs()
		logf(fmt.Sprintf("==> Rolling back to r%d: image %s, its environment and settings (no rebuild)", target.ReleaseNumber, target.ImageRef))
		env, err := e.releaseEnv(bctx, dep)
		if err == nil {
			err = e.startAndSwitch(bctx, app, dep, dep.ImageRef, rc, env, "rolled_back", logf)
		}
		if err != nil {
			e.failRelease(bctx, app, dep, err, "", false)
			return
		}
		e.finishLive(bctx, dep.ID)
	}()
	return dep, nil
}

func (e *Engine) waitHealthy(ctx context.Context, appName string, rc RuntimeConfig, relNum int, serviceName string) error {
	timeout := time.Duration(rc.HealthTimeoutSec) * time.Second
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	deadline := time.Now().Add(timeout)

	switch rc.HealthType {
	case "none":
		if _, err := e.host.RunCommand(ctx, "systemctl", "--user", "is-active", serviceName); err != nil {
			return fmt.Errorf("container failed to stay running: %w", err)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(e.cfg.StabilityWindow):
		}
		if _, err := e.host.RunCommand(ctx, "systemctl", "--user", "is-active", serviceName); err != nil {
			return fmt.Errorf("container exited during stability window: %w", err)
		}
		return nil

	case "cmd":
		for {
			if _, err := e.host.RunCommand(ctx, "systemctl", "--user", "is-active", serviceName); err == nil {
				return nil
			}
			if time.Now().After(deadline) {
				return fmt.Errorf("container health command timed out after %s", timeout)
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(e.cfg.ProbeInterval):
			}
		}

	default: // "http"
		healthPath := "/"
		if rc.HealthPath != nil && *rc.HealthPath != "" {
			healthPath = *rc.HealthPath
		}
		if !strings.HasPrefix(healthPath, "/") {
			healthPath = "/" + healthPath
		}
		probeURL := fmt.Sprintf("http://%s:%d%s", name.AppContainerName(appName, relNum), rc.Port, healthPath)
		needed := rc.HealthConsecutive
		if needed <= 0 {
			needed = 3
		}
		consecutive := 0
		for {
			_, err := e.host.RunCommand(ctx, "podman", "unshare", "--rootless-netns", e.cfg.BinaryPath, "probe", probeURL)
			if err == nil {
				consecutive++
				if consecutive >= needed {
					return nil
				}
			} else {
				consecutive = 0
			}
			if time.Now().After(deadline) {
				return fmt.Errorf("HTTP health check %s timed out after %s", healthPath, timeout)
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(e.cfg.ProbeInterval):
			}
		}
	}
}

func (e *Engine) cleanupCandidate(app *store.App, relNum int, imageRef string) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	_ = RemoveReleaseUnit(ctx, e.host, e.cfg.QuadletDir, e.cfg.EnvDir, app.Name, relNum)
	_, _ = e.host.RunCommand(ctx, "systemctl", "--user", "daemon-reload")
	if imageRef != "" {
		_, _ = e.host.RunCommand(ctx, "podman", "rmi", "-f", imageRef)
	}
}

func (e *Engine) mintRepoToken(ctx context.Context, app *store.App) (string, error) {
	if e.gh == nil {
		return "", errors.New("GitHub client is not configured")
	}
	appIDStr, err := e.store.GetSetting(ctx, "github_app_id")
	if err != nil || appIDStr == "" {
		return "", errors.New("GitHub is not connected")
	}
	appID, err := strconv.ParseInt(appIDStr, 10, 64)
	if err != nil {
		return "", fmt.Errorf("invalid github_app_id: %w", err)
	}
	pemSealed, err := e.store.GetSetting(ctx, "github_app_pem_encrypted")
	if err != nil || pemSealed == "" {
		return "", errors.New("GitHub App private key is missing")
	}
	pemBytes, err := crypto.Decrypt(e.cfg.MasterKey, "github_app_pem_encrypted", pemSealed)
	if err != nil {
		return "", fmt.Errorf("decrypting GitHub App key: %w", err)
	}
	fullName := app.RepoOwner + "/" + app.RepoName
	repoRec, err := e.store.GetGitHubRepositoryByFullName(ctx, fullName)
	if err != nil {
		return "", fmt.Errorf("repository %s is not accessible to the GitHub App: %w", fullName, err)
	}
	token, _, err := e.gh.CreateInstallationToken(ctx, appID, pemBytes, repoRec.InstallationID, []string{app.RepoName})
	if err != nil {
		return "", fmt.Errorf("minting installation token: %w", err)
	}
	return token, nil
}

// RemoveApp stops and removes all containers, units, images, Traefik routes, env files,
// bare mirror, and database rows for an app (APP-9).
func (e *Engine) RemoveApp(ctx context.Context, appID string) error {
	app, err := e.store.GetApp(ctx, appID)
	if err != nil {
		return err
	}

	e.mu.Lock()
	if qDepID, ok := e.pending[appID]; ok {
		delete(e.pending, appID)
		now := nowUTC()
		_ = e.store.UpdateDeploymentStatus(ctx, qDepID, "cancelled", "app removed", &now)
		e.builder.Logs().Finish(qDepID)
	}
	if e.activeAppID == appID && e.activeStop != nil {
		e.cancelled[e.activeDepID] = true
		e.activeStop()
	}
	e.mu.Unlock()

	deps, _ := e.store.ListDeploymentsByApp(ctx, appID)
	for _, d := range deps {
		img := d.ImageRef
		if img == "" {
			img = builder.ImageRef(app.Name, d.ReleaseNumber)
		}
		e.cleanupCandidate(app, d.ReleaseNumber, img)
		e.builder.Logs().Delete(d.ID)
	}
	_ = e.host.Remove(filepath.Join(e.cfg.TraefikDynamicDir, name.AppDynamicFilename(app.Name)))
	_ = e.host.RemoveAll(filepath.Join(e.cfg.MirrorsDir, app.Name+".git"))
	_ = e.host.RemoveAll(filepath.Join(e.cfg.BuildsDir, app.Name)) // the builder leaves the empty per-app folder

	return e.store.DeleteApp(ctx, appID)
}

func nowUTC() string {
	return time.Now().UTC().Format(time.RFC3339)
}

func randomHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
