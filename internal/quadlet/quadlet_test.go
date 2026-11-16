package quadlet

import (
	"flag"
	"os"
	"path/filepath"
	"testing"
)

var update = flag.Bool("update", false, "update golden files")

func TestQuadletGolden(t *testing.T) {
	cases := map[string]struct {
		isNetwork bool
		netSpec   NetworkSpec
		contSpec  ContainerSpec
	}{
		"minimal": {
			contSpec: ContainerSpec{
				AppName:          "demo",
				ReleaseNumber:    1,
				Image:            "localhost/app-demo:r1",
				DropCapabilities: true,
			},
		},
		"custom-limits": {
			contSpec: ContainerSpec{
				AppName:       "shop",
				ReleaseNumber: 2,
				Image:         "ghcr.io/org/shop:sha256-abcdef",
				Limits: Limits{
					Memory:    "1g",
					CPUs:      "2.0",
					PidsLimit: 256,
				},
				DropCapabilities: true,
			},
		},
		"readonly-root": {
			contSpec: ContainerSpec{
				AppName:          "worker",
				ReleaseNumber:    3,
				Image:            "localhost/worker:r3",
				ReadOnlyRoot:     true,
				DropCapabilities: true,
			},
		},
		"caps-added": {
			contSpec: ContainerSpec{
				AppName:          "proxy",
				ReleaseNumber:    1,
				Image:            "localhost/proxy:r1",
				DropCapabilities: true,
				AddCapabilities:  []string{"NET_BIND_SERVICE"},
			},
		},
		"env-file": {
			contSpec: ContainerSpec{
				AppName:          "backend",
				ReleaseNumber:    4,
				Image:            "localhost/backend:r4",
				EnvFile:          "/home/server/.config/dermaga/env/backend.env",
				DropCapabilities: true,
			},
		},
		"health-cmd": {
			contSpec: ContainerSpec{
				AppName:       "backend-api",
				ReleaseNumber: 5,
				Image:         "localhost/backend-api:r5",
				Health: HealthSpec{
					Type:          "cmd",
					Cmd:           "curl -fsS http://localhost:8080/health || exit 1",
					TimeoutSec:    45,
					NotifyHealthy: true,
				},
				DropCapabilities: true,
			},
		},
		"url-escaped-health": {
			contSpec: ContainerSpec{
				AppName:       "api-server",
				ReleaseNumber: 1,
				Image:         "docker.io/library/api:v1",
				Health: HealthSpec{
					Type:          "cmd",
					Cmd:           "curl -f http://localhost:8080/health%20check",
					TimeoutSec:    30,
					NotifyHealthy: true,
				},
				DropCapabilities: false,
			},
		},
		"network-unit": {
			isNetwork: true,
			netSpec: NetworkSpec{
				NetworkName: "web",
			},
		},
	}

	testdataDir := filepath.Join("testdata")
	if err := os.MkdirAll(testdataDir, 0755); err != nil {
		t.Fatalf("creating testdata dir: %v", err)
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			var got []byte
			var err error

			if tc.isNetwork {
				got, err = RenderNetwork(tc.netSpec)
			} else {
				got, err = RenderContainer(tc.contSpec)
			}
			if err != nil {
				t.Fatalf("rendering failed: %v", err)
			}

			goldenFile := filepath.Join(testdataDir, name+".golden")
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
		})
	}
}
