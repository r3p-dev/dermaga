package auth_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"dermaga/internal/auth"
	"dermaga/internal/store"
)

func TestSetupStateMachine_SETUP1(t *testing.T) {
	ctx := context.Background()
	s, err := store.Open(filepath.Join(t.TempDir(), "setup-state.db"))
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer s.Close()

	state, err := auth.GetSetupState(ctx, s)
	if err != nil || state != auth.SetupStateUninitialized {
		t.Fatalf("expected uninitialized, got %q (err=%v)", state, err)
	}

	if err := auth.SetAdminPassword(ctx, s, "panel", "correct-horse-battery"); err != nil {
		t.Fatalf("SetAdminPassword failed: %v", err)
	}
	state, err = auth.GetSetupState(ctx, s)
	if err != nil || state != auth.SetupStateAdminCreated {
		t.Fatalf("expected admin_created, got %q (err=%v)", state, err)
	}

	if err := s.SetSetting(ctx, auth.SettingHookDomain, "hooks.example.com"); err != nil {
		t.Fatalf("SetSetting hook_domain failed: %v", err)
	}
	state, err = auth.GetSetupState(ctx, s)
	if err != nil || state != auth.SetupStateDomainSet {
		t.Fatalf("expected domain_set, got %q (err=%v)", state, err)
	}

	if err := s.SetSetting(ctx, auth.SettingGitHubAppID, "12345"); err != nil {
		t.Fatalf("SetSetting github_app_id failed: %v", err)
	}
	state, err = auth.GetSetupState(ctx, s)
	if err != nil || state != auth.SetupStateReady {
		t.Fatalf("expected ready, got %q (err=%v)", state, err)
	}
}

func TestSetupTokenLifecycle_SEC8(t *testing.T) {
	ctx := context.Background()
	s, err := store.Open(filepath.Join(t.TempDir(), "setup-token.db"))
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer s.Close()

	now := time.Date(2026, 11, 25, 12, 0, 0, 0, time.UTC)

	token, err := auth.IssueSetupToken(ctx, s, "server_command", now)
	if err != nil {
		t.Fatalf("IssueSetupToken failed: %v", err)
	}
	if len(token) < 40 {
		t.Fatalf("expected base64url 32-byte token, got len=%d", len(token))
	}

	// Stored value must be hashed, never plaintext
	storedHash, err := s.GetSetting(ctx, auth.SettingSetupTokenHash)
	if err != nil {
		t.Fatalf("GetSetting setup_token_hash failed: %v", err)
	}
	if storedHash == token || storedHash != auth.HashSessionToken(token) {
		t.Fatalf("setup token must be stored as SHA-256 hash")
	}

	// Wrong token rejected
	if _, err := auth.ExchangeSetupToken(ctx, s, "wrong-token", now.Add(time.Minute)); err != auth.ErrSetupTokenInvalid {
		t.Fatalf("expected ErrSetupTokenInvalid, got %v", err)
	}

	// Valid token exchanged for setup session cookie token (single-use)
	setupSess, err := auth.ExchangeSetupToken(ctx, s, token, now.Add(time.Minute))
	if err != nil || setupSess == "" {
		t.Fatalf("expected ExchangeSetupToken to succeed, got sess=%q err=%v", setupSess, err)
	}
	if !auth.ValidateSetupSession(ctx, s, setupSess, now.Add(2*time.Minute)) {
		t.Fatalf("expected ValidateSetupSession to return true")
	}

	// Reuse of same setup token must fail
	if _, err := auth.ExchangeSetupToken(ctx, s, token, now.Add(2*time.Minute)); err != auth.ErrSetupTokenInvalid {
		t.Fatalf("expected ErrSetupTokenInvalid on reuse, got %v", err)
	}

	// Expiry after 24h must fail
	token2, err := auth.IssueSetupToken(ctx, s, "server_command", now)
	if err != nil {
		t.Fatalf("IssueSetupToken 2 failed: %v", err)
	}
	if _, err := auth.ExchangeSetupToken(ctx, s, token2, now.Add(24*time.Hour+time.Second)); err != auth.ErrSetupTokenExpired {
		t.Fatalf("expected ErrSetupTokenExpired after 24h, got %v", err)
	}

	// Once admin password exists, issuing or using a setup token is refused
	token3, err := auth.IssueSetupToken(ctx, s, "server_command", now)
	if err != nil {
		t.Fatalf("IssueSetupToken 3 failed: %v", err)
	}
	if err := auth.SetAdminPassword(ctx, s, "panel", "correct-horse-battery"); err != nil {
		t.Fatalf("SetAdminPassword failed: %v", err)
	}
	if _, err := auth.IssueSetupToken(ctx, s, "server_command", now); err != auth.ErrSetupAlreadyComplete {
		t.Fatalf("expected ErrSetupAlreadyComplete on issue, got %v", err)
	}
	if _, err := auth.ExchangeSetupToken(ctx, s, token3, now.Add(time.Minute)); err != auth.ErrSetupAlreadyComplete {
		t.Fatalf("expected ErrSetupAlreadyComplete on exchange, got %v", err)
	}
}
