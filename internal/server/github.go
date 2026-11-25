package server

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"dermaga/internal/auth"
	"dermaga/internal/crypto"
	"dermaga/internal/github"
	"dermaga/internal/store"
)

const (
	settingGitHubAppID         = auth.SettingGitHubAppID
	settingGitHubAppSlug       = "github_app_slug"
	settingGitHubAppName       = "github_app_name"
	settingGitHubAppHTMLURL    = "github_app_html_url"
	settingGitHubAppPEMEnc     = "github_app_pem_encrypted"
	settingGitHubClientID      = "github_client_id"
	settingGitHubClientSecEnc  = "github_client_secret_encrypted"
	settingGitHubWebhookSecEnc = "github_webhook_secret_encrypted"

	manifestStateTTL = 15 * time.Minute
)

type pendingManifestState struct {
	SessionID string
	ExpiresAt time.Time
}

func (s *Server) githubClient() *github.Client {
	if s.cfg.GitHub != nil {
		return s.cfg.GitHub
	}
	return github.NewClient()
}

func (s *Server) currentSessionID(r *http.Request) (string, bool) {
	cookie, err := r.Cookie(auth.SessionCookieName)
	if err != nil || cookie.Value == "" {
		return "", false
	}
	return auth.HashSessionToken(cookie.Value), true
}

func (s *Server) buildGitHubStatus(ctx context.Context) (GitHubStatusResponse, error) {
	hookDomain, _ := s.cfg.Store.GetSetting(ctx, auth.SettingHookDomain)
	appIDStr, err := s.cfg.Store.GetSetting(ctx, settingGitHubAppID)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return GitHubStatusResponse{}, err
	}

	resp := GitHubStatusResponse{
		HookDomain:    hookDomain,
		Installations: []GitHubInstallationInfo{},
		Repositories:  []GitHubRepositoryInfo{},
	}

	if appIDStr == "" {
		return resp, nil
	}

	appID, _ := strconv.ParseInt(appIDStr, 10, 64)
	slug, _ := s.cfg.Store.GetSetting(ctx, settingGitHubAppSlug)
	name, _ := s.cfg.Store.GetSetting(ctx, settingGitHubAppName)
	htmlURL, _ := s.cfg.Store.GetSetting(ctx, settingGitHubAppHTMLURL)

	resp.Connected = true
	resp.App = &GitHubAppInfo{
		ID:         appID,
		Slug:       slug,
		Name:       name,
		HTMLURL:    htmlURL,
		InstallURL: s.githubClient().AppInstallURL(slug),
	}

	installs, err := s.cfg.Store.ListGitHubInstallations(ctx)
	if err != nil {
		return GitHubStatusResponse{}, err
	}
	for _, inst := range installs {
		resp.Installations = append(resp.Installations, GitHubInstallationInfo{
			ID:                  inst.ID,
			AccountLogin:        inst.AccountLogin,
			AccountType:         inst.AccountType,
			RepositorySelection: inst.RepositorySelection,
			SuspendedAt:         inst.SuspendedAt,
			HTMLURL:             inst.HTMLURL,
		})
	}

	repos, err := s.cfg.Store.ListGitHubRepositories(ctx)
	if err != nil {
		return GitHubStatusResponse{}, err
	}
	for _, repo := range repos {
		resp.Repositories = append(resp.Repositories, GitHubRepositoryInfo{
			ID:             repo.ID,
			InstallationID: repo.InstallationID,
			FullName:       repo.FullName,
			Owner:          repo.Owner,
			Name:           repo.Name,
			Private:        repo.Private,
			DefaultBranch:  repo.DefaultBranch,
			HTMLURL:        repo.HTMLURL,
		})
	}

	if last, err := s.cfg.Store.GetLastWebhookDelivery(ctx); err == nil && last != nil {
		resp.LastDelivery = &GitHubDeliveryInfo{
			DeliveryID:         last.DeliveryID,
			Event:              last.Event,
			Action:             last.Action,
			RepositoryFullName: last.RepositoryFullName,
			Status:             last.Status,
			ReceivedAt:         last.ReceivedAt,
		}
	}

	return resp, nil
}

