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

// Config configures file directories for reconciliation.
type Config struct {
	QuadletDir        string
	TraefikDynamicDir string
}

// Report records the outcome of a reconciliation pass (DEP-10).
type Report struct {
	OrphansRemoved       []string `json:"orphans_removed"`
	FilesReRendered      []string `json:"files_re_rendered"`
	UnitsStarted         []string `json:"units_started"`
	VerifyRoutersCleaned []string `json:"verify_routers_cleaned"`
	ForeignFilesReported []string `json:"foreign_files_reported"`
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
			hasNetworkUnit = true
			continue
		}

		if strings.HasPrefix(fname, name.AppPrefix) && strings.HasSuffix(fname, ".container") {
			// This is a dermaga container unit
			if _, isActive := activeUnits[fname]; isActive {
				foundUnitsOnDisk[fname] = true
			} else {
				// Orphan unit! Stop unit and remove file
				appName, relNum, ok := parseContainerFilename(fname)
				if ok {
					serviceName := name.AppServiceName(appName, relNum)
					_, _ = r.host.RunCommand(ctx, "systemctl", "--user", "stop", serviceName)
				}
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

	// 2c. Reload systemd daemon if Quadlet units changed
	if daemonReloadNeeded {
		if _, err := r.host.RunCommand(ctx, "systemctl", "--user", "daemon-reload"); err != nil {
			return nil, fmt.Errorf("reloading systemd user daemon: %w", err)
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

	for _, entry := range traefikEntries {
		if entry.IsDir() {
			continue
		}
		fname := entry.Name()
		filePath := filepath.Join(r.cfg.TraefikDynamicDir, fname)

		if fname == "00-global.yml" {
			hasGlobalConfig = true
			continue
		}

		// Leftover verify router check: app-*-verify.yml
		if strings.HasPrefix(fname, name.AppPrefix) && strings.HasSuffix(fname, "-verify.yml") {
			if err := r.host.Remove(filePath); err == nil {
				report.VerifyRoutersCleaned = append(report.VerifyRoutersCleaned, fname)
			}
			continue
		}

		// App routing config check: app-<name>.yml
		if strings.HasPrefix(fname, name.AppPrefix) && strings.HasSuffix(fname, ".yml") {
			appName := strings.TrimSuffix(strings.TrimPrefix(fname, name.AppPrefix), ".yml")
			if _, isActive := activeAppNames[appName]; isActive {
				foundConfigsOnDisk[fname] = true
			} else {
				// Orphan config! Remove it
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
