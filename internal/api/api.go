// Package api serves dermaga's JSON API on two listeners: the panel
// (127.0.0.1, session auth, Host/Origin checks) and the CLI Unix socket
// (authenticated by its 0600 file mode).
package api

import (
	"bufio"
	"bytes"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/r3p-dev/dermaga/internal/audit"
	"github.com/r3p-dev/dermaga/internal/auth"
	"github.com/r3p-dev/dermaga/internal/name"
)

const (
	maxBody       = 64 << 10
	sessionCookie = name.Name + "_session"
)

type Server struct {
	DB      *sql.DB
	Auth    *auth.Service
	Port    int // panel port, for the Host and Origin checks
	Started time.Time
}

// NewHTTPServer applies the timeouts every listener must have (BRIEF §4.4).
func NewHTTPServer(h http.Handler) *http.Server {
	return &http.Server{
		Handler:           h,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		// SSE routes (from M3) lift this per request with http.ResponseController.SetWriteDeadline.
		WriteTimeout:   60 * time.Second,
		IdleTimeout:    120 * time.Second,
		MaxHeaderBytes: 64 << 10,
	}
}

// Panel is the handler for 127.0.0.1:<port>, reached only through the SSH tunnel.
func (s *Server) Panel() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "%s %s: the web UI arrives in M7; the JSON API is under /api/.\n", name.Name, name.Version())
	})
	mux.HandleFunc("POST /api/login", s.login)
	mux.HandleFunc("POST /api/logout", s.logout)
	mux.Handle("GET /api/status", s.session(s.status))
	mux.Handle("GET /api/audit", s.session(s.auditList))
	return s.guard(mux)
}

// Socket is the handler for the CLI's Unix socket. Admin routes exist only here.
func (s *Server) Socket() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/status", s.status)
	mux.HandleFunc("GET /api/audit", s.auditList)
	mux.HandleFunc("PUT /api/admin/password", s.setPassword)
	mux.HandleFunc("PUT /api/admin/totp", s.enableTOTP)
	mux.HandleFunc("DELETE /api/admin/totp", s.disableTOTP)
	return mux
}

// guard rejects DNS-rebinding (Host) and cross-site writes (Origin) before routing (BRIEF §4.2).
func (s *Server) guard(next http.Handler) http.Handler {
	hosts := map[string]bool{fmt.Sprintf("127.0.0.1:%d", s.Port): true, fmt.Sprintf("localhost:%d", s.Port): true}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !hosts[r.Host] {
			writeError(w, http.StatusMisdirectedRequest, "unexpected Host header")
			return
		}
		switch r.Method {
		case http.MethodGet, http.MethodHead, http.MethodOptions:
		default:
			o, ok := strings.CutPrefix(r.Header.Get("Origin"), "http://")
			if !ok || !hosts[o] {
				writeError(w, http.StatusForbidden, "missing or foreign Origin")
				return
			}
		}
		h := w.Header()
		h.Set("Cache-Control", "no-store")
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "no-referrer")
		next.ServeHTTP(w, r)
	})
}

func (s *Server) session(h http.HandlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := r.Cookie(sessionCookie)
		if err != nil || !s.Auth.Session(c.Value) {
			writeError(w, http.StatusUnauthorized, "not logged in")
			return
		}
		h(w, r)
	})
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Password string `json:"password"`
		TOTP     string `json:"totp"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	token, err := s.Auth.Login(req.Password, req.TOTP)
	var blocked *auth.BlockedError
	switch {
	case errors.As(err, &blocked):
		w.Header().Set("Retry-After", strconv.Itoa(int(blocked.RetryAfter.Seconds()+0.999)))
		writeError(w, http.StatusTooManyRequests, blocked.Error())
		return
	case errors.Is(err, auth.ErrInvalid):
		s.audit("panel", "auth.login_failed", "")
		writeError(w, http.StatusUnauthorized, err.Error())
		return
	case err != nil:
		internalError(w, err)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookie, Value: token, Path: "/", HttpOnly: true, SameSite: http.SameSiteStrictMode,
		MaxAge: int(auth.AbsoluteTimeout.Seconds()),
	})
	s.audit("panel", "auth.login", "")
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(sessionCookie); err == nil && s.Auth.Session(c.Value) {
		if err := s.Auth.Logout(c.Value); err != nil {
			internalError(w, err)
			return
		}
		s.audit("panel", "auth.logout", "")
	}
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: "", Path: "/", HttpOnly: true, SameSite: http.SameSiteStrictMode, MaxAge: -1})
	w.WriteHeader(http.StatusNoContent)
}

// Status is the response of GET /api/status.
type Status struct {
	Name             string    `json:"name"`
	Version          string    `json:"version"`
	GoVersion        string    `json:"goVersion"`
	PID              int       `json:"pid"`
	StartedAt        time.Time `json:"startedAt"`
	RSSKB            int64     `json:"rssKB"`
	AdminPasswordSet bool      `json:"adminPasswordSet"`
	TOTPEnabled      bool      `json:"totpEnabled"`
}

func (s *Server) status(w http.ResponseWriter, r *http.Request) {
	pw, totp, err := s.Auth.State()
	if err != nil {
		internalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, Status{
		Name: name.Name, Version: name.Version(), GoVersion: runtime.Version(), PID: os.Getpid(),
		StartedAt: s.Started, RSSKB: rssKB(), AdminPasswordSet: pw, TOTPEnabled: totp,
	})
}

func (s *Server) auditList(w http.ResponseWriter, r *http.Request) {
	limit := 50
	if v := r.URL.Query().Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > 1000 {
			writeError(w, http.StatusBadRequest, "limit must be 1..1000")
			return
		}
		limit = n
	}
	entries, err := audit.List(s.DB, limit)
	if err != nil {
		internalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, entries)
}

func (s *Server) setPassword(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Password string `json:"password"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	if err := s.Auth.SetPassword(req.Password); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	s.audit("cli", "admin.set_password", "")
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) enableTOTP(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Secret string `json:"secret"`
		Code   string `json:"code"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	if err := s.Auth.EnableTOTP(req.Secret, req.Code); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	s.audit("cli", "admin.totp_enable", "")
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) disableTOTP(w http.ResponseWriter, r *http.Request) {
	if err := s.Auth.DisableTOTP(); err != nil {
		internalError(w, err)
		return
	}
	s.audit("cli", "admin.totp_disable", "")
	w.WriteHeader(http.StatusNoContent)
}

// audit failures are logged, not returned: the action itself already happened.
func (s *Server) audit(actor, action, target string) {
	if err := audit.Log(s.DB, actor, action, target, ""); err != nil {
		slog.Error("audit log write failed", "action", action, "err", err)
	}
}

func readJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	d := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBody))
	d.DisallowUnknownFields()
	if err := d.Decode(v); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return false
	}
	return true
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}

func internalError(w http.ResponseWriter, err error) {
	slog.Error("request failed", "err", err)
	writeError(w, http.StatusInternalServerError, "internal error")
}

// rssKB reads VmRSS from /proc/self/status (BRIEF §9 budget: < 30 MB idle).
func rssKB() int64 {
	b, err := os.ReadFile("/proc/self/status")
	if err != nil {
		return 0
	}
	sc := bufio.NewScanner(bytes.NewReader(b))
	for sc.Scan() {
		if v, ok := strings.CutPrefix(sc.Text(), "VmRSS:"); ok {
			n, _ := strconv.ParseInt(strings.TrimSuffix(strings.TrimSpace(v), " kB"), 10, 64)
			return n
		}
	}
	return 0
}
