// Package crypto provides AES-256-GCM authenticated encryption at rest bound to column names,
// master key management, and constant-time HMAC verification.
package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const (
	// KeySize is 32 bytes for AES-256.
	KeySize = 32
	// NonceSize is 12 bytes for standard GCM.
	NonceSize = 12
)

var (
	ErrInvalidKeySize     = errors.New("invalid master key size: must be 32 bytes")
	ErrCiphertextTooShort = errors.New("ciphertext is too short")
	ErrAuthentication     = errors.New("decryption failed: authentication mismatch or corrupted ciphertext")
)

// GenerateRandomKey generates a cryptographically secure 32-byte random key.
func GenerateRandomKey() ([]byte, error) {
	key := make([]byte, KeySize)
	if _, err := io.ReadFull(rand.Reader, key); err != nil {
		return nil, fmt.Errorf("reading random bytes: %w", err)
	}
	return key, nil
}

// LoadOrCreateMasterKey reads the master key from the specified path or creates it
// with 0600 permissions if it does not exist.
func LoadOrCreateMasterKey(path string) ([]byte, error) {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		dir := filepath.Dir(path)
		if err := os.MkdirAll(dir, 0700); err != nil {
			return nil, fmt.Errorf("creating directory for master key: %w", err)
		}

		key, err := GenerateRandomKey()
		if err != nil {
			return nil, err
		}

		// Write key atomically with 0600 permissions
		tmpPath := fmt.Sprintf("%s.tmp.%d", path, os.Getpid())
		if err := os.WriteFile(tmpPath, key, 0600); err != nil {
			return nil, fmt.Errorf("writing temporary master key: %w", err)
		}
		if err := os.Rename(tmpPath, path); err != nil {
			_ = os.Remove(tmpPath)
			return nil, fmt.Errorf("moving master key to target path: %w", err)
		}
		return key, nil
	} else if err != nil {
		return nil, fmt.Errorf("checking master key file: %w", err)
	}

	key, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading master key file: %w", err)
	}
	if len(key) != KeySize {
		return nil, fmt.Errorf("%w: got %d bytes", ErrInvalidKeySize, len(key))
	}

	return key, nil
}

// Encrypt encrypts plaintext using AES-256-GCM, binding the ciphertext to the column name via AAD.
func Encrypt(key []byte, column string, plaintext []byte) (string, error) {
	if len(key) != KeySize {
		return "", ErrInvalidKeySize
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return "", fmt.Errorf("creating cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("creating gcm: %w", err)
	}

	nonce := make([]byte, NonceSize)
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("generating nonce: %w", err)
	}

	// Seal appends ciphertext and authentication tag to nonce, with column as additional data (AAD)
	sealed := gcm.Seal(nonce, nonce, plaintext, []byte(column))
	return base64.StdEncoding.EncodeToString(sealed), nil
}

// Decrypt decrypts a base64-encoded ciphertext using AES-256-GCM, verifying that it matches the column AAD.
func Decrypt(key []byte, column string, encodedCiphertext string) ([]byte, error) {
	if len(key) != KeySize {
		return nil, ErrInvalidKeySize
	}

	data, err := base64.StdEncoding.DecodeString(encodedCiphertext)
	if err != nil {
		return nil, fmt.Errorf("decoding base64 ciphertext: %w", err)
	}

	if len(data) < NonceSize+16 {
		return nil, ErrCiphertextTooShort
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("creating cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("creating gcm: %w", err)
	}

	nonce, ciphertext := data[:NonceSize], data[NonceSize:]
	plaintext, err := gcm.Open(nil, nonce, ciphertext, []byte(column))
	if err != nil {
		return nil, ErrAuthentication
	}

	return plaintext, nil
}

// VerifyHMACSHA256 performs constant-time verification of an HMAC-SHA256 signature (SEC-11).
// The expected signature may optionally contain the "sha256=" prefix.
func VerifyHMACSHA256(secret, payload []byte, signatureHeader string) bool {
	sigHex := strings.TrimPrefix(signatureHeader, "sha256=")
	sigBytes, err := hex.DecodeString(sigHex)
	if err != nil || len(sigBytes) != sha256.Size {
		return false
	}

	mac := hmac.New(sha256.New, secret)
	mac.Write(payload)
	expectedMAC := mac.Sum(nil)

	return subtle.ConstantTimeCompare(sigBytes, expectedMAC) == 1
}
