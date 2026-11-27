// Package webhook serves POST /hooks/github-app over $XDG_RUNTIME_DIR/dermaga/hooks.sock,
// enforcing SEC-11 through SEC-14 and updating installation/repository state for GHA-7 and GHA-8.
package webhook

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"dermaga/internal/crypto"
	"dermaga/internal/store"
)

const (
	// MaxBodyBytes caps webhook payloads at 25 MiB per GitHub's limit (GHA-8, SEC-11).
	MaxBodyBytes = 25 << 20

	settingWebhookSecretEnc = "github_webhook_secret_encrypted"
)

// Config configures the webhook handler and Unix socket listener.
type Config struct {
	SocketPath string
	Store      *store.Store
	MasterKey  []byte
	Logger     *slog.Logger
	Now        func() time.Time
	OnPush     func(ctx context.Context, p Push)
}

// Push is a verified push delivery, reduced to what deciding on a deployment needs (APP-8).
type Push struct {
	DeliveryID string
	Repo       string // owner/name
	Ref        string // refs/heads/<branch> or refs/tags/<tag>
	SHA        string // the commit the ref now points at
	Message    string // head commit message
	Deleted    bool   // the ref was deleted
	// Files changed by the pushed commits; nil when the payload does not say (no commits listed,
	// a commit without file lists, or the 2048-commit cap reached), which means "deploy anyway".
	Files []string
}

// maxPushCommits is GitHub's cap on the commits array of a push payload: "The array includes a
// maximum of 2048 commits" (docs.github.com/en/webhooks/webhook-events-and-payloads#push).
const maxPushCommits = 2048

// Handler handles GitHub App webhook deliveries on /hooks/github-app.
type Handler struct {
	cfg     Config
	httpSrv *http.Server
	ln      net.Listener

	mu      sync.Mutex
	buckets map[string]*tokenBucket
}

type tokenBucket struct {
	tokens float64
	last   time.Time
}

// New creates a new webhook Handler.
func New(cfg Config) *Handler {
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	h := &Handler{
		cfg:     cfg,
		buckets: make(map[string]*tokenBucket),
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/hooks/github-app", h.serveHook)
	h.httpSrv = &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	return h
}

// Handler returns the HTTP handler for testing.
func (h *Handler) Handler() http.Handler {
	return h.httpSrv.Handler
}

// Start creates and serves the 0600 Unix domain socket at cfg.SocketPath.
func (h *Handler) Start() error {
	if h.cfg.SocketPath == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(h.cfg.SocketPath), 0700); err != nil {
		return fmt.Errorf("creating hooks socket dir: %w", err)
	}
	_ = os.Remove(h.cfg.SocketPath)
	ln, err := net.Listen("unix", h.cfg.SocketPath)
	if err != nil {
		return fmt.Errorf("listening on hooks socket: %w", err)
	}
	if err := os.Chmod(h.cfg.SocketPath, 0600); err != nil {
		_ = ln.Close()
		return fmt.Errorf("chmod hooks socket: %w", err)
	}
	h.ln = ln
	return h.httpSrv.Serve(ln)
}

// Close gracefully shuts down the webhook server and removes the socket.
func (h *Handler) Close() error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err := h.httpSrv.Shutdown(ctx)
	if h.cfg.SocketPath != "" {
		_ = os.Remove(h.cfg.SocketPath)
	}
	return err
}

func (h *Handler) now() time.Time {
	if h.cfg.Now != nil {
		return h.cfg.Now()
	}
	return time.Now()
}

func (h *Handler) allowRate(key string) bool {
	if key == "" {
		key = "_global"
	}
	now := h.now()
	h.mu.Lock()
	defer h.mu.Unlock()

	const (
		maxTokens = 10.0
		refillSec = 2.0 // 0.5 req/s sustained, burst 10
	)
	b, ok := h.buckets[key]
	if !ok {
		h.buckets[key] = &tokenBucket{tokens: maxTokens - 1, last: now}
		return true
	}
	elapsed := now.Sub(b.last).Seconds()
	if elapsed > 0 {
		b.tokens += elapsed / refillSec
		if b.tokens > maxTokens {
			b.tokens = maxTokens
		}
		b.last = now
	}
	if b.tokens < 1.0 {
		return false
	}
	b.tokens -= 1.0
	return true
}

