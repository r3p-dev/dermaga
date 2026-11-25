package github_test

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"dermaga/internal/github"
)

func testRSAKeyPEM(t *testing.T) (*rsa.PrivateKey, []byte) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generating RSA key: %v", err)
	}
	block := &pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(key),
	}
	return key, pem.EncodeToMemory(block)
}

func TestBuildManifest_GHA1_GHA2(t *testing.T) {
	m := github.BuildManifest("dermaga-test", "http://localhost:7070/", "hooks.example.com")
	if m.Public {
		t.Errorf("expected private GitHub App (public=false)")
	}
	if m.HookAttributes.URL != "https://hooks.example.com/hooks/github-app" || !m.HookAttributes.Active {
		t.Errorf("unexpected hook_attributes: %+v", m.HookAttributes)
	}
	if m.RedirectURL != "http://localhost:7070/settings/github/callback" {
		t.Errorf("unexpected redirect_url: %q", m.RedirectURL)
	}
	if m.SetupURL != "http://localhost:7070/settings/github/install-callback" {
		t.Errorf("unexpected setup_url: %q", m.SetupURL)
	}
	if m.DefaultPermissions["contents"] != "read" || m.DefaultPermissions["metadata"] != "read" || len(m.DefaultPermissions) != 2 {
		t.Errorf("expected minimum permissions contents:read, metadata:read; got %+v", m.DefaultPermissions)
	}
	if len(m.DefaultEvents) != 1 || m.DefaultEvents[0] != "push" {
		t.Errorf("expected default_events [push], got %+v", m.DefaultEvents)
	}
}

func TestSignAppJWT_RS256(t *testing.T) {
	priv, pemBytes := testRSAKeyPEM(t)
	now := time.Unix(1700000000, 0).UTC()

	tok, err := github.SignAppJWT(12345, pemBytes, now)
	if err != nil {
		t.Fatalf("SignAppJWT failed: %v", err)
	}
	parts := strings.Split(tok, ".")
	if len(parts) != 3 {
		t.Fatalf("expected 3 JWT parts, got %d", len(parts))
	}

	sig, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		t.Fatalf("decoding signature: %v", err)
	}
	digest := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	if err := rsa.VerifyPKCS1v15(&priv.PublicKey, crypto.SHA256, digest[:], sig); err != nil {
		t.Fatalf("RS256 signature verification failed: %v", err)
	}

	claimsRaw, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		t.Fatalf("decoding claims: %v", err)
	}
	var claims struct {
		IAT int64  `json:"iat"`
		EXP int64  `json:"exp"`
		ISS string `json:"iss"`
	}
	if err := json.Unmarshal(claimsRaw, &claims); err != nil {
		t.Fatalf("unmarshaling claims: %v", err)
	}
	if claims.ISS != "12345" || claims.EXP <= claims.IAT {
		t.Errorf("unexpected claims: %+v", claims)
	}
}

