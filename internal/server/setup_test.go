package server_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"dermaga/internal/auth"
	"dermaga/internal/crypto"
	"dermaga/internal/server"
	"dermaga/internal/store"
)

func newTestServerWithDB(t *testing.T) (*server.Server, *store.Store) {
	t.Helper()
	tmpDir := t.TempDir()
	st, err := store.Open(filepath.Join(tmpDir, "test.db"))
	if err != nil {
		t.Fatalf("store.Open failed: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	key, err := crypto.GenerateRandomKey()
	if err != nil {
		t.Fatalf("GenerateRandomKey failed: %v", err)
	}

	srv, err := server.NewServer(server.Config{
		ListenAddr: "127.0.0.1:0",
		Store:      st,
		MasterKey:  key,
		Version:    "test",
		Commit:     "abc1234",
	})
	if err != nil {
		t.Fatalf("NewServer failed: %v", err)
	}
	return srv, st
}

func postJSON(h http.Handler, path string, body any, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(b))
	req.Host = "127.0.0.1:7070"
	req.Header.Set("Origin", "http://127.0.0.1:7070")
	req.Header.Set("Content-Type", "application/json")
	req.RemoteAddr = "127.0.0.1:12345"
	for _, c := range cookies {
		req.AddCookie(c)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func findCookie(cookies []*http.Cookie, name string) *http.Cookie {
	for _, c := range cookies {
		if c.Name == name && c.MaxAge >= 0 && c.Value != "" {
			return c
		}
	}
	return nil
}

func TestSetupModeEnforcement_SETUP1_SEC3(t *testing.T) {
	srv, _ := newTestServerWithDB(t)
	h := srv.Handler()

	// Every UI page except /setup redirects to /setup
	for _, path := range []string{"/", "/login", "/audit-logs", "/unknown"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Host = "127.0.0.1:7070"
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusFound || rec.Header().Get("Location") != "/setup" {
			t.Errorf("GET %s in setup mode: expected 302 -> /setup, got %d (%q)", path, rec.Code, rec.Header().Get("Location"))
		}
	}

	// /setup itself is served (200)
	req := httptest.NewRequest(http.MethodGet, "/setup", nil)
	req.Host = "127.0.0.1:7070"
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("GET /setup in setup mode: expected 200, got %d", rec.Code)
	}

	// API routes refuse in setup mode
	for _, path := range []string{"/api/server/status", "/api/audit-logs"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Host = "127.0.0.1:7070"
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusForbidden {
			t.Errorf("GET %s in setup mode: expected 403, got %d", path, rec.Code)
		}
	}
	rec = postJSON(h, "/api/auth/login", server.LoginRequest{Password: "anything"})
	if rec.Code != http.StatusForbidden {
		t.Errorf("POST /api/auth/login in setup mode: expected 403, got %d", rec.Code)
	}

	// SEC-3 on /api/setup/verify-token: foreign Host 421, missing Origin 403
	badHost := httptest.NewRequest(http.MethodPost, "/api/setup/verify-token", strings.NewReader(`{"token":"x"}`))
	badHost.Host = "evil.example:7070"
	badHost.Header.Set("Origin", "http://127.0.0.1:7070")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, badHost)
	if rec.Code != http.StatusMisdirectedRequest {
		t.Errorf("foreign Host on /api/setup/verify-token: expected 421, got %d", rec.Code)
	}

	noOrigin := httptest.NewRequest(http.MethodPost, "/api/setup/verify-token", strings.NewReader(`{"token":"x"}`))
	noOrigin.Host = "127.0.0.1:7070"
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, noOrigin)
	if rec.Code != http.StatusForbidden {
		t.Errorf("missing Origin on /api/setup/verify-token: expected 403, got %d", rec.Code)
	}
}

