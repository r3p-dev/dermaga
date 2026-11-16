// Package quadlet generates systemd Quadlet unit files for Podman rootless containers
// and networks, enforcing security boundaries, cgroup resource limits, and boot reliability.
package quadlet

// ContainerSpec defines the parameters for generating a Quadlet .container unit.
type ContainerSpec struct {
	AppName          string
	ReleaseNumber    int
	Image            string
	Network          string
	Port             int
	Limits           Limits
	Health           HealthSpec
	ReadOnlyRoot     bool
	DropCapabilities bool
	AddCapabilities  []string
	EnvFile          string
	StopTimeoutSec   int
	RestartSec       string
	RestartSteps     int
	RestartMaxDelay  string
}

// Limits defines cgroup resource constraints mapped via PodmanArgs (DEC-05).
type Limits struct {
	Memory    string // e.g. "256m", "1g"
	CPUs      string // e.g. "1.0", "0.5"
	PidsLimit int    // e.g. 128
}

// HealthSpec defines container health check settings.
type HealthSpec struct {
	Type          string // "http", "cmd", "none"
	Path          string // HTTP probe path (e.g. "/health")
	Cmd           string // Shell command for cmd health check
	TimeoutSec    int    // Start timeout in seconds
	NotifyHealthy bool   // Whether to set Notify=healthy (Spike 3.5)
}

// NetworkSpec defines the parameters for generating a Quadlet .network unit.
type NetworkSpec struct {
	NetworkName string
}
