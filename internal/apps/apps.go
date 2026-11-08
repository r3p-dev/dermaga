// Package apps stores apps, their domains, env vars, releases and webhook
// deliveries (BRIEF §3.4). Secrets are sealed with secrets.Box and bound to
// their column.
package apps

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/r3p-dev/dermaga/internal/secrets"
	"github.com/r3p-dev/dermaga/internal/spec"
)

var ErrNotFound = errors.New("no such app")

type Store struct {
	DB  *sql.DB
	Box *secrets.Box
}

type App struct {
	ID   string   `json:"id"`
	Spec spec.App `json:"-"`
	Live *Release `json:"live,omitempty"`
}

type Release struct {
	ID         int64     `json:"id"`
	Rel8       string    `json:"rel8"`
	Status     string    `json:"status"`
	Trigger    string    `json:"trigger"`
	Ref        string    `json:"ref,omitempty"`
	SHA        string    `json:"sha,omitempty"`
	Message    string    `json:"message,omitempty"`
	Image      string    `json:"image,omitempty"`
	Error      string    `json:"error,omitempty"`
	LogPath    string    `json:"-"`
	CreatedAt  time.Time `json:"createdAt"`
	FinishedAt time.Time `json:"finishedAt,omitzero"`
}

// Terminal release states (BRIEF §3.4 state machine).
func Terminal(status string) bool {
	return status == "live" || status == "failed" || status == "rolled_back" || status == "superseded"
}

func randHex(n int) string {
	b := make([]byte, n)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// Create stores a validated app with a random id (used in the webhook URL)
// and a random 32-byte webhook secret, which it returns once.
func (s Store) Create(ctx context.Context, a spec.App) (id, secret string, err error) {
	if err := a.Validate(); err != nil {
		return "", "", err
	}
	id, secret = randHex(8), randHex(32)
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return "", "", err
	}
	defer tx.Rollback()
	b, _ := json.Marshal(a)
	js := string(b) // STRICT TEXT column: a []byte would be a BLOB
	if _, err := tx.ExecContext(ctx, `INSERT INTO apps (id, name, spec, webhook_secret, created_at) VALUES (?, ?, ?, ?, ?)`,
		id, a.Name, js, s.Box.Seal(secret, "apps.webhook_secret"), time.Now().Unix()); err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			return "", "", fmt.Errorf("app %s already exists", a.Name)
		}
		return "", "", err
	}
	if err := putDomains(ctx, tx, id, a.Domains); err != nil {
		return "", "", err
	}
	return id, secret, tx.Commit()
}

