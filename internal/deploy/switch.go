package deploy

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"dermaga/internal/crypto"
	"dermaga/internal/host"
	"dermaga/internal/name"
	"dermaga/internal/quadlet"
	"dermaga/internal/store"
	"dermaga/internal/traefik"
)

// RetainedReleases is how many earlier live releases keep their image for rollback (DEP-4).
const RetainedReleases = 5

// DefaultDrainGrace is how long a release that lost its traffic keeps running before it is
// stopped, after a normal switch and after one recovered at startup (PRD DEP-1 grace).
const DefaultDrainGrace = 10 * time.Second

// ErrBuildRunning refuses a rollback while the same app is building (owner decision; the API
// shows it as "Cancel the running build first").
var ErrBuildRunning = errors.New("cancel the running build first")

// ErrSwitchRunning refuses a rollback while another release of the app is starting or switching.
var ErrSwitchRunning = errors.New("another release of this app is starting or switching traffic")

// ---- memory check ----

// parseMemory turns an app memory limit (`^[1-9][0-9]*[kKmMgG]?$`, quadlet.validate) into bytes.
func parseMemory(s string) (int64, error) {
	s = strings.TrimSpace(s)
	mult := int64(1)
	switch strings.ToLower(s[len(s)-1:]) {
	case "k":
		mult = 1 << 10
	case "m":
		mult = 1 << 20
	case "g":
		mult = 1 << 30
	}
	if mult > 1 {
		s = s[:len(s)-1]
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil || n <= 0 {
		return 0, fmt.Errorf("invalid memory limit %q", s)
	}
	return n * mult, nil
}

// MemoryCheck says whether the host can start a release with the given memory limit next to
// everything already running (owner decision: refuse when MemAvailable < the candidate's limit).
type MemoryCheck struct {
	AvailableBytes int64 `json:"available_bytes"`
	NeededBytes    int64 `json:"needed_bytes"`
	OK             bool  `json:"ok"`
}

// CheckMemory reads MemAvailable from /proc/meminfo and compares it with limit.
func (e *Engine) CheckMemory(limit string) (MemoryCheck, error) {
	need, err := parseMemory(limit)
	if err != nil {
		return MemoryCheck{}, err
	}
	data, err := e.host.ReadFile(e.cfg.MeminfoPath)
	if err != nil {
		return MemoryCheck{}, fmt.Errorf("reading %s: %w", e.cfg.MeminfoPath, err)
	}
	sc := bufio.NewScanner(bytes.NewReader(data))
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) >= 2 && f[0] == "MemAvailable:" {
			kb, err := strconv.ParseInt(f[1], 10, 64)
			if err != nil {
				break
			}
			avail := kb * 1024
			return MemoryCheck{AvailableBytes: avail, NeededBytes: need, OK: avail >= need}, nil
		}
	}
	return MemoryCheck{}, fmt.Errorf("no MemAvailable line in %s", e.cfg.MeminfoPath)
}

func (e *Engine) requireMemory(limit string) error {
	mc, err := e.CheckMemory(limit)
	if err != nil {
		return err
	}
	if !mc.OK {
		return fmt.Errorf("not enough free memory to start the new release next to the live one: %d MiB available, its memory limit is %d MiB. The live release keeps serving; free memory or lower the app's memory limit, then redeploy",
			mc.AvailableBytes>>20, mc.NeededBytes>>20)
	}
	return nil
}

// ---- per-app lock: one release of an app starts or switches at a time ----

func (e *Engine) appLock(appID string) chan struct{} {
	e.mu.Lock()
	defer e.mu.Unlock()
	l, ok := e.appLocks[appID]
	if !ok {
		l = make(chan struct{}, 1)
		e.appLocks[appID] = l
	}
	return l
}

// ---- routes (route.go builds and writes them) ----

// waitVerified polls Traefik until a request carrying token gets it back (the verify router of the
// file just written is loaded) from a reachable release, or timeout passes.
func (e *Engine) waitVerified(ctx context.Context, primary, prefix, token string, timeout time.Duration) error {
	path := prefix
	if path == "" {
		path = "/"
	}
	deadline := time.Now().Add(timeout)
	var last error
	for {
		ok, err := e.cfg.VerifyRoute(ctx, primary, path, token)
		if ok {
			return nil
		}
		last = err
		if time.Now().After(deadline) {
			if last != nil {
				//lint:ignore ST1005 Traefik is a proper noun; this text is the deployment's failure reason
				return fmt.Errorf("Traefik did not answer through the verify router within %s: %w", timeout, last)
			}
			//lint:ignore ST1005 Traefik is a proper noun; this text is the deployment's failure reason
			return fmt.Errorf("Traefik did not answer through the verify router within %s", timeout)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(e.cfg.ProbeInterval):
		}
	}
}

