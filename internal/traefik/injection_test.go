package traefik

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v2"
)

func TestTraefikInjectionRejection(t *testing.T) {
	badStrings := []string{
		"test\ninjection",
		"test\r\ninjection",
		"test\x00injection",
		"test; rm -rf /",
		"test`id`",
		"test$(id)",
		"test${VAR}",
		"test\"quote",
		"test'quote",
		"test|pipe",
		"test&background",
		"test>redirect",
	}

	baseSpec := AppSpec{
		AppName:       "clean-app",
		ReleaseNumber: 1,
		Port:          8080,
		Domains: []DomainSpec{
			{Domain: "clean.example.com", IsPrimary: true},
		},
	}

	// 1. AppName injection
	for _, bad := range badStrings {
		spec := baseSpec
		spec.AppName = "app-" + bad
		if _, err := RenderApp(spec); err == nil {
			t.Errorf("expected error for bad AppName %q, got nil", bad)
		}
	}

	// 2. Domain injection
	for _, bad := range badStrings {
		spec := baseSpec
		spec.Domains = []DomainSpec{{Domain: bad, IsPrimary: true}}
		if _, err := RenderApp(spec); err == nil {
			t.Errorf("expected error for bad Domain %q, got nil", bad)
		}
	}

	// 3. PathPrefix injection
	for _, bad := range []string{"api\npath", "/api\r\npath", "/api\x00path", "/api; evil", "/api`id`", "/api\"quote"} {
		spec := baseSpec
		spec.Domains = []DomainSpec{{Domain: "clean.example.com", IsPrimary: true, PathPrefix: bad}}
		if _, err := RenderApp(spec); err == nil {
			t.Errorf("expected error for bad PathPrefix %q, got nil", bad)
		}
	}

	// 4. Header Name injection
	for _, bad := range []string{"X-Header\nInjected", "Header: value", "X-Header\r\nFoo", "Header name with space", "X-Header\x00"} {
		spec := baseSpec
		spec.Headers = HeaderSpec{
			CustomResponseHeaders: map[string]string{bad: "clean-val"},
		}
		if _, err := RenderApp(spec); err == nil {
			t.Errorf("expected error for bad header name %q, got nil", bad)
		}
	}

	// 5. Header Value injection (newlines, null bytes)
	for _, bad := range []string{"val\ninjected", "val\r\ninjected", "val\x00injected"} {
		spec := baseSpec
		spec.Headers = HeaderSpec{
			CustomResponseHeaders: map[string]string{"X-Clean": bad},
		}
		if _, err := RenderApp(spec); err == nil {
			t.Errorf("expected error for bad header value %q, got nil", bad)
		}
	}

	// 6. Basic Auth Username injection
	for _, bad := range []string{"user:colon", "user\nnewline", "user\x00null", "user with space"} {
		spec := baseSpec
		spec.Middleware = MiddlewareSpec{
			BasicAuth: []BasicAuthUser{{Username: bad, Hash: "$2a$10$abcdefghijklmnopqrstuuABCDEFGHIJKLMNOPQRSTUVWXYZ012"}},
		}
		if _, err := RenderApp(spec); err == nil {
			t.Errorf("expected error for bad basic auth username %q, got nil", bad)
		}
	}

	// 7. Basic Auth Hash injection
	for _, bad := range []string{"hash\nnewline", "hash\x00null", "hash with space"} {
		spec := baseSpec
		spec.Middleware = MiddlewareSpec{
			BasicAuth: []BasicAuthUser{{Username: "admin", Hash: bad}},
		}
		if _, err := RenderApp(spec); err == nil {
			t.Errorf("expected error for bad basic auth hash %q, got nil", bad)
		}
	}

	// 8. IP Allowlist injection
	for _, bad := range []string{"192.168.1.1\n[evil]", "not-an-ip", "10.0.0.1/33", "192.168.1.1; rm"} {
		spec := baseSpec
		spec.Middleware = MiddlewareSpec{
			IPAllowList: []string{bad},
		}
		if _, err := RenderApp(spec); err == nil {
			t.Errorf("expected error for bad allowlist CIDR %q, got nil", bad)
		}
	}

	// 9. Switch VerifyToken injection
	for _, bad := range badStrings {
		spec := baseSpec
		spec.Switch = &SwitchSpec{
			CandidateReleaseNumber: 2,
			CandidatePort:          8080,
			VerifyToken:            bad,
		}
		if _, err := RenderApp(spec); err == nil {
			t.Errorf("expected error for bad VerifyToken %q, got nil", bad)
		}
	}
}