// Save replaces an existing app's spec and domains.
func (s Store) Save(ctx context.Context, id string, a spec.App) error {
	if err := a.Validate(); err != nil {
		return err
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	b, _ := json.Marshal(a)
	js := string(b) // STRICT TEXT column: a []byte would be a BLOB
	if _, err := tx.ExecContext(ctx, `UPDATE apps SET spec = ? WHERE id = ?`, js, id); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM domains WHERE app_id = ?`, id); err != nil {
		return err
	}
	if err := putDomains(ctx, tx, id, a.Domains); err != nil {
		return err
	}
	return tx.Commit()
}

func putDomains(ctx context.Context, tx *sql.Tx, id string, ds []spec.Domain) error {
	for _, d := range ds {
		if _, err := tx.ExecContext(ctx, `INSERT INTO domains (host, app_id, is_primary, redirect) VALUES (?, ?, ?, ?)`,
			d.Host, id, d.Primary, d.Redirect); err != nil {
			if strings.Contains(err.Error(), "UNIQUE") {
				return fmt.Errorf("domain %s already belongs to another app", d.Host)
			}
			return err
		}
	}
	return nil
}

func (s Store) Remove(ctx context.Context, id string) error {
	_, err := s.DB.ExecContext(ctx, `DELETE FROM apps WHERE id = ?`, id)
	return err
}

const appCols = `SELECT a.id, a.name, a.spec FROM apps a`

// List returns every app with its domains and live release, by name.
func (s Store) List(ctx context.Context) ([]App, error) {
	return s.query(ctx, appCols+` ORDER BY a.name`)
}

// Get finds an app by name.
func (s Store) Get(ctx context.Context, name string) (App, error) {
	l, err := s.query(ctx, appCols+` WHERE a.name = ?`, name)
	if err != nil || len(l) == 0 {
		return App{}, errors.Join(ErrNotFound, err)
	}
	return l[0], nil
}

// ByID finds an app by its webhook id and returns its webhook secret.
func (s Store) ByID(ctx context.Context, id string) (App, string, error) {
	l, err := s.query(ctx, appCols+` WHERE a.id = ?`, id)
	if err != nil || len(l) == 0 {
		return App{}, "", errors.Join(ErrNotFound, err)
	}
	var sealed string
	if err := s.DB.QueryRowContext(ctx, `SELECT webhook_secret FROM apps WHERE id = ?`, id).Scan(&sealed); err != nil {
		return App{}, "", err
	}
	secret, err := s.Box.Open(sealed, "apps.webhook_secret")
	return l[0], secret, err
}

func (s Store) query(ctx context.Context, q string, args ...any) ([]App, error) {
	rows, err := s.DB.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	var out []App
	for rows.Next() {
		var a App
		var js string
		if err := rows.Scan(&a.ID, &a.Spec.Name, &js); err != nil {
			rows.Close()
			return nil, err
		}
		if err := json.Unmarshal([]byte(js), &a.Spec); err != nil {
			rows.Close()
			return nil, fmt.Errorf("app %s: spec: %w", a.Spec.Name, err)
		}
		out = append(out, a)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i := range out {
		ds, err := s.DB.QueryContext(ctx, `SELECT host, is_primary, redirect FROM domains WHERE app_id = ? ORDER BY is_primary DESC, host`, out[i].ID)
		if err != nil {
			return nil, err
		}
		for ds.Next() {
			var d spec.Domain
			if err := ds.Scan(&d.Host, &d.Primary, &d.Redirect); err != nil {
				ds.Close()
				return nil, err
			}
			out[i].Spec.Domains = append(out[i].Spec.Domains, d)
		}
		ds.Close()
		rs, err := s.releases(ctx, `WHERE app_id = ? AND status = 'live'`, out[i].ID)
		if err != nil {
			return nil, err
		}
		if len(rs) > 0 {
			out[i].Live = &rs[0]
		}
	}
	return out, nil
}

var envKeyRE = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,127}$`)

// ValidEnv checks a variable for Podman's env file: one KEY=VALUE per line,
// the value taken literally, so it can't contain a line break or NUL.
func ValidEnv(k, v string) error {
	if !envKeyRE.MatchString(k) {
		return fmt.Errorf("env key %q: letters, digits and _, not starting with a digit", k)
	}
	if strings.ContainsAny(v, "\n\r\x00") || len(v) > 32<<10 {
		return fmt.Errorf("env %s: the value can't contain line breaks or NUL and must be at most 32 KiB", k)
	}
	return nil
}

func (s Store) Env(ctx context.Context, appID string) (map[string]string, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT key, value FROM env_vars WHERE app_id = ?`, appID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	env := map[string]string{}
	for rows.Next() {
		var k, sealed string
		if err := rows.Scan(&k, &sealed); err != nil {
			return nil, err
		}
		v, err := s.Box.Open(sealed, "env_vars.value")
		if err != nil {
			return nil, fmt.Errorf("env %s: %w", k, err)
		}
		env[k] = v
	}
	return env, rows.Err()
}

// SetEnv sets and unsets variables in one transaction.
func (s Store) SetEnv(ctx context.Context, appID string, set map[string]string, unset []string) error {
	for k, v := range set {
		if err := ValidEnv(k, v); err != nil {
			return err
		}
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for k, v := range set {
		if _, err := tx.ExecContext(ctx, `INSERT INTO env_vars (app_id, key, value) VALUES (?, ?, ?)
			ON CONFLICT (app_id, key) DO UPDATE SET value = excluded.value`, appID, k, s.Box.Seal(v, "env_vars.value")); err != nil {
			return err
		}
	}
	for _, k := range unset {
		if _, err := tx.ExecContext(ctx, `DELETE FROM env_vars WHERE app_id = ? AND key = ?`, appID, k); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// EnvFile renders the env file Podman reads (EnvironmentFile=), sorted by key.
func EnvFile(env map[string]string) []byte {
	keys := make([]string, 0, len(env))
	for k := range env {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	for _, k := range keys {
		b.WriteString(k + "=" + env[k] + "\n")
	}
	return []byte(b.String())
}

// NewRelease queues a release with a fresh random rel8.
func (s Store) NewRelease(ctx context.Context, appID, trigger, ref string) (Release, error) {
	for {
		r := Release{Rel8: randHex(4), Status: "queued", Trigger: trigger, Ref: ref, CreatedAt: time.Now()}
		res, err := s.DB.ExecContext(ctx, `INSERT INTO releases (app_id, rel8, image, status, trigger, ref, created_at) VALUES (?, ?, '', 'queued', ?, ?, ?)`,
			appID, r.Rel8, trigger, ref, r.CreatedAt.Unix())
		if err != nil && strings.Contains(err.Error(), "UNIQUE") {
			continue // rel8 collision within this app: draw again
		}
		if err != nil {
			return Release{}, err
		}
		r.ID, _ = res.LastInsertId()
		return r, nil
	}
}

// Update writes a release's progress. Terminal states get finished_at.
func (s Store) Update(ctx context.Context, r Release) error {
	fin := int64(0)
	if Terminal(r.Status) {
		fin = time.Now().Unix()
	}
	_, err := s.DB.ExecContext(ctx, `UPDATE releases SET status = ?, sha = ?, message = ?, image = ?, error = ?, log_path = ?,
		finished_at = CASE WHEN ? > 0 THEN ? ELSE finished_at END WHERE id = ?`,
		r.Status, r.SHA, r.Message, r.Image, r.Error, r.LogPath, fin, fin, r.ID)
	return err
}

// PromoteLive makes release id the app's live release; the previous live
// one becomes superseded (or rolled_back if this was a rollback). One transaction,
// so there is always one live release.
func (s Store) PromoteLive(ctx context.Context, appID string, r Release) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	prevStatus := "superseded"
	if r.Trigger == "rollback" {
		prevStatus = "rolled_back"
	}
	if _, err := tx.ExecContext(ctx, `UPDATE releases SET status = ? WHERE app_id = ? AND status = 'live'`, prevStatus, appID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE releases SET status = 'live', image = ?, sha = ?, message = ?, finished_at = ? WHERE id = ?`,
		r.Image, r.SHA, r.Message, time.Now().Unix(), r.ID); err != nil {
		return err
	}
	return tx.Commit()
}

