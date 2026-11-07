package config

import (
	_ "github.com/r3p-dev/dermaga/internal/golden" // accepts go test ./... -update
	"os"
	"path/filepath"
	"testing"
)

func TestLoad(t *testing.T) {
	dir := t.TempDir()
	write := func(s string) string {
		p := filepath.Join(dir, "config.toml")
		if err := os.WriteFile(p, []byte(s), 0o600); err != nil {
			t.Fatal(err)
		}
		return p
	}

	c, err := Load(filepath.Join(dir, "missing.toml"))
	if err != nil || c.Panel.Port != 7070 {
		t.Fatalf("missing file: %+v %v", c, err)
	}
	c, err = Load(write("[panel]\nport = 7171\n"))
	if err != nil || c.Panel.Port != 7171 {
		t.Fatalf("port override: %+v %v", c, err)
	}
	for _, bad := range []string{"[panel]\nprot = 7171\n", "[panel]\nport = 0\n", "[panel]\nport = 70000\n", "port = "} {
		if _, err := Load(write(bad)); err == nil {
			t.Errorf("accepted %q", bad)
		}
	}
}
