package server_test

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"dermaga/internal/access"
	"dermaga/internal/auth"
	"dermaga/internal/crypto"
	"dermaga/internal/host"
	"dermaga/internal/reconciler"
	"dermaga/internal/server"
	"dermaga/internal/store"
	"dermaga/internal/traefik"
)

func newAuthenticatedTestServer(t *testing.T) (*server.Server, *store.Store, *host.FakeHost, *http.Cookie) {
	t.Helper()
	ctx := context.Background()
	st, err := store.Open(filepath.Join(t.TempDir(), "settings.db"))
	if err != nil {
		t.Fatalf("store.Open failed: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	key, _ := crypto.GenerateRandomKey()
	fh := host.NewFakeHost()
	rec := reconciler.New(fh, st, reconciler.Config{
		QuadletDir:        "/tmp/quadlet",
		TraefikDynamicDir: "/tmp/traefik-dynamic",
	})

	_ = st.SetSetting(ctx, access.SettingAddress, "203.0.113.10")
	_ = st.SetSetting(ctx, access.SettingPort, "4222")
	_ = auth.SetAdminPassword(ctx, st, "panel", "Correct-Horse-Battery-99")

	srv, err := server.NewServer(server.Config{
		ListenAddr: "127.0.0.1:0",
		Store:      st,
		Reconciler: rec,
		DNSLookup: func(_ context.Context, network, _ string) ([]net.IP, error) {
			if network == "ip4" {
				return []net.IP{net.ParseIP("203.0.113.10")}, nil
			}
			return nil, nil
		},
		MasterKey: key,
	})
	if err != nil {
		t.Fatalf("NewServer failed: %v", err)
	}

	loginRec := postJSON(srv.Handler(), "/api/auth/login", server.LoginRequest{Password: "Correct-Horse-Battery-99"})
	if loginRec.Code != http.StatusOK {
		t.Fatalf("login failed: %d %s", loginRec.Code, loginRec.Body.String())
	}
	cookie := findCookie(loginRec.Result().Cookies(), auth.SessionCookieName)
	if cookie == nil {
		t.Fatalf("expected session cookie")
	}
	return srv, st, fh, cookie
}

func TestSettingsAccessAndSessions_LAP3_ADM5(t *testing.T) {
	srv, _, _, cookie1 := newAuthenticatedTestServer(t)
	h := srv.Handler()

	// Second session
	login2 := postJSON(h, "/api/auth/login", server.LoginRequest{Password: "Correct-Horse-Battery-99"})
	cookie2 := findCookie(login2.Result().Cookies(), auth.SessionCookieName)
	if cookie2 == nil {
		t.Fatalf("expected second session cookie")
	}

	// LAP-3: GET /api/settings/access
	req := httptest.NewRequest(http.MethodGet, "/api/settings/access", nil)
	req.Host = "127.0.0.1:7070"
	req.AddCookie(cookie1)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/settings/access failed: %d", rec.Code)
	}
	var acc server.AccessSettingsResponse
	_ = json.NewDecoder(rec.Body).Decode(&acc)
	if acc.Address != "203.0.113.10" || acc.Port != 4222 || !strings.Contains(acc.LaptopCommand, "LocalForward 7070") {
		t.Fatalf("unexpected access settings: %+v", acc)
	}

	// ADM-5: list sessions marks current
	req = httptest.NewRequest(http.MethodGet, "/api/auth/sessions", nil)
	req.Host = "127.0.0.1:7070"
	req.AddCookie(cookie1)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/auth/sessions failed: %d", rec.Code)
	}
	var list server.ActiveSessionsResponse
	_ = json.NewDecoder(rec.Body).Decode(&list)
	if len(list.Sessions) != 2 {
		t.Fatalf("expected 2 active sessions, got %d", len(list.Sessions))
	}

	var currentID, otherID string
	for _, s := range list.Sessions {
		if s.Current {
			currentID = s.ID
		} else {
			otherID = s.ID
		}
	}
	if currentID == "" || otherID == "" {
		t.Fatalf("expected 1 current and 1 other session, got %+v", list.Sessions)
	}

	// Current session cannot be revoked from list
	delCur := httptest.NewRequest(http.MethodDelete, "/api/auth/sessions/"+currentID, nil)
	delCur.Host = "127.0.0.1:7070"
	delCur.Header.Set("Origin", "http://127.0.0.1:7070")
	delCur.AddCookie(cookie1)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, delCur)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 when revoking current session, got %d", rec.Code)
	}

	// Revoke other session succeeds
	delOther := httptest.NewRequest(http.MethodDelete, "/api/auth/sessions/"+otherID, nil)
	delOther.Host = "127.0.0.1:7070"
	delOther.Header.Set("Origin", "http://127.0.0.1:7070")
	delOther.AddCookie(cookie1)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, delOther)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 when revoking other session, got %d", rec.Code)
	}
}