// handleGetGitHubStatus returns the GitHub App connection status, installations, and repos (GHA-6, GHA-8).
func (s *Server) handleGetGitHubStatus(w http.ResponseWriter, r *http.Request) {
	resp, err := s.buildGitHubStatus(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to load GitHub status"})
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

// handleStartGitHubManifest generates a single-use session-bound CSRF state and GitHub App manifest (GHA-1, GHA-2).
func (s *Server) handleStartGitHubManifest(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	hookDomain, _ := s.cfg.Store.GetSetting(ctx, auth.SettingHookDomain)
	if strings.TrimSpace(hookDomain) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error": "Hook domain must be configured before connecting GitHub.",
		})
		return
	}

	sessionID, ok := s.currentSessionID(r)
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}

	rawState := make([]byte, 32)
	if _, err := rand.Read(rawState); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to generate state"})
		return
	}
	state := base64.RawURLEncoding.EncodeToString(rawState)
	stateSum := sha256.Sum256([]byte(state))
	stateHash := hex.EncodeToString(stateSum[:])

	now := s.now()
	s.mu.Lock()
	for k, v := range s.manifestState {
		if now.After(v.ExpiresAt) {
			delete(s.manifestState, k)
		}
	}
	s.manifestState[stateHash] = pendingManifestState{
		SessionID: sessionID,
		ExpiresAt: now.Add(manifestStateTTL),
	}
	s.mu.Unlock()

	host := r.Host
	if host == "" {
		host = "localhost:7070"
	}
	panelOrigin := "http://" + host

	hostname, _ := os.Hostname()
	appName := "dermaga"
	if hostname != "" {
		appName = "dermaga-" + hostname
	}
	manifest := github.BuildManifest(appName, panelOrigin, hookDomain)
	manifestBytes, err := json.Marshal(manifest)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to encode manifest"})
		return
	}

	writeJSON(w, http.StatusOK, GitHubManifestStartResponse{
		ActionURL:    s.githubClient().ManifestCreateURL(state),
		ManifestJSON: string(manifestBytes),
	})
}

func (s *Server) consumeManifestState(state, sessionID string) bool {
	if state == "" || sessionID == "" {
		return false
	}
	sum := sha256.Sum256([]byte(state))
	stateHash := hex.EncodeToString(sum[:])

	s.mu.Lock()
	defer s.mu.Unlock()
	pending, ok := s.manifestState[stateHash]
	if !ok {
		return false
	}
	delete(s.manifestState, stateHash)
	if s.now().After(pending.ExpiresAt) {
		return false
	}
	return pending.SessionID == sessionID
}

func (s *Server) now() time.Time {
	if s.cfg.Now != nil {
		return s.cfg.Now()
	}
	return time.Now()
}

