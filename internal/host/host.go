// Package host is dermaga's access to the machine: generated files written
// atomically, and systemctl --user / podman run as argument slices with
// timeouts (BRIEF §3.2). Never a shell.
package host

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/r3p-dev/dermaga/internal/name"
)

// WriteFile replaces path atomically: temp file in the same directory, fsync,
// rename, fsync of the directory. Readers (Traefik, systemd) see the old or
// the new content, never a partial file.
func WriteFile(path string, data []byte, mode fs.FileMode) error {
	dir := filepath.Dir(path)
	f, err := os.CreateTemp(dir, "."+name.Name+"-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name()) // no-op after the rename
	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err := f.Chmod(mode); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := os.Rename(f.Name(), path); err != nil {
		return err
	}
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}

// Run executes argv with a timeout and returns trimmed stdout. The error
// includes stderr.
func Run(ctx context.Context, timeout time.Duration, argv ...string) (string, error) {
	return RunStdinEnv(ctx, timeout, nil, nil, nil, argv...)
}

// RunStdin executes argv with stdin and returns trimmed stdout.
func RunStdin(ctx context.Context, timeout time.Duration, stdin io.Reader, argv ...string) (string, error) {
	return RunStdinEnv(ctx, timeout, stdin, nil, nil, argv...)
}

// RunEnv is Run with an environment (nil: inherit) and, if log != nil, stdout
// and stderr copied to log as they arrive.
func RunEnv(ctx context.Context, timeout time.Duration, env []string, log io.Writer, argv ...string) (string, error) {
	return RunStdinEnv(ctx, timeout, nil, env, log, argv...)
}

// RunStdinEnv is RunEnv with stdin support.
func RunStdinEnv(ctx context.Context, timeout time.Duration, stdin io.Reader, env []string, log io.Writer, argv ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	var out, errb bytes.Buffer
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.Env = env
	cmd.Stdin = stdin
	cmd.Stdout, cmd.Stderr = io.Writer(&out), io.Writer(&errb)
	if log != nil {
		cmd.Stdout, cmd.Stderr = io.MultiWriter(&out, log), io.MultiWriter(&errb, log)
	}
	if err := cmd.Run(); err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			err = fmt.Errorf("timed out after %s", timeout)
		}
		msg := strings.TrimSpace(errb.String())
		if len(msg) > 2000 {
			msg = "…" + msg[len(msg)-2000:]
		}
		cmdline := strings.Join(argv, " ")
		if len(cmdline) > 200 {
			cmdline = cmdline[:200] + "…"
		}
		return "", fmt.Errorf("%s: %w: %s", cmdline, err, msg)
	}
	return strings.TrimSpace(out.String()), nil
}

// Host is the real machine. It implements reconcile.Host.
type Host struct {
	QuadletDir string // ~/.config/containers/systemd
	DynamicDir string // ~/.config/traefik/dynamic
}

const cmdTimeout = 30 * time.Second

// Units lists the dg-*.container files in the Quadlet directory.
func (h Host) Units() ([]string, error) {
	return glob(h.QuadletDir, name.Unit+"*.container")
}

// AppFiles lists the app-*.yml files in Traefik's dynamic directory.
func (h Host) AppFiles() ([]string, error) {
	return glob(h.DynamicDir, name.AppFile+"*.yml")
}

func glob(dir, pattern string) ([]string, error) {
	m, err := filepath.Glob(filepath.Join(dir, pattern))
	if err != nil {
		return nil, err
	}
	for i := range m {
		m[i] = filepath.Base(m[i])
	}
	return m, nil
}

// ActiveState is systemd's ActiveState of a user unit (active, activating,
// inactive, failed, …), or "queued" for an inactive unit with a pending job,
// as at boot before systemd gets to it.
func (h Host) ActiveState(ctx context.Context, unit string) (string, error) {
	out, err := Run(ctx, cmdTimeout, "systemctl", "--user", "show", "-p", "ActiveState", "-p", "Job", unit)
	if err != nil {
		return "", err
	}
	var state, job string
	for _, l := range strings.Split(out, "\n") {
		if v, ok := strings.CutPrefix(l, "ActiveState="); ok {
			state = v
		}
		if v, ok := strings.CutPrefix(l, "Job="); ok {
			job = v
		}
	}
	if state == "inactive" && job != "" {
		return "queued", nil
	}
	return state, nil
}

// Start queues a start and returns at once (D3: the daemon owns every deadline).
func (h Host) Start(ctx context.Context, unit string) error {
	_, err := Run(ctx, cmdTimeout, "systemctl", "--user", "start", "--no-block", unit)
	return err
}

func (h Host) DaemonReload(ctx context.Context) error {
	_, err := Run(ctx, cmdTimeout, "systemctl", "--user", "daemon-reload")
	return err
}

// RemoveContainer removes a release in ARCHITECTURE D3's order: container
// first (bounded by stopTimeout), then the unit, then its file. The caller
// runs daemon-reload afterwards.
func (h Host) RemoveContainer(ctx context.Context, container string, stopTimeout int) error {
	if _, err := Run(ctx, time.Duration(stopTimeout+30)*time.Second,
		"podman", "rm", "--force", "--ignore", "--time", strconv.Itoa(stopTimeout), container); err != nil {
		return err
	}
	svc := container + ".service"
	// the unit may already be stopped or unknown to systemd: errors here change nothing
	Run(ctx, cmdTimeout, "systemctl", "--user", "stop", svc)
	Run(ctx, cmdTimeout, "systemctl", "--user", "reset-failed", svc)
	err := os.Remove(filepath.Join(h.QuadletDir, container+".container"))
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	return err
}

func (h Host) ReadAppFile(n string) ([]byte, error) {
	b, err := os.ReadFile(filepath.Join(h.DynamicDir, n))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	return b, err
}

func (h Host) WriteAppFile(n string, data []byte) error {
	// 0600: basic-auth hashes live here; Traefik reads as container root = this user
	return WriteFile(filepath.Join(h.DynamicDir, n), data, 0o600)
}

func (h Host) RemoveAppFile(n string) error {
	return os.Remove(filepath.Join(h.DynamicDir, n))
}
