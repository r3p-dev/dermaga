package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"dermaga/internal/auth"
	"dermaga/internal/crypto"
	"dermaga/internal/store"
)

const (
	minPasswordLength = 8
	maxPasswordLength = 1024
)

func (s *Server) hasValidSetupSession(r *http.Request) bool {
	cookie, err := r.Cookie(auth.SetupSessionCookieName)
	if err != nil || cookie.Value == "" {
		return false
	}
	return auth.ValidateSetupSession(r.Context(), s.cfg.Store, cookie.Value, time.Now())
}

func (s *Server) requireSetupSession(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.hasValidSetupSession(r) {
			writeJSON(w, http.StatusUnauthorized, SetupStepResponse{
				Success: false,
				Error:   "Setup session required. Verify your setup token first.",
			})
			return
		}
		next.ServeHTTP(w, r)
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func setSetupSessionCookie(w http.ResponseWriter, token string) {
	http.SetCookie(w, &http.Cookie{
		Name:     auth.SetupSessionCookieName,
		Value:    token,
		Path:     "/",
		MaxAge:   int(auth.SetupSessionTTL.Seconds()),
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
	})
}

func clearSetupSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     auth.SetupSessionCookieName,
		Value:    "",
		Path:     "/",
		Expires:  time.Unix(0, 0),
		MaxAge:   -1,
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
	})
}

func (s *Server) issueAdminSessionCookie(w http.ResponseWriter, r *http.Request) error {
	token, err := auth.GenerateSessionToken()
	if err != nil {
		return err
	}
	tokenHash := auth.HashSessionToken(token)
	expiresAt := time.Now().Add(auth.IdleTimeout)
	if err := s.cfg.Store.CreateSession(r.Context(), tokenHash, "admin", expiresAt); err != nil {
		return err
	}
	http.SetCookie(w, &http.Cookie{
		Name:     auth.SessionCookieName,
		Value:    token,
		Path:     "/",
		Expires:  time.Now().Add(auth.AbsoluteTimeout),
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
	})
	return nil
}

// handleSetupVerifyToken exchanges the single-use setup token for a short-lived setup session cookie (SETUP-1, SEC-8).
func (s *Server) handleSetupVerifyToken(w http.ResponseWriter, r *http.Request) {
	clientIP := clientIPFromRequest(r)
	if delay, allowed := s.rateLimiter.Check(clientIP); !allowed {
		w.Header().Set("Retry-After", strconv.Itoa(int(delay.Seconds())+1))
		writeJSON(w, http.StatusTooManyRequests, SetupStepResponse{
			Success: false,
			Error:   fmt.Sprintf("Too many failed attempts. Try again in %d seconds.", int(delay.Seconds())+1),
		})
		return
	}

	var req SetupVerifyTokenRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, SetupStepResponse{Success: false, Error: "Invalid JSON payload"})
		return
	}

	token := strings.TrimSpace(req.Token)
	if token == "" {
		writeJSON(w, http.StatusBadRequest, SetupStepResponse{Success: false, Error: "Setup token is required"})
		return
	}

	setupSess, err := auth.ExchangeSetupToken(r.Context(), s.cfg.Store, token, time.Now())
	switch {
	case err == nil:
		s.rateLimiter.RecordSuccess(clientIP)
		setSetupSessionCookie(w, setupSess)
		writeJSON(w, http.StatusOK, SetupStepResponse{Success: true, NextStep: "password"})
	case errors.Is(err, auth.ErrSetupAlreadyComplete):
		writeJSON(w, http.StatusForbidden, SetupStepResponse{Success: false, Error: "Setup is already complete. Please sign in."})
	case errors.Is(err, auth.ErrSetupTokenExpired):
		s.rateLimiter.RecordFailure(clientIP)
		writeJSON(w, http.StatusUnauthorized, SetupStepResponse{Success: false, Error: "Setup token has expired. Run `dermaga panel` on the server for a fresh link."})
	default:
		s.rateLimiter.RecordFailure(clientIP)
		writeJSON(w, http.StatusUnauthorized, SetupStepResponse{Success: false, Error: "Invalid setup token. Check the link or run `dermaga panel` on the server."})
	}
}

