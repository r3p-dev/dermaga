package auth_test

import (
	"strings"
	"testing"
	"time"

	"dermaga/internal/auth"
)

func TestTOTP_SecretURIAndQR_SETUP2(t *testing.T) {
	secret, err := auth.GenerateTOTPSecret()
	if err != nil {
		t.Fatalf("GenerateTOTPSecret failed: %v", err)
	}
	if len(secret) != 32 {
		t.Fatalf("expected 32-char base32 secret (20 bytes), got %d (%q)", len(secret), secret)
	}

	uri := auth.TOTPURI(secret, "dermaga", "admin")
	if !strings.HasPrefix(uri, "otpauth://totp/dermaga:admin?") {
		t.Fatalf("unexpected otpauth URI: %q", uri)
	}
	if !strings.Contains(uri, "secret="+secret) {
		t.Fatalf("expected URI to include secret, got %q", uri)
	}

	svg, err := auth.GenerateQRCodeSVG(uri)
	if err != nil {
		t.Fatalf("GenerateQRCodeSVG failed: %v", err)
	}
	if !strings.HasPrefix(svg, "<svg ") || !strings.Contains(svg, "<path ") {
		t.Fatalf("expected valid inline SVG, got %q", svg[:min(len(svg), 80)])
	}
}

func TestTOTP_VerifySkewAndReplay_ADM1(t *testing.T) {
	secret, err := auth.GenerateTOTPSecret()
	if err != nil {
		t.Fatalf("GenerateTOTPSecret failed: %v", err)
	}

	now := time.Unix(1_700_000_010, 0).UTC()
	currentStep := now.Unix() / 30

	codeCurrent, err := auth.ComputeTOTP(secret, currentStep)
	if err != nil {
		t.Fatalf("ComputeTOTP failed: %v", err)
	}
	codePrev, _ := auth.ComputeTOTP(secret, currentStep-1)
	codeNext, _ := auth.ComputeTOTP(secret, currentStep+1)
	codeFar, _ := auth.ComputeTOTP(secret, currentStep+2)

	// Current step accepted when lastUsedStep == -1
	step, err := auth.VerifyTOTPCode(secret, codeCurrent, now, -1)
	if err != nil || step != currentStep {
		t.Fatalf("expected current step %d accepted, got step=%d err=%v", currentStep, step, err)
	}

	// Replay of same step rejected
	if _, err := auth.VerifyTOTPCode(secret, codeCurrent, now, currentStep); err != auth.ErrTOTPCodeReused {
		t.Fatalf("expected ErrTOTPCodeReused, got %v", err)
	}

	// Previous step also rejected when currentStep was already used
	if _, err := auth.VerifyTOTPCode(secret, codePrev, now, currentStep); err != auth.ErrTOTPCodeReused {
		t.Fatalf("expected ErrTOTPCodeReused for older step, got %v", err)
	}

	// +/- 1 step skew accepted when not yet used
	if step, err := auth.VerifyTOTPCode(secret, prevOrNext(codePrev), now, -1); err != nil || step != currentStep-1 {
		t.Fatalf("expected -1 skew accepted, got step=%d err=%v", step, err)
	}
	if step, err := auth.VerifyTOTPCode(secret, codeNext, now, currentStep); err != nil || step != currentStep+1 {
		t.Fatalf("expected +1 skew accepted, got step=%d err=%v", step, err)
	}

	// +2 step outside skew rejected
	if _, err := auth.VerifyTOTPCode(secret, codeFar, now, -1); err != auth.ErrInvalidTOTPCode {
		t.Fatalf("expected ErrInvalidTOTPCode for +2 step, got %v", err)
	}

	// Non-numeric / short codes rejected
	for _, bad := range []string{"", "12345", "1234567", "abcdef"} {
		if _, err := auth.VerifyTOTPCode(secret, bad, now, -1); err != auth.ErrInvalidTOTPCode {
			t.Fatalf("expected ErrInvalidTOTPCode for %q, got %v", bad, err)
		}
	}
}

func prevOrNext(s string) string { return s }
