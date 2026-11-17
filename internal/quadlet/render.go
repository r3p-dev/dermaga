package quadlet

import (
	"fmt"
	"strconv"
	"strings"

	"dermaga/internal/name"
)

// RenderContainer renders a validated ContainerSpec into a Quadlet .container unit.
func RenderContainer(spec ContainerSpec) ([]byte, error) {
	if err := ValidateContainerSpec(spec); err != nil {
		return nil, fmt.Errorf("validating container spec: %w", err)
	}

	netName := spec.Network
	if netName == "" {
		netName = name.BridgeNetworkName()
	}

	pidsLimit := spec.Limits.PidsLimit
	if pidsLimit <= 0 {
		pidsLimit = 128
	}

	stopTimeout := spec.StopTimeoutSec
	if stopTimeout <= 0 {
		stopTimeout = 10
	}

	restartSec := spec.RestartSec
	if restartSec == "" {
		restartSec = "1s"
	}
	restartSteps := spec.RestartSteps
	if restartSteps <= 0 {
		restartSteps = 3
	}
	restartMaxDelay := spec.RestartMaxDelay
	if restartMaxDelay == "" {
		restartMaxDelay = "8s"
	}

	startTimeout := spec.Health.TimeoutSec
	if startTimeout <= 0 {
		startTimeout = 30
	}

	doc := iniDocument{
		HeaderComment: ContainerMarker(spec.AppName),
	}

	// [Unit]
	unitSec := doc.addSection("Unit")
	unitSec.add("Description", fmt.Sprintf("dermaga app %s release %d", spec.AppName, spec.ReleaseNumber))

	// [Container]
	contSec := doc.addSection("Container")
	contSec.add("ContainerName", name.AppContainerName(spec.AppName, spec.ReleaseNumber))
	contSec.add("Image", spec.Image)
	contSec.add("Network", netName)

	var podmanArgs []string
	if spec.Limits.Memory != "" {
		podmanArgs = append(podmanArgs, "--memory="+spec.Limits.Memory)
	}
	if spec.Limits.CPUs != "" {
		podmanArgs = append(podmanArgs, "--cpus="+spec.Limits.CPUs)
	}
	if len(podmanArgs) > 0 {
		contSec.add("PodmanArgs", strings.Join(podmanArgs, " "))
	}

	contSec.add("PidsLimit", strconv.Itoa(pidsLimit))
	contSec.add("NoNewPrivileges", "true")

	if spec.DropCapabilities {
		contSec.add("DropCapability", "ALL")
	}
	if len(spec.AddCapabilities) > 0 {
		contSec.add("AddCapability", strings.Join(spec.AddCapabilities, " "))
	}

	if spec.ReadOnlyRoot {
		contSec.add("ReadOnly", "true")
		contSec.add("ReadOnlyTmpfs", "true")
	}

	if spec.EnvFile != "" {
		contSec.add("EnvironmentFile", spec.EnvFile)
	}

	contSec.add("StopTimeout", strconv.Itoa(stopTimeout))

	if spec.Health.Cmd != "" {
		// Escape '%' to '%%' to prevent unintended systemd specifier expansion (%h, %u, %20)
		escapedCmd := strings.ReplaceAll(spec.Health.Cmd, "%", "%%")
		contSec.add("HealthCmd", escapedCmd)
	}
	if spec.Health.NotifyHealthy || spec.Health.Cmd != "" {
		contSec.add("Notify", "healthy")
	}

	// [Quadlet] - CRITICAL: DefaultDependencies=false eliminates the 90s boot delay (DEC-06)
	quadletSec := doc.addSection("Quadlet")
	quadletSec.add("DefaultDependencies", "false")

	// [Service]
	srvSec := doc.addSection("Service")
	srvSec.add("Restart", "on-failure")
	srvSec.add("RestartSec", restartSec)
	srvSec.add("RestartSteps", strconv.Itoa(restartSteps))
	srvSec.add("RestartMaxDelaySec", restartMaxDelay)
	srvSec.add("TimeoutStartSec", strconv.Itoa(startTimeout))
	srvSec.add("TimeoutStopSec", strconv.Itoa(stopTimeout+10))

	// [Install]
	instSec := doc.addSection("Install")
	instSec.add("WantedBy", "default.target")

	return doc.render()
}

// RenderNetwork renders a validated NetworkSpec into a Quadlet .network unit.
func RenderNetwork(spec NetworkSpec) ([]byte, error) {
	if err := ValidateNetworkSpec(spec); err != nil {
		return nil, fmt.Errorf("validating network spec: %w", err)
	}

	doc := iniDocument{
		HeaderComment: NetworkMarker(spec.NetworkName),
	}

	// [Network]
	netSec := doc.addSection("Network")
	netSec.add("NetworkName", spec.NetworkName)

	// [Quadlet] - CRITICAL: DefaultDependencies=false eliminates the 90s boot delay (DEC-06)
	quadletSec := doc.addSection("Quadlet")
	quadletSec.add("DefaultDependencies", "false")

	return doc.render()
}

// ContainerMarker returns the explicit ownership marker comment for an app container unit.
func ContainerMarker(appName string) string {
	return fmt.Sprintf("# managed by dermaga; app=%s", appName)
}

// NetworkMarker returns the explicit ownership marker comment for a network unit.
func NetworkMarker(networkName string) string {
	return fmt.Sprintf("# managed by dermaga; network=%s", networkName)
}

// HasContainerMarker reports whether content contains the container ownership marker for appName.
func HasContainerMarker(content, appName string) bool {
	lines := strings.Split(content, "\n")
	if len(lines) == 0 {
		return false
	}
	expected := ContainerMarker(appName)
	for i := 0; i < len(lines) && i < 3; i++ {
		line := strings.TrimSpace(lines[i])
		if line == expected || strings.HasPrefix(line, "# managed by dermaga; app="+appName) {
			return true
		}
	}
	return false
}

// HasNetworkMarker reports whether content contains the network ownership marker for networkName.
func HasNetworkMarker(content, networkName string) bool {
	lines := strings.Split(content, "\n")
	if len(lines) == 0 {
		return false
	}
	expected := NetworkMarker(networkName)
	for i := 0; i < len(lines) && i < 3; i++ {
		if strings.TrimSpace(lines[i]) == expected {
			return true
		}
	}
	return false
}

// HasAnyMarker reports whether content contains any dermaga management marker.
func HasAnyMarker(content string) bool {
	lines := strings.Split(content, "\n")
	if len(lines) == 0 {
		return false
	}
	for i := 0; i < len(lines) && i < 3; i++ {
		line := strings.TrimSpace(lines[i])
		if strings.HasPrefix(line, "# managed by dermaga") {
			return true
		}
	}
	return false
}

// UnitFileName returns the Quadlet filename for an app release container (e.g. "app-demo-r1.container").
func UnitFileName(appName string, releaseNumber int) string {
	return fmt.Sprintf("%s.container", strings.TrimSuffix(name.AppServiceName(appName, releaseNumber), ".service"))
}

// NetworkFileName returns the Quadlet filename for a network (e.g. "web.network").
func NetworkFileName(networkName string) string {
	return fmt.Sprintf("%s.network", networkName)
}
