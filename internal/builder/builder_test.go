package builder

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"dermaga/internal/host"
)

func TestMasker_ChunkBoundaryAndLongestFirst_SEC9(t *testing.T) {
	m := NewMasker([]string{"secr", "secret-token-123"})
	var got strings.Builder
	sm := NewStreamMasker(m, func(s string) {
		got.WriteString(s)
	})

	chunks := []string{
		"step 1: using sec",
		"ret-token-123 in build\n",
		"also secr here\n",
	}
	for _, c := range chunks {
		if _, err := sm.Write([]byte(c)); err != nil {
			t.Fatalf("Write: %v", err)
		}
	}
	sm.Flush()

	out := got.String()
	if strings.Contains(out, "secret-token-123") || strings.Contains(out, "secr") {
		t.Fatalf("secret leaked across chunk boundary: %q", out)
	}
	if !strings.Contains(out, "using [MASKED] in build") {
		t.Fatalf("expected longest-first masking, got %q", out)
	}
}

// Values shorter than MinMaskedLength are not masked: "b2" turned the commit SHA in a real build
// log into "...b9ff[MASKED]" (M7a VM run). Four characters and more are still masked.
func TestMasker_ShortValuesNotMasked_SEC9(t *testing.T) {
	m := NewMasker([]string{"b2", "abc", "wxyz", "töp"})
	line := "==> Fetching r3p-dev/dermaga-test-app (f3810c7cbeeecf2662b4a4d176f1761475b9ffb2) abc wxyz töp"
	want := "==> Fetching r3p-dev/dermaga-test-app (f3810c7cbeeecf2662b4a4d176f1761475b9ffb2) abc [MASKED] töp"
	if got := m.MaskString(line); got != want {
		t.Fatalf("MaskString:\n got %q\nwant %q", got, want)
	}
	var streamed strings.Builder
	sm := NewStreamMasker(m, func(s string) { streamed.WriteString(s) })
	_, _ = sm.Write([]byte(line[:40]))
	_, _ = sm.Write([]byte(line[40:]))
	sm.Flush()
	if streamed.String() != want {
		t.Fatalf("stream:\n got %q\nwant %q", streamed.String(), want)
	}
}

func TestLineAccumulator_BoundedFlushAndSplitSecret_SEC9(t *testing.T) {
	secret := "top_secret_across_flush_boundary_999"
	m := NewMasker([]string{secret})
	var lines []string
	la := NewLineAccumulator(func(line string) {
		lines = append(lines, line)
	})
	sm := NewStreamMasker(m, la.Write)

	// Emit > 4096 bytes without any newline, with secret split across the 4096-byte boundary.
	prefix := strings.Repeat("a", maxLogLineBytes-10)
	_, _ = sm.Write([]byte(prefix + secret[:15]))
	_, _ = sm.Write([]byte(secret[15:] + strings.Repeat("b", 100)))
	sm.Flush()
	la.Flush()

	if len(lines) < 2 {
		t.Fatalf("expected long line without newline to flush at size bound, got %d line(s)", len(lines))
	}
	joined := strings.Join(lines, "")
	if strings.Contains(joined, secret) {
		t.Fatalf("secret leaked across bounded flush boundary")
	}
	if !strings.Contains(joined, "[MASKED]") {
		t.Fatalf("expected [MASKED] in flushed output")
	}
}

