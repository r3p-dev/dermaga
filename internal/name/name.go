// Package name holds the project name and everything derived from it, so a
// rename touches one place (ARCHITECTURE D13).
package name

import "runtime/debug"

// Name is used for the binary, units, paths, sockets and the session cookie.
const Name = "dermaga"

// Version is the module version stamped by the Go toolchain (VCS revision for
// local builds, the tag for releases).
func Version() string {
	if bi, ok := debug.ReadBuildInfo(); ok && bi.Main.Version != "" {
		return bi.Main.Version
	}
	return "unknown"
}
