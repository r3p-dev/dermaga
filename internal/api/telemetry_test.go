package api

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/r3p-dev/dermaga/internal/backup"
	"github.com/r3p-dev/dermaga/internal/notify"
	"github.com/r3p-dev/dermaga/internal/telemetry"
)

type mockNotifyClient struct {
	lastReq  *http.Request
	lastBody string
}

func (m *mockNotifyClient) Do(req *http.Request) (*http.Response, error) {
	m.lastReq = req
	if req.Body != nil {
		b, _ := io.ReadAll(req.Body)
		m.lastBody = string(b)
	}
	return &http.Response{
		StatusCode: 200,
		Body:       http.NoBody,
	}, nil
}

func TestTelemetryAndNotificationAPIs(t *testing.T) {
	s := newServer(t)
	box := s.Apps.Box
	s.Box = box
	mockClient := &mockNotifyClient{}
	s.Notifier = &notify.Dispatcher{Client: mockClient, DefaultBox: box}
	s.Telemetry = telemetry.NewCollector(s.DB, s.Paths.AccessLog(), s.Paths.DataDir, nil)

	// Pre-populate ring buffer
	s.Telemetry.Ring.Add(telemetry.ContainerMetric{
		Timestamp:  time.Now(),
		Container:  "dg-portfolio-12345678",
		CPUPercent: 1.5,
		MemBytes:   50 * 1024 * 1024,
	})

	// 1. GET /api/telemetry/overview via socket
	rec := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/api/telemetry/overview", nil)
	s.Socket().ServeHTTP(rec, r)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/telemetry/overview failed: %d: %s", rec.Code, rec.Body.String())
	}
	var ov TelemetryOverview
	if err := json.Unmarshal(rec.Body.Bytes(), &ov); err != nil {
		t.Fatalf("failed to parse overview json: %v", err)
	}
	if ov.Host.MemTotalBytes == 0 {
		t.Errorf("expected non-zero MemTotalBytes, got 0")
	}

	// 2. GET /api/telemetry/metrics via socket
	rec = httptest.NewRecorder()
	r = httptest.NewRequest("GET", "/api/telemetry/metrics?container=dg-portfolio-12345678", nil)
	s.Socket().ServeHTTP(rec, r)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/telemetry/metrics failed: %d: %s", rec.Code, rec.Body.String())
	}
	var metricsMap map[string][]telemetry.ContainerMetric
	if err := json.Unmarshal(rec.Body.Bytes(), &metricsMap); err != nil {
		t.Fatalf("failed to parse metrics json: %v", err)
	}
	samples, ok := metricsMap["dg-portfolio-12345678"]
	if !ok || len(samples) == 0 {
		t.Fatalf("expected samples for dg-portfolio-12345678, got: %+v", metricsMap)
	}
	if samples[0].CPUPercent != 1.5 {
		t.Errorf("expected CPU 1.5, got %f", samples[0].CPUPercent)
	}

	// 3. GET /api/settings/notifications (initially empty)
	rec = httptest.NewRecorder()
	r = httptest.NewRequest("GET", "/api/settings/notifications", nil)
	s.Socket().ServeHTTP(rec, r)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/settings/notifications failed: %d: %s", rec.Code, rec.Body.String())
	}
	var nView NotificationSettingsView
	if err := json.Unmarshal(rec.Body.Bytes(), &nView); err != nil {
		t.Fatalf("failed to parse notification settings json: %v", err)
	}
	if nView.Configured {
		t.Errorf("expected initial notification settings not configured")
	}

	// 4. POST /api/settings/notifications (save Telegram settings)
	updateReq := UpdateNotificationSettingsRequest{
		Provider: "telegram",
		Target:   "-10019999999",
		Token:    "123456:ABC-DEF-token",
		Events:   "deploy.failed,backup.failed",
	}
	b, _ := json.Marshal(updateReq)
	rec = httptest.NewRecorder()
	r = httptest.NewRequest("POST", "/api/settings/notifications", bytes.NewReader(b))
	s.Socket().ServeHTTP(rec, r)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /api/settings/notifications failed: %d: %s", rec.Code, rec.Body.String())
	}

	// 5. GET /api/settings/notifications (verify updated and token not leaked)
	rec = httptest.NewRecorder()
	r = httptest.NewRequest("GET", "/api/settings/notifications", nil)
	s.Socket().ServeHTTP(rec, r)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/settings/notifications failed: %d: %s", rec.Code, rec.Body.String())
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &nView); err != nil {
		t.Fatalf("failed to parse notification settings json: %v", err)
	}
	if nView.Provider != "telegram" || nView.Target != "-10019999999" || !nView.Configured {
		t.Errorf("unexpected notification settings view: %+v", nView)
	}

	// 6. POST /api/notifications/test
	testReq := TestNotificationRequest{}
	b, _ = json.Marshal(testReq)
	rec = httptest.NewRecorder()
	r = httptest.NewRequest("POST", "/api/notifications/test", bytes.NewReader(b))
	s.Socket().ServeHTTP(rec, r)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /api/notifications/test failed: %d: %s", rec.Code, rec.Body.String())
	}
	var testResp TestNotificationResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &testResp); err != nil {
		t.Fatalf("failed to parse test notification response: %v", err)
	}
	if !testResp.Success {
		t.Errorf("expected test notification success, got failure: %s", testResp.Detail)
	}
	if mockClient.lastReq == nil || mockClient.lastReq.URL.String() != "https://api.telegram.org/bot123456:ABC-DEF-token/sendMessage" {
		t.Errorf("unexpected mock request: %+v", mockClient.lastReq)
	}

	// 7. GET /api/telemetry/traffic
	rec = httptest.NewRecorder()
	r = httptest.NewRequest("GET", "/api/telemetry/traffic?app=portfolio", nil)
	s.Socket().ServeHTTP(rec, r)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/telemetry/traffic failed: %d: %s", rec.Code, rec.Body.String())
	}
	var traffic []telemetry.RequestBucket
	if err := json.Unmarshal(rec.Body.Bytes(), &traffic); err != nil {
		t.Fatalf("failed to parse traffic json: %v", err)
	}
}

