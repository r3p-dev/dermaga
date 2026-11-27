// Package store provides SQLite persistence for dermaga.
package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// App represents an application managed by dermaga (APP-1, APP-5, APP-6, APP-7).
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
	HealthConsecutive int     `json:"health_consecutive"`
	MemoryLimit       string  `json:"memory_limit"`
	CPULimit          string  `json:"cpu_limit"`
	PidsLimit         int     `json:"pids_limit"`
	ReadOnlyRoot      bool    `json:"read_only_root"`
	AddCapabilities   string  `json:"add_capabilities"`
	EnvEncrypted      string  `json:"env_encrypted"`
	BuildEnvEncrypted string  `json:"build_env_encrypted"`
	AutoDeploy        bool    `json:"auto_deploy"`
	WatchPaths        string  `json:"watch_paths"`    // APP-8: one path.Match glob per line
	EnvVersionID      *string `json:"env_version_id"` // current env version (DEC-32)
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
	EnvSnapshotEncrypted string  `json:"-"`                    // pre-0005 releases only (migration 0005)
	EnvVersionID         *string `json:"env_version_id"`       // DEC-32
	RuntimeConfig        string  `json:"runtime_config"`       // JSON without secrets (DEP-4)
	SourceDeploymentID   *string `json:"source_deployment_id"` // set on trigger=rollback
	BuildLogs            string  `json:"build_logs"`
	ErrorMessage         string  `json:"error_message"`
	ImageRef             string  `json:"image_ref"`
	StartedAt            string  `json:"started_at"`
	FinishedAt           *string `json:"finished_at"`
}

// DeploymentStatusLabel maps internal deployment status values to PRD DEP-9 labels.
func DeploymentStatusLabel(status string) string {
	switch status {
	case "probing":
		return "healthy"
	case "active":
		return "live"
	case "superseded":
		return "replaced by a newer deploy"
	case "switching":
		return "Switching traffic"
	case "inactive":
		return "Previous release"
	case "rolled_back":
		return "Rolled back"
	default:
		return status
	}
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
	if app.HealthConsecutive <= 0 {
		app.HealthConsecutive = 3
	}
	if app.MemoryLimit == "" {
		app.MemoryLimit = "512m"
	}
	if app.CPULimit == "" {
		app.CPULimit = "1.0"
	}
	if app.PidsLimit <= 0 {
		app.PidsLimit = 128
	}

	query := `
		INSERT INTO apps (
			id, name, repo_owner, repo_name, branch,
			containerfile_path, build_context, command, port,
			health_type, health_path, health_cmd, health_timeout_sec, health_consecutive,
			memory_limit, cpu_limit, pids_limit, read_only_root, add_capabilities,
			env_encrypted, build_env_encrypted, auto_deploy, watch_paths, active_release_id,
			created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`
	autoDeployInt := 0
	if app.AutoDeploy {
		autoDeployInt = 1
	}
	readOnlyRootInt := 0
	if app.ReadOnlyRoot {
		readOnlyRootInt = 1
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, query,
		app.ID, app.Name, app.RepoOwner, app.RepoName, app.Branch,
		app.ContainerfilePath, app.BuildContext, app.Command, app.Port,
		app.HealthType, app.HealthPath, app.HealthCmd, app.HealthTimeoutSec, app.HealthConsecutive,
		app.MemoryLimit, app.CPULimit, app.PidsLimit, readOnlyRootInt, app.AddCapabilities,
		app.EnvEncrypted, app.BuildEnvEncrypted, autoDeployInt, app.WatchPaths, app.ActiveReleaseID,
		app.CreatedAt, app.UpdatedAt,
	); err != nil {
		return err
	}
	if err := addEnvVersion(ctx, tx, app); err != nil {
		return err
	}
	return tx.Commit()
}

const appSelectCols = `
	id, name, repo_owner, repo_name, branch,
	containerfile_path, build_context, command, port,
	health_type, health_path, health_cmd, health_timeout_sec, health_consecutive,
	memory_limit, cpu_limit, pids_limit, read_only_root, add_capabilities,
	env_encrypted, build_env_encrypted, auto_deploy, watch_paths, env_version_id, active_release_id,
	created_at, updated_at
`

