package server

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"dermaga/internal/access"
	"dermaga/internal/auth"
	"dermaga/internal/builder"
	"dermaga/internal/deploy"
	"dermaga/internal/github"
	"dermaga/internal/host"
	"dermaga/internal/store"
)

func setupAppsTestServer(t *testing.T) (*Server, *store.Store, *host.FakeHost, string) {
	t.Helper()
	tmp := t.TempDir()
	s, err := store.Open(filepath.Join(tmp, "test.db"))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })

	masterKey := make([]byte, 32)
	_, _ = rand.Read(masterKey)

	ctx := context.Background()
	hash, _ := auth.HashPassword("StrongPassword!123")
	_ = s.SetSetting(ctx, "admin_password_hash", hash)
	_ = s.SetSetting(ctx, "hook_domain", "hooks.example.com")
	_ = s.ReplaceGitHubInstallationsAndRepos(ctx,
		[]store.GitHubInstallation{{ID: 42, AccountLogin: "octo", AccountType: "User"}},
		[]store.GitHubRepository{{ID: 1, InstallationID: 42, FullName: "octo/portfolio", Owner: "octo", Name: "portfolio", DefaultBranch: "main"}},
	)

	token, err := auth.GenerateSessionToken()
	if err != nil {
		t.Fatalf("GenerateSessionToken: %v", err)
	}
	_ = s.CreateSession(ctx, auth.HashSessionToken(token), "admin", time.Now().Add(time.Hour))

	fh := host.NewFakeHost()
	bld := builder.New(fh, builder.BuildConfig{
		MirrorsDir: filepath.Join(tmp, "mirrors"),
		BuildsDir:  filepath.Join(tmp, "builds"),
	}, nil)
	eng := deploy.NewEngine(s, fh, github.NewClient(), bld, deploy.Config{
		QuadletDir:        filepath.Join(tmp, "quadlet"),
		TraefikDynamicDir: filepath.Join(tmp, "traefik"),
		EnvDir:            filepath.Join(tmp, "env"),
		MirrorsDir:        filepath.Join(tmp, "mirrors"),
		BuildsDir:         filepath.Join(tmp, "builds"),
		BinaryPath:        "/usr/local/bin/dermaga",
		MasterKey:         masterKey,
		StabilityWindow:   5 * time.Millisecond,
		ProbeInterval:     5 * time.Millisecond,
		DrainGrace:        5 * time.Millisecond,
		SwitchTimeout:     100 * time.Millisecond,
		RouteTimeout:      100 * time.Millisecond,
		VerifyRoute:       func(context.Context, string, string, string) (bool, error) { return !failVerify.Load(), nil },
	})
	fh.Files["/proc/meminfo"] = []byte("MemTotal: 4000000 kB\nMemAvailable: 2097152 kB\n")

	_ = s.SetSetting(ctx, access.SettingAddress, "203.0.113.10")
	srv, err := NewServer(Config{
		Store:     s,
		Deploy:    eng,
		Host:      fh,
		MasterKey: masterKey,
		DNSLookup: fakeDNS,
	})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	return srv, s, fh, token
}

// failVerify makes the fake Traefik never load a route (APP-10 failure path).
var failVerify atomic.Bool

// fakeDNS: names starting with "ok." point to this server (203.0.113.10); others have no records.
// Tests never query real DNS.
func fakeDNS(_ context.Context, network, host string) ([]net.IP, error) {
	if network == "ip4" && strings.HasPrefix(host, "ok.") {
		return []net.IP{net.ParseIP("203.0.113.10")}, nil
	}
	return nil, &net.DNSError{Err: "no such host", Name: host, IsNotFound: true}
}

func doAPIReq(srv *Server, method, path, token string, body any) *httptest.ResponseRecorder {
	var buf bytes.Buffer
	if body != nil {
		_ = json.NewEncoder(&buf).Encode(body)
	}
	req := httptest.NewRequest(method, "http://127.0.0.1:7070"+path, &buf)
	req.Host = "127.0.0.1:7070"
	if method != http.MethodGet {
		req.Header.Set("Origin", "http://127.0.0.1:7070")
	}
	if token != "" {
		req.AddCookie(&http.Cookie{Name: auth.SessionCookieName, Value: token})
	}
	rr := httptest.NewRecorder()
	srv.httpServer.Handler.ServeHTTP(rr, req)
	return rr
}

