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

	"dermaga/internal/auth"
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
	MasterKey  []byte
	Version    string
	Commit     string
}

// Server encapsulates the HTTP and Unix socket listeners.
type Server struct {
	cfg          Config
	httpServer   *http.Server
	unixListener net.Listener
	rateLimiter  *auth.RateLimiter
	startedAt    time.Time
	mu           sync.Mutex
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
		cfg.Version = "0.1.0-dev"
	}

	s := &Server{
		cfg:         cfg,
		rateLimiter: auth.NewRateLimiter(),
		startedAt:   time.Now(),
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

		// 2. Origin verification on mutating methods
		switch r.Method {
		case http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodPatch:
			origin := r.Header.Get("Origin")
			if origin == "" {
				// Also check Referer if Origin is omitted by some clients
				referer := r.Header.Get("Referer")
				if referer == "" {
					http.Error(w, "Forbidden: missing Origin header on mutating request (SEC-3)", http.StatusForbidden)
					return
				}
				origin = referer
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
	// Authentication routes
	mux.HandleFunc("POST /api/auth/login", s.handleLogin)
	mux.HandleFunc("POST /api/auth/logout", s.handleLogout)
	mux.HandleFunc("GET /api/auth/session", s.handleSession)

	// Audit logs
	mux.HandleFunc("GET /api/audit-logs", s.requireAuth(s.handleAuditLogs))

	// Server status
	mux.HandleFunc("GET /api/server/status", s.requireAuth(s.handleServerStatus))

	// Embedded UI
	mux.Handle("/", DistHandler())
}

// requireAuth middleware validates the session cookie.
func (s *Server) requireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
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
		if time.Now().After(sess.ExpiresAt) {
			_ = s.cfg.Store.DeleteSession(r.Context(), tokenHash)
			http.Error(w, `{"error":"session expired"}`, http.StatusUnauthorized)
			return
		}

		// Check absolute timeout (7 days from creation)
		if time.Now().After(sess.CreatedAt.Add(auth.AbsoluteTimeout)) {
			_ = s.cfg.Store.DeleteSession(r.Context(), tokenHash)
			http.Error(w, `{"error":"absolute session timeout reached"}`, http.StatusUnauthorized)
			return
		}

		next.ServeHTTP(w, r)
	}
}

// handleLogin handles admin password authentication (ADM-1, ADM-2).
func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	clientIP, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		clientIP = r.RemoteAddr
	}

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
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(LoginResponse{Success: false, Error: "Admin password not set. Setup required."})
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

	// Clear failure count on successful authentication
	s.rateLimiter.RecordSuccess(clientIP)

	// Create session
	token, err := auth.GenerateSessionToken()
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(LoginResponse{Success: false, Error: "Failed to generate session"})
		return
	}

	tokenHash := auth.HashSessionToken(token)
	expiresAt := time.Now().Add(auth.IdleTimeout)
	if err := s.cfg.Store.CreateSession(r.Context(), tokenHash, "admin", expiresAt); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(LoginResponse{Success: false, Error: "Failed to record session"})
		return
	}

	// Set session cookie (SEC-7)
	http.SetCookie(w, &http.Cookie{
		Name:     auth.SessionCookieName,
		Value:    token,
		Path:     "/",
		Expires:  time.Now().Add(auth.AbsoluteTimeout),
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
	})

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

// handleSession returns authentication status.
func (s *Server) handleSession(w http.ResponseWriter, r *http.Request) {
	resp := SessionInfo{}

	// Check if setup is needed
	_, err := s.cfg.Store.GetSetting(r.Context(), auth.SettingAdminPassword)
	if errors.Is(err, store.ErrNotFound) {
		resp.SetupRequired = true
	}

	cookie, err := r.Cookie(auth.SessionCookieName)
	if err == nil && cookie.Value != "" {
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