// verifyThroughTraefik is the real VerifyRoute: `dermaga probe` in the rootless netns (like the
// health probe, DEC-03) sends host+path with the token to Traefik's internal plain-HTTP verify
// entrypoint. Not 443: with sniStrict, Traefik refuses the TLS handshake for a domain whose
// certificate is still pending (DEC-33).
func (e *Engine) verifyThroughTraefik(ctx context.Context, host, path, token string) (bool, error) {
	u := fmt.Sprintf("http://%s:%d%s", quadlet.TraefikContainerName, traefik.VerifyPort, path)
	out, err := e.host.RunCommand(ctx, "podman", "unshare", "--rootless-netns", e.cfg.BinaryPath, "probe", "-host", host, "-verify-token", token, u)
	if err != nil {
		// The probe's last line is its reason ("Error: ..."); the rest is the command wrapper.
		lines := strings.Split(strings.TrimSpace(string(out)), "\n")
		if reason := strings.TrimPrefix(lines[len(lines)-1], "Error: "); reason != "" {
			return false, errors.New(reason)
		}
		return false, err
	}
	return true, nil
}

// CheckVerifyResponse decides a verify request (used by `dermaga probe -verify-token`): the token
// must come back, which only the verify router of the file just written adds, and Traefik must
// have reached the candidate (any status but 502/503/504; the app may answer 401/404 itself).
func CheckVerifyResponse(resp *http.Response, token string) error {
	if resp.Header.Get(traefik.VerifyHeader) != token {
		return errors.New("verify router not loaded yet")
	}
	if resp.StatusCode == http.StatusBadGateway || resp.StatusCode == http.StatusServiceUnavailable || resp.StatusCode == http.StatusGatewayTimeout {
		//lint:ignore ST1005 Traefik is a proper noun; this text is the deployment's failure reason
		return fmt.Errorf("Traefik could not reach the new release (HTTP %d)", resp.StatusCode)
	}
	return nil
}

// switchTraffic moves the app's route from release `live` (livePort) to the candidate, proving
// each step through Traefik (DEP-1, DEC-33):
//  1. live + verify router → candidate (token A); wait for A: Traefik loaded the candidate service;
//  2. main → candidate + verify router (token B) in one file; wait for B: since Traefik loads a file
//     as a whole, the main router has moved too;
//  3. main → candidate only.
//
// On any failure before step 3 the live-only route is written back.
func (e *Engine) switchTraffic(ctx context.Context, app *store.App, live, livePort, cand, candPort int, logf func(string)) error {
	restore := func(cause error) error {
		if _, _, err := e.writeRoute(context.WithoutCancel(ctx), app, live, livePort, nil); err != nil {
			return fmt.Errorf("%v; restoring the previous route also failed: %w", cause, err)
		}
		return cause
	}
	tokA, tokB := randomHex(16), randomHex(16)
	primary, prefix, err := e.writeRoute(ctx, app, live, livePort, &traefik.SwitchSpec{CandidateReleaseNumber: cand, CandidatePort: candPort, VerifyToken: tokA})
	if err != nil {
		return restore(err)
	}
	if err := e.waitVerified(ctx, primary, prefix, tokA, e.cfg.SwitchTimeout); err != nil {
		return restore(err)
	}
	logf(fmt.Sprintf("==> Traefik reaches r%d through the verify router", cand))
	if _, _, err := e.writeRoute(ctx, app, cand, candPort, &traefik.SwitchSpec{CandidateReleaseNumber: cand, CandidatePort: candPort, VerifyToken: tokB}); err != nil {
		return restore(err)
	}
	if err := e.waitVerified(ctx, primary, prefix, tokB, e.cfg.SwitchTimeout); err != nil {
		return restore(err)
	}
	logf(fmt.Sprintf("==> Traffic switched from r%d to r%d", live, cand))
	if _, _, err := e.writeRoute(ctx, app, cand, candPort, nil); err != nil {
		// Traffic already flows to the candidate; only the verify router is left. The startup
		// reconciler removes it at the latest; the release counts as switched.
		logf("==> Warning: could not remove the verify router: " + err.Error())
	}
	return nil
}

// stopRelease stops a release that no longer serves and removes its unit and env file. The image
// stays for rollback until retention removes it.
func (e *Engine) stopRelease(ctx context.Context, appName string, release int) {
	_ = RemoveReleaseUnit(ctx, e.host, e.cfg.QuadletDir, e.cfg.EnvDir, appName, release)
	_, _ = e.host.RunCommand(ctx, "systemctl", "--user", "daemon-reload")
}

