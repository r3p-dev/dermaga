// Package webhook serves POST /hooks/github/{appId} on hooks.sock (BRIEF
// §4.4, §5): HMAC-SHA256 checked before any JSON parsing, 1 MiB body limit,
// push (or workflow_run for the CI gate) on the configured repo and branch
// only, dedupe on X-GitHub-Delivery, a per-app rate limit, then 202 and enqueue.
package webhook

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/r3p-dev/dermaga/internal/apps"
	"github.com/r3p-dev/dermaga/internal/spec"
)

const MaxBody = 1 << 20

// Handler needs to look up apps, remember deliveries and enqueue deploys.
type Handler struct {
	Lookup func(ctx context.Context, id string) (apps.App, string, error) // app and webhook secret
	Record func(ctx context.Context, delivery, appID, result string) (dup bool, err error)
	Deploy func(ctx context.Context, app, ref string) error
	Audit  func(action, target, details string)

	once    sync.Once
	dummy   []byte // HMAC key for unknown app ids, so they cost the same as known ones
	mu      sync.Mutex
	buckets map[string]*bucket
	now     func() time.Time
	mac     func(key, body []byte) []byte // computed for every request (tests count calls)
}

func (h *Handler) init() {
	h.once.Do(func() {
		h.dummy = make([]byte, 32)
		rand.Read(h.dummy)
		h.buckets = map[string]*bucket{}
		if h.now == nil {
			h.now = time.Now
		}
		if h.mac == nil {
			h.mac = sign
		}
	})
}

func sign(key, body []byte) []byte {
	m := hmac.New(sha256.New, key)
	m.Write(body)
	return m.Sum(nil)
}

// Mux returns the hook listener's handler: nothing else is reachable on it.
func (h *Handler) Mux() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /hooks/github/{appId}", h.serve)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { io.WriteString(w, "ok\n") })
	return mux
}

var idRE = regexp.MustCompile(`^[0-9a-f]{16}$`)

func reply(w http.ResponseWriter, code int, result string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"result": result})
}

func (h *Handler) serve(w http.ResponseWriter, r *http.Request) {
	h.init()
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, MaxBody))
	if err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			reply(w, http.StatusRequestEntityTooLarge, "body larger than 1 MiB")
			return
		}
		reply(w, http.StatusBadRequest, "unreadable body")
		return
	}

	// Same work for unknown and known ids: one lookup, one HMAC, one comparison.
	id := r.PathValue("appId")
	key, known := h.dummy, false
	var app apps.App
	if idRE.MatchString(id) {
		a, secret, err := h.Lookup(r.Context(), id)
		switch {
		case err == nil:
			app, key, known = a, []byte(secret), true
		case !errors.Is(err, apps.ErrNotFound):
			slog.Error("webhook: app lookup", "err", err)
			reply(w, http.StatusInternalServerError, "internal error")
			return
		}
	}
	want := h.mac(key, body)
	got, _ := hex.DecodeString(strings.TrimPrefix(r.Header.Get("X-Hub-Signature-256"), "sha256="))
	valid := hmac.Equal(want, got) && strings.HasPrefix(r.Header.Get("X-Hub-Signature-256"), "sha256=")
	if !known {
		reply(w, http.StatusNotFound, "not found")
		return
	}
	if !valid {
		reply(w, http.StatusUnauthorized, "bad signature")
		return
	}

	// Signed by GitHub with this app's secret from here on.
	name := app.Spec.Name
	if !h.allow(id) {
		h.Audit("webhook.rate_limited", name, "")
		reply(w, http.StatusTooManyRequests, "rate limited")
		return
	}
	delivery := r.Header.Get("X-GitHub-Delivery")
	if delivery == "" || len(delivery) > 100 {
		reply(w, http.StatusBadRequest, "missing X-GitHub-Delivery")
		return
	}
	event := r.Header.Get("X-GitHub-Event")
	ref, why := decide(app.Spec, event, body)
	result := "queued"
	if ref == "" {
		result = "ignored: " + why
	}
	dup, err := h.Record(r.Context(), delivery, id, result)
	if err != nil {
		slog.Error("webhook: recording delivery", "err", err)
		reply(w, http.StatusInternalServerError, "internal error")
		return
	}
	if dup {
		reply(w, http.StatusAccepted, "duplicate delivery, ignored")
		return
	}
	if ref == "" {
		h.Audit("webhook.ignored", name, event+" "+delivery+": "+why)
		reply(w, http.StatusAccepted, result)
		return
	}
	if err := h.Deploy(r.Context(), name, ref); err != nil {
		slog.Error("webhook: enqueue", "app", name, "err", err)
		reply(w, http.StatusInternalServerError, "internal error")
		return
	}
	h.Audit("webhook.deploy", name, event+" "+delivery+" "+ref)
	reply(w, http.StatusAccepted, result)
}

