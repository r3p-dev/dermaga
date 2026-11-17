package main

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"dermaga/internal/auth"
	"dermaga/internal/config"
	"dermaga/internal/store"
)

func TestCLIAdminSetPassword_DirectFallback(t *testing.T) {
	tmpDir := t.TempDir()

	t.Setenv("HOME", tmpDir)
	t.Setenv("XDG_RUNTIME_DIR", filepath.Join(tmpDir, "run"))

	paths := config.ResolvePaths()

	// Pre-create database
	st, err := store.Open(paths.DBPath)
	if err != nil {
		t.Fatalf("store.Open failed: %v", err)
	}
	_ = st.Close()

	// Run admin set-password reading from piped stdin
	stdin := strings.NewReader("cli-test-password-123\n")
	err = runAdmin([]string{"set-password"}, stdin)
	if err != nil {
		t.Fatalf("runAdmin set-password failed: %v", err)
	}

	// Verify in database
	st, err = store.Open(paths.DBPath)
	if err != nil {
		t.Fatalf("re-opening database failed: %v", err)
	}
	defer st.Close()

	hash, err := st.GetSetting(context.Background(), auth.SettingAdminPassword)
	if err != nil {
		t.Fatalf("GetSetting failed: %v", err)
	}

	valid, err := auth.VerifyPassword("cli-test-password-123", hash)
	if err != nil || !valid {
		t.Errorf("stored password verification failed")
	}

	// Run admin totp disable without daemon running (triggers direct fallback)
	err = runAdmin([]string{"totp", "disable"}, strings.NewReader(""))
	if err != nil {
		t.Fatalf("runAdmin totp disable failed: %v", err)
	}

	totp, err := st.GetSetting(context.Background(), auth.SettingTOTPEnabled)
	if err != nil || totp != "false" {
		t.Errorf("expected totp to be 'false', got %q", totp)
	}
}

func TestCLIAdminSetPassword_EmptyInputRejected(t *testing.T) {
	// Empty line should be rejected
	stdin := strings.NewReader("\n")
	err := runAdmin([]string{"set-password"}, stdin)
	if err == nil {
		t.Fatalf("expected error on empty password, got nil")
	}
	if !strings.Contains(err.Error(), "password cannot be empty") {
		t.Errorf("expected 'password cannot be empty' error, got %v", err)
	}

	// EOF / no input should be rejected
	stdinEmpty := strings.NewReader("")
	err = runAdmin([]string{"set-password"}, stdinEmpty)
	if err == nil {
		t.Fatalf("expected error on EOF password input, got nil")
	}
}

func TestCLIAdminSetPassword_ShortPasswordRejected(t *testing.T) {
	stdin := strings.NewReader("short\n")
	err := runAdmin([]string{"set-password"}, stdin)
	if err == nil {
		t.Fatalf("expected error on short password, got nil")
	}
	if !strings.Contains(err.Error(), "at least 8 characters") {
		t.Errorf("expected 'at least 8 characters' error, got %v", err)
	}
}

func TestCLIAdminSetPassword_UnexpectedArgs(t *testing.T) {
	// Extra arguments like --password must be rejected
	stdin := strings.NewReader("somepassword123\n")
	err := runAdmin([]string{"set-password", "--password", "somepassword123"}, stdin)
	if err == nil {
		t.Fatalf("expected error when extra arguments are passed, got nil")
	}
	if !strings.Contains(err.Error(), "set-password takes no arguments") {
		t.Errorf("expected 'set-password takes no arguments' error, got %v", err)
	}
}

func TestCLIPanel_InactiveDaemon(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)
	t.Setenv("XDG_RUNTIME_DIR", filepath.Join(tmpDir, "run"))

	// Calling runPanel when daemon is not running should cleanly inform the user without crashing
	err := runPanel(nil)
	if err != nil {
		t.Errorf("expected runPanel to handle inactive daemon cleanly, got error: %v", err)
	}
}

// INST-6 / INST-12: what the installer records is what `dermaga panel` prints.
func TestCLIInstallSettingsFeedsLaptopCommand(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)
	t.Setenv("XDG_RUNTIME_DIR", filepath.Join(tmpDir, "run"))

	fp := "SHA256:V+KZKBpZYT+xiu/DXAsTjf2LTerWPN+DYUlih/Z1U9A"
	if err := runInstallSettings([]string{"--ssh-address", "192.168.122.217", "--ssh-port", "4222", "--login-key-fingerprint", fp}); err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(config.ResolvePaths().DBPath)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	tg, err := accessTarget(context.Background(), st)
	if err != nil {
		t.Fatal(err)
	}
	if tg.Address != "192.168.122.217" || tg.Port != 4222 || tg.KeyFingerprint != fp {
		t.Errorf("stored target = %+v", tg)
	}

	for _, bad := range [][]string{
		{"--ssh-address", "1.2.3.4'; id", "--ssh-port", "4222"},
		{"--ssh-address", "1.2.3.4", "--ssh-port", "0"},
		{"--ssh-address", "1.2.3.4", "--ssh-port", "22", "--login-key-fingerprint", "MD5:x"},
	} {
		if err := runInstallSettings(bad); err == nil {
			t.Errorf("accepted %v", bad)
		}
	}
}
