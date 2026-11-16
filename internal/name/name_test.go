package name_test

import (
	"testing"

	"dermaga/internal/name"
)

func TestValidate(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{"valid short", "app-1", false},
		{"valid web", "my-portfolio", false},
		{"valid alpha num", "a1", false},
		{"too short", "a", true},
		{"starts with digit", "1app", true},
		{"starts with hyphen", "-app", true},
		{"ends with hyphen", "app-", true},
		{"uppercase letter", "MyApp", true},
		{"special characters", "app$name", true},
		{"underscore", "app_name", true},
		{"reserved dermaga", "dermaga", true},
		{"reserved admin", "admin", true},
		{"reserved traefik", "traefik", true},
		// SEC-16–SEC-19: injection prevention
		{"SEC-16 newline injection", "app\nname", true},
		{"SEC-16 carriage return newline", "app\r\nname", true},
		{"SEC-16 percent character", "app%20name", true},
		{"SEC-16 percent format", "app%s", true},
		{"SEC-16 dollar expansion", "app$name", true},
		{"SEC-16 shell subshell command", "app$(whoami)", true},
		{"SEC-16 shell variable syntax", "app${HOME}", true},
		{"SEC-16 backtick command substitution", "app`id`", true},
		{"SEC-16 single quote", "app'name", true},
		{"SEC-16 double quote", "app\"name", true},
		{"SEC-16 path traversal dot dot", "../app", true},
		{"SEC-16 path slash", "app/name", true},
		{"SEC-16 semicolon shell separator", "app;rm -rf /", true},
		{"SEC-16 pipe operator", "app|cat", true},
		{"SEC-16 ampersand background", "app&echo", true},
		{"SEC-16 redirect operator", "app>out", true},
		{"SEC-16 null byte injection", "app\x00name", true},
		{"SEC-16 space character", "app name", true},
		{"SEC-16 tab character", "app\tname", true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := name.Validate(tc.input)
			if (err != nil) != tc.wantErr {
				t.Errorf("Validate(%q) error = %v, wantErr %v", tc.input, err, tc.wantErr)
			}
		})
	}
}

func TestPrefixes(t *testing.T) {
	if got := name.AppContainerName("web", 1); got != "app-web-r1" {
		t.Errorf("AppContainerName = %q, want %q", got, "app-web-r1")
	}
	if got := name.AppServiceName("web", 1); got != "app-web-r1.service" {
		t.Errorf("AppServiceName = %q, want %q", got, "app-web-r1.service")
	}
}