func TestAppsAPI_CreateEnvRevealDelete_WIZ6_APP1_UI5_APP9(t *testing.T) {
	srv, s, _, token := setupAppsTestServer(t)

	// Unauthenticated rejected (401)
	if rr := doAPIReq(srv, http.MethodGet, "/api/apps", "", nil); rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 unauthenticated, got %d", rr.Code)
	}

	// Hook domain rejected as app domain (WIZ-6 / APP-2)
	rr := doAPIReq(srv, http.MethodPost, "/api/apps", token, CreateAppRequest{
		Name:         "portfolio",
		RepoFullName: "octo/portfolio",
		Branch:       "main",
		Port:         3000,
		HealthType:   "http",
		HealthPath:   "/health",
		Domain:       "hooks.example.com",
	})
	if rr.Code != http.StatusBadRequest || !strings.Contains(rr.Body.String(), "hook domain") {
		t.Fatalf("expected hook domain validation error, got %d: %s", rr.Code, rr.Body.String())
	}

	// Valid creation
	rr = doAPIReq(srv, http.MethodPost, "/api/apps", token, CreateAppRequest{
		Name:         "portfolio",
		RepoFullName: "octo/portfolio",
		Branch:       "main",
		Port:         3000,
		HealthType:   "http",
		HealthPath:   "/health",
		Domain:       "portfolio.example.com",
		Env: []EnvVarInput{
			{Key: "API_KEY", Value: "secret-123", Runtime: true, BuildTime: false},
			{Key: "PUBLIC_URL", Value: "https://portfolio.example.com", Runtime: true, BuildTime: true},
		},
	})
	if rr.Code != http.StatusCreated {
		t.Fatalf("expected 201 Created, got %d: %s", rr.Code, rr.Body.String())
	}
	var created CreateAppResponse
	_ = json.Unmarshal(rr.Body.Bytes(), &created)

	// GET /api/apps/{id} hides secret values (UI-5)
	rr = doAPIReq(srv, http.MethodGet, "/api/apps/"+created.App.ID, token, nil)
	if rr.Code != http.StatusOK || strings.Contains(rr.Body.String(), "secret-123") {
		t.Fatalf("GET app leaked secret or failed: %d %s", rr.Code, rr.Body.String())
	}

	// Explicit reveal returns value and writes audit log (UI-5, AUD-1)
	rr = doAPIReq(srv, http.MethodPost, "/api/apps/"+created.App.ID+"/env/reveal", token, EnvRevealRequest{
		Key: "API_KEY", Scope: "runtime",
	})
	if rr.Code != http.StatusOK || !strings.Contains(rr.Body.String(), "secret-123") {
		t.Fatalf("reveal failed: %d %s", rr.Code, rr.Body.String())
	}
	logs, _ := s.ListAuditLogs(context.Background(), store.AuditFilter{Limit: 10})
	foundReveal := false
	for _, l := range logs {
		if l.Event == "secret_reveal" && !strings.Contains(l.Description, "secret-123") {
			foundReveal = true
		}
	}
	if !foundReveal {
		t.Fatalf("expected secret_reveal audit log without secret value, got %+v", logs)
	}

	// Delete requires typed confirmation (APP-9, UI-6)
	rr = doAPIReq(srv, http.MethodDelete, "/api/apps/"+created.App.ID, token, DeleteAppRequest{ConfirmName: "wrong"})
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 on wrong confirm_name, got %d", rr.Code)
	}
	rr = doAPIReq(srv, http.MethodDelete, "/api/apps/"+created.App.ID, token, DeleteAppRequest{ConfirmName: "portfolio"})
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 on delete, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestAppsAPI_RuntimeLogMasking_SEC9_LOG1(t *testing.T) {
	srv, s, fh, token := setupAppsTestServer(t)
	ctx := context.Background()

	envSealed, _ := deploy.SealEnvMap(srv.cfg.MasterKey, "env_encrypted", map[string]string{"DB_PASS": "rt_secret_val_456"})
	_ = s.CreateApp(ctx, &store.App{
		ID: "app_rt", Name: "rtapp", RepoOwner: "octo", RepoName: "portfolio",
		Port: 8080, HealthType: "none", EnvEncrypted: envSealed,
	})
	_ = s.CreateDeployment(ctx, &store.Deployment{
		ID: "dep_rt1", AppID: "app_rt", ReleaseNumber: 1, Status: "active",
		Trigger: "manual", CommitHash: "abc", CommitMessage: "init",
	})
	_ = s.SetActiveRelease(ctx, "app_rt", "dep_rt1")

	fh.StreamMock = func(ctx context.Context, env []string, onLine func(string), name string, args ...string) error {
		onLine("2026-11-26T00:00:00Z connecting with rt_secret_val_456")
		return nil
	}

	rr := doAPIReq(srv, http.MethodGet, "/api/apps/app_rt/logs/stream?follow=false&lines=50", token, nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	body := rr.Body.String()
	if strings.Contains(body, "rt_secret_val_456") {
		t.Fatalf("runtime secret leaked in runtime log stream: %s", body)
	}
	if !strings.Contains(body, "[MASKED]") {
		t.Fatalf("expected [MASKED] in runtime log stream: %s", body)
	}
}

// APP-8: auto-deploy and watch paths are saved through the settings form, validated server-side,
// and left unchanged when a request omits them.
func TestAppsAPI_AutoDeployAndWatchPaths_APP8(t *testing.T) {
	srv, s, _, token := setupAppsTestServer(t)
	ctx := context.Background()
	app := &store.App{ID: "app_w", Name: "web", RepoOwner: "octo", RepoName: "portfolio", Branch: "main",
		Port: 3000, HealthType: "none", MemoryLimit: "512m", AutoDeploy: true}
	if err := s.CreateApp(ctx, app); err != nil {
		t.Fatalf("CreateApp: %v", err)
	}
	f, tr := false, true
	settings := func(auto *bool, watch *string) UpdateAppSettingsRequest {
		return UpdateAppSettingsRequest{Branch: "main", Port: 3000, HealthType: "none", AutoDeploy: auto, WatchPaths: watch}
	}
	str := func(v string) *string { return &v }

	if rr := doAPIReq(srv, http.MethodPatch, "/api/apps/app_w", "", settings(&f, nil)); rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rr.Code)
	}
	for _, bad := range []string{"/web/", "../up", "[abc"} {
		rr := doAPIReq(srv, http.MethodPatch, "/api/apps/app_w", token, settings(&f, str("ok/\n"+bad)))
		if rr.Code != http.StatusBadRequest || !strings.Contains(rr.Body.String(), "watch path") || strings.Contains(rr.Body.String(), "APP-8") {
			t.Fatalf("bad watch path %q: %d %s", bad, rr.Code, rr.Body.String())
		}
	}
	if got, _ := s.GetApp(ctx, "app_w"); !got.AutoDeploy || got.WatchPaths != "" {
		t.Fatalf("a refused request changed the app: %+v", got)
	}

	rr := doAPIReq(srv, http.MethodPatch, "/api/apps/app_w", token, settings(&f, str(" web/ \r\n\n go.* ")))
	var resp AppDetailResponse
	_ = json.Unmarshal(rr.Body.Bytes(), &resp)
	if rr.Code != http.StatusOK || resp.AutoDeploy || resp.WatchPaths != "web/\ngo.*" {
		t.Fatalf("save: %d auto=%v watch=%q", rr.Code, resp.AutoDeploy, resp.WatchPaths)
	}
	rr = doAPIReq(srv, http.MethodPatch, "/api/apps/app_w", token, settings(nil, nil))
	_ = json.Unmarshal(rr.Body.Bytes(), &resp)
	if resp.AutoDeploy || resp.WatchPaths != "web/\ngo.*" {
		t.Fatalf("omitted fields changed: auto=%v watch=%q", resp.AutoDeploy, resp.WatchPaths)
	}
	rr = doAPIReq(srv, http.MethodPatch, "/api/apps/app_w", token, settings(&tr, str("")))
	_ = json.Unmarshal(rr.Body.Bytes(), &resp)
	if !resp.AutoDeploy || resp.WatchPaths != "" {
		t.Fatalf("re-enable: auto=%v watch=%q", resp.AutoDeploy, resp.WatchPaths)
	}
}

