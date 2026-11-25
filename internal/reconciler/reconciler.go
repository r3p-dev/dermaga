// Package reconciler implements DEP-10 startup reconciliation for dermaga:
// ensuring exactly one live release and one route per active app, repairing
// missing infrastructure or unit files, starting stopped live units, cleaning
// orphaned files and leftover verify routers, and reporting foreign files without touching them.
package reconciler

import (
	"context"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"dermaga/internal/host"
	"dermaga/internal/name"
	"dermaga/internal/quadlet"
	"dermaga/internal/store"
	"dermaga/internal/traefik"
)

// Config configures file directories and safety parameters for reconciliation.
type Config struct {
	QuadletDir        string
	TraefikDynamicDir string
	AllowEmptyDBPrune bool
	// UserUnitDir (~/.config/systemd/user) and TraefikDir (~/.local/share/dermaga/traefik) hold
	// the Traefik sockets and static config. When either is empty the Traefik step is skipped.
	UserUnitDir string
	TraefikDir  string
	// HookRelayRootfsDir, BinaryPath, and SocketDir configure the rootless hook-relay container (DEC-30).
	HookRelayRootfsDir string
	BinaryPath         string
	SocketDir          string
}

// Report records the outcome of a reconciliation pass (DEP-10).
type Report struct {
	OrphansRemoved       []string `json:"orphans_removed"`
	FilesReRendered      []string `json:"files_re_rendered"`
	UnitsStarted         []string `json:"units_started"`
	VerifyRoutersCleaned []string `json:"verify_routers_cleaned"`
	ForeignFilesReported []string `json:"foreign_files_reported"`
	SafeDefaultTriggered bool     `json:"safe_default_triggered,omitempty"`
}

// Reconciler inspects the persistent store and host filesystem to achieve self-healing.
type Reconciler struct {
	host  host.Host
	store *store.Store
	cfg   Config
}

// New initializes a new Reconciler.
func New(h host.Host, s *store.Store, cfg Config) *Reconciler {
	return &Reconciler{
		host:  h,
		store: s,
		cfg:   cfg,
	}
}

