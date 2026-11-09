// Package name holds the project name and everything derived from it, so a
// rename touches one place (ARCHITECTURE D13).
package name

import "runtime/debug"

// Name is used for the binary, units, paths, sockets and the session cookie.
const Name = "dermaga"

// Prefixes of what dermaga generates. Startup reconciliation never touches a
// unit or Traefik file without them (BRIEF §3.5).
const (
	Unit    = "dg-"  // Quadlet and systemd units: dg-<app>-<rel8>, dg-hooks, dg-db-<name>, dg-backup-*
	AppFile = "app-" // Traefik dynamic files app-<name>.yml, and the routers/services in them
)

// VersionOverride allows injecting the version at build time via
// -ldflags "-X github.com/r3p-dev/dermaga/internal/name.VersionOverride=...".
var VersionOverride string

// Version is the module version stamped by the Go toolchain (VCS revision for
// local builds, the tag for releases) or overridden by VersionOverride.
func Version() string {
	if VersionOverride != "" {
		return VersionOverride
	}
	if bi, ok := debug.ReadBuildInfo(); ok && bi.Main.Version != "" {
		return bi.Main.Version
	}
	return "dev"
}
