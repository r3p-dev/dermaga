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

	// Test FakeHost ReadDir
	_ = fh.WriteFileAtomic("/etc/foo.conf", []byte("foo"), 0644)
	_ = fh.WriteFileAtomic("/etc/bar.conf", []byte("bar"), 0644)
	_ = fh.WriteFileAtomic("/var/other.conf", []byte("other"), 0644)

	entries, err := fh.ReadDir("/etc")
	if err != nil {
		t.Fatalf("FakeHost ReadDir failed: %v", err)
	}
	if len(entries) != 3 { // test.conf, foo.conf, bar.conf
		t.Fatalf("expected 3 entries in /etc, got %d", len(entries))
	}
	if entries[0].Name() != "bar.conf" || entries[1].Name() != "foo.conf" || entries[2].Name() != "test.conf" {
		t.Errorf("unexpected entry names or order: %v, %v, %v", entries[0].Name(), entries[1].Name(), entries[2].Name())
	}
}

func TestRealHost_ReadDir(t *testing.T) {
	tmpDir := t.TempDir()
	h := host.NewRealHost()

	if err := h.WriteFileAtomic(filepath.Join(tmpDir, "b.txt"), []byte("b"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := h.WriteFileAtomic(filepath.Join(tmpDir, "a.txt"), []byte("a"), 0644); err != nil {
		t.Fatal(err)
	}

	entries, err := h.ReadDir(tmpDir)
	if err != nil {
		t.Fatalf("ReadDir failed: %v", err)
	}
	if len(entries) != 2 || entries[0].Name() != "a.txt" || entries[1].Name() != "b.txt" {
		t.Errorf("unexpected ReadDir result: %v", entries)
	}
}
