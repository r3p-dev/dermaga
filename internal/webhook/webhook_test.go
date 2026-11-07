package webhook

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/r3p-dev/dermaga/internal/apps"
	_ "github.com/r3p-dev/dermaga/internal/golden" // accepts go test ./... -update
	"github.com/r3p-dev/dermaga/internal/spec"
)

const (
	id     = "0123456789abcdef"
	secret = "topsecret-webhook-key"
	sha    = "0f1e2d3c4b5a69788796a5b4c3d2e1f00f1e2d3c"
)

type env struct {
	h        *Handler
	deployed []string
	seen     map[string]bool
	macs     int
	lookups  int
	now      time.Time
	app      spec.App
}

func newEnv() *env {
	e := &env{seen: map[string]bool{}, now: time.Unix(1_700_000_000, 0)}
	e.app = spec.App{Name: "portfolio", Source: spec.Source{Type: "git", Repo: "git@github.com:r3p-dev/portfolio.git", Branch: "main", AutoDeploy: true}}
	e.h = &Handler{
		Lookup: func(_ context.Context, got string) (apps.App, string, error) {
			e.lookups++
			if got != id {
				return apps.App{}, "", apps.ErrNotFound
			}
			return apps.App{ID: id, Spec: e.app}, secret, nil
		},
		Record: func(_ context.Context, d, _, _ string) (bool, error) {
			dup := e.seen[d]
			e.seen[d] = true
			return dup, nil
		},
		Deploy: func(_ context.Context, app, ref string) error {
			e.deployed = append(e.deployed, app+"@"+ref)
			return nil
		},
		Audit: func(string, string, string) {},
		now:   func() time.Time { return e.now },
		mac:   func(k, b []byte) []byte { e.macs++; return sign(k, b) },
	}
	return e
}

func push(ref string, files ...string) []byte {
	b, _ := json.Marshal(map[string]any{
		"ref": ref, "after": sha, "repository": map[string]any{"full_name": "r3p-dev/portfolio"},
		"commits": []any{map[string]any{"added": files, "removed": []string{}, "modified": []string{}}},
	})
	return b
}

var delivery = 0

func (e *env) send(appID, event string, body []byte, sig string) *httptest.ResponseRecorder {
	if sig == "sign" {
		sig = "sha256=" + hex.EncodeToString(sign([]byte(secret), body))
	}
	delivery++
	r := httptest.NewRequest("POST", "/hooks/github/"+appID, bytes.NewReader(body))
	if sig != "" {
		r.Header.Set("X-Hub-Signature-256", sig)
	}
	r.Header.Set("X-GitHub-Event", event)
	r.Header.Set("X-GitHub-Delivery", "d-"+strings.Repeat("x", delivery%3)+string(rune('a'+delivery%26))+time.Now().Format("150405.000000000"))
	w := httptest.NewRecorder()
	e.h.Mux().ServeHTTP(w, r)
	return w
}

func TestSignatureBeforeAnythingElse(t *testing.T) {
	e := newEnv()
	body := push("refs/heads/main")
	for name, c := range map[string]struct {
		id, sig string
		code    int
	}{
		"good":              {id, "sign", 202},
		"missing signature": {id, "", 401},
		"wrong signature":   {id, "sha256=" + strings.Repeat("00", 32), 401},
		"no sha256= prefix": {id, hex.EncodeToString(sign([]byte(secret), body)), 401},
		"unknown id":        {"fedcba9876543210", "sign", 404},
		"malformed id":      {"NOT-AN-ID", "sign", 404},
	} {
		macs := e.macs
		if w := e.send(c.id, "push", body, c.sig); w.Code != c.code {
			t.Errorf("%s: %d %s, want %d", name, w.Code, w.Body, c.code)
		}
		// the HMAC is computed on every path, so unknown ids cost the same as bad signatures
		if e.macs != macs+1 {
			t.Errorf("%s: HMAC computed %d times", name, e.macs-macs)
		}
	}
	// a body that isn't JSON is rejected by the signature, never parsed first
	if w := e.send(id, "push", []byte("{not json"), "sha256=00"); w.Code != 401 {
		t.Errorf("unsigned garbage: %d", w.Code)
	}
	if len(e.deployed) != 1 || e.deployed[0] != "portfolio@"+sha {
		t.Errorf("deployed %v", e.deployed)
	}
}

func TestBodyLimit(t *testing.T) {
	e := newEnv()
	big := append(push("refs/heads/main"), bytes.Repeat([]byte(" "), MaxBody)...)
	if w := e.send(id, "push", big, "sign"); w.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("%d", w.Code)
	}
}

func TestFilters(t *testing.T) {
	e := newEnv()
	other, _ := json.Marshal(map[string]any{"ref": "refs/heads/main", "after": sha, "repository": map[string]any{"full_name": "evil/portfolio"}})
	deleted, _ := json.Marshal(map[string]any{"ref": "refs/heads/main", "after": strings.Repeat("0", 40), "deleted": true, "repository": map[string]any{"full_name": "r3p-dev/portfolio"}})
	for name, c := range map[string]struct {
		event string
		body  []byte
	}{
		"wrong branch":  {"push", push("refs/heads/dev")},
		"tag":           {"push", push("refs/tags/v1")},
		"wrong repo":    {"push", other},
		"wrong event":   {"issues", push("refs/heads/main")},
		"ping":          {"ping", []byte(`{"zen":"hi"}`)},
		"branch delete": {"push", deleted},
	} {
		w := e.send(id, c.event, c.body, "sign")
		if w.Code != 202 || !strings.Contains(w.Body.String(), "ignored") {
			t.Errorf("%s: %d %s", name, w.Code, w.Body)
		}
	}
	e.app.Source.AutoDeploy = false
	if w := e.send(id, "push", push("refs/heads/main"), "sign"); !strings.Contains(w.Body.String(), "auto-deploy is off") {
		t.Errorf("auto-deploy off: %s", w.Body)
	}
	if len(e.deployed) != 0 {
		t.Errorf("deployed %v", e.deployed)
	}
}

