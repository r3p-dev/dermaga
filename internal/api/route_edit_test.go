package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/r3p-dev/dermaga/internal/apps"
	"github.com/r3p-dev/dermaga/internal/audit"
	"github.com/r3p-dev/dermaga/internal/deploy"
	"github.com/r3p-dev/dermaga/internal/spec"
	"github.com/r3p-dev/dermaga/internal/traefik"
)

// routeHost is the part of deploy.Host a route edit uses. Like Traefik, Verify
// only succeeds while the written file carries the token router, and reject
// makes it behave as if Traefik refused files containing that text.
type routeHost struct {
	deploy.Host // nil: any other host call in a route edit is a bug and panics
	mu          sync.Mutex
	file        []byte
	writes      int
	reject      string
	inVerify    chan struct{} // if set: Verify signals here, then waits for release
	release     chan struct{}
	tokens      []string // every verify token seen
	log         string   // what Traefik "logged"
}

func (h *routeHost) ProxyLog(context.Context, time.Time) (string, error) { return h.log, nil }

func (h *routeHost) WriteAppFile(_ string, b []byte) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.file, h.writes = b, h.writes+1
	return nil
}

func (h *routeHost) ReadAppFile(string) ([]byte, error) { return h.file, nil }

func (h *routeHost) Verify(_ context.Context, _, _, token string, _ bool, _ io.Writer) error {
	if h.inVerify != nil {
		h.inVerify <- struct{}{}
		<-h.release
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.tokens = append(h.tokens, token)
	if !strings.Contains(string(h.file), token) || (h.reject != "" && strings.Contains(string(h.file), h.reject)) {
		return errors.New("probe: no success within 10s")
	}
	return nil
}

// liveApp creates app "shop" with a live release and its steady route file.
func liveApp(t *testing.T) (*Server, *routeHost, apps.App) {
	t.Helper()
	s := newServer(t)
	h := &routeHost{}
	s.Engine = &deploy.Engine{Apps: s.Apps, Host: h, Site: traefik.Site{}}
	ctx := context.Background()
	id, _, err := s.Apps.Create(ctx, spec.App{
		Name: "shop", Port: 80, Domains: []spec.Domain{{Host: "shop.example.com", Primary: true}},
		Source: spec.Source{Type: "image", Image: "docker.io/library/nginx:1.27.0"},
	})
	if err != nil {
		t.Fatal(err)
	}
	r, _ := s.Apps.NewRelease(ctx, id, "manual", "")
	r.Image = "img:1"
	if err := s.Apps.PromoteLive(ctx, id, r); err != nil {
		t.Fatal(err)
	}
	a, err := s.Apps.Get(ctx, "shop")
	if err != nil {
		t.Fatal(err)
	}
	h.file, err = traefik.App(a.Spec, "dg-shop-"+a.Live.Rel8, "", traefik.Site{})
	if err != nil {
		t.Fatal(err)
	}
	return s, h, a
}

// edit PUTs the app with its stored domains and the given middleware JSON.
func edit(s *Server, a apps.App, domains, middleware string) *httptest.ResponseRecorder {
	if domains == "" {
		domains = `[{"host":"shop.example.com","primary":true}]`
	}
	body := `{"domains":` + domains + `,"spec":{"port":80,"source":{"type":"image","image":"docker.io/library/nginx:1.27.0"},"middleware":` + middleware + `}}`
	return do(s.Socket(), req{method: "PUT", path: "/api/apps/shop", body: body})
}

func storedSpec(t *testing.T, s *Server) spec.App {
	t.Helper()
	a, err := s.Apps.Get(context.Background(), "shop")
	if err != nil {
		t.Fatal(err)
	}
	return a.Spec
}

func lastAudit(t *testing.T, s *Server) audit.Entry {
	t.Helper()
	es, err := audit.List(s.DB, 1)
	if err != nil || len(es) == 0 {
		t.Fatalf("audit: %v", err)
	}
	return es[0]
}

// A route edit is served through Traefik before it is stored (D7); when
// Traefik does not serve it, the previous file and DB row stay (D8).
func TestRouteEditVerifiedAndRolledBack(t *testing.T) {
	s, h, a := liveApp(t)
	before := string(h.file)

	w := edit(s, a, "", `{"headers":{"set":{"X-Test":"one"}}}`)
	if w.Code != http.StatusOK {
		t.Fatalf("edit: %d %s", w.Code, w.Body)
	}
	if h.writes != 2 || !strings.Contains(string(h.file), "X-Test") || strings.Contains(string(h.file), traefik.VerifyHeader) {
		t.Errorf("after a good edit: %d writes, want the token file then the steady file without the token router:\n%s", h.writes, h.file)
	}
	if got := storedSpec(t, s).Middleware.Headers.Set["X-Test"]; got != "one" {
		t.Errorf("stored header %q, want one", got)
	}
	if e := lastAudit(t, s); e.Action != "app.edit" || e.Target != "shop" {
		t.Errorf("audit after success: %+v", e)
	}

	// Traefik "rejects" the next edit: file and DB row must be as before it.
	good := string(h.file)
	h.reject, h.writes = "X-Broken", 0
	h.log = `{"level":"error","error":"collecting file configs: /etc/traefik/dynamic/app-other.yml: yaml: line 3","message":"Error occurred during watcher callback"}
{"level":"error","error":"collecting file configs: /etc/traefik/dynamic/app-shop.yml: template: :74: unexpected \"{\" in command","message":"Error occurred during watcher callback"}`
	w = edit(s, a, "", `{"headers":{"set":{"X-Broken":"two"}}}`)
	if w.Code != http.StatusBadGateway || !strings.Contains(w.Body.String(), "not served through Traefik within 10s") || !strings.Contains(w.Body.String(), "previous route was restored") {
		t.Errorf("rejected edit: %d %s; want 502 naming the timeout and the restore", w.Code, w.Body)
	}
	// The error and the audit row quote Traefik's reason for this app's file
	// and never carry a verify token, the probe command or another app's error.
	detail := lastAudit(t, s).Details
	for where, text := range map[string]string{"API error": w.Body.String(), "audit detail": detail} {
		if !strings.Contains(text, "Traefik logged: Error occurred during watcher callback: collecting file configs: /etc/traefik/dynamic/app-shop.yml: template: :74") {
			t.Errorf("%s does not quote Traefik's reason: %s", where, text)
		}
		for _, bad := range append([]string{"app-other.yml", "podman", "probe", traefik.VerifyHeader}, h.tokens...) {
			if strings.Contains(text, bad) {
				t.Errorf("%s contains %q: %s", where, bad, text)
			}
		}
	}
	if len(h.tokens) < 3 {
		t.Fatalf("test bug: only %d verify tokens seen", len(h.tokens))
	}
	// Without a log line for this app the error says so plainly.
	h.log = `{"level":"error","error":"/etc/traefik/dynamic/app-other.yml: broken"}`
	if w2 := edit(s, a, "", `{"headers":{"set":{"X-Broken":"three"}}}`); !strings.Contains(w2.Body.String(), "no error was logged for this app") || strings.Contains(w2.Body.String(), "app-other.yml") {
		t.Errorf("no log line for this app: %s", w2.Body)
	}
	h.writes -= 3 // the second rejected edit wrote three times as well
	if string(h.file) != good {
		t.Errorf("the route file was not restored:\n%s", h.file)
	}
	if h.writes != 3 { // new+token, restored+token, restored steady
		t.Errorf("%d writes during the rollback, want 3", h.writes)
	}
	sp := storedSpec(t, s)
	if _, ok := sp.Middleware.Headers.Set["X-Broken"]; ok || sp.Middleware.Headers.Set["X-Test"] != "one" {
		t.Errorf("the DB row changed although verification failed: %+v", sp.Middleware.Headers)
	}
	if e := lastAudit(t, s); e.Action != "app.edit_failed" || e.Target != "shop" || !strings.Contains(e.Details, "not served through Traefik") {
		t.Errorf("audit after failure: %+v", e)
	}
	if before == good {
		t.Fatal("test bug: the first edit did not change the file")
	}
}

// Control characters and separators never reach a route file or the DB: the
// HTTP API answers 400 and writes nothing.
func TestRouteEditRejectsInjection(t *testing.T) {
	s, h, a := liveApp(t)
	before, row := string(h.file), storedSpec(t, s)
	q := func(v string) string { b, _ := json.Marshal(v); return string(b) }
	cases := map[string][2]string{ // name → {domains JSON ("" = unchanged), middleware JSON}
		"header name CRLF":       {"", `{"headers":{"set":{` + q("X-A\r\nX-Evil") + `:"v"}}}`},
		"header name colon":      {"", `{"headers":{"set":{` + q("X-A: v\nX-B") + `:"v"}}}`},
		"header name space":      {"", `{"headers":{"set":{"X A":"v"}}}`},
		"header value CRLF":      {"", `{"headers":{"set":{"X-A":` + q("v\r\nSet-Cookie: admin=1") + `}}}`},
		"header value LF":        {"", `{"headers":{"set":{"X-A":` + q("v\nX-B: 1") + `}}}`},
		"header value NUL":       {"", `{"headers":{"set":{"X-A":` + q("v\x00w") + `}}}`},
		"header value DEL":       {"", `{"headers":{"set":{"X-A":` + q("v\x7fw") + `}}}`},
		"header value ESC":       {"", `{"headers":{"set":{"X-A":` + q("v\x1b[31m") + `}}}`},
		"removed header CRLF":    {"", `{"headers":{"remove":[` + q("Server\r\nX-Evil") + `]}}`},
		"basic auth user CRLF":   {"", `{"basicAuth":[{"user":` + q("ad\r\nmin") + `,"password":"long-enough-password"}]}`},
		"basic auth user colon":  {"", `{"basicAuth":[{"user":"ad:min","password":"long-enough-password"}]}`},
		"basic auth user space":  {"", `{"basicAuth":[{"user":"ad min","password":"long-enough-password"}]}`},
		"basic auth pw newline":  {"", `{"basicAuth":[{"user":"admin","password":` + q("long-enough\npassword") + `}]}`},
		"path prefix CRLF":       {"", `{"pathPrefix":` + q("/app\r\nX: 1") + `}`},
		"path prefix backtick":   {"", `{"pathPrefix":` + q("/app`) || Host(`evil.example") + `}`},
		"path prefix dot-dot":    {"", `{"pathPrefix":"/app/../admin"}`},
		"domain CRLF":            {`[{"host":` + q("shop.example.com\r\nevil.example") + `,"primary":true}]`, `{}`},
		"domain backtick":        {`[{"host":` + q("shop.example.com`) || Host(`evil.example") + `,"primary":true}]`, `{}`},
		"domain space":           {`[{"host":"shop example.com","primary":true}]`, `{}`},
		"domain NUL":             {`[{"host":` + q("shop.example.com\x00") + `,"primary":true}]`, `{}`},
		"ip allowlist injection": {"", `{"ipAllowList":[` + q("10.0.0.1\n0.0.0.0/0") + `]}`},
	}
	for name, c := range cases {
		w := edit(s, a, c[0], c[1])
		if w.Code != http.StatusBadRequest {
			t.Errorf("%s: %d %s, want 400", name, w.Code, w.Body)
		}
	}
	// Health path is part of the spec, not the middleware.
	body := `{"domains":[{"host":"shop.example.com","primary":true}],"spec":{"port":80,"source":{"type":"image","image":"docker.io/library/nginx:1.27.0"},"health":{"path":` + q("/healthz\r\nX: 1") + `}}}`
	if w := do(s.Socket(), req{method: "PUT", path: "/api/apps/shop", body: body}); w.Code != http.StatusBadRequest {
		t.Errorf("health path CRLF: %d %s, want 400", w.Code, w.Body)
	}
	if h.writes != 0 || string(h.file) != before {
		t.Errorf("a rejected request wrote the route file (%d writes)", h.writes)
	}
	after := storedSpec(t, s)
	b1, _ := json.Marshal(row)
	b2, _ := json.Marshal(after)
	if string(b1) != string(b2) {
		t.Errorf("a rejected request changed the DB row:\n%s\n%s", b1, b2)
	}

	// A domain of another app is refused before any route is written.
	if _, _, err := s.Apps.Create(context.Background(), spec.App{Name: "other", Port: 80,
		Domains: []spec.Domain{{Host: "other.example.com", Primary: true}}, Source: spec.Source{Type: "image", Image: "docker.io/library/nginx:1.27.0"}}); err != nil {
		t.Fatal(err)
	}
	w := edit(s, a, `[{"host":"shop.example.com","primary":true},{"host":"other.example.com"}]`, `{}`)
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "already belongs to another app") || h.writes != 0 {
		t.Errorf("foreign domain: %d %s, %d writes; want 400 and no write", w.Code, w.Body, h.writes)
	}
}

