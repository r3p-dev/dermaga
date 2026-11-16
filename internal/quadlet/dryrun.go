package quadlet

import (
	"context"
	"fmt"
	"path/filepath"

	"dermaga/internal/host"
)

const (
	// DefaultGeneratorPath is the standard path to podman-system-generator on Debian 13.
	DefaultGeneratorPath = "/usr/lib/systemd/system-generators/podman-system-generator"
)

// DryRunValidator validates Quadlet units using the host's podman-system-generator in dryrun mode.
type DryRunValidator struct {
	host          host.Host
	generatorPath string
}

// NewDryRunValidator creates a new validator backed by host.
func NewDryRunValidator(h host.Host, generatorPath string) *DryRunValidator {
	if generatorPath == "" {
		generatorPath = DefaultGeneratorPath
	}
	return &DryRunValidator{
		host:          h,
		generatorPath: generatorPath,
	}
}

// ValidateUnit writes unitData to tempDir/unitName and executes podman-system-generator --dryrun --user.
// It returns the generator output and any validation error.
func (v *DryRunValidator) ValidateUnit(ctx context.Context, unitName string, unitData []byte, tempDir string) ([]byte, error) {
	unitPath := filepath.Join(tempDir, unitName)
	if err := v.host.WriteFileAtomic(unitPath, unitData, 0644); err != nil {
		return nil, fmt.Errorf("writing unit to %q: %w", unitPath, err)
	}

	env := []string{fmt.Sprintf("QUADLET_UNIT_DIRS=%s", tempDir)}
	out, err := v.host.RunCommandWithEnv(ctx, env, v.generatorPath, "--dryrun", "--user")
	if err != nil {
		return out, fmt.Errorf("generator rejected unit %s: %w", unitName, err)
	}
	return out, nil
}