func TestSettingsSecurityPasswordAndTOTP_ADM4_SEC10(t *testing.T) {
	srv, st, _, cookie1 := newAuthenticatedTestServer(t)
	h := srv.Handler()

	// Create second session to verify password change ends other sessions
	login2 := postJSON(h, "/api/auth/login", server.LoginRequest{Password: "Correct-Horse-Battery-99"})
	cookie2 := findCookie(login2.Result().Cookies(), auth.SessionCookieName)

	// Wrong current password rejected
	rec := postJSON(h, "/api/settings/password", server.ChangePasswordRequest{
		CurrentPassword: "wrong-password",
		NewPassword:     "New-Strong-Passphrase-88",
	}, cookie1)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for wrong current password, got %d", rec.Code)
	}

	// Valid password change keeps cookie1 and revokes cookie2
	rec = postJSON(h, "/api/settings/password", server.ChangePasswordRequest{
		CurrentPassword: "Correct-Horse-Battery-99",
		NewPassword:     "New-Strong-Passphrase-88",
	}, cookie1)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for password change, got %d: %s", rec.Code, rec.Body.String())
	}

	// cookie2 must now be unauthorized
	req2 := httptest.NewRequest(http.MethodGet, "/api/server/status", nil)
	req2.Host = "127.0.0.1:7070"
	req2.AddCookie(cookie2)
	rec2 := httptest.NewRecorder()
	h.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusUnauthorized {
		t.Fatalf("expected revoked session cookie2 to get 401, got %d", rec2.Code)
	}

	// Enable TOTP in Settings (SEC-10 requires current password)
	rec = postJSON(h, "/api/settings/totp/init", server.SettingsTOTPInitRequest{
		CurrentPassword: "New-Strong-Passphrase-88",
	}, cookie1)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for settings/totp/init, got %d: %s", rec.Code, rec.Body.String())
	}
	var initResp server.SetupTOTPInitResponse
	_ = json.NewDecoder(rec.Body).Decode(&initResp)

	code0, _ := auth.ComputeTOTP(initResp.Secret, time.Now().Unix()/30)
	rec = postJSON(h, "/api/settings/totp/enable", server.SetupTOTPVerifyRequest{Code: code0}, cookie1)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for settings/totp/enable, got %d: %s", rec.Code, rec.Body.String())
	}
	if !auth.IsTOTPEnabled(context.Background(), st) {
		t.Fatalf("expected TOTP enabled")
	}

	// Disable TOTP requires BOTH password and current TOTP code
	rec = postJSON(h, "/api/settings/totp/disable", server.SettingsTOTPDisableRequest{
		CurrentPassword: "New-Strong-Passphrase-88",
		TOTPCode:        "",
	}, cookie1)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 when TOTP code omitted on disable, got %d", rec.Code)
	}

	code1, _ := auth.ComputeTOTP(initResp.Secret, time.Now().Unix()/30+1)
	rec = postJSON(h, "/api/settings/totp/disable", server.SettingsTOTPDisableRequest{
		CurrentPassword: "New-Strong-Passphrase-88",
		TOTPCode:        code1,
	}, cookie1)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 when disabling TOTP with password + code, got %d: %s", rec.Code, rec.Body.String())
	}
	if auth.IsTOTPEnabled(context.Background(), st) {
		t.Fatalf("expected TOTP disabled")
	}
}

func TestSettingsHookDomain_SETUP2_TLS5(t *testing.T) {
	srv, st, fh, cookie1 := newAuthenticatedTestServer(t)
	h := srv.Handler()

	// Check DNS
	rec := postJSON(h, "/api/settings/hook-domain/check", server.HookDomainCheckRequest{Domain: "hooks.example.com"}, cookie1)
	if rec.Code != http.StatusOK {
		t.Fatalf("hook-domain/check failed: %d %s", rec.Code, rec.Body.String())
	}

	// Save hook domain via PUT
	req := httptest.NewRequest(http.MethodPut, "/api/settings/hook-domain", strings.NewReader(`{"domain":"hooks.example.com"}`))
	req.Host = "127.0.0.1:7070"
	req.Header.Set("Origin", "http://127.0.0.1:7070")
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(cookie1)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT /api/settings/hook-domain failed: %d %s", rec.Code, rec.Body.String())
	}

	state, _ := auth.GetSetupState(context.Background(), st)
	if state != auth.SetupStateDomainSet {
		t.Fatalf("expected setup state domain_set, got %q", state)
	}

	data, err := fh.ReadFile(filepath.Join("/tmp/traefik-dynamic", traefik.HookDomainFileName))
	if err != nil {
		t.Fatalf("expected 01-hook-domain.yml to be written: %v", err)
	}
	if !strings.Contains(string(data), "Host(`hooks.example.com`)") {
		t.Errorf("unexpected 01-hook-domain.yml content:\n%s", string(data))
	}
}