// Basic-auth passwords arrive in plaintext, are hashed by the daemon and never
// come back; a user sent without a password keeps the stored hash.
func TestBasicAuthHashedOnServer(t *testing.T) {
	s, h, a := liveApp(t)
	const pass = "correct horse battery"
	hashOf := func(user string) string {
		for _, u := range storedSpec(t, s).Middleware.BasicAuth {
			if u.User == user {
				return u.Hash
			}
		}
		return ""
	}
	leak := func(where, body string) {
		t.Helper()
		if strings.Contains(body, pass) || strings.Contains(body, "$2a$") || strings.Contains(body, `"hash"`) || strings.Contains(body, `"password"`) {
			t.Errorf("%s leaks a password or hash: %s", where, body)
		}
	}

	w := edit(s, a, "", `{"basicAuth":[{"user":"admin","password":"`+pass+`"}]}`)
	if w.Code != http.StatusOK {
		t.Fatalf("add user: %d %s", w.Code, w.Body)
	}
	leak("the PUT response", w.Body.String())
	first := hashOf("admin")
	if bcrypt.CompareHashAndPassword([]byte(first), []byte(pass)) != nil {
		t.Fatalf("the stored hash does not verify the password: %q", first)
	}
	if !strings.Contains(string(h.file), first) || strings.Contains(string(h.file), pass) {
		t.Errorf("the route file must carry the hash and never the password")
	}
	leak("GET /api/apps/shop", do(s.Socket(), req{method: "GET", path: "/api/apps/shop"}).Body.String())
	leak("GET /api/apps", do(s.Socket(), req{method: "GET", path: "/api/apps"}).Body.String())

	// Round trip without a password (what every other edit sends): hash kept.
	// A hash sent by a client is ignored.
	if w := edit(s, a, "", `{"basicAuth":[{"user":"admin","hash":"$2a$10$abcdefghijklmnopqrstuvABCDEFGHIJKLMNOPQRSTUVWXYZ01234"}],"noCompress":true}`); w.Code != http.StatusOK || hashOf("admin") != first {
		t.Errorf("edit without a password: %d, hash kept = %v", w.Code, hashOf("admin") == first)
	}
	// A new password replaces the hash.
	if w := edit(s, a, "", `{"basicAuth":[{"user":"admin","password":"another long password"}]}`); w.Code != http.StatusOK || hashOf("admin") == first ||
		bcrypt.CompareHashAndPassword([]byte(hashOf("admin")), []byte("another long password")) != nil {
		t.Errorf("password change: %d", w.Code)
	}
	for name, mw := range map[string]string{
		"new user without a password": `{"basicAuth":[{"user":"bob"}]}`,
		"new user with only a hash":   `{"basicAuth":[{"user":"bob","hash":"$2a$10$abcdefghijklmnopqrstuvABCDEFGHIJKLMNOPQRSTUVWXYZ01234"}]}`,
		"short password":              `{"basicAuth":[{"user":"bob","password":"short"}]}`,
		"password over 72 bytes":      `{"basicAuth":[{"user":"bob","password":"` + strings.Repeat("x", 73) + `"}]}`,
	} {
		if w := edit(s, a, "", mw); w.Code != http.StatusBadRequest {
			t.Errorf("%s: %d %s, want 400", name, w.Code, w.Body)
		}
	}
	// Removing the user opens the route again.
	if w := edit(s, a, "", `{}`); w.Code != http.StatusOK || len(storedSpec(t, s).Middleware.BasicAuth) != 0 || strings.Contains(string(h.file), "basicAuth") {
		t.Errorf("remove user: %d, file still has basicAuth = %v", w.Code, strings.Contains(string(h.file), "basicAuth"))
	}
	// The spec validator refuses to store a plaintext password, whoever calls it.
	sp := storedSpec(t, s)
	sp.Middleware.BasicAuth = []spec.BasicAuthUser{{User: "x", Password: "plaintext-password", Hash: first}}
	if err := sp.Validate(); err == nil {
		t.Error("a spec with a plaintext password validated")
	}
}

