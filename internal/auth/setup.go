package auth

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"time"

	"dermaga/internal/store"
)

const (
	// Setup token and session lifetimes (SETUP-1, SEC-8).
	SetupTokenTTL   = 24 * time.Hour
	SetupSessionTTL = 30 * time.Minute

	SetupSessionCookieName = "dermaga_setup_session"

	// Settings keys for first-run setup (SETUP-1, SETUP-2, SEC-8).
	SettingSetupTokenHash        = "setup_token_hash"
	SettingSetupTokenExpiresAt   = "setup_token_expires_at"
	SettingSetupSessionHash      = "setup_session_hash"
	SettingSetupSessionExpiresAt = "setup_session_expires_at"
	SettingTOTPPendingSecret     = "admin_totp_pending_secret"
	SettingTOTPLastStep          = "admin_totp_last_step"
	SettingHookDomain            = "hook_domain"
	SettingGitHubAppID           = "github_app_id"
)

// SetupState represents the server-wide setup state machine (ARCHITECTURE §6.3).
type SetupState string

const (
	SetupStateUninitialized SetupState = "uninitialized"
	SetupStateAdminCreated  SetupState = "admin_created"
	SetupStateDomainSet     SetupState = "domain_set"
	SetupStateReady         SetupState = "ready"
)

var (
	ErrSetupAlreadyComplete = errors.New("setup is already complete")
	ErrSetupTokenExpired    = errors.New("setup token has expired")
	ErrSetupTokenInvalid    = errors.New("invalid setup token")
)

// GetSetupState returns the current setup state derived from persisted settings.
func GetSetupState(ctx context.Context, s *store.Store) (SetupState, error) {
	_, err := s.GetSetting(ctx, SettingAdminPassword)
	if errors.Is(err, store.ErrNotFound) {
		return SetupStateUninitialized, nil
	}
	if err != nil {
		return "", fmt.Errorf("reading admin password setting: %w", err)
	}

	hookDomain, err := s.GetSetting(ctx, SettingHookDomain)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return "", fmt.Errorf("reading hook domain setting: %w", err)
	}
	if hookDomain == "" {
		return SetupStateAdminCreated, nil
	}

	appID, err := s.GetSetting(ctx, SettingGitHubAppID)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return "", fmt.Errorf("reading GitHub App setting: %w", err)
	}
	if appID == "" {
		return SetupStateDomainSet, nil
	}

	return SetupStateReady, nil
}

// IsSetupMode reports whether no admin password exists yet (SETUP-1).
func IsSetupMode(ctx context.Context, s *store.Store) (bool, error) {
	state, err := GetSetupState(ctx, s)
	if err != nil {
		return false, err
	}
	return state == SetupStateUninitialized, nil
}

// IssueSetupToken generates a new 32-byte single-use setup token, stores its SHA-256 hash
// with a 24 h expiry, and returns the raw token for display in the setup link (SEC-8, SETUP-3).
func IssueSetupToken(ctx context.Context, s *store.Store, origin string, now time.Time) (string, error) {
	setupMode, err := IsSetupMode(ctx, s)
	if err != nil {
		return "", err
	}
	if !setupMode {
		return "", ErrSetupAlreadyComplete
	}

	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("generating setup token: %w", err)
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	hash := HashSessionToken(token)
	expiresAt := now.UTC().Add(SetupTokenTTL).Format(time.RFC3339)

	if err := s.SetSetting(ctx, SettingSetupTokenHash, hash); err != nil {
		return "", fmt.Errorf("storing setup token hash: %w", err)
	}
	if err := s.SetSetting(ctx, SettingSetupTokenExpiresAt, expiresAt); err != nil {
		return "", fmt.Errorf("storing setup token expiry: %w", err)
	}

	_ = s.RecordAudit(ctx, origin, "server", "setup_token_issued", "First-run setup token issued", nil, nil)
	return token, nil
}

