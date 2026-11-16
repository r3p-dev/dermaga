package server_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"

	"dermaga/internal/auth"
	"dermaga/internal/server"
	"dermaga/internal/store"
)

func setupTestServer(t *testing.T) (*server.Server, *store.Store, string) {
	t.Helper()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "server_test.db")
	sockPath := filepath.Join(tmpDir, "daemon.sock")

	st, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("store.Open failed: %v", err)
	}

	// Pre-seed admin password
	_ = auth.SetAdminPassword(context.Background(), st, "server_command", "correct-password-123")

	srv, err := server.NewServer(server.Config{
		ListenAddr: "127.0.0.1:7070",
		SocketPath: sockPath,
		Store:      st,
		Version:    "0.1.0-test",
		Commit:     "abc1234",
	})
	if err != nil {
		t.Fatalf("NewServer failed: %v", err)
	}

	t.Cleanup(func() {
		_ = srv.Close()
		_ = st.Close()
	})

	return srv, st, sockPath
}

func TestSEC3_DNSRebindingAndOriginProtection(t *testing.T) {
	srv, _, _ := setupTestServer(t)
	handler := srv.Handler()

	// 1. Foreign Host -> 421 Misdirected Request
	req := httptest.NewRequest("GET", "/api/auth/session", nil)
	req.Host = "attacker.example.com"
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusMisdirectedRequest {
		t.Errorf("expected Host attacker.example.com to return 421, got %d", rec.Code)
	}

	// 2. Valid Host 127.0.0.1:7070 -> allowed
	req = httptest.NewRequest("GET", "/api/auth/session", nil)
	req.Host = "127.0.0.1:7070"
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("expected Host 127.0.0.1:7070 to return 200, got %d", rec.Code)
	}

	// 3. Valid Host localhost:7070 -> allowed
	req = httptest.NewRequest("GET", "/api/auth/session", nil)
	req.Host = "localhost:7070"
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("expected Host localhost:7070 to return 200, got %d", rec.Code)
	}

	// 4. Mutating request without Origin -> 403 Forbidden
	loginBody, _ := json.Marshal(server.LoginRequest{Password: "test"})
	req = httptest.NewRequest("POST", "/api/auth/login", bytes.NewReader(loginBody))
	req.Host = "localhost:7070"
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Errorf("expected mutating request without Origin to return 403, got %d", rec.Code)
	}

	// 4b. Mutating request with missing Origin but present Referer -> still 403 Forbidden (SEC-3 requires matching Origin)
	req = httptest.NewRequest("POST", "/api/auth/login", bytes.NewReader(loginBody))
	req.Host = "localhost:7070"
	req.Header.Set("Referer", "http://localhost:7070/login")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Errorf("expected mutating request with missing Origin (even with Referer) to return 403, got %d", rec.Code)
	}

	// 5. Mutating request with foreign Origin -> 403 Forbidden
	req = httptest.NewRequest("POST", "/api/auth/login", bytes.NewReader(loginBody))
	req.Host = "localhost:7070"
	req.Header.Set("Origin", "http://evil-site.com")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Errorf("expected mutating request with foreign Origin to return 403, got %d", rec.Code)
	}

	// 6. Mutating request with valid Origin -> allowed through
	req = httptest.NewRequest("POST", "/api/auth/login", bytes.NewReader(loginBody))
	req.Host = "localhost:7070"
	req.Header.Set("Origin", "http://localhost:7070")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	// Invalid password should return 401, NOT 403 Forbidden
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected valid Origin with wrong password to return 401, got %d", rec.Code)
	}
}

