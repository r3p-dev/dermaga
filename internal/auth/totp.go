package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"crypto/subtle"
	"encoding/base32"
	"encoding/binary"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	qrcode "github.com/skip2/go-qrcode"
)

const (
	totpPeriod = 30
	totpDigits = 6
	totpSkew   = 1 // +/- 1 time step (30s)
)

var (
	ErrInvalidTOTPCode = errors.New("invalid TOTP code")
	ErrTOTPCodeReused  = errors.New("TOTP code already used")
)

// GenerateTOTPSecret generates a random 20-byte base32-encoded secret (RFC 6238).
func GenerateTOTPSecret() (string, error) {
	b := make([]byte, 20)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generating TOTP secret: %w", err)
	}
	return base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(b), nil
}

// TOTPURI builds an otpauth:// URI for authenticator apps.
func TOTPURI(secret, issuer, account string) string {
	label := url.PathEscape(issuer + ":" + account)
	q := url.Values{}
	q.Set("secret", secret)
	q.Set("issuer", issuer)
	q.Set("algorithm", "SHA1")
	q.Set("digits", strconv.Itoa(totpDigits))
	q.Set("period", strconv.Itoa(totpPeriod))
	return fmt.Sprintf("otpauth://totp/%s?%s", label, q.Encode())
}

// GenerateQRCodeSVG renders a QR code for the given payload as an inline SVG string.
func GenerateQRCodeSVG(payload string) (string, error) {
	qr, err := qrcode.New(payload, qrcode.Medium)
	if err != nil {
		return "", fmt.Errorf("generating QR code: %w", err)
	}
	qr.DisableBorder = false
	bitmap := qr.Bitmap()
	n := len(bitmap)
	if n == 0 {
		return "", errors.New("empty QR bitmap")
	}

	var path strings.Builder
	for y, row := range bitmap {
		for x, dark := range row {
			if dark {
				fmt.Fprintf(&path, "M%d,%dh1v1h-1z", x, y)
			}
		}
	}

	return fmt.Sprintf(
		`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 %d %d" width="200" height="200" role="img" aria-label="TOTP QR code" shape-rendering="crispEdges"><rect width="%d" height="%d" fill="#ffffff"/><path d="%s" fill="#1A1B36"/></svg>`,
		n, n, n, n, path.String(),
	), nil
}

// ComputeTOTP computes the 6-digit RFC 6238 code for a base32 secret at the given step.
func ComputeTOTP(secret string, step int64) (string, error) {
	normalized := strings.ToUpper(strings.TrimSpace(secret))
	key, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(normalized)
	if err != nil {
		return "", fmt.Errorf("decoding TOTP secret: %w", err)
	}

	var msg [8]byte
	binary.BigEndian.PutUint64(msg[:], uint64(step))

	mac := hmac.New(sha1.New, key)
	mac.Write(msg[:])
	sum := mac.Sum(nil)

	offset := sum[len(sum)-1] & 0x0f
	code := (int(sum[offset]&0x7f) << 24) |
		(int(sum[offset+1]) << 16) |
		(int(sum[offset+2]) << 8) |
		int(sum[offset+3])

	otp := code % 1000000
	return fmt.Sprintf("%06d", otp), nil
}

// VerifyTOTPCode validates a 6-digit TOTP code with +/-1 step skew and replay protection.
// lastUsedStep should be -1 when no code has been verified yet.
func VerifyTOTPCode(secret, code string, now time.Time, lastUsedStep int64) (int64, error) {
	code = strings.TrimSpace(code)
	if len(code) != totpDigits {
		return 0, ErrInvalidTOTPCode
	}
	for i := 0; i < len(code); i++ {
		if code[i] < '0' || code[i] > '9' {
			return 0, ErrInvalidTOTPCode
		}
	}

	currentStep := now.Unix() / totpPeriod
	for delta := -totpSkew; delta <= totpSkew; delta++ {
		step := currentStep + int64(delta)
		if step < 0 {
			continue
		}
		expected, err := ComputeTOTP(secret, step)
		if err != nil {
			return 0, err
		}
		if subtle.ConstantTimeCompare([]byte(code), []byte(expected)) == 1 {
			if lastUsedStep >= 0 && step <= lastUsedStep {
				return 0, ErrTOTPCodeReused
			}
			return step, nil
		}
	}

	return 0, ErrInvalidTOTPCode
}
