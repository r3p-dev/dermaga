package webhook_test

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"dermaga/internal/crypto"
	"dermaga/internal/store"
	"dermaga/internal/webhook"
)

func signPayload(secret, body []byte) string {
	m := hmac.New(sha256.New, secret)
	m.Write(body)
	return "sha256=" + hex.EncodeToString(m.Sum(nil))
}

func setupWebhookTest(t *testing.T) (*webhook.Handler, *store.Store, []byte, *bytes.Buffer) {
	t.Helper()
	s, err := store.Open(filepath.Join(t.TempDir(), "wh.db"))
	if err != nil {
		t.Fatalf("store.Open failed: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })

	masterKey, _ := crypto.GenerateRandomKey()
	whSecret := []byte("super-secret-webhook-key-32bytes")
	enc, err := crypto.Encrypt(masterKey, "github_webhook_secret_encrypted", whSecret)
	if err != nil {
		t.Fatalf("Encrypt failed: %v", err)
	}
	_ = s.SetSetting(context.Background(), "github_webhook_secret_encrypted", enc)

	var logBuf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logBuf, nil))
	h := webhook.New(webhook.Config{
		Store:     s,
		MasterKey: masterKey,
		Logger:    logger,
	})
	return h, s, whSecret, &logBuf
}

func TestWebhookSecurity_SEC11_SEC12_SEC13_SEC14_GHA8(t *testing.T) {
	h, s, secret, logBuf := setupWebhookTest(t)
	srv := h.Handler()

	// Non-POST rejected (GHA-8)
	reqGet := httptest.NewRequest(http.MethodGet, "/hooks/github-app", nil)
	rrGet := httptest.NewRecorder()
	srv.ServeHTTP(rrGet, reqGet)
	if rrGet.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405 on GET, got %d", rrGet.Code)
	}

	// SEC-11: invalid signature returns 401 BEFORE JSON parsing, and logs no secret/sig/payload
	malformedJSON := []byte(`{"not-valid-json":`)
	reqBadSig := httptest.NewRequest(http.MethodPost, "/hooks/github-app", bytes.NewReader(malformedJSON))
	reqBadSig.Header.Set("X-GitHub-Delivery", "deliv-bad-1")
	reqBadSig.Header.Set("X-GitHub-Event", "push")
	reqBadSig.Header.Set("X-Hub-Signature-256", "sha256=deadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef")
	rrBadSig := httptest.NewRecorder()
	srv.ServeHTTP(rrBadSig, reqBadSig)
	if rrBadSig.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 on bad signature, got %d", rrBadSig.Code)
	}
	logOut := logBuf.String()
	if !strings.Contains(logOut, "signature mismatch") || !strings.Contains(logOut, "deliv-bad-1") {
		t.Errorf("expected signature mismatch + delivery_id in log, got %q", logOut)
	}
	for _, forbidden := range []string{string(secret), "deadbeef", "not-valid-json"} {
		if strings.Contains(logOut, forbidden) {
			t.Errorf("SEC-11 oracle leak: log contained %q", forbidden)
		}
	}

	// SEC-11 / GHA-8: >25 MiB body returns 413
	oversized := bytes.Repeat([]byte("a"), webhook.MaxBodyBytes+1)
	reqBig := httptest.NewRequest(http.MethodPost, "/hooks/github-app", bytes.NewReader(oversized))
	rrBig := httptest.NewRecorder()
	srv.ServeHTTP(rrBig, reqBig)
	if rrBig.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("expected 413 on >25 MiB body, got %d", rrBig.Code)
	}

	// SEC-14: unknown repo push gets 202; duplicate X-GitHub-Delivery deduped with same 202 (SEC-13)
	pushBody := []byte(`{"ref":"refs/heads/main","after":"abc123","repository":{"full_name":"unknown/repo"}}`)
	sig := signPayload(secret, pushBody)
	for i := 0; i < 2; i++ {
		req := httptest.NewRequest(http.MethodPost, "/hooks/github-app", bytes.NewReader(pushBody))
		req.Header.Set("X-GitHub-Delivery", "deliv-push-1")
		req.Header.Set("X-GitHub-Event", "push")
		req.Header.Set("X-Hub-Signature-256", sig)
		rr := httptest.NewRecorder()
		srv.ServeHTTP(rr, req)
		if rr.Code != http.StatusAccepted {
			t.Fatalf("push %d expected 202, got %d", i, rr.Code)
		}
	}
	last, err := s.GetLastWebhookDelivery(context.Background())
	if err != nil || last == nil || last.DeliveryID != "deliv-push-1" || last.Status != "accepted" {
		t.Fatalf("unexpected last delivery: %+v (err=%v)", last, err)
	}

	// SEC-12 / GHA-8: unknown event (e.g. star) returns 202 and is marked ignored
	starBody := []byte(`{"action":"created","repository":{"full_name":"r3p-dev/dermaga-test-app"}}`)
	reqStar := httptest.NewRequest(http.MethodPost, "/hooks/github-app", bytes.NewReader(starBody))
	reqStar.Header.Set("X-GitHub-Delivery", "deliv-star-1")
	reqStar.Header.Set("X-GitHub-Event", "star")
	reqStar.Header.Set("X-Hub-Signature-256", signPayload(secret, starBody))
	rrStar := httptest.NewRecorder()
	srv.ServeHTTP(rrStar, reqStar)
	if rrStar.Code != http.StatusAccepted {
		t.Fatalf("expected 202 on ignored event, got %d", rrStar.Code)
	}
	last, _ = s.GetLastWebhookDelivery(context.Background())
	if last == nil || last.Status != "ignored" {
		t.Fatalf("expected ignored status for star event, got %+v", last)
	}

	// SEC-13: per-repo rate limit returns 429 after burst
	rateHit := false
	for i := 0; i < 15; i++ {
		req := httptest.NewRequest(http.MethodPost, "/hooks/github-app", bytes.NewReader(pushBody))
		req.Header.Set("X-GitHub-Delivery", "deliv-rate-"+string(rune('a'+i)))
		req.Header.Set("X-GitHub-Event", "push")
		req.Header.Set("X-Hub-Signature-256", sig)
		rr := httptest.NewRecorder()
		srv.ServeHTTP(rr, req)
		if rr.Code == http.StatusTooManyRequests {
			rateHit = true
			break
		}
	}
	if !rateHit {
		t.Error("expected 429 rate limit after burst")
	}
}