// Reconcile executes a full reconciliation cycle per DEP-10.
func (r *Reconciler) Reconcile(ctx context.Context) (*Report, error) {
	report := &Report{
		OrphansRemoved:       []string{},
		FilesReRendered:      []string{},
		UnitsStarted:         []string{},
		VerifyRoutersCleaned: []string{},
		ForeignFilesReported: []string{},
	}

	// Ensure directories exist
	if err := r.host.MkdirAll(r.cfg.QuadletDir, 0755); err != nil {
		return nil, fmt.Errorf("ensuring quadlet dir %q: %w", r.cfg.QuadletDir, err)
	}
	if err := r.host.MkdirAll(r.cfg.TraefikDynamicDir, 0755); err != nil {
		return nil, fmt.Errorf("ensuring traefik dynamic dir %q: %w", r.cfg.TraefikDynamicDir, err)
	}

	// 1. Fetch active apps and their active deployments from SQLite
	activeApps, err := r.store.GetActiveApps(ctx)
	if err != nil {
		return nil, fmt.Errorf("fetching active apps from store: %w", err)
	}

	// Maps for fast lookup
	// activeUnits: app-<name>-r<num>.container -> ActiveAppRecord
	activeUnits := make(map[string]store.ActiveAppRecord)
	// activeAppNames: <name> -> ActiveAppRecord
	activeAppNames := make(map[string]store.ActiveAppRecord)
	for _, rec := range activeApps {
		unitFilename := fmt.Sprintf("%s%s-r%d.container", name.AppPrefix, rec.App.Name, rec.Deployment.ReleaseNumber)
		activeUnits[unitFilename] = rec
		activeAppNames[rec.App.Name] = rec
	}

	daemonReloadNeeded := false

	// 2. Reconcile Quadlet Directory
	quadletEntries, err := r.host.ReadDir(r.cfg.QuadletDir)
	if err != nil {
		return nil, fmt.Errorf("reading quadlet dir: %w", err)
	}

	foundUnitsOnDisk := make(map[string]bool)
	hasNetworkUnit := false

	for _, entry := range quadletEntries {
		if entry.IsDir() {
			continue
		}
		fname := entry.Name()
		filePath := filepath.Join(r.cfg.QuadletDir, fname)

		if fname == fmt.Sprintf("%s.network", name.BridgeNetworkName()) {
			content, err := r.host.ReadFile(filePath)
			if err == nil && quadlet.HasNetworkMarker(string(content), name.BridgeNetworkName()) {
				hasNetworkUnit = true
				continue
			}
			// web.network exists without dermaga marker: foreign!
			report.ForeignFilesReported = append(report.ForeignFilesReported, fname)
			continue
		}

		if fname == quadlet.TraefikContainerFile && r.traefikEnabled() {
			continue // handled by ensureTraefik
		}
		if fname == quadlet.HookRelayContainerFile && r.hookRelayEnabled() {
			continue // handled by ensureHookRelay
		}

		if strings.HasPrefix(fname, name.AppPrefix) && strings.HasSuffix(fname, ".container") {
			appName, relNum, ok := parseContainerFilename(fname)
			if !ok {
				report.ForeignFilesReported = append(report.ForeignFilesReported, fname)
				continue
			}
			content, err := r.host.ReadFile(filePath)
			if err != nil || !quadlet.HasContainerMarker(string(content), appName) {
				// Foreign file: matches name pattern but lacks explicit ownership marker!
				// DEP-10: reported, never touched.
				report.ForeignFilesReported = append(report.ForeignFilesReported, fname)
				continue
			}

			// This is an authentic dermaga-managed container unit
			if _, isActive := activeUnits[fname]; isActive {
				foundUnitsOnDisk[fname] = true
			} else {
				// Candidate orphan unit!
				// Safe default: if DB has 0 active apps, do NOT prune managed units!
				if len(activeApps) == 0 && !r.cfg.AllowEmptyDBPrune {
					report.SafeDefaultTriggered = true
					continue
				}
				serviceName := name.AppServiceName(appName, relNum)
				_, _ = r.host.RunCommand(ctx, "systemctl", "--user", "stop", serviceName)
				if err := r.host.Remove(filePath); err == nil {
					report.OrphansRemoved = append(report.OrphansRemoved, fname)
					daemonReloadNeeded = true
				}
			}
		} else {
			// Foreign file! Never touch, report per DEP-10
			report.ForeignFilesReported = append(report.ForeignFilesReported, fname)
		}
	}

	// 2a. Re-render missing web.network if absent
	if !hasNetworkUnit {
		netSpec := quadlet.NetworkSpec{
			NetworkName: name.BridgeNetworkName(),
		}
		netData, err := quadlet.RenderNetwork(netSpec)
		if err != nil {
			return nil, fmt.Errorf("rendering network unit: %w", err)
		}
		netPath := filepath.Join(r.cfg.QuadletDir, fmt.Sprintf("%s.network", name.BridgeNetworkName()))
		if err := r.host.WriteFileAtomic(netPath, netData, 0644); err != nil {
			return nil, fmt.Errorf("writing network unit: %w", err)
		}
		report.FilesReRendered = append(report.FilesReRendered, fmt.Sprintf("%s.network", name.BridgeNetworkName()))
		daemonReloadNeeded = true
	}

	// 2b. Re-render missing Quadlet units for active apps
	for unitFilename, rec := range activeUnits {
		if !foundUnitsOnDisk[unitFilename] {
			cSpec := quadlet.ContainerSpec{
				AppName:          rec.App.Name,
				ReleaseNumber:    rec.Deployment.ReleaseNumber,
				Image:            fmt.Sprintf("localhost/%s:r%d", rec.App.Name, rec.Deployment.ReleaseNumber),
				Network:          name.BridgeNetworkName(),
				Port:             rec.App.Port,
				DropCapabilities: true,
			}
			if rec.App.HealthType == "cmd" && rec.App.HealthCmd != nil && *rec.App.HealthCmd != "" {
				cSpec.Health = quadlet.HealthSpec{
					Type:          "cmd",
					Cmd:           *rec.App.HealthCmd,
					TimeoutSec:    rec.App.HealthTimeoutSec,
					NotifyHealthy: true,
				}
			} else if rec.App.HealthType == "http" && rec.App.HealthPath != nil && *rec.App.HealthPath != "" {
				cSpec.Health = quadlet.HealthSpec{
					Type:          "http",
					Path:          *rec.App.HealthPath,
					TimeoutSec:    rec.App.HealthTimeoutSec,
					NotifyHealthy: true,
				}
			} else {
				cSpec.Health = quadlet.HealthSpec{Type: "none"}
			}

			cData, err := quadlet.RenderContainer(cSpec)
			if err != nil {
				return nil, fmt.Errorf("rendering container unit for %s: %w", rec.App.Name, err)
			}
			cPath := filepath.Join(r.cfg.QuadletDir, unitFilename)
			if err := r.host.WriteFileAtomic(cPath, cData, 0644); err != nil {
				return nil, fmt.Errorf("writing container unit for %s: %w", rec.App.Name, err)
			}
			report.FilesReRendered = append(report.FilesReRendered, unitFilename)
			daemonReloadNeeded = true
		}
	}

	// 2b'. Traefik units and static config (HOST-6)
	traefikOwned, traefikChanged := false, false
	if r.traefikEnabled() {
		traefikOwned, traefikChanged, err = r.ensureTraefik(report)
		if err != nil {
			return nil, err
		}
		if traefikChanged {
			daemonReloadNeeded = true
		}
	}

	// 2b2. Ensure hook-relay unit when GitHub App and hook_domain are both configured (DEC-30)
	relayOwned, relayDesired, relayChanged, err := r.ensureHookRelay(ctx, report)
	if err != nil {
		return nil, err
	}
	if relayChanged {
		daemonReloadNeeded = true
	}

	// 2c. Reload systemd daemon if Quadlet units changed
	if daemonReloadNeeded {
		if _, err := r.host.RunCommand(ctx, "systemctl", "--user", "daemon-reload"); err != nil {
			return nil, fmt.Errorf("reloading systemd user daemon: %w", err)
		}
	}

	if relayOwned && relayDesired {
		action := "start"
		if relayChanged {
			action = "restart"
		}
		if _, err := r.host.RunCommand(ctx, "systemctl", "--user", action, "--no-block", quadlet.HookRelayServiceName); err != nil {
			return nil, fmt.Errorf("starting hook-relay: %w", err)
		}
	}

	// 2d. Start Traefik without blocking: the first start pulls the image and the daemon must
	// not wait on systemctl start (CONVENTIONS §4).
	if traefikOwned {
		if _, err := r.host.RunCommand(ctx, "systemctl", "--user", "enable", "--now",
			quadlet.TraefikHTTPSocketFile, quadlet.TraefikHTTPSSocketFile); err != nil {
			return nil, fmt.Errorf("enabling traefik sockets: %w", err)
		}
		action := "start"
		if traefikChanged {
			action = "restart"
		}
		if _, err := r.host.RunCommand(ctx, "systemctl", "--user", action, "--no-block", quadlet.TraefikServiceName); err != nil {
			return nil, fmt.Errorf("starting traefik: %w", err)
		}
	}

	// 3. Start stopped live units
	for _, rec := range activeUnits {
		serviceName := name.AppServiceName(rec.App.Name, rec.Deployment.ReleaseNumber)
		out, err := r.host.RunCommand(ctx, "systemctl", "--user", "is-active", serviceName)
		status := strings.TrimSpace(string(out))
		if err != nil || status != "active" {
			if _, startErr := r.host.RunCommand(ctx, "systemctl", "--user", "start", serviceName); startErr != nil {
				return nil, fmt.Errorf("starting live unit %s: %w", serviceName, startErr)
			}
			report.UnitsStarted = append(report.UnitsStarted, serviceName)
		}
	}

	// 4. Reconcile Traefik Dynamic Directory
	traefikEntries, err := r.host.ReadDir(r.cfg.TraefikDynamicDir)
	if err != nil {
		return nil, fmt.Errorf("reading traefik dynamic dir: %w", err)
	}

	foundConfigsOnDisk := make(map[string]bool)
	hasGlobalConfig := false
	hasHookDomainConfig := false

	for _, entry := range traefikEntries {
		if entry.IsDir() {
			continue
		}
		fname := entry.Name()
		filePath := filepath.Join(r.cfg.TraefikDynamicDir, fname)

		if fname == "00-global.yml" {
			content, err := r.host.ReadFile(filePath)
			if err == nil && traefik.HasGlobalMarker(string(content)) {
				hasGlobalConfig = true
				continue
			}
			// Foreign global config!
			report.ForeignFilesReported = append(report.ForeignFilesReported, fname)
			continue
		}

		if fname == traefik.HookDomainFileName {
			content, err := r.host.ReadFile(filePath)
			if err == nil && traefik.HasHookDomainMarker(string(content)) {
				hasHookDomainConfig = true
				continue
			}
			report.ForeignFilesReported = append(report.ForeignFilesReported, fname)
			continue
		}

		// Leftover verify router check: app-*-verify.yml
		if strings.HasPrefix(fname, name.AppPrefix) && strings.HasSuffix(fname, "-verify.yml") {
			appName := strings.TrimSuffix(strings.TrimPrefix(fname, name.AppPrefix), "-verify.yml")
			content, err := r.host.ReadFile(filePath)
			if err != nil || !traefik.HasAppMarker(string(content), appName) {
				// Foreign file matching name pattern without ownership marker!
				report.ForeignFilesReported = append(report.ForeignFilesReported, fname)
				continue
			}
			if err := r.host.Remove(filePath); err == nil {
				report.VerifyRoutersCleaned = append(report.VerifyRoutersCleaned, fname)
			}
			continue
		}

		// App routing config check: app-<name>.yml
		if strings.HasPrefix(fname, name.AppPrefix) && strings.HasSuffix(fname, ".yml") {
			appName := strings.TrimSuffix(strings.TrimPrefix(fname, name.AppPrefix), ".yml")
			content, err := r.host.ReadFile(filePath)
			if err != nil || !traefik.HasAppMarker(string(content), appName) {
				// Foreign file matching name pattern without ownership marker!
				report.ForeignFilesReported = append(report.ForeignFilesReported, fname)
				continue
			}
			if _, isActive := activeAppNames[appName]; isActive {
				foundConfigsOnDisk[fname] = true
			} else {
				// Candidate orphan config!
				// Safe default: if DB has 0 active apps, do NOT prune managed configs!
				if len(activeApps) == 0 && !r.cfg.AllowEmptyDBPrune {
					report.SafeDefaultTriggered = true
					continue
				}
				if err := r.host.Remove(filePath); err == nil {
					report.OrphansRemoved = append(report.OrphansRemoved, fname)
				}
			}
			continue
		}

		// Foreign file! Never touch, report per DEP-10
		report.ForeignFilesReported = append(report.ForeignFilesReported, fname)
	}

	// 4a. Re-render missing 00-global.yml if absent
	if !hasGlobalConfig {
		globalData, err := traefik.RenderGlobal(traefik.GlobalSpec{})
		if err != nil {
			return nil, fmt.Errorf("rendering global dynamic config: %w", err)
		}
		globalPath := filepath.Join(r.cfg.TraefikDynamicDir, "00-global.yml")
		if err := r.host.WriteFileAtomic(globalPath, globalData, 0644); err != nil {
			return nil, fmt.Errorf("writing global dynamic config: %w", err)
		}
		report.FilesReRendered = append(report.FilesReRendered, "00-global.yml")
	}

	// 4a2. Re-render missing 01-hook-domain.yml when hook_domain is configured (TLS-5, APP-2)
	if !hasHookDomainConfig {
		if hookDomain, err := r.store.GetSetting(ctx, "hook_domain"); err == nil && hookDomain != "" {
			if err := r.EnsureHookDomain(ctx, hookDomain); err != nil {
				return nil, err
			}
			report.FilesReRendered = append(report.FilesReRendered, traefik.HookDomainFileName)
		}
	}

	// 4b. Re-render missing Traefik dynamic configs for active apps
	for appName, rec := range activeAppNames {
		configFilename := fmt.Sprintf("%s%s.yml", name.AppPrefix, appName)
		if !foundConfigsOnDisk[configFilename] {
			var domSpecs []traefik.DomainSpec
			for _, d := range rec.Domains {
				redir := ""
				if d.RedirectTo != nil {
					redir = *d.RedirectTo
				}
				pfx := ""
				if d.PathPrefix != nil {
					pfx = *d.PathPrefix
				}
				domSpecs = append(domSpecs, traefik.DomainSpec{
					Domain:      d.Domain,
					IsPrimary:   d.IsPrimary,
					RedirectTo:  redir,
					PathPrefix:  pfx,
					StripPrefix: d.StripPrefix,
				})
			}

			appSpec := traefik.AppSpec{
				AppName:       rec.App.Name,
				ReleaseNumber: rec.Deployment.ReleaseNumber,
				Port:          rec.App.Port,
				Domains:       domSpecs,
			}
			appData, err := traefik.RenderApp(appSpec)
			if err != nil {
				return nil, fmt.Errorf("rendering traefik config for %s: %w", appName, err)
			}
			cfgPath := filepath.Join(r.cfg.TraefikDynamicDir, configFilename)
			if err := r.host.WriteFileAtomic(cfgPath, appData, 0644); err != nil {
				return nil, fmt.Errorf("writing traefik config for %s: %w", appName, err)
			}
			report.FilesReRendered = append(report.FilesReRendered, configFilename)
		}
	}

	return report, nil
}

