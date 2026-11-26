package deploy

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"dermaga/internal/builder"
	"dermaga/internal/crypto"
	"dermaga/internal/github"
	"dermaga/internal/host"
	"dermaga/internal/store"
)

func setupTestEngine(t *testing.T) (*store.Store, *host.FakeHost, *Engine, []byte) {
	t.Helper()
	tmp := t.TempDir()
	s, err := store.Open(filepath.Join(tmp, "test.db"))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })

	masterKey := make([]byte, 32)
	_, _ = rand.Read(masterKey)

	rsaKey, _ := rsa.GenerateKey(rand.Reader, 2048)
	pemBytes := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(rsaKey)})
	pemSealed, err := crypto.Encrypt(masterKey, "github_app_pem_encrypted", pemBytes)
	if err != nil {
		t.Fatalf("encrypt pem: %v", err)
	}
	ctx := context.Background()
	_ = s.SetSetting(ctx, "github_app_id", "123")
	_ = s.SetSetting(ctx, "github_app_pem_encrypted", pemSealed)
	_ = s.ReplaceGitHubInstallationsAndRepos(ctx,
		[]store.GitHubInstallation{{ID: 42, AccountLogin: "octo", AccountType: "User"}},
		[]store.GitHubRepository{{ID: 1, InstallationID: 42, FullName: "octo/web", Owner: "octo", Name: "web", DefaultBranch: "main"}},
	)

	ghSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"token":      "ghs_test_token",
			"expires_at": "2026-11-25T16:00:00Z",
		})
	}))
	t.Cleanup(ghSrv.Close)

	ghClient := github.NewClient()
	ghClient.APIBaseURL = ghSrv.URL

	fh := host.NewFakeHost()
	b := builder.New(fh, builder.BuildConfig{
		MirrorsDir: filepath.Join(tmp, "mirrors"),
		BuildsDir:  filepath.Join(tmp, "builds"),
	}, nil)

	// 4 GiB available unless a test overrides it.
	fh.Files["/proc/meminfo"] = []byte("MemTotal:        8000000 kB\nMemAvailable:    4194304 kB\n")
	traefikDir := filepath.Join(tmp, "traefik")
	eng := NewEngine(s, fh, ghClient, b, Config{
		QuadletDir:        filepath.Join(tmp, "quadlet"),
		TraefikDynamicDir: filepath.Join(tmp, "traefik"),
		EnvDir:            filepath.Join(tmp, "env"),
		MirrorsDir:        filepath.Join(tmp, "mirrors"),
		BuildsDir:         filepath.Join(tmp, "builds"),
		BinaryPath:        "/usr/local/bin/dermaga",
		MasterKey:         masterKey,
		StabilityWindow:   10 * time.Millisecond,
		ProbeInterval:     5 * time.Millisecond,
		DrainGrace:        10 * time.Millisecond,
		SwitchTimeout:     200 * time.Millisecond,
		// Fake Traefik: the token comes back once the route file on disk contains it.
		VerifyRoute: func(_ context.Context, _, _, token string) (bool, error) {
			entries, _ := fh.ReadDir(traefikDir)
			for _, en := range entries {
				if data, err := fh.ReadFile(filepath.Join(traefikDir, en.Name())); err == nil && strings.Contains(string(data), token) {
					return true, nil
				}
			}
			return false, nil
		},
	})
	return s, fh, eng, masterKey
}

