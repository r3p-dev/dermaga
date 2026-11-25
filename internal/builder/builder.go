package builder

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"dermaga/internal/host"
	"dermaga/internal/name"
)

// BuildConfig configures mirror/build directories and podman build limits (DEP-7).
type BuildConfig struct {
	MirrorsDir string
	BuildsDir  string
	Memory     string        // e.g. "2g"
	CPUs       string        // e.g. "2"
	Timeout    time.Duration // default 20m
}

// BuildRequest describes a single image build from a GitHub repository.
type BuildRequest struct {
	AppName           string
	RepoOwner         string
	RepoName          string
	CommitRef         string // branch or commit SHA
	ReleaseNumber     int
	DeploymentID      string
	ContainerfilePath string
	BuildContext      string
	InstallToken      string // in-memory repo-scoped token (GHA-5)
	BuildEnv          map[string]string
	RuntimeSecrets    []string // runtime values to also mask in build logs (SEC-9)
}

// Builder executes git mirror fetches, archive exports, and podman builds via host.Host.
type Builder struct {
	host host.Host
	cfg  BuildConfig
	logs *LogHub
}

// New creates a Builder.
func New(h host.Host, cfg BuildConfig, logs *LogHub) *Builder {
	if cfg.Memory == "" {
		cfg.Memory = "2g"
	}
	if cfg.CPUs == "" {
		cfg.CPUs = "2"
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 20 * time.Minute
	}
	if logs == nil {
		logs = NewLogHub()
	}
	return &Builder{host: h, cfg: cfg, logs: logs}
}

// Logs returns the LogHub used by this Builder.
func (b *Builder) Logs() *LogHub {
	return b.logs
}

// ImageRef returns the local OCI image tag for an app release.
func ImageRef(appName string, releaseNumber int) string {
	return fmt.Sprintf("localhost/%s/%s:r%d", name.ProjectName, appName, releaseNumber)
}