// ExchangeSetupToken verifies a single-use setup token in constant time, consumes it,
// and creates a short-lived setup session token (SEC-8).
func ExchangeSetupToken(ctx context.Context, s *store.Store, token string, now time.Time) (string, error) {
	setupMode, err := IsSetupMode(ctx, s)
	if err != nil {
		return "", err
	}
	if !setupMode {
		return "", ErrSetupAlreadyComplete
	}

	storedHash, err := s.GetSetting(ctx, SettingSetupTokenHash)
	if errors.Is(err, store.ErrNotFound) || storedHash == "" {
		return "", ErrSetupTokenInvalid
	}
	if err != nil {
		return "", fmt.Errorf("reading setup token hash: %w", err)
	}

	expiresStr, err := s.GetSetting(ctx, SettingSetupTokenExpiresAt)
	if err != nil {
		return "", ErrSetupTokenInvalid
	}
	expiresAt, err := time.Parse(time.RFC3339, expiresStr)
	if err != nil {
		return "", ErrSetupTokenInvalid
	}

	if now.UTC().After(expiresAt) {
		_ = s.DeleteSetting(ctx, SettingSetupTokenHash)
		_ = s.DeleteSetting(ctx, SettingSetupTokenExpiresAt)
		_ = s.RecordAudit(ctx, "panel", "setup", "setup_token_expired", "Expired setup token rejected", nil, nil)
		return "", ErrSetupTokenExpired
	}

	candidateHash := HashSessionToken(token)
	if subtle.ConstantTimeCompare([]byte(candidateHash), []byte(storedHash)) != 1 {
		return "", ErrSetupTokenInvalid
	}

	// Single-use: consume the setup token immediately.
	_ = s.DeleteSetting(ctx, SettingSetupTokenHash)
	_ = s.DeleteSetting(ctx, SettingSetupTokenExpiresAt)

	setupSession, err := GenerateSessionToken()
	if err != nil {
		return "", fmt.Errorf("generating setup session: %w", err)
	}
	if err := s.SetSetting(ctx, SettingSetupSessionHash, HashSessionToken(setupSession)); err != nil {
		return "", fmt.Errorf("storing setup session: %w", err)
	}
	if err := s.SetSetting(ctx, SettingSetupSessionExpiresAt, now.UTC().Add(SetupSessionTTL).Format(time.RFC3339)); err != nil {
		return "", fmt.Errorf("storing setup session expiry: %w", err)
	}

	_ = s.RecordAudit(ctx, "panel", "setup", "setup_token_used", "Setup token verified and exchanged for setup session", nil, nil)
	return setupSession, nil
}

// ValidateSetupSession verifies the short-lived setup session cookie in constant time.
func ValidateSetupSession(ctx context.Context, s *store.Store, token string, now time.Time) bool {
	if token == "" {
		return false
	}
	storedHash, err := s.GetSetting(ctx, SettingSetupSessionHash)
	if err != nil || storedHash == "" {
		return false
	}
	expiresStr, err := s.GetSetting(ctx, SettingSetupSessionExpiresAt)
	if err != nil {
		return false
	}
	expiresAt, err := time.Parse(time.RFC3339, expiresStr)
	if err != nil || now.UTC().After(expiresAt) {
		return false
	}
	candidateHash := HashSessionToken(token)
	return subtle.ConstantTimeCompare([]byte(candidateHash), []byte(storedHash)) == 1
}

// ClearSetupSession removes the temporary setup session and any unused setup token.
func ClearSetupSession(ctx context.Context, s *store.Store) {
	_ = s.DeleteSetting(ctx, SettingSetupSessionHash)
	_ = s.DeleteSetting(ctx, SettingSetupSessionExpiresAt)
	_ = s.DeleteSetting(ctx, SettingSetupTokenHash)
	_ = s.DeleteSetting(ctx, SettingSetupTokenExpiresAt)
	_ = s.DeleteSetting(ctx, SettingTOTPPendingSecret)
}

// IsTOTPEnabled reports whether TOTP two-factor authentication is enabled for admin.
func IsTOTPEnabled(ctx context.Context, s *store.Store) bool {
	val, err := s.GetSetting(ctx, SettingTOTPEnabled)
	return err == nil && val == "true"
}

// LastTOTPStep returns the last accepted TOTP time step, or -1 if none.
func LastTOTPStep(ctx context.Context, s *store.Store) int64 {
	val, err := s.GetSetting(ctx, SettingTOTPLastStep)
	if err != nil || val == "" {
		return -1
	}
	step, err := strconv.ParseInt(val, 10, 64)
	if err != nil {
		return -1
	}
	return step
}

// SaveLastTOTPStep persists the last accepted TOTP time step for replay prevention.
func SaveLastTOTPStep(ctx context.Context, s *store.Store, step int64) error {
	return s.SetSetting(ctx, SettingTOTPLastStep, strconv.FormatInt(step, 10))
}