// EnsureHookDomain renders and atomically writes 01-hook-domain.yml for the given domain (TLS-5, APP-2).
func (r *Reconciler) EnsureHookDomain(ctx context.Context, domain string) error {
	if r.cfg.TraefikDynamicDir == "" {
		return nil
	}
	hookPath := filepath.Join(r.cfg.TraefikDynamicDir, traefik.HookDomainFileName)
	if existing, err := r.host.ReadFile(hookPath); err == nil && !traefik.HasHookDomainMarker(string(existing)) {
		return fmt.Errorf("refusing to overwrite foreign %s without dermaga marker", traefik.HookDomainFileName)
	}
	backendURL := ""
	if r.githubConnected(ctx) && r.hookRelayEnabled() {
		backendURL = quadlet.HookRelayBackendURL
	}
	data, err := traefik.RenderHookDomain(domain, backendURL)
	if err != nil {
		return fmt.Errorf("rendering hook domain config: %w", err)
	}
	if err := r.host.WriteFileAtomic(hookPath, data, 0644); err != nil {
		return fmt.Errorf("writing hook domain config: %w", err)
	}
	return nil
}

// EnsureGitHubRelay installs/starts hook-relay and updates 01-hook-domain.yml to route to it (GHA-1, DEC-30).
func (r *Reconciler) EnsureGitHubRelay(ctx context.Context, hookDomain string) error {
	if r.hookRelayEnabled() {
		report := &Report{}
		owned, desired, changed, err := r.ensureHookRelay(ctx, report)
		if err != nil {
			return err
		}
		if changed {
			if _, err := r.host.RunCommand(ctx, "systemctl", "--user", "daemon-reload"); err != nil {
				return fmt.Errorf("reloading systemd user daemon: %w", err)
			}
		}
		if owned && desired {
			action := "start"
			if changed {
				action = "restart"
			}
			if _, err := r.host.RunCommand(ctx, "systemctl", "--user", action, "--no-block", quadlet.HookRelayServiceName); err != nil {
				return fmt.Errorf("starting hook-relay: %w", err)
			}
		}
	}
	if hookDomain != "" {
		return r.EnsureHookDomain(ctx, hookDomain)
	}
	return nil
}