func TestACMEOnEveryRouter(t *testing.T) {
	spec := AppSpec{
		AppName:       "acme-check",
		ReleaseNumber: 1,
		Port:          8080,
		Domains: []DomainSpec{
			{Domain: "acme.example.com", IsPrimary: true},
			{Domain: "www.acme.example.com", RedirectTo: "acme.example.com"},
		},
		Switch: &SwitchSpec{
			CandidateReleaseNumber: 2,
			CandidatePort:          8080,
			VerifyToken:            "test-token",
		},
	}

	data, err := RenderApp(spec)
	if err != nil {
		t.Fatalf("render app: %v", err)
	}

	var doc Dynamic
	if err := json.Unmarshal(StripHeaderComment(data), &doc); err != nil {
		t.Fatalf("unmarshal json: %v", err)
	}

	if doc.HTTP == nil || len(doc.HTTP.Routers) == 0 {
		t.Fatalf("expected routers, got none")
	}

	// Verify that every single router has TLS configured with certResolver: "le"
	for routerName, r := range doc.HTTP.Routers {
		if r.TLS == nil {
			t.Errorf("router %q missing TLS configuration", routerName)
			continue
		}
		if r.TLS.CertResolver != "le" {
			t.Errorf("router %q certResolver is %q, want 'le'", routerName, r.TLS.CertResolver)
		}
	}

	// Verify verify router is present when Switch != nil
	verifyRouter, hasVerify := doc.HTTP.Routers["app-acme-check-verify"]
	if !hasVerify {
		t.Errorf("expected verify router app-acme-check-verify, not found")
	} else {
		if !strings.Contains(verifyRouter.Rule, "X-Dermaga-Verify") {
			t.Errorf("verify router rule missing X-Dermaga-Verify: %s", verifyRouter.Rule)
		}
	}

	// Render without Switch: verify router must NOT be present
	spec.Switch = nil
	dataNoSwitch, err := RenderApp(spec)
	if err != nil {
		t.Fatalf("render app without switch: %v", err)
	}
	var docNoSwitch Dynamic
	if err := json.Unmarshal(StripHeaderComment(dataNoSwitch), &docNoSwitch); err != nil {
		t.Fatalf("unmarshal json: %v", err)
	}
	if _, hasVerifyNoSwitch := docNoSwitch.HTTP.Routers["app-acme-check-verify"]; hasVerifyNoSwitch {
		t.Errorf("verify router must NOT be present when Switch is nil")
	}
}

func TestDefaultHeadersMatchVPSSetup(t *testing.T) {
	// Read reference header block from vps-setup.sh golden file
	goldenPath := filepath.Join("testdata", "vps-setup-headers.golden")
	goldenData, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("reading vps-setup-headers.golden: %v", err)
	}

	var ref struct {
		SecurityHeaders struct {
			STSSeconds              int64             `yaml:"stsSeconds"`
			STSIncludeSubdomains    bool              `yaml:"stsIncludeSubdomains"`
			ContentTypeNosniff      bool              `yaml:"contentTypeNosniff"`
			CustomFrameOptionsValue string            `yaml:"customFrameOptionsValue"`
			ReferrerPolicy          string            `yaml:"referrerPolicy"`
			CustomResponseHeaders   map[string]string `yaml:"customResponseHeaders"`
		} `yaml:"securityHeaders"`
		PerAppDefaultHeaders map[string]string `yaml:"perAppDefaultHeaders"`
	}
	if err := yaml.Unmarshal(goldenData, &ref); err != nil {
		t.Fatalf("unmarshaling vps-setup-headers.golden: %v", err)
	}

	spec := AppSpec{
		AppName:       "headers-check",
		ReleaseNumber: 1,
		Port:          8080,
		Domains: []DomainSpec{
			{Domain: "headers.example.com", IsPrimary: true},
		},
	}

	data, err := RenderApp(spec)
	if err != nil {
		t.Fatalf("render app: %v", err)
	}

	var doc Dynamic
	if err := json.Unmarshal(StripHeaderComment(data), &doc); err != nil {
		t.Fatalf("unmarshal json: %v", err)
	}

	mid, exists := doc.HTTP.Middlewares["app-headers-check-headers"]
	if !exists || mid.Headers == nil {
		t.Fatalf("missing headers middleware")
	}

	h := mid.Headers
	if h.STSSeconds != ref.SecurityHeaders.STSSeconds {
		t.Errorf("STSSeconds = %d, want %d (from vps-setup.sh)", h.STSSeconds, ref.SecurityHeaders.STSSeconds)
	}
	if h.STSIncludeSubdomains != ref.SecurityHeaders.STSIncludeSubdomains {
		t.Errorf("STSIncludeSubdomains = %v, want %v (from vps-setup.sh)", h.STSIncludeSubdomains, ref.SecurityHeaders.STSIncludeSubdomains)
	}
	if h.ContentTypeNosniff != ref.SecurityHeaders.ContentTypeNosniff {
		t.Errorf("ContentTypeNosniff = %v, want %v (from vps-setup.sh)", h.ContentTypeNosniff, ref.SecurityHeaders.ContentTypeNosniff)
	}
	if h.CustomFrameOptionsValue != ref.SecurityHeaders.CustomFrameOptionsValue {
		t.Errorf("CustomFrameOptionsValue = %q, want %q (from vps-setup.sh)", h.CustomFrameOptionsValue, ref.SecurityHeaders.CustomFrameOptionsValue)
	}
	if h.ReferrerPolicy != ref.SecurityHeaders.ReferrerPolicy {
		t.Errorf("ReferrerPolicy = %q, want %q (from vps-setup.sh)", h.ReferrerPolicy, ref.SecurityHeaders.ReferrerPolicy)
	}
	for k, expectedVal := range ref.SecurityHeaders.CustomResponseHeaders {
		if gotVal, ok := h.CustomResponseHeaders[k]; !ok || gotVal != expectedVal {
			t.Errorf("CustomResponseHeaders[%q] = %q, want %q (from vps-setup.sh)", k, gotVal, expectedVal)
		}
	}
	for k, expectedVal := range ref.PerAppDefaultHeaders {
		if gotVal, ok := h.CustomResponseHeaders[k]; !ok || gotVal != expectedVal {
			t.Errorf("CustomResponseHeaders[%q] = %q, want %q (from vps-setup.sh)", k, gotVal, expectedVal)
		}
	}
}
