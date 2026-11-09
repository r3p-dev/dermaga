package backup

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/r3p-dev/dermaga/internal/config"
	"github.com/r3p-dev/dermaga/internal/host"
)

// SystemHost implements Host using the real operating system and executables.
type SystemHost struct {
	Paths config.Paths
}

func NewSystemHost(paths config.Paths) SystemHost {
	return SystemHost{Paths: paths}
}

func (s SystemHost) Run(ctx context.Context, timeout time.Duration, argv ...string) (string, error) {
	return host.Run(ctx, timeout, argv...)
}

func (s SystemHost) RunStdin(ctx context.Context, timeout time.Duration, stdin io.Reader, argv ...string) (string, error) {
	return host.RunStdin(ctx, timeout, stdin, argv...)
}

func (s SystemHost) RunEnv(ctx context.Context, timeout time.Duration, env []string, log io.Writer, argv ...string) (string, error) {
	return host.RunEnv(ctx, timeout, env, log, argv...)
}

func (s SystemHost) RunRestic(ctx context.Context, env []string, stdin io.Reader, stdout io.Writer, argv ...string) (string, error) {
	fullEnv := append(os.Environ(), env...)
	var outBuf, errBuf bytes.Buffer
	var outWriter io.Writer = &outBuf
	if stdout != nil {
		outWriter = io.MultiWriter(stdout, &outBuf)
	}
	cmd := exec.CommandContext(ctx, "restic", argv...)
	cmd.Env = fullEnv
	cmd.Stdin = stdin
	cmd.Stdout = outWriter
	cmd.Stderr = &errBuf
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(errBuf.String())
		if len(msg) > 2000 {
			msg = "…" + msg[len(msg)-2000:]
		}
		return "", fmt.Errorf("restic %s: %w: %s", strings.Join(argv, " "), err, msg)
	}
	return strings.TrimSpace(outBuf.String()), nil
}

func (s SystemHost) WriteUserUnit(filename string, data []byte) error {
	dir := s.Paths.SystemdUserDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	return host.WriteFile(filepath.Join(dir, filename), data, 0o644)
}

func (s SystemHost) RemoveUserUnit(filename string) error {
	err := os.Remove(filepath.Join(s.Paths.SystemdUserDir(), filename))
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

func (s SystemHost) ReloadSystemd(ctx context.Context) error {
	_, err := host.Run(ctx, 30*time.Second, "systemctl", "--user", "daemon-reload")
	return err
}

func (s SystemHost) EnableTimer(ctx context.Context, timerUnit string) error {
	_, err := host.Run(ctx, 30*time.Second, "systemctl", "--user", "enable", "--now", timerUnit)
	return err
}

func (s SystemHost) DisableTimer(ctx context.Context, timerUnit string) error {
	_, _ = host.Run(ctx, 30*time.Second, "systemctl", "--user", "stop", timerUnit)
	_, err := host.Run(ctx, 30*time.Second, "systemctl", "--user", "disable", timerUnit)
	return err
}

func (s SystemHost) ActiveState(ctx context.Context, unit string) (string, error) {
	return host.Host{}.ActiveState(ctx, unit)
}

func (s SystemHost) ListTimers(ctx context.Context) (string, error) {
	return host.Run(ctx, 30*time.Second, "systemctl", "--user", "list-timers", "--all")
}

func (s SystemHost) ReadFile(path string) ([]byte, error) {
	return os.ReadFile(path)
}

func (s SystemHost) WriteFile(path string, data []byte, mode os.FileMode) error {
	return host.WriteFile(path, data, mode)
}

func (s SystemHost) Remove(path string) error {
	return os.Remove(path)
}
