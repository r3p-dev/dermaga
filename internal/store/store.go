// Package store provides SQLite persistence for dermaga using modernc.org/sqlite,
// managing numbered schema migrations, settings, sessions, apps, and audit logs.
package store

import (
	"context"
	"database/sql"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

var (
	ErrNotFound      = errors.New("record not found")
	ErrNewerDatabase = errors.New("database schema version is newer than supported by this binary")
	ErrInvalidOrigin = errors.New("invalid audit log origin")
)

// Store encapsulates the SQLite database connection.
type Store struct {
	db   *sql.DB
	path string
}

// AuditLog represents an entry in the audit log table (AUD-1).
type AuditLog struct {
	ID          int64           `json:"id"`
	Origin      string          `json:"origin"`
	AppID       *string         `json:"app_id"`
	Event       string          `json:"event"`
	Actor       string          `json:"actor"`
	Description string          `json:"description"`
	Details     json.RawMessage `json:"details"`
	CreatedAt   string          `json:"created_at"`
}

// AuditFilter defines query parameters for filtering audit logs (AUD-2).
type AuditFilter struct {
	Origin string
	AppID  *string
	Since  *time.Time
	Until  *time.Time
	Limit  int
	Offset int
}

// Session represents an authenticated user session (SEC-7).
type Session struct {
	ID        string    `json:"id"`
	UserID    string    `json:"user_id"`
	ExpiresAt time.Time `json:"expires_at"`
	CreatedAt time.Time `json:"created_at"`
}

// Open initializes and migrates the SQLite database at the specified path.
// It ensures 0700 directory permissions and 0600 file permissions.
func Open(path string) (*Store, error) {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, fmt.Errorf("creating database directory: %w", err)
	}

	// Ensure the file exists with 0600 permissions
	if _, err := os.Stat(path); os.IsNotExist(err) {
		f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
		if err != nil {
			return nil, fmt.Errorf("creating database file: %w", err)
		}
		_ = f.Close()
	} else if err != nil {
		return nil, fmt.Errorf("checking database file: %w", err)
	} else {
		_ = os.Chmod(path, 0600)
	}

	dsn := fmt.Sprintf("file:%s?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(ON)", path)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("opening sqlite database: %w", err)
	}

	// Run pragmas directly to ensure WAL, busy timeout, and foreign keys
	pragmas := []string{
		"PRAGMA journal_mode = WAL;",
		"PRAGMA busy_timeout = 5000;",
		"PRAGMA foreign_keys = ON;",
	}
	for _, p := range pragmas {
		if _, err := db.Exec(p); err != nil {
			_ = db.Close()
			return nil, fmt.Errorf("executing pragma %q: %w", p, err)
		}
	}

	s := &Store{db: db, path: path}
	if err := s.migrate(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("running migrations: %w", err)
	}

	return s, nil
}

// Close closes the database connection.
func (s *Store) Close() error {
	return s.db.Close()
}

// DB returns the underlying *sql.DB for direct transactions if needed.
func (s *Store) DB() *sql.DB {
	return s.db
}

// migrate discovers embedded SQL migration files and executes pending ones in order.
func (s *Store) migrate() error {
	entries, err := migrationsFS.ReadDir("migrations")
	if err != nil {
		return fmt.Errorf("reading migrations directory: %w", err)
	}

	type migrationFile struct {
		version int
		name    string
	}

	var files []migrationFile
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}
		parts := strings.SplitN(entry.Name(), "_", 2)
		if len(parts) < 2 {
			continue
		}
		ver, err := strconv.Atoi(parts[0])
		if err != nil {
			continue
		}
		files = append(files, migrationFile{version: ver, name: entry.Name()})
	}

	sort.Slice(files, func(i, j int) bool {
		return files[i].version < files[j].version
	})

	latestSupportedVersion := 0
	if len(files) > 0 {
		latestSupportedVersion = files[len(files)-1].version
	}

	// Check if schema_migrations table exists
	var tableExists int
	err = s.db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='schema_migrations'`).Scan(&tableExists)
	if err != nil {
		return fmt.Errorf("checking schema_migrations table: %w", err)
	}

	appliedVersions := make(map[int]bool)
	maxAppliedVersion := 0

	if tableExists > 0 {
		rows, err := s.db.Query(`SELECT version FROM schema_migrations ORDER BY version ASC`)
		if err != nil {
			return fmt.Errorf("querying schema_migrations: %w", err)
		}
		defer rows.Close()

		for rows.Next() {
			var ver int
			if err := rows.Scan(&ver); err != nil {
				return fmt.Errorf("scanning migration version: %w", err)
			}
			appliedVersions[ver] = true
			if ver > maxAppliedVersion {
				maxAppliedVersion = ver
			}
		}
		if err := rows.Err(); err != nil {
			return err
		}
	}

	// Refuse if the database schema version is newer than the binary
	if maxAppliedVersion > latestSupportedVersion {
		return fmt.Errorf("%w: db version %d > binary version %d", ErrNewerDatabase, maxAppliedVersion, latestSupportedVersion)
	}

	for _, mf := range files {
		if appliedVersions[mf.version] {
			continue
		}

		content, err := migrationsFS.ReadFile(filepath.Join("migrations", mf.name))
		if err != nil {
			return fmt.Errorf("reading migration %s: %w", mf.name, err)
		}

		tx, err := s.db.Begin()
		if err != nil {
			return fmt.Errorf("starting transaction for %s: %w", mf.name, err)
		}

		if _, err := tx.Exec(string(content)); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("executing migration %s: %w", mf.name, err)
		}

		if _, err := tx.Exec(`INSERT INTO schema_migrations (version) VALUES (?)`, mf.version); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("recording migration version %d: %w", mf.version, err)
		}

		if err := tx.Commit(); err != nil {
			return fmt.Errorf("committing migration %s: %w", mf.name, err)
		}
	}

	return nil
}

// CurrentVersion returns the highest applied schema migration version.
func (s *Store) CurrentVersion(ctx context.Context) (int, error) {
	var maxVer sql.NullInt64
	err := s.db.QueryRowContext(ctx, `SELECT MAX(version) FROM schema_migrations`).Scan(&maxVer)
	if err != nil {
		return 0, err
	}
	if !maxVer.Valid {
		return 0, nil
	}
	return int(maxVer.Int64), nil
}

// --- Settings ---

// GetSetting returns the value of a setting key or ErrNotFound.
func (s *Store) GetSetting(ctx context.Context, key string) (string, error) {
	var val string
	err := s.db.QueryRowContext(ctx, `SELECT value FROM settings WHERE key = ?`, key).Scan(&val)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", err
	}
	return val, nil
}

// SetSetting inserts or updates a setting key with a value.
func (s *Store) SetSetting(ctx context.Context, key, value string) error {
	query := `
		INSERT INTO settings (key, value, updated_at)
		VALUES (?, ?, strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
		ON CONFLICT(key) DO UPDATE SET
			value = excluded.value,
			updated_at = excluded.updated_at
	`
	_, err := s.db.ExecContext(ctx, query, key, value)
	return err
}

// DeleteSetting removes a setting key.
func (s *Store) DeleteSetting(ctx context.Context, key string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM settings WHERE key = ?`, key)
	return err
}

