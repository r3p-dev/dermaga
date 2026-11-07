package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"crypto/subtle"
	"encoding/base32"
	"encoding/binary"
	"fmt"
	"net/url"
	"time"

	"github.com/r3p-dev/dermaga/internal/name"
)

// TOTP per RFC 6238 with the parameters every authenticator app supports:
// SHA-1, 6 digits, 30 s steps.
const totpStep = 30

var b32 = base32.StdEncoding.WithPadding(base32.NoPadding)

// NewTOTPSecret returns a random 160-bit secret, base32 without padding.
func NewTOTPSecret() string {
	k := make([]byte, 20)
	rand.Read(k)
	return b32.EncodeToString(k)
}

// TOTPURI is the otpauth:// URI authenticator apps import.
func TOTPURI(secret, account string) string {
	v := url.Values{"secret": {secret}, "issuer": {name.Name}, "algorithm": {"SHA1"}, "digits": {"6"}, "period": {"30"}}
	return "otpauth://totp/" + url.PathEscape(name.Name+":"+account) + "?" + v.Encode()
}

// hotp is RFC 4226 §5.3.
func hotp(key []byte, counter uint64, digits int) string {
	m := hmac.New(sha1.New, key)
	binary.Write(m, binary.BigEndian, counter)
	sum := m.Sum(nil)
	off := sum[len(sum)-1] & 0x0f
	v := binary.BigEndian.Uint32(sum[off:]) & 0x7fffffff
	mod := uint32(1)
	for range digits {
		mod *= 10
	}
	return fmt.Sprintf("%0*d", digits, v%mod)
}

// checkTOTP accepts the current step and one step either side (clock skew) and
// returns the matching step so the caller can refuse a replayed code.
func checkTOTP(secret, code string, now time.Time) (int64, bool) {
	key, err := b32.DecodeString(secret)
	if err != nil || len(key) < 16 || len(code) != 6 {
		return 0, false
	}
	step := now.Unix() / totpStep
	for _, s := range []int64{step - 1, step, step + 1} {
		if subtle.ConstantTimeCompare([]byte(hotp(key, uint64(s), 6)), []byte(code)) == 1 {
			return s, true
		}
	}
	return 0, false
}
