// Package backup implements local and remote restic repository management,
// streaming database dumps (D10), system and volume backups, systemd timer
// orchestration, and disaster recovery restore workflows (BRIEF §3.8).
package backup

import (
	"context"
	"database/sql"
	"io"
	"os"
	"time"

	"github.com/r3p-dev/dermaga/internal/config"
	"github.com/r3p-dev/dermaga/internal/databases"
	"github.com/r3p-dev/dermaga/internal/secrets"
)

// Snapshot represents a restic repository snapshot.
type Snapshot struct {
	ID        string    `json:"id"`
	ShortID   string    `json:"short_id"`
	Time      time.Time `json:"time"`
	Paths     []string  `json:"paths"`
	Tags      []string  `json:"tags"`
	Hostname  string    `json:"hostname,omitempty"`
	Username  string    `json:"username,omitempty"`
	SizeBytes int64     `json:"size_bytes,omitempty"`
}

// Settings holds restic repository and S3 backend configuration.
type Settings struct {
	Repository string `json:"repository"`
	// Password never leaves the daemon through this struct: see GeneratePassword
	// and RevealPassword. PasswordState is none, unconfirmed or confirmed.
	Password      string `json:"-"`
	PasswordState string `json:"passwordState"`
	S3Endpoint    string `json:"s3Endpoint,omitempty"`
	S3Bucket      string `json:"s3Bucket,omitempty"`
	S3Region      string `json:"s3Region,omitempty"`
	S3AccessKey   string `json:"s3AccessKey,omitempty"`
	S3SecretKey   string `json:"s3SecretKey,omitempty"`
	Configured    bool   `json:"configured"`
}

// TestRestoreResult is the outcome of restoring into an isolated throwaway sandbox.
type TestRestoreResult struct {
	Database   string    `json:"database"`
	SnapshotID string    `json:"snapshotId"`
	Duration   string    `json:"duration"`
	SanityOut  string    `json:"sanityOut"`
	Success    bool      `json:"success"`
	TestedAt   time.Time `json:"testedAt"`
}

// Host abstracts OS and subprocess operations for backups and restores.
type Host interface {
	Run(ctx context.Context, timeout time.Duration, argv ...string) (string, error)
	RunStdin(ctx context.Context, timeout time.Duration, stdin io.Reader, argv ...string) (string, error)
	RunEnv(ctx context.Context, timeout time.Duration, env []string, log io.Writer, argv ...string) (string, error)
	RunRestic(ctx context.Context, env []string, stdin io.Reader, stdout io.Writer, argv ...string) (string, error)
	WriteUserUnit(filename string, data []byte) error
	RemoveUserUnit(filename string) error
	ReloadSystemd(ctx context.Context) error
	EnableTimer(ctx context.Context, timerUnit string) error
	DisableTimer(ctx context.Context, timerUnit string) error
	ActiveState(ctx context.Context, unit string) (string, error)
	ListTimers(ctx context.Context) (string, error)
	ReadFile(path string) ([]byte, error)
	WriteFile(path string, data []byte, mode os.FileMode) error
	Remove(path string) error
}

// Manager coordinates all backup, restore, and timer orchestration.
type Manager struct {
	DB        *sql.DB
	Box       *secrets.Box
	Databases databases.Store
	Paths     config.Paths
	Host      Host
}

func NewManager(db *sql.DB, box *secrets.Box, dbStore databases.Store, paths config.Paths, h Host) *Manager {
	if h == nil {
		h = NewSystemHost(paths)
	}
	return &Manager{
		DB:        db,
		Box:       box,
		Databases: dbStore,
		Paths:     paths,
		Host:      h,
	}
}