// RemoveGitHubRelay stops/removes hook-relay.container and removes 01-hook-domain.yml on Disconnect (GHA-6).
func (r *Reconciler) RemoveGitHubRelay(ctx context.Context) error {
	if r.hookRelayEnabled() {
		relayPath := filepath.Join(r.cfg.QuadletDir, quadlet.HookRelayContainerFile)
		if existing, err := r.host.ReadFile(relayPath); err == nil {
			if !quadlet.HasUnitMarker(string(existing), quadlet.HookRelayContainerFile) {
				return fmt.Errorf("refusing to remove foreign %s without dermaga marker", quadlet.HookRelayContainerFile)
			}
			_, _ = r.host.RunCommand(ctx, "systemctl", "--user", "stop", quadlet.HookRelayServiceName)
			if err := r.host.Remove(relayPath); err != nil {
				return fmt.Errorf("removing %s: %w", quadlet.HookRelayContainerFile, err)
			}
			_, _ = r.host.RunCommand(ctx, "systemctl", "--user", "daemon-reload")
		}
	}
	if r.cfg.TraefikDynamicDir != "" {
		hookPath := filepath.Join(r.cfg.TraefikDynamicDir, traefik.HookDomainFileName)
		if existing, err := r.host.ReadFile(hookPath); err == nil {
			if !traefik.HasHookDomainMarker(string(existing)) {
				return fmt.Errorf("refusing to remove foreign %s without dermaga marker", traefik.HookDomainFileName)
			}
			if err := r.host.Remove(hookPath); err != nil {
				return fmt.Errorf("removing %s: %w", traefik.HookDomainFileName, err)
			}
		}
	}
	return nil
}

