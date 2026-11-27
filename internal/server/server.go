// Package server implements the loopback HTTP server and Unix domain socket server
// for the dermaga panel and CLI recovery commands.
package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"dermaga/internal/access"
	"dermaga/internal/auth"
	"dermaga/internal/crypto"
	"dermaga/internal/deploy"
	"dermaga/internal/github"
	"dermaga/internal/host"
	"dermaga/internal/reconciler"
	"dermaga/internal/store"
)

// EnsureNonRoot validates SEC-1: dermaga refuses to run with root privileges.
func EnsureNonRoot() error {
	if os.Geteuid() == 0 {
		return errors.New("dermaga refuses to run as root (SEC-1)")
	}
	return nil
}

// Config specifies the runtime configuration for the dermaga server.
type Config struct {
	ListenAddr string // Defaults to "127.0.0.1:7070"
	SocketPath string // Defaults to "$XDG_RUNTIME_DIR/dermaga/daemon.sock"
	Store      *store.Store
	Reconciler *reconciler.Reconciler
	Deploy     *deploy.Engine
	Host       host.Host
	GitHub     *github.Client
	DNSLookup  access.DNSLookup
	MasterKey  []byte
	Version    string
	Commit     string
	Now        func() time.Time
}

// Server encapsulates the HTTP and Unix socket listeners.
type Server struct {
	cfg           Config
	httpServer    *http.Server
	unixListener  net.Listener
	rateLimiter   *auth.RateLimiter
	startedAt     time.Time
	mu            sync.Mutex
	manifestState map[string]pendingManifestState
}

// NewServer initializes the Server instance.
func NewServer(cfg Config) (*Server, error) {
	if err := EnsureNonRoot(); err != nil {
		return nil, err
	}

	if cfg.ListenAddr == "" {
		cfg.ListenAddr = "127.0.0.1:7070"
	}
	if cfg.Version == "" {
		cfg.Version = "dev"
	}

	s := &Server{
		cfg:           cfg,
		rateLimiter:   auth.NewRateLimiter(),
		startedAt:     time.Now(),
		manifestState: make(map[string]pendingManifestState),
	}

	mux := http.NewServeMux()
	s.registerRoutes(mux)

	// Wrap mux with DNS-rebinding protection and timeout middleware (SEC-3, SEC-15)
	handler := s.securityMiddleware(mux)

	s.httpServer = &http.Server{
		Addr:              cfg.ListenAddr,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	return s, nil
}

// securityMiddleware enforces SEC-3:
// 1. Host header must resolve to 127.0.0.1 or localhost (returns 421 Misdirected Request if invalid).
// 2. Mutating requests (POST, PUT, DELETE, PATCH) must include an Origin header matching 127.0.0.1 or localhost (returns 403 Forbidden).
func (s *Server) securityMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 1. Host verification
		host := r.Host
		if h, _, err := net.SplitHostPort(host); err == nil {
			host = h
		}
		host = strings.ToLower(host)

		if host != "127.0.0.1" && host != "localhost" {
			http.Error(w, "Misdirected Request: host must be 127.0.0.1 or localhost (SEC-3)", http.StatusMisdirectedRequest)
			return
		}

		// 2. Origin verification on mutating methods (SEC-3)
		switch r.Method {
		case http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodPatch:
			origin := r.Header.Get("Origin")
			if origin == "" {
				http.Error(w, "Forbidden: missing Origin header on mutating request (SEC-3)", http.StatusForbidden)
				return
			}

			u, err := url.Parse(origin)
			if err != nil {
				http.Error(w, "Forbidden: invalid Origin header (SEC-3)", http.StatusForbidden)
				return
			}

			originHost := u.Hostname()
			if originHost != "127.0.0.1" && originHost != "localhost" {
				http.Error(w, "Forbidden: foreign Origin rejected (SEC-3)", http.StatusForbidden)
				return
			}
		}

		next.ServeHTTP(w, r)
	})
}

