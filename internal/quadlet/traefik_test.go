package quadlet

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func checkGolden(t *testing.T, name string, got []byte) {
	t.Helper()
	goldenFile := filepath.Join("testdata", name+".golden")
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
		t.Errorf("mismatch in golden test %s:\n--- Got:\n%s\n--- Want:\n%s", name, got, want)
	}
}

// HOST-6: Traefik units with socket activation, file provider only, no Podman socket.
func TestTraefikGolden(t *testing.T) {
	c, err := RenderTraefik(TraefikSpec{
		Image:      "docker.io/library/traefik:v3.7.19@sha256:aa942a00bce033c909bf1f7e861d93761be4b31a035f10d2410d33e38a8f41cf",
		ConfigDir:  "/home/server/.local/share/dermaga/traefik",
		DynamicDir: "/home/server/.local/share/dermaga/traefik/dynamic",
	})
	if err != nil {
		t.Fatal(err)
	}
	checkGolden(t, "traefik-container", c)
	if strings.Contains(string(c), "podman.sock") {
		t.Error("SEC-5: traefik unit must not mount the Podman socket")
	}

	h, err := RenderSocket(TraefikHTTPSocketFile, TraefikHTTPSocket())
	if err != nil {
		t.Fatal(err)
	}
	checkGolden(t, "traefik-http-socket", h)

	s, err := RenderSocket(TraefikHTTPSSocketFile, TraefikHTTPSSocket())
	if err != nil {
		t.Fatal(err)
	}
	checkGolden(t, "traefik-https-socket", s)

	if !HasUnitMarker(string(c), TraefikContainerFile) || HasUnitMarker(string(c), TraefikHTTPSocketFile) {
		t.Error("unit marker must match exactly the file it belongs to")
	}
}

func TestRenderTraefikRejectsUnsafeValues(t *testing.T) {
	good := TraefikSpec{Image: "docker.io/library/traefik:v3", ConfigDir: "/a", DynamicDir: "/a/b"}
	for name, mutate := range map[string]func(*TraefikSpec){
		"relative dir":     func(s *TraefikSpec) { s.ConfigDir = "a" },
		"colon in dir":     func(s *TraefikSpec) { s.DynamicDir = "/a:/etc/shadow" },
		"specifier in dir": func(s *TraefikSpec) { s.ConfigDir = "/%h" },
		"newline in dir":   func(s *TraefikSpec) { s.ConfigDir = "/a\nVolume=/:/host" },
		"space in image":   func(s *TraefikSpec) { s.Image = "traefik latest" },
	} {
		spec := good
		mutate(&spec)
		if _, err := RenderTraefik(spec); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
	if _, err := RenderTraefik(good); err != nil {
		t.Errorf("valid spec rejected: %v", err)
	}
}
