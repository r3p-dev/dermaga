// Package github implements the GitHub App manifest flow, RS256 App JWT signing,
// webhook configuration, installation/repository listing, and repo-scoped installation tokens.
package github

import (
	"bytes"
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	// APIVersion pins the GitHub REST API calendar version (DEC-09).
	APIVersion = "2026-03-10"

	DefaultAPIBaseURL = "https://api.github.com"
	DefaultWebBaseURL = "https://github.com"
)

// Client communicates with the GitHub REST API for the server's GitHub App.
type Client struct {
	APIBaseURL string
	WebBaseURL string
	HTTPClient *http.Client
	Now        func() time.Time
}

// NewClient returns a Client configured for github.com with safe HTTP timeouts.
func NewClient() *Client {
	return &Client{
		APIBaseURL: DefaultAPIBaseURL,
		WebBaseURL: DefaultWebBaseURL,
		HTTPClient: &http.Client{Timeout: 30 * time.Second},
		Now:        time.Now,
	}
}

func (c *Client) apiBase() string {
	if c.APIBaseURL != "" {
		return strings.TrimRight(c.APIBaseURL, "/")
	}
	return DefaultAPIBaseURL
}

func (c *Client) webBase() string {
	if c.WebBaseURL != "" {
		return strings.TrimRight(c.WebBaseURL, "/")
	}
	return DefaultWebBaseURL
}

func (c *Client) httpClient() *http.Client {
	if c.HTTPClient != nil {
		return c.HTTPClient
	}
	return &http.Client{Timeout: 30 * time.Second}
}

func (c *Client) now() time.Time {
	if c.Now != nil {
		return c.Now()
	}
	return time.Now()
}

// Manifest is the JSON payload posted to GitHub's App Manifest flow (GHA-1, GHA-2).
type Manifest struct {
	Name               string            `json:"name"`
	URL                string            `json:"url"`
	HookAttributes     HookAttributes    `json:"hook_attributes"`
	RedirectURL        string            `json:"redirect_url"`
	SetupURL           string            `json:"setup_url"`
	SetupOnUpdate      bool              `json:"setup_on_update"`
	Public             bool              `json:"public"`
	DefaultPermissions map[string]string `json:"default_permissions"`
	DefaultEvents      []string          `json:"default_events"`
}

// HookAttributes configures the webhook URL in the App manifest.
type HookAttributes struct {
	URL    string `json:"url"`
	Active bool   `json:"active"`
}

// BuildManifest constructs the GitHub App manifest with minimum permissions (GHA-1, GHA-2).
func BuildManifest(appName, panelOrigin, hookDomain string) Manifest {
	if strings.TrimSpace(appName) == "" {
		appName = "dermaga"
	}
	origin := strings.TrimRight(panelOrigin, "/")
	return Manifest{
		Name: appName,
		URL:  origin,
		HookAttributes: HookAttributes{
			URL:    fmt.Sprintf("https://%s/hooks/github-app", strings.TrimSpace(hookDomain)),
			Active: true,
		},
		RedirectURL:   origin + "/settings/github/callback",
		SetupURL:      origin + "/settings/github/install-callback",
		SetupOnUpdate: true,
		Public:        false,
		DefaultPermissions: map[string]string{
			"contents": "read",
			"metadata": "read",
		},
		DefaultEvents: []string{"push"},
	}
}

// ManifestCreateURL returns the GitHub URL where the browser POSTs the manifest form.
func (c *Client) ManifestCreateURL(state string) string {
	return fmt.Sprintf("%s/settings/apps/new?state=%s", c.webBase(), url.QueryEscape(state))
}

// AppInstallURL returns the GitHub URL to install or configure repositories for the App (GHA-3).
func (c *Client) AppInstallURL(slug string) string {
	return fmt.Sprintf("%s/apps/%s/installations/new", c.webBase(), url.PathEscape(slug))
}

// ManifestConversion is returned by POST /app-manifests/{code}/conversions (GHA-1).
type ManifestConversion struct {
	ID            int64  `json:"id"`
	Slug          string `json:"slug"`
	Name          string `json:"name"`
	HTMLURL       string `json:"html_url"`
	PEM           string `json:"pem"`
	ClientID      string `json:"client_id"`
	ClientSecret  string `json:"client_secret"`
	WebhookSecret string `json:"webhook_secret"`
}

// AppInfo represents the authenticated GitHub App metadata from GET /app.
type AppInfo struct {
	ID      int64  `json:"id"`
	Slug    string `json:"slug"`
	Name    string `json:"name"`
	HTMLURL string `json:"html_url"`
}

// Installation represents a GitHub App installation from GET /app/installations.
type Installation struct {
	ID                  int64               `json:"id"`
	Account             InstallationAccount `json:"account"`
	RepositorySelection string              `json:"repository_selection"`
	SuspendedAt         *string             `json:"suspended_at"`
	HTMLURL             string              `json:"html_url"`
}

