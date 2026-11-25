package server_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"dermaga/internal/auth"
	"dermaga/internal/crypto"
	"dermaga/internal/github"
	"dermaga/internal/server"
	"dermaga/internal/store"
)

func testGitHubPEM(t *testing.T) []byte {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generating RSA key: %v", err)
	}
	return pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(key),
	})
}

func setupAuthedServerWithFakeGitHub(t *testing.T) (*server.Server, *store.Store, []byte, string, *time.Time) {
	t.Helper()
	pemBytes := testGitHubPEM(t)
	now := time.Unix(1700000000, 0).UTC()
	fakeGH := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/app-manifests/valid-code/conversions":
			_ = json.NewEncoder(w).Encode(github.ManifestConversion{
				ID:           1001,
				Slug:         "dermaga-fake",
				Name:         "Dermaga Fake",
				HTMLURL:      "https://github.com/apps/dermaga-fake",
				PEM:          string(pemBytes),
				ClientID:     "Iv1.123",
				ClientSecret: "super-client-secret",
			})
		case r.Method == http.MethodPatch && r.URL.Path == "/app/hook/config":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{}`))
		case r.Method == http.MethodGet && r.URL.Path == "/app/installations":
			_ = json.NewEncoder(w).Encode([]github.Installation{
				{
					ID:                  888,
					Account:             github.InstallationAccount{Login: "r3p-dev", Type: "User"},
					RepositorySelection: "selected",
					HTMLURL:             "https://github.com/settings/installations/888",
				},
			})
		case r.Method == http.MethodPost && r.URL.Path == "/app/installations/888/access_tokens":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"token":      "ghs_test_mem_only",
				"expires_at": time.Now().Add(time.Hour).UTC().Format(time.RFC3339),
			})
		case r.Method == http.MethodGet && r.URL.Path == "/installation/repositories":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"total_count": 1,
				"repositories": []github.Repository{
					{
						ID:            901,
						Name:          "dermaga-test-app",
						FullName:      "r3p-dev/dermaga-test-app",
						Owner:         github.RepositoryOwner{Login: "r3p-dev"},
						Private:       true,
						DefaultBranch: "main",
						HTMLURL:       "https://github.com/r3p-dev/dermaga-test-app",
					},
				},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(fakeGH.Close)

	s, err := store.Open(filepath.Join(t.TempDir(), "gh.db"))
	if err != nil {
		t.Fatalf("store.Open failed: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })

	ctx := context.Background()
	hash, _ := auth.HashPassword("StrongPassword123!")
	_ = s.SetSetting(ctx, auth.SettingAdminPassword, hash)

	masterKey, _ := crypto.GenerateRandomKey()
	srv, err := server.NewServer(server.Config{
		ListenAddr: "127.0.0.1:7070",
		Store:      s,
		MasterKey:  masterKey,
		Now:        func() time.Time { return now },
		GitHub: &github.Client{
			APIBaseURL: fakeGH.URL,
			WebBaseURL: fakeGH.URL,
			HTTPClient: fakeGH.Client(),
		},
	})
	if err != nil {
		t.Fatalf("NewServer failed: %v", err)
	}

	rawTok, _ := auth.GenerateSessionToken()
	_ = s.CreateSession(ctx, auth.HashSessionToken(rawTok), "admin", time.Now().Add(time.Hour))
	return srv, s, masterKey, rawTok, &now
}

func TestSettingsGitHubFlow_GHA1_GHA6_SEC3_SEC9(t *testing.T) {
	srv, s, masterKey, tok, nowPtr := setupAuthedServerWithFakeGitHub(t)
	h := srv.Handler()

	doReq := func(method, path string, body any, cookieTok string) *httptest.ResponseRecorder {
		var buf bytes.Buffer
		if body != nil {
			_ = json.NewEncoder(&buf).Encode(body)
		}
		req := httptest.NewRequest(method, path, &buf)
		req.Host = "localhost:7070"
		if method != http.MethodGet {
			req.Header.Set("Origin", "http://localhost:7070")
		}
		if cookieTok != "" {
			req.AddCookie(&http.Cookie{Name: auth.SessionCookieName, Value: cookieTok})
		}
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, req)
		return rr
	}

	// Unauthenticated refused (auth check)
	if rr := doReq(http.MethodGet, "/api/settings/github", nil, ""); rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 without session, got %d", rr.Code)
	}

	// Blocked when hook_domain is empty (owner decision #3)
	if rr := doReq(http.MethodPost, "/api/settings/github/manifest", nil, tok); rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 when hook_domain is empty, got %d (%s)", rr.Code, rr.Body.String())
	}

	// Set hook_domain
	_ = s.SetSetting(context.Background(), auth.SettingHookDomain, "hooks.example.com")

	// Start manifest flow
	rr := doReq(http.MethodPost, "/api/settings/github/manifest", nil, tok)
	if rr.Code != http.StatusOK {
		t.Fatalf("manifest start failed: %d %s", rr.Code, rr.Body.String())
	}
	var startResp server.GitHubManifestStartResponse
	_ = json.Unmarshal(rr.Body.Bytes(), &startResp)
	u, err := url.Parse(startResp.ActionURL)
	if err != nil {
		t.Fatalf("invalid action_url: %v", err)
	}
	state := u.Query().Get("state")
	if state == "" {
		t.Fatal("expected state in action_url")
	}

	// SEC-3: foreign Origin on callback rejected with 403
	reqBadOrigin := httptest.NewRequest(http.MethodPost, "/api/settings/github/callback", strings.NewReader(`{"code":"valid-code","state":"`+state+`"}`))
	reqBadOrigin.Host = "localhost:7070"
	reqBadOrigin.Header.Set("Origin", "https://evil.example")
	reqBadOrigin.AddCookie(&http.Cookie{Name: auth.SessionCookieName, Value: tok})
	rrBadOrigin := httptest.NewRecorder()
	h.ServeHTTP(rrBadOrigin, reqBadOrigin)
	if rrBadOrigin.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for foreign Origin, got %d", rrBadOrigin.Code)
	}

	// Different session cannot consume state
	otherTok, _ := auth.GenerateSessionToken()
	_ = s.CreateSession(context.Background(), auth.HashSessionToken(otherTok), "admin", time.Now().Add(time.Hour))
	if rrOther := doReq(http.MethodPost, "/api/settings/github/callback", server.GitHubCallbackRequest{Code: "valid-code", State: state}, otherTok); rrOther.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for state bound to another session, got %d", rrOther.Code)
	}

	// Expired state refused
	rrExp := doReq(http.MethodPost, "/api/settings/github/manifest", nil, tok)
	var startExp server.GitHubManifestStartResponse
	_ = json.Unmarshal(rrExp.Body.Bytes(), &startExp)
	uExp, _ := url.Parse(startExp.ActionURL)
	stateExp := uExp.Query().Get("state")
	*nowPtr = nowPtr.Add(16 * time.Minute)
	if rrExpCB := doReq(http.MethodPost, "/api/settings/github/callback", server.GitHubCallbackRequest{Code: "valid-code", State: stateExp}, tok); rrExpCB.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for expired state, got %d", rrExpCB.Code)
	}

	// Note: re-issue state for clean valid callback test
	rr2 := doReq(http.MethodPost, "/api/settings/github/manifest", nil, tok)
	var start2 server.GitHubManifestStartResponse
	_ = json.Unmarshal(rr2.Body.Bytes(), &start2)
	u2, _ := url.Parse(start2.ActionURL)
	state2 := u2.Query().Get("state")

	// Valid callback
	rrCB := doReq(http.MethodPost, "/api/settings/github/callback", server.GitHubCallbackRequest{Code: "valid-code", State: state2}, tok)
	if rrCB.Code != http.StatusOK {
		t.Fatalf("callback failed: %d %s", rrCB.Code, rrCB.Body.String())
	}

	// Single-use: replaying same state2 fails
	rrReplay := doReq(http.MethodPost, "/api/settings/github/callback", server.GitHubCallbackRequest{Code: "valid-code", State: state2}, tok)
	if rrReplay.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 on replayed state, got %d", rrReplay.Code)
	}

	// SEC-9 / GHA-4: secrets encrypted in DB, never plaintext; never in API responses or audit logs
	ctx := context.Background()
	pemEnc, _ := s.GetSetting(ctx, "github_app_pem_encrypted")
	whEnc, _ := s.GetSetting(ctx, "github_webhook_secret_encrypted")
	if strings.Contains(pemEnc, "BEGIN RSA PRIVATE KEY") {
		t.Fatal("PEM stored in plaintext!")
	}
	whPlain, err := crypto.Decrypt(masterKey, "github_webhook_secret_encrypted", whEnc)
	if err != nil {
		t.Fatalf("webhook secret decrypt failed: %v", err)
	}
	if strings.Contains(rrCB.Body.String(), "BEGIN RSA PRIVATE KEY") ||
		strings.Contains(rrCB.Body.String(), "super-client-secret") ||
		strings.Contains(rrCB.Body.String(), string(whPlain)) {
		t.Fatal("callback response leaked secret material")
	}

	// Sync installations + repos (GHA-3, GHA-5, GHA-6)
	rrSync := doReq(http.MethodPost, "/api/settings/github/sync", nil, tok)
	if rrSync.Code != http.StatusOK {
		t.Fatalf("sync failed: %d %s", rrSync.Code, rrSync.Body.String())
	}
	if strings.Contains(rrSync.Body.String(), "BEGIN RSA PRIVATE KEY") ||
		strings.Contains(rrSync.Body.String(), "super-client-secret") ||
		strings.Contains(rrSync.Body.String(), string(whPlain)) {
		t.Fatal("sync response leaked secret material")
	}
	var status server.GitHubStatusResponse
	_ = json.Unmarshal(rrSync.Body.Bytes(), &status)
	if !status.Connected || len(status.Installations) != 1 || len(status.Repositories) != 1 || status.Repositories[0].FullName != "r3p-dev/dermaga-test-app" {
		t.Fatalf("unexpected GitHub status after sync: %+v", status)
	}

	// Disconnect requires typed confirmation (GHA-6)
	if rrDiscBad := doReq(http.MethodDelete, "/api/settings/github", server.GitHubDisconnectRequest{Confirm: "nope"}, tok); rrDiscBad.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 without typed 'disconnect', got %d", rrDiscBad.Code)
	}
	rrDisc := doReq(http.MethodDelete, "/api/settings/github", server.GitHubDisconnectRequest{Confirm: "disconnect"}, tok)
	if rrDisc.Code != http.StatusOK {
		t.Fatalf("disconnect failed: %d %s", rrDisc.Code, rrDisc.Body.String())
	}
	rrAfter := doReq(http.MethodGet, "/api/settings/github", nil, tok)
	var afterStatus server.GitHubStatusResponse
	_ = json.Unmarshal(rrAfter.Body.Bytes(), &afterStatus)
	if afterStatus.Connected || len(afterStatus.Repositories) != 0 {
		t.Fatalf("expected disconnected empty state, got %+v", afterStatus)
	}

	// Assert encrypted keys, secrets, installations, and repos are deleted from DB
	for _, k := range []string{
		"github_app_id",
		"github_app_pem_encrypted",
		"github_client_secret_encrypted",
		"github_webhook_secret_encrypted",
	} {
		if _, err := s.GetSetting(ctx, k); err != store.ErrNotFound {
			t.Errorf("expected setting %s deleted after disconnect, got err=%v", k, err)
		}
	}
	if insts, err := s.ListGitHubInstallations(ctx); err != nil || len(insts) != 0 {
		t.Errorf("expected 0 installations after disconnect, got %d (err=%v)", len(insts), err)
	}
	if repos, err := s.ListGitHubRepositories(ctx); err != nil || len(repos) != 0 {
		t.Errorf("expected 0 repos after disconnect, got %d (err=%v)", len(repos), err)
	}

	// Assert audit logs never contain secret material
	logs, err := s.ListAuditLogs(ctx, store.AuditFilter{Limit: 50})
	if err != nil {
		t.Fatalf("ListAuditLogs failed: %v", err)
	}
	for _, entry := range logs {
		if strings.Contains(entry.Description, "BEGIN RSA PRIVATE KEY") ||
			strings.Contains(entry.Description, "super-client-secret") ||
			strings.Contains(entry.Description, string(whPlain)) ||
			strings.Contains(string(entry.Details), "BEGIN RSA PRIVATE KEY") ||
			strings.Contains(string(entry.Details), "super-client-secret") ||
			strings.Contains(string(entry.Details), string(whPlain)) {
			t.Fatalf("audit log %d leaked secret material", entry.ID)
		}
	}
}
