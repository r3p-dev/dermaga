package deploy

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/r3p-dev/dermaga/internal/apps"
	"github.com/r3p-dev/dermaga/internal/config"
	"github.com/r3p-dev/dermaga/internal/gitsrc"
	"github.com/r3p-dev/dermaga/internal/host"
	"github.com/r3p-dev/dermaga/internal/name"
	"github.com/r3p-dev/dermaga/internal/quadlet"
	"github.com/r3p-dev/dermaga/internal/traefik"
)

// System is the real Host: git, podman and systemctl --user.
type System struct {
	host.Host
	Paths config.Paths
	Self  string // this binary, run as `dermaga probe` inside the rootless netns (D2)
	// Build limits (BRIEF §3.5 step 4).
	BuildMemory  string
	BuildCPUs    int
	BuildTimeout time.Duration
}

// NewSystem returns a System with the brief's default build limits.
func NewSystem(p config.Paths, self string) System {
	return System{
		Host:  host.Host{QuadletDir: p.QuadletDir, DynamicDir: p.DynamicDir()},
		Paths: p, Self: self, BuildMemory: "2g", BuildCPUs: 2, BuildTimeout: 20 * time.Minute,
	}
}

// ImageRepo is where a git app's images live: localhost/dermaga/<app>:<sha12>.
func ImageRepo(app string) string { return "localhost/" + name.Name + "/" + app }

func (s System) Prepare(ctx context.Context, a apps.App, ref, rel8 string, log io.Writer) (Prepared, error) {
	src := a.Spec.Source
	if src.Type == "image" {
		fmt.Fprintf(log, "==> pulling %s\n", src.Image)
		if _, err := host.RunEnv(ctx, 15*time.Minute, nil, log, "podman", "pull", "--", src.Image); err != nil {
			return Prepared{}, err
		}
		hc, err := s.imageHealthcheck(ctx, src.Image)
		return Prepared{Image: src.Image, Healthcheck: hc}, err
	}

	repo := gitsrc.Repo{URL: src.Repo, Mirror: s.Paths.Mirror(a.Spec.Name), Key: s.Paths.DeployKey(a.Spec.Name), KnownHosts: s.Paths.KnownHosts()}
	if _, err := os.Stat(repo.KnownHosts); err != nil {
		return Prepared{}, fmt.Errorf("%s missing: run `%s github known-hosts` first", repo.KnownHosts, name.Name)
	}
	fmt.Fprintf(log, "==> fetching %s\n", src.Repo)
	if err := repo.Fetch(ctx, log); err != nil {
		return Prepared{}, fmt.Errorf("fetching %s (is the deploy key added to the repo?): %w", src.Repo, err)
	}
	sha, subject, err := repo.Resolve(ctx, src.Branch, ref)
	if err != nil {
		return Prepared{}, err
	}
	fmt.Fprintf(log, "==> commit %s %s\n", sha[:12], subject)
	p := Prepared{Image: ImageRepo(a.Spec.Name) + ":" + sha[:12], SHA: sha, Subject: subject}
	// The commit alone doesn't define the image: a changed build setting must rebuild it.
	build := "context=" + src.Context + ";containerfile=" + src.Containerfile
	if got, err := host.Run(ctx, cmdTimeout, "podman", "image", "inspect", "--format", `{{index .Labels "`+buildLabel+`"}}`, "--", p.Image); err == nil && got == build {
		fmt.Fprintf(log, "==> image %s already built with these settings, not rebuilding\n", p.Image)
		p.Healthcheck, err = s.imageHealthcheck(ctx, p.Image)
		return p, err
	}

	tmp := filepath.Join(s.Paths.StateDir, "tmp")
	if err := os.MkdirAll(tmp, 0o700); err != nil {
		return Prepared{}, err
	}
	dir, err := os.MkdirTemp(tmp, "build-"+a.Spec.Name+"-")
	if err != nil {
		return Prepared{}, err
	}
	defer os.RemoveAll(dir)
	srcDir := filepath.Join(dir, "src")
	if err := repo.Export(ctx, sha, srcDir); err != nil {
		return Prepared{}, fmt.Errorf("exporting %s: %w", sha[:12], err)
	}
	ctxDir := filepath.Join(srcDir, src.Context) // spec-validated: relative, no ..
	cf := src.Containerfile
	if cf == "" {
		cf = "Containerfile"
		if _, err := os.Stat(filepath.Join(ctxDir, cf)); errors.Is(err, fs.ErrNotExist) {
			cf = "Dockerfile"
		}
	}
	if _, err := os.Stat(filepath.Join(ctxDir, cf)); err != nil {
		return Prepared{}, fmt.Errorf("no %s in %s/%s of commit %s", cf, src.RepoFullName(), src.Context, sha[:12])
	}
	quota := strconv.Itoa(s.BuildCPUs * 100000)
	fmt.Fprintf(log, "==> building %s (memory %s, %d CPUs, timeout %s)\n", p.Image, s.BuildMemory, s.BuildCPUs, s.BuildTimeout)
	if _, err := host.RunEnv(ctx, s.BuildTimeout, nil, log, "podman", "build",
		"--memory", s.BuildMemory, "--memory-swap", s.BuildMemory, "--cpu-period", "100000", "--cpu-quota", quota,
		"--pull=missing", "--file", filepath.Join(ctxDir, cf), "--tag", p.Image,
		"--label", "org.opencontainers.image.source=https://github.com/"+src.RepoFullName(),
		"--label", "org.opencontainers.image.revision="+sha,
		"--label", buildLabel+"="+build,
		"--", ctxDir); err != nil {
		return Prepared{}, fmt.Errorf("build: %w", err)
	}
	p.Healthcheck, err = s.imageHealthcheck(ctx, p.Image)
	return p, err
}

