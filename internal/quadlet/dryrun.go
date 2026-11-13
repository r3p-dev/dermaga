package quadlet

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Bin is Debian's Quadlet generator.
const Bin = "/usr/libexec/podman/quadlet"

// MinPodman is the oldest Podman dermaga supports: the version Debian 13
// ships. The newest Quadlet key dermaga writes, DefaultDependencies=, needs
// 5.3.0 (StopTimeout= needs 5.0.0); older Quadlets reject the units.
const MinPodman = "5.4.2"

// DryRun validates files (name → content) the way Quadlet will see them once
// installed: it stages a copy of dir's top-level files, overlays files, and
// runs quadlet -dryrun -user on the copy (ARCHITECTURE D5). A rejected unit
// would otherwise silently get no .service. dir may be "" or missing.
func DryRun(ctx context.Context, dir string, files map[string][]byte) error {
	stage, err := os.MkdirTemp("", "dermaga-quadlet-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)
	if dir != "" {
		entries, err := os.ReadDir(dir)
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
		for _, e := range entries {
			if !e.Type().IsRegular() {
				continue
			}
			b, err := os.ReadFile(filepath.Join(dir, e.Name()))
			if err != nil {
				return err
			}
			if err := os.WriteFile(filepath.Join(stage, e.Name()), b, 0o600); err != nil {
				return err
			}
		}
	}
	for n, b := range files {
		if n != filepath.Base(n) || strings.HasPrefix(n, ".") {
			return fmt.Errorf("unit file name %q: not a plain file name", n)
		}
		if err := os.WriteFile(filepath.Join(stage, n), b, 0o600); err != nil {
			return err
		}
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, Bin, "-dryrun", "-user")
	cmd.Env = append(os.Environ(), "QUADLET_UNIT_DIRS="+stage)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		var msgs []string
		for _, l := range strings.Split(stderr.String(), "\n") {
			if l != "" && !strings.Contains(l, "Loading source unit file") {
				msgs = append(msgs, l)
			}
		}
		return fmt.Errorf("quadlet rejected the units (%v): %s", err, strings.Join(msgs, "; "))
	}
	return nil
}
