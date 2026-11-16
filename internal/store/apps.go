// Package store provides SQLite persistence for dermaga.
package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// App represents an application managed by dermaga (APP-1).
type App struct {
	ID                string  `json:"id"`
	Name              string  `json:"name"`
	RepoOwner         string  `json:"repo_owner"`
	RepoName          string  `json:"repo_name"`
	Branch            string  `json:"branch"`
	ContainerfilePath string  `json:"containerfile_path"`
	BuildContext      string  `json:"build_context"`
	Command           *string `json:"command"`
	Port              int     `json:"port"`
	HealthType        string  `json:"health_type"` // 'http', 'cmd', 'none'
	HealthPath        *string `json:"health_path"`
	HealthCmd         *string `json:"health_cmd"`
	HealthTimeoutSec  int     `json:"health_timeout_sec"`
	EnvEncrypted      string  `json:"env_encrypted"`
	BuildEnvEncrypted string  `json:"build_env_encrypted"`
	AutoDeploy        bool    `json:"auto_deploy"`
	ActiveReleaseID   *string `json:"active_release_id"`
	CreatedAt         string  `json:"created_at"`
	UpdatedAt         string  `json:"updated_at"`
}

// Domain represents a routing domain for an application (APP-2).
type Domain struct {
	ID          string  `json:"id"`
	AppID       string  `json:"app_id"`
	Domain      string  `json:"domain"`
	IsPrimary   bool    `json:"is_primary"`
	RedirectTo  *string `json:"redirect_to"`
	PathPrefix  *string `json:"path_prefix"`
	StripPrefix bool    `json:"strip_prefix"`
	CreatedAt   string  `json:"created_at"`
}

// Deployment represents a deployment release of an application (DEP-9).
type Deployment struct {
	ID                   string  `json:"id"`
	AppID                string  `json:"app_id"`
	ReleaseNumber        int     `json:"release_number"`
	Status               string  `json:"status"`
	Trigger              string  `json:"trigger"`
	CommitHash           string  `json:"commit_hash"`
	CommitMessage        string  `json:"commit_message"`
	EnvSnapshotEncrypted string  `json:"env_snapshot_encrypted"`
	BuildLogs            string  `json:"build_logs"`
	StartedAt            string  `json:"started_at"`
	FinishedAt           *string `json:"finished_at"`
}

// ActiveAppRecord encapsulates an active application with its active release and domains for reconciliation (DEP-10).
type ActiveAppRecord struct {
	App        App
	Deployment Deployment
	Domains    []Domain
}

// CreateApp inserts a new application record.
func (s *Store) CreateApp(ctx context.Context, app *App) error {
	if app.CreatedAt == "" {
		app.CreatedAt = time.Now().UTC().Format(time.RFC3339)
	}
	if app.UpdatedAt == "" {
		app.UpdatedAt = app.CreatedAt
	}
	if app.Branch == "" {
		app.Branch = "main"
	}
	if app.ContainerfilePath == "" {
		app.ContainerfilePath = "Containerfile"
	}
	if app.BuildContext == "" {
		app.BuildContext = "."
	}
	if app.HealthTimeoutSec <= 0 {
		app.HealthTimeoutSec = 30
	}

	query := `
		INSERT INTO apps (
			id, name, repo_owner, repo_name, branch,
			containerfile_path, build_context, command, port,
			health_type, health_path, health_cmd, health_timeout_sec,
			env_encrypted, build_env_encrypted, auto_deploy, active_release_id,
			created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`
	autoDeployInt := 0
	if app.AutoDeploy {
		autoDeployInt = 1
	}

	_, err := s.db.ExecContext(ctx, query,
		app.ID, app.Name, app.RepoOwner, app.RepoName, app.Branch,
		app.ContainerfilePath, app.BuildContext, app.Command, app.Port,
		app.HealthType, app.HealthPath, app.HealthCmd, app.HealthTimeoutSec,
		app.EnvEncrypted, app.BuildEnvEncrypted, autoDeployInt, app.ActiveReleaseID,
		app.CreatedAt, app.UpdatedAt,
	)
	return err
}