func TestSetupFlowWithTOTP_SETUP2_SEC8_ADM1(t *testing.T) {
	ctx := context.Background()
	srv, st := newTestServerWithDB(t)
	h := srv.Handler()

	token, err := auth.IssueSetupToken(ctx, st, "server_command", time.Now())
	if err != nil {
		t.Fatalf("IssueSetupToken failed: %v", err)
	}

	// Invalid token rejected
	rec := postJSON(h, "/api/setup/verify-token", server.SetupVerifyTokenRequest{Token: "invalid"})
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for invalid token, got %d", rec.Code)
	}

	// Valid token exchanged for setup session cookie
	rec = postJSON(h, "/api/setup/verify-token", server.SetupVerifyTokenRequest{Token: token})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for valid token, got %d: %s", rec.Code, rec.Body.String())
	}
	setupCookie := findCookie(rec.Result().Cookies(), auth.SetupSessionCookieName)
	if setupCookie == nil {
		t.Fatalf("expected %s cookie to be set", auth.SetupSessionCookieName)
	}

	// Reuse of same setup token rejected
	rec = postJSON(h, "/api/setup/verify-token", server.SetupVerifyTokenRequest{Token: token})
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 on setup token reuse, got %d", rec.Code)
	}

	// Password policy enforced server-side (< 8 chars rejected)
	rec = postJSON(h, "/api/setup/password", server.SetupPasswordRequest{Password: "short"}, setupCookie)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for short password, got %d", rec.Code)
	}

	// Valid password accepted
	pass := "correct-horse-battery-99"
	rec = postJSON(h, "/api/setup/password", server.SetupPasswordRequest{Password: pass}, setupCookie)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for valid password, got %d: %s", rec.Code, rec.Body.String())
	}

	// Init TOTP enrolment
	rec = postJSON(h, "/api/setup/totp/init", map[string]any{}, setupCookie)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for totp/init, got %d: %s", rec.Code, rec.Body.String())
	}
	var initResp server.SetupTOTPInitResponse
	if err := json.NewDecoder(rec.Body).Decode(&initResp); err != nil {
		t.Fatalf("decode totp/init: %v", err)
	}
	if initResp.Secret == "" || !strings.HasPrefix(initResp.QRSVG, "<svg ") {
		t.Fatalf("expected secret and QR SVG in totp/init response")
	}

	// Wrong TOTP code rejected
	rec = postJSON(h, "/api/setup/totp/verify", server.SetupTOTPVerifyRequest{Code: "000000"}, setupCookie)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for wrong TOTP code, got %d", rec.Code)
	}

	// Valid TOTP code enables TOTP and issues normal admin session cookie
	code, err := auth.ComputeTOTP(initResp.Secret, time.Now().Unix()/30)
	if err != nil {
		t.Fatalf("ComputeTOTP failed: %v", err)
	}
	rec = postJSON(h, "/api/setup/totp/verify", server.SetupTOTPVerifyRequest{Code: code}, setupCookie)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for totp/verify, got %d: %s", rec.Code, rec.Body.String())
	}
	adminCookie := findCookie(rec.Result().Cookies(), auth.SessionCookieName)
	if adminCookie == nil {
		t.Fatalf("expected admin session cookie after setup completion")
	}

	// After setup completes, /setup redirects to /login
	req := httptest.NewRequest(http.MethodGet, "/setup", nil)
	req.Host = "127.0.0.1:7070"
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusFound || rec.Header().Get("Location") != "/login" {
		t.Errorf("GET /setup after completion: expected 302 -> /login, got %d", rec.Code)
	}

	// Login without TOTP code prompts totp_required=true
	rec = postJSON(h, "/api/auth/login", server.LoginRequest{Password: pass})
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 when TOTP code omitted, got %d", rec.Code)
	}
	var loginResp server.LoginResponse
	_ = json.NewDecoder(rec.Body).Decode(&loginResp)
	if !loginResp.TOTPRequired {
		t.Errorf("expected totp_required=true")
	}

	// Reusing the same TOTP code used during setup is rejected (replay protection)
	rec = postJSON(h, "/api/auth/login", server.LoginRequest{Password: pass, TOTPCode: code})
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 on TOTP replay, got %d", rec.Code)
	}

	// Next step TOTP code succeeds
	nextCode, _ := auth.ComputeTOTP(initResp.Secret, time.Now().Unix()/30+1)
	rec = postJSON(h, "/api/auth/login", server.LoginRequest{Password: pass, TOTPCode: nextCode})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 with valid TOTP code, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestSetupFlowSkipTOTPAndSocketToken_SETUP2_SETUP3(t *testing.T) {
	tmpDir := t.TempDir()
	st, err := store.Open(filepath.Join(tmpDir, "skip.db"))
	if err != nil {
		t.Fatalf("store.Open failed: %v", err)
	}
	defer st.Close()
	key, _ := crypto.GenerateRandomKey()
	sockPath := filepath.Join(tmpDir, "daemon.sock")

	srv, err := server.NewServer(server.Config{
		ListenAddr: "127.0.0.1:0",
		SocketPath: sockPath,
		Store:      st,
		MasterKey:  key,
	})
	if err != nil {
		t.Fatalf("NewServer failed: %v", err)
	}
	go func() { _ = srv.Start() }()
	defer srv.Close()

	// Wait briefly for unix socket
	var conn net.Conn
	for i := 0; i < 20; i++ {
		conn, err = net.DialTimeout("unix", sockPath, time.Second)
		if err == nil {
			break
		}
		time.Sleep(25 * time.Millisecond)
	}
	if err != nil {
		t.Fatalf("failed to dial unix socket: %v", err)
	}
	_ = json.NewEncoder(conn).Encode(server.RecoveryRequest{Action: "setup-token"})
	var sockResp server.RecoveryResponse
	_ = json.NewDecoder(conn).Decode(&sockResp)
	_ = conn.Close()
	if !sockResp.Success || sockResp.Token == "" {
		t.Fatalf("expected setup-token over unix socket to succeed, got %+v", sockResp)
	}

	h := srv.Handler()
	rec := postJSON(h, "/api/setup/verify-token", server.SetupVerifyTokenRequest{Token: sockResp.Token})
	if rec.Code != http.StatusOK {
		t.Fatalf("verify-token failed: %d", rec.Code)
	}
	setupCookie := findCookie(rec.Result().Cookies(), auth.SetupSessionCookieName)

	rec = postJSON(h, "/api/setup/password", server.SetupPasswordRequest{Password: "correct-horse-battery"}, setupCookie)
	if rec.Code != http.StatusOK {
		t.Fatalf("password failed: %d", rec.Code)
	}

	rec = postJSON(h, "/api/setup/totp/skip", map[string]any{}, setupCookie)
	if rec.Code != http.StatusOK {
		t.Fatalf("totp/skip failed: %d", rec.Code)
	}
	if findCookie(rec.Result().Cookies(), auth.SessionCookieName) == nil {
		t.Fatalf("expected admin session cookie after skip")
	}
	if auth.IsTOTPEnabled(context.Background(), st) {
		t.Fatalf("expected TOTP to remain disabled after skip")
	}
}
