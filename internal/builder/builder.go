package builder

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"strconv"
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
	Memory            string   // optional override for DEP-7
	CPUs              string   // optional override for DEP-7
	Timeout           time.Duration
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

	timeout := b.cfg.Timeout
	if req.Timeout > 0 {
		timeout = req.Timeout
	}
	mem := b.cfg.Memory
	if req.Memory != "" {
		mem = req.Memory
	}
	cpus := b.cfg.CPUs
	if req.CPUs != "" {
		cpus = req.CPUs
	}

	cpuQuota, err := cpuQuotaMicros(cpus)
	if err != nil {
		return "", err
	}
	if err := ValidateMemoryLimit(mem); err != nil {
		return "", err
	}

	buildCtx, cancel := context.WithTimeout(ctx, timeout)
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
		"--memory", mem,
		"--cpu-period", "100000",
		"--cpu-quota", strconv.FormatInt(cpuQuota, 10),
		"-f", containerfileAbs,
		"-t", imageRef,
	}
	var buildProcEnv []string
	for k, v := range req.BuildEnv {
		args = append(args, "--build-arg", k)
		buildProcEnv = append(buildProcEnv, k+"="+v)
	}
	args = append(args, contextAbs)

	emit(fmt.Sprintf("==> Building image %s (memory=%s, cpus=%s)", imageRef, mem, cpus))

	var lineBuf strings.Builder
	sm := NewStreamMasker(masker, func(chunk string) {
		lineBuf.WriteString(chunk)
		for {
			s := lineBuf.String()
			idx := strings.IndexByte(s, '\n')
			if idx < 0 {
				break
			}
			line := s[:idx]
			lineBuf.Reset()
			lineBuf.WriteString(s[idx+1:])
			if line != "" {
				b.logs.Append(req.DeploymentID, line)
			}
		}
	})
	err = b.host.StreamCommandWithEnv(buildCtx, buildProcEnv, func(line string) {
		_, _ = sm.Write([]byte(line + "\n"))
	}, "podman", args...)
	sm.Flush()
	if rem := strings.TrimRight(lineBuf.String(), "\n"); rem != "" {
		b.logs.Append(req.DeploymentID, rem)
	}

	if err != nil {
		if errors.Is(ctx.Err(), context.Canceled) {
			return "", context.Canceled
		}
		if errors.Is(buildCtx.Err(), context.DeadlineExceeded) {
			return "", fmt.Errorf("build timed out after %s", timeout)
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

var memoryLimitRE = regexp.MustCompile(`^[1-9][0-9]*[bkmgBKMG]?$`)

// ValidateMemoryLimit validates a Podman memory limit string (e.g. "2g", "512m").
func ValidateMemoryLimit(mem string) error {
	if !memoryLimitRE.MatchString(strings.TrimSpace(mem)) {
		return fmt.Errorf("invalid build memory limit %q (expected positive number with optional b/k/m/g suffix)", mem)
	}
	return nil
}

// ValidateTimeoutSec validates a positive build timeout in seconds (DEP-7).
func ValidateTimeoutSec(raw string) (time.Duration, error) {
	secs, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || secs <= 0 {
		return 0, fmt.Errorf("invalid build timeout %q (must be a positive integer in seconds)", raw)
	}
	return time.Duration(secs) * time.Second, nil
}

func cpuQuotaMicros(cpus string) (int64, error) {
	v, err := strconv.ParseFloat(strings.TrimSpace(cpus), 64)
	if err != nil || v <= 0 {
		return 0, fmt.Errorf("invalid build CPU limit %q (must be a positive number)", cpus)
	}
	q := int64(v * 100000)
	if q < 1000 {
		q = 1000
	}
	return q, nil
}
