package deploy

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"dermaga/internal/name"
	"dermaga/internal/quadlet"
	"dermaga/internal/store"
)

// RuntimeConfig is the non-secret part of a release's runtime configuration, stored as JSON on
// the deployment row (DEP-4). The environment lives in an encrypted env version (DEC-32).
type RuntimeConfig struct {
	Port              int     `json:"port"`
	Command           *string `json:"command,omitempty"`
	HealthType        string  `json:"health_type"`
	HealthPath        *string `json:"health_path,omitempty"`
	HealthCmd         *string `json:"health_cmd,omitempty"`
	HealthTimeoutSec  int     `json:"health_timeout_sec"`
	HealthConsecutive int     `json:"health_consecutive"`
	MemoryLimit       string  `json:"memory_limit"`
	CPULimit          string  `json:"cpu_limit"`
	PidsLimit         int     `json:"pids_limit"`
	ReadOnlyRoot      bool    `json:"read_only_root"`
	AddCapabilities   string  `json:"add_capabilities"`
}

// RuntimeConfigOf captures the app's current settings for a new release.
func RuntimeConfigOf(app *store.App) RuntimeConfig {
	return RuntimeConfig{
		Port: app.Port, Command: app.Command,
		HealthType: app.HealthType, HealthPath: app.HealthPath, HealthCmd: app.HealthCmd,
		HealthTimeoutSec: app.HealthTimeoutSec, HealthConsecutive: app.HealthConsecutive,
		MemoryLimit: app.MemoryLimit, CPULimit: app.CPULimit, PidsLimit: app.PidsLimit,
		ReadOnlyRoot: app.ReadOnlyRoot, AddCapabilities: app.AddCapabilities,
	}
}

// ReleaseRuntimeConfig returns the config a release was deployed with; releases from before
// migration 0005 that were never converted fall back to the app's current settings.
func ReleaseRuntimeConfig(app *store.App, dep *store.Deployment) (RuntimeConfig, error) {
	if dep.RuntimeConfig == "" {
		return RuntimeConfigOf(app), nil
	}
	var rc RuntimeConfig
	if err := json.Unmarshal([]byte(dep.RuntimeConfig), &rc); err != nil {
		return rc, fmt.Errorf("release r%d has an unreadable runtime config: %w", dep.ReleaseNumber, err)
	}
	return rc, nil
}

func (rc RuntimeConfig) json() string {
	b, _ := json.Marshal(rc)
	return string(b)
}

// EnvFilePath is where a release's runtime env file lives while the release runs (0600).
func EnvFilePath(envDir, appName string, release int) string {
	return filepath.Join(envDir, fmt.Sprintf("%s-r%d.env", appName, release))
}

// ContainerSpecFor builds the Quadlet spec of one release. The deploy engine and the startup
// reconciler (DEP-10) both use it, so a re-rendered unit is identical to the deployed one.
func ContainerSpecFor(appName string, release int, image string, rc RuntimeConfig, envFile string) quadlet.ContainerSpec {
	var addCaps []string
	for _, c := range strings.Split(rc.AddCapabilities, ",") {
		if c = strings.TrimSpace(c); c != "" {
			addCaps = append(addCaps, c)
		}
	}
	var healthPath, healthCmd string
	if rc.HealthPath != nil {
		healthPath = *rc.HealthPath
	}
	if rc.HealthCmd != nil {
		healthCmd = *rc.HealthCmd
	}
	return quadlet.ContainerSpec{
		AppName:       appName,
		ReleaseNumber: release,
		Image:         image,
		Network:       name.BridgeNetworkName(),
		Port:          rc.Port,
		Limits:        quadlet.Limits{Memory: rc.MemoryLimit, CPUs: rc.CPULimit, PidsLimit: rc.PidsLimit},
		Health: quadlet.HealthSpec{
			Type:          rc.HealthType,
			Path:          healthPath,
			Cmd:           healthCmd,
			TimeoutSec:    rc.HealthTimeoutSec,
			NotifyHealthy: rc.HealthType == "cmd",
		},
		ReadOnlyRoot:     rc.ReadOnlyRoot,
		DropCapabilities: true,
		AddCapabilities:  addCaps,
		EnvFile:          envFile,
	}
}
