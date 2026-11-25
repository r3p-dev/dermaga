package traefik_test

import (
	"strings"
	"testing"

	"dermaga/internal/traefik"
)

func TestRenderHookDomain_APP2_TLS5(t *testing.T) {
	out, err := traefik.RenderHookDomain("Hooks.Example.COM", "")
	if err != nil {
		t.Fatalf("RenderHookDomain failed: %v", err)
	}
	s := string(out)
	if !traefik.HasHookDomainMarker(s) {
		t.Errorf("expected hook-domain ownership marker")
	}
	for _, want := range []string{
		"Host(`hooks.example.com`) && Path(`/hooks/github-app`) && Method(`POST`)",
		`"certResolver": "le"`,
		`"websecure"`,
		`"servers": []`,
	} {
		if !strings.Contains(s, want) {
			t.Errorf("expected %q in rendered hook-domain.yml:\n%s", want, s)
		}
	}
}

func TestValidateHookDomain_SEC16(t *testing.T) {
	for _, bad := range []string{
		"",
		"localhost",
		"192.0.2.1",
		"hooks",
		"bad\nhost.example.com",
		"bad`host.example.com",
		"bad$host.example.com",
		"bad%host.example.com",
		"-bad.example.com",
	} {
		if err := traefik.ValidateHookDomain(bad); err == nil {
			t.Errorf("expected ValidateHookDomain(%q) to fail", bad)
		}
	}
}
