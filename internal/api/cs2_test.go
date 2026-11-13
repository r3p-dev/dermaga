package api

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/r3p-dev/dermaga/internal/backup"
	"github.com/r3p-dev/dermaga/internal/host"
	"github.com/r3p-dev/dermaga/internal/spec"
)

// The create API accepts exactly the strategies the UI is generated from
// (spec.StrategyBlueGreen, spec.StrategyRecreate) and the empty default.
func TestCreateAppStrategies(t *testing.T) {
	s := newServer(t)
	create := func(name, strategy string) int {
		body := `{"name":"` + name + `","domains":[{"host":"` + name + `.example.com","primary":true}],"spec":{"port":80,"strategy":"` + strategy +
			`","source":{"type":"image","image":"docker.io/library/nginx:1.27.0"}}}`
		return do(s.Socket(), req{method: "POST", path: "/api/apps", body: body}).Code
	}
	for i, ok := range []string{spec.StrategyBlueGreen, spec.StrategyRecreate, ""} {
		if code := create("ok"+string(rune('a'+i)), ok); code != http.StatusCreated {
			t.Errorf("strategy %q: %d, want 201", ok, code)
		}
	}
	for _, bad := range []string{"zero-downtime", "bluegreen", "rolling"} { // "zero-downtime" is what the wizard used to send (G10)
		if code := create("bad", bad); code != http.StatusBadRequest {
			t.Errorf("strategy %q: %d, want 400", bad, code)
		}
	}
}

// The wizard must take its strategy options from the generated constants, not
// from literals: this fails if someone writes a value into the page by hand.
func TestWizardUsesGeneratedStrategies(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "..", "ui", "src", "routes", "apps", "new", "+page.svelte"))
	if err != nil {
		t.Fatal(err)
	}
	page := string(b)
	for _, want := range []string{"StrategyBlueGreen", "StrategyRecreate", "$state<string>(StrategyBlueGreen)"} {
		if !strings.Contains(page, want) {
			t.Errorf("the wizard does not use %s", want)
		}
	}
	// The server needs exactly one primary domain, so the form must require it.
	if !regexp.MustCompile(`(?s)id="domain"[^>]*\brequired\b`).MatchString(page) {
		t.Error("the wizard's domain field is not required")
	}
	for _, lit := range []string{`value="zero-downtime"`, `value="recreate"`, `value="blue-green"`, `'zero-downtime'`} {
		if strings.Contains(page, lit) {
			t.Errorf("the wizard has a hand-written strategy value: %s", lit)
		}
	}
}

// Responses the UI reads are typed structs with the field names it uses.
func TestTypedResponses(t *testing.T) {
	s := newServer(t)
	s.Box = s.Apps.Box
	s.Paths.ConfigDir = t.TempDir()
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}
	if err := os.WriteFile(s.Paths.MasterKey(), key, 0o600); err != nil {
		t.Fatal(err)
	}
	s.Backup = backup.NewManager(s.DB, s.Box, s.Databases, s.Paths, nil)

	var k KeysExportResponse
	w := do(s.Socket(), req{method: "GET", path: "/api/keys/export"})
	if err := json.Unmarshal(w.Body.Bytes(), &k); err != nil || w.Code != http.StatusOK {
		t.Fatalf("keys export: %d %v", w.Code, err)
	}
	if k.Key != "000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f" || !strings.Contains(k.Instructions, "MASTER KEY") {
		t.Errorf("keys export: key %q", k.Key)
	}
	if !strings.Contains(w.Body.String(), `"key":`) || strings.Contains(w.Body.String(), "masterKeyB64") {
		t.Errorf("keys export field names: %s", w.Body.String()[:40])
	}

	var st Status
	w = do(s.Socket(), req{method: "GET", path: "/api/status"})
	if err := json.Unmarshal(w.Body.Bytes(), &st); err != nil {
		t.Fatal(err)
	}
	if st.SSHPort != host.SSHPort() || st.SSHUser == "" || st.PanelPort != 7070 {
		t.Errorf("status SSH fields: port %q (sshd says %q), user %q, panel %d", st.SSHPort, host.SSHPort(), st.SSHUser, st.PanelPort)
	}
	if addr, public := host.Address(); st.ServerAddress != addr || st.ServerAddressPublic != public {
		t.Errorf("status address %q public=%v, host says %q %v", st.ServerAddress, st.ServerAddressPublic, addr, public)
	}

	// SSH snippets never invent a host: the server's public address or the placeholder.
	hostName, port, _ := sshTarget()
	if _, public := host.Address(); !public && hostName != SSHHostPlaceholder {
		t.Errorf("without a public address the SSH host must be the placeholder, got %q", hostName)
	}
	if port != host.SSHPort() {
		t.Errorf("snippet port %q, sshd says %q", port, host.SSHPort())
	}
}