// handleSetupPassword sets the initial admin password (SETUP-2).
func (s *Server) handleSetupPassword(w http.ResponseWriter, r *http.Request) {
	setupMode, err := auth.IsSetupMode(r.Context(), s.cfg.Store)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, SetupStepResponse{Success: false, Error: "Database error"})
		return
	}
	if !setupMode {
		writeJSON(w, http.StatusForbidden, SetupStepResponse{Success: false, Error: "Admin password is already set."})
		return
	}

	var req SetupPasswordRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, SetupStepResponse{Success: false, Error: "Invalid JSON payload"})
		return
	}

	if len(req.Password) < minPasswordLength {
		writeJSON(w, http.StatusBadRequest, SetupStepResponse{Success: false, Error: fmt.Sprintf("Password must be at least %d characters long.", minPasswordLength)})
		return
	}
	if len(req.Password) > maxPasswordLength {
		writeJSON(w, http.StatusBadRequest, SetupStepResponse{Success: false, Error: "Password is too long."})
		return
	}

	if err := auth.SetAdminPassword(r.Context(), s.cfg.Store, "panel", req.Password); err != nil {
		writeJSON(w, http.StatusInternalServerError, SetupStepResponse{Success: false, Error: "Failed to save admin password"})
		return
	}

	writeJSON(w, http.StatusOK, SetupStepResponse{Success: true, NextStep: "totp"})
}

// handleSetupTOTPInit generates a TOTP secret and QR code for enrolment (SETUP-2).
// The secret is returned only during enrolment and stored encrypted at rest.
func (s *Server) handleSetupTOTPInit(w http.ResponseWriter, r *http.Request) {
	setupMode, err := auth.IsSetupMode(r.Context(), s.cfg.Store)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, SetupStepResponse{Success: false, Error: "Database error"})
		return
	}
	if setupMode {
		writeJSON(w, http.StatusBadRequest, SetupStepResponse{Success: false, Error: "Set the admin password before configuring TOTP."})
		return
	}
	if auth.IsTOTPEnabled(r.Context(), s.cfg.Store) {
		writeJSON(w, http.StatusForbidden, SetupStepResponse{Success: false, Error: "TOTP is already enabled."})
		return
	}

	secret, err := auth.GenerateTOTPSecret()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, SetupStepResponse{Success: false, Error: "Failed to generate TOTP secret"})
		return
	}

	sealed, err := crypto.Encrypt(s.cfg.MasterKey, auth.SettingTOTPPendingSecret, []byte(secret))
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, SetupStepResponse{Success: false, Error: "Failed to encrypt TOTP secret"})
		return
	}
	if err := s.cfg.Store.SetSetting(r.Context(), auth.SettingTOTPPendingSecret, sealed); err != nil {
		writeJSON(w, http.StatusInternalServerError, SetupStepResponse{Success: false, Error: "Failed to store TOTP secret"})
		return
	}

	uri := auth.TOTPURI(secret, "dermaga", "admin")
	svg, err := auth.GenerateQRCodeSVG(uri)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, SetupStepResponse{Success: false, Error: "Failed to render QR code"})
		return
	}

	writeJSON(w, http.StatusOK, SetupTOTPInitResponse{
		Secret:     secret,
		OTPAuthURI: uri,
		QRSVG:      svg,
	})
}