func TestAuthFlow_LoginLogoutSession(t *testing.T) {
	srv, st, _ := setupTestServer(t)
	handler := srv.Handler()

	// 1. Initial session check (unauthenticated)
	req := httptest.NewRequest("GET", "/api/auth/session", nil)
	req.Host = "localhost:7070"
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	var sessInfo server.SessionInfo
	_ = json.NewDecoder(rec.Body).Decode(&sessInfo)
	if sessInfo.Authenticated {
		t.Errorf("expected unauthenticated session initial state")
	}

	// 2. Login with correct password
	loginBody, _ := json.Marshal(server.LoginRequest{Password: "correct-password-123"})
	req = httptest.NewRequest("POST", "/api/auth/login", bytes.NewReader(loginBody))
	req.Host = "localhost:7070"
	req.Header.Set("Origin", "http://localhost:7070")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("login failed: %d: %s", rec.Code, rec.Body.String())
	}

	cookies := rec.Result().Cookies()
	var sessionCookie *http.Cookie
	for _, c := range cookies {
		if c.Name == auth.SessionCookieName {
			sessionCookie = c
			break
		}
	}
	if sessionCookie == nil || sessionCookie.Value == "" {
		t.Fatalf("expected dermaga_session cookie to be set")
	}
	if !sessionCookie.HttpOnly {
		t.Errorf("expected session cookie to have HttpOnly=true (SEC-7)")
	}

	// Verify audit log recorded login
	logs, _ := st.ListAuditLogs(context.Background(), store.AuditFilter{Origin: "panel"})
	if len(logs) == 0 || logs[0].Event != "login" {
		t.Errorf("expected audit log event 'login', got %v", logs)
	}

	// 3. Verify session endpoint returns authenticated
	req = httptest.NewRequest("GET", "/api/auth/session", nil)
	req.Host = "localhost:7070"
	req.AddCookie(sessionCookie)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	_ = json.NewDecoder(rec.Body).Decode(&sessInfo)
	if !sessInfo.Authenticated || sessInfo.UserID != "admin" {
		t.Errorf("expected authenticated admin session, got %+v", sessInfo)
	}

	// 4. Access protected server status
	req = httptest.NewRequest("GET", "/api/server/status", nil)
	req.Host = "localhost:7070"
	req.AddCookie(sessionCookie)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected server status 200, got %d", rec.Code)
	}
	var status server.ServerStatus
	_ = json.NewDecoder(rec.Body).Decode(&status)
	if status.Version != "0.1.0-test" || !status.DatabaseOK {
		t.Errorf("unexpected status response: %+v", status)
	}

	// 5. Access audit logs with filter
	req = httptest.NewRequest("GET", "/api/audit-logs?origin=panel", nil)
	req.Host = "localhost:7070"
	req.AddCookie(sessionCookie)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected audit logs 200, got %d", rec.Code)
	}
	var auditResp server.AuditLogsResponse
	_ = json.NewDecoder(rec.Body).Decode(&auditResp)
	if len(auditResp.Logs) == 0 {
		t.Errorf("expected audit logs in response, got 0")
	}

	// 6. Logout
	req = httptest.NewRequest("POST", "/api/auth/logout", nil)
	req.Host = "localhost:7070"
	req.Header.Set("Origin", "http://localhost:7070")
	req.AddCookie(sessionCookie)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("logout failed: %d", rec.Code)
	}

	// Verify session invalidated
	req = httptest.NewRequest("GET", "/api/server/status", nil)
	req.Host = "localhost:7070"
	req.AddCookie(sessionCookie)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 Unauthorized after logout, got %d", rec.Code)
	}
}

func TestUnixSocketCommands(t *testing.T) {
	srv, st, sockPath := setupTestServer(t)

	// Start server (starts unix listener)
	go func() {
		_ = srv.Start()
	}()
	time.Sleep(100 * time.Millisecond)

	// Verify socket file permissions (0600)
	info, err := os.Stat(sockPath)
	if err != nil {
		t.Fatalf("failed to stat unix socket: %v", err)
	}
	if info.Mode().Perm() != 0600 {
		t.Errorf("expected socket permissions 0600, got %o", info.Mode().Perm())
	}

	// Helper to send recovery command over socket
	sendCmd := func(req server.RecoveryRequest) server.RecoveryResponse {
		conn, err := net.Dial("unix", sockPath)
		if err != nil {
			t.Fatalf("net.Dial unix failed: %v", err)
		}
		defer conn.Close()

		if err := json.NewEncoder(conn).Encode(req); err != nil {
			t.Fatalf("encode failed: %v", err)
		}

		var resp server.RecoveryResponse
		if err := json.NewDecoder(conn).Decode(&resp); err != nil {
			t.Fatalf("decode failed: %v", err)
		}
		return resp
	}

	// 1. Status command
	resp := sendCmd(server.RecoveryRequest{Action: "status"})
	if !resp.Success {
		t.Errorf("status command failed: %s", resp.Message)
	}

	// 2. Set password command
	resp = sendCmd(server.RecoveryRequest{Action: "set-password", Password: "recovered-password"})
	if !resp.Success {
		t.Errorf("set-password failed: %s", resp.Message)
	}

	// Verify in DB
	hash, _ := st.GetSetting(context.Background(), auth.SettingAdminPassword)
	valid, _ := auth.VerifyPassword("recovered-password", hash)
	if !valid {
		t.Errorf("password in DB does not match recovered password")
	}

	// 3. Disable TOTP command
	resp = sendCmd(server.RecoveryRequest{Action: "totp-disable"})
	if !resp.Success {
		t.Errorf("totp-disable failed: %s", resp.Message)
	}
	totp, _ := st.GetSetting(context.Background(), auth.SettingTOTPEnabled)
	if totp != "false" {
		t.Errorf("expected totp to be 'false', got %q", totp)
	}
}

