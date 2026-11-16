package crypto_test

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"

	"dermaga/internal/crypto"
)

func TestMasterKeyLifecycle(t *testing.T) {
	tmpDir := t.TempDir()
	keyPath := filepath.Join(tmpDir, "master.key")

	key1, err := crypto.LoadOrCreateMasterKey(keyPath)
	if err != nil {
		t.Fatalf("failed to create master key: %v", err)
	}
	if len(key1) != crypto.KeySize {
		t.Fatalf("expected key length %d, got %d", crypto.KeySize, len(key1))
	}

	info, err := os.Stat(keyPath)
	if err != nil {
		t.Fatalf("failed to stat key file: %v", err)
	}
	// Check mode 0600
	if info.Mode().Perm() != 0600 {
		t.Errorf("expected file mode 0600, got %o", info.Mode().Perm())
	}

	// Reload existing key
	key2, err := crypto.LoadOrCreateMasterKey(keyPath)
	if err != nil {
		t.Fatalf("failed to load existing master key: %v", err)
	}
	if string(key1) != string(key2) {
		t.Errorf("reloaded key does not match original key")
	}
}

func TestEncryptDecrypt_RoundTrip(t *testing.T) {
	key, err := crypto.GenerateRandomKey()
	if err != nil {
		t.Fatalf("GenerateRandomKey failed: %v", err)
	}

	plaintext := []byte("SECRET_API_KEY=1234567890abcdef")
	column := "env_encrypted"

	ciphertext, err := crypto.Encrypt(key, column, plaintext)
	if err != nil {
		t.Fatalf("Encrypt failed: %v", err)
	}

	decrypted, err := crypto.Decrypt(key, column, ciphertext)
	if err != nil {
		t.Fatalf("Decrypt failed: %v", err)
	}

	if string(decrypted) != string(plaintext) {
		t.Errorf("decrypted %q != plaintext %q", string(decrypted), string(plaintext))
	}
}

func TestEncryptDecrypt_Tamper(t *testing.T) {
	key, err := crypto.GenerateRandomKey()
	if err != nil {
		t.Fatalf("GenerateRandomKey failed: %v", err)
	}

	plaintext := []byte("super-secret-data")
	column := "build_env_encrypted"

	ciphertext, err := crypto.Encrypt(key, column, plaintext)
	if err != nil {
		t.Fatalf("Encrypt failed: %v", err)
	}

	raw, err := base64.StdEncoding.DecodeString(ciphertext)
	if err != nil {
		t.Fatalf("DecodeString failed: %v", err)
	}

	// Tamper with one byte in the ciphertext payload
	raw[len(raw)-1] ^= 0xff
	tampered := base64.StdEncoding.EncodeToString(raw)

	_, err = crypto.Decrypt(key, column, tampered)
	if err == nil {
		t.Errorf("expected decryption error on tampered ciphertext, got nil")
	}
}

func TestEncryptDecrypt_WrongKey(t *testing.T) {
	key1, _ := crypto.GenerateRandomKey()
	key2, _ := crypto.GenerateRandomKey()

	plaintext := []byte("secret")
	column := "secret_column"

	ciphertext, err := crypto.Encrypt(key1, column, plaintext)
	if err != nil {
		t.Fatalf("Encrypt failed: %v", err)
	}

	_, err = crypto.Decrypt(key2, column, ciphertext)
	if err == nil {
		t.Errorf("expected decryption error with wrong key, got nil")
	}
}

func TestEncryptDecrypt_WrongColumn(t *testing.T) {
	key, _ := crypto.GenerateRandomKey()
	plaintext := []byte("secret")

	ciphertext, err := crypto.Encrypt(key, "col_a", plaintext)
	if err != nil {
		t.Fatalf("Encrypt failed: %v", err)
	}

	// Decrypting with wrong column must fail because column is bound via AAD
	_, err = crypto.Decrypt(key, "col_b", ciphertext)
	if err == nil {
		t.Errorf("expected decryption error with wrong column AAD binding, got nil")
	}
}

func TestVerifyHMACSHA256(t *testing.T) {
	secret := []byte("my-webhook-secret-32-bytes-long!")
	payload := []byte(`{"ref":"refs/heads/main","after":"12345"}`)

	mac := hmac.New(sha256.New, secret)
	mac.Write(payload)
	validHex := hex.EncodeToString(mac.Sum(nil))

	if !crypto.VerifyHMACSHA256(secret, payload, validHex) {
		t.Errorf("expected valid signature without prefix to pass")
	}
	if !crypto.VerifyHMACSHA256(secret, payload, "sha256="+validHex) {
		t.Errorf("expected valid signature with sha256= prefix to pass")
	}

	// Negative tests
	if crypto.VerifyHMACSHA256(secret, payload, "sha256=0000000000000000000000000000000000000000000000000000000000000000") {
		t.Errorf("expected invalid signature to fail")
	}
	if crypto.VerifyHMACSHA256([]byte("wrong-secret"), payload, validHex) {
		t.Errorf("expected wrong secret to fail")
	}
	if crypto.VerifyHMACSHA256(secret, []byte("tampered payload"), validHex) {
		t.Errorf("expected tampered payload to fail")
	}
}