// GetApp retrieves an app by ID.
func (s *Store) GetApp(ctx context.Context, id string) (*App, error) {
	query := `
		SELECT
			id, name, repo_owner, repo_name, branch,
			containerfile_path, build_context, command, port,
			health_type, health_path, health_cmd, health_timeout_sec,
			env_encrypted, build_env_encrypted, auto_deploy, active_release_id,
			created_at, updated_at
		FROM apps WHERE id = ?
	`
	return s.scanApp(s.db.QueryRowContext(ctx, query, id))
}

// GetAppByName retrieves an app by name.
func (s *Store) GetAppByName(ctx context.Context, name string) (*App, error) {
	query := `
		SELECT
			id, name, repo_owner, repo_name, branch,
			containerfile_path, build_context, command, port,
			health_type, health_path, health_cmd, health_timeout_sec,
			env_encrypted, build_env_encrypted, auto_deploy, active_release_id,
			created_at, updated_at
		FROM apps WHERE name = ?
	`
	return s.scanApp(s.db.QueryRowContext(ctx, query, name))
}

// ListApps returns all registered applications.
func (s *Store) ListApps(ctx context.Context) ([]App, error) {
	query := `
		SELECT
			id, name, repo_owner, repo_name, branch,
			containerfile_path, build_context, command, port,
			health_type, health_path, health_cmd, health_timeout_sec,
			env_encrypted, build_env_encrypted, auto_deploy, active_release_id,
			created_at, updated_at
		FROM apps ORDER BY name ASC
	`
	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("querying apps: %w", err)
	}
	defer rows.Close()

	var apps []App
	for rows.Next() {
		app, err := s.scanApp(rows)
		if err != nil {
			return nil, err
		}
		apps = append(apps, *app)
	}
	return apps, rows.Err()
}

// CreateDomain inserts a new domain routing record.
func (s *Store) CreateDomain(ctx context.Context, d *Domain) error {
	if d.CreatedAt == "" {
		d.CreatedAt = time.Now().UTC().Format(time.RFC3339)
	}
	isPrimaryInt := 0
	if d.IsPrimary {
		isPrimaryInt = 1
	}
	stripPrefixInt := 0
	if d.StripPrefix {
		stripPrefixInt = 1
	}

	query := `
		INSERT INTO domains (id, app_id, domain, is_primary, redirect_to, path_prefix, strip_prefix, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`
	_, err := s.db.ExecContext(ctx, query, d.ID, d.AppID, d.Domain, isPrimaryInt, d.RedirectTo, d.PathPrefix, stripPrefixInt, d.CreatedAt)
	return err
}

// ListDomainsByApp retrieves all domains associated with an app.
func (s *Store) ListDomainsByApp(ctx context.Context, appID string) ([]Domain, error) {
	query := `
		SELECT id, app_id, domain, is_primary, redirect_to, path_prefix, strip_prefix, created_at
		FROM domains WHERE app_id = ? ORDER BY is_primary DESC, domain ASC
	`
	rows, err := s.db.QueryContext(ctx, query, appID)
	if err != nil {
		return nil, fmt.Errorf("querying domains: %w", err)
	}
	defer rows.Close()

	var domains []Domain
	for rows.Next() {
		var d Domain
		var isPrimaryInt, stripPrefixInt int
		var createdAt sql.NullString
		if err := rows.Scan(&d.ID, &d.AppID, &d.Domain, &isPrimaryInt, &d.RedirectTo, &d.PathPrefix, &stripPrefixInt, &createdAt); err != nil {
			return nil, fmt.Errorf("scanning domain: %w", err)
		}
		d.IsPrimary = (isPrimaryInt == 1)
		d.StripPrefix = (stripPrefixInt == 1)
		if createdAt.Valid {
			d.CreatedAt = createdAt.String
		}
		domains = append(domains, d)
	}
	return domains, rows.Err()
}