// InstallationAccount describes the user or organization owning an installation.
type InstallationAccount struct {
	Login string `json:"login"`
	Type  string `json:"type"`
}

// Repository represents a repository accessible to a GitHub App installation.
type Repository struct {
	ID            int64           `json:"id"`
	Name          string          `json:"name"`
	FullName      string          `json:"full_name"`
	Owner         RepositoryOwner `json:"owner"`
	Private       bool            `json:"private"`
	DefaultBranch string          `json:"default_branch"`
	HTMLURL       string          `json:"html_url"`
}

// RepositoryOwner identifies the repository owner.
type RepositoryOwner struct {
	Login string `json:"login"`
}

// SignAppJWT signs a short-lived RS256 JWT for GitHub App authentication using crypto/rsa.
func SignAppJWT(appID int64, pemBytes []byte, now time.Time) (string, error) {
	if appID <= 0 {
		return "", errors.New("invalid GitHub App ID")
	}
	priv, err := parseRSAPrivateKey(pemBytes)
	if err != nil {
		return "", err
	}

	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"RS256","typ":"JWT"}`))
	claimsBytes, err := json.Marshal(struct {
		IAT int64  `json:"iat"`
		EXP int64  `json:"exp"`
		ISS string `json:"iss"`
	}{
		IAT: now.Add(-60 * time.Second).Unix(),
		EXP: now.Add(9 * time.Minute).Unix(),
		ISS: strconv.FormatInt(appID, 10),
	})
	if err != nil {
		return "", fmt.Errorf("marshaling JWT claims: %w", err)
	}
	payload := base64.RawURLEncoding.EncodeToString(claimsBytes)
	signingInput := header + "." + payload

	digest := sha256.Sum256([]byte(signingInput))
	sig, err := rsa.SignPKCS1v15(rand.Reader, priv, crypto.SHA256, digest[:])
	if err != nil {
		return "", fmt.Errorf("signing App JWT: %w", err)
	}
	return signingInput + "." + base64.RawURLEncoding.EncodeToString(sig), nil
}

func parseRSAPrivateKey(pemBytes []byte) (*rsa.PrivateKey, error) {
	block, _ := pem.Decode(pemBytes)
	if block == nil {
		return nil, errors.New("decoding GitHub App PEM: no PEM block found")
	}
	if key, err := x509.ParsePKCS1PrivateKey(block.Bytes); err == nil {
		return key, nil
	}
	anyKey, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parsing GitHub App private key: %w", err)
	}
	rsaKey, ok := anyKey.(*rsa.PrivateKey)
	if !ok {
		return nil, errors.New("GitHub App private key is not RSA")
	}
	return rsaKey, nil
}

// ExchangeManifestCode exchanges the temporary manifest code for GitHub App credentials (GHA-1).
func (c *Client) ExchangeManifestCode(ctx context.Context, code string) (*ManifestConversion, error) {
	if strings.TrimSpace(code) == "" {
		return nil, errors.New("manifest conversion code is empty")
	}
	endpoint := fmt.Sprintf("%s/app-manifests/%s/conversions", c.apiBase(), url.PathEscape(code))
	var out ManifestConversion
	if err := c.doJSON(ctx, http.MethodPost, endpoint, "", nil, &out); err != nil {
		return nil, fmt.Errorf("exchanging manifest code: %w", err)
	}
	if out.ID <= 0 || out.PEM == "" {
		return nil, errors.New("GitHub manifest conversion returned incomplete App credentials")
	}
	return &out, nil
}

// ConfigureWebhook sets the webhook URL, JSON content type, and secret via PATCH /app/hook/config (GHA-1).
func (c *Client) ConfigureWebhook(ctx context.Context, appID int64, pemBytes []byte, webhookURL, webhookSecret string) error {
	jwt, err := SignAppJWT(appID, pemBytes, c.now())
	if err != nil {
		return err
	}
	body := struct {
		URL         string `json:"url"`
		ContentType string `json:"content_type"`
		Secret      string `json:"secret"`
		InsecureSSL string `json:"insecure_ssl"`
	}{
		URL:         webhookURL,
		ContentType: "json",
		Secret:      webhookSecret,
		InsecureSSL: "0",
	}
	endpoint := fmt.Sprintf("%s/app/hook/config", c.apiBase())
	if err := c.doJSON(ctx, http.MethodPatch, endpoint, "Bearer "+jwt, body, nil); err != nil {
		return fmt.Errorf("configuring GitHub App webhook: %w", err)
	}
	return nil
}

// GetApp retrieves the authenticated GitHub App metadata via GET /app.
func (c *Client) GetApp(ctx context.Context, appID int64, pemBytes []byte) (*AppInfo, error) {
	jwt, err := SignAppJWT(appID, pemBytes, c.now())
	if err != nil {
		return nil, err
	}
	var out AppInfo
	if err := c.doJSON(ctx, http.MethodGet, c.apiBase()+"/app", "Bearer "+jwt, nil, &out); err != nil {
		return nil, fmt.Errorf("fetching GitHub App metadata: %w", err)
	}
	return &out, nil
}

// ListInstallations lists all installations for the GitHub App (GHA-3, GHA-6).
func (c *Client) ListInstallations(ctx context.Context, appID int64, pemBytes []byte) ([]Installation, error) {
	jwt, err := SignAppJWT(appID, pemBytes, c.now())
	if err != nil {
		return nil, err
	}
	var all []Installation
	for page := 1; page <= 50; page++ {
		endpoint := fmt.Sprintf("%s/app/installations?per_page=100&page=%d", c.apiBase(), page)
		var batch []Installation
		if err := c.doJSON(ctx, http.MethodGet, endpoint, "Bearer "+jwt, nil, &batch); err != nil {
			return nil, fmt.Errorf("listing GitHub App installations: %w", err)
		}
		all = append(all, batch...)
		if len(batch) < 100 {
			break
		}
	}
	return all, nil
}

// CreateInstallationToken mints an in-memory installation access token, optionally scoped to specific repo names (GHA-5).
func (c *Client) CreateInstallationToken(ctx context.Context, appID int64, pemBytes []byte, installationID int64, repoNames []string) (string, time.Time, error) {
	jwt, err := SignAppJWT(appID, pemBytes, c.now())
	if err != nil {
		return "", time.Time{}, err
	}
	var reqBody any
	if len(repoNames) > 0 {
		reqBody = struct {
			Repositories []string `json:"repositories"`
		}{Repositories: repoNames}
	}
	endpoint := fmt.Sprintf("%s/app/installations/%d/access_tokens", c.apiBase(), installationID)
	var resp struct {
		Token     string    `json:"token"`
		ExpiresAt time.Time `json:"expires_at"`
	}
	if err := c.doJSON(ctx, http.MethodPost, endpoint, "Bearer "+jwt, reqBody, &resp); err != nil {
		return "", time.Time{}, fmt.Errorf("creating installation token: %w", err)
	}
	if resp.Token == "" {
		return "", time.Time{}, errors.New("GitHub returned empty installation token")
	}
	return resp.Token, resp.ExpiresAt, nil
}

// ListInstallationRepositories lists repositories accessible to a specific installation (GHA-6).
// Uses an ephemeral in-memory installation token (GHA-5).
func (c *Client) ListInstallationRepositories(ctx context.Context, appID int64, pemBytes []byte, installationID int64) ([]Repository, error) {
	token, _, err := c.CreateInstallationToken(ctx, appID, pemBytes, installationID, nil)
	if err != nil {
		return nil, err
	}
	var all []Repository
	for page := 1; page <= 50; page++ {
		endpoint := fmt.Sprintf("%s/installation/repositories?per_page=100&page=%d", c.apiBase(), page)
		var resp struct {
			TotalCount   int          `json:"total_count"`
			Repositories []Repository `json:"repositories"`
		}
		if err := c.doJSON(ctx, http.MethodGet, endpoint, "token "+token, nil, &resp); err != nil {
			return nil, fmt.Errorf("listing installation %d repositories: %w", installationID, err)
		}
		all = append(all, resp.Repositories...)
		if len(resp.Repositories) < 100 {
			break
		}
	}
	return all, nil
}

func (c *Client) doJSON(ctx context.Context, method, endpoint, authHeader string, reqBody any, out any) error {
	var bodyReader io.Reader
	if reqBody != nil {
		data, err := json.Marshal(reqBody)
		if err != nil {
			return fmt.Errorf("marshaling request body: %w", err)
		}
		bodyReader = bytes.NewReader(data)
	}

	req, err := http.NewRequestWithContext(ctx, method, endpoint, bodyReader)
	if err != nil {
		return fmt.Errorf("building request: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", APIVersion)
	req.Header.Set("User-Agent", "dermaga")
	if authHeader != "" {
		req.Header.Set("Authorization", authHeader)
	}
	if reqBody != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.httpClient().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return fmt.Errorf("reading GitHub response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var ghErr struct {
			Message string `json:"message"`
		}
		_ = json.Unmarshal(raw, &ghErr)
		if ghErr.Message != "" {
			return fmt.Errorf("GitHub API %s: %s", resp.Status, ghErr.Message)
		}
		return fmt.Errorf("GitHub API %s", resp.Status)
	}
	if out != nil && len(raw) > 0 {
		if err := json.Unmarshal(raw, out); err != nil {
			return fmt.Errorf("decoding GitHub response: %w", err)
		}
	}
	return nil
}
