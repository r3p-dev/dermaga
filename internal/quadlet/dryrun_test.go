package quadlet

import (
	"context"
	"strings"
	"testing"

	"dermaga/internal/host"
)

func TestDryRunValidatorWithFakeHost(t *testing.T) {
	fake := host.NewFakeHost()
	fake.CommandMock = func(name string, args ...string) ([]byte, error) {
		return []byte("---test.service---\n[Unit]\nDescription=test\n"), nil
	}

	validator := NewDryRunValidator(fake, "/mock/generator")
	ctx := context.Background()

	out, err := validator.ValidateUnit(ctx, "test.container", []byte("[Container]\nImage=alpine\n"), "/tmp/quadlet-test")
	if err != nil {
		t.Fatalf("validation failed: %v", err)
	}

	if !strings.Contains(string(out), "test.service") {
		t.Errorf("unexpected output: %s", string(out))
	}

	if len(fake.Commands) != 1 {
		t.Fatalf("expected 1 command call, got %d", len(fake.Commands))
	}

	cmd := fake.Commands[0]
	if cmd.Name != "/mock/generator" {
		t.Errorf("expected command /mock/generator, got %s", cmd.Name)
	}
	if len(cmd.Args) != 2 || cmd.Args[0] != "--dryrun" || cmd.Args[1] != "--user" {
		t.Errorf("unexpected args: %v", cmd.Args)
	}
	if len(cmd.Env) != 1 || cmd.Env[0] != "QUADLET_UNIT_DIRS=/tmp/quadlet-test" {
		t.Errorf("unexpected env: %v", cmd.Env)
	}
}