// Build fetches the repository mirror, exports the commit, runs podman build with limits,
// and returns the built image reference. Build context is always cleaned up afterwards.
func (b *Builder) Build(ctx context.Context, req BuildRequest) (string, error) {
	if req.AppName == "" || req.DeploymentID == "" || req.ReleaseNumber <= 0 {
		return "", errors.New("invalid build request: missing app name, deployment ID, or release number")
	}

	secrets := make([]string, 0, 1+len(req.BuildEnv)+len(req.RuntimeSecrets))
	if req.InstallToken != "" {
		secrets = append(secrets, req.InstallToken)
	}
	for _, v := range req.BuildEnv {
		secrets = append(secrets, v)
	}
	secrets = append(secrets, req.RuntimeSecrets...)
	masker := NewMasker(secrets)

	emit := func(line string) {
		b.logs.Append(req.DeploymentID, masker.MaskString(line))
	}

	buildCtx, cancel := context.WithTimeout(ctx, b.cfg.Timeout)
	defer cancel()

	if err := b.host.MkdirAll(b.cfg.MirrorsDir, 0700); err != nil {
		return "", fmt.Errorf("creating mirrors dir: %w", err)
	}

	exportDir := filepath.Join(b.cfg.BuildsDir, req.AppName, req.DeploymentID)
	if err := b.host.MkdirAll(exportDir, 0700); err != nil {
		return "", fmt.Errorf("creating build export dir: %w", err)
	}
	defer func() {
		_ = b.host.RemoveAll(exportDir)
	}()

	containerfileAbs, contextAbs, err := resolveBuildPaths(exportDir, req.ContainerfilePath, req.BuildContext)
	if err != nil {
		return "", err
	}

	mirrorDir := filepath.Join(b.cfg.MirrorsDir, req.AppName+".git")
	repoURL := fmt.Sprintf("https://github.com/%s/%s.git", req.RepoOwner, req.RepoName)
	gitEnv := gitAuthEnv(req.InstallToken)

	emit(fmt.Sprintf("==> Fetching %s/%s (%s)", req.RepoOwner, req.RepoName, req.CommitRef))
	if _, statErr := b.host.Stat(mirrorDir); statErr != nil {
		if _, err := b.host.RunCommandWithEnv(buildCtx, gitEnv, "git", "clone", "--mirror", repoURL, mirrorDir); err != nil {
			return "", fmt.Errorf("cloning mirror: %s", masker.MaskString(err.Error()))
		}
	} else {
		if _, err := b.host.RunCommandWithEnv(buildCtx, gitEnv, "git", "-C", mirrorDir, "fetch", "--prune", "origin"); err != nil {
			return "", fmt.Errorf("fetching mirror: %s", masker.MaskString(err.Error()))
		}
	}

	tarPath := filepath.Join(exportDir, ".export.tar")
	if _, err := b.host.RunCommand(buildCtx, "git", "-C", mirrorDir, "archive", "--format=tar", "--output="+tarPath, req.CommitRef); err != nil {
		return "", fmt.Errorf("exporting archive for %s: %s", req.CommitRef, masker.MaskString(err.Error()))
	}
	if _, err := b.host.RunCommand(buildCtx, "tar", "-xf", tarPath, "-C", exportDir); err != nil {
		return "", fmt.Errorf("extracting build context: %s", masker.MaskString(err.Error()))
	}
	_ = b.host.Remove(tarPath)

	imageRef := ImageRef(req.AppName, req.ReleaseNumber)
	args := []string{
		"build",
		"--memory", b.cfg.Memory,
		"--cpus", b.cfg.CPUs,
		"-f", containerfileAbs,
		"-t", imageRef,
	}
	for k, v := range req.BuildEnv {
		args = append(args, "--build-arg", k+"="+v)
	}
	args = append(args, contextAbs)

	emit(fmt.Sprintf("==> Building image %s (memory=%s, cpus=%s)", imageRef, b.cfg.Memory, b.cfg.CPUs))

	sm := NewStreamMasker(masker, func(chunk string) {
		for _, line := range strings.Split(strings.TrimRight(chunk, "\n"), "\n") {
			if line != "" {
				b.logs.Append(req.DeploymentID, line)
			}
		}
	})
	err = b.host.StreamCommandWithEnv(buildCtx, nil, func(line string) {
		_, _ = sm.Write([]byte(line + "\n"))
	}, "podman", args...)
	sm.Flush()

	if err != nil {
		if errors.Is(ctx.Err(), context.Canceled) {
			return "", context.Canceled
		}
		if errors.Is(buildCtx.Err(), context.DeadlineExceeded) {
			return "", fmt.Errorf("build timed out after %s", b.cfg.Timeout)
		}
		return "", fmt.Errorf("podman build failed: %s", masker.MaskString(err.Error()))
	}

	emit("==> Image built: " + imageRef)
	return imageRef, nil
}

// gitAuthEnv passes the GitHub App installation token via GIT_CONFIG_* environment variables,
// keeping the token out of URLs, .git/config, command arguments, and logs (GHA-5).
func gitAuthEnv(token string) []string {
	env := []string{"GIT_TERMINAL_PROMPT=0"}
	if token == "" {
		return env
	}
	cred := base64.StdEncoding.EncodeToString([]byte("x-access-token:" + token))
	return append(env,
		"GIT_CONFIG_COUNT=1",
		"GIT_CONFIG_KEY_0=http.https://github.com/.extraheader",
		"GIT_CONFIG_VALUE_0=AUTHORIZATION: basic "+cred,
	)
}

func resolveBuildPaths(exportDir, containerfilePath, buildContext string) (string, string, error) {
	if containerfilePath == "" {
		containerfilePath = "Containerfile"
	}
	if buildContext == "" {
		buildContext = "."
	}
	cleanExport := filepath.Clean(exportDir)
	cfAbs := filepath.Clean(filepath.Join(cleanExport, containerfilePath))
	ctxAbs := filepath.Clean(filepath.Join(cleanExport, buildContext))

	if cfAbs == cleanExport || !strings.HasPrefix(cfAbs, cleanExport+string(filepath.Separator)) {
		return "", "", fmt.Errorf("containerfile path %q escapes build directory", containerfilePath)
	}
	if ctxAbs != cleanExport && !strings.HasPrefix(ctxAbs, cleanExport+string(filepath.Separator)) {
		return "", "", fmt.Errorf("build context %q escapes build directory", buildContext)
	}
	return cfAbs, ctxAbs, nil
}
