package auth_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"dermaga/internal/auth"
	"dermaga/internal/store"
)

func TestPasswordHashingAndVerification(t *testing.T) {
	password := "correct-horse-battery-staple"

	hash, err := auth.HashPassword(password)
	if err != nil {
		t.Fatalf("HashPassword failed: %v", err)
	}

	valid, err := auth.VerifyPassword(password, hash)
	if err != nil {
		t.Fatalf("VerifyPassword failed: %v", err)
	}
	if !valid {
		t.Errorf("expected valid password verification to return true")
	}

	// Incorrect password
	valid, err = auth.VerifyPassword("wrong-password", hash)
	if err != nil {
		t.Fatalf("VerifyPassword with wrong password failed: %v", err)
	}
	if valid {
		t.Errorf("expected wrong password verification to return false")
	}
}

func TestRateLimiter(t *testing.T) {
	rl := auth.NewRateLimiter()
	ip := "127.0.0.1"

	// First two failures should not block
	backoff := rl.RecordFailure(ip)
	if backoff != 0 {
		t.Errorf("expected 0 backoff on 1st failure, got %v", backoff)
	}
	backoff = rl.RecordFailure(ip)
	if backoff != 0 {
		t.Errorf("expected 0 backoff on 2nd failure, got %v", backoff)
	}

	// 3rd failure: 1s backoff
	backoff = rl.RecordFailure(ip)
	if backoff != 1*time.Second {
		t.Errorf("expected 1s backoff on 3rd failure, got %v", backoff)
	}

	dur, allowed := rl.Check(ip)
	if allowed || dur <= 0 {
		t.Errorf("expected Check to return blocked after 3rd failure")
	}

	// Success clears failure tracking
	rl.RecordSuccess(ip)
	_, allowed = rl.Check(ip)
	if !allowed {
		t.Errorf("expected Check to allow after RecordSuccess")
	}
}

func TestRecoveryFunctions(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	s, err := store.Open(filepath.Join(tmpDir, "recovery.db"))
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer s.Close()

	// Create a dummy session
	token, _ := auth.GenerateSessionToken()
	_ = s.CreateSession(ctx, auth.HashSessionToken(token), "admin", time.Now().Add(1*time.Hour))

	// Set password
	newPass := "new-admin-password"
	if err := auth.SetAdminPassword(ctx, s, "server_command", newPass); err != nil {
		t.Fatalf("SetAdminPassword failed: %v", err)
	}

	// Session must be deleted
	_, err = s.GetSession(ctx, auth.HashSessionToken(token))
	if err == nil {
		t.Errorf("expected session to be revoked after password change")
	}

	// Password in settings must verify
	storedHash, err := s.GetSetting(ctx, auth.SettingAdminPassword)
	if err != nil {
		t.Fatalf("GetSetting failed: %v", err)
	}
	valid, err := auth.VerifyPassword(newPass, storedHash)
	if err != nil || !valid {
		t.Errorf("stored password failed verification")
	}

	// Disable TOTP
	if err := auth.DisableAdminTOTP(ctx, s, "server_command"); err != nil {
		t.Fatalf("DisableAdminTOTP failed: %v", err)
	}
	totpVal, err := s.GetSetting(ctx, auth.SettingTOTPEnabled)
	if err != nil || totpVal != "false" {
		t.Errorf("expected totp to be 'false', got %q", totpVal)
	}
}