func TestBuilder_BuildAndMasking_GHA5_DEP7_DEP8(t *testing.T) {
	fh := host.NewFakeHost()
	fh.StreamMock = func(ctx context.Context, env []string, onLine func(string), name string, args ...string) error {
		onLine("STEP 1/2: FROM alpine:3.20")
		onLine("STEP 2/2: ENV TOKEN=ghs_secret_install_tok BUILD_KEY=super_build_secret")
		return nil
	}

	tmp := t.TempDir()
	b := New(fh, BuildConfig{
		MirrorsDir: filepath.Join(tmp, "mirrors"),
		BuildsDir:  filepath.Join(tmp, "builds"),
		Memory:     "2g",
		CPUs:       "2",
		Timeout:    5 * time.Minute,
	}, nil)

	img, err := b.Build(context.Background(), BuildRequest{
		AppName:           "portfolio",
		RepoOwner:         "octo",
		RepoName:          "portfolio",
		CommitRef:         "main",
		ReleaseNumber:     1,
		DeploymentID:      "dep_1",
		ContainerfilePath: "Containerfile",
		BuildContext:      ".",
		InstallToken:      "ghs_secret_install_tok",
		BuildEnv:          map[string]string{"PUBLIC_KEY": "super_build_secret"},
		RuntimeSecrets:    []string{"runtime_db_pass"},
	})
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}
	if img != "localhost/dermaga/portfolio:r1" {
		t.Fatalf("unexpected image ref: %q", img)
	}

	// Verify token never appeared in command args or logs.
	for _, c := range fh.Commands {
		joined := c.Name + " " + strings.Join(c.Args, " ")
		if strings.Contains(joined, "ghs_secret_install_tok") {
			t.Fatalf("install token leaked in command args: %s", joined)
		}
		if c.Name == "git" && (c.Args[0] == "clone" || c.Args[1] == "fetch") {
			if !strings.Contains(strings.Join(c.Env, "\n"), "GIT_CONFIG_COUNT=1") {
				t.Fatalf("expected GIT_CONFIG_COUNT=1 in git env, got %v", c.Env)
			}
		}
	}

	logs := b.Logs().Snapshot("dep_1")
	if strings.Contains(logs, "ghs_secret_install_tok") || strings.Contains(logs, "super_build_secret") {
		t.Fatalf("secret leaked in build logs: %s", logs)
	}
	if !strings.Contains(logs, "[MASKED]") {
		t.Fatalf("expected [MASKED] in build logs, got: %s", logs)
	}

	// Verify ephemeral build dir was cleaned up.
	exportDir := filepath.Join(tmp, "builds", "portfolio", "dep_1")
	if _, err := os.Stat(exportDir); !os.IsNotExist(err) {
		t.Fatalf("expected export dir to be cleaned up")
	}
}

func TestBuilder_PathTraversalRejected(t *testing.T) {
	fh := host.NewFakeHost()
	b := New(fh, BuildConfig{MirrorsDir: "/tmp/m", BuildsDir: "/tmp/b"}, nil)
	_, err := b.Build(context.Background(), BuildRequest{
		AppName:           "app",
		DeploymentID:      "dep_1",
		ReleaseNumber:     1,
		ContainerfilePath: "../outside/Containerfile",
		BuildContext:      ".",
	})
	if err == nil || !strings.Contains(err.Error(), "escapes build directory") {
		t.Fatalf("expected path traversal error, got %v", err)
	}
}

func TestBuilder_ValidateLimitsAndBuildArgEnv_DEP7_DEC31(t *testing.T) {
	for _, bad := range []string{"", "0", "-1g", "abc", "512x"} {
		if err := ValidateMemoryLimit(bad); err == nil {
			t.Fatalf("expected error for memory %q", bad)
		}
	}
	for _, bad := range []string{"", "0", "-5", "abc"} {
		if _, err := ValidateTimeoutSec(bad); err == nil {
			t.Fatalf("expected error for timeout %q", bad)
		}
		if _, err := cpuQuotaMicros(bad); err == nil {
			t.Fatalf("expected error for cpus %q", bad)
		}
	}
	q, err := cpuQuotaMicros("1.5")
	if err != nil || q != 150000 {
		t.Fatalf("expected 150000, got %d (err=%v)", q, err)
	}

	fh := host.NewFakeHost()
	b := New(fh, BuildConfig{MirrorsDir: "/tmp/m", BuildsDir: "/tmp/b"}, nil)
	_, err = b.Build(context.Background(), BuildRequest{
		AppName: "app", DeploymentID: "dep_1", ReleaseNumber: 1,
		BuildEnv: map[string]string{"BUILD_VAR": "secret_val"},
	})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	for _, c := range fh.Commands {
		if c.Name == "podman" {
			joined := strings.Join(c.Args, " ")
			if strings.Contains(joined, "secret_val") {
				t.Fatalf("build-arg value leaked in podman argv: %s", joined)
			}
			if !strings.Contains(strings.Join(c.Env, "\n"), "BUILD_VAR=secret_val") {
				t.Fatalf("expected BUILD_VAR in podman env, got %v", c.Env)
			}
		}
	}
}