func waitForStatus(t *testing.T, s *store.Store, depID string, want ...string) *store.Deployment {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		dep, err := s.GetDeployment(context.Background(), depID)
		if err == nil {
			for _, w := range want {
				if dep.Status == w {
					return dep
				}
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	dep, _ := s.GetDeployment(context.Background(), depID)
	t.Fatalf("timed out waiting for %s status %v (got %+v)", depID, want, dep)
	return nil
}

func TestFirstDeploy_Success_DEP9(t *testing.T) {
	s, fh, eng, masterKey := setupTestEngine(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := eng.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}

	envSealed, _ := SealEnvMap(masterKey, "env_encrypted", map[string]string{"APP_SECRET": "runtime_val"})
	healthPath := "/health"
	app := &store.App{
		ID:                "app_1",
		Name:              "portfolio",
		RepoOwner:         "octo",
		RepoName:          "web",
		Branch:            "main",
		ContainerfilePath: "Containerfile",
		BuildContext:      ".",
		Port:              3000,
		HealthType:        "http",
		HealthPath:        &healthPath,
		HealthTimeoutSec:  10,
		HealthConsecutive: 2,
		MemoryLimit:       "512m",
		CPULimit:          "1.0",
		PidsLimit:         128,
		EnvEncrypted:      envSealed,
	}
	if err := s.CreateApp(ctx, app); err != nil {
		t.Fatalf("CreateApp: %v", err)
	}
	if err := s.CreateDomain(ctx, &store.Domain{ID: "dom_1", AppID: "app_1", Domain: "portfolio.example.com", IsPrimary: true}); err != nil {
		t.Fatalf("CreateDomain: %v", err)
	}

	dep, err := eng.Enqueue(ctx, "app_1", "manual", "abc1234", "initial")
	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	got := waitForStatus(t, s, dep.ID, "active")
	if got.ImageRef != "localhost/dermaga/portfolio:r1" {
		t.Fatalf("unexpected image ref: %q", got.ImageRef)
	}

	// Verify Quadlet unit, env file, and Traefik dynamic file were written.
	if _, err := fh.Stat(filepath.Join(eng.cfg.QuadletDir, "app-portfolio-r1.container")); err != nil {
		t.Fatalf("expected Quadlet unit written: %v", err)
	}
	if _, err := fh.Stat(filepath.Join(eng.cfg.TraefikDynamicDir, "app-portfolio.yml")); err != nil {
		t.Fatalf("expected Traefik route written: %v", err)
	}
	envData, err := fh.ReadFile(filepath.Join(eng.cfg.EnvDir, "portfolio-r1.env"))
	if err != nil || !strings.Contains(string(envData), "APP_SECRET=runtime_val") {
		t.Fatalf("expected runtime env file, got %q (err=%v)", string(envData), err)
	}
}

func TestFirstDeploy_FailureLeavesNothing_DEP3(t *testing.T) {
	s, fh, eng, _ := setupTestEngine(t)
	fh.CommandMock = func(name string, args ...string) ([]byte, error) {
		if name == "podman" && len(args) > 0 && args[0] == "unshare" {
			return []byte("probe failed"), context.DeadlineExceeded
		}
		return []byte("ok"), nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	_ = eng.Start(ctx)

	healthPath := "/health"
	_ = s.CreateApp(ctx, &store.App{
		ID: "app_fail", Name: "failapp", RepoOwner: "octo", RepoName: "web",
		Port: 8080, HealthType: "http", HealthPath: &healthPath, HealthTimeoutSec: 1, HealthConsecutive: 1,
	})
	_ = s.CreateDomain(ctx, &store.Domain{ID: "dom_f", AppID: "app_fail", Domain: "fail.example.com", IsPrimary: true})

	dep, err := eng.Enqueue(ctx, "app_fail", "manual", "deadbeef", "broken")
	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	waitForStatus(t, s, dep.ID, "failed")

	if _, err := fh.Stat(filepath.Join(eng.cfg.QuadletDir, "app-failapp-r1.container")); err == nil {
		t.Fatalf("expected Quadlet unit removed on failed first deploy")
	}
	if _, err := fh.Stat(filepath.Join(eng.cfg.TraefikDynamicDir, "app-failapp.yml")); err == nil {
		t.Fatalf("expected no Traefik route on failed first deploy")
	}
}

func TestFirstDeploy_CancelRunningBuildLeavesNothing_DEP6(t *testing.T) {
	s, fh, eng, _ := setupTestEngine(t)
	buildStarted := make(chan struct{})
	fh.StreamMock = func(ctx context.Context, env []string, onLine func(string), name string, args ...string) error {
		close(buildStarted)
		<-ctx.Done()
		return ctx.Err()
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	_ = eng.Start(ctx)

	_ = s.CreateApp(ctx, &store.App{
		ID: "app_c", Name: "cancelapp", RepoOwner: "octo", RepoName: "web",
		Port: 8080, HealthType: "none",
	})
	dep, err := eng.Enqueue(ctx, "app_c", "manual", "abc", "cancel me")
	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	<-buildStarted
	if err := eng.Cancel(ctx, dep.ID); err != nil {
		t.Fatalf("Cancel: %v", err)
	}
	waitForStatus(t, s, dep.ID, "cancelled")

	if _, err := fh.Stat(filepath.Join(eng.cfg.QuadletDir, "app-cancelapp-r1.container")); err == nil {
		t.Fatalf("expected no Quadlet unit after cancel")
	}
	if _, err := fh.Stat(filepath.Join(eng.cfg.TraefikDynamicDir, "app-cancelapp.yml")); err == nil {
		t.Fatalf("expected no Traefik route after cancel")
	}
}