// registerRoutes configures all HTTP endpoints.
func (s *Server) registerRoutes(mux *http.ServeMux) {
	// Setup routes (SETUP-1, SETUP-2, SEC-8)
	mux.HandleFunc("POST /api/setup/verify-token", s.handleSetupVerifyToken)
	mux.HandleFunc("POST /api/setup/password", s.requireSetupSession(s.handleSetupPassword))
	mux.HandleFunc("POST /api/setup/totp/init", s.requireSetupSession(s.handleSetupTOTPInit))
	mux.HandleFunc("POST /api/setup/totp/verify", s.requireSetupSession(s.handleSetupTOTPVerify))
	mux.HandleFunc("POST /api/setup/totp/skip", s.requireSetupSession(s.handleSetupTOTPSkip))

	// Authentication routes
	mux.HandleFunc("POST /api/auth/login", s.handleLogin)
	mux.HandleFunc("POST /api/auth/logout", s.handleLogout)
	mux.HandleFunc("GET /api/auth/session", s.handleSession)
	mux.HandleFunc("GET /api/auth/sessions", s.requireAuth(s.handleListSessions))
	mux.HandleFunc("DELETE /api/auth/sessions/{id}", s.requireAuth(s.handleRevokeSession))
	mux.HandleFunc("POST /api/auth/sessions/revoke-others", s.requireAuth(s.handleRevokeOtherSessions))

	// Settings routes (LAP-3, ADM-4, SEC-10, SETUP-2)
	mux.HandleFunc("GET /api/settings/access", s.requireAuth(s.handleGetAccessSettings))
	mux.HandleFunc("POST /api/settings/password", s.requireAuth(s.handleChangePassword))
	mux.HandleFunc("POST /api/settings/totp/init", s.requireAuth(s.handleSettingsTOTPInit))
	mux.HandleFunc("POST /api/settings/totp/enable", s.requireAuth(s.handleSettingsTOTPEnable))
	mux.HandleFunc("POST /api/settings/totp/disable", s.requireAuth(s.handleSettingsTOTPDisable))
	mux.HandleFunc("GET /api/settings/hook-domain", s.requireAuth(s.handleGetHookDomain))
	mux.HandleFunc("POST /api/settings/hook-domain/check", s.requireAuth(s.handleCheckHookDomainDNS))
	mux.HandleFunc("PUT /api/settings/hook-domain", s.requireAuth(s.handleSaveHookDomain))
	mux.HandleFunc("GET /api/settings/github", s.requireAuth(s.handleGetGitHubStatus))
	mux.HandleFunc("POST /api/settings/github/manifest", s.requireAuth(s.handleStartGitHubManifest))
	mux.HandleFunc("POST /api/settings/github/callback", s.requireAuth(s.handleGitHubCallback))
	mux.HandleFunc("POST /api/settings/github/sync", s.requireAuth(s.handleSyncGitHub))
	mux.HandleFunc("DELETE /api/settings/github", s.requireAuth(s.handleDisconnectGitHub))

	// Wizard & Apps routes (WIZ-1–WIZ-6, APP-1, APP-5–APP-7, APP-9, DEP-3, DEP-5–DEP-9, LOG-1–LOG-2)
	mux.HandleFunc("GET /api/github/repos/{owner}/{repo}/branches", s.requireAuth(s.handleListRepoBranches))
	mux.HandleFunc("GET /api/github/repos/{owner}/{repo}/inspect", s.requireAuth(s.handleInspectRepo))
	mux.HandleFunc("POST /api/domains/check", s.requireAuth(s.handleCheckDomainDNS))
	mux.HandleFunc("GET /api/apps", s.requireAuth(s.handleListApps))
	mux.HandleFunc("POST /api/apps", s.requireAuth(s.handleCreateApp))
	mux.HandleFunc("GET /api/apps/{id}", s.requireAuth(s.handleGetApp))
	mux.HandleFunc("PATCH /api/apps/{id}", s.requireAuth(s.handleUpdateAppSettings))
	mux.HandleFunc("DELETE /api/apps/{id}", s.requireAuth(s.handleDeleteApp))
	mux.HandleFunc("PUT /api/apps/{id}/env", s.requireAuth(s.handleUpdateAppEnv))
	mux.HandleFunc("POST /api/apps/{id}/env/reveal", s.requireAuth(s.handleRevealAppEnv))
	mux.HandleFunc("POST /api/apps/{id}/deploy", s.requireAuth(s.handleTriggerDeploy))
	mux.HandleFunc("GET /api/apps/{id}/deployments", s.requireAuth(s.handleListAppDeployments))
	mux.HandleFunc("POST /api/apps/{id}/rollback", s.requireAuth(s.handleRollback))
	mux.HandleFunc("GET /api/apps/{id}/rollback-preview", s.requireAuth(s.handleRollbackPreview))
	mux.HandleFunc("GET /api/apps/{id}/memory-check", s.requireAuth(s.handleMemoryCheck))
	mux.HandleFunc("GET /api/apps/{id}/domains", s.requireAuth(s.handleListDomains))
	mux.HandleFunc("PUT /api/apps/{id}/domains", s.requireAuth(s.handleUpdateDomains))
	mux.HandleFunc("POST /api/apps/{id}/domains/check", s.requireAuth(s.handleCheckDomainsAgain))
	mux.HandleFunc("GET /api/apps/{id}/logs/stream", s.requireAuth(s.handleStreamRuntimeLogs))
	mux.HandleFunc("POST /api/deployments/{id}/cancel", s.requireAuth(s.handleCancelDeployment))
	mux.HandleFunc("GET /api/deployments/{id}/logs/stream", s.requireAuth(s.handleStreamBuildLogs))

	// Audit logs
	mux.HandleFunc("GET /api/audit-logs", s.requireAuth(s.handleAuditLogs))

	// Server status
	mux.HandleFunc("GET /api/server/status", s.requireAuth(s.handleServerStatus))

	// Embedded UI with server-enforced setup mode redirect (SETUP-1)
	mux.Handle("/", s.setupRedirectMiddleware(DistHandler()))
}