func TestFakeGitHub_ManifestWebhookInstallReposAndToken_GHA1_GHA5_DEC09(t *testing.T) {
	_, pemBytes := testRSAKeyPEM(t)
	var apiVersions []string
	var patchedHook struct {
		URL         string `json:"url"`
		ContentType string `json:"content_type"`
		Secret      string `json:"secret"`
	}
	var tokenReqRepos []string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		apiVersions = append(apiVersions, r.Header.Get("X-GitHub-Api-Version"))
		w.Header().Set("Content-Type", "application/json")

		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/app-manifests/code-123/conversions":
			_ = json.NewEncoder(w).Encode(github.ManifestConversion{
				ID:           999,
				Slug:         "dermaga-vm",
				Name:         "Dermaga VM",
				HTMLURL:      "https://github.com/apps/dermaga-vm",
				PEM:          string(pemBytes),
				ClientID:     "Iv1.abc",
				ClientSecret: "cs-secret",
			})
		case r.Method == http.MethodPatch && r.URL.Path == "/app/hook/config":
			if !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") {
				http.Error(w, "missing bearer JWT", http.StatusUnauthorized)
				return
			}
			_ = json.NewDecoder(r.Body).Decode(&patchedHook)
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{}`))
		case r.Method == http.MethodGet && r.URL.Path == "/app/installations":
			_ = json.NewEncoder(w).Encode([]github.Installation{
				{
					ID:                  555,
					Account:             github.InstallationAccount{Login: "r3p-dev", Type: "User"},
					RepositorySelection: "selected",
					HTMLURL:             "https://github.com/settings/installations/555",
				},
			})
		case r.Method == http.MethodPost && r.URL.Path == "/app/installations/555/access_tokens":
			var body struct {
				Repositories []string `json:"repositories"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			tokenReqRepos = body.Repositories
			_ = json.NewEncoder(w).Encode(map[string]any{
				"token":      "ghs_ephemeral_token",
				"expires_at": time.Now().Add(time.Hour).UTC().Format(time.RFC3339),
			})
		case r.Method == http.MethodGet && r.URL.Path == "/installation/repositories":
			if r.Header.Get("Authorization") != "token ghs_ephemeral_token" {
				http.Error(w, "bad token", http.StatusUnauthorized)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"total_count": 1,
				"repositories": []github.Repository{
					{
						ID:            777,
						Name:          "dermaga-test-app",
						FullName:      "r3p-dev/dermaga-test-app",
						Owner:         github.RepositoryOwner{Login: "r3p-dev"},
						Private:       true,
						DefaultBranch: "main",
						HTMLURL:       "https://github.com/r3p-dev/dermaga-test-app",
					},
				},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	client := &github.Client{
		APIBaseURL: srv.URL,
		WebBaseURL: srv.URL,
		HTTPClient: srv.Client(),
	}

	ctx := context.Background()
	conv, err := client.ExchangeManifestCode(ctx, "code-123")
	if err != nil {
		t.Fatalf("ExchangeManifestCode failed: %v", err)
	}
	if conv.ID != 999 || conv.Slug != "dermaga-vm" {
		t.Fatalf("unexpected conversion: %+v", conv)
	}

	if err := client.ConfigureWebhook(ctx, conv.ID, []byte(conv.PEM), "https://hooks.example.com/hooks/github-app", "whsec-32b"); err != nil {
		t.Fatalf("ConfigureWebhook failed: %v", err)
	}
	if patchedHook.ContentType != "json" || patchedHook.Secret != "whsec-32b" || patchedHook.URL != "https://hooks.example.com/hooks/github-app" {
		t.Fatalf("unexpected patched hook config: %+v", patchedHook)
	}

	installs, err := client.ListInstallations(ctx, conv.ID, []byte(conv.PEM))
	if err != nil || len(installs) != 1 || installs[0].ID != 555 {
		t.Fatalf("unexpected installations: %+v (err=%v)", installs, err)
	}

	repos, err := client.ListInstallationRepositories(ctx, conv.ID, []byte(conv.PEM), 555)
	if err != nil || len(repos) != 1 || repos[0].FullName != "r3p-dev/dermaga-test-app" {
		t.Fatalf("unexpected repos: %+v (err=%v)", repos, err)
	}

	tok, _, err := client.CreateInstallationToken(ctx, conv.ID, []byte(conv.PEM), 555, []string{"dermaga-test-app"})
	if err != nil || tok != "ghs_ephemeral_token" {
		t.Fatalf("unexpected scoped token: %q (err=%v)", tok, err)
	}
	if len(tokenReqRepos) != 1 || tokenReqRepos[0] != "dermaga-test-app" {
		t.Errorf("expected repo-scoped token request for dermaga-test-app, got %+v", tokenReqRepos)
	}

	for i, v := range apiVersions {
		if v != github.APIVersion {
			t.Errorf("request %d X-GitHub-Api-Version = %q, want %q (DEC-09)", i, v, github.APIVersion)
		}
	}
}
