package api

import (
	"bytes"
	"encoding/json"
	"log"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/r3p-dev/dermaga/internal/audit"
	"github.com/r3p-dev/dermaga/internal/backup"
	"github.com/r3p-dev/dermaga/internal/notify"
)

// TestBackupPasswordLifecycle walks the restic password through
// none → unconfirmed → confirmed (ARCHITECTURE v2 D62) on both listeners and
// checks that the password appears only in the generate and reveal responses:
// not in GET settings, logs, audit rows or errors.
func TestBackupPasswordLifecycle(t *testing.T) {
	var logs bytes.Buffer
	oldSlog := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug})))
	log.SetOutput(&logs)
	defer func() { slog.SetDefault(oldSlog); log.SetOutput(os.Stderr) }()

	s := newServer(t)
	s.Box = s.Apps.Box
	s.Backup = backup.NewManager(s.DB, s.Box, s.Databases, s.Paths, nil)
	mock := &mockNotifyClient{}
	s.Notifier = &notify.Dispatcher{Client: mock, DefaultBox: s.Box}
	_ = notify.SaveSettings(t.Context(), s.DB, s.Box, notify.Settings{Provider: notify.ProviderNtfy, Target: "t", Events: "backup.failed"})
	panel, sock := s.Panel(), s.Socket()
	cookie := login(t, panel)
	const origin = "http://127.0.0.1:7070"
	cli := func(method, path, body string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		sock.ServeHTTP(rec, httptest.NewRequest(method, path, strings.NewReader(body)))
		return rec
	}
	state := func() string {
		var st backup.Settings
		w := do(panel, req{method: "GET", path: "/api/backup/settings", cookie: cookie})
		if err := json.Unmarshal(w.Body.Bytes(), &st); err != nil {
			t.Fatalf("GET settings: %d %s", w.Code, w.Body)
		}
		return st.PasswordState
	}
	var bodies []string // every response body that must NOT contain the password
	keep := func(w *httptest.ResponseRecorder) *httptest.ResponseRecorder {
		bodies = append(bodies, w.Body.String())
		return w
	}

	// none: GET does not create one; backups fail clearly and notify.
	if got := state(); got != backup.PasswordNone {
		t.Fatalf("initial state %q, want none", got)
	}
	if w := keep(cli("POST", "/api/backup/password/reveal", "")); w.Code != http.StatusNotFound {
		t.Errorf("reveal without a password: %d, want 404", w.Code)
	}
	// The failure says what to do, in the API error (printed by the CLI and so
	// in the journal) and in the notification body, for manual and timer runs.
	actionable := func(where, text string) {
		t.Helper()
		for _, want := range []string{"set up the backup password first", "Backups in the panel", "dermaga backup password init"} {
			if !strings.Contains(text, want) {
				t.Errorf("%s does not say %q: %s", where, want, text)
			}
		}
	}
	w0 := keep(cli("POST", "/api/backup/run", `{"target":"system"}`))
	if w0.Code != http.StatusConflict || mock.lastReq == nil {
		t.Fatalf("backup without a password: %d, notified=%v; want 409 and a backup.failed notification", w0.Code, mock.lastReq != nil)
	}
	actionable("the API error", w0.Body.String())
	actionable("the notification for a manual run", mock.lastBody)
	mock.lastReq, mock.lastBody = nil, ""
	if w := keep(cli("POST", "/api/backup/run", `{"target":"system","scheduled":true}`)); w.Code != http.StatusConflict || mock.lastReq != nil {
		t.Errorf("scheduled backup without a password: %d, notified=%v; want 409 and no direct notification", w.Code, mock.lastReq != nil)
	}
	keep(cli("POST", "/api/backup/failure-notify", `{"unit":"dg-backup-system.service"}`))
	actionable("the notification for a timer run", mock.lastBody)
	if !strings.Contains(logs.String(), "set up the backup password first") {
		t.Errorf("the failure log does not name the missing password:\n%s", logs.String())
	}

	// generate (panel): shown once, no-store, state unconfirmed; a second call is refused.
	w := do(panel, req{method: "POST", path: "/api/backup/password", origin: origin, cookie: cookie})
	var gen BackupPasswordResponse
	if err := json.Unmarshal(w.Body.Bytes(), &gen); err != nil || w.Code != http.StatusOK || len(gen.Password) != 64 {
		t.Fatalf("generate: %d, %d-char password, %v", w.Code, len(gen.Password), err)
	}
	if cc := w.Header().Get("Cache-Control"); cc != "no-store" {
		t.Errorf("generate Cache-Control %q, want no-store", cc)
	}
	if gen.PasswordState != backup.PasswordUnconfirmed || gen.RecoveryStatement != backup.RecoveryStatement {
		t.Errorf("generate response: state %q, statement set = %v", gen.PasswordState, gen.RecoveryStatement != "")
	}
	if w := keep(do(panel, req{method: "POST", path: "/api/backup/password", origin: origin, cookie: cookie})); w.Code != http.StatusConflict {
		t.Errorf("second generate: %d, want 409", w.Code)
	}

	// GET settings (both listeners) and PUT never carry or accept the password.
	keep(do(panel, req{method: "GET", path: "/api/backup/settings", cookie: cookie}))
	keep(cli("GET", "/api/backup/settings", ""))
	keep(cli("GET", "/api/status", ""))
	if w := keep(cli("PUT", "/api/backup/settings", `{"repository":"/x","password":"new"}`)); w.Code != http.StatusBadRequest {
		t.Errorf("PUT with a password: %d, want 400", w.Code)
	}

	// reveal (panel) needs the admin password; a wrong one is denied and audited.
	if w := keep(do(panel, req{method: "POST", path: "/api/backup/password/reveal", origin: origin, cookie: cookie, body: `{"password":"wrong"}`})); w.Code != http.StatusForbidden {
		t.Errorf("panel reveal with a wrong password: %d, want 403", w.Code)
	}
	if w := keep(do(panel, req{method: "POST", path: "/api/backup/password/reveal", origin: origin, body: `{"password":"` + pw + `"}`})); w.Code != http.StatusUnauthorized {
		t.Errorf("panel reveal without a session: %d, want 401", w.Code)
	}
	w = do(panel, req{method: "POST", path: "/api/backup/password/reveal", origin: origin, cookie: cookie, body: `{"password":"` + pw + `"}`})
	var rev BackupPasswordResponse
	json.Unmarshal(w.Body.Bytes(), &rev)
	if w.Code != http.StatusOK || rev.Password != gen.Password || w.Header().Get("Cache-Control") != "no-store" {
		t.Errorf("panel reveal: %d, same password = %v, Cache-Control %q", w.Code, rev.Password == gen.Password, w.Header().Get("Cache-Control"))
	}
	// reveal (CLI socket): no re-authentication, still no-store.
	w = cli("POST", "/api/backup/password/reveal", "")
	json.Unmarshal(w.Body.Bytes(), &rev)
	if w.Code != http.StatusOK || rev.Password != gen.Password || w.Header().Get("Cache-Control") != "no-store" {
		t.Errorf("cli reveal: %d, same password = %v, Cache-Control %q", w.Code, rev.Password == gen.Password, w.Header().Get("Cache-Control"))
	}

	// confirm.
	if w := keep(cli("POST", "/api/backup/password/confirm", "")); w.Code != http.StatusOK || state() != backup.PasswordConfirmed {
		t.Errorf("confirm: %d, state %q", w.Code, state())
	}

	// Failed panel reveals share the login limiter: after enough of them even
	// the right password is refused with 429, and so is a login.
	for range 6 {
		keep(do(panel, req{method: "POST", path: "/api/backup/password/reveal", origin: origin, cookie: cookie, body: `{"password":"wrong"}`}))
	}
	if w := keep(do(panel, req{method: "POST", path: "/api/backup/password/reveal", origin: origin, cookie: cookie, body: `{"password":"` + pw + `"}`})); w.Code != http.StatusTooManyRequests || w.Header().Get("Retry-After") == "" {
		t.Errorf("reveal after repeated failures: %d (Retry-After %q), want 429", w.Code, w.Header().Get("Retry-After"))
	}
	if w := keep(do(panel, req{method: "POST", path: "/api/login", origin: origin, body: `{"password":"` + pw + `"}`})); w.Code != http.StatusTooManyRequests {
		t.Errorf("login while reveals are backed off: %d, want 429", w.Code)
	}

	// Audit: the four events plus the denial, none carrying the password.
	entries, err := audit.List(s.DB, 100)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, e := range entries {
		seen[e.Action] = true
		if strings.Contains(e.Details+e.Target, gen.Password) {
			t.Errorf("audit entry %s carries the password", e.Action)
		}
	}
	for _, a := range []string{"backup.password_generated", "backup.password_revealed", "backup.password_confirmed", "backup.password_reveal_denied"} {
		if !seen[a] {
			t.Errorf("audit event %s missing (have %v)", a, seen)
		}
	}

	// The password is nowhere but the generate and reveal bodies.
	if strings.Contains(logs.String(), gen.Password) {
		t.Errorf("the password appears in the daemon log output")
	}
	for _, b := range bodies {
		if strings.Contains(b, gen.Password) {
			t.Errorf("the password appears in a response that must not carry it: %s", b)
		}
	}
	if logs.Len() == 0 {
		t.Error("no log output was captured; the log check proves nothing")
	}
}

