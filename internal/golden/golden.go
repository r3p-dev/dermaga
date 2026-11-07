// Package golden compares generator output with files in testdata/.
// `go test ./... -update` rewrites them. go test passes -update to every test
// binary, so every package with tests imports this one (blank import if it
// has no golden files), or its tests fail on an unknown flag.
package golden

import (
	"bytes"
	"flag"
	"os"
	"path/filepath"
	"testing"
)

var update = flag.Bool("update", false, "rewrite golden files in testdata/")

// Check compares got with testdata/<name>, or writes it with -update.
func Check(t *testing.T, name string, got []byte) {
	t.Helper()
	path := filepath.Join("testdata", name)
	if *update {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v (run go test ./... -update to create it)", err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("%s differs from the generated output (go test ./... -update to accept):\n--- want\n%s\n--- got\n%s", path, want, got)
	}
}
