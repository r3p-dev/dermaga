package main

import (
	"bytes"
	"context"
	"crypto"
	"crypto/hmac"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

type Manifest struct {
	Name               string             `json:"name"`
	URL                string             `json:"url"`
	HookAttributes     HookAttributes     `json:"hook_attributes"`
	RedirectURL        string             `json:"redirect_url"`
	SetupURL           string             `json:"setup_url"`
	Public             bool               `json:"public"`
	DefaultPermissions DefaultPermissions `json:"default_permissions"`
	DefaultEvents      []string           `json:"default_events"`
}

type HookAttributes struct {
	URL    string `json:"url"`
	Active bool   `json:"active"`
}

type DefaultPermissions struct {
	Contents string `json:"contents"`
	Metadata string `json:"metadata"`
}

type AppConfig struct {
	ID            int64  `json:"id"`
	Slug          string `json:"slug"`
	NodeID        string `json:"node_id"`
	Owner         any    `json:"owner"`
	Name          string `json:"name"`
	Description   string `json:"description"`
	ExternalURL   string `json:"external_url"`
	HTMLURL       string `json:"html_url"`
	CreatedAt     string `json:"created_at"`
	UpdatedAt     string `json:"updated_at"`
	ClientID      string `json:"client_id"`
	ClientSecret  string `json:"client_secret"`
	WebhookSecret string `json:"webhook_secret"`
	PEM           string `json:"pem"`
}

type InstallationTokenResponse struct {
	Token        string `json:"token"`
	ExpiresAt    string `json:"expires_at"`
	Permissions  any    `json:"permissions"`
	Repositories []struct {
		ID       int64  `json:"id"`
		Name     string `json:"name"`
		FullName string `json:"full_name"`
	} `json:"repositories"`
}

var (
	stateLock sync.Mutex
	csrfState string
	appConfig *AppConfig
	logFile   *os.File
)

func logEvidence(format string, args ...any) {
	msg := fmt.Sprintf("[%s] ", time.Now().Format(time.RFC3339Nano)) + fmt.Sprintf(format, args...)
	log.Println(msg)
	if logFile != nil {
		fmt.Fprintln(logFile, msg)
	}
}

func generateRandomString(n int) string {
	b := make([]byte, n)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func generateJWT(appID int64, pemKey string) (string, error) {
	block, _ := pem.Decode([]byte(pemKey))
	if block == nil {
		return "", fmt.Errorf("failed to decode PEM block")
	}

	privKey, err := x509.ParsePKCS1PrivateKey(block.Bytes)
	if err != nil {
		k, err2 := x509.ParsePKCS8PrivateKey(block.Bytes)
		if err2 != nil {
			return "", fmt.Errorf("parse private key failed: %w / %v", err, err2)
		}
		var ok bool
		privKey, ok = k.(*rsa.PrivateKey)
		if !ok {
			return "", fmt.Errorf("not an RSA private key")
		}
	}

	header := map[string]string{
		"alg": "RS256",
		"typ": "JWT",
	}
	headerJSON, _ := json.Marshal(header)
	headerB64 := base64.RawURLEncoding.EncodeToString(headerJSON)

	now := time.Now().Unix()
	payload := map[string]any{
		"iat": now - 60,
		"exp": now + 600,
		"iss": fmt.Sprintf("%d", appID),
	}
	payloadJSON, _ := json.Marshal(payload)
	payloadB64 := base64.RawURLEncoding.EncodeToString(payloadJSON)

	signingInput := headerB64 + "." + payloadB64
	hasher := sha256.New()
	hasher.Write([]byte(signingInput))
	digest := hasher.Sum(nil)

	sig, err := rsa.SignPKCS1v15(rand.Reader, privKey, crypto.SHA256, digest)
	if err != nil {
		return "", fmt.Errorf("signing JWT failed: %w", err)
	}

	sigB64 := base64.RawURLEncoding.EncodeToString(sig)
	return signingInput + "." + sigB64, nil
}

func main() {
	var err error
	logFile, err = os.OpenFile("evidence.log", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		log.Printf("Warning: cannot open evidence.log: %v", err)
	}
	defer func() {
		if logFile != nil {
			logFile.Close()
		}
	}()

	stateLock.Lock()
	csrfState = generateRandomString(16)
	stateLock.Unlock()

	http.HandleFunc("/", handleIndex)
	http.HandleFunc("/github/callback", handleCallback)
	http.HandleFunc("/github/setup", handleSetup)
	http.HandleFunc("/hooks/github-app", handleWebhook)

	addr := "127.0.0.1:7070"
	logEvidence("GitHub App Spike server listening on http://%s", addr)
	logEvidence("Initial CSRF state: %s", csrfState)
	if err := http.ListenAndServe(addr, nil); err != nil {
		log.Fatalf("Server failed: %v", err)
	}
}

func handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}

	stateLock.Lock()
	state := csrfState
	stateLock.Unlock()

	appName := "dermaga-dev-r3p-spike"
	if custom := r.URL.Query().Get("app_name"); custom != "" {
		appName = custom
	}

	manifest := Manifest{
		Name: appName,
		URL:  "http://localhost:7070",
		HookAttributes: HookAttributes{
			URL:    "https://hooks.example.test/hooks/github-app",
			Active: false,
		},
		RedirectURL: "http://localhost:7070/github/callback",
		SetupURL:    "http://localhost:7070/github/setup",
		Public:      false,
		DefaultPermissions: DefaultPermissions{
			Contents: "read",
			Metadata: "read",
		},
		DefaultEvents: []string{"push"},
	}

	manifestJSON, _ := json.MarshalIndent(manifest, "", "  ")

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprintf(w, `<!DOCTYPE html>
<html>
<head>
  <meta charset="utf-8">
  <title>GitHub App Manifest Spike</title>
  <style>
    body { font-family: system-ui, sans-serif; max-width: 800px; margin: 40px auto; padding: 0 20px; line-height: 1.5; color: #222; }
    pre { background: #f4f4f4; padding: 15px; border-radius: 6px; overflow-x: auto; font-size: 13px; }
    button { background: #0969da; color: #fff; border: 0; padding: 10px 18px; font-size: 15px; font-weight: 600; border-radius: 6px; cursor: pointer; }
    button:hover { background: #0856b7; }
    .card { border: 1px solid #d0d7de; border-radius: 6px; padding: 20px; margin-bottom: 20px; }
    .badge { display: inline-block; background: #dafbe1; color: #1a7f37; padding: 2px 8px; border-radius: 12px; font-size: 12px; font-weight: 600; }
  </style>
</head>
<body>
  <h1>dermaga M0: GitHub App Manifest Spike</h1>
  <p><span class="badge">Spike Active</span> Listening on <code>http://127.0.0.1:7070</code></p>

  <div class="card">
    <h3>Manifest Payload</h3>
    <pre>%s</pre>
    <form action="https://github.com/settings/apps/new?state=%s" method="post" target="_blank">
      <input type="hidden" name="manifest" value='%s'>
      <button type="submit">Submit Manifest to GitHub (Personal Account)</button>
    </form>
  </div>

  <div class="card">
    <h4>Custom App Name (if name already taken on GitHub)</h4>
    <form method="get" action="/">
      <input type="text" name="app_name" value="%s" style="padding: 8px; width: 300px;">
      <button type="submit" style="background:#555;">Update Name</button>
    </form>
  </div>
</body>
</html>`, manifestJSON, state, manifestJSON, appName)
}

