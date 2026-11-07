package auth

import (
	"bytes"
	"errors"
	_ "github.com/r3p-dev/dermaga/internal/golden" // accepts go test ./... -update
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/r3p-dev/dermaga/internal/secrets"
	"github.com/r3p-dev/dermaga/internal/store"
)

// RFC 6238 Appendix B, SHA-1 rows (8 digits, ASCII seed "12345678901234567890").
func TestHOTPMatchesRFC6238Vectors(t *testing.T) {
	key := []byte("12345678901234567890")
	for _, v := range []struct {
		t    int64
		code string
	}{{59, "94287082"}, {1111111109, "07081804"}, {1111111111, "14050471"},
		{1234567890, "89005924"}, {2000000000, "69279037"}, {20000000000, "65353130"}} {
		if got := hotp(key, uint64(v.t/30), 8); got != v.code {
			t.Errorf("T=%d: got %s want %s", v.t, got, v.code)
		}
	}
}

func TestPassword(t *testing.T) {
	h := HashPassword("correct horse battery")
	if !strings.HasPrefix(h, "$argon2id$v=19$m=19456,t=2,p=1$") {
		t.Fatalf("hash format %q", h)
	}
	if !VerifyPassword(h, "correct horse battery") || VerifyPassword(h, "correct horse batterY") {
		t.Fatal("verify")
	}
	if h == HashPassword("correct horse battery") {
		t.Fatal("salt not random")
	}
	for _, bad := range []string{"", "$argon2i$v=19$m=1,t=1,p=1$AA$AA", strings.Replace(h, "v=19", "v=16", 1), h + "$x"} {
		if VerifyPassword(bad, "correct horse battery") {
			t.Errorf("accepted %q", bad)
		}
	}
}

type fixture struct {
	*Service
	clock time.Time
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	box, _ := secrets.New(bytes.Repeat([]byte{7}, 32))
	f := &fixture{Service: New(db, box), clock: time.Unix(1_800_000_000, 0)}
	f.now = func() time.Time { return f.clock }
	return f
}

func TestLoginWithoutPasswordSetFails(t *testing.T) {
	f := newFixture(t)
	if _, err := f.Login("", ""); err != ErrInvalid {
		t.Fatal(err)
	}
}

func TestLoginBackoffAndReset(t *testing.T) {
	f := newFixture(t)
	if err := f.SetPassword("short"); err == nil {
		t.Fatal("short password accepted")
	}
	if err := f.SetPassword("a-long-enough-password"); err != nil {
		t.Fatal(err)
	}
	for i := range freeFailures {
		if _, err := f.Login("wrong", ""); err != ErrInvalid {
			t.Fatalf("attempt %d: %v", i, err)
		}
	}
	// 5th failure armed a 1 s block: even the right password is refused unchecked
	var be *BlockedError
	if _, err := f.Login("a-long-enough-password", ""); !errors.As(err, &be) || be.RetryAfter != time.Second {
		t.Fatalf("want 1s block, got %v", err)
	}
	f.clock = f.clock.Add(time.Second)
	f.Login("wrong", "") // 6th failure → 2 s
	if _, err := f.Login("a-long-enough-password", ""); !errors.As(err, &be) || be.RetryAfter != 2*time.Second {
		t.Fatalf("want 2s block, got %v", err)
	}
	f.fails = 40 // backoff is capped
	f.clock = f.clock.Add(time.Hour)
	f.Login("wrong", "")
	if _, err := f.Login("x", ""); !errors.As(err, &be) || be.RetryAfter != maxBackoff {
		t.Fatalf("want cap %v, got %v", maxBackoff, err)
	}
	f.clock = f.clock.Add(maxBackoff)
	tok, err := f.Login("a-long-enough-password", "")
	if err != nil || tok == "" || f.fails != 0 {
		t.Fatalf("success after block: %q %v fails=%d", tok, err, f.fails)
	}
}

func TestTOTPRequiredAndNotReplayable(t *testing.T) {
	f := newFixture(t)
	f.SetPassword("a-long-enough-password")
	if s := NewTOTPSecret(); len(s) != 32 {
		t.Fatalf("secret %q", s)
	}
	key := []byte("12345678901234567890")
	secret := b32.EncodeToString(key)
	code := func(at time.Time) string { return hotp(key, uint64(at.Unix()/30), 6) }

	// at this fixed clock, none of the three accepted steps yields 000000
	if err := f.EnableTOTP(secret, "000000"); err == nil {
		t.Fatal("wrong confirmation code accepted")
	}
	if err := f.EnableTOTP(secret, code(f.clock)); err != nil {
		t.Fatal(err)
	}
	if _, totp, _ := f.State(); !totp {
		t.Fatal("TOTP not stored")
	}
	if _, err := f.Login("a-long-enough-password", ""); err != ErrInvalid {
		t.Fatal("login without code accepted")
	}
	if _, err := f.Login("a-long-enough-password", code(f.clock)); err != nil {
		t.Fatal(err)
	}
	if _, err := f.Login("a-long-enough-password", code(f.clock)); err != ErrInvalid {
		t.Fatal("replayed code accepted")
	}
	f.clock = f.clock.Add(30 * time.Second)
	if _, err := f.Login("a-long-enough-password", code(f.clock.Add(30*time.Second))); err != nil {
		t.Fatal("next-step code (skew) rejected:", err)
	}
	if _, err := f.Login("a-long-enough-password", code(f.clock.Add(-5*time.Minute))); err != ErrInvalid {
		t.Fatal("stale code accepted")
	}
	f.DisableTOTP()
	if _, err := f.Login("a-long-enough-password", ""); err != nil {
		t.Fatal("TOTP still required after disable")
	}
}

func TestSessionTimeoutsLogoutAndPasswordChange(t *testing.T) {
	f := newFixture(t)
	f.SetPassword("a-long-enough-password")
	tok, _ := f.Login("a-long-enough-password", "")
	if !f.Session(tok) || f.Session("") || f.Session(tok+"x") {
		t.Fatal("basic session check")
	}

	// idle: activity every 11 h keeps it alive until the 7 d absolute limit
	start := f.clock
	for f.clock.Sub(start) < AbsoluteTimeout-11*time.Hour {
		f.clock = f.clock.Add(11 * time.Hour)
		if !f.Session(tok) {
			t.Fatalf("expired early after %v", f.clock.Sub(start))
		}
	}
	f.clock = start.Add(AbsoluteTimeout)
	if f.Session(tok) {
		t.Fatal("absolute timeout not enforced")
	}

	tok, _ = f.Login("a-long-enough-password", "")
	f.clock = f.clock.Add(IdleTimeout)
	if f.Session(tok) {
		t.Fatal("idle timeout not enforced")
	}

	tok, _ = f.Login("a-long-enough-password", "")
	f.Logout(tok)
	if f.Session(tok) {
		t.Fatal("session alive after logout")
	}
	var n int
	f.db.QueryRow(`SELECT count(*) FROM sessions`).Scan(&n)
	if n != 0 {
		t.Fatalf("%d session rows left", n)
	}

	tok, _ = f.Login("a-long-enough-password", "")
	f.SetPassword("another-long-password")
	if f.Session(tok) {
		t.Fatal("session survived password change")
	}
}