// handleGitHubCallback verifies the single-use session-bound state, exchanges the manifest code,
// configures the 32-byte webhook secret via PATCH /app/hook/config, and stores encrypted credentials (GHA-1, GHA-4, SEC-9).
func (s *Server) handleGitHubCallback(w http.ResponseWriter, r *http.Request) {
	var req GitHubCallbackRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, GitHubCallbackResponse{Error: "invalid request body"})
		return
	}
	req.Code = strings.TrimSpace(req.Code)
	req.State = strings.TrimSpace(req.State)
	if req.Code == "" || req.State == "" {
		writeJSON(w, http.StatusBadRequest, GitHubCallbackResponse{Error: "missing code or state"})
		return
	}

	sessionID, ok := s.currentSessionID(r)
	if !ok {
		writeJSON(w, http.StatusUnauthorized, GitHubCallbackResponse{Error: "unauthorized"})
		return
	}
	if !s.consumeManifestState(req.State, sessionID) {
		writeJSON(w, http.StatusBadRequest, GitHubCallbackResponse{Error: "invalid or expired GitHub manifest state; please restart Connect GitHub"})
		return
	}

	ctx := r.Context()
	hookDomain, _ := s.cfg.Store.GetSetting(ctx, auth.SettingHookDomain)
	if strings.TrimSpace(hookDomain) == "" {
		writeJSON(w, http.StatusBadRequest, GitHubCallbackResponse{Error: "Hook domain must be configured before connecting GitHub."})
		return
	}

	client := s.githubClient()
	conv, err := client.ExchangeManifestCode(ctx, req.Code)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, GitHubCallbackResponse{Error: fmt.Sprintf("GitHub manifest exchange failed: %v", err)})
		return
	}

	rawSecret, err := crypto.GenerateRandomKey()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, GitHubCallbackResponse{Error: "failed to generate webhook secret"})
		return
	}
	webhookSecret := hex.EncodeToString(rawSecret)
	webhookURL := fmt.Sprintf("https://%s/hooks/github-app", strings.TrimSpace(hookDomain))
	if err := client.ConfigureWebhook(ctx, conv.ID, []byte(conv.PEM), webhookURL, webhookSecret); err != nil {
		writeJSON(w, http.StatusBadGateway, GitHubCallbackResponse{Error: fmt.Sprintf("failed to configure GitHub webhook: %v", err)})
		return
	}

	pemEnc, err := crypto.Encrypt(s.cfg.MasterKey, settingGitHubAppPEMEnc, []byte(conv.PEM))
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, GitHubCallbackResponse{Error: "failed to encrypt GitHub App key"})
		return
	}
	clientSecEnc, err := crypto.Encrypt(s.cfg.MasterKey, settingGitHubClientSecEnc, []byte(conv.ClientSecret))
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, GitHubCallbackResponse{Error: "failed to encrypt GitHub client secret"})
		return
	}
	webhookSecEnc, err := crypto.Encrypt(s.cfg.MasterKey, settingGitHubWebhookSecEnc, []byte(webhookSecret))
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, GitHubCallbackResponse{Error: "failed to encrypt GitHub webhook secret"})
		return
	}

	pairs := map[string]string{
		settingGitHubAppID:         strconv.FormatInt(conv.ID, 10),
		settingGitHubAppSlug:       conv.Slug,
		settingGitHubAppName:       conv.Name,
		settingGitHubAppHTMLURL:    conv.HTMLURL,
		settingGitHubClientID:      conv.ClientID,
		settingGitHubAppPEMEnc:     pemEnc,
		settingGitHubClientSecEnc:  clientSecEnc,
		settingGitHubWebhookSecEnc: webhookSecEnc,
	}
	for k, v := range pairs {
		if err := s.cfg.Store.SetSetting(ctx, k, v); err != nil {
			writeJSON(w, http.StatusInternalServerError, GitHubCallbackResponse{Error: "failed to save GitHub App settings"})
			return
		}
	}

	_ = s.cfg.Store.RecordAudit(ctx, "panel", "admin", "github_connect", fmt.Sprintf("Connected GitHub App %s (%d)", conv.Slug, conv.ID), nil, nil)

	installURL := client.AppInstallURL(conv.Slug)
	writeJSON(w, http.StatusOK, GitHubCallbackResponse{
		Success: true,
		App: &GitHubAppInfo{
			ID:         conv.ID,
			Slug:       conv.Slug,
			Name:       conv.Name,
			HTMLURL:    conv.HTMLURL,
			InstallURL: installURL,
		},
		InstallURL: installURL,
	})
}