func isStaticAssetPath(path string) bool {
	if strings.HasPrefix(path, "/_app/") {
		return true
	}
	switch path {
	case "/logo.svg", "/favicon.ico", "/favicon.png", "/favicon-16x16.png", "/favicon.svg", "/robots.txt":
		return true
	}
	return false
}

func (s *Server) setupRedirectMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") {
			http.NotFound(w, r)
			return
		}
		if !isStaticAssetPath(r.URL.Path) {
			setupMode, err := auth.IsSetupMode(r.Context(), s.cfg.Store)
			if err == nil {
				isSetupRoute := r.URL.Path == "/setup" || r.URL.Path == "/setup/"
				if setupMode && !isSetupRoute {
					http.Redirect(w, r, "/setup", http.StatusFound)
					return
				}
				if !setupMode && isSetupRoute && !s.hasValidSetupSession(r) {
					allowStep3 := false
					if state, stateErr := auth.GetSetupState(r.Context(), s.cfg.Store); stateErr == nil && state == auth.SetupStateAdminCreated {
						if hash := sessionTokenHashFromRequest(r); hash != "" {
							if sess, sessErr := s.cfg.Store.GetSession(r.Context(), hash); sessErr == nil && sess != nil && time.Now().Before(sess.ExpiresAt) {
								allowStep3 = true
							}
						}
					}
					if !allowStep3 {
						http.Redirect(w, r, "/login", http.StatusFound)
						return
					}
				}
			}
		}
		next.ServeHTTP(w, r)
	})
}

func clientIPFromRequest(r *http.Request) string {
	clientIP, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return clientIP
}