// --- Sessions ---

// CreateSession stores a new hashed session token (SEC-7).
func (s *Store) CreateSession(ctx context.Context, tokenHash, userID string, expiresAt time.Time) error {
	query := `INSERT INTO sessions (id, user_id, expires_at) VALUES (?, ?, ?)`
	_, err := s.db.ExecContext(ctx, query, tokenHash, userID, expiresAt.UTC().Format(time.RFC3339))
	return err
}

// GetSession retrieves a session by its token hash.
func (s *Store) GetSession(ctx context.Context, tokenHash string) (*Session, error) {
	var sess Session
	var expStr, createdStr string
	query := `SELECT id, user_id, expires_at, created_at FROM sessions WHERE id = ?`
	err := s.db.QueryRowContext(ctx, query, tokenHash).Scan(&sess.ID, &sess.UserID, &expStr, &createdStr)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}

	sess.ExpiresAt, _ = time.Parse(time.RFC3339, expStr)
	sess.CreatedAt, _ = time.Parse(time.RFC3339, createdStr)
	return &sess, nil
}

// DeleteSession deletes a specific session.
func (s *Store) DeleteSession(ctx context.Context, tokenHash string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE id = ?`, tokenHash)
	return err
}

// DeleteAllSessions deletes all sessions (e.g. on password reset or admin recovery, ADM-4, ADM-6).
func (s *Store) DeleteAllSessions(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM sessions`)
	return err
}

// --- Audit Logs (AUD-1, AUD-2) ---

var validOrigins = map[string]bool{
	"panel":          true,
	"webhook":        true,
	"server_command": true,
	"daemon":         true,
}

// RecordAudit logs a mutating action per AUD-1.
func (s *Store) RecordAudit(ctx context.Context, origin, actor, event, description string, appID *string, details map[string]any) error {
	if !validOrigins[origin] {
		return fmt.Errorf("%w: %q", ErrInvalidOrigin, origin)
	}

	detailsJSON := "{}"
	if len(details) > 0 {
		b, err := json.Marshal(details)
		if err != nil {
			return fmt.Errorf("marshaling audit details: %w", err)
		}
		detailsJSON = string(b)
	}

	query := `
		INSERT INTO audit_logs (origin, app_id, event, actor, description, details)
		VALUES (?, ?, ?, ?, ?, ?)
	`
	_, err := s.db.ExecContext(ctx, query, origin, appID, event, actor, description, detailsJSON)
	return err
}

// ListAuditLogs queries audit logs matching the given filter (AUD-2).
func (s *Store) ListAuditLogs(ctx context.Context, filter AuditFilter) ([]AuditLog, error) {
	var conditions []string
	var args []any

	if filter.Origin != "" {
		conditions = append(conditions, "origin = ?")
		args = append(args, filter.Origin)
	}
	if filter.AppID != nil {
		conditions = append(conditions, "app_id = ?")
		args = append(args, *filter.AppID)
	}
	if filter.Since != nil {
		conditions = append(conditions, "created_at >= ?")
		args = append(args, filter.Since.UTC().Format(time.RFC3339))
	}
	if filter.Until != nil {
		conditions = append(conditions, "created_at <= ?")
		args = append(args, filter.Until.UTC().Format(time.RFC3339))
	}

	query := `SELECT id, origin, app_id, event, actor, description, details, created_at FROM audit_logs`
	if len(conditions) > 0 {
		query += " WHERE " + strings.Join(conditions, " AND ")
	}
	query += " ORDER BY id DESC"

	limit := filter.Limit
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	query += " LIMIT ?"
	args = append(args, limit)

	if filter.Offset > 0 {
		query += " OFFSET ?"
		args = append(args, filter.Offset)
	}

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("querying audit logs: %w", err)
	}
	defer rows.Close()

	var logs []AuditLog
	for rows.Next() {
		var l AuditLog
		var detailsStr string
		if err := rows.Scan(&l.ID, &l.Origin, &l.AppID, &l.Event, &l.Actor, &l.Description, &detailsStr, &l.CreatedAt); err != nil {
			return nil, fmt.Errorf("scanning audit log: %w", err)
		}
		l.Details = json.RawMessage(detailsStr)
		logs = append(logs, l)
	}

	return logs, rows.Err()
}