// The S3 secret key: GET masks it, PUT keeps it when the field is absent or
// empty, replaces it when a new one is sent, and refuses the mask (gap G43).
func TestBackupSettingsS3SecretKept(t *testing.T) {
	s := newServer(t)
	s.Box = s.Apps.Box
	s.Backup = backup.NewManager(s.DB, s.Box, s.Databases, s.Paths, nil)
	panel := s.Panel()
	cookie := login(t, panel)
	put := func(body string) int {
		return do(panel, req{method: "PUT", path: "/api/backup/settings", origin: "http://127.0.0.1:7070", cookie: cookie, body: body}).Code
	}
	stored := func() string {
		st, err := s.Backup.GetSettings(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		return st.S3SecretKey
	}
	base := `"repository":"s3:https://h/b","s3Endpoint":"h","s3Bucket":"b","s3AccessKey":"AK","configured":true,"passwordState":"none"`

	if code := put(`{` + base + `,"s3SecretKey":"first-secret"}`); code != http.StatusNoContent || stored() != "first-secret" {
		t.Fatalf("initial save: %d, stored %q", code, stored())
	}
	w := do(panel, req{method: "GET", path: "/api/backup/settings", cookie: cookie})
	if strings.Contains(w.Body.String(), "first-secret") || !strings.Contains(w.Body.String(), s3SecretMask) {
		t.Errorf("GET must mask the secret: %s", w.Body)
	}
	if code := put(`{` + base + `,"s3Region":"eu"}`); code != http.StatusNoContent || stored() != "first-secret" {
		t.Errorf("save without the secret: %d, stored %q; want it kept", code, stored())
	}
	if code := put(`{` + base + `,"s3SecretKey":""}`); code != http.StatusNoContent || stored() != "first-secret" {
		t.Errorf("save with an empty secret: %d, stored %q; want it kept", code, stored())
	}
	for _, mask := range []string{s3SecretMask, "••••", "********"} {
		if code := put(`{` + base + `,"s3SecretKey":"` + mask + `"}`); code != http.StatusBadRequest || stored() != "first-secret" {
			t.Errorf("save with the mask %q: %d, stored %q; want 400 and the secret kept", mask, code, stored())
		}
	}
	if code := put(`{` + base + `,"s3SecretKey":"second-secret"}`); code != http.StatusNoContent || stored() != "second-secret" {
		t.Errorf("save with a new secret: %d, stored %q", code, stored())
	}
}