// GetApp retrieves an app by ID.
func (s *Store) GetApp(ctx context.Context, id string) (*App, error) {
	return s.scanApp(s.db.QueryRowContext(ctx, `SELECT `+appSelectCols+` FROM apps WHERE id = ?`, id))
}

// GetAppByName retrieves an app by name.
func (s *Store) GetAppByName(ctx context.Context, name string) (*App, error) {
	return s.scanApp(s.db.QueryRowContext(ctx, `SELECT `+appSelectCols+` FROM apps WHERE name = ?`, name))
}

// ListApps returns all registered applications.
func (s *Store) ListApps(ctx context.Context) ([]App, error) {
	return s.listApps(ctx, ``)
}

// ListAppsByRepo returns the apps built from owner/name; GitHub names are case-insensitive (APP-8).
func (s *Store) ListAppsByRepo(ctx context.Context, owner, name string) ([]App, error) {
	return s.listApps(ctx, `WHERE repo_owner = ? COLLATE NOCASE AND repo_name = ? COLLATE NOCASE`, owner, name)
}

func (s *Store) listApps(ctx context.Context, where string, args ...any) ([]App, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+appSelectCols+` FROM apps `+where+` ORDER BY name ASC`, args...)
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

	_, err := s.db.ExecContext(ctx, `
		INSERT INTO deployments (
			id, app_id, release_number, status, trigger,
			commit_hash, commit_message, env_version_id, runtime_config, source_deployment_id,
			build_logs, error_message, image_ref, started_at, finished_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		dep.ID, dep.AppID, dep.ReleaseNumber, dep.Status, dep.Trigger,
		dep.CommitHash, dep.CommitMessage, dep.EnvVersionID, dep.RuntimeConfig, dep.SourceDeploymentID,
		dep.BuildLogs, dep.ErrorMessage, dep.ImageRef, dep.StartedAt, dep.FinishedAt,
	)
	return err
}

// GetDeployment retrieves a deployment by ID.
func (s *Store) GetDeployment(ctx context.Context, id string) (*Deployment, error) {
	dep, err := scanDeployment(s.db.QueryRowContext(ctx, `SELECT `+deploymentCols+` FROM deployments WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("querying deployment: %w", err)
	}
	return dep, nil
}

