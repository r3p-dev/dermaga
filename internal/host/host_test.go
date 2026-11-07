package host

import (
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