// APP-8: Settings → GitHub shows the newest ignored push with its reason.
func TestGitHubStatus_LastIgnoredPush_APP8(t *testing.T) {
	srv, s, _, token := setupAppsTestServer(t)
	ctx := context.Background()
	_ = s.SetSetting(ctx, settingGitHubAppID, "123") // connected: delivery info is shown only then
	var status GitHubStatusResponse
	_ = json.Unmarshal(doAPIReq(srv, http.MethodGet, "/api/settings/github", token, nil).Body.Bytes(), &status)
	if status.LastIgnoredPush != nil {
		t.Fatalf("no ignored push yet, got %+v", status.LastIgnoredPush)
	}
	_ = s.RecordAudit(ctx, "webhook", "github", "push_ignored", "Push to dev (abc1234) ignored for web: the app deploys from main.", nil, nil)
	_ = s.RecordAudit(ctx, "webhook", "github", "push_deploy", "Push to main (def5678) queued release r2 for web.", nil, nil)
	rr := doAPIReq(srv, http.MethodGet, "/api/settings/github", token, nil)
	_ = json.Unmarshal(rr.Body.Bytes(), &status)
	if rr.Code != http.StatusOK || status.LastIgnoredPush == nil || !strings.Contains(status.LastIgnoredPush.Description, "deploys from main") {
		t.Fatalf("last ignored push: %d %+v", rr.Code, status.LastIgnoredPush)
	}
}
