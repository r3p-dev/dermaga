package store_test

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"dermaga/internal/store"
)

func TestStoreMigrations_FreshAndRerun(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	// 1. Fresh database initialization
	s, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}

	ver, err := s.CurrentVersion(ctx)
	if err != nil {
		t.Fatalf("CurrentVersion failed: %v", err)
	}
	if ver != 2 {
		t.Errorf("expected version 2, got %d", ver)
	}

	info, err := os.Stat(dbPath)
	if err != nil {
		t.Fatalf("stat failed: %v", err)
	}
	if info.Mode().Perm() != 0600 {
		t.Errorf("expected db file mode 0600, got %o", info.Mode().Perm())
	}
	s.Close()

	// 2. Re-run on existing database (idempotent)
	s2, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("re-opening existing store failed: %v", err)
	}
	defer s2.Close()

	ver2, err := s2.CurrentVersion(ctx)
	if err != nil {
		t.Fatalf("CurrentVersion on rerun failed: %v", err)
	}
	if ver2 != 2 {
		t.Errorf("expected version 2 after rerun, got %d", ver2)
	}
}

func TestStoreMigration0001To0002(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "v1.db")

	initSQL, err := os.ReadFile(filepath.Join("migrations", "0001_init.sql"))
	if err != nil {
		t.Fatalf("reading 0001_init.sql: %v", err)
	}

	rawDB, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("sql.Open failed: %v", err)
	}
	if _, err := rawDB.Exec(string(initSQL)); err != nil {
		t.Fatalf("exec 0001_init.sql: %v", err)
	}
	if _, err := rawDB.Exec(`INSERT INTO schema_migrations (version) VALUES (1)`); err != nil {
		t.Fatalf("insert schema_migrations: %v", err)
	}
	if _, err := rawDB.Exec(`INSERT INTO sessions (id, user_id, expires_at, created_at) VALUES ('old-hash', 'admin', '2026-12-01T00:00:00Z', '2026-11-25T10:00:00Z')`); err != nil {
		t.Fatalf("insert v1 session: %v", err)
	}
	_ = rawDB.Close()

	s, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("store.Open upgrade failed: %v", err)
	}
	defer s.Close()

	ctx := context.Background()
	ver, err := s.CurrentVersion(ctx)
	if err != nil || ver != 2 {
		t.Fatalf("expected upgraded version 2, got %d (err=%v)", ver, err)
	}

	sess, err := s.GetSession(ctx, "old-hash")
	if err != nil {
		t.Fatalf("GetSession on upgraded row failed: %v", err)
	}
	if sess.LastSeenAt.IsZero() || !sess.LastSeenAt.Equal(sess.CreatedAt) {
		t.Errorf("expected last_seen_at backfilled to created_at (%v), got %v", sess.CreatedAt, sess.LastSeenAt)
	}
}

func TestStoreSessionsMetadataAndRevokeOthers_ADM5(t *testing.T) {
	ctx := context.Background()
	s, err := store.Open(filepath.Join(t.TempDir(), "sessions.db"))
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer s.Close()

	longUA := strings.Repeat("Mozilla/5.0 ", 40)
	exp := time.Now().Add(time.Hour)
	if err := s.CreateSessionWithMeta(ctx, "h1", "admin", longUA, exp); err != nil {
		t.Fatalf("CreateSessionWithMeta 1 failed: %v", err)
	}
	if err := s.CreateSessionWithMeta(ctx, "h2", "admin", "Firefox/132.0", exp); err != nil {
		t.Fatalf("CreateSessionWithMeta 2 failed: %v", err)
	}

	list, err := s.ListSessions(ctx)
	if err != nil || len(list) != 2 {
		t.Fatalf("expected 2 sessions, got %d (err=%v)", len(list), err)
	}
	for _, sess := range list {
		if sess.ID == "h1" && len(sess.UserAgent) > 256 {
			t.Errorf("expected UserAgent truncated <= 256, got %d", len(sess.UserAgent))
		}
	}

	if err := s.DeleteOtherSessions(ctx, "h1"); err != nil {
		t.Fatalf("DeleteOtherSessions failed: %v", err)
	}
	list, _ = s.ListSessions(ctx)
	if len(list) != 1 || list[0].ID != "h1" {
		t.Fatalf("expected only h1 remaining, got %+v", list)
	}
}

func TestStoreMigrations_NewerDBRefusal(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "newer.db")

	// Pre-create database with a higher version
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("sql.Open failed: %v", err)
	}
	_, err = db.Exec(`
		CREATE TABLE schema_migrations (version INTEGER PRIMARY KEY, applied_at TEXT);
		INSERT INTO schema_migrations (version, applied_at) VALUES (999, '2026-11-16T00:00:00Z');
	`)
	if err != nil {
		t.Fatalf("inserting fake migration failed: %v", err)
	}
	db.Close()

	// Opening with binary that only knows version 1 must fail with ErrNewerDatabase
	_, err = store.Open(dbPath)
	if !errors.Is(err, store.ErrNewerDatabase) {
		t.Fatalf("expected ErrNewerDatabase, got: %v", err)
	}
}