// requireAuth middleware validates the session cookie and refuses API access in setup mode.
func (s *Server) requireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if setupMode, err := auth.IsSetupMode(r.Context(), s.cfg.Store); err == nil && setupMode {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"error":"setup required"}`))
			return
		}

		cookie, err := r.Cookie(auth.SessionCookieName)
		if err != nil || cookie.Value == "" {
			http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
			return
		}

		tokenHash := auth.HashSessionToken(cookie.Value)
		sess, err := s.cfg.Store.GetSession(r.Context(), tokenHash)
		if err != nil || sess == nil {
			http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
			return
		}

		// Check idle timeout
		now := time.Now()
		if now.After(sess.ExpiresAt) {
			_ = s.cfg.Store.DeleteSession(r.Context(), tokenHash)
			http.Error(w, `{"error":"session expired"}`, http.StatusUnauthorized)
			return
		}

		// Check absolute timeout (7 days from creation)
		if now.After(sess.CreatedAt.Add(auth.AbsoluteTimeout)) {
			_ = s.cfg.Store.DeleteSession(r.Context(), tokenHash)
			http.Error(w, `{"error":"absolute session timeout reached"}`, http.StatusUnauthorized)
			return
		}

		// Update last_seen_at at most once per minute (ADM-5)
		if now.Sub(sess.LastSeenAt) >= time.Minute {
			_ = s.cfg.Store.TouchSession(r.Context(), tokenHash, now, now.Add(auth.IdleTimeout))
		}

		next.ServeHTTP(w, r)
	}
}

// handleLogin handles admin password and optional TOTP authentication (ADM-1, ADM-2).
func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	clientIP := clientIPFromRequest(r)

	// Rate limit check
	if delay, allowed := s.rateLimiter.Check(clientIP); !allowed {
		w.Header().Set("Retry-After", strconv.Itoa(int(delay.Seconds())+1))
		w.WriteHeader(http.StatusTooManyRequests)
		_ = json.NewEncoder(w).Encode(LoginResponse{
			Success: false,
			Error:   fmt.Sprintf("Too many failed attempts. Try again in %d seconds.", int(delay.Seconds())+1),
		})
		return
	}

	var req LoginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(LoginResponse{Success: false, Error: "Invalid JSON payload"})
		return
	}

	// Verify admin password
	storedHash, err := s.cfg.Store.GetSetting(r.Context(), auth.SettingAdminPassword)
	if errors.Is(err, store.ErrNotFound) {
		w.WriteHeader(http.StatusForbidden)
		_ = json.NewEncoder(w).Encode(LoginResponse{Success: false, Error: "Setup required. Open /setup to configure the server."})
		return
	} else if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(LoginResponse{Success: false, Error: "Database error"})
		return
	}

	valid, err := auth.VerifyPassword(req.Password, storedHash)
	if err != nil || !valid {
		backoff := s.rateLimiter.RecordFailure(clientIP)
		_ = s.cfg.Store.RecordAudit(r.Context(), "panel", clientIP, "login_failed", "Failed admin login attempt", nil, nil)

		w.WriteHeader(http.StatusUnauthorized)
		resp := LoginResponse{Success: false, Error: "Invalid password"}
		if backoff > 0 {
			resp.Error = fmt.Sprintf("Invalid password. Backoff delay: %d seconds.", int(backoff.Seconds()))
		}
		_ = json.NewEncoder(w).Encode(resp)
		return
	}

	// Verify TOTP when enabled (ADM-1)
	if auth.IsTOTPEnabled(r.Context(), s.cfg.Store) {
		if strings.TrimSpace(req.TOTPCode) == "" {
			w.WriteHeader(http.StatusUnauthorized)
			_ = json.NewEncoder(w).Encode(LoginResponse{
				Success:      false,
				TOTPRequired: true,
				Error:        "Enter your 6-digit authenticator code.",
			})
			return
		}

		sealed, err := s.cfg.Store.GetSetting(r.Context(), auth.SettingTOTPSecret)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			_ = json.NewEncoder(w).Encode(LoginResponse{Success: false, Error: "Failed to load TOTP configuration"})
			return
		}
		secretBytes, err := crypto.Decrypt(s.cfg.MasterKey, auth.SettingTOTPSecret, sealed)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			_ = json.NewEncoder(w).Encode(LoginResponse{Success: false, Error: "Failed to decrypt TOTP configuration"})
			return
		}

		step, err := auth.VerifyTOTPCode(string(secretBytes), req.TOTPCode, time.Now(), auth.LastTOTPStep(r.Context(), s.cfg.Store))
		if err != nil {
			backoff := s.rateLimiter.RecordFailure(clientIP)
			_ = s.cfg.Store.RecordAudit(r.Context(), "panel", clientIP, "login_failed", "Failed admin TOTP verification", nil, nil)

			w.WriteHeader(http.StatusUnauthorized)
			msg := "Invalid authenticator code"
			if errors.Is(err, auth.ErrTOTPCodeReused) {
				msg = "That code was already used. Wait for the next code."
			}
			if backoff > 0 {
				msg = fmt.Sprintf("%s. Backoff delay: %d seconds.", msg, int(backoff.Seconds()))
			}
			_ = json.NewEncoder(w).Encode(LoginResponse{Success: false, TOTPRequired: true, Error: msg})
			return
		}
		_ = auth.SaveLastTOTPStep(r.Context(), s.cfg.Store, step)
	}

	// Clear failure count on successful authentication
	s.rateLimiter.RecordSuccess(clientIP)

	// Create session
	if err := s.issueAdminSessionCookie(w, r); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(LoginResponse{Success: false, Error: "Failed to record session"})
		return
	}

	_ = s.cfg.Store.RecordAudit(r.Context(), "panel", "admin", "login", "Admin logged in successfully", nil, nil)

	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(LoginResponse{Success: true})
}

// handleLogout ends the session (ADM-3).
func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie(auth.SessionCookieName)
	if err == nil && cookie.Value != "" {
		tokenHash := auth.HashSessionToken(cookie.Value)
		_ = s.cfg.Store.DeleteSession(r.Context(), tokenHash)
	}

	// Clear cookie
	http.SetCookie(w, &http.Cookie{
		Name:     auth.SessionCookieName,
		Value:    "",
		Path:     "/",
		Expires:  time.Unix(0, 0),
		MaxAge:   -1,
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
	})

	_ = s.cfg.Store.RecordAudit(r.Context(), "panel", "admin", "logout", "Admin logged out", nil, nil)

	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]bool{"success": true})
}

// handleSession returns authentication and setup status.
func (s *Server) handleSession(w http.ResponseWriter, r *http.Request) {
	state, err := auth.GetSetupState(r.Context(), s.cfg.Store)
	if err != nil {
		state = auth.SetupStateUninitialized
	}

	resp := SessionInfo{
		SetupRequired:      state == auth.SetupStateUninitialized,
		SetupAuthenticated: s.hasValidSetupSession(r),
		SetupState:         string(state),
		TOTPEnabled:        auth.IsTOTPEnabled(r.Context(), s.cfg.Store),
	}

	cookie, err := r.Cookie(auth.SessionCookieName)
	if err == nil && cookie.Value != "" && !resp.SetupRequired {
		tokenHash := auth.HashSessionToken(cookie.Value)
		sess, err := s.cfg.Store.GetSession(r.Context(), tokenHash)
		if err == nil && sess != nil {
			if time.Now().Before(sess.ExpiresAt) && time.Now().Before(sess.CreatedAt.Add(auth.AbsoluteTimeout)) {
				resp.Authenticated = true
				resp.UserID = sess.UserID
				resp.ExpiresAt = sess.ExpiresAt
			}
		}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

// handleAuditLogs lists audit logs with filtering (AUD-2).
func (s *Server) handleAuditLogs(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	filter := store.AuditFilter{
		Origin: q.Get("origin"),
	}

	if appID := q.Get("app_id"); appID != "" {
		filter.AppID = &appID
	}
	if sinceStr := q.Get("since"); sinceStr != "" {
		if t, err := time.Parse(time.RFC3339, sinceStr); err == nil {
			filter.Since = &t
		}
	}
	if untilStr := q.Get("until"); untilStr != "" {
		if t, err := time.Parse(time.RFC3339, untilStr); err == nil {
			filter.Until = &t
		}
	}
	if limitStr := q.Get("limit"); limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil {
			filter.Limit = l
		}
	}
	if offsetStr := q.Get("offset"); offsetStr != "" {
		if o, err := strconv.Atoi(offsetStr); err == nil {
			filter.Offset = o
		}
	}

	logs, err := s.cfg.Store.ListAuditLogs(r.Context(), filter)
	if err != nil {
		http.Error(w, `{"error":"failed to fetch audit logs"}`, http.StatusInternalServerError)
		return
	}

	var entries []AuditLogEntry
	for _, l := range logs {
		entries = append(entries, AuditLogEntry{
			ID:          l.ID,
			Origin:      l.Origin,
			AppID:       l.AppID,
			Event:       l.Event,
			Actor:       l.Actor,
			Description: l.Description,
			Details:     l.Details,
			CreatedAt:   l.CreatedAt,
		})
	}

	resp := AuditLogsResponse{
		Logs:   entries,
		Total:  len(entries),
		Limit:  filter.Limit,
		Offset: filter.Offset,
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

// handleServerStatus returns daemon runtime status (DASH-1).
func readProcRSS() uint64 {
	data, err := os.ReadFile("/proc/self/status")
	if err != nil {
		var mem runtime.MemStats
		runtime.ReadMemStats(&mem)
		return mem.Sys
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "VmRSS:") {
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				kb, err := strconv.ParseUint(fields[1], 10, 64)
				if err == nil {
					return kb * 1024
				}
			}
		}
	}
	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)
	return mem.Sys
}

func (s *Server) handleServerStatus(w http.ResponseWriter, r *http.Request) {
	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)

	ver, _ := s.cfg.Store.CurrentVersion(r.Context())

	status := ServerStatus{
		Version:     s.cfg.Version,
		Commit:      s.cfg.Commit,
		StartedAt:   s.startedAt,
		UptimeSec:   int64(time.Since(s.startedAt).Seconds()),
		DatabaseOK:  true,
		SchemaVer:   ver,
		TotalMemory: mem.Sys,
		UsedMemory:  mem.Alloc,
		DaemonRSS:   readProcRSS(),

		CertificateWarnings: s.certificateWarnings(r.Context()),
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(status)
}

// Start launches both the HTTP server and Unix domain socket listener.
func (s *Server) Start() error {
	if s.cfg.SocketPath != "" {
		if err := s.startUnixSocket(); err != nil {
			return fmt.Errorf("starting unix socket: %w", err)
		}
	}

	return s.httpServer.ListenAndServe()
}

// startUnixSocket initializes the Unix domain socket for CLI recovery commands (SRV-2).
func (s *Server) startUnixSocket() error {
	dir := filepath.Dir(s.cfg.SocketPath)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("creating socket directory: %w", err)
	}

	_ = os.Remove(s.cfg.SocketPath)

	l, err := net.Listen("unix", s.cfg.SocketPath)
	if err != nil {
		return fmt.Errorf("listening on unix socket: %w", err)
	}

	// Enforce 0600 socket permissions
	if err := os.Chmod(s.cfg.SocketPath, 0600); err != nil {
		_ = l.Close()
		return fmt.Errorf("chmod unix socket: %w", err)
	}

	s.unixListener = l

	go s.serveUnixSocket(l)
	return nil
}

// serveUnixSocket handles incoming recovery connections over daemon.sock (SRV-2).
func (s *Server) serveUnixSocket(l net.Listener) {
	for {
		conn, err := l.Accept()
		if err != nil {
			return
		}
		go s.handleUnixConn(conn)
	}
}

// handleUnixConn processes a single CLI command over daemon.sock.
func (s *Server) handleUnixConn(conn net.Conn) {
	defer conn.Close()

	var req RecoveryRequest
	if err := json.NewDecoder(conn).Decode(&req); err != nil {
		_ = json.NewEncoder(conn).Encode(RecoveryResponse{Success: false, Message: "invalid command request"})
		return
	}

	ctx := context.Background()

	switch req.Action {
	case "set-password":
		if req.Password == "" {
			_ = json.NewEncoder(conn).Encode(RecoveryResponse{Success: false, Message: "password cannot be empty"})
			return
		}
		if err := auth.SetAdminPassword(ctx, s.cfg.Store, "server_command", req.Password); err != nil {
			_ = json.NewEncoder(conn).Encode(RecoveryResponse{Success: false, Message: err.Error()})
			return
		}
		_ = json.NewEncoder(conn).Encode(RecoveryResponse{Success: true, Message: "Admin password updated and all sessions revoked."})

	case "totp-disable":
		if err := auth.DisableAdminTOTP(ctx, s.cfg.Store, "server_command"); err != nil {
			_ = json.NewEncoder(conn).Encode(RecoveryResponse{Success: false, Message: err.Error()})
			return
		}
		_ = json.NewEncoder(conn).Encode(RecoveryResponse{Success: true, Message: "TOTP two-factor authentication disabled."})

	case "status":
		ver, _ := s.cfg.Store.CurrentVersion(ctx)
		msg := fmt.Sprintf("dermaga daemon is active (uptime: %s, schema: v%d)", time.Since(s.startedAt).Round(time.Second), ver)
		_ = json.NewEncoder(conn).Encode(RecoveryResponse{Success: true, Message: msg})

	case "setup-token":
		token, err := auth.IssueSetupToken(ctx, s.cfg.Store, "server_command", time.Now())
		if err != nil {
			_ = json.NewEncoder(conn).Encode(RecoveryResponse{Success: false, Message: err.Error()})
			return
		}
		_ = json.NewEncoder(conn).Encode(RecoveryResponse{Success: true, Token: token})

	default:
		_ = json.NewEncoder(conn).Encode(RecoveryResponse{Success: false, Message: fmt.Sprintf("unknown action %q", req.Action)})
	}
}

// Close gracefully shuts down the server.
func (s *Server) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	var errs []error
	if s.httpServer != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if err := s.httpServer.Shutdown(ctx); err != nil {
			errs = append(errs, err)
		}
	}
	if s.unixListener != nil {
		if err := s.unixListener.Close(); err != nil {
			errs = append(errs, err)
		}
		_ = os.Remove(s.cfg.SocketPath)
	}

	if len(errs) > 0 {
		return fmt.Errorf("errors closing server: %v", errs)
	}
	return nil
}

// Handler returns the HTTP handler for testing.
func (s *Server) Handler() http.Handler {
	return s.httpServer.Handler
}
