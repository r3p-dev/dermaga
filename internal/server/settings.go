package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"dermaga/internal/access"
	"dermaga/internal/auth"
	"dermaga/internal/crypto"
	"dermaga/internal/store"
	"dermaga/internal/traefik"
)

func sessionTokenHashFromRequest(r *http.Request) string {
	cookie, err := r.Cookie(auth.SessionCookieName)
	if err != nil || cookie.Value == "" {
		return ""
	}
	return auth.HashSessionToken(cookie.Value)
}

func (s *Server) verifyAdminReauth(r *http.Request, password, totpCode string, requireTOTP bool) (int, string) {
	clientIP := clientIPFromRequest(r)
	if delay, allowed := s.rateLimiter.Check(clientIP); !allowed {
		return http.StatusTooManyRequests, fmt.Sprintf("Too many failed attempts. Try again in %d seconds.", int(delay.Seconds())+1)
	}

	storedHash, err := s.cfg.Store.GetSetting(r.Context(), auth.SettingAdminPassword)
	if err != nil {
		return http.StatusInternalServerError, "Failed to read admin credentials"
	}

	valid, err := auth.VerifyPassword(password, storedHash)
	if err != nil || !valid {
		s.rateLimiter.RecordFailure(clientIP)
		_ = s.cfg.Store.RecordAudit(r.Context(), "panel", "admin", "reauth_failed", "Failed re-authentication attempt", nil, nil)
		return http.StatusUnauthorized, "Current password is incorrect"
	}

	if requireTOTP && auth.IsTOTPEnabled(r.Context(), s.cfg.Store) {
		if strings.TrimSpace(totpCode) == "" {
			return http.StatusUnauthorized, "Current 6-digit authenticator code is required"
		}
		sealed, err := s.cfg.Store.GetSetting(r.Context(), auth.SettingTOTPSecret)
		if err != nil {
			return http.StatusInternalServerError, "Failed to load TOTP configuration"
		}
		secretBytes, err := crypto.Decrypt(s.cfg.MasterKey, auth.SettingTOTPSecret, sealed)
		if err != nil {
			return http.StatusInternalServerError, "Failed to decrypt TOTP configuration"
		}
		step, err := auth.VerifyTOTPCode(string(secretBytes), totpCode, time.Now(), auth.LastTOTPStep(r.Context(), s.cfg.Store))
		if err != nil {
			s.rateLimiter.RecordFailure(clientIP)
			_ = s.cfg.Store.RecordAudit(r.Context(), "panel", "admin", "reauth_failed", "Failed TOTP re-authentication attempt", nil, nil)
			if errors.Is(err, auth.ErrTOTPCodeReused) {
				return http.StatusUnauthorized, "That authenticator code was already used. Wait for the next code."
			}
			return http.StatusUnauthorized, "Invalid authenticator code"
		}
		_ = auth.SaveLastTOTPStep(r.Context(), s.cfg.Store, step)
	}

	s.rateLimiter.RecordSuccess(clientIP)
	return http.StatusOK, ""
}

// handleListSessions returns all active admin sessions (ADM-5).
func (s *Server) handleListSessions(w http.ResponseWriter, r *http.Request) {
	currentHash := sessionTokenHashFromRequest(r)
	all, err := s.cfg.Store.ListSessions(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to list sessions"})
		return
	}

	now := time.Now()
	entries := make([]ActiveSessionEntry, 0, len(all))
	for _, sess := range all {
		if now.After(sess.ExpiresAt) || now.After(sess.CreatedAt.Add(auth.AbsoluteTimeout)) {
			continue
		}
		entries = append(entries, ActiveSessionEntry{
			ID:         sess.ID,
			UserAgent:  sess.UserAgent,
			CreatedAt:  sess.CreatedAt,
			LastSeenAt: sess.LastSeenAt,
			ExpiresAt:  sess.ExpiresAt,
			Current:    sess.ID == currentHash,
		})
	}

	writeJSON(w, http.StatusOK, ActiveSessionsResponse{Sessions: entries})
}

