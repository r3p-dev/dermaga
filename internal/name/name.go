// Package name defines the project name and all derived resource identifiers
// and validation routines for dermaga.
package name

import (
	"fmt"
	"regexp"
)

// Base project constants
const (
	ProjectName   = "dermaga"
	ProjectPrefix = "dg-"
	AppPrefix     = "app-"
)

var (
	// validNameRegex enforces SEC-17: ^[a-z][a-z0-9-]{0,30}[a-z0-9]$
	validNameRegex = regexp.MustCompile(`^[a-z][a-z0-9-]{0,30}[a-z0-9]$`)

	// Reserved names that cannot be used as app or resource names
	reservedNames = map[string]bool{
		"dermaga":   true,
		"admin":     true,
		"traefik":   true,
		"relay":     true,
		"api":       true,
		"system":    true,
		"root":      true,
		"daemon":    true,
		"probe":     true,
		"hook":      true,
		"hooks":     true,
		"web":       true,
		"localhost": true,
	}
)

// Validate checks whether a name adheres to SEC-17 and is not reserved.
func Validate(n string) error {
	if len(n) < 2 || len(n) > 32 {
		return fmt.Errorf("name %q must be between 2 and 32 characters long", n)
	}
	if !validNameRegex.MatchString(n) {
		return fmt.Errorf("name %q must start with a lowercase letter, end with a letter or digit, and contain only lowercase letters, digits, and hyphens", n)
	}
	if reservedNames[n] {
		return fmt.Errorf("name %q is a reserved system identifier", n)
	}
	return nil
}

// AppContainerName returns the container name for an app release.
func AppContainerName(appName string, releaseNum int) string {
	return fmt.Sprintf("%s%s-r%d", AppPrefix, appName, releaseNum)
}

// AppServiceName returns the systemd unit name for an app release.
func AppServiceName(appName string, releaseNum int) string {
	return fmt.Sprintf("%s%s-r%d.service", AppPrefix, appName, releaseNum)
}

// AppQuadletFilename returns the Quadlet .container filename for an app release.
func AppQuadletFilename(appName string, releaseNum int) string {
	return fmt.Sprintf("%s%s-r%d.container", AppPrefix, appName, releaseNum)
}

// AppDynamicFilename returns the Traefik dynamic config filename for an app.
func AppDynamicFilename(appName string) string {
	return fmt.Sprintf("%s%s.yml", AppPrefix, appName)
}

// SystemdUserUnitName returns the systemd user service name for dermaga daemon.
func SystemdUserUnitName() string {
	return fmt.Sprintf("%s.service", ProjectName)
}

// BridgeNetworkName returns the container bridge network name.
func BridgeNetworkName() string {
	return "web"
}
