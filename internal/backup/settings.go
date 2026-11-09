package backup

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
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

// GetSettings retrieves backup settings, auto-generating and persisting a secure
// restic password if one has not yet been initialized.
func (m *Manager) GetSettings(ctx context.Context) (Settings, error) {
	var repo, encPass, s3Endpoint, s3Bucket, s3Region, s3Access, encSecret string
	err := m.DB.QueryRowContext(ctx, `SELECT
		restic_repository, restic_password, s3_endpoint, s3_bucket, s3_region, s3_access_key, s3_secret_key
		FROM settings WHERE id = 1`).Scan(
		&repo, &encPass, &s3Endpoint, &s3Bucket, &s3Region, &s3Access, &encSecret,
	)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return Settings{}, err
	}

	var pass, s3Secret string
	if encPass != "" && m.Box != nil {
		p, err := m.Box.Open(encPass, aadResticPass)
		if err == nil {
			pass = p
		}
	}

	// Auto-generate restic password if missing (BRIEF §3.8: generate restic password)
	if pass == "" && m.Box != nil {
		pass = randHex(32) // 32 random bytes = 64 hex chars
		sealed := m.Box.Seal(pass, aadResticPass)
		_, _ = m.DB.ExecContext(ctx, `UPDATE settings SET restic_password = ? WHERE id = 1`, sealed)
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
		Repository:  repo,
		Password:    pass,
		S3Endpoint:  s3Endpoint,
		S3Bucket:    s3Bucket,
		S3Region:    s3Region,
		S3AccessKey: s3Access,
		S3SecretKey: s3Secret,
		Configured:  configured,
	}, nil
}

// SaveSettings updates the repository and credentials settings.
func (m *Manager) SaveSettings(ctx context.Context, s Settings) error {
	var encPass, encSecret string
	if s.Password != "" && m.Box != nil {
		encPass = m.Box.Seal(s.Password, aadResticPass)
	}
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
		restic_password = CASE WHEN ? != '' THEN ? ELSE restic_password END,
		s3_endpoint = ?,
		s3_bucket = ?,
		s3_region = ?,
		s3_access_key = ?,
		s3_secret_key = CASE WHEN ? != '' THEN ? ELSE s3_secret_key END
		WHERE id = 1`,
		repo, encPass, encPass, s.S3Endpoint, s.S3Bucket, s.S3Region, s.S3AccessKey, encSecret, encSecret,
	)
	return err
}

// ResticEnv builds the environment slice required to run restic commands securely.
func (m *Manager) ResticEnv(ctx context.Context) ([]string, error) {
	st, err := m.GetSettings(ctx)
	if err != nil {
		return nil, err
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
