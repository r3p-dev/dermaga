package api

import (
	"bytes"
	"context"
	"encoding/json"
	_ "github.com/r3p-dev/dermaga/internal/golden" // accepts go test ./... -update
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/r3p-dev/dermaga/internal/apps"
	"github.com/r3p-dev/dermaga/internal/auth"
	"github.com/r3p-dev/dermaga/internal/deploy"
	"github.com/r3p-dev/dermaga/internal/secrets"
	"github.com/r3p-dev/dermaga/internal/spec"
	"github.com/r3p-dev/dermaga/internal/store"
)

const pw = "a-long-enough-password"

func newServer(t *testing.T) *Server {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	box, _ := secrets.New(bytes.Repeat([]byte{3}, 32))
	s := &Server{DB: db, Auth: auth.New(db, box), Port: 7070, Started: time.Now(), Apps: apps.Store{DB: db, Box: box}}
	if err := s.Auth.SetPassword(pw); err != nil {
		t.Fatal(err)
	}
	return s
}

type req struct {
	method, path, host, origin, body string
	cookie                           *http.Cookie
}

func do(h http.Handler, r req) *httptest.ResponseRecorder {
	if r.host == "" {
		r.host = "127.0.0.1:7070"
	}
	q := httptest.NewRequest(r.method, r.path, strings.NewReader(r.body))
	q.Host = r.host
	if r.origin != "" {
		q.Header.Set("Origin", r.origin)
	}
	if r.cookie != nil {
		q.AddCookie(r.cookie)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, q)
	return w
}

func login(t *testing.T, h http.Handler) *http.Cookie {
	t.Helper()
	w := do(h, req{method: "POST", path: "/api/login", origin: "http://127.0.0.1:7070", body: `{"password":"` + pw + `"}`})
	if w.Code != http.StatusNoContent {
		t.Fatalf("login: %d %s", w.Code, w.Body)
	}
	return w.Result().Cookies()[0]
}

func TestHostCheck(t *testing.T) {
	h := newServer(t).Panel()
	for _, host := range []string{"evil.example", "evil.example:7070", "127.0.0.1", "127.0.0.1:7071", "localhost", "[::1]:7070", "10.0.0.5:7070"} {
		if w := do(h, req{method: "GET", path: "/api/status", host: host}); w.Code != http.StatusMisdirectedRequest {
			t.Errorf("Host %q: %d, want 421", host, w.Code)
		}
	}
	for _, host := range []string{"127.0.0.1:7070", "localhost:7070"} {
		if w := do(h, req{method: "GET", path: "/api/status", host: host}); w.Code != http.StatusUnauthorized {
			t.Errorf("Host %q: %d, want 401 (passes Host check)", host, w.Code)
		}
	}
}

func TestOriginCheckOnMutatingRequests(t *testing.T) {
	h := newServer(t).Panel()
	body := `{"password":"` + pw + `"}`
	for _, o := range []string{"", "null", "http://evil.example", "https://127.0.0.1:7070", "http://127.0.0.1:7071", "http://127.0.0.1:7070.evil.example"} {
		if w := do(h, req{method: "POST", path: "/api/login", origin: o, body: body}); w.Code != http.StatusForbidden {
			t.Errorf("Origin %q: %d, want 403", o, w.Code)
		}
	}
	if w := do(h, req{method: "POST", path: "/api/login", origin: "http://localhost:7070", body: body}); w.Code != http.StatusNoContent {
		t.Errorf("same-origin login: %d", w.Code)
	}
}

func TestLoginSessionLogoutAndAudit(t *testing.T) {
	s := newServer(t)
	h := s.Panel()
	if w := do(h, req{method: "POST", path: "/api/login", origin: "http://127.0.0.1:7070", body: `{"password":"nope"}`}); w.Code != http.StatusUnauthorized {
		t.Fatalf("bad password: %d", w.Code)
	}
	c := login(t, h)
	if c.Name != "dermaga_session" || !c.HttpOnly || c.SameSite != http.SameSiteStrictMode || c.Path != "/" || c.Secure || c.MaxAge != 7*24*3600 {
		t.Fatalf("cookie attributes: %+v", c)
	}
	if hdr := do(h, req{method: "POST", path: "/api/login", origin: "http://127.0.0.1:7070", body: `{"password":"` + pw + `"}`}).Header().Get("Set-Cookie"); !strings.Contains(hdr, "HttpOnly") || !strings.Contains(hdr, "SameSite=Strict") || !strings.Contains(hdr, "Path=/") {
		t.Fatalf("Set-Cookie %q", hdr)
	}

	w := do(h, req{method: "GET", path: "/api/status", cookie: c})
	var st Status
	json.Unmarshal(w.Body.Bytes(), &st)
	if w.Code != 200 || !st.AdminPasswordSet || st.Name != "dermaga" || w.Header().Get("X-Frame-Options") != "DENY" {
		t.Fatalf("status: %d %s", w.Code, w.Body)
	}

	if w := do(h, req{method: "POST", path: "/api/logout", origin: "http://127.0.0.1:7070", cookie: c}); w.Code != http.StatusNoContent {
		t.Fatalf("logout: %d", w.Code)
	}
	if w := do(h, req{method: "GET", path: "/api/status", cookie: c}); w.Code != http.StatusUnauthorized {
		t.Fatalf("old cookie after logout: %d", w.Code)
	}

	w = do(s.Socket(), req{method: "GET", path: "/api/audit?limit=10"})
	var entries []struct{ Actor, Action string }
	json.Unmarshal(w.Body.Bytes(), &entries)
	var got []string
	for _, e := range entries {
		got = append(got, e.Actor+":"+e.Action)
	}
	if want := "panel:auth.logout panel:auth.login panel:auth.login panel:auth.login_failed"; strings.Join(got, " ") != want {
		t.Fatalf("audit: %v, want %s", got, want)
	}
}