func (h *Handler) serveHook(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, MaxBodyBytes))
	if err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			http.Error(w, "payload too large", http.StatusRequestEntityTooLarge)
			return
		}
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	deliveryID := strings.TrimSpace(r.Header.Get("X-GitHub-Delivery"))
	event := strings.TrimSpace(r.Header.Get("X-GitHub-Event"))
	sigHeader := r.Header.Get("X-Hub-Signature-256")

	// SEC-11: Verify HMAC-SHA256 before any JSON parsing.
	ctx := r.Context()
	encSecret, err := h.cfg.Store.GetSetting(ctx, settingWebhookSecretEnc)
	var secret []byte
	if err == nil && encSecret != "" {
		secret, err = crypto.Decrypt(h.cfg.MasterKey, settingWebhookSecretEnc, encSecret)
	}
	if err != nil || len(secret) == 0 || !strings.HasPrefix(sigHeader, "sha256=") || !crypto.VerifyHMACSHA256(secret, body, sigHeader) {
		// SEC-11 / CONVENTIONS §4: log ONLY delivery_id, event, source, and "signature mismatch".
		h.cfg.Logger.Warn("signature mismatch",
			"delivery_id", deliveryID,
			"event", event,
			"source", "hooks.sock",
		)
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	if deliveryID == "" {
		http.Error(w, "missing delivery id", http.StatusBadRequest)
		return
	}

	// Parse minimal envelope after signature verification.
	var env struct {
		Action     string `json:"action"`
		Ref        string `json:"ref"`
		After      string `json:"after"`
		Deleted    bool   `json:"deleted"`
		HeadCommit *struct {
			Message string `json:"message"`
		} `json:"head_commit"`
		Commits []struct {
			Added    *[]string `json:"added"`
			Modified *[]string `json:"modified"`
			Removed  *[]string `json:"removed"`
		} `json:"commits"`
		Repository *struct {
			FullName string `json:"full_name"`
		} `json:"repository"`
		Installation *struct {
			ID                  int64   `json:"id"`
			RepositorySelection string  `json:"repository_selection"`
			SuspendedAt         *string `json:"suspended_at"`
			HTMLURL             string  `json:"html_url"`
			Account             struct {
				Login string `json:"login"`
				Type  string `json:"type"`
			} `json:"account"`
		} `json:"installation"`
		RepositoriesAdded []struct {
			ID            int64  `json:"id"`
			Name          string `json:"name"`
			FullName      string `json:"full_name"`
			Private       bool   `json:"private"`
			DefaultBranch string `json:"default_branch"`
		} `json:"repositories_added"`
		RepositoriesRemoved []struct {
			ID int64 `json:"id"`
		} `json:"repositories_removed"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}

	repoName := ""
	if env.Repository != nil {
		repoName = env.Repository.FullName
	}

	rateKey := repoName
	if rateKey == "" && env.Installation != nil {
		rateKey = fmt.Sprintf("inst:%d", env.Installation.ID)
	}
	if !h.allowRate(rateKey) {
		http.Error(w, "rate limit exceeded", http.StatusTooManyRequests)
		return
	}

	// SEC-12: Only process push, installation, and installation_repositories.
	status := "accepted"
	if event != "push" && event != "installation" && event != "installation_repositories" {
		status = "ignored"
	}

	// SEC-13: Dedupe on X-GitHub-Delivery.
	dup, err := h.cfg.Store.RecordWebhookDelivery(ctx, store.WebhookDelivery{
		DeliveryID:         deliveryID,
		Event:              event,
		Action:             env.Action,
		RepositoryFullName: repoName,
		Status:             status,
	})
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if dup {
		writeAccepted(w)
		return
	}

	switch event {
	case "installation":
		if env.Installation != nil {
			h.handleInstallationEvent(ctx, env.Action, env.Installation.ID, env.Installation.Account.Login, env.Installation.Account.Type, env.Installation.RepositorySelection, env.Installation.SuspendedAt, env.Installation.HTMLURL)
		}
	case "installation_repositories":
		if env.Installation != nil {
			if env.Action == "added" {
				var added []store.GitHubRepository
				for _, r := range env.RepositoriesAdded {
					owner, name, _ := strings.Cut(r.FullName, "/")
					added = append(added, store.GitHubRepository{
						ID:             r.ID,
						InstallationID: env.Installation.ID,
						FullName:       r.FullName,
						Owner:          owner,
						Name:           name,
						Private:        r.Private,
						DefaultBranch:  r.DefaultBranch,
					})
				}
				_ = h.cfg.Store.AddGitHubRepositories(ctx, added)
			} else if env.Action == "removed" {
				var ids []int64
				for _, r := range env.RepositoriesRemoved {
					ids = append(ids, r.ID)
				}
				_ = h.cfg.Store.DeleteGitHubRepositoriesByID(ctx, ids)
				_ = h.cfg.Store.RecordAudit(ctx, "webhook", deliveryID, "github_repos_removed", fmt.Sprintf("Removed %d repository access(es) from installation %d", len(ids), env.Installation.ID), nil, nil)
			}
		}
	case "push":
		if h.cfg.OnPush != nil && repoName != "" {
			p := Push{DeliveryID: deliveryID, Repo: repoName, Ref: env.Ref, SHA: env.After, Deleted: env.Deleted}
			if env.HeadCommit != nil {
				p.Message = env.HeadCommit.Message
			}
			if len(env.Commits) < maxPushCommits {
				for _, c := range env.Commits {
					if c.Added == nil && c.Modified == nil && c.Removed == nil {
						p.Files = nil
						break
					}
					for _, l := range []*[]string{c.Added, c.Modified, c.Removed} {
						if l != nil {
							p.Files = append(p.Files, *l...)
						}
					}
				}
			}
			h.cfg.OnPush(ctx, p)
		}
	}

	// SEC-14: Known and unknown repositories receive the exact same 202 response.
	writeAccepted(w)
}

func (h *Handler) handleInstallationEvent(ctx context.Context, action string, id int64, login, accType, sel string, suspendedAt *string, htmlURL string) {
	switch action {
	case "deleted":
		_ = h.cfg.Store.DeleteGitHubInstallation(ctx, id)
		_ = h.cfg.Store.RecordAudit(ctx, "webhook", "github", "github_installation_deleted", fmt.Sprintf("GitHub App installation %d (%s) uninstalled", id, login), nil, nil)
	case "suspend":
		now := h.now().UTC().Format(time.RFC3339)
		if suspendedAt == nil {
			suspendedAt = &now
		}
		_ = h.cfg.Store.SetGitHubInstallationSuspended(ctx, id, suspendedAt)
		_ = h.cfg.Store.RecordAudit(ctx, "webhook", "github", "github_installation_suspended", fmt.Sprintf("GitHub App installation %d (%s) suspended", id, login), nil, nil)
	case "unsuspend":
		_ = h.cfg.Store.SetGitHubInstallationSuspended(ctx, id, nil)
		_ = h.cfg.Store.RecordAudit(ctx, "webhook", "github", "github_installation_unsuspended", fmt.Sprintf("GitHub App installation %d (%s) unsuspended", id, login), nil, nil)
	case "created", "new_permissions_accepted":
		_ = h.cfg.Store.UpsertGitHubInstallation(ctx, store.GitHubInstallation{
			ID:                  id,
			AccountLogin:        login,
			AccountType:         accType,
			RepositorySelection: sel,
			SuspendedAt:         suspendedAt,
			HTMLURL:             htmlURL,
		})
	}
}

func writeAccepted(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	_, _ = w.Write([]byte(`{"status":"accepted"}`))
}