// An edit and a deploy of the same app share one lock and never interleave.
func TestRouteEditTakesTheDeployLock(t *testing.T) {
	s, h, a := liveApp(t)

	// 1. While something else (a deploy) holds the app's lock, an edit waits.
	unlock := s.Engine.Lock("shop")
	done := make(chan int, 1)
	go func() { done <- edit(s, a, "", `{"noCompress":true}`).Code }()
	select {
	case code := <-done:
		t.Fatalf("the edit finished (%d) while the app's lock was held", code)
	case <-time.After(150 * time.Millisecond):
	}
	if h.writes != 0 {
		t.Fatalf("the edit wrote the route while the app's lock was held")
	}
	unlock()
	if code := <-done; code != http.StatusOK {
		t.Fatalf("edit after unlock: %d", code)
	}

	// 2. While an edit waits for Traefik, a deploy (Lock) cannot start.
	h.inVerify, h.release = make(chan struct{}), make(chan struct{})
	go func() { done <- edit(s, a, "", `{}`).Code }()
	<-h.inVerify // the edit is inside D7 verification, holding the lock
	locked := make(chan struct{})
	go func() { defer s.Engine.Lock("shop")(); close(locked) }()
	select {
	case <-locked:
		t.Fatal("a deploy could take the app's lock in the middle of a route edit")
	case <-time.After(150 * time.Millisecond):
	}
	close(h.release)
	if code := <-done; code != http.StatusOK {
		t.Fatalf("edit: %d", code)
	}
	select {
	case <-locked:
	case <-time.After(2 * time.Second):
		t.Fatal("the lock was not released after the edit")
	}
}