// RemoveReleaseUnit stops a release's service and removes its unit and its env file (runtime
// secrets): the cleanup shared by the engine and the startup reconciler, so neither leaves a
// per-release file behind. The image is kept (retention decides). The caller reloads systemd.
func RemoveReleaseUnit(ctx context.Context, h host.Host, quadletDir, envDir, appName string, release int) error {
	_, _ = h.RunCommand(ctx, "systemctl", "--user", "stop", name.AppServiceName(appName, release))
	if envDir != "" {
		_ = h.Remove(EnvFilePath(envDir, appName, release))
	}
	return h.Remove(filepath.Join(quadletDir, name.AppQuadletFilename(appName, release)))
}

// RetainedSet returns the releases that keep their image: the live one plus the newest
// RetainedReleases releases that were live before (DEP-4). deps must be newest first.
func RetainedSet(deps []store.Deployment, activeID string) []store.Deployment {
	var out []store.Deployment
	previous := 0
	for _, d := range deps {
		switch {
		case d.ID == activeID:
			out = append(out, d)
		case (d.Status == "inactive" || d.Status == "rolled_back") && previous < RetainedReleases:
			out = append(out, d)
			previous++
		}
	}
	return out
}

// applyRetention removes images of releases outside the retained set (never an image a retained
// release uses: rollbacks share their source's image) and env versions nothing needs any more.
func (e *Engine) applyRetention(ctx context.Context, app *store.App, activeID string) {
	deps, err := e.store.ListDeploymentsByApp(ctx, app.ID)
	if err != nil {
		return
	}
	keepImg := map[string]bool{}
	var keepEnv []string
	for _, d := range RetainedSet(deps, activeID) {
		keepImg[d.ImageRef] = true
		if d.EnvVersionID != nil {
			keepEnv = append(keepEnv, *d.EnvVersionID)
		}
	}
	for _, d := range deps {
		switch d.Status {
		case "queued", "building", "starting", "probing", "switching":
			if d.EnvVersionID != nil {
				keepEnv = append(keepEnv, *d.EnvVersionID) // still needed to start
			}
			keepImg[d.ImageRef] = true
		}
	}
	removed := map[string]bool{}
	for _, d := range deps {
		if d.ImageRef == "" || keepImg[d.ImageRef] || removed[d.ImageRef] || (d.Status != "inactive" && d.Status != "rolled_back") {
			continue
		}
		removed[d.ImageRef] = true
		_, _ = e.host.RunCommand(ctx, "podman", "rmi", "--ignore", d.ImageRef)
	}
	_, _ = e.store.DeleteEnvVersionsExcept(ctx, app.ID, keepEnv)
}

// ---- env ----

func (e *Engine) releaseEnv(ctx context.Context, dep *store.Deployment) (map[string]string, error) {
	if dep.EnvVersionID == nil {
		return nil, fmt.Errorf("release r%d has no stored environment", dep.ReleaseNumber)
	}
	v, err := e.store.GetEnvVersion(ctx, *dep.EnvVersionID)
	if err != nil {
		return nil, fmt.Errorf("loading the environment of r%d: %w", dep.ReleaseNumber, err)
	}
	return OpenEnvMap(e.cfg.MasterKey, "env_encrypted", v.EnvEncrypted)
}

// convertLegacySnapshots moves pre-0005 release snapshots (env + config in one encrypted blob)
// into env versions plus a plain runtime config (migration 0005, DEC-32).
func (e *Engine) convertLegacySnapshots(ctx context.Context) error {
	deps, err := e.store.ListLegacySnapshotDeployments(ctx)
	if err != nil {
		return err
	}
	for _, d := range deps {
		raw, err := crypto.Decrypt(e.cfg.MasterKey, "env_snapshot_encrypted", d.EnvSnapshotEncrypted)
		if err != nil {
			return fmt.Errorf("decrypting the snapshot of release %s: %w", d.ID, err)
		}
		var snap struct {
			Env map[string]string `json:"env"`
			RuntimeConfig
		}
		if err := json.Unmarshal(raw, &snap); err != nil {
			return fmt.Errorf("reading the snapshot of release %s: %w", d.ID, err)
		}
		app, err := e.store.GetApp(ctx, d.AppID)
		if err != nil {
			return err
		}
		rc := RuntimeConfigOf(app) // limits were not part of the old snapshot
		rc.Port, rc.Command = snap.Port, snap.Command
		rc.HealthType, rc.HealthPath, rc.HealthCmd, rc.HealthTimeoutSec = snap.HealthType, snap.HealthPath, snap.HealthCmd, snap.HealthTimeoutSec
		sealed, err := SealEnvMap(e.cfg.MasterKey, "env_encrypted", snap.Env)
		if err != nil {
			return err
		}
		if err := e.store.AddLegacyEnvVersion(ctx, d.ID, d.AppID, sealed, rc.json()); err != nil {
			return err
		}
	}
	return nil
}
