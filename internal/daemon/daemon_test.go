package daemon

import (
	_ "github.com/r3p-dev/dermaga/internal/golden" // accepts go test ./... -update
	"os"
	"path/filepath"
	"testing"
)

func TestRefusesRoot(t *testing.T) {
	if refuseRoot(0) == nil {
		t.Fatal("uid 0 accepted")
	}
	if err := refuseRoot(1001); err != nil {
		t.Fatal(err)
	}
}

func TestListenUnixModeStaleAndRunning(t *testing.T) {
	path := filepath.Join(t.TempDir(), "d.sock")
	os.WriteFile(path, nil, 0o644) // stale leftover
	ln, err := listenUnix(path)
	if err != nil {
		t.Fatal(err)
	}
	fi, _ := os.Stat(path)
	if fi.Mode()&os.ModeSocket == 0 || fi.Mode().Perm() != 0o600 {
		t.Fatalf("mode %v", fi.Mode())
	}
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			c.Close()
		}
	}()
	if _, err := listenUnix(path); err == nil {
		t.Fatal("second daemon allowed while the first is running")
	}
	ln.Close()
}

func TestSameFileAndCopyFile(t *testing.T) {
	dir := t.TempDir()
	a, b := filepath.Join(dir, "a"), filepath.Join(dir, "b")
	os.WriteFile(a, []byte("binary v1"), 0o755)
	if sameFile(a, b) {
		t.Fatal("missing file reported equal")
	}
	if err := copyFile(a, b); err != nil {
		t.Fatal(err)
	}
	fi, _ := os.Stat(b)
	if !sameFile(a, b) || fi.Mode().Perm() != 0o755 {
		t.Fatalf("copy differs or mode %v", fi.Mode().Perm())
	}
	os.WriteFile(a, []byte("binary v2"), 0o755) // same size, different content
	if sameFile(a, b) {
		t.Fatal("different content reported equal")
	}
}
