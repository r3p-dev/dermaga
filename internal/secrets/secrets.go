// Package secrets encrypts values at rest with AES-256-GCM under master.key
// (BRIEF §3.4). It protects the database file and its backups, nothing more.
package secrets

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

const prefix = "v1:"

var ErrDecrypt = errors.New("secrets: cannot decrypt (wrong key, wrong context or tampered value)")

// LoadOrCreateKey reads the 32-byte key at path, or creates it if it doesn't
// exist. An existing key is never overwritten: losing it loses every secret.
func LoadOrCreateKey(path string) ([]byte, error) {
	k, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return createKey(path)
	}
	if err != nil {
		return nil, err
	}
	fi, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if fi.Mode().Perm()&0o077 != 0 {
		return nil, fmt.Errorf("%s: mode %v, must be 0600", path, fi.Mode().Perm())
	}
	if len(k) != 32 {
		return nil, fmt.Errorf("%s: %d bytes, want 32", path, len(k))
	}
	return k, nil
}

func createKey(path string) ([]byte, error) {
	k := make([]byte, 32)
	rand.Read(k)
	f, err := os.CreateTemp(filepath.Dir(path), ".master.key-*") // created 0600
	if err != nil {
		return nil, err
	}
	defer os.Remove(f.Name())
	if _, err := f.Write(k); err != nil {
		f.Close()
		return nil, err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return nil, err
	}
	if err := f.Close(); err != nil {
		return nil, err
	}
	// link, not rename: fails instead of replacing a key another process just created
	if err := os.Link(f.Name(), path); err != nil {
		return nil, err
	}
	return k, nil
}

// Box seals and opens values. The aad names the value's context (for example
// "settings.totp_secret") so a ciphertext can't be moved to another column.
type Box struct{ aead cipher.AEAD }

func New(key []byte) (*Box, error) {
	if len(key) != 32 {
		return nil, errors.New("secrets: key must be 32 bytes")
	}
	b, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	g, err := cipher.NewGCM(b)
	if err != nil {
		return nil, err
	}
	return &Box{g}, nil
}

func (b *Box) Seal(plaintext, aad string) string {
	nonce := make([]byte, b.aead.NonceSize())
	rand.Read(nonce)
	return prefix + base64.StdEncoding.EncodeToString(b.aead.Seal(nonce, nonce, []byte(plaintext), []byte(aad)))
}

func (b *Box) Open(sealed, aad string) (string, error) {
	raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(sealed, prefix))
	if !strings.HasPrefix(sealed, prefix) || err != nil || len(raw) < b.aead.NonceSize() {
		return "", ErrDecrypt
	}
	n := b.aead.NonceSize()
	pt, err := b.aead.Open(nil, raw[:n], raw[n:], []byte(aad))
	if err != nil {
		return "", ErrDecrypt
	}
	return string(pt), nil
}