func TestLoginBackoffReturns429(t *testing.T) {
	h := newServer(t).Panel()
	var w *httptest.ResponseRecorder
	for range 6 {
		w = do(h, req{method: "POST", path: "/api/login", origin: "http://127.0.0.1:7070", body: `{"password":"nope"}`})
	}
	if w.Code != http.StatusTooManyRequests || w.Header().Get("Retry-After") == "" {
		t.Fatalf("6th attempt: %d Retry-After=%q", w.Code, w.Header().Get("Retry-After"))
	}
}

func TestBadBodies(t *testing.T) {
	h := newServer(t).Panel()
	for _, b := range []string{"", "{", `{"password":"x","extra":1}`, `{"password":"` + strings.Repeat("a", maxBody) + `"}`} {
		if w := do(h, req{method: "POST", path: "/api/login", origin: "http://127.0.0.1:7070", body: b}); w.Code != http.StatusBadRequest {
			t.Errorf("body %.20q: %d", b, w.Code)
		}
	}
}

func TestSocketAdminRoutesAreNotOnPanel(t *testing.T) {
	s := newServer(t)
	c := login(t, s.Panel())
	if w := do(s.Panel(), req{method: "PUT", path: "/api/admin/password", origin: "http://127.0.0.1:7070", cookie: c, body: `{"password":"zzzzzzzzzzzzzzzz"}`}); w.Code != http.StatusNotFound && w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("panel set-password: %d", w.Code)
	}
	if w := do(s.Socket(), req{method: "PUT", path: "/api/admin/password", body: `{"password":"short"}`}); w.Code != http.StatusBadRequest {
		t.Fatalf("short password via socket: %d", w.Code)
	}
	if w := do(s.Socket(), req{method: "PUT", path: "/api/admin/password", body: `{"password":"zzzzzzzzzzzzzzzz"}`}); w.Code != http.StatusNoContent {
		t.Fatalf("set-password via socket: %d", w.Code)
	}
}

func TestServerTimeouts(t *testing.T) {
	srv := NewHTTPServer(http.NotFoundHandler())
	if srv.ReadHeaderTimeout <= 0 || srv.ReadTimeout <= 0 || srv.WriteTimeout <= 0 || srv.IdleTimeout <= 0 {
		t.Fatalf("missing timeout: %+v", srv)
	}
}

func TestAppRoutesOnPanelNeedSessionAndOrigin(t *testing.T) {
	s := newServer(t)
	h := s.Panel()
	if w := do(h, req{method: "GET", path: "/api/apps"}); w.Code != http.StatusUnauthorized {
		t.Fatalf("no session: %d", w.Code)
	}
	c := login(t, h)
	if w := do(h, req{method: "GET", path: "/api/apps", cookie: c}); w.Code != http.StatusOK || strings.TrimSpace(w.Body.String()) != "[]" {
		t.Fatalf("with session: %d %s", w.Code, w.Body)
	}
	body := `{"name":"x","domains":[{"host":"x.example.com","primary":true}],"spec":{"port":80,"source":{"type":"image","image":"docker.io/library/nginx:1"}}}`
	if w := do(h, req{method: "POST", path: "/api/apps", cookie: c, body: body}); w.Code != http.StatusForbidden {
		t.Fatalf("POST without Origin: %d", w.Code)
	}
	for _, p := range []string{"/api/apps/x/env?reveal=1", "/api/apps/x/webhook"} {
		if w := do(h, req{method: "GET", path: p}); w.Code != http.StatusUnauthorized {
			t.Errorf("%s without session: %d", p, w.Code)
		}
	}
}

func TestRollbackEndpoint(t *testing.T) {
	s := newServer(t)
	s.Engine = &deploy.Engine{Apps: s.Apps}
	ctx := context.Background()
	a := spec.App{
		Name:    "shop",
		Port:    80,
		Domains: []spec.Domain{{Host: "shop.example.com", Primary: true}},
		Source:  spec.Source{Type: "image", Image: "docker.io/library/nginx:1.27.0"},
	}
	id, _, err := s.Apps.Create(ctx, a)
	if err != nil {
		t.Fatal(err)
	}

	w := do(s.Socket(), req{method: "POST", path: "/api/apps/shop/rollback", body: `{}`})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 when no releases, got %d %s", w.Code, w.Body)
	}

	r1, _ := s.Apps.NewRelease(ctx, id, "manual", "")
	r1.Image = "img:1"
	s.Apps.PromoteLive(ctx, id, r1)

	r2, _ := s.Apps.NewRelease(ctx, id, "manual", "")
	r2.Image = "img:2"
	s.Apps.PromoteLive(ctx, id, r2)

	w = do(s.Socket(), req{method: "POST", path: "/api/apps/shop/rollback", body: `{}`})
	if w.Code != http.StatusAccepted {
		t.Fatalf("rollback: %d %s", w.Code, w.Body)
	}
	var queued apps.Release
	json.Unmarshal(w.Body.Bytes(), &queued)
	if queued.Trigger != "rollback" || queued.Ref != r1.Rel8 {
		t.Fatalf("queued rollback release: %+v", queued)
	}

	w = do(s.Socket(), req{method: "POST", path: "/api/apps/shop/rollback", body: `{"to":"nope"}`})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 on nonexistent target, got %d", w.Code)
	}
}
