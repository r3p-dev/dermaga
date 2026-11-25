package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// GitHubInstallation represents an installation of the server's GitHub App (GHA-3, GHA-6, GHA-7).
type GitHubInstallation struct {
	ID                  int64   `json:"id"`
	AccountLogin        string  `json:"account_login"`
	AccountType         string  `json:"account_type"`
	RepositorySelection string  `json:"repository_selection"`
	SuspendedAt         *string `json:"suspended_at,omitempty"`
	HTMLURL             string  `json:"html_url"`
	UpdatedAt           string  `json:"updated_at"`
}

// GitHubRepository represents a repository visible to a GitHub App installation (GHA-3, GHA-6).
type GitHubRepository struct {
	ID             int64  `json:"id"`
	InstallationID int64  `json:"installation_id"`
	FullName       string `json:"full_name"`
	Owner          string `json:"owner"`
	Name           string `json:"name"`
	Private        bool   `json:"private"`
	DefaultBranch  string `json:"default_branch"`
	HTMLURL        string `json:"html_url"`
	UpdatedAt      string `json:"updated_at"`
}

// WebhookDelivery records a received GitHub webhook delivery for deduplication and UI status (GHA-8, SEC-13).
type WebhookDelivery struct {
	DeliveryID         string `json:"delivery_id"`
	Event              string `json:"event"`
	Action             string `json:"action"`
	RepositoryFullName string `json:"repository_full_name"`
	Status             string `json:"status"`
	ReceivedAt         string `json:"received_at"`
}

// ReplaceGitHubInstallationsAndRepos atomically replaces the cached GitHub App installations
// and repositories with the latest snapshot fetched from GitHub.
func (s *Store) ReplaceGitHubInstallationsAndRepos(ctx context.Context, installs []GitHubInstallation, repos []GitHubRepository) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin github sync tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, `DELETE FROM github_repositories`); err != nil {
		return fmt.Errorf("clearing github repositories: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM github_installations`); err != nil {
		return fmt.Errorf("clearing github installations: %w", err)
	}

	for _, inst := range installs {
		sel := inst.RepositorySelection
		if sel == "" {
			sel = "selected"
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO github_installations (id, account_login, account_type, repository_selection, suspended_at, html_url, updated_at)
			 VALUES (?, ?, ?, ?, ?, ?, strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))`,
			inst.ID, inst.AccountLogin, inst.AccountType, sel, inst.SuspendedAt, inst.HTMLURL,
		); err != nil {
			return fmt.Errorf("inserting github installation %d: %w", inst.ID, err)
		}
	}

	for _, repo := range repos {
		priv := 0
		if repo.Private {
			priv = 1
		}
		branch := repo.DefaultBranch
		if branch == "" {
			branch = "main"
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO github_repositories (id, installation_id, full_name, owner, name, private, default_branch, html_url, updated_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))`,
			repo.ID, repo.InstallationID, repo.FullName, repo.Owner, repo.Name, priv, branch, repo.HTMLURL,
		); err != nil {
			return fmt.Errorf("inserting github repository %s: %w", repo.FullName, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("committing github sync tx: %w", err)
	}
	return nil
}

// ListGitHubInstallations returns all cached GitHub App installations ordered by login.
func (s *Store) ListGitHubInstallations(ctx context.Context) ([]GitHubInstallation, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, account_login, account_type, repository_selection, suspended_at, html_url, updated_at
		 FROM github_installations
		 ORDER BY account_login ASC, id ASC`,
	)
	if err != nil {
		return nil, fmt.Errorf("listing github installations: %w", err)
	}
	defer rows.Close()

	var out []GitHubInstallation
	for rows.Next() {
		var inst GitHubInstallation
		var suspended sql.NullString
		if err := rows.Scan(&inst.ID, &inst.AccountLogin, &inst.AccountType, &inst.RepositorySelection, &suspended, &inst.HTMLURL, &inst.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scanning github installation: %w", err)
		}
		if suspended.Valid {
			v := suspended.String
			inst.SuspendedAt = &v
		}
		out = append(out, inst)
	}
	return out, rows.Err()
}

// ListGitHubRepositories returns all cached GitHub repositories ordered by full_name.
func (s *Store) ListGitHubRepositories(ctx context.Context) ([]GitHubRepository, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, installation_id, full_name, owner, name, private, default_branch, html_url, updated_at
		 FROM github_repositories
		 ORDER BY full_name ASC`,
	)
	if err != nil {
		return nil, fmt.Errorf("listing github repositories: %w", err)
	}
	defer rows.Close()

	var out []GitHubRepository
	for rows.Next() {
		var repo GitHubRepository
		var priv int
		if err := rows.Scan(&repo.ID, &repo.InstallationID, &repo.FullName, &repo.Owner, &repo.Name, &priv, &repo.DefaultBranch, &repo.HTMLURL, &repo.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scanning github repository: %w", err)
		}
		repo.Private = priv != 0
		out = append(out, repo)
	}
	return out, rows.Err()
}

// GetLastWebhookDelivery returns the most recently recorded webhook delivery, or nil if none exist (GHA-8).
func (s *Store) GetLastWebhookDelivery(ctx context.Context) (*WebhookDelivery, error) {
	var d WebhookDelivery
	err := s.db.QueryRowContext(ctx,
		`SELECT delivery_id, event, action, repository_full_name, status, received_at
		 FROM webhook_deliveries
		 ORDER BY rowid DESC
		 LIMIT 1`,
	).Scan(&d.DeliveryID, &d.Event, &d.Action, &d.RepositoryFullName, &d.Status, &d.ReceivedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("querying last webhook delivery: %w", err)
	}
	return &d, nil
}

