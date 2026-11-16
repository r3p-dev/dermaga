package host_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"dermaga/internal/host"
)

func TestRealHost_WriteFileAtomic(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "sub", "test.txt")

	h := host.NewRealHost()
	content := []byte("hello atomic world")
	perm := os.FileMode(0600)

	if err := h.WriteFileAtomic(filePath, content, perm); err != nil {
		t.Fatalf("WriteFileAtomic failed: %v", err)
	}

	read, err := h.ReadFile(filePath)
	if err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}
	if string(read) != string(content) {
		t.Errorf("read %q, want %q", string(read), string(content))
	}

	info, err := h.Stat(filePath)
	if err != nil {
		t.Fatalf("Stat failed: %v", err)
	}
	if info.Mode().Perm() != perm {
		t.Errorf("got mode %o, want %o", info.Mode().Perm(), perm)
	}
}

func TestRealHost_RunCommand(t *testing.T) {
	h := host.NewRealHost()
	ctx := context.Background()

	out, err := h.RunCommand(ctx, "echo", "dermaga")
	if err != nil {
		t.Fatalf("RunCommand failed: %v", err)
	}
	if string(out) != "dermaga\n" {
		t.Errorf("got output %q, want %q", string(out), "dermaga\n")
	}
}

func TestFakeHost(t *testing.T) {
	fh := host.NewFakeHost()
	ctx := context.Background()

	_ = fh.WriteFileAtomic("/etc/test.conf", []byte("conf-data"), 0644)
	data, err := fh.ReadFile("/etc/test.conf")
	if err != nil || string(data) != "conf-data" {
		t.Errorf("expected conf-data, got %s (err %v)", string(data), err)
	}

	_, err = fh.RunCommand(ctx, "systemctl", "--user", "status", "test")
	if err != nil {
		t.Fatalf("RunCommand failed: %v", err)
	}
	if len(fh.Commands) != 1 || fh.Commands[0].Name != "systemctl" {
		t.Errorf("command not recorded properly")
	}
}
