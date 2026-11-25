package quadlet

import (
	"fmt"
	"path/filepath"
	"strings"
)

const (
	HookRelayContainerFile = "hook-relay.container"
	HookRelayServiceName   = "hook-relay.service"
	HookRelayBackendURL    = "http://hook-relay:8080"
)

// HookRelaySpec defines the minimal rootless webhook relay container (DEC-02, DEC-30).
type HookRelaySpec struct {
	RootfsDir  string // Empty directory managed by the reconciler (Rootfs=, no base image)
	BinaryPath string // Host path to static dermaga binary, mounted read-only at /dermaga
	SocketDir  string // Host $XDG_RUNTIME_DIR/dermaga directory containing hooks.sock, mounted read-only at /run/dermaga
}

// RenderHookRelay renders hook-relay.container with strict isolation (ReadOnly, NoNewPrivileges,
// DropCapability=ALL, DefaultDependencies=false per DEC-06, ordered after dermaga.service).
func RenderHookRelay(spec HookRelaySpec) ([]byte, error) {
	for _, dir := range []string{spec.RootfsDir, spec.BinaryPath, spec.SocketDir} {
		if !filepath.IsAbs(dir) || strings.ContainsAny(dir, " \t\r\n\x00%\\:`'\"$;|&<>") {
			return nil, fmt.Errorf("invalid hook-relay path %q", dir)
		}
	}

	doc := iniDocument{HeaderComment: UnitMarker(HookRelayContainerFile)}

	unit := doc.addSection("Unit")
	unit.add("Description", "dermaga GitHub webhook relay")
	unit.add("After", "dermaga.service")

	c := doc.addSection("Container")
	c.add("ContainerName", "hook-relay")
	c.add("Rootfs", spec.RootfsDir)
	c.add("Exec", "/dermaga hook-relay -listen :8080 -socket /run/dermaga/hooks.sock")
	c.add("Network", NetworkFileName("web"))
	c.add("NoNewPrivileges", "true")
	c.add("ReadOnly", "true")
	c.add("DropCapability", "ALL")
	c.add("PidsLimit", "64")
	c.add("PodmanArgs", "--memory=64m")
	c.add("Volume", spec.BinaryPath+":/dermaga:ro")
	c.add("Volume", spec.SocketDir+":/run/dermaga:ro")

	q := doc.addSection("Quadlet")
	q.add("DefaultDependencies", "false") // DEC-06

	s := doc.addSection("Service")
	s.add("Restart", "always")
	s.add("RestartSec", "3s")

	inst := doc.addSection("Install")
	inst.add("WantedBy", "default.target")

	return doc.render()
}
