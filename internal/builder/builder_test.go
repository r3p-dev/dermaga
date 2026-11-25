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
	m := NewMasker([]string{"sec", "secret-token-123"})
	var got strings.Builder
	sm := NewStreamMasker(m, func(s string) {
		got.WriteString(s)
	})

	chunks := []string{
		"step 1: using sec",
		"ret-token-123 in build\n",
		"also sec here\n",
	}
	for _, c := range chunks {
		if _, err := sm.Write([]byte(c)); err != nil {
			t.Fatalf("Write: %v", err)
		}
	}
	sm.Flush()

	out := got.String()
	if strings.Contains(out, "secret-token-123") || strings.Contains(out, "sec") {
		t.Fatalf("secret leaked across chunk boundary: %q", out)
	}
	if !strings.Contains(out, "using [MASKED] in build") {
		t.Fatalf("expected longest-first masking, got %q", out)
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