// handleSetupTOTPVerify verifies the 6-digit code, enables TOTP, and logs the admin in (SETUP-2).
func (s *Server) handleSetupTOTPVerify(w http.ResponseWriter, r *http.Request) {
	clientIP := clientIPFromRequest(r)
	if delay, allowed := s.rateLimiter.Check(clientIP); !allowed {
		w.Header().Set("Retry-After", strconv.Itoa(int(delay.Seconds())+1))
		writeJSON(w, http.StatusTooManyRequests, SetupStepResponse{
			Success: false,
			Error:   fmt.Sprintf("Too many failed attempts. Try again in %d seconds.", int(delay.Seconds())+1),
		})
		return
	}

	var req SetupTOTPVerifyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, SetupStepResponse{Success: false, Error: "Invalid JSON payload"})
		return
	}

	sealed, err := s.cfg.Store.GetSetting(r.Context(), auth.SettingTOTPPendingSecret)
	if errors.Is(err, store.ErrNotFound) || sealed == "" {
		writeJSON(w, http.StatusBadRequest, SetupStepResponse{Success: false, Error: "TOTP enrolment has not been started."})
		return
	} else if err != nil {
		writeJSON(w, http.StatusInternalServerError, SetupStepResponse{Success: false, Error: "Database error"})
		return
	}

	secretBytes, err := crypto.Decrypt(s.cfg.MasterKey, auth.SettingTOTPPendingSecret, sealed)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, SetupStepResponse{Success: false, Error: "Failed to decrypt TOTP secret"})
		return
	}

	step, err := auth.VerifyTOTPCode(string(secretBytes), req.Code, time.Now(), auth.LastTOTPStep(r.Context(), s.cfg.Store))
	if err != nil {
		s.rateLimiter.RecordFailure(clientIP)
		msg := "Invalid verification code. Check your authenticator clock and try again."
		if errors.Is(err, auth.ErrTOTPCodeReused) {
			msg = "That code was already used. Wait for the next 30-second code."
		}
		writeJSON(w, http.StatusUnauthorized, SetupStepResponse{Success: false, Error: msg})
		return
	}

	s.rateLimiter.RecordSuccess(clientIP)

	finalSealed, err := crypto.Encrypt(s.cfg.MasterKey, auth.SettingTOTPSecret, secretBytes)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, SetupStepResponse{Success: false, Error: "Failed to store TOTP secret"})
		return
	}
	if err := s.cfg.Store.SetSetting(r.Context(), auth.SettingTOTPSecret, finalSealed); err != nil {
		writeJSON(w, http.StatusInternalServerError, SetupStepResponse{Success: false, Error: "Failed to save TOTP secret"})
		return
	}
	if err := s.cfg.Store.SetSetting(r.Context(), auth.SettingTOTPEnabled, "true"); err != nil {
		writeJSON(w, http.StatusInternalServerError, SetupStepResponse{Success: false, Error: "Failed to enable TOTP"})
		return
	}
	_ = auth.SaveLastTOTPStep(r.Context(), s.cfg.Store, step)
	_ = s.cfg.Store.RecordAudit(r.Context(), "panel", "admin", "setup_totp_enabled", "TOTP two-factor authentication enabled during setup", nil, nil)

	auth.ClearSetupSession(r.Context(), s.cfg.Store)
	clearSetupSessionCookie(w)
	if err := s.issueAdminSessionCookie(w, r); err != nil {
		writeJSON(w, http.StatusInternalServerError, SetupStepResponse{Success: false, Error: "TOTP enabled, but failed to start session"})
		return
	}

	writeJSON(w, http.StatusOK, SetupStepResponse{Success: true, NextStep: "done"})
}

// handleSetupTOTPSkip skips TOTP enrolment during setup with an audit record and starts the admin session (SETUP-2).
func (s *Server) handleSetupTOTPSkip(w http.ResponseWriter, r *http.Request) {
	setupMode, err := auth.IsSetupMode(r.Context(), s.cfg.Store)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, SetupStepResponse{Success: false, Error: "Database error"})
		return
	}
	if setupMode {
		writeJSON(w, http.StatusBadRequest, SetupStepResponse{Success: false, Error: "Set the admin password before continuing."})
		return
	}

	if err := s.cfg.Store.SetSetting(r.Context(), auth.SettingTOTPEnabled, "false"); err != nil {
		writeJSON(w, http.StatusInternalServerError, SetupStepResponse{Success: false, Error: "Database error"})
		return
	}
	_ = s.cfg.Store.RecordAudit(r.Context(), "panel", "admin", "setup_totp_skipped", "TOTP two-factor authentication skipped during setup", nil, nil)

	auth.ClearSetupSession(r.Context(), s.cfg.Store)
	clearSetupSessionCookie(w)
	if err := s.issueAdminSessionCookie(w, r); err != nil {
		writeJSON(w, http.StatusInternalServerError, SetupStepResponse{Success: false, Error: "Failed to start session"})
		return
	}

	writeJSON(w, http.StatusOK, SetupStepResponse{Success: true, NextStep: "done"})
}