const (
	cmdTimeout = 30 * time.Second
	buildLabel = name.Name + ".build" // build settings an image was built with
)

func (s System) imageHealthcheck(ctx context.Context, image string) (bool, error) {
	out, err := host.Run(ctx, cmdTimeout, "podman", "image", "inspect", "--format", "{{if .HealthCheck}}yes{{end}}", "--", image)
	return out == "yes", err
}

func (s System) InstallUnits(ctx context.Context, files map[string][]byte) error {
	if err := quadlet.DryRun(ctx, s.QuadletDir, files); err != nil {
		return err
	}
	if err := os.MkdirAll(s.QuadletDir, 0o755); err != nil {
		return err
	}
	for n, b := range files {
		if err := host.WriteFile(filepath.Join(s.QuadletDir, n), b, 0o644); err != nil {
			return err
		}
	}
	return s.DaemonReload(ctx)
}

func (s System) WriteEnv(app string, data []byte) ([]byte, error) {
	p := s.Paths.EnvFile(app)
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return nil, err
	}
	prev, err := os.ReadFile(p)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}
	return prev, host.WriteFile(p, data, 0o600)
}

func (s System) Stop(ctx context.Context, unit string) error {
	_, err := host.Run(ctx, 90*time.Second, "systemctl", "--user", "stop", unit)
	return err
}

// Healthy waits until c passes. A unit that fails or restarts ends the wait
// at once instead of at the deadline.
func (s System) Healthy(ctx context.Context, c Check, log io.Writer) error {
	ctx, cancel := context.WithTimeout(ctx, c.Timeout)
	defer cancel()
	errc := make(chan error, 2)
	go func() { errc <- s.watch(ctx, c.Unit) }()
	go func() {
		switch {
		case c.Path != "":
			errc <- s.probe(ctx, c, log)
		case c.Notify:
			errc <- s.until(ctx, c.Unit, func(state string, _ int) bool { return state == "active" }, 1)
		default:
			errc <- s.until(ctx, c.Unit, func(state string, n int) bool { return state == "active" && n == 0 }, 10)
		}
	}()
	err := <-errc
	if err != nil && ctx.Err() != nil {
		return fmt.Errorf("not healthy within %s", c.Timeout)
	}
	return err
}

