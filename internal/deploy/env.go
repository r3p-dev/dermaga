// Package deploy orchestrates the single global build queue, first-deploy lifecycle,
// health probing, failure/cancellation cleanup, and app removal (DEP-3, DEP-5–DEP-9, APP-9).
package deploy

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"dermaga/internal/crypto"
)

// SealEnvMap encrypts an environment variable map bound to its SQLite column name (SEC-9).
func SealEnvMap(masterKey []byte, column string, vars map[string]string) (string, error) {
	if len(vars) == 0 {
		return "", nil
	}
	raw, err := json.Marshal(vars)
	if err != nil {
		return "", fmt.Errorf("marshaling %s: %w", column, err)
	}
	sealed, err := crypto.Encrypt(masterKey, column, raw)
	if err != nil {
		return "", fmt.Errorf("encrypting %s: %w", column, err)
	}
	return sealed, nil
}

// OpenEnvMap decrypts an environment variable map from its sealed column value (SEC-9).
func OpenEnvMap(masterKey []byte, column, sealed string) (map[string]string, error) {
	if sealed == "" {
		return map[string]string{}, nil
	}
	raw, err := crypto.Decrypt(masterKey, column, sealed)
	if err != nil {
		return nil, fmt.Errorf("decrypting %s: %w", column, err)
	}
	var vars map[string]string
	if err := json.Unmarshal(raw, &vars); err != nil {
		return nil, fmt.Errorf("decoding %s: %w", column, err)
	}
	if vars == nil {
		vars = map[string]string{}
	}
	return vars, nil
}

// ValidateEnvKey checks that an environment variable key is a valid POSIX/container identifier.
func ValidateEnvKey(k string) error {
	if k == "" {
		return fmt.Errorf("environment variable key cannot be empty")
	}
	for i := 0; i < len(k); i++ {
		c := k[i]
		isAlpha := (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || c == '_'
		isDigit := c >= '0' && c <= '9'
		if i == 0 && !isAlpha {
			return fmt.Errorf("environment variable key %q must start with a letter or underscore", k)
		}
		if !isAlpha && !isDigit {
			return fmt.Errorf("environment variable key %q contains invalid character %q", k, c)
		}
	}
	return nil
}

// ValidateEnvValue checks that an environment variable value contains no newlines or null bytes.
func ValidateEnvValue(k, v string) error {
	if strings.ContainsAny(v, "\r\n\x00") {
		return fmt.Errorf("environment variable %q cannot contain newlines or null bytes", k)
	}
	return nil
}

// FormatEnvFile serializes runtime environment variables for a Quadlet EnvironmentFile= (0600).
func FormatEnvFile(vars map[string]string) ([]byte, error) {
	keys := make([]string, 0, len(vars))
	for k := range vars {
		if err := ValidateEnvKey(k); err != nil {
			return nil, err
		}
		if err := ValidateEnvValue(k, vars[k]); err != nil {
			return nil, err
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var buf bytes.Buffer
	for _, k := range keys {
		buf.WriteString(k)
		buf.WriteByte('=')
		buf.WriteString(vars[k])
		buf.WriteByte('\n')
	}
	return buf.Bytes(), nil
}
