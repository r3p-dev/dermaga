//go:build !faultinject

package deploy

// injectRouteFault is the identity in every normal build. The variant that
// can corrupt a route file exists only under the faultinject build tag
// (fault_on.go), which no Makefile target or workflow sets.
func injectRouteFault(_ string, route []byte) []byte { return route }