func TestBackupFailureNotificationDispatch(t *testing.T) {
	s := newServer(t)
	box := s.Apps.Box
	s.Box = box
	mockClient := &mockNotifyClient{}
	s.Notifier = &notify.Dispatcher{Client: mockClient, DefaultBox: box}

	// Configure ntfy
	ctx := context.Background()
	_ = notify.SaveSettings(ctx, s.DB, box, notify.Settings{
		Provider: notify.ProviderNtfy,
		Target:   "backup-alerts-test",
		Events:   "backup.failed",
	})

	rec := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/api/backup/failure-notify", strings.NewReader(`{"unit":"dg-backup-db-postgres.service"}`))
	s.Socket().ServeHTTP(rec, r)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /api/backup/failure-notify failed: %d", rec.Code)
	}

	if mockClient.lastReq == nil {
		t.Fatal("expected dispatch on backup failure notification, got nil")
	}
	if mockClient.lastReq.URL.String() != "https://ntfy.sh/backup-alerts-test" {
		t.Errorf("unexpected target URL: %s", mockClient.lastReq.URL.String())
	}
}

// Failed backups started through the API (panel, manual CLI) notify too; a
// timer run (scheduled) leaves it to the unit's OnFailure= so there is one alert.
func TestAPIBackupFailureNotifies(t *testing.T) {
	s := newServer(t)
	box := s.Apps.Box
	s.Box = box
	mock := &mockNotifyClient{}
	s.Notifier = &notify.Dispatcher{Client: mock, DefaultBox: box}
	s.Backup = backup.NewManager(s.DB, box, s.Databases, s.Paths, nil)
	_ = notify.SaveSettings(context.Background(), s.DB, box, notify.Settings{
		Provider: notify.ProviderNtfy, Target: "backup-alerts-test", Events: "backup.failed",
	})
	post := func(path, body string) int {
		rec := httptest.NewRecorder()
		s.Socket().ServeHTTP(rec, httptest.NewRequest("POST", path, strings.NewReader(body)))
		return rec.Code
	}
	for _, c := range []struct {
		name, path, body string
		notified         bool
	}{
		{"manual run", "/api/backup/run", `{"target":"db:nope"}`, true},
		{"timer run", "/api/backup/run", `{"target":"db:nope","scheduled":true}`, false},
		{"database page", "/api/databases/nope/backup", ``, true},
	} {
		mock.lastReq = nil
		if code := post(c.path, c.body); code < 400 {
			t.Fatalf("%s: backup of a missing database returned %d", c.name, code)
		}
		if got := mock.lastReq != nil; got != c.notified {
			t.Errorf("%s: notified = %v, want %v", c.name, got, c.notified)
		}
	}
}
