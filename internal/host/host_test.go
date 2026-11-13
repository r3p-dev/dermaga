package host

import (
	"net"
	"os"
	"path/filepath"
	"testing"

	_ "github.com/r3p-dev/dermaga/internal/golden" // accepts go test ./... -update
)

func TestWriteFileReplacesAtomically(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "app-shop.yml")
	for _, s := range []string{"old", "new"} {
		if err := WriteFile(p, []byte(s), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	b, _ := os.ReadFile(p)
	fi, _ := os.Stat(p)
	entries, _ := os.ReadDir(dir)
	if string(b) != "new" || fi.Mode().Perm() != 0o600 || len(entries) != 1 {
		t.Fatalf("content %q mode %v files %d (temp file left behind?)", b, fi.Mode().Perm(), len(entries))
	}
}

func TestPublicIP(t *testing.T) {
	for ip, want := range map[string]bool{
		"203.0.113.7": true, "8.8.8.8": true, "2001:db8::1": true,
		"192.168.122.217": false, "10.0.0.5": false, "172.16.4.1": false, "100.64.1.2": false,
		"127.0.0.1": false, "169.254.1.1": false, "fd00::1": false, "fe80::1": false,
	} {
		if got := publicIP(net.ParseIP(ip)); got != want {
			t.Errorf("publicIP(%s) = %v, want %v", ip, got, want)
		}
	}
}
