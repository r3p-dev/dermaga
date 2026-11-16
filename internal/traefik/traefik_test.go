package traefik

import (
	"flag"
	"os"
	"path/filepath"
	"testing"
)

var update = flag.Bool("update", false, "update golden files")

func boolPtr(b bool) *bool {
	return &b
}

func TestTraefikGolden(t *testing.T) {
	testdataDir := filepath.Join("testdata")
	if err := os.MkdirAll(testdataDir, 0755); err != nil {
		t.Fatalf("creating testdata dir: %v", err)
	}

	// 1. Static Config Golden Test
	t.Run("static-config", func(t *testing.T) {
		spec := StaticSpec{
			ACMEEmail:      "admin@example.com",
			ACMEStaging:    false,
			AccessLog:      true,
			TrustedProxies: []string{"173.245.48.0/20", "103.21.244.0/22"},
		}
		got, err := RenderStatic(spec)
		if err != nil {
			t.Fatalf("render static: %v", err)
		}
		compareGolden(t, testdataDir, "static-config", got)
	})

	// 2. Global Dynamic Config Golden Test
	t.Run("global-dynamic", func(t *testing.T) {
		spec := GlobalSpec{
			TrustedProxies:          []string{"173.245.48.0/20"},
			DefaultRateLimitAverage: 100,
			DefaultRateLimitBurst:   200,
			DefaultCompressMinBytes: 1024,
		}
		got, err := RenderGlobal(spec)
		if err != nil {
			t.Fatalf("render global: %v", err)
		}
		compareGolden(t, testdataDir, "global-dynamic", got)
	})

	// 3. App Dynamic Config Golden Tests
	appCases := map[string]AppSpec{
		"minimal": {
			AppName:       "portfolio",
			ReleaseNumber: 1,
			Port:          8080,
			Domains: []DomainSpec{
				{Domain: "portfolio.example.com", IsPrimary: true},
			},
		},
		"middleware-toggles": {
			AppName:       "blog",
			ReleaseNumber: 2,
			Port:          3000,
			Domains: []DomainSpec{
				{Domain: "blog.example.com", IsPrimary: true},
			},
			Headers: HeaderSpec{
				PermissionsPolicy: "camera=(), microphone=()",
			},
			Middleware: MiddlewareSpec{
				Compress: boolPtr(true),
				RateLimit: &RateLimitSpec{
					Average: 50,
					Burst:   100,
				},
			},
		},
		"aliases-www": {
			AppName:       "landing",
			ReleaseNumber: 1,
			Port:          80,
			Domains: []DomainSpec{
				{Domain: "landing.com", IsPrimary: true},
				{Domain: "landing.net"},
				{Domain: "www.landing.com", RedirectTo: "landing.com"},
				{Domain: "www.landing.net", RedirectTo: "landing.com"},
			},
		},
		"path-prefix": {
			AppName:       "api-service",
			ReleaseNumber: 3,
			Port:          8080,
			Domains: []DomainSpec{
				{Domain: "example.com", IsPrimary: true, PathPrefix: "/api/v1", StripPrefix: true},
			},
		},
		"path-prefix-no-strip": {
			AppName:       "docs-service",
			ReleaseNumber: 1,
			Port:          4000,
			Domains: []DomainSpec{
				{Domain: "example.com", IsPrimary: true, PathPrefix: "/docs", StripPrefix: false},
			},
		},
		"basic-auth": {
			AppName:       "internal-portal",
			ReleaseNumber: 1,
			Port:          8080,
			Domains: []DomainSpec{
				{Domain: "portal.example.com", IsPrimary: true},
			},
			Middleware: MiddlewareSpec{
				BasicAuth: []BasicAuthUser{
					{Username: "admin", Hash: "$2a$10$mip4oyvC1HpMevKOZl8OL.HH2EV2Brq6pga/CUOIUZ62TMxii4ZEm"},
					{Username: "operator", Hash: "$2a$10$mip4oyvC1HpMevKOZl8OL.HH2EV2Brq6pga/CUOIUZ62TMxii4ZEm"},
				},
			},
		},
		"allowlist": {
			AppName:       "secure-admin",
			ReleaseNumber: 1,
			Port:          8443,
			Domains: []DomainSpec{
				{Domain: "admin.example.com", IsPrimary: true},
			},
			Middleware: MiddlewareSpec{
				IPAllowList: []string{"192.168.1.0/24", "10.0.0.1/32"},
			},
		},
		"body-size": {
			AppName:       "uploader",
			ReleaseNumber: 1,
			Port:          9000,
			Domains: []DomainSpec{
				{Domain: "upload.example.com", IsPrimary: true},
			},
			Middleware: MiddlewareSpec{
				MaxRequestBodyBytes: 10485760, // 10 MiB
			},
		},
		"custom-removed-headers": {
			AppName:       "custom-headers-app",
			ReleaseNumber: 1,
			Port:          8080,
			Domains: []DomainSpec{
				{Domain: "headers.example.com", IsPrimary: true},
			},
			Headers: HeaderSpec{
				CustomResponseHeaders: map[string]string{
					"X-Custom-Header":         "custom-value-123",
					"Content-Security-Policy": "default-src 'self'",
					"X-Frame-Options":         "", // removes SAMEORIGIN
				},
			},
		},
		"switch-verify": {
			AppName:       "web-app",
			ReleaseNumber: 1,
			Port:          8080,
			Domains: []DomainSpec{
				{Domain: "webapp.example.com", IsPrimary: true},
			},
			Switch: &SwitchSpec{
				CandidateReleaseNumber: 2,
				CandidatePort:          8080,
				VerifyToken:            "test-switch-verify-token-999",
			},
		},
	}

	for name, spec := range appCases {
		t.Run(name, func(t *testing.T) {
			got, err := RenderApp(spec)
			if err != nil {
				t.Fatalf("render app: %v", err)
			}
			compareGolden(t, testdataDir, name, got)
		})
	}
}

func compareGolden(t *testing.T, dir, name string, got []byte) {
	t.Helper()
	goldenFile := filepath.Join(dir, name+".golden")
	if *update {
		if err := os.WriteFile(goldenFile, got, 0644); err != nil {
			t.Fatalf("writing golden file: %v", err)
		}
	}

	want, err := os.ReadFile(goldenFile)
	if err != nil {
		t.Fatalf("reading golden file %q: %v (run with -update to generate)", goldenFile, err)
	}

	if string(got) != string(want) {
		t.Errorf("mismatch in golden test %s:\n--- Got:\n%s\n--- Want:\n%s", name, string(got), string(want))
	}
}
