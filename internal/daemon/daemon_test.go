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
