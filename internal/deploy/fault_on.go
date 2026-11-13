//go:build faultinject

package deploy

import "os"

// injectRouteFault (test builds only: go build -tags faultinject) appends
// garbage to the route file of the app named in DERMAGA_FAULT_ROUTE, so the
// real Traefik rejects the file and D8 can be observed on a real host. The
// validation and verification code is the same as in a release build.
func injectRouteFault(app string, route []byte) []byte {
	if os.Getenv("DERMAGA_FAULT_ROUTE") != app {
		return route
	}
	return append(append([]byte{}, route...), "\n}}} faultinject: not valid YAML {{{\n"...)
}