func (s *Server) loadGitHubCredentials(ctx context.Context) (int64, []byte, error) {
	idStr, err := s.cfg.Store.GetSetting(ctx, settingGitHubAppID)
	if err != nil || idStr == "" {
		return 0, nil, errors.New("GitHub App is not connected")
	}
	appID, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil || appID <= 0 {
		return 0, nil, errors.New("invalid stored GitHub App ID")
	}
	pemEnc, err := s.cfg.Store.GetSetting(ctx, settingGitHubAppPEMEnc)
	if err != nil || pemEnc == "" {
		return 0, nil, errors.New("GitHub App private key is missing")
	}
	pemBytes, err := crypto.Decrypt(s.cfg.MasterKey, settingGitHubAppPEMEnc, pemEnc)
	if err != nil {
		return 0, nil, fmt.Errorf("decrypting GitHub App private key: %w", err)
	}
	return appID, pemBytes, nil
}

// handleSyncGitHub refreshes installations and repositories from the GitHub API (GHA-3, GHA-6).
func (s *Server) handleSyncGitHub(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	appID, pemBytes, err := s.loadGitHubCredentials(ctx)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	client := s.githubClient()
	installs, err := client.ListInstallations(ctx, appID, pemBytes)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": fmt.Sprintf("failed to list installations: %v", err)})
		return
	}

	var storeInstalls []store.GitHubInstallation
	var storeRepos []store.GitHubRepository
	for _, inst := range installs {
		storeInstalls = append(storeInstalls, store.GitHubInstallation{
			ID:                  inst.ID,
			AccountLogin:        inst.Account.Login,
			AccountType:         inst.Account.Type,
			RepositorySelection: inst.RepositorySelection,
			SuspendedAt:         inst.SuspendedAt,
			HTMLURL:             inst.HTMLURL,
		})
		if inst.SuspendedAt != nil {
			continue
		}
		repos, err := client.ListInstallationRepositories(ctx, appID, pemBytes, inst.ID)
		if err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]string{"error": fmt.Sprintf("failed to list repositories for %s: %v", inst.Account.Login, err)})
			return
		}
		for _, repo := range repos {
			storeRepos = append(storeRepos, store.GitHubRepository{
				ID:             repo.ID,
				InstallationID: inst.ID,
				FullName:       repo.FullName,
				Owner:          repo.Owner.Login,
				Name:           repo.Name,
				Private:        repo.Private,
				DefaultBranch:  repo.DefaultBranch,
				HTMLURL:        repo.HTMLURL,
			})
		}
	}

	if err := s.cfg.Store.ReplaceGitHubInstallationsAndRepos(ctx, storeInstalls, storeRepos); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to save GitHub repositories"})
		return
	}

	status, err := s.buildGitHubStatus(ctx)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to load GitHub status"})
		return
	}
	writeJSON(w, http.StatusOK, status)
}

// handleDisconnectGitHub requires typed confirmation and removes all stored GitHub App credentials (GHA-6).
func (s *Server) handleDisconnectGitHub(w http.ResponseWriter, r *http.Request) {
	var req GitHubDisconnectRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10)).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, GitHubDisconnectResponse{Error: "invalid request body"})
		return
	}
	if strings.TrimSpace(req.Confirm) != "disconnect" {
		writeJSON(w, http.StatusBadRequest, GitHubDisconnectResponse{Error: `type "disconnect" to confirm`})
		return
	}

	ctx := r.Context()
	slug, _ := s.cfg.Store.GetSetting(ctx, settingGitHubAppSlug)
	if err := s.cfg.Store.ClearGitHubData(ctx); err != nil {
		writeJSON(w, http.StatusInternalServerError, GitHubDisconnectResponse{Error: "failed to clear GitHub data"})
		return
	}
	_ = s.cfg.Store.RecordAudit(ctx, "panel", "admin", "github_disconnect", "Disconnected GitHub App", nil, nil)

	deleteURL := "https://github.com/settings/apps"
	if slug != "" {
		deleteURL = fmt.Sprintf("https://github.com/settings/apps/%s/advanced", slug)
	}
	writeJSON(w, http.StatusOK, GitHubDisconnectResponse{
		Success:      true,
		DeleteAppURL: deleteURL,
	})
}