func (s System) unitState(ctx context.Context, unit string) (string, int, error) {
	out, err := host.Run(ctx, cmdTimeout, "systemctl", "--user", "show", "-p", "ActiveState", "-p", "NRestarts", unit)
	if err != nil {
		return "", 0, err
	}
	var state string
	var n int
	for _, l := range strings.Split(out, "\n") {
		if v, ok := strings.CutPrefix(l, "ActiveState="); ok {
			state = v
		}
		if v, ok := strings.CutPrefix(l, "NRestarts="); ok {
			n, _ = strconv.Atoi(v)
		}
	}
	return state, n, nil
}

// watch returns an error as soon as the unit fails or has restarted.
func (s System) watch(ctx context.Context, unit string) error {
	for {
		state, n, err := s.unitState(ctx, unit)
		if err == nil && (state == "failed" || n > 0) {
			return fmt.Errorf("%s %s after %d restarts (journalctl --user -u %s)", unit, state, n, unit)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Second):
		}
	}
}

// until polls the unit once a second until ok holds for need consecutive polls.
func (s System) until(ctx context.Context, unit string, ok func(string, int) bool, need int) error {
	for streak := 0; ; {
		state, n, err := s.unitState(ctx, unit)
		if err == nil && ok(state, n) {
			if streak++; streak >= need {
				return nil
			}
		} else {
			streak = 0
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Second):
		}
	}
}

// probe runs `dermaga probe` inside the rootless netns, where container names
// resolve through aardvark-dns at the web network's gateway (D2).
func (s System) probe(ctx context.Context, c Check, log io.Writer) error {
	gw, err := host.Run(ctx, cmdTimeout, "podman", "network", "inspect", "web", "--format", "{{range .Subnets}}{{.Gateway}}{{end}}")
	if err != nil {
		return err
	}
	dl, _ := ctx.Deadline()
	_, err = host.RunEnv(ctx, time.Until(dl)+5*time.Second, nil, log, "podman", "unshare", "--rootless-netns", s.Self, "probe",
		"-url", fmt.Sprintf("http://%s:%d%s", c.Container, c.Port, c.Path), "-dns", gw+":53",
		"-successes", strconv.Itoa(c.Successes), "-interval", "1s", "-timeout", time.Until(dl).Round(time.Second).String())
	return err
}

// Verify runs `dermaga probe` against Traefik's verify entrypoint, by the
// Traefik container's name on the web network (D7).
func (s System) Verify(ctx context.Context, appHost, path, token string, anyStatus bool, log io.Writer) error {
	gw, err := host.Run(ctx, cmdTimeout, "podman", "network", "inspect", "web", "--format", "{{range .Subnets}}{{.Gateway}}{{end}}")
	if err != nil {
		return err
	}
	args := []string{"podman", "unshare", "--rootless-netns", s.Self, "probe",
		"-url", fmt.Sprintf("http://traefik:%d%s", traefik.VerifyPort, path), "-dns", gw + ":53",
		"-host", appHost, "-header", traefik.VerifyHeader + ": " + token,
		"-successes", "1", "-interval", "250ms", "-timeout", VerifyTimeout.String()}
	if anyStatus {
		args = append(args, "-any-status")
	}
	_, err = host.RunEnv(ctx, VerifyTimeout+10*time.Second, nil, log, args...)
	return err
}

func (s System) ImageHealthcheck(ctx context.Context, image string) (bool, error) {
	return s.imageHealthcheck(ctx, image)
}

// PruneImages keeps the images in keep and removes older localhost/dermaga/<app> images (BRIEF §3.5 step 8).
func (s System) PruneImages(ctx context.Context, app string, keep []string) error {
	repo := ImageRepo(app)
	out, err := host.Run(ctx, cmdTimeout, "podman", "images", "--format", "{{.Repository}}:{{.Tag}}", "--filter", "reference="+repo)
	if err != nil {
		return err
	}
	keepMap := make(map[string]bool, len(keep))
	for _, k := range keep {
		keepMap[k] = true
	}
	var toRemove []string
	for _, img := range strings.Split(strings.TrimSpace(out), "\n") {
		img = strings.TrimSpace(img)
		if img == "" || keepMap[img] {
			continue
		}
		toRemove = append(toRemove, img)
	}
	if len(toRemove) > 0 {
		args := append([]string{"podman", "rmi", "--"}, toRemove...)
		_, err := host.Run(ctx, 2*time.Minute, args...)
		return err
	}
	return nil
}