// handleRevokeSession revokes a single non-current session (ADM-5).
func (s *Server) handleRevokeSession(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeJSON(w, http.StatusBadRequest, SetupStepResponse{Success: false, Error: "Session ID is required"})
		return
	}
	if id == sessionTokenHashFromRequest(r) {
		writeJSON(w, http.StatusBadRequest, SetupStepResponse{Success: false, Error: "Cannot revoke your current session from the list; use Log out instead."})
		return
	}
	if err := s.cfg.Store.DeleteSession(r.Context(), id); err != nil {
		writeJSON(w, http.StatusInternalServerError, SetupStepResponse{Success: false, Error: "Failed to revoke session"})
		return
	}
	_ = s.cfg.Store.RecordAudit(r.Context(), "panel", "admin", "session_revoked", "Revoked an active session", nil, nil)
	writeJSON(w, http.StatusOK, SetupStepResponse{Success: true})
}

// handleRevokeOtherSessions ends every session except the current one (ADM-5).
func (s *Server) handleRevokeOtherSessions(w http.ResponseWriter, r *http.Request) {
	currentHash := sessionTokenHashFromRequest(r)
	if err := s.cfg.Store.DeleteOtherSessions(r.Context(), currentHash); err != nil {
		writeJSON(w, http.StatusInternalServerError, SetupStepResponse{Success: false, Error: "Failed to revoke other sessions"})
		return
	}
	_ = s.cfg.Store.RecordAudit(r.Context(), "panel", "admin", "sessions_revoked_others", "Logged out every other active session", nil, nil)
	writeJSON(w, http.StatusOK, SetupStepResponse{Success: true})
}

// handleGetAccessSettings returns the LAP-1/LAP-3 SSH tunnel command and instructions.
func (s *Server) handleGetAccessSettings(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	addr, _ := s.cfg.Store.GetSetting(ctx, access.SettingAddress)
	portStr, _ := s.cfg.Store.GetSetting(ctx, access.SettingPort)
	fp, _ := s.cfg.Store.GetSetting(ctx, access.SettingKeyFingerprint)
	port, _ := strconv.Atoi(portStr)
	if port == 0 {
		port = 4222
	}

	hostname := "server"
	if h, err := os.Hostname(); err == nil && h != "" {
		hostname = strings.ToLower(strings.SplitN(h, ".", 2)[0])
	}

	var laptopCmd string
	if addr != "" {
		t := access.Target{
			Address:        addr,
			Port:           port,
			ServerHostname: hostname,
			KeyFingerprint: fp,
		}
		if err := t.Validate(); err == nil {
			laptopCmd, _ = access.LaptopCommand(t)
		}
	}

	directSSH := ""
	if addr != "" {
		directSSH = fmt.Sprintf("ssh -N -L 7070:127.0.0.1:7070 -p %d server@%s", port, addr)
	}

	writeJSON(w, http.StatusOK, AccessSettingsResponse{
		Address:       addr,
		Port:          port,
		LaptopCommand: laptopCmd,
		DirectSSH:     directSSH,
		PanelURL:      access.PanelURL,
		WindowsHint:   fmt.Sprintf("PowerShell / Windows Terminal: %s (then open %s)", directSSH, access.PanelURL),
		MobileHint:    fmt.Sprintf("In Termius / Blink / ConnectBot: host %s, port %d, user server, SSH key auth, local port forward 7070 -> 127.0.0.1:7070.", addr, port),
	})
}