func (r *Reconciler) hookRelayEnabled() bool {
	return r.cfg.QuadletDir != "" && r.cfg.HookRelayRootfsDir != "" && r.cfg.BinaryPath != "" && r.cfg.SocketDir != ""
}

func (r *Reconciler) githubConnected(ctx context.Context) bool {
	if r.store == nil {
		return false
	}
	appID, err := r.store.GetSetting(ctx, "github_app_id")
	return err == nil && appID != ""
}

func (r *Reconciler) ensureHookRelay(ctx context.Context, report *Report) (owned, desired, changed bool, err error) {
	if !r.hookRelayEnabled() {
		return false, false, false, nil
	}
	hookDomain, _ := r.store.GetSetting(ctx, "hook_domain")
	desired = r.githubConnected(ctx) && strings.TrimSpace(hookDomain) != ""
	relayPath := filepath.Join(r.cfg.QuadletDir, quadlet.HookRelayContainerFile)

	if !desired {
		if existing, err := r.host.ReadFile(relayPath); err == nil {
			if !quadlet.HasUnitMarker(string(existing), quadlet.HookRelayContainerFile) {
				report.ForeignFilesReported = append(report.ForeignFilesReported, quadlet.HookRelayContainerFile)
				return false, false, false, nil
			}
			_, _ = r.host.RunCommand(ctx, "systemctl", "--user", "stop", quadlet.HookRelayServiceName)
			if err := r.host.Remove(relayPath); err != nil {
				return false, false, false, fmt.Errorf("removing orphaned %s: %w", quadlet.HookRelayContainerFile, err)
			}
			report.OrphansRemoved = append(report.OrphansRemoved, quadlet.HookRelayContainerFile)
			return false, false, true, nil
		}
		return false, false, false, nil
	}

	if err := r.host.MkdirAll(r.cfg.HookRelayRootfsDir, 0755); err != nil {
		return false, true, false, fmt.Errorf("creating hook-relay rootfs dir: %w", err)
	}
	unitBytes, err := quadlet.RenderHookRelay(quadlet.HookRelaySpec{
		RootfsDir:  r.cfg.HookRelayRootfsDir,
		BinaryPath: r.cfg.BinaryPath,
		SocketDir:  r.cfg.SocketDir,
	})
	if err != nil {
		return false, true, false, fmt.Errorf("rendering %s: %w", quadlet.HookRelayContainerFile, err)
	}

	existing, err := r.host.ReadFile(relayPath)
	if err == nil {
		if !quadlet.HasUnitMarker(string(existing), quadlet.HookRelayContainerFile) {
			report.ForeignFilesReported = append(report.ForeignFilesReported, quadlet.HookRelayContainerFile)
			return false, true, false, nil
		}
		if string(existing) == string(unitBytes) {
			return true, true, false, nil
		}
	}
	if err := r.host.WriteFileAtomic(relayPath, unitBytes, 0644); err != nil {
		return false, true, false, fmt.Errorf("writing %s: %w", quadlet.HookRelayContainerFile, err)
	}
	report.FilesReRendered = append(report.FilesReRendered, quadlet.HookRelayContainerFile)
	return true, true, true, nil
}