// UpdateApp updates mutable configuration fields of an existing app (APP-1, APP-5, APP-6, APP-7).
func (s *Store) UpdateApp(ctx context.Context, app *App) error {
	app.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	autoDeployInt := 0
	if app.AutoDeploy {
		autoDeployInt = 1
	}
	readOnlyRootInt := 0
	if app.ReadOnlyRoot {
		readOnlyRootInt = 1
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	var storedEnv string
	err = tx.QueryRowContext(ctx, `SELECT env_encrypted FROM apps WHERE id = ?`, app.ID).Scan(&storedEnv)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	// A re-sealed environment always differs (random nonce): every env save is a new version.
	if app.EnvEncrypted != storedEnv {
		if err := addEnvVersion(ctx, tx, app); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE apps SET
			branch = ?, containerfile_path = ?, build_context = ?, command = ?, port = ?,
			health_type = ?, health_path = ?, health_cmd = ?, health_timeout_sec = ?, health_consecutive = ?,
			memory_limit = ?, cpu_limit = ?, pids_limit = ?, read_only_root = ?, add_capabilities = ?,
			env_encrypted = ?, build_env_encrypted = ?, auto_deploy = ?, watch_paths = ?, updated_at = ?
		WHERE id = ?`,
		app.Branch, app.ContainerfilePath, app.BuildContext, app.Command, app.Port,
		app.HealthType, app.HealthPath, app.HealthCmd, app.HealthTimeoutSec, app.HealthConsecutive,
		app.MemoryLimit, app.CPULimit, app.PidsLimit, readOnlyRootInt, app.AddCapabilities,
		app.EnvEncrypted, app.BuildEnvEncrypted, autoDeployInt, app.WatchPaths, app.UpdatedAt, app.ID,
	); err != nil {
		return err
	}
	return tx.Commit()
}

// DeleteApp removes an app and cascades to domains and deployments (APP-9).
func (s *Store) DeleteApp(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM apps WHERE id = ?`, id)
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

// DomainExists checks whether a domain hostname is already assigned to any app.
func (s *Store) DomainExists(ctx context.Context, domain string) (bool, error) {
	var count int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM domains WHERE domain = ?`, domain).Scan(&count)
	return count > 0, err
}

// NextReleaseNumber returns the next sequential release number for an app.
func (s *Store) NextReleaseNumber(ctx context.Context, appID string) (int, error) {
	var maxRel sql.NullInt64
	err := s.db.QueryRowContext(ctx, `SELECT MAX(release_number) FROM deployments WHERE app_id = ?`, appID).Scan(&maxRel)
	if err != nil {
		return 0, err
	}
	if !maxRel.Valid {
		return 1, nil
	}
	return int(maxRel.Int64) + 1, nil
}

// ListDeploymentsByApp returns deployments for an app ordered newest first (DEP-9).
func (s *Store) ListDeploymentsByApp(ctx context.Context, appID string) ([]Deployment, error) {
	return s.queryDeployments(ctx, `WHERE app_id = ? ORDER BY release_number DESC`, appID)
}

// UpdateDeploymentStatus updates status, optional error message, and finished_at for a deployment.
func (s *Store) UpdateDeploymentStatus(ctx context.Context, id, status, errorMsg string, finishedAt *string) error {
	res, err := s.db.ExecContext(ctx, `
		UPDATE deployments SET status = ?, error_message = ?, finished_at = ? WHERE id = ?`,
		status, errorMsg, finishedAt, id)
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

// SetDeploymentBuildLogs stores the masked build log text for a deployment (LOG-2).
func (s *Store) SetDeploymentBuildLogs(ctx context.Context, id, buildLogs string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE deployments SET build_logs = ? WHERE id = ?`, buildLogs, id)
	return err
}

// SetDeploymentImageRef records the built image reference for a deployment.
func (s *Store) SetDeploymentImageRef(ctx context.Context, id, imageRef string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE deployments SET image_ref = ? WHERE id = ?`, imageRef, id)
	return err
}

// FailInFlightDeployments marks any queued or in-progress deployments as failed after a daemon
// restart and returns them, so their leftovers (image) can be cleaned up.
func (s *Store) FailInFlightDeployments(ctx context.Context, reason string) ([]Deployment, error) {
	now := time.Now().UTC().Format(time.RFC3339)
	rows, err := s.db.QueryContext(ctx, `
		UPDATE deployments
		SET status = 'failed', error_message = ?, finished_at = ?
		WHERE status IN ('queued', 'building', 'starting', 'probing', 'switching')
		RETURNING `+deploymentCols, reason, now)
	if err != nil {
		return nil, err
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
	var autoDeployInt, readOnlyRootInt int
	var createdAt, updatedAt sql.NullString
	err := row.Scan(
		&a.ID, &a.Name, &a.RepoOwner, &a.RepoName, &a.Branch,
		&a.ContainerfilePath, &a.BuildContext, &a.Command, &a.Port,
		&a.HealthType, &a.HealthPath, &a.HealthCmd, &a.HealthTimeoutSec, &a.HealthConsecutive,
		&a.MemoryLimit, &a.CPULimit, &a.PidsLimit, &readOnlyRootInt, &a.AddCapabilities,
		&a.EnvEncrypted, &a.BuildEnvEncrypted, &autoDeployInt, &a.WatchPaths, &a.EnvVersionID, &a.ActiveReleaseID,
		&createdAt, &updatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("scanning app: %w", err)
	}
	a.AutoDeploy = (autoDeployInt == 1)
	a.ReadOnlyRoot = (readOnlyRootInt == 1)
	if createdAt.Valid {
		a.CreatedAt = createdAt.String
	}
	if updatedAt.Valid {
		a.UpdatedAt = updatedAt.String
	}
	return &a, nil
}