func TestAUD2_FilterByTime(t *testing.T) {
	srv, st, _ := setupTestServer(t)
	handler := srv.Handler()

	// 1. Create admin session
	token, _ := auth.GenerateSessionToken()
	tokenHash := auth.HashSessionToken(token)
	_ = st.CreateSession(context.Background(), tokenHash, "admin", time.Now().Add(1*time.Hour))
	sessionCookie := &http.Cookie{Name: auth.SessionCookieName, Value: token}

	// 2. Insert audit logs at distinct times
	t1 := time.Now().Add(-2 * time.Hour).UTC().Truncate(time.Second)
	t2 := time.Now().Add(-1 * time.Hour).UTC().Truncate(time.Second)
	t3 := time.Now().UTC().Truncate(time.Second)

	_, err := st.DB().Exec(`
		INSERT INTO audit_logs (origin, event, actor, description, details, created_at)
		VALUES ('panel', 'event_old', 'admin', 'old log', '{}', ?),
		       ('panel', 'event_mid', 'admin', 'mid log', '{}', ?),
		       ('panel', 'event_new', 'admin', 'new log', '{}', ?)
	`, t1.Format(time.RFC3339), t2.Format(time.RFC3339), t3.Format(time.RFC3339))
	if err != nil {
		t.Fatalf("failed to insert test audit logs: %v", err)
	}

	fetchLogs := func(query string) []server.AuditLogEntry {
		req := httptest.NewRequest("GET", "/api/audit-logs"+query, nil)
		req.Host = "localhost:7070"
		req.AddCookie(sessionCookie)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("fetch audit logs failed with %d: %s", rec.Code, rec.Body.String())
		}
		var resp server.AuditLogsResponse
		_ = json.NewDecoder(rec.Body).Decode(&resp)
		return resp.Logs
	}

	// 3. Filter since t2 -> event_mid and event_new, NOT event_old
	sinceQuery := fmt.Sprintf("?since=%s", url.QueryEscape(t2.Format(time.RFC3339)))
	logsSince := fetchLogs(sinceQuery)
	for _, l := range logsSince {
		if l.Event == "event_old" {
			t.Errorf("TestAUD2: expected event_old to be filtered out with since=%s", t2.Format(time.RFC3339))
		}
	}

	// 4. Filter until t2 -> event_old and event_mid, NOT event_new
	untilQuery := fmt.Sprintf("?until=%s", url.QueryEscape(t2.Format(time.RFC3339)))
	logsUntil := fetchLogs(untilQuery)
	for _, l := range logsUntil {
		if l.Event == "event_new" {
			t.Errorf("TestAUD2: expected event_new to be filtered out with until=%s", t2.Format(time.RFC3339))
		}
	}

	// 5. Filter window (since t2 & until t2) -> only event_mid
	windowQuery := fmt.Sprintf("?since=%s&until=%s", url.QueryEscape(t2.Format(time.RFC3339)), url.QueryEscape(t2.Format(time.RFC3339)))
	logsWindow := fetchLogs(windowQuery)
	foundMid := false
	for _, l := range logsWindow {
		if l.Event == "event_mid" {
			foundMid = true
		}
		if l.Event == "event_old" || l.Event == "event_new" {
			t.Errorf("TestAUD2: unexpected log %s in bounded time window", l.Event)
		}
	}
	if !foundMid {
		t.Errorf("TestAUD2: expected event_mid in time window query, got %+v", logsWindow)
	}
}