func (r *Reconciler) traefikEnabled() bool {
	return r.cfg.UserUnitDir != "" && r.cfg.TraefikDir != ""
}

// ensureTraefik writes the Traefik container unit, its two sockets and its static config when
// they are missing, or when dermaga owns them and their content is out of date (a new dermaga
// release can pin a new image). A file without dermaga's marker is foreign: reported, never
// touched, and Traefik is then not started by dermaga (e.g. vps-setup.sh's own Traefik).
func (r *Reconciler) ensureTraefik(report *Report) (owned, changed bool, err error) {
	container, err := quadlet.RenderTraefik(quadlet.TraefikSpec{
		Image:      traefik.Image,
		ConfigDir:  r.cfg.TraefikDir,
		DynamicDir: r.cfg.TraefikDynamicDir,
	})
	if err != nil {
		return false, false, fmt.Errorf("rendering traefik unit: %w", err)
	}
	httpSocket, err := quadlet.RenderSocket(quadlet.TraefikHTTPSocketFile, quadlet.TraefikHTTPSocket())
	if err != nil {
		return false, false, fmt.Errorf("rendering traefik http socket: %w", err)
	}
	httpsSocket, err := quadlet.RenderSocket(quadlet.TraefikHTTPSSocketFile, quadlet.TraefikHTTPSSocket())
	if err != nil {
		return false, false, fmt.Errorf("rendering traefik https socket: %w", err)
	}
	static, err := traefik.RenderStatic(traefik.StaticSpec{ACMEStorage: "/etc/traefik/acme.json"})
	if err != nil {
		return false, false, fmt.Errorf("rendering traefik static config: %w", err)
	}

	files := []struct {
		path  string
		data  []byte
		owned func(string) bool
	}{
		{filepath.Join(r.cfg.QuadletDir, quadlet.TraefikContainerFile), container,
			func(c string) bool { return quadlet.HasUnitMarker(c, quadlet.TraefikContainerFile) }},
		{filepath.Join(r.cfg.UserUnitDir, quadlet.TraefikHTTPSocketFile), httpSocket,
			func(c string) bool { return quadlet.HasUnitMarker(c, quadlet.TraefikHTTPSocketFile) }},
		{filepath.Join(r.cfg.UserUnitDir, quadlet.TraefikHTTPSSocketFile), httpsSocket,
			func(c string) bool { return quadlet.HasUnitMarker(c, quadlet.TraefikHTTPSSocketFile) }},
		{filepath.Join(r.cfg.TraefikDir, "traefik.yml"), static,
			func(c string) bool { first, _, _ := strings.Cut(c, "\n"); return first == traefik.StaticMarker() }},
	}

	owned = true
	for _, f := range files {
		current, readErr := r.host.ReadFile(f.path)
		switch {
		case readErr == nil && string(current) == string(f.data):
			continue
		case readErr == nil && !f.owned(string(current)):
			report.ForeignFilesReported = append(report.ForeignFilesReported, filepath.Base(f.path))
			owned = false
			continue
		}
		if err := r.host.WriteFileAtomic(f.path, f.data, 0644); err != nil {
			return false, false, fmt.Errorf("writing %s: %w", f.path, err)
		}
		report.FilesReRendered = append(report.FilesReRendered, filepath.Base(f.path))
		changed = true
	}
	return owned, changed, nil
}

// parseContainerFilename parses app-<name>-r<num>.container.
func parseContainerFilename(fname string) (string, int, bool) {
	if !strings.HasPrefix(fname, name.AppPrefix) || !strings.HasSuffix(fname, ".container") {
		return "", 0, false
	}
	middle := strings.TrimSuffix(strings.TrimPrefix(fname, name.AppPrefix), ".container")
	rIdx := strings.LastIndex(middle, "-r")
	if rIdx == -1 {
		return "", 0, false
	}
	appName := middle[:rIdx]
	relStr := middle[rIdx+2:]
	relNum, err := strconv.Atoi(relStr)
	if err != nil || relNum < 1 {
		return "", 0, false
	}
	return appName, relNum, true
}
