package cli

import (
	"encoding/json"
	"net"
	"net/http"
	"path/filepath"
	"testing"

	"github.com/r3p-dev/dermaga/internal/api"
)

func TestCLINotifyAndConfig(t *testing.T) {
	var savedReq api.UpdateNotificationSettingsRequest
	mux := http.NewServeMux()
	mux.HandleFunc("/api/settings/notifications", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" {
			json.NewEncoder(w).Encode(api.NotificationSettingsView{
				Provider:   "telegram",
				Target:     "-10012345",
				Configured: true,
				Events:     "deploy.failed,backup.failed",
			})
			return
		} else if r.Method == "POST" {
			json.NewDecoder(r.Body).Decode(&savedReq)
			json.NewEncoder(w).Encode(map[string]string{"status": "saved"})
			return
		}
		http.NotFound(w, r)
	})
	mux.HandleFunc("/api/notifications/test", func(w http.ResponseWriter, r *http.Request) {
		var req api.TestNotificationRequest
		json.NewDecoder(r.Body).Decode(&req)
		json.NewEncoder(w).Encode(api.TestNotificationResponse{
			Success: true,
			Detail:  "delivered",
		})
	})

	dir := t.TempDir()
	sockPath := filepath.Join(dir, "test.sock")
	ln, err := net.Listen("unix", sockPath)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	srv := &http.Server{Handler: mux}
	go srv.Serve(ln)
	defer srv.Close()

	socketOverride = sockPath
	defer func() { socketOverride = "" }()

	// Test config notifications show
	if err := cmdConfig([]string{"notifications", "show"}); err != nil {
		t.Fatalf("config notifications show failed: %v", err)
	}

	// Test config notifications set
	if err := cmdConfig([]string{"notifications", "set", "--provider", "discord", "--target", "https://discord.com/webhook", "--events", "disk.warning"}); err != nil {
		t.Fatalf("config notifications set failed: %v", err)
	}
	if savedReq.Provider != "discord" || savedReq.Target != "https://discord.com/webhook" || savedReq.Events != "disk.warning" {
		t.Errorf("saved settings mismatch: %+v", savedReq)
	}

	// Test notify test
	if err := cmdNotify([]string{"test"}); err != nil {
		t.Fatalf("notify test failed: %v", err)
	}
}