func TestWebhookInstallationEvents_GHA7(t *testing.T) {
	h, s, secret, _ := setupWebhookTest(t)
	srv := h.Handler()
	ctx := context.Background()

	_ = s.ReplaceGitHubInstallationsAndRepos(ctx,
		[]store.GitHubInstallation{{ID: 55, AccountLogin: "r3p-dev", AccountType: "User"}},
		[]store.GitHubRepository{
			{ID: 101, InstallationID: 55, FullName: "r3p-dev/repo-a", Owner: "r3p-dev", Name: "repo-a"},
			{ID: 102, InstallationID: 55, FullName: "r3p-dev/repo-b", Owner: "r3p-dev", Name: "repo-b"},
		},
	)

	// Remove repo-a via installation_repositories
	remBody := []byte(`{"action":"removed","installation":{"id":55},"repositories_removed":[{"id":101}]}`)
	reqRem := httptest.NewRequest(http.MethodPost, "/hooks/github-app", bytes.NewReader(remBody))
	reqRem.Header.Set("X-GitHub-Delivery", "deliv-rem-1")
	reqRem.Header.Set("X-GitHub-Event", "installation_repositories")
	reqRem.Header.Set("X-Hub-Signature-256", signPayload(secret, remBody))
	rrRem := httptest.NewRecorder()
	srv.ServeHTTP(rrRem, reqRem)
	if rrRem.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d", rrRem.Code)
	}
	repos, _ := s.ListGitHubRepositories(ctx)
	if len(repos) != 1 || repos[0].ID != 102 {
		t.Fatalf("expected only repo-b remaining, got %+v", repos)
	}

	// Suspend installation
	suspBody := []byte(`{"action":"suspend","installation":{"id":55,"account":{"login":"r3p-dev","type":"User"}}}`)
	reqSusp := httptest.NewRequest(http.MethodPost, "/hooks/github-app", bytes.NewReader(suspBody))
	reqSusp.Header.Set("X-GitHub-Delivery", "deliv-susp-1")
	reqSusp.Header.Set("X-GitHub-Event", "installation")
	reqSusp.Header.Set("X-Hub-Signature-256", signPayload(secret, suspBody))
	rrSusp := httptest.NewRecorder()
	srv.ServeHTTP(rrSusp, reqSusp)
	if rrSusp.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d", rrSusp.Code)
	}
	insts, _ := s.ListGitHubInstallations(ctx)
	if len(insts) != 1 || insts[0].SuspendedAt == nil {
		t.Fatalf("expected installation suspended, got %+v", insts)
	}

	// Delete installation cascades repos
	delBody := []byte(`{"action":"deleted","installation":{"id":55,"account":{"login":"r3p-dev","type":"User"}}}`)
	reqDel := httptest.NewRequest(http.MethodPost, "/hooks/github-app", bytes.NewReader(delBody))
	reqDel.Header.Set("X-GitHub-Delivery", "deliv-del-1")
	reqDel.Header.Set("X-GitHub-Event", "installation")
	reqDel.Header.Set("X-Hub-Signature-256", signPayload(secret, delBody))
	rrDel := httptest.NewRecorder()
	srv.ServeHTTP(rrDel, reqDel)
	if rrDel.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d", rrDel.Code)
	}
	insts, _ = s.ListGitHubInstallations(ctx)
	repos, _ = s.ListGitHubRepositories(ctx)
	if len(insts) != 0 || len(repos) != 0 {
		t.Fatalf("expected 0 installations and 0 repos after uninstall, got %d/%d", len(insts), len(repos))
	}
}