// CreateDeployment inserts a deployment release record.
func (s *Store) CreateDeployment(ctx context.Context, dep *Deployment) error {
	if dep.StartedAt == "" {
		dep.StartedAt = time.Now().UTC().Format(time.RFC3339)
	}

	query := `
		INSERT INTO deployments (
			id, app_id, release_number, status, trigger,
			commit_hash, commit_message, env_snapshot_encrypted, build_logs,
			started_at, finished_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`
	_, err := s.db.ExecContext(ctx, query,
		dep.ID, dep.AppID, dep.ReleaseNumber, dep.Status, dep.Trigger,
		dep.CommitHash, dep.CommitMessage, dep.EnvSnapshotEncrypted, dep.BuildLogs,
		dep.StartedAt, dep.FinishedAt,
	)
	return err
}

// GetDeployment retrieves a deployment by ID.
func (s *Store) GetDeployment(ctx context.Context, id string) (*Deployment, error) {
	query := `
		SELECT
			id, app_id, release_number, status, trigger,
			commit_hash, commit_message, env_snapshot_encrypted, build_logs,
			started_at, finished_at
		FROM deployments WHERE id = ?
	`
	var dep Deployment
	var startedAt sql.NullString
	err := s.db.QueryRowContext(ctx, query, id).Scan(
		&dep.ID, &dep.AppID, &dep.ReleaseNumber, &dep.Status, &dep.Trigger,
		&dep.CommitHash, &dep.CommitMessage, &dep.EnvSnapshotEncrypted, &dep.BuildLogs,
		&startedAt, &dep.FinishedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("querying deployment: %w", err)
	}
	if startedAt.Valid {
		dep.StartedAt = startedAt.String
	}
	return &dep, nil
}

// SetActiveRelease updates the active release ID for an app.
func (s *Store) SetActiveRelease(ctx context.Context, appID, releaseID string) error {
	query := `
		UPDATE apps SET active_release_id = ?, updated_at = strftime('%Y-%m-%dT%H:%M:%SZ', 'now')
		WHERE id = ?
	`
	res, err := s.db.ExecContext(ctx, query, releaseID, appID)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// GetActiveApps returns all apps that currently have an active release, along with their active deployment and domains.
func (s *Store) GetActiveApps(ctx context.Context) ([]ActiveAppRecord, error) {
	apps, err := s.ListApps(ctx)
	if err != nil {
		return nil, err
	}

	var activeRecords []ActiveAppRecord
	for _, a := range apps {
		if a.ActiveReleaseID == nil || *a.ActiveReleaseID == "" {
			continue
		}

		dep, err := s.GetDeployment(ctx, *a.ActiveReleaseID)
		if err != nil {
			if errors.Is(err, ErrNotFound) {
				continue
			}
			return nil, fmt.Errorf("fetching active release for app %s: %w", a.Name, err)
		}

		domains, err := s.ListDomainsByApp(ctx, a.ID)
		if err != nil {
			return nil, fmt.Errorf("fetching domains for app %s: %w", a.Name, err)
		}

		activeRecords = append(activeRecords, ActiveAppRecord{
			App:        a,
			Deployment: *dep,
			Domains:    domains,
		})
	}

	return activeRecords, nil
}

type rowScanner interface {
	Scan(dest ...any) error
}

func (s *Store) scanApp(row rowScanner) (*App, error) {
	var a App
	var autoDeployInt int
	var createdAt, updatedAt sql.NullString
	err := row.Scan(
		&a.ID, &a.Name, &a.RepoOwner, &a.RepoName, &a.Branch,
		&a.ContainerfilePath, &a.BuildContext, &a.Command, &a.Port,
		&a.HealthType, &a.HealthPath, &a.HealthCmd, &a.HealthTimeoutSec,
		&a.EnvEncrypted, &a.BuildEnvEncrypted, &autoDeployInt, &a.ActiveReleaseID,
		&createdAt, &updatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("scanning app: %w", err)
	}
	a.AutoDeploy = (autoDeployInt == 1)
	if createdAt.Valid {
		a.CreatedAt = createdAt.String
	}
	if updatedAt.Valid {
		a.UpdatedAt = updatedAt.String
	}
	return &a, nil
}
