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

func TestDeriveSSHConfig_WithSSHConnection_PublicIP(t *testing.T) {
	env := "203.0.113.10 54321 198.51.100.50 2200"
	info := deriveSSHConfig(env, nil)

	if info.HostName != "198.51.100.50" {
		t.Errorf("expected HostName 198.51.100.50, got %q", info.HostName)
	}
	if info.Port != "2200" {
		t.Errorf("expected Port 2200, got %q", info.Port)
	}
	if !info.Reliable {
		t.Errorf("expected Reliable=true for public routable IP")
	}
	if info.Notice != "" {
		t.Errorf("expected empty notice for public IP, got %q", info.Notice)
	}
}

func TestDeriveSSHConfig_WithSSHConnection_PrivateIP(t *testing.T) {
	env := "192.168.122.1 58338 192.168.122.217 4222"
	info := deriveSSHConfig(env, nil)

	if info.HostName != "192.168.122.217" {
		t.Errorf("expected HostName 192.168.122.217, got %q", info.HostName)
	}
	if info.Port != "4222" {
		t.Errorf("expected Port 4222, got %q", info.Port)
	}
	if info.Reliable {
		t.Errorf("expected Reliable=false for private IP")
	}
	if !strings.Contains(info.Notice, "private or local network address") {
		t.Errorf("expected notice explaining private address, got %q", info.Notice)
	}
}

func TestDeriveSSHConfig_WithoutSSHConnection_CustomPort(t *testing.T) {
	sshdConfig := `# OpenSSH Daemon Configuration
Port 4222
PermitRootLogin no
`
	info := deriveSSHConfig("", strings.NewReader(sshdConfig))

	if info.HostName != "<your-server-ip-or-hostname>" {
		t.Errorf("expected placeholder HostName, got %q", info.HostName)
	}
	if info.Port != "4222" {
		t.Errorf("expected Port 4222 parsed from sshd_config, got %q", info.Port)
	}
	if info.Reliable {
		t.Errorf("expected Reliable=false without active SSH connection")
	}
	if !strings.Contains(info.Notice, "No active SSH session detected") {
		t.Errorf("expected notice for no active session, got %q", info.Notice)
	}
}

func TestDeriveSSHConfig_WithoutSSHConnection_DefaultPort(t *testing.T) {
	info := deriveSSHConfig("", nil)

	if info.HostName != "<your-server-ip-or-hostname>" {
		t.Errorf("expected placeholder HostName, got %q", info.HostName)
	}
	if info.Port != "22" {
		t.Errorf("expected default Port 22, got %q", info.Port)
	}
	if info.Reliable {
		t.Errorf("expected Reliable=false")
	}
}