// RollbackTarget finds a previous release with an image to roll back to.
// If targetRel8 is empty, it finds the most recent non-live release with an image.
func (s Store) RollbackTarget(ctx context.Context, appID, targetRel8 string) (Release, error) {
	if targetRel8 != "" {
		r, err := s.Release(ctx, appID, targetRel8)
		if err != nil {
			return Release{}, err
		}
		if r.Image == "" {
			return Release{}, fmt.Errorf("release %s has no built image", targetRel8)
		}
		if r.Status == "live" {
			return Release{}, fmt.Errorf("release %s is already live", targetRel8)
		}
		return r, nil
	}
	rs, err := s.Releases(ctx, appID, 20)
	if err != nil {
		return Release{}, err
	}
	for _, r := range rs {
		if r.Status != "live" && r.Image != "" {
			return r, nil
		}
	}
	return Release{}, errors.New("no previous release with an image to roll back to")
}

// Releases returns an app's newest releases first.
func (s Store) Releases(ctx context.Context, appID string, limit int) ([]Release, error) {
	return s.releases(ctx, `WHERE app_id = ? ORDER BY id DESC LIMIT ?`, appID, limit)
}

// Release finds one release of an app by rel8.
func (s Store) Release(ctx context.Context, appID, rel8 string) (Release, error) {
	rs, err := s.releases(ctx, `WHERE app_id = ? AND rel8 = ?`, appID, rel8)
	if err != nil || len(rs) == 0 {
		return Release{}, errors.Join(errors.New("no such release"), err)
	}
	return rs[0], nil
}

// Unfinished returns releases that a crashed daemon left in a non-terminal state.
func (s Store) Unfinished(ctx context.Context) ([]Release, error) {
	return s.releases(ctx, `WHERE status IN ('queued', 'building', 'starting', 'healthy')`)
}

func (s Store) releases(ctx context.Context, where string, args ...any) ([]Release, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT id, rel8, status, trigger, ref, sha, message, image, error, log_path, created_at, finished_at FROM releases `+where, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Release
	for rows.Next() {
		var r Release
		var c, f int64
		if err := rows.Scan(&r.ID, &r.Rel8, &r.Status, &r.Trigger, &r.Ref, &r.SHA, &r.Message, &r.Image, &r.Error, &r.LogPath, &c, &f); err != nil {
			return nil, err
		}
		r.CreatedAt = time.Unix(c, 0)
		if f > 0 {
			r.FinishedAt = time.Unix(f, 0)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// RecordDelivery stores a webhook delivery id; dup is true if it was seen before.
func (s Store) RecordDelivery(ctx context.Context, delivery, appID, result string) (dup bool, err error) {
	_, err = s.DB.ExecContext(ctx, `INSERT INTO webhook_deliveries (delivery_id, app_id, received_at, result) VALUES (?, ?, ?, ?)`,
		delivery, appID, time.Now().Unix(), result)
	if err != nil && strings.Contains(err.Error(), "UNIQUE") {
		return true, nil
	}
	return false, err
}

func (s Store) HookHost(ctx context.Context) (string, error) {
	var h string
	err := s.DB.QueryRowContext(ctx, `SELECT hook_host FROM settings WHERE id = 1`).Scan(&h)
	return h, err
}

func (s Store) SetHookHost(ctx context.Context, h string) error {
	if h != "" && !spec.ValidDomain(h) {
		return fmt.Errorf("hook host %q: not a valid lowercase hostname", h)
	}
	_, err := s.DB.ExecContext(ctx, `UPDATE settings SET hook_host = ? WHERE id = 1`, h)
	return err
}
