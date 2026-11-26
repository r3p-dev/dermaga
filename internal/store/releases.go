package store

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"
)

// EnvVersion is one immutable, encrypted runtime environment of an app (DEC-32). Releases point at
// the version they were deployed with, so a rollback restores exactly that environment (DEP-4).
type EnvVersion struct {
	ID           string `json:"id"`
	AppID        string `json:"app_id"`
	Version      int    `json:"version"`
	EnvEncrypted string `json:"-"`
	CreatedAt    string `json:"created_at"`
}

func newID(prefix string) string {
	b := make([]byte, 12)
	_, _ = rand.Read(b)
	return prefix + hex.EncodeToString(b)
}

// addEnvVersion stores app.EnvEncrypted as the app's next env version and points the app at it.
func addEnvVersion(ctx context.Context, tx *sql.Tx, app *App) error {
	id, err := insertEnvVersion(ctx, tx, app.ID, app.EnvEncrypted)
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE apps SET env_version_id = ? WHERE id = ?`, id, app.ID); err != nil {
		return err
	}
	app.EnvVersionID = &id
	return nil
}

type execQuerier interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

func insertEnvVersion(ctx context.Context, q execQuerier, appID, sealed string) (string, error) {
	var next int
	if err := q.QueryRowContext(ctx, `SELECT COALESCE(MAX(version), 0) + 1 FROM env_versions WHERE app_id = ?`, appID).Scan(&next); err != nil {
		return "", err
	}
	id := newID("env_")
	if _, err := q.ExecContext(ctx, `INSERT INTO env_versions (id, app_id, version, env_encrypted) VALUES (?, ?, ?, ?)`,
		id, appID, next, sealed); err != nil {
		return "", fmt.Errorf("inserting env version: %w", err)
	}
	return id, nil
}

// GetEnvVersion returns one env version.
func (s *Store) GetEnvVersion(ctx context.Context, id string) (*EnvVersion, error) {
	var v EnvVersion
	err := s.db.QueryRowContext(ctx, `SELECT id, app_id, version, env_encrypted, created_at FROM env_versions WHERE id = ?`, id).
		Scan(&v.ID, &v.AppID, &v.Version, &v.EnvEncrypted, &v.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("querying env version: %w", err)
	}
	return &v, nil
}

// DeleteEnvVersionsExcept removes an app's env versions that are neither current nor in keep
// (the versions retained releases point at). Releases that pointed at a removed version keep
// their history row; their env_version_id becomes NULL, so they can no longer be rolled back to.
func (s *Store) DeleteEnvVersionsExcept(ctx context.Context, appID string, keep []string) (int64, error) {
	args := []any{appID, appID}
	placeholders := make([]string, 0, len(keep))
	for _, id := range keep {
		placeholders = append(placeholders, "?")
		args = append(args, id)
	}
	query := `DELETE FROM env_versions WHERE app_id = ?
		AND id IS NOT (SELECT env_version_id FROM apps WHERE id = ?)`
	if len(placeholders) > 0 {
		query += ` AND id NOT IN (` + strings.Join(placeholders, ", ") + `)`
	}
	res, err := s.db.ExecContext(ctx, query, args...)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// AddLegacyEnvVersion stores a pre-0005 release's environment as a new env version of its app
// (without changing the app's current version) and links the release to it.
func (s *Store) AddLegacyEnvVersion(ctx context.Context, depID, appID, sealed, runtimeConfig string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	id, err := insertEnvVersion(ctx, tx, appID, sealed)
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE deployments SET env_version_id = ?, runtime_config = ?, env_snapshot_encrypted = '' WHERE id = ?`,
		id, runtimeConfig, depID); err != nil {
		return err
	}
	return tx.Commit()
}

// ListLegacySnapshotDeployments returns releases created before migration 0005 whose environment
// is still in env_snapshot_encrypted.
func (s *Store) ListLegacySnapshotDeployments(ctx context.Context) ([]Deployment, error) {
	return s.queryDeployments(ctx, `WHERE env_version_id IS NULL AND env_snapshot_encrypted != '' ORDER BY app_id, release_number`)
}

// FinishSwitch records a completed route switch in one transaction: the new release becomes the
// app's live release and the old one gets oldStatus ("inactive", or "rolled_back" after a rollback).
func (s *Store) FinishSwitch(ctx context.Context, appID, newDepID, oldDepID, oldStatus string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `UPDATE apps SET active_release_id = ?, updated_at = ? WHERE id = ?`, newDepID, now, appID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE deployments SET status = 'active', error_message = '', finished_at = ? WHERE id = ?`, now, newDepID); err != nil {
		return err
	}
	if oldDepID != "" {
		if _, err := tx.ExecContext(ctx, `UPDATE deployments SET status = ? WHERE id = ?`, oldStatus, oldDepID); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// FindDeploymentByRelease returns an app's deployment with the given release number.
func (s *Store) FindDeploymentByRelease(ctx context.Context, appID string, release int) (*Deployment, error) {
	deps, err := s.queryDeployments(ctx, `WHERE app_id = ? AND release_number = ?`, appID, release)
	if err != nil {
		return nil, err
	}
	if len(deps) == 0 {
		return nil, ErrNotFound
	}
	return &deps[0], nil
}

const deploymentCols = `
	id, app_id, release_number, status, trigger,
	commit_hash, commit_message, env_snapshot_encrypted, env_version_id, runtime_config, source_deployment_id,
	build_logs, error_message, image_ref, started_at, finished_at`

func scanDeployment(row rowScanner) (*Deployment, error) {
	var dep Deployment
	var startedAt sql.NullString
	if err := row.Scan(
		&dep.ID, &dep.AppID, &dep.ReleaseNumber, &dep.Status, &dep.Trigger,
		&dep.CommitHash, &dep.CommitMessage, &dep.EnvSnapshotEncrypted, &dep.EnvVersionID, &dep.RuntimeConfig, &dep.SourceDeploymentID,
		&dep.BuildLogs, &dep.ErrorMessage, &dep.ImageRef, &startedAt, &dep.FinishedAt,
	); err != nil {
		return nil, err
	}
	if startedAt.Valid {
		dep.StartedAt = startedAt.String
	}
	return &dep, nil
}

func (s *Store) queryDeployments(ctx context.Context, where string, args ...any) ([]Deployment, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+deploymentCols+` FROM deployments `+where, args...)
	if err != nil {
		return nil, fmt.Errorf("querying deployments: %w", err)
	}
	defer rows.Close()
	var deps []Deployment
	for rows.Next() {
		dep, err := scanDeployment(rows)
		if err != nil {
			return nil, fmt.Errorf("scanning deployment: %w", err)
		}
		deps = append(deps, *dep)
	}
	return deps, rows.Err()
}
