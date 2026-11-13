package backup

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

const (
	aadResticPass = "settings.restic_password"
	aadS3Secret   = "settings.s3_secret_key"
)

func randHex(n int) string {
	b := make([]byte, n)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// Password states (ARCHITECTURE v2 D62).
const (
	PasswordNone        = "none"        // no password yet: backups fail until one is generated
	PasswordUnconfirmed = "unconfirmed" // set, but the owner has not confirmed an offline copy
	PasswordConfirmed   = "confirmed"
)

// ErrNoPassword is returned by every restic operation while no password exists.
// The text is what the owner sees in the CLI, the journal and the notification,
// so it says what to do.
var ErrNoPassword = errors.New("set up the backup password first: open Backups in the panel and choose \"Generate password\", or run `dermaga backup password init`")

// ErrPasswordExists is returned by GeneratePassword: a second password would
// lock the existing repository.
var ErrPasswordExists = errors.New("a restic password already exists; reveal it instead")

// RecoveryStatement is shown wherever the password is shown (UI and CLI).
const RecoveryStatement = "To restore this server you need two things: the master key (`dermaga keys export`) and this restic password. " +
	"Without the restic password the backups cannot be read by anyone. " +
	"Without the master key the restored database cannot decrypt app secrets, environment variables and database passwords. " +
	"Store both offline, in different places from the server."

// GetSettings reads the backup settings. It never writes: the password is
// created only by GeneratePassword.
func (m *Manager) GetSettings(ctx context.Context) (Settings, error) {
	var repo, encPass, s3Endpoint, s3Bucket, s3Region, s3Access, encSecret string
	var confirmedAt int64
	err := m.DB.QueryRowContext(ctx, `SELECT
		restic_repository, restic_password, restic_password_confirmed_at, s3_endpoint, s3_bucket, s3_region, s3_access_key, s3_secret_key
		FROM settings WHERE id = 1`).Scan(
		&repo, &encPass, &confirmedAt, &s3Endpoint, &s3Bucket, &s3Region, &s3Access, &encSecret,
	)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return Settings{}, err
	}

	var pass, s3Secret string
	state := PasswordNone
	if encPass != "" && m.Box != nil {
		if pass, err = m.Box.Open(encPass, aadResticPass); err != nil {
			return Settings{}, fmt.Errorf("decrypting the restic password (wrong master key?): %w", err)
		}
		state = PasswordUnconfirmed
		if confirmedAt > 0 {
			state = PasswordConfirmed
		}
	}

	if encSecret != "" && m.Box != nil {
		s, err := m.Box.Open(encSecret, aadS3Secret)
		if err == nil {
			s3Secret = s
		}
	}

	if repo == "" {
		if s3Bucket != "" && s3Endpoint != "" {
			ep := strings.TrimPrefix(s3Endpoint, "https://")
			ep = strings.TrimPrefix(ep, "http://")
			repo = fmt.Sprintf("s3:https://%s/%s", ep, s3Bucket)
		} else {
			repo = m.Paths.ResticDir()
		}
	}

	configured := encPass != "" || repo != m.Paths.ResticDir()

	return Settings{
		Repository:    repo,
		Password:      pass,
		PasswordState: state,
		S3Endpoint:    s3Endpoint,
		S3Bucket:      s3Bucket,
		S3Region:      s3Region,
		S3AccessKey:   s3Access,
		S3SecretKey:   s3Secret,
		Configured:    configured,
	}, nil
}

// GeneratePassword creates the restic password once and returns it for its
// single display. It fails with ErrPasswordExists when one is already stored.
func (m *Manager) GeneratePassword(ctx context.Context) (string, error) {
	pass := randHex(32) // 32 random bytes = 64 hex chars
	res, err := m.DB.ExecContext(ctx, `UPDATE settings SET restic_password = ?, restic_password_confirmed_at = 0
		WHERE id = 1 AND restic_password = ''`, m.Box.Seal(pass, aadResticPass))
	if err != nil {
		return "", err
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return "", ErrPasswordExists
	}
	return pass, nil
}

// RevealPassword returns the stored password; the caller authenticates and audits.
func (m *Manager) RevealPassword(ctx context.Context) (string, error) {
	st, err := m.GetSettings(ctx)
	if err != nil {
		return "", err
	}
	if st.Password == "" {
		return "", ErrNoPassword
	}
	return st.Password, nil
}

// ConfirmPassword records that the owner stored the password offline.
func (m *Manager) ConfirmPassword(ctx context.Context) error {
	res, err := m.DB.ExecContext(ctx, `UPDATE settings SET restic_password_confirmed_at = ?
		WHERE id = 1 AND restic_password != ''`, time.Now().Unix())
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return ErrNoPassword
	}
	return nil
}

// SaveSettings updates the repository and S3 settings. It never touches the
// restic password: replacing it would lock the existing repository.
func (m *Manager) SaveSettings(ctx context.Context, s Settings) error {
	var encSecret string
	if s.S3SecretKey != "" && m.Box != nil {
		encSecret = m.Box.Seal(s.S3SecretKey, aadS3Secret)
	}

	repo := s.Repository
	if repo == "" && s.S3Bucket != "" && s.S3Endpoint != "" {
		ep := strings.TrimPrefix(s.S3Endpoint, "https://")
		ep = strings.TrimPrefix(ep, "http://")
		repo = fmt.Sprintf("s3:https://%s/%s", ep, s.S3Bucket)
	}

	_, err := m.DB.ExecContext(ctx, `UPDATE settings SET
		restic_repository = ?,
		s3_endpoint = ?,
		s3_bucket = ?,
		s3_region = ?,
		s3_access_key = ?,
		s3_secret_key = CASE WHEN ? != '' THEN ? ELSE s3_secret_key END
		WHERE id = 1`,
		repo, s.S3Endpoint, s.S3Bucket, s.S3Region, s.S3AccessKey, encSecret, encSecret,
	)
	return err
}

// ResticEnv builds the environment slice required to run restic commands securely.
func (m *Manager) ResticEnv(ctx context.Context) ([]string, error) {
	st, err := m.GetSettings(ctx)
	if err != nil {
		return nil, err
	}
	if st.Password == "" {
		return nil, ErrNoPassword
	}
	env := []string{
		"RESTIC_REPOSITORY=" + st.Repository,
		"RESTIC_PASSWORD=" + st.Password,
	}
	if st.S3AccessKey != "" {
		env = append(env, "AWS_ACCESS_KEY_ID="+st.S3AccessKey)
	}
	if st.S3SecretKey != "" {
		env = append(env, "AWS_SECRET_ACCESS_KEY="+st.S3SecretKey)
	}
	if st.S3Region != "" {
		env = append(env, "AWS_DEFAULT_REGION="+st.S3Region)
	}
	return env, nil
}
