package deploy

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
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
	"dermaga/internal/traefik"
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
}

// Engine manages the single global build queue and deployment lifecycle (DEP-5–DEP-9).
type Engine struct {
	store   *store.Store
	host    host.Host
	gh      *github.Client
	builder *builder.Builder
	cfg     Config

	mu          sync.Mutex
	order       []string          // appID queue order
	pending     map[string]string // appID -> queued depID
	activeDepID string
	activeAppID string
	activeStop  context.CancelFunc
	cancelled   map[string]bool
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
	return &Engine{
		store:     s,
		host:      h,
		gh:        gh,
		builder:   b,
		cfg:       cfg,
		pending:   make(map[string]string),
		cancelled: make(map[string]bool),
		wake:      make(chan struct{}, 1),
		stopCh:    make(chan struct{}),
	}
}

// Logs returns the Builder's LogHub for live SSE streaming.
func (e *Engine) Logs() *builder.LogHub {
	return e.builder.Logs()
}

// Start recovers any in-flight deployments interrupted by a previous daemon crash/restart
// and launches the single-concurrency build worker.
func (e *Engine) Start(ctx context.Context) error {
	if _, err := e.store.FailInFlightDeployments(ctx, "daemon restarted mid-deployment"); err != nil {
		return fmt.Errorf("failing interrupted deployments: %w", err)
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

	runtimeEnv, err := OpenEnvMap(e.cfg.MasterKey, "env_encrypted", app.EnvEncrypted)
	if err != nil {
		return nil, err
	}
	snapshot := struct {
		Env              map[string]string `json:"env"`
		Port             int               `json:"port"`
		HealthType       string            `json:"health_type"`
		HealthPath       *string           `json:"health_path"`
		HealthCmd        *string           `json:"health_cmd"`
		HealthTimeoutSec int               `json:"health_timeout_sec"`
		Command          *string           `json:"command"`
	}{
		Env:              runtimeEnv,
		Port:             app.Port,
		HealthType:       app.HealthType,
		HealthPath:       app.HealthPath,
		HealthCmd:        app.HealthCmd,
		HealthTimeoutSec: app.HealthTimeoutSec,
		Command:          app.Command,
	}
	snapSealed, err := sealJSON(e.cfg.MasterKey, "env_snapshot_encrypted", snapshot)
	if err != nil {
		return nil, err
	}

	depID := "dep_" + randomHex(12)
	dep := &store.Deployment{
		ID:                   depID,
		AppID:                appID,
		ReleaseNumber:        relNum,
		Status:               "queued",
		Trigger:              trigger,
		CommitHash:           commitHash,
		CommitMessage:        commitMsg,
		EnvSnapshotEncrypted: snapSealed,
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
			_ = e.store.SetDeploymentBuildLogs(ctx, depID, e.builder.Logs().Snapshot(depID))
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

	failOrCancel := func(err error, imageRef string) {
		e.cleanupCandidate(app, dep.ReleaseNumber, imageRef)
		if app.ActiveReleaseID == nil {
			_ = e.host.Remove(filepath.Join(e.cfg.TraefikDynamicDir, name.AppDynamicFilename(app.Name)))
		}
		now := nowUTC()
		status := "failed"
		msg := err.Error()
		if isCancelled() {
			status = "cancelled"
			msg = "cancelled by operator"
			e.builder.Logs().Append(depID, "==> Deployment cancelled; candidate resources cleaned up")
		} else {
			e.builder.Logs().Append(depID, "==> Deployment failed: "+msg)
		}
		_ = e.store.SetDeploymentBuildLogs(parentCtx, depID, e.builder.Logs().Snapshot(depID))
		_ = e.store.UpdateDeploymentStatus(parentCtx, depID, status, msg, &now)
		e.builder.Logs().Finish(depID)
	}

	// 1. Building
	_ = e.store.UpdateDeploymentStatus(parentCtx, depID, "building", "", nil)
	token, err := e.mintRepoToken(runCtx, app)
	if err != nil {
		failOrCancel(err, "")
		return
	}

	buildEnv, err := OpenEnvMap(e.cfg.MasterKey, "build_env_encrypted", app.BuildEnvEncrypted)
	if err != nil {
		failOrCancel(err, "")
		return
	}
	runtimeEnv, err := OpenEnvMap(e.cfg.MasterKey, "env_encrypted", app.EnvEncrypted)
	if err != nil {
		failOrCancel(err, "")
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
			failOrCancel(err, "")
			return
		}
	}
	buildCPU, _ := e.store.GetSetting(parentCtx, "build_cpu_limit")
	var buildTimeout time.Duration
	if ts, err := e.store.GetSetting(parentCtx, "build_timeout_sec"); err == nil && ts != "" {
		parsed, err := builder.ValidateTimeoutSec(ts)
		if err != nil {
			failOrCancel(err, "")
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
		failOrCancel(err, "")
		return
	}
	_ = e.store.SetDeploymentImageRef(parentCtx, depID, imageRef)

	// 2. Starting
	_ = e.store.UpdateDeploymentStatus(parentCtx, depID, "starting", "", nil)
	e.builder.Logs().Append(depID, fmt.Sprintf("==> Starting release r%d", dep.ReleaseNumber))

	envFilePath := ""
	if len(runtimeEnv) > 0 {
		envData, err := FormatEnvFile(runtimeEnv)
		if err != nil {
			failOrCancel(err, imageRef)
			return
		}
		envFilePath = filepath.Join(e.cfg.EnvDir, fmt.Sprintf("%s-r%d.env", app.Name, dep.ReleaseNumber))
		if err := e.host.WriteFileAtomic(envFilePath, envData, 0600); err != nil {
			failOrCancel(fmt.Errorf("writing env file: %w", err), imageRef)
			return
		}
	}

	var addCaps []string
	for _, c := range strings.Split(app.AddCapabilities, ",") {
		c = strings.TrimSpace(c)
		if c != "" {
			addCaps = append(addCaps, c)
		}
	}
	var healthPath, healthCmd string
	if app.HealthPath != nil {
		healthPath = *app.HealthPath
	}
	if app.HealthCmd != nil {
		healthCmd = *app.HealthCmd
	}

	spec := quadlet.ContainerSpec{
		AppName:       app.Name,
		ReleaseNumber: dep.ReleaseNumber,
		Image:         imageRef,
		Network:       name.BridgeNetworkName(),
		Port:          app.Port,
		Limits: quadlet.Limits{
			Memory:    app.MemoryLimit,
			CPUs:      app.CPULimit,
			PidsLimit: app.PidsLimit,
		},
		Health: quadlet.HealthSpec{
			Type:          app.HealthType,
			Path:          healthPath,
			Cmd:           healthCmd,
			TimeoutSec:    app.HealthTimeoutSec,
			NotifyHealthy: app.HealthType == "cmd",
		},
		ReadOnlyRoot:     app.ReadOnlyRoot,
		DropCapabilities: true,
		AddCapabilities:  addCaps,
		EnvFile:          envFilePath,
	}

	unitBytes, err := quadlet.RenderContainer(spec)
	if err != nil {
		failOrCancel(fmt.Errorf("rendering quadlet unit: %w", err), imageRef)
		return
	}

	unitName := name.AppQuadletFilename(app.Name, dep.ReleaseNumber)
	dryDir := filepath.Join(e.cfg.BuildsDir, ".dryrun-"+depID)
	defer func() { _ = e.host.RemoveAll(dryDir) }()
	if _, err := quadlet.NewDryRunValidator(e.host, "").ValidateUnit(runCtx, unitName, unitBytes, dryDir); err != nil {
		failOrCancel(fmt.Errorf("quadlet dry-run failed: %w", err), imageRef)
		return
	}

	unitPath := filepath.Join(e.cfg.QuadletDir, unitName)
	if err := e.host.WriteFileAtomic(unitPath, unitBytes, 0644); err != nil {
		failOrCancel(fmt.Errorf("writing quadlet unit: %w", err), imageRef)
		return
	}
	if _, err := e.host.RunCommand(runCtx, "systemctl", "--user", "daemon-reload"); err != nil {
		failOrCancel(fmt.Errorf("systemd daemon-reload: %w", err), imageRef)
		return
	}
	serviceName := name.AppServiceName(app.Name, dep.ReleaseNumber)
	if _, err := e.host.RunCommand(runCtx, "systemctl", "--user", "start", serviceName); err != nil {
		failOrCancel(fmt.Errorf("starting container service: %w", err), imageRef)
		return
	}

	// 3. Probing (healthy)
	_ = e.store.UpdateDeploymentStatus(parentCtx, depID, "probing", "", nil)
	if err := e.waitHealthy(runCtx, app, dep.ReleaseNumber, serviceName); err != nil {
		failOrCancel(err, imageRef)
		return
	}

	// 4. Route & Active (live)
	domains, err := e.store.ListDomainsByApp(parentCtx, app.ID)
	if err != nil {
		failOrCancel(fmt.Errorf("loading domains: %w", err), imageRef)
		return
	}
	domainSpecs := make([]traefik.DomainSpec, 0, len(domains))
	for _, d := range domains {
		var redir, pfx string
		if d.RedirectTo != nil {
			redir = *d.RedirectTo
		}
		if d.PathPrefix != nil {
			pfx = *d.PathPrefix
		}
		domainSpecs = append(domainSpecs, traefik.DomainSpec{
			Domain:      d.Domain,
			IsPrimary:   d.IsPrimary,
			RedirectTo:  redir,
			PathPrefix:  pfx,
			StripPrefix: d.StripPrefix,
		})
	}
	traefikBytes, err := traefik.RenderApp(traefik.AppSpec{
		AppName:       app.Name,
		ReleaseNumber: dep.ReleaseNumber,
		Port:          app.Port,
		Domains:       domainSpecs,
	})
	if err != nil {
		failOrCancel(fmt.Errorf("rendering traefik route: %w", err), imageRef)
		return
	}
	traefikPath := filepath.Join(e.cfg.TraefikDynamicDir, name.AppDynamicFilename(app.Name))
	if err := e.host.WriteFileAtomic(traefikPath, traefikBytes, 0600); err != nil {
		failOrCancel(fmt.Errorf("writing traefik route: %w", err), imageRef)
		return
	}

	now := nowUTC()
	_ = e.store.SetActiveRelease(parentCtx, app.ID, depID)
	_ = e.store.UpdateDeploymentStatus(parentCtx, depID, "active", "", &now)
	e.builder.Logs().Append(depID, "==> Deployment is live")
	_ = e.store.SetDeploymentBuildLogs(parentCtx, depID, e.builder.Logs().Snapshot(depID))
	e.builder.Logs().Finish(depID)
}

func (e *Engine) waitHealthy(ctx context.Context, app *store.App, relNum int, serviceName string) error {
	timeout := time.Duration(app.HealthTimeoutSec) * time.Second
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	deadline := time.Now().Add(timeout)

	switch app.HealthType {
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
		if app.HealthPath != nil && *app.HealthPath != "" {
			healthPath = *app.HealthPath
		}
		if !strings.HasPrefix(healthPath, "/") {
			healthPath = "/" + healthPath
		}
		probeURL := fmt.Sprintf("http://%s:%d%s", name.AppContainerName(app.Name, relNum), app.Port, healthPath)
		needed := app.HealthConsecutive
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

	serviceName := name.AppServiceName(app.Name, relNum)
	_, _ = e.host.RunCommand(ctx, "systemctl", "--user", "stop", serviceName)
	_ = e.host.Remove(filepath.Join(e.cfg.QuadletDir, name.AppQuadletFilename(app.Name, relNum)))
	_, _ = e.host.RunCommand(ctx, "systemctl", "--user", "daemon-reload")
	_ = e.host.Remove(filepath.Join(e.cfg.EnvDir, fmt.Sprintf("%s-r%d.env", app.Name, relNum)))
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
		e.cleanupCandidate(app, d.ReleaseNumber, d.ImageRef)
		e.builder.Logs().Delete(d.ID)
	}
	_ = e.host.Remove(filepath.Join(e.cfg.TraefikDynamicDir, name.AppDynamicFilename(app.Name)))
	_ = e.host.RemoveAll(filepath.Join(e.cfg.MirrorsDir, app.Name+".git"))

	return e.store.DeleteApp(ctx, appID)
}

func sealJSON(key []byte, col string, v any) (string, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	return crypto.Encrypt(key, col, raw)
}

func nowUTC() string {
	return time.Now().UTC().Format(time.RFC3339)
}

func randomHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
