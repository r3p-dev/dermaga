package secrets

import (
	"bytes"
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRoundTripTamperWrongKeyWrongContext(t *testing.T) {
	k1, k2 := bytes.Repeat([]byte{1}, 32), bytes.Repeat([]byte{2}, 32)
	b1, _ := New(k1)
	b2, _ := New(k2)

	s := b1.Seal("hunter2", "settings.totp_secret")
	if strings.Contains(s, "hunter2") || !strings.HasPrefix(s, "v1:") {
		t.Fatalf("bad ciphertext %q", s)
	}
	if s == b1.Seal("hunter2", "settings.totp_secret") {
		t.Fatal("nonce reused")
	}
	if got, err := b1.Open(s, "settings.totp_secret"); err != nil || got != "hunter2" {
		t.Fatalf("round trip: %q %v", got, err)
	}
	if _, err := b2.Open(s, "settings.totp_secret"); err != ErrDecrypt {
		t.Fatal("wrong key accepted")
	}
	if _, err := b1.Open(s, "env_vars.value"); err != ErrDecrypt {
		t.Fatal("wrong context accepted")
	}
	raw, _ := base64.StdEncoding.DecodeString(s[3:])
	raw[len(raw)-1] ^= 1
	if _, err := b1.Open("v1:"+base64.StdEncoding.EncodeToString(raw), "settings.totp_secret"); err != ErrDecrypt {
		t.Fatal("tampered ciphertext accepted")
	}
	for _, bad := range []string{"", "v1:", "v1:!!!", "v2:" + s[3:], s[3:]} {
		if _, err := b1.Open(bad, "settings.totp_secret"); err != ErrDecrypt {
			t.Errorf("accepted %q", bad)
		}
	}
	if _, err := New(k1[:16]); err == nil {
		t.Fatal("short key accepted")
	}
}

func TestLoadOrCreateKey(t *testing.T) {
	p := filepath.Join(t.TempDir(), "master.key")
	k, err := LoadOrCreateKey(p)
	if err != nil || len(k) != 32 {
		t.Fatal(err)
	}
	fi, _ := os.Stat(p)
	if fi.Mode().Perm() != 0o600 {
		t.Fatalf("mode %v", fi.Mode().Perm())
	}
	k2, err := LoadOrCreateKey(p)
	if err != nil || !bytes.Equal(k, k2) {
		t.Fatal("key changed on reload")
	}
	os.Chmod(p, 0o644)
	if _, err := LoadOrCreateKey(p); err == nil {
		t.Fatal("world-readable key accepted")
	}
	os.WriteFile(p, []byte("short"), 0o600)
	os.Chmod(p, 0o600)
	if _, err := LoadOrCreateKey(p); err == nil {
		t.Fatal("short key accepted")
	}
	if entries, _ := os.ReadDir(filepath.Dir(p)); len(entries) != 1 {
		t.Fatalf("temp files left: %v", entries)
	}
}