func TestEffectiveHeadersEndpoint(t *testing.T) {
	s, _, a := liveApp(t)
	if w := edit(s, a, "", `{"headers":{"set":{"x-frame-options":"DENY","X-Custom":"1","Cache-Control":""},"remove":["referrer-policy"]}}`); w.Code != http.StatusOK {
		t.Fatalf("edit: %d %s", w.Code, w.Body)
	}
	w := do(s.Socket(), req{method: "GET", path: "/api/apps/shop/headers"})
	var v EffectiveHeadersView
	if err := json.Unmarshal(w.Body.Bytes(), &v); err != nil || w.Code != http.StatusOK {
		t.Fatalf("headers: %d %s", w.Code, w.Body)
	}
	got := map[string]EffectiveHeader{}
	var names []string
	for _, h := range v.Headers {
		got[h.Name] = h
		names = append(names, h.Name)
	}
	if !sortedStrings(names) {
		t.Errorf("headers not sorted: %v", names)
	}
	want := map[string]EffectiveHeader{
		"X-Frame-Options":           {Name: "X-Frame-Options", Value: "DENY", Source: "app"}, // override of a default, canonicalised
		"X-Custom":                  {Name: "X-Custom", Value: "1", Source: "app"},           // added
		"Cache-Control":             {Name: "Cache-Control", Value: "", Source: "app"},       // stripped by the app
		"Server":                    {Name: "Server", Value: "", Source: "default"},          // stripped by default
		"X-Content-Type-Options":    {Name: "X-Content-Type-Options", Value: "nosniff", Source: "default"},
		"Strict-Transport-Security": {Name: "Strict-Transport-Security", Value: spec.DefaultHeaders["Strict-Transport-Security"], Source: "default"},
	}
	for n, wh := range want {
		if got[n] != wh {
			t.Errorf("%s: %+v, want %+v", n, got[n], wh)
		}
	}
	if _, ok := got["Referrer-Policy"]; ok || v.DefaultsOff {
		t.Errorf("removed default still present, or defaultsOff set: %+v", v)
	}
	if w := do(s.Socket(), req{method: "GET", path: "/api/apps/nope/headers"}); w.Code != http.StatusNotFound {
		t.Errorf("unknown app: %d", w.Code)
	}
	if w := do(s.Panel(), req{method: "GET", path: "/api/apps/shop/headers"}); w.Code != http.StatusUnauthorized {
		t.Errorf("panel without a session: %d", w.Code)
	}
}