// handleChangePassword updates the admin password after re-authentication and ends other sessions (ADM-4, SEC-10).
func (s *Server) handleChangePassword(w http.ResponseWriter, r *http.Request) {
	var req ChangePasswordRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, SetupStepResponse{Success: false, Error: "Invalid JSON payload"})
		return
	}

	if code, msg := s.verifyAdminReauth(r, req.CurrentPassword, req.TOTPCode, true); code != http.StatusOK {
		writeJSON(w, code, SetupStepResponse{Success: false, Error: msg})
		return
	}

	if len(req.NewPassword) < minPasswordLength {
		writeJSON(w, http.StatusBadRequest, SetupStepResponse{Success: false, Error: fmt.Sprintf("Password must be at least %d characters long.", minPasswordLength)})
		return
	}
	if len(req.NewPassword) > maxPasswordLength {
		writeJSON(w, http.StatusBadRequest, SetupStepResponse{Success: false, Error: "Password is too long."})
		return
	}

	hash, err := auth.HashPassword(req.NewPassword)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, SetupStepResponse{Success: false, Error: "Failed to hash new password"})
		return
	}
	if err := s.cfg.Store.SetSetting(r.Context(), auth.SettingAdminPassword, hash); err != nil {
		writeJSON(w, http.StatusInternalServerError, SetupStepResponse{Success: false, Error: "Failed to save new password"})
		return
	}

	// ADM-4: Changing the password ends every other session while keeping the current session active.
	currentHash := sessionTokenHashFromRequest(r)
	if err := s.cfg.Store.DeleteOtherSessions(r.Context(), currentHash); err != nil {
		writeJSON(w, http.StatusInternalServerError, SetupStepResponse{Success: false, Error: "Password updated, but failed to revoke other sessions"})
		return
	}

	_ = s.cfg.Store.RecordAudit(r.Context(), "panel", "admin", "password_changed", "Admin password changed; all other sessions revoked", nil, nil)
	writeJSON(w, http.StatusOK, SetupStepResponse{Success: true})
}

// handleSettingsTOTPInit starts TOTP enrolment from Settings → Security after password re-auth (ADM-4, SEC-10).
func (s *Server) handleSettingsTOTPInit(w http.ResponseWriter, r *http.Request) {
	if auth.IsTOTPEnabled(r.Context(), s.cfg.Store) {
		writeJSON(w, http.StatusConflict, SetupStepResponse{Success: false, Error: "TOTP is already enabled."})
		return
	}

	var req SettingsTOTPInitRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, SetupStepResponse{Success: false, Error: "Invalid JSON payload"})
		return
	}

	if code, msg := s.verifyAdminReauth(r, req.CurrentPassword, "", false); code != http.StatusOK {
		writeJSON(w, code, SetupStepResponse{Success: false, Error: msg})
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

// handleSettingsTOTPEnable verifies the 6-digit code and enables TOTP (ADM-4, SEC-10).
func (s *Server) handleSettingsTOTPEnable(w http.ResponseWriter, r *http.Request) {
	clientIP := clientIPFromRequest(r)
	if delay, allowed := s.rateLimiter.Check(clientIP); !allowed {
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
		msg := "Invalid verification code."
		if errors.Is(err, auth.ErrTOTPCodeReused) {
			msg = "That code was already used. Wait for the next code."
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
	_ = s.cfg.Store.DeleteSetting(r.Context(), auth.SettingTOTPPendingSecret)
	_ = auth.SaveLastTOTPStep(r.Context(), s.cfg.Store, step)
	_ = s.cfg.Store.RecordAudit(r.Context(), "panel", "admin", "totp_enabled", "TOTP two-factor authentication enabled in Settings", nil, nil)

	writeJSON(w, http.StatusOK, SetupStepResponse{Success: true})
}

// handleSettingsTOTPDisable disables TOTP after verifying both password and current TOTP code (ADM-4, SEC-10).
func (s *Server) handleSettingsTOTPDisable(w http.ResponseWriter, r *http.Request) {
	if !auth.IsTOTPEnabled(r.Context(), s.cfg.Store) {
		writeJSON(w, http.StatusBadRequest, SetupStepResponse{Success: false, Error: "TOTP is already disabled."})
		return
	}

	var req SettingsTOTPDisableRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, SetupStepResponse{Success: false, Error: "Invalid JSON payload"})
		return
	}

	if code, msg := s.verifyAdminReauth(r, req.CurrentPassword, req.TOTPCode, true); code != http.StatusOK {
		writeJSON(w, code, SetupStepResponse{Success: false, Error: msg})
		return
	}

	if err := auth.DisableAdminTOTP(r.Context(), s.cfg.Store, "panel"); err != nil {
		writeJSON(w, http.StatusInternalServerError, SetupStepResponse{Success: false, Error: "Failed to disable TOTP"})
		return
	}
	_ = s.cfg.Store.DeleteSetting(r.Context(), auth.SettingTOTPSecret)

	writeJSON(w, http.StatusOK, SetupStepResponse{Success: true})
}

func toHookDomainDNSInfo(res access.DNSCheckResult) *HookDomainDNSInfo {
	return &HookDomainDNSInfo{
		Domain:       res.Domain,
		ServerIP:     res.ServerIP,
		ARecords:     res.ARecords,
		AAAARecords:  res.AAAARecords,
		AMatches:     res.AMatches,
		AAAAMismatch: res.AAAAMismatch,
		Warning:      res.Warning,
	}
}

func (s *Server) checkHookDomainDNS(ctx context.Context, domain string) (*HookDomainDNSInfo, string) {
	serverIP, _ := s.cfg.Store.GetSetting(ctx, access.SettingAddress)
	res := access.CheckDomainDNS(ctx, domain, serverIP, s.cfg.DNSLookup)
	return toHookDomainDNSInfo(res), serverIP
}

// handleGetHookDomain returns the saved hook domain, server IP, setup state, and DNS check (SETUP-2).
func (s *Server) handleGetHookDomain(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	hookDomain, _ := s.cfg.Store.GetSetting(ctx, auth.SettingHookDomain)
	serverIP, _ := s.cfg.Store.GetSetting(ctx, access.SettingAddress)
	state, _ := auth.GetSetupState(ctx, s.cfg.Store)

	resp := HookDomainResponse{
		HookDomain: hookDomain,
		ServerIP:   serverIP,
		SetupState: string(state),
	}
	if hookDomain != "" {
		resp.DNSCheck, _ = s.checkHookDomainDNS(ctx, hookDomain)
		resp.Certificate = s.hookDomainCertificate(hookDomain)
	}
	writeJSON(w, http.StatusOK, resp)
}

// handleCheckHookDomainDNS checks A/AAAA records for a candidate hook domain (SETUP-2).
func (s *Server) handleCheckHookDomainDNS(w http.ResponseWriter, r *http.Request) {
	var req HookDomainCheckRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, SetupStepResponse{Success: false, Error: "Invalid JSON payload"})
		return
	}
	domain := strings.ToLower(strings.TrimSpace(req.Domain))
	if err := traefik.ValidateHookDomain(domain); err != nil {
		writeJSON(w, http.StatusBadRequest, SetupStepResponse{Success: false, Error: err.Error()})
		return
	}
	info, _ := s.checkHookDomainDNS(r.Context(), domain)
	writeJSON(w, http.StatusOK, info)
}