func TestDuplicateDelivery(t *testing.T) {
	e := newEnv()
	body := push("refs/heads/main")
	r := func() *httptest.ResponseRecorder {
		req := httptest.NewRequest("POST", "/hooks/github/"+id, bytes.NewReader(body))
		req.Header.Set("X-Hub-Signature-256", "sha256="+hex.EncodeToString(sign([]byte(secret), body)))
		req.Header.Set("X-GitHub-Event", "push")
		req.Header.Set("X-GitHub-Delivery", "same-delivery")
		w := httptest.NewRecorder()
		e.h.Mux().ServeHTTP(w, req)
		return w
	}
	if w := r(); w.Code != 202 || !strings.Contains(w.Body.String(), "queued") {
		t.Fatal(w.Code, w.Body)
	}
	if w := r(); w.Code != 202 || !strings.Contains(w.Body.String(), "duplicate") {
		t.Fatal(w.Code, w.Body)
	}
	if len(e.deployed) != 1 {
		t.Fatalf("deployed %v", e.deployed)
	}
}

func TestRateLimitPerApp(t *testing.T) {
	e := newEnv()
	codes := map[int]int{}
	for range 12 {
		codes[e.send(id, "push", push("refs/heads/main"), "sign").Code]++
	}
	if codes[202] != 10 || codes[429] != 2 {
		t.Fatalf("%v", codes)
	}
	e.now = e.now.Add(6 * time.Second) // one token back
	if w := e.send(id, "push", push("refs/heads/main"), "sign"); w.Code != 202 {
		t.Fatalf("after refill: %d", w.Code)
	}
	// unsigned requests never touch the bucket
	e2 := newEnv()
	for range 20 {
		e2.send(id, "push", push("refs/heads/main"), "sha256=00")
	}
	if w := e2.send(id, "push", push("refs/heads/main"), "sign"); w.Code != 202 {
		t.Fatalf("bucket drained by unsigned requests: %d", w.Code)
	}
}

func TestWatchPaths(t *testing.T) {
	e := newEnv()
	e.app.Source.WatchPaths = []string{"src/**", "Containerfile", "*.ts"}
	for files, want := range map[string]bool{
		"src/pages/index.astro": true,
		"Containerfile":         true,
		"server.ts":             true,
		"scripts/x.ts":          false, // *.ts doesn't cross directories
		"README.md":             false,
		"":                      true, // no file list: fail open
	} {
		var fs []string
		if files != "" {
			fs = []string{files}
		}
		e.deployed = nil
		e.send(id, "push", push("refs/heads/main", fs...), "sign")
		if (len(e.deployed) == 1) != want {
			t.Errorf("%q: deployed %v, want %v", files, e.deployed, want)
		}
	}
	if !globRE("**/*.go").MatchString("main.go") || !globRE("**/*.go").MatchString("a/b/c.go") || globRE("a/*").MatchString("a/b/c") {
		t.Error("glob semantics")
	}
}

func TestWorkflowRunGate(t *testing.T) {
	e := newEnv()
	e.app.Source.Trigger, e.app.Source.Workflow = "workflow_run", "CI"
	run := func(action, conclusion, branch, wf string) []byte {
		b, _ := json.Marshal(map[string]any{"action": action, "repository": map[string]any{"full_name": "r3p-dev/portfolio"},
			"workflow_run": map[string]any{"name": wf, "path": ".github/workflows/ci.yml", "head_branch": branch, "head_sha": sha, "conclusion": conclusion, "event": "push"}})
		return b
	}
	cases := []struct {
		event string
		body  []byte
		ok    bool
	}{
		{"workflow_run", run("completed", "success", "main", "CI"), true},
		{"workflow_run", run("completed", "failure", "main", "CI"), false},
		{"workflow_run", run("requested", "", "main", "CI"), false},
		{"workflow_run", run("completed", "success", "dev", "CI"), false},
		{"workflow_run", run("completed", "success", "main", "Lint"), false},
		{"push", push("refs/heads/main"), false}, // in CI-gate mode a push alone doesn't deploy
	}
	for i, c := range cases {
		e.deployed = nil
		e.send(id, c.event, c.body, "sign")
		if (len(e.deployed) == 1) != c.ok {
			t.Errorf("case %d: deployed %v, want %v", i, e.deployed, c.ok)
		}
	}
	e.app.Source.Workflow = ".github/workflows/ci.yml" // the path works as well as the name
	e.deployed = nil
	e.send(id, "workflow_run", run("completed", "success", "main", "CI"), "sign")
	if len(e.deployed) != 1 {
		t.Error("workflow path not matched")
	}
}

func TestOnlyHookRoutes(t *testing.T) {
	e := newEnv()
	for _, p := range []string{"/api/status", "/", "/hooks/github/" + id + "/x"} {
		w := httptest.NewRecorder()
		e.h.Mux().ServeHTTP(w, httptest.NewRequest("POST", p, nil))
		if w.Code != 404 {
			t.Errorf("%s: %d", p, w.Code)
		}
	}
	w := httptest.NewRecorder()
	e.h.Mux().ServeHTTP(w, httptest.NewRequest("GET", "/hooks/github/"+id, nil))
	if w.Code != 405 {
		t.Errorf("GET on the hook: %d", w.Code)
	}
}
