package quadlet

import (
	"strings"
	"testing"
)

func TestHookRelayGolden_DEC30(t *testing.T) {
	out, err := RenderHookRelay(HookRelaySpec{
		RootfsDir:  "/home/server/.local/share/dermaga/hook-relay-rootfs",
		BinaryPath: "/home/server/.local/bin/dermaga",
		SocketDir:  "/run/user/1001/dermaga",
	})
	if err != nil {
		t.Fatalf("RenderHookRelay failed: %v", err)
	}
	checkGolden(t, "hook-relay-container", out)

	s := string(out)
	if !HasUnitMarker(s, HookRelayContainerFile) {
		t.Error("expected DEC-17 unit marker")
	}
	if strings.Contains(s, "podman.sock") {
		t.Error("SEC-5: hook-relay must not mount podman.sock")
	}
	for _, bad := range []string{"\n/home/server/.local/share/dermaga/hook-relay-rootfs\n", "CapAdd="} {
		if strings.Contains(s, bad) {
			t.Errorf("unexpected content %q in hook-relay.container", bad)
		}
	}
}

func TestHookRelayValidation_SEC16(t *testing.T) {
	badSpecs := []HookRelaySpec{
		{RootfsDir: "relative/path", BinaryPath: "/bin/dermaga", SocketDir: "/run/dermaga"},
		{RootfsDir: "/tmp/rootfs\nExec=sh", BinaryPath: "/bin/dermaga", SocketDir: "/run/dermaga"},
		{RootfsDir: "/tmp/rootfs", BinaryPath: "/bin/dermaga;id", SocketDir: "/run/dermaga"},
	}
	for _, sp := range badSpecs {
		if _, err := RenderHookRelay(sp); err == nil {
			t.Errorf("expected RenderHookRelay(%+v) to fail", sp)
		}
	}
}