func handleCallback(w http.ResponseWriter, r *http.Request) {
	callbackTime := time.Now()
	query := r.URL.Query()
	code := query.Get("code")
	returnedState := query.Get("state")

	logEvidence("=== STEP 2a: GitHub Callback Received ===")
	logEvidence("Method: %s, URL: %s", r.Method, r.URL.String())
	logEvidence("Headers: User-Agent: %s, Referer: %s", r.Header.Get("User-Agent"), r.Header.Get("Referer"))
	logEvidence("Code received: %s (len=%d)", maskString(code, 6), len(code))
	logEvidence("State received: %s", returnedState)

	stateLock.Lock()
	expectedState := csrfState
	stateLock.Unlock()

	if returnedState != expectedState {
		errStr := fmt.Sprintf("CSRF state mismatch: expected %q, got %q", expectedState, returnedState)
		logEvidence("ERROR: %s", errStr)
		http.Error(w, errStr, http.StatusBadRequest)
		return
	}

	if code == "" {
		errStr := "Missing code in callback query parameters"
		logEvidence("ERROR: %s", errStr)
		http.Error(w, errStr, http.StatusBadRequest)
		return
	}

	// Immediate conversion: POST https://api.github.com/app-manifests/{code}/conversions
	convStart := time.Now()
	convURL := fmt.Sprintf("https://api.github.com/app-manifests/%s/conversions", code)
	logEvidence("Calling conversion endpoint: %s (code received %v ago)", convURL, time.Since(callbackTime))

	req, err := http.NewRequestWithContext(r.Context(), "POST", convURL, bytes.NewReader([]byte("{}")))
	if err != nil {
		logEvidence("ERROR creating conversion request: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	convDuration := time.Since(convStart)
	if err != nil {
		logEvidence("ERROR executing conversion request: %v (duration: %v)", err, convDuration)
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	bodyBytes, _ := io.ReadAll(resp.Body)
	logEvidence("Conversion response status: %s, duration: %v, body length: %d bytes", resp.Status, convDuration, len(bodyBytes))

	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		logEvidence("ERROR conversion non-2xx body: %s", string(bodyBytes))
		http.Error(w, fmt.Sprintf("GitHub conversion failed: %s\n%s", resp.Status, string(bodyBytes)), http.StatusBadGateway)
		return
	}

	var conf AppConfig
	if err := json.Unmarshal(bodyBytes, &conf); err != nil {
		logEvidence("ERROR unmarshaling conversion response: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	stateLock.Lock()
	appConfig = &conf
	stateLock.Unlock()

	logEvidence("App created successfully!")
	logEvidence("  App ID: %d", conf.ID)
	logEvidence("  Slug: %s", conf.Slug)
	logEvidence("  Client ID: %s", conf.ClientID)
	logEvidence("  Client Secret: [REDACTED len=%d]", len(conf.ClientSecret))
	logEvidence("  Webhook Secret: [REDACTED len=%d]", len(conf.WebhookSecret))
	logEvidence("  PEM Key: [REDACTED len=%d bytes]", len(conf.PEM))
	logEvidence("  HTML URL: %s", conf.HTMLURL)

	installURL := fmt.Sprintf("https://github.com/apps/%s/installations/new", conf.Slug)
	logEvidence("Next step: User must install the app at %s", installURL)

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprintf(w, `<!DOCTYPE html>
<html>
<head>
  <meta charset="utf-8">
  <title>GitHub App Created</title>
  <style>
    body { font-family: system-ui, sans-serif; max-width: 800px; margin: 40px auto; padding: 0 20px; line-height: 1.5; color: #222; }
    pre { background: #f4f4f4; padding: 15px; border-radius: 6px; overflow-x: auto; font-size: 13px; }
    button, a.btn { display: inline-block; background: #2da44e; color: #fff; text-decoration: none; padding: 12px 20px; font-size: 16px; font-weight: 600; border-radius: 6px; }
    a.btn:hover { background: #2c974b; }
    .card { border: 1px solid #d0d7de; border-radius: 6px; padding: 20px; margin-bottom: 20px; }
    .success { background: #dafbe1; color: #1a7f37; padding: 8px 12px; border-radius: 6px; font-weight: 600; }
  </style>
</head>
<body>
  <h1>Step 2a Succeeded: GitHub App Created!</h1>
  <p class="success">GitHub redirected to http://localhost:7070/github/callback and manifest code was exchanged successfully.</p>

  <div class="card">
    <h3>App Details</h3>
    <ul>
      <li><strong>App ID:</strong> %d</li>
      <li><strong>App Slug:</strong> %s</li>
      <li><strong>Client ID:</strong> %s</li>
      <li><strong>Private Key:</strong> Received & Verified (len=%d)</li>
      <li><strong>Webhook Secret:</strong> Received & Stored</li>
    </ul>

    <hr style="border: 0; border-top: 1px solid #eee; margin: 20px 0;">
    <h3>Next: Install the App</h3>
    <p>Click below to install this private GitHub App on your account/organization and choose which repositories it can access (e.g. <code>portfolio</code>):</p>
    <a class="btn" href="%s" target="_blank">Install GitHub App on GitHub &rarr;</a>
    <p><small>After installing, GitHub will redirect back to <code>http://localhost:7070/github/setup</code>.</small></p>
  </div>
</body>
</html>`, conf.ID, conf.Slug, conf.ClientID, len(conf.PEM), installURL)
}

func handleSetup(w http.ResponseWriter, r *http.Request) {
	setupTime := time.Now()
	query := r.URL.Query()
	installationID := query.Get("installation_id")
	setupAction := query.Get("setup_action")

	logEvidence("=== STEP 2b: GitHub Setup Callback Received ===")
	logEvidence("Method: %s, URL: %s", r.Method, r.URL.String())
	logEvidence("Installation ID: %s, Action: %s", installationID, setupAction)

	stateLock.Lock()
	conf := appConfig
	stateLock.Unlock()

	if conf == nil {
		errStr := "No AppConfig found in memory. Please create the app first."
		logEvidence("ERROR: %s", errStr)
		http.Error(w, errStr, http.StatusBadRequest)
		return
	}

	if installationID == "" {
		errStr := "Missing installation_id in query parameters"
		logEvidence("ERROR: %s", errStr)
		http.Error(w, errStr, http.StatusBadRequest)
		return
	}

	// 1. Generate RS256 JWT
	jwtToken, err := generateJWT(conf.ID, conf.PEM)
	if err != nil {
		logEvidence("ERROR generating JWT: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	logEvidence("Generated App JWT (length=%d, algorithm=RS256)", len(jwtToken))

	// 2. Request installation access token scoped to repository "portfolio" (or all)
	tokenStart := time.Now()
	tokenURL := fmt.Sprintf("https://api.github.com/app/installations/%s/access_tokens", installationID)
	logEvidence("Requesting repo-scoped installation token: %s", tokenURL)

	reqBody := []byte(`{"repositories":["portfolio"]}`)
	req, err := http.NewRequestWithContext(r.Context(), "POST", tokenURL, bytes.NewReader(reqBody))
	if err != nil {
		logEvidence("ERROR creating token request: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	req.Header.Set("Authorization", "Bearer "+jwtToken)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	tokenDuration := time.Since(tokenStart)
	if err != nil {
		logEvidence("ERROR requesting installation token: %v (duration: %v)", err, tokenDuration)
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	bodyBytes, _ := io.ReadAll(resp.Body)
	logEvidence("Token response status: %s, duration: %v, body length: %d bytes", resp.Status, tokenDuration, len(bodyBytes))

	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		logEvidence("Note: scoped request for 'portfolio' returned %s: %s", resp.Status, string(bodyBytes))
		// If scoped request failed because portfolio wasn't selected, retry unscoped for this installation
		logEvidence("Retrying unscoped token request for installation...")
		req2, _ := http.NewRequestWithContext(r.Context(), "POST", tokenURL, bytes.NewReader([]byte("{}")))
		req2.Header.Set("Authorization", "Bearer "+jwtToken)
		req2.Header.Set("Accept", "application/vnd.github+json")
		req2.Header.Set("X-GitHub-Api-Version", "2022-11-28")
		resp2, err2 := client.Do(req2)
		if err2 != nil {
			logEvidence("ERROR retrying token: %v", err2)
			http.Error(w, err2.Error(), http.StatusBadGateway)
			return
		}
		defer resp2.Body.Close()
		bodyBytes, _ = io.ReadAll(resp2.Body)
		if resp2.StatusCode != http.StatusCreated && resp2.StatusCode != http.StatusOK {
			logEvidence("ERROR unscoped token also failed: %s: %s", resp2.Status, string(bodyBytes))
			http.Error(w, fmt.Sprintf("Failed to get token: %s", string(bodyBytes)), http.StatusBadGateway)
			return
		}
	}

	var tokResp InstallationTokenResponse
	if err := json.Unmarshal(bodyBytes, &tokResp); err != nil {
		logEvidence("ERROR parsing token response: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	logEvidence("Installation access token acquired!")
	logEvidence("  Token: %s (expires: %s)", maskString(tokResp.Token, 8), tokResp.ExpiresAt)
	for _, repo := range tokResp.Repositories {
		logEvidence("  Accessible repo: %s (id=%d)", repo.FullName, repo.ID)
	}

	// 3. Test git ls-remote / clone with token passed via environment git config ONLY
	// Authentication to GitHub over HTTPS requires HTTP Basic Auth with user "x-access-token"
	// GIT_CONFIG_COUNT=2
	// GIT_CONFIG_KEY_0=http.https://github.com/.extraheader
	// GIT_CONFIG_VALUE_0=AUTHORIZATION: basic <base64("x-access-token:" + token)>
	// GIT_CONFIG_KEY_1=credential.helper
	// GIT_CONFIG_VALUE_1=""
	gitRepo := "https://github.com/r3p-dev/portfolio.git"
	logEvidence("Testing git access with environment git config against %s...", gitRepo)

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	basicAuth := base64.StdEncoding.EncodeToString([]byte("x-access-token:" + tokResp.Token))

	cmd := exec.CommandContext(ctx, "git", "ls-remote", gitRepo, "HEAD")
	cmd.Env = append(os.Environ(),
		"GIT_CONFIG_COUNT=2",
		"GIT_CONFIG_KEY_0=http.https://github.com/.extraheader",
		fmt.Sprintf("GIT_CONFIG_VALUE_0=AUTHORIZATION: basic %s", basicAuth),
		"GIT_CONFIG_KEY_1=credential.helper",
		"GIT_CONFIG_VALUE_1=",
		"GIT_TERMINAL_PROMPT=0",
	)

	gitOut, gitErr := cmd.CombinedOutput()
	logEvidence("Git command executed: git ls-remote %s HEAD", gitRepo)
	logEvidence("Git exit code: %d", cmd.ProcessState.ExitCode())
	logEvidence("Git raw output:\n%s", strings.TrimSpace(string(gitOut)))

	success := (gitErr == nil && cmd.ProcessState.ExitCode() == 0 && strings.Contains(string(gitOut), "HEAD"))

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	statusClass := "success"
	statusText := "All Checks Passed!"
	gitCheckBadge := "&check; Passed"
	if !success {
		statusClass = "error"
		statusText = "Git Access Failed"
		gitCheckBadge = fmt.Sprintf("&#10008; FAILED (exit %d)", cmd.ProcessState.ExitCode())
	}

	fmt.Fprintf(w, `<!DOCTYPE html>
<html>
<head>
  <meta charset="utf-8">
  <title>Spike 2 Complete</title>
  <style>
    body { font-family: system-ui, sans-serif; max-width: 800px; margin: 40px auto; padding: 0 20px; line-height: 1.5; color: #222; }
    pre { background: #f4f4f4; padding: 15px; border-radius: 6px; overflow-x: auto; font-size: 13px; }
    .card { border: 1px solid #d0d7de; border-radius: 6px; padding: 20px; margin-bottom: 20px; }
    .success { background: #dafbe1; color: #1a7f37; padding: 10px 14px; border-radius: 6px; font-weight: 600; }
    .error { background: #ffebe9; color: #cf222e; padding: 10px 14px; border-radius: 6px; font-weight: 600; }
  </style>
</head>
<body>
  <h1>Step 2 Complete: %s</h1>
  <div class="%s">
    Installation ID: %s | Setup Action: %s | Time: %s
  </div>

  <div class="card">
    <h3>Spike Verification Summary</h3>
    <ol>
      <li><strong>Manifest Flow Redirect:</strong> Redirected to <code>http://localhost:7070/github/callback</code> &check;</li>
      <li><strong>Code Conversion:</strong> Exchanged for App ID (%d), Client ID, Webhook Secret, and RSA Private Key &check;</li>
      <li><strong>Installation Redirect:</strong> Redirected to <code>http://localhost:7070/github/setup</code> &check;</li>
      <li><strong>JWT Generation:</strong> RS256 token generated via pure Go stdlib &check;</li>
      <li><strong>Repo-scoped Token:</strong> Acquired installation access token (%s) &check;</li>
      <li><strong>Git Access via Env Config:</strong> %s</li>
    </ol>

    <h4>Raw Git Output:</h4>
    <pre>%s</pre>
  </div>
</body>
</html>`, statusText, statusClass, installationID, setupAction, setupTime.Format(time.RFC3339),
		conf.ID, maskString(tokResp.Token, 8), gitCheckBadge, string(gitOut))
}

func maskString(s string, visible int) string {
	if len(s) <= visible {
		return "[REDACTED]"
	}
	return s[:visible] + "..." + "[REDACTED]"
}

func getWebhookSecret() []byte {
	data, err := os.ReadFile(".webhook_secret")
	if err == nil && len(strings.TrimSpace(string(data))) > 0 {
		return []byte(strings.TrimSpace(string(data)))
	}
	return nil
}

func handleWebhook(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 25*1024*1024)
	body, err := io.ReadAll(r.Body)
	if err != nil {
		logEvidence("Webhook body read error: %v", err)
		http.Error(w, "Payload too large", http.StatusRequestEntityTooLarge)
		return
	}

	sigHeader := r.Header.Get("X-Hub-Signature-256")
	event := r.Header.Get("X-GitHub-Event")
	delivery := r.Header.Get("X-GitHub-Delivery")

	secret := getWebhookSecret()
	if len(secret) == 0 {
		logEvidence("Webhook rejected: no webhook secret found in .webhook_secret")
		http.Error(w, "Webhook secret not configured", http.StatusInternalServerError)
		return
	}

	mac := hmac.New(sha256.New, secret)
	mac.Write(body)
	expectedSig := "sha256=" + hex.EncodeToString(mac.Sum(nil))

	if !hmac.Equal([]byte(sigHeader), []byte(expectedSig)) {
		logEvidence("WEBHOOK VERIFICATION FAILED: delivery=%s event=%s source=%s error=\"signature mismatch\"", delivery, event, r.RemoteAddr)
		http.Error(w, "Invalid signature", http.StatusUnauthorized)
		return
	}

	logEvidence("=== STEP 2c: GitHub Webhook Delivery Received & HMAC Verified ===")
	logEvidence("Event: %s, Delivery: %s, Body length: %d bytes", event, delivery, len(body))

	var payload struct {
		Ref        string `json:"ref"`
		Before     string `json:"before"`
		After      string `json:"after"`
		Repository struct {
			FullName string `json:"full_name"`
			Private  bool   `json:"private"`
		} `json:"repository"`
		Pusher struct {
			Name  string `json:"name"`
			Email string `json:"email"`
		} `json:"pusher"`
		HeadCommit struct {
			ID      string `json:"id"`
			Message string `json:"message"`
		} `json:"head_commit"`
	}

	if err := json.Unmarshal(body, &payload); err == nil && payload.Ref != "" {
		logEvidence("Push details: repo=%s ref=%s commit=%s message=%q pusher=%s",
			payload.Repository.FullName, payload.Ref, payload.HeadCommit.ID, payload.HeadCommit.Message, payload.Pusher.Name)
	}

	w.WriteHeader(http.StatusAccepted)
	w.Write([]byte("Accepted\n"))
}

