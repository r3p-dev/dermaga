package quadlet

import (
	"strings"
	"testing"
)

func TestQuadletInjectionRejection(t *testing.T) {
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

	baseSpec := ContainerSpec{
		AppName:          "valid-app",
		ReleaseNumber:    1,
		Image:            "localhost/valid-image:r1",
		DropCapabilities: true,
	}

	// 1. AppName injection
	for _, bad := range badStrings {
		spec := baseSpec
		spec.AppName = "app-" + bad
		if _, err := RenderContainer(spec); err == nil {
			t.Errorf("expected error for bad AppName %q, got nil", bad)
		}
	}

	// 2. Image injection
	for _, bad := range badStrings {
		spec := baseSpec
		spec.Image = "image-" + bad
		if _, err := RenderContainer(spec); err == nil {
			t.Errorf("expected error for bad Image %q, got nil", bad)
		}
	}

	// 3. Limits.Memory injection
	for _, bad := range []string{"1g\nExecStartPre=/bin/evil", "100%", "$MEM", "512mb", "10g; rm -rf"} {
		spec := baseSpec
		spec.Limits.Memory = bad
		if _, err := RenderContainer(spec); err == nil {
			t.Errorf("expected error for bad Memory %q, got nil", bad)
		}
	}

	// 4. Limits.CPUs injection
	for _, bad := range []string{"1.0\n[Service]", "2 CPUs", "$CPUS", "1.5; id", "`date`"} {
		spec := baseSpec
		spec.Limits.CPUs = bad
		if _, err := RenderContainer(spec); err == nil {
			t.Errorf("expected error for bad CPUs %q, got nil", bad)
		}
	}

	// 5. Capability injection
	for _, bad := range []string{"NET_BIND_SERVICE\nDropCapability=NONE", "CHOWN; id", "`id`", "CAP $FOO"} {
		spec := baseSpec
		spec.AddCapabilities = []string{bad}
		if _, err := RenderContainer(spec); err == nil {
			t.Errorf("expected error for bad capability %q, got nil", bad)
		}
	}

	// 6. EnvFile injection (newlines, null bytes, relative paths)
	for _, bad := range []string{"/path/to/env\n[Service]", "relative/env", "/path\x00evil"} {
		spec := baseSpec
		spec.EnvFile = bad
		if _, err := RenderContainer(spec); err == nil {
			t.Errorf("expected error for bad EnvFile %q, got nil", bad)
		}
	}

	// 7. HealthCmd injection
	for _, bad := range []string{"curl http://localhost\n[Service]\nExecStart=/bin/evil", "curl\x00evil"} {
		spec := baseSpec
		spec.Health.Cmd = bad
		if _, err := RenderContainer(spec); err == nil {
			t.Errorf("expected error for bad Health.Cmd %q, got nil", bad)
		}
	}

	// 8. NetworkName injection
	for _, bad := range badStrings {
		netSpec := NetworkSpec{NetworkName: "net-" + bad}
		if _, err := RenderNetwork(netSpec); err == nil {
			t.Errorf("expected error for bad NetworkName %q, got nil", bad)
		}
	}
}

func TestDefaultDependenciesGuaranteed(t *testing.T) {
	spec := ContainerSpec{
		AppName:          "demo",
		ReleaseNumber:    1,
		Image:            "localhost/demo:r1",
		DropCapabilities: true,
	}

	data, err := RenderContainer(spec)
	if err != nil {
		t.Fatalf("render container: %v", err)
	}

	out := string(data)
	if !strings.Contains(out, "[Quadlet]\nDefaultDependencies=false") {
		t.Errorf("rendered container missing [Quadlet] DefaultDependencies=false:\n%s", out)
	}

	netData, err := RenderNetwork(NetworkSpec{NetworkName: "web"})
	if err != nil {
		t.Fatalf("render network: %v", err)
	}

	netOut := string(netData)
	if !strings.Contains(netOut, "[Quadlet]\nDefaultDependencies=false") {
		t.Errorf("rendered network missing [Quadlet] DefaultDependencies=false:\n%s", netOut)
	}
}
