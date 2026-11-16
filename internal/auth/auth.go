// Package auth implements argon2id password hashing, session management with
// idle and absolute limits, and login rate limiting.
package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/argon2"

	"dermaga/internal/store"
)

const (
	// Argon2id parameters per RFC 9106 §4 (Second Recommended Option: t=3, m=64 MiB, p=4)
	argonTime    = 3
	argonMemory  = 64 * 1024 // 64 MB
	argonThreads = 4
	argonKeyLen  = 32
	saltLen      = 16

	// Session limits (ADM-3)
	IdleTimeout     = 12 * time.Hour
	AbsoluteTimeout = 7 * 24 * time.Hour

	// Session cookie name
	SessionCookieName = "dermaga_session"

	// Setting keys
	SettingAdminPassword = "admin_password_hash"
	SettingTOTPEnabled   = "admin_totp_enabled"
	SettingTOTPSecret    = "admin_totp_secret"
)

var (
	ErrInvalidPassword = errors.New("invalid password")
	ErrSessionExpired  = errors.New("session has expired")
	ErrRateLimited     = errors.New("too many failed login attempts, please try again later")
)

// HashPassword hashes a plaintext password using Argon2id.
func HashPassword(password string) (string, error) {
	salt := make([]byte, saltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("generating salt: %w", err)
	}

	hash := argon2.IDKey([]byte(password), salt, argonTime, argonMemory, argonThreads, argonKeyLen)

	b64Salt := base64.RawStdEncoding.EncodeToString(salt)
	b64Hash := base64.RawStdEncoding.EncodeToString(hash)

	// Format: $argon2id$v=19$m=65536,t=1,p=4$<salt>$<hash>
	encoded := fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, argonMemory, argonTime, argonThreads, b64Salt, b64Hash)

	return encoded, nil
}

// VerifyPassword checks if a plaintext password matches an encoded Argon2id hash.
func VerifyPassword(password, encodedHash string) (bool, error) {
	parts := strings.Split(encodedHash, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return false, errors.New("invalid argon2id hash format")
	}

	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil {
		return false, fmt.Errorf("parsing version: %w", err)
	}
	if version != argon2.Version {
		return false, fmt.Errorf("unsupported argon2 version: %d", version)
	}

	var memory uint32
	var time uint32
	var threads uint8
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &memory, &time, &threads); err != nil {
		return false, fmt.Errorf("parsing parameters: %w", err)
	}

	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return false, fmt.Errorf("decoding salt: %w", err)
	}

	expectedHash, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil {
		return false, fmt.Errorf("decoding hash: %w", err)
	}

	computedHash := argon2.IDKey([]byte(password), salt, time, memory, threads, uint32(len(expectedHash)))

	if subtle.ConstantTimeCompare(computedHash, expectedHash) == 1 {
		return true, nil
	}
	return false, nil
}

// GenerateSessionToken creates a cryptographically secure 32-byte session token.
func GenerateSessionToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("reading random bytes: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// HashSessionToken computes SHA-256 of the session token for storage (SEC-7).
func HashSessionToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return fmt.Sprintf("%x", sum[:])
}

// RateLimiter tracks failed login attempts and applies exponential backoff (ADM-2).
type RateLimiter struct {
	mu       sync.Mutex
	failures map[string]*failureRecord
}

type failureRecord struct {
	count       int
	blockedTill time.Time
}

// NewRateLimiter creates a RateLimiter.
func NewRateLimiter() *RateLimiter {
	return &RateLimiter{
		failures: make(map[string]*failureRecord),
	}
}

// Check verifies if the given identifier (e.g. client IP) is currently blocked.
func (rl *RateLimiter) Check(identifier string) (time.Duration, bool) {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	rec, ok := rl.failures[identifier]
	if !ok {
		return 0, true
	}

	now := time.Now()
	if now.Before(rec.blockedTill) {
		return rec.blockedTill.Sub(now), false
	}

	return 0, true
}

// RecordFailure records a failed login attempt and calculates backoff.
func (rl *RateLimiter) RecordFailure(identifier string) time.Duration {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	rec, ok := rl.failures[identifier]
	if !ok {
		rec = &failureRecord{}
		rl.failures[identifier] = rec
	}

	rec.count++

	// Progressive backoff:
	// 1-2 attempts: no delay
	// 3 attempts: 1 second
	// 4 attempts: 2 seconds
	// 5 attempts: 4 seconds
	// 6+ attempts: capped at 30 seconds
	var backoff time.Duration
	if rec.count >= 6 {
		backoff = 30 * time.Second
	} else if rec.count >= 3 {
		backoff = time.Duration(1<<(rec.count-3)) * time.Second
	}

	rec.blockedTill = time.Now().Add(backoff)
	return backoff
}

// RecordSuccess clears failure tracking on successful authentication.
func (rl *RateLimiter) RecordSuccess(identifier string) {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	delete(rl.failures, identifier)
}

// --- Admin Recovery Direct SQLite Fallback (SRV-2, ADM-6) ---

// SetAdminPassword sets the admin password in the database and revokes all active sessions.
func SetAdminPassword(ctx context.Context, s *store.Store, origin, newPassword string) error {
	hash, err := HashPassword(newPassword)
	if err != nil {
		return fmt.Errorf("hashing password: %w", err)
	}

	if err := s.SetSetting(ctx, SettingAdminPassword, hash); err != nil {
		return fmt.Errorf("saving password hash: %w", err)
	}

	// Revoke all existing sessions
	if err := s.DeleteAllSessions(ctx); err != nil {
		return fmt.Errorf("revoking sessions: %w", err)
	}

	_ = s.RecordAudit(ctx, origin, "server", "admin_set_password", "Admin password set/reset; all sessions revoked", nil, nil)
	return nil
}

// DisableAdminTOTP disables TOTP in the database.
func DisableAdminTOTP(ctx context.Context, s *store.Store, origin string) error {
	if err := s.SetSetting(ctx, SettingTOTPEnabled, "false"); err != nil {
		return fmt.Errorf("updating totp setting: %w", err)
	}

	_ = s.RecordAudit(ctx, origin, "server", "admin_totp_disable", "TOTP two-factor authentication disabled", nil, nil)
	return nil
}
