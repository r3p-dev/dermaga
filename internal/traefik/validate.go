package traefik

import (
	"fmt"
	"net"
	"regexp"
	"strings"

	"dermaga/internal/name"
	"golang.org/x/crypto/bcrypt"
)

var (
	// domainRegex validates RFC 1123 DNS hostname syntax
	domainRegex = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?(\.[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?)*$`)

	// headerNameRegex validates HTTP header field names per RFC 7230
	headerNameRegex = regexp.MustCompile(`^[A-Za-z0-9-]+$`)

	// basicAuthUserRegex validates usernames for HTTP basic auth
	basicAuthUserRegex = regexp.MustCompile(`^[a-zA-Z0-9_.-]+$`)

	// tokenRegex validates switch verification tokens
	tokenRegex = regexp.MustCompile(`^[a-zA-Z0-9_\-]+$`)
)

// ValidateAppSpec validates all fields of an AppSpec against SEC-16–SEC-19 injection rules.
func ValidateAppSpec(spec AppSpec) error {
	if err := name.Validate(spec.AppName); err != nil {
		return fmt.Errorf("invalid app name: %w", err)
	}
	if spec.ReleaseNumber <= 0 {
		return fmt.Errorf("release number must be positive: %d", spec.ReleaseNumber)
	}
	if spec.Port <= 0 || spec.Port > 65535 {
		return fmt.Errorf("port must be in range [1, 65535]: %d", spec.Port)
	}

	if len(spec.Domains) == 0 {
		return fmt.Errorf("at least one domain is required")
	}

	primaryCount := 0
	for _, d := range spec.Domains {
		if err := ValidateDomain(d.Domain); err != nil {
			return fmt.Errorf("invalid domain %q: %w", d.Domain, err)
		}
		if d.IsPrimary {
			primaryCount++
		}
		if d.RedirectTo != "" {
			if err := ValidateDomain(d.RedirectTo); err != nil {
				return fmt.Errorf("invalid redirect target domain %q: %w", d.RedirectTo, err)
			}
		}
		if d.PathPrefix != "" {
			if !strings.HasPrefix(d.PathPrefix, "/") {
				return fmt.Errorf("path prefix must start with '/': %q", d.PathPrefix)
			}
			if strings.ContainsAny(d.PathPrefix, " \t\r\n\x00`'\"$;|&<>") {
				return fmt.Errorf("path prefix %q contains illegal characters", d.PathPrefix)
			}
		}
	}

	if primaryCount == 0 {
		return fmt.Errorf("app spec must contain exactly one primary domain, found 0")
	}
	if primaryCount > 1 {
		return fmt.Errorf("app spec must contain at most one primary domain, found %d", primaryCount)
	}

	for k, v := range spec.Headers.CustomResponseHeaders {
		if !headerNameRegex.MatchString(k) {
			return fmt.Errorf("invalid header name %q", k)
		}
		if strings.ContainsAny(v, "\r\n\x00") {
			return fmt.Errorf("header %q value contains illegal newline or null byte", k)
		}
	}
	if spec.Headers.PermissionsPolicy != "" && strings.ContainsAny(spec.Headers.PermissionsPolicy, "\r\n\x00") {
		return fmt.Errorf("permissions policy contains illegal newline or null byte")
	}

	for _, u := range spec.Middleware.BasicAuth {
		if !basicAuthUserRegex.MatchString(u.Username) {
			return fmt.Errorf("invalid basic auth username %q", u.Username)
		}
		if u.Hash == "" || strings.ContainsAny(u.Hash, "\r\n\x00 \t") {
			return fmt.Errorf("invalid password hash for user %q", u.Username)
		}
	}

	for _, cidr := range spec.Middleware.IPAllowList {
		if _, _, err := net.ParseCIDR(cidr); err != nil {
			if ip := net.ParseIP(cidr); ip == nil {
				return fmt.Errorf("invalid IP or CIDR in allowlist: %q", cidr)
			}
		}
	}

	if spec.Switch != nil {
		if spec.Switch.CandidateReleaseNumber <= 0 {
			return fmt.Errorf("switch candidate release number must be positive: %d", spec.Switch.CandidateReleaseNumber)
		}
		if spec.Switch.CandidatePort <= 0 || spec.Switch.CandidatePort > 65535 {
			return fmt.Errorf("switch candidate port out of range: %d", spec.Switch.CandidatePort)
		}
		if !tokenRegex.MatchString(spec.Switch.VerifyToken) {
			return fmt.Errorf("invalid switch verification token %q", spec.Switch.VerifyToken)
		}
	}

	return nil
}

// ValidateDomain validates domain string syntax according to RFC 1123.
func ValidateDomain(domain string) error {
	domain = strings.ToLower(domain)
	if len(domain) == 0 || len(domain) > 253 {
		return fmt.Errorf("domain length must be between 1 and 253 characters")
	}
	if !domainRegex.MatchString(domain) {
		return fmt.Errorf("domain %q contains invalid characters or structure", domain)
	}
	if domain == "localhost" {
		return fmt.Errorf("cannot use localhost as an app domain")
	}
	return nil
}

// HashBasicAuthPassword generates a bcrypt hash for a plaintext password.
func HashBasicAuthPassword(password string) (string, error) {
	bytes, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", fmt.Errorf("hashing password: %w", err)
	}
	return string(bytes), nil
}