func TestSettings(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	s, err := store.Open(filepath.Join(tmpDir, "settings.db"))
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer s.Close()

	// Not found check
	_, err = s.GetSetting(ctx, "hook_domain")
	if !errors.Is(err, store.ErrNotFound) {
		t.Errorf("expected ErrNotFound for missing key, got %v", err)
	}

	// Set and get
	if err := s.SetSetting(ctx, "hook_domain", "hooks.example.com"); err != nil {
		t.Fatalf("SetSetting failed: %v", err)
	}
	val, err := s.GetSetting(ctx, "hook_domain")
	if err != nil {
		t.Fatalf("GetSetting failed: %v", err)
	}
	if val != "hooks.example.com" {
		t.Errorf("GetSetting = %q, want %q", val, "hooks.example.com")
	}

	// Overwrite
	if err := s.SetSetting(ctx, "hook_domain", "hooks.updated.com"); err != nil {
		t.Fatalf("SetSetting update failed: %v", err)
	}
	val, _ = s.GetSetting(ctx, "hook_domain")
	if val != "hooks.updated.com" {
		t.Errorf("GetSetting = %q, want %q", val, "hooks.updated.com")
	}

	// Delete
	if err := s.DeleteSetting(ctx, "hook_domain"); err != nil {
		t.Fatalf("DeleteSetting failed: %v", err)
	}
	_, err = s.GetSetting(ctx, "hook_domain")
	if !errors.Is(err, store.ErrNotFound) {
		t.Errorf("expected ErrNotFound after delete, got %v", err)
	}
}

func TestSessions(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	s, err := store.Open(filepath.Join(tmpDir, "sessions.db"))
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer s.Close()

	tokenHash := "token-hash-12345"
	userID := "admin"
	expiresAt := time.Now().Add(12 * time.Hour).Truncate(time.Second)

	if err := s.CreateSession(ctx, tokenHash, userID, expiresAt); err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}

	sess, err := s.GetSession(ctx, tokenHash)
	if err != nil {
		t.Fatalf("GetSession failed: %v", err)
	}
	if sess.UserID != userID {
		t.Errorf("expected user ID %q, got %q", userID, sess.UserID)
	}
	if !sess.ExpiresAt.Equal(expiresAt) {
		t.Errorf("expected expiry %v, got %v", expiresAt, sess.ExpiresAt)
	}

	// Delete one
	if err := s.DeleteSession(ctx, tokenHash); err != nil {
		t.Fatalf("DeleteSession failed: %v", err)
	}
	_, err = s.GetSession(ctx, tokenHash)
	if !errors.Is(err, store.ErrNotFound) {
		t.Errorf("expected ErrNotFound after deletion, got %v", err)
	}

	// Delete all
	_ = s.CreateSession(ctx, "hash-1", "admin", expiresAt)
	_ = s.CreateSession(ctx, "hash-2", "admin", expiresAt)
	if err := s.DeleteAllSessions(ctx); err != nil {
		t.Fatalf("DeleteAllSessions failed: %v", err)
	}
	_, err = s.GetSession(ctx, "hash-1")
	if !errors.Is(err, store.ErrNotFound) {
		t.Errorf("expected hash-1 to be deleted")
	}
}

func TestAuditLogs(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	s, err := store.Open(filepath.Join(tmpDir, "audit.db"))
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer s.Close()

	// Invalid origin
	err = s.RecordAudit(ctx, "invalid_origin", "admin", "login", "desc", nil, nil)
	if !errors.Is(err, store.ErrInvalidOrigin) {
		t.Errorf("expected ErrInvalidOrigin, got: %v", err)
	}

	// Valid logs
	appID := "app_1"
	_, err = s.DB().Exec(`
		INSERT INTO apps (id, name, repo_owner, repo_name, branch, port, health_type)
		VALUES (?, 'test-app', 'r3p-dev', 'test-app', 'main', 8080, 'none')
	`, appID)
	if err != nil {
		t.Fatalf("inserting test app failed: %v", err)
	}

	err = s.RecordAudit(ctx, "panel", "admin", "login", "Admin logged in", nil, map[string]any{"ip": "127.0.0.1"})
	if err != nil {
		t.Fatalf("RecordAudit panel failed: %v", err)
	}

	err = s.RecordAudit(ctx, "webhook", "sha256-delivery-1", "push", "GitHub push received", &appID, map[string]any{"ref": "refs/heads/main"})
	if err != nil {
		t.Fatalf("RecordAudit webhook failed: %v", err)
	}

	// Query without filter
	logs, err := s.ListAuditLogs(ctx, store.AuditFilter{})
	if err != nil {
		t.Fatalf("ListAuditLogs failed: %v", err)
	}
	if len(logs) != 2 {
		t.Fatalf("expected 2 logs, got %d", len(logs))
	}

	// Filter by origin
	panelLogs, err := s.ListAuditLogs(ctx, store.AuditFilter{Origin: "panel"})
	if err != nil {
		t.Fatalf("ListAuditLogs with origin filter failed: %v", err)
	}
	if len(panelLogs) != 1 || panelLogs[0].Event != "login" {
		t.Errorf("expected 1 login panel log, got %d", len(panelLogs))
	}

	// Filter by AppID
	appLogs, err := s.ListAuditLogs(ctx, store.AuditFilter{AppID: &appID})
	if err != nil {
		t.Fatalf("ListAuditLogs with app filter failed: %v", err)
	}
	if len(appLogs) != 1 || appLogs[0].Event != "push" {
		t.Errorf("expected 1 push app log, got %d", len(appLogs))
	}
}
