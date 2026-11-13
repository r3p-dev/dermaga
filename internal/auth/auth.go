// Package auth implements the panel's single-admin login (BRIEF §4.3):
// argon2id password, optional TOTP, login backoff and server-side sessions.
package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/r3p-dev/dermaga/internal/secrets"
)

const (
	IdleTimeout     = 12 * time.Hour
	AbsoluteTimeout = 7 * 24 * time.Hour
	MinPasswordLen  = 12

	freeFailures = 5 // failed logins before backoff starts
	maxBackoff   = 15 * time.Minute
	aadTOTP      = "settings.totp_secret"
)

var ErrInvalid = errors.New("invalid credentials")

// BlockedError means login is in backoff; no credential was checked.
type BlockedError struct{ RetryAfter time.Duration }

func (e *BlockedError) Error() string {
	return fmt.Sprintf("too many failed logins, retry in %s", e.RetryAfter.Round(time.Second))
}

type Service struct {
	db  *sql.DB
	box *secrets.Box
	now func() time.Time

	// mu serialises logins: with one admin there is no reason to run argon2
	// concurrently, and it caps login memory at one hash at a time.
	mu           sync.Mutex
	fails        int
	blockedUntil time.Time
	lastTOTPStep int64 // a TOTP code is accepted once
}

func New(db *sql.DB, box *secrets.Box) *Service {
	return &Service{db: db, box: box, now: time.Now}
}

// Login checks the password (and TOTP code when enabled) and returns a new
// session token. Backoff is global: the panel only has one user, and every
// request arrives from 127.0.0.1 through the SSH tunnel anyway.
func (s *Service) Login(password, code string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	if err := s.check(password, code, now); err != nil {
		return "", err
	}
	return s.createSession(now)
}

// Verify re-authenticates the admin for a sensitive action (revealing the
// restic password) without creating a session. Failures count against the same
// limiter and backoff as Login.
func (s *Service) Verify(password, code string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.check(password, code, s.now())
}

// check is the one place credentials are tested; callers hold mu.
func (s *Service) check(password, code string, now time.Time) error {
	if now.Before(s.blockedUntil) {
		return &BlockedError{s.blockedUntil.Sub(now)}
	}
	var hash, totpSealed string
	if err := s.db.QueryRow(`SELECT admin_password_hash, totp_secret FROM settings WHERE id = 1`).Scan(&hash, &totpSealed); err != nil {
		return err
	}
	ok := hash != "" && VerifyPassword(hash, password)
	if ok && totpSealed != "" {
		secret, err := s.box.Open(totpSealed, aadTOTP)
		if err != nil {
			return err
		}
		step, valid := checkTOTP(secret, code, now)
		ok = valid && step > s.lastTOTPStep
		if ok {
			s.lastTOTPStep = step
		}
	}
	if !ok {
		s.fails++
		if n := s.fails - freeFailures; n >= 0 {
			s.blockedUntil = now.Add(min(time.Second<<min(n, 10), maxBackoff))
		}
		return ErrInvalid
	}
	s.fails = 0
	return nil
}

func tokenHash(token string) []byte {
	h := sha256.Sum256([]byte(token))
	return h[:]
}

func (s *Service) createSession(now time.Time) (string, error) {
	b := make([]byte, 32)
	rand.Read(b)
	token := base64.RawURLEncoding.EncodeToString(b)
	t := now.Unix()
	if _, err := s.db.Exec(`DELETE FROM sessions WHERE created_at <= ? OR last_seen_at <= ?`,
		t-int64(AbsoluteTimeout.Seconds()), t-int64(IdleTimeout.Seconds())); err != nil {
		return "", err
	}
	if _, err := s.db.Exec(`INSERT INTO sessions (id_hash, created_at, last_seen_at) VALUES (?, ?, ?)`, tokenHash(token), t, t); err != nil {
		return "", err
	}
	return token, nil
}

// Session reports whether token is a live session and refreshes its idle timer.
// Any database error counts as "not logged in".
func (s *Service) Session(token string) bool {
	if token == "" {
		return false
	}
	h, now := tokenHash(token), s.now().Unix()
	var created, seen int64
	if err := s.db.QueryRow(`SELECT created_at, last_seen_at FROM sessions WHERE id_hash = ?`, h).Scan(&created, &seen); err != nil {
		return false
	}
	if now-created >= int64(AbsoluteTimeout.Seconds()) || now-seen >= int64(IdleTimeout.Seconds()) {
		s.db.Exec(`DELETE FROM sessions WHERE id_hash = ?`, h)
		return false
	}
	if now-seen >= 60 { // one write per minute is precise enough for a 12 h timeout
		s.db.Exec(`UPDATE sessions SET last_seen_at = ? WHERE id_hash = ?`, now, h)
	}
	return true
}

func (s *Service) Logout(token string) error {
	_, err := s.db.Exec(`DELETE FROM sessions WHERE id_hash = ?`, tokenHash(token))
	return err
}

// SetPassword replaces the admin password and ends every session.
func (s *Service) SetPassword(pw string) error {
	if len(pw) < MinPasswordLen || len(pw) > 1024 {
		return fmt.Errorf("password must be %d to 1024 characters", MinPasswordLen)
	}
	h := HashPassword(pw)
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := s.db.Exec(`UPDATE settings SET admin_password_hash = ? WHERE id = 1`, h); err != nil {
		return err
	}
	s.fails, s.blockedUntil = 0, time.Time{}
	_, err := s.db.Exec(`DELETE FROM sessions`)
	return err
}

// EnableTOTP stores secret after code proves the authenticator has it.
func (s *Service) EnableTOTP(secret, code string) error {
	if _, ok := checkTOTP(secret, code, s.now()); !ok {
		return errors.New("code does not match the secret (check the phone's clock)")
	}
	_, err := s.db.Exec(`UPDATE settings SET totp_secret = ? WHERE id = 1`, s.box.Seal(secret, aadTOTP))
	return err
}

func (s *Service) DisableTOTP() error {
	_, err := s.db.Exec(`UPDATE settings SET totp_secret = '' WHERE id = 1`)
	return err
}

// State reports whether a password and TOTP are configured.
func (s *Service) State() (passwordSet, totpEnabled bool, err error) {
	err = s.db.QueryRow(`SELECT admin_password_hash != '', totp_secret != '' FROM settings WHERE id = 1`).Scan(&passwordSet, &totpEnabled)
	return
}