func sortedStrings(s []string) bool {
	for i := 1; i < len(s); i++ {
		if s[i-1] > s[i] {
			return false
		}
	}
	return true
}

// An app without a live release has no route: its edit is stored and nothing
// is written or verified. The first deploy renders it (deploy.TestFirstDeployRendersStoredMiddleware).
func TestEditWithoutLiveReleaseIsStoredOnly(t *testing.T) {
	s := newServer(t)
	h := &routeHost{}
	s.Engine = &deploy.Engine{Apps: s.Apps, Host: h, Site: traefik.Site{}}
	if _, _, err := s.Apps.Create(context.Background(), spec.App{Name: "shop", Port: 80,
		Domains: []spec.Domain{{Host: "shop.example.com", Primary: true}}, Source: spec.Source{Type: "image", Image: "docker.io/library/nginx:1.27.0"}}); err != nil {
		t.Fatal(err)
	}
	if w := edit(s, apps.App{}, "", `{"headers":{"set":{"X-Test":"one"}}}`); w.Code != http.StatusOK {
		t.Fatalf("edit: %d %s", w.Code, w.Body)
	}
	if h.writes != 0 || len(h.tokens) != 0 || storedSpec(t, s).Middleware.Headers.Set["X-Test"] != "one" {
		t.Errorf("edit without a live release: %d writes, %d verifications, stored %v", h.writes, len(h.tokens), storedSpec(t, s).Middleware.Headers.Set)
	}
	if w := edit(s, apps.App{}, "", `{"headers":{"set":{"X-Test":"a\r\nb"}}}`); w.Code != http.StatusBadRequest {
		t.Errorf("invalid edit without a live release: %d, want 400", w.Code)
	}
}