// handleSaveHookDomain saves the reserved hook domain and renders its Traefik router (SETUP-2, APP-2, TLS-5).
func (s *Server) handleSaveHookDomain(w http.ResponseWriter, r *http.Request) {
	var req HookDomainSaveRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, SetupStepResponse{Success: false, Error: "Invalid JSON payload"})
		return
	}
	domain := strings.ToLower(strings.TrimSpace(req.Domain))
	if err := traefik.ValidateHookDomain(domain); err != nil {
		writeJSON(w, http.StatusBadRequest, SetupStepResponse{Success: false, Error: err.Error()})
		return
	}

	if err := s.cfg.Store.SetSetting(r.Context(), auth.SettingHookDomain, domain); err != nil {
		writeJSON(w, http.StatusInternalServerError, SetupStepResponse{Success: false, Error: "Failed to save hook domain"})
		return
	}

	if s.cfg.Reconciler != nil {
		if err := s.cfg.Reconciler.EnsureHookDomain(r.Context(), domain); err != nil {
			writeJSON(w, http.StatusInternalServerError, SetupStepResponse{Success: false, Error: fmt.Sprintf("Hook domain saved, but Traefik config failed: %v", err)})
			return
		}
	}

	_ = s.cfg.Store.RecordAudit(r.Context(), "panel", "admin", "hook_domain_set",
		fmt.Sprintf("Configured reserved hook domain %s", domain), nil, nil)

	info, serverIP := s.checkHookDomainDNS(r.Context(), domain)
	state, _ := auth.GetSetupState(r.Context(), s.cfg.Store)
	writeJSON(w, http.StatusOK, HookDomainResponse{
		HookDomain:  domain,
		ServerIP:    serverIP,
		SetupState:  string(state),
		DNSCheck:    info,
		Certificate: s.hookDomainCertificate(domain),
	})
}