type payload struct {
	Ref        string `json:"ref"`
	After      string `json:"after"`
	Deleted    bool   `json:"deleted"`
	Repository struct {
		FullName string `json:"full_name"`
	} `json:"repository"`
	Commits []struct {
		Added    []string `json:"added"`
		Removed  []string `json:"removed"`
		Modified []string `json:"modified"`
	} `json:"commits"`
	Action      string `json:"action"`
	WorkflowRun struct {
		Name       string `json:"name"`
		Path       string `json:"path"`
		HeadBranch string `json:"head_branch"`
		HeadSHA    string `json:"head_sha"`
		Conclusion string `json:"conclusion"`
		Event      string `json:"event"`
	} `json:"workflow_run"`
}

var shaRE = regexp.MustCompile(`^[0-9a-f]{40}$`)

// decide returns the commit to deploy, or "" and the reason to ignore the delivery.
func decide(a spec.App, event string, body []byte) (ref, why string) {
	src := a.Source
	if src.Type != "git" {
		return "", "not a git app"
	}
	trigger := src.Trigger
	if trigger == "" {
		trigger = "push"
	}
	if event == "ping" {
		return "", "ping"
	}
	if event != trigger {
		return "", "event " + event + " (this app deploys on " + trigger + ")"
	}
	var p payload
	if err := json.Unmarshal(body, &p); err != nil {
		return "", "payload is not JSON"
	}
	if !strings.EqualFold(p.Repository.FullName, src.RepoFullName()) {
		return "", "repository " + p.Repository.FullName + " is not " + src.RepoFullName()
	}
	if !src.AutoDeploy {
		return "", "auto-deploy is off"
	}
	if trigger == "workflow_run" {
		w := p.WorkflowRun
		switch {
		case p.Action != "completed":
			return "", "workflow_run " + p.Action
		case w.Conclusion != "success":
			return "", "workflow concluded " + w.Conclusion
		case w.HeadBranch != src.Branch:
			return "", "branch " + w.HeadBranch + " is not " + src.Branch
		case w.Name != src.Workflow && w.Path != src.Workflow:
			return "", "workflow " + w.Name + " is not " + src.Workflow
		case w.Event == "pull_request":
			return "", "workflow ran for a pull request"
		case !shaRE.MatchString(w.HeadSHA):
			return "", "no head_sha"
		}
		return w.HeadSHA, "" // no file list in workflow_run: watch paths fail open
	}
	switch {
	case p.Ref != "refs/heads/"+src.Branch:
		return "", "ref " + p.Ref + " is not refs/heads/" + src.Branch
	case p.Deleted:
		return "", "branch deleted"
	case !shaRE.MatchString(p.After):
		return "", "no commit SHA"
	}
	if len(src.WatchPaths) > 0 {
		var files []string
		for _, c := range p.Commits {
			files = append(append(append(files, c.Added...), c.Removed...), c.Modified...)
		}
		if len(files) > 0 && !anyMatch(src.WatchPaths, files) { // no file list: fail open (BRIEF §5)
			return "", "no changed file matches the watch paths"
		}
	}
	return p.After, ""
}

func anyMatch(globs, files []string) bool {
	for _, g := range globs {
		re := globRE(g)
		for _, f := range files {
			if re.MatchString(f) {
				return true
			}
		}
	}
	return false
}

// globRE: * matches within a path segment, ** across segments, ? one character.
func globRE(g string) *regexp.Regexp {
	var b strings.Builder
	b.WriteString("^")
	for i := 0; i < len(g); i++ {
		switch c := g[i]; {
		case c == '*' && i+1 < len(g) && g[i+1] == '*':
			i++
			if i+1 < len(g) && g[i+1] == '/' { // "**/" also matches zero directories
				i++
				b.WriteString("(.*/)?")
			} else {
				b.WriteString(".*")
			}
		case c == '*':
			b.WriteString("[^/]*")
		case c == '?':
			b.WriteString("[^/]")
		default:
			b.WriteString(regexp.QuoteMeta(string(c)))
		}
	}
	b.WriteString("$")
	return regexp.MustCompile(b.String())
}

// Per-app token bucket: bursts of 10, refilled at 1 per 6 s (10 a minute).
type bucket struct {
	tokens float64
	last   time.Time
}

const (
	burst  = 10
	refill = 6 * time.Second
)

func (h *Handler) allow(id string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	now := h.now()
	b := h.buckets[id]
	if b == nil {
		b = &bucket{tokens: burst, last: now}
		h.buckets[id] = b
	}
	b.tokens = min(burst, b.tokens+now.Sub(b.last).Seconds()/refill.Seconds())
	b.last = now
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}
