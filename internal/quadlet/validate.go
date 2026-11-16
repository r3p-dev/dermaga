package quadlet

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	"dermaga/internal/name"
)

var (
	// imageRefRegex enforces safe container image reference syntax without shell metacharacters
	imageRefRegex = regexp.MustCompile(`^[a-zA-Z0-9_.\-/:@]+$`)

	// memoryRegex validates cgroup memory strings e.g. 128m, 1g, 512k
	memoryRegex = regexp.MustCompile(`^[1-9][0-9]*[kKmMgG]?$`)

	// cpusRegex validates cpu limit strings e.g. 1, 0.5, 2.5
	cpusRegex = regexp.MustCompile(`^[0-9]+(\.[0-9]+)?$`)

	// capRegex validates Linux capability names (e.g. NET_BIND_SERVICE, CHOWN)
	capRegex = regexp.MustCompile(`^[A-Z0-9_]+$`)
)

// ValidateContainerSpec validates all fields of a ContainerSpec against SEC-16–SEC-19 rules.
func ValidateContainerSpec(spec ContainerSpec) error {
	if err := name.Validate(spec.AppName); err != nil {
		return fmt.Errorf("invalid app name: %w", err)
	}
	if spec.ReleaseNumber <= 0 {
		return fmt.Errorf("release number must be positive, got %d", spec.ReleaseNumber)
	}
	if spec.Image == "" {
		return fmt.Errorf("image must not be empty")
	}
	if !imageRefRegex.MatchString(spec.Image) || strings.ContainsAny(spec.Image, " \t\r\n`'\"$;|&<>") {
		return fmt.Errorf("invalid image reference %q: contains illegal characters", spec.Image)
	}
	if spec.Network != "" {
		if strings.ContainsAny(spec.Network, " \t\r\n`'\"$;|&<>") {
			return fmt.Errorf("invalid network name %q", spec.Network)
		}
	}
	if spec.Port < 0 || spec.Port > 65535 {
		return fmt.Errorf("port out of range [0, 65535]: %d", spec.Port)
	}

	if spec.Limits.Memory != "" && !memoryRegex.MatchString(spec.Limits.Memory) {
		return fmt.Errorf("invalid memory limit %q (must match pattern like 256m, 1g)", spec.Limits.Memory)
	}
	if spec.Limits.CPUs != "" && !cpusRegex.MatchString(spec.Limits.CPUs) {
		return fmt.Errorf("invalid CPUs limit %q (must match pattern like 1.0, 0.5)", spec.Limits.CPUs)
	}
	if spec.Limits.PidsLimit < 0 {
		return fmt.Errorf("pids limit cannot be negative: %d", spec.Limits.PidsLimit)
	}

	for _, cap := range spec.AddCapabilities {
		if !capRegex.MatchString(cap) {
			return fmt.Errorf("invalid capability name %q", cap)
		}
	}

	if spec.EnvFile != "" {
		if !filepath.IsAbs(spec.EnvFile) {
			return fmt.Errorf("env file path must be absolute: %q", spec.EnvFile)
		}
		if strings.ContainsAny(spec.EnvFile, "\r\n\x00") {
			return fmt.Errorf("env file path contains newline or null byte: %q", spec.EnvFile)
		}
	}

	if spec.Health.Type != "" {
		switch spec.Health.Type {
		case "http", "cmd", "none":
			// valid
		default:
			return fmt.Errorf("unsupported health type %q (must be http, cmd, or none)", spec.Health.Type)
		}
	}
	if spec.Health.Cmd != "" && strings.ContainsAny(spec.Health.Cmd, "\r\n\x00") {
		return fmt.Errorf("health command contains newline or null byte")
	}

	return nil
}

// ValidateNetworkSpec validates a NetworkSpec.
func ValidateNetworkSpec(spec NetworkSpec) error {
	if spec.NetworkName == "" {
		return fmt.Errorf("network name must not be empty")
	}
	if strings.ContainsAny(spec.NetworkName, " \t\r\n\x00`'\"$;|&<>") {
		return fmt.Errorf("invalid network name %q", spec.NetworkName)
	}
	return nil
}