// RecordWebhookDelivery records a webhook delivery if its delivery_id has not been seen yet.
// Returns duplicate=true if the delivery_id already exists (SEC-13).
func (s *Store) RecordWebhookDelivery(ctx context.Context, d WebhookDelivery) (duplicate bool, err error) {
	res, err := s.db.ExecContext(ctx,
		`INSERT OR IGNORE INTO webhook_deliveries (delivery_id, event, action, repository_full_name, status)
		 VALUES (?, ?, ?, ?, ?)`,
		d.DeliveryID, d.Event, d.Action, d.RepositoryFullName, d.Status,
	)
	if err != nil {
		return false, fmt.Errorf("recording webhook delivery: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("checking webhook delivery rows affected: %w", err)
	}
	return n == 0, nil
}

// ClearGitHubData removes all cached installations, repositories, deliveries, and GitHub settings keys (GHA-6).
func (s *Store) ClearGitHubData(ctx context.Context) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin clear github tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	for _, q := range []string{
		`DELETE FROM github_repositories`,
		`DELETE FROM github_installations`,
		`DELETE FROM webhook_deliveries`,
		`DELETE FROM settings WHERE key LIKE 'github_%'`,
	} {
		if _, err := tx.ExecContext(ctx, q); err != nil {
			return fmt.Errorf("executing %q: %w", q, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("committing clear github tx: %w", err)
	}
	return nil
}

// UpsertGitHubInstallation inserts or updates a single GitHub App installation (GHA-7).
func (s *Store) UpsertGitHubInstallation(ctx context.Context, inst GitHubInstallation) error {
	sel := inst.RepositorySelection
	if sel == "" {
		sel = "selected"
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO github_installations (id, account_login, account_type, repository_selection, suspended_at, html_url, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
		 ON CONFLICT(id) DO UPDATE SET
		   account_login = excluded.account_login,
		   account_type = excluded.account_type,
		   repository_selection = excluded.repository_selection,
		   suspended_at = excluded.suspended_at,
		   html_url = excluded.html_url,
		   updated_at = excluded.updated_at`,
		inst.ID, inst.AccountLogin, inst.AccountType, sel, inst.SuspendedAt, inst.HTMLURL,
	)
	return err
}

// DeleteGitHubInstallation removes an installation and its cascaded repositories (GHA-7).
func (s *Store) DeleteGitHubInstallation(ctx context.Context, installationID int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM github_installations WHERE id = ?`, installationID)
	return err
}

// SetGitHubInstallationSuspended updates suspended_at for an installation (GHA-7).
func (s *Store) SetGitHubInstallationSuspended(ctx context.Context, installationID int64, suspendedAt *string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE github_installations SET suspended_at = ?, updated_at = strftime('%Y-%m-%dT%H:%M:%SZ', 'now') WHERE id = ?`,
		suspendedAt, installationID,
	)
	return err
}

// AddGitHubRepositories inserts or updates repositories for an installation (GHA-7).
func (s *Store) AddGitHubRepositories(ctx context.Context, repos []GitHubRepository) error {
	for _, repo := range repos {
		priv := 0
		if repo.Private {
			priv = 1
		}
		branch := repo.DefaultBranch
		if branch == "" {
			branch = "main"
		}
		if _, err := s.db.ExecContext(ctx,
			`INSERT INTO github_repositories (id, installation_id, full_name, owner, name, private, default_branch, html_url, updated_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
			 ON CONFLICT(id) DO UPDATE SET
			   installation_id = excluded.installation_id,
			   full_name = excluded.full_name,
			   owner = excluded.owner,
			   name = excluded.name,
			   private = excluded.private,
			   default_branch = excluded.default_branch,
			   html_url = excluded.html_url,
			   updated_at = excluded.updated_at`,
			repo.ID, repo.InstallationID, repo.FullName, repo.Owner, repo.Name, priv, branch, repo.HTMLURL,
		); err != nil {
			return fmt.Errorf("upserting github repository %s: %w", repo.FullName, err)
		}
	}
	return nil
}

// DeleteGitHubRepositoriesByID removes repositories by their GitHub IDs (GHA-7).
func (s *Store) DeleteGitHubRepositoriesByID(ctx context.Context, ids []int64) error {
	for _, id := range ids {
		if _, err := s.db.ExecContext(ctx, `DELETE FROM github_repositories WHERE id = ?`, id); err != nil {
			return fmt.Errorf("deleting github repository %d: %w", id, err)
		}
	}
	return nil
}

// GetGitHubRepositoryByFullName looks up an accessible repository by owner/name.
func (s *Store) GetGitHubRepositoryByFullName(ctx context.Context, fullName string) (*GitHubRepository, error) {
	var r GitHubRepository
	var priv int
	err := s.db.QueryRowContext(ctx, `
		SELECT id, installation_id, full_name, owner, name, private, default_branch, html_url, updated_at
		FROM github_repositories WHERE full_name = ?`, fullName).Scan(
		&r.ID, &r.InstallationID, &r.FullName, &r.Owner, &r.Name, &priv, &r.DefaultBranch, &r.HTMLURL, &r.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("querying github repository %q: %w", fullName, err)
	}
	r.Private = (priv == 1)
	return &r, nil
}
