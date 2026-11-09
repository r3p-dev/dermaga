package api

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/r3p-dev/dermaga/internal/audit"
	"github.com/r3p-dev/dermaga/internal/notify"
	"github.com/r3p-dev/dermaga/internal/telemetry"
)

type NotificationSettingsView struct {
	Provider   string `json:"provider"`
	Target     string `json:"target"`
	Configured bool   `json:"configured"`
	Events     string `json:"events"`
}

type UpdateNotificationSettingsRequest struct {
	Provider string `json:"provider"`
	Target   string `json:"target"`
	Token    string `json:"token,omitempty"`
	Events   string `json:"events"`
}

type TestNotificationRequest struct {
	Provider string `json:"provider,omitempty"`
	Target   string `json:"target,omitempty"`
	Token    string `json:"token,omitempty"`
}

type TestNotificationResponse struct {
	Success bool   `json:"success"`
	Detail  string `json:"detail"`
}

type TelemetryOverview struct {
	Host        telemetry.HostTelemetry `json:"host"`
	DaemonRSSKB uint64                  `json:"daemonRssKb"`
	DaemonPID   int                     `json:"daemonPid"`
	Uptime      string                  `json:"uptime"`
	ActiveApps  int                     `json:"activeApps"`
	TotalApps   int                     `json:"totalApps"`
	TotalDBs    int                     `json:"totalDbs"`
}

func (s *Server) telemetryRoutes(mux *http.ServeMux, wrap func(http.HandlerFunc) http.Handler, actor string) {
	mux.Handle("GET /api/telemetry/overview", wrap(s.getTelemetryOverview))
	mux.Handle("GET /api/telemetry/metrics", wrap(s.getTelemetryMetrics))
	mux.Handle("GET /api/telemetry/traffic", wrap(s.getTelemetryTraffic))
	mux.Handle("GET /api/settings/notifications", wrap(s.getNotificationSettings))
	mux.Handle("POST /api/settings/notifications", wrap(func(w http.ResponseWriter, r *http.Request) {
		s.updateNotificationSettings(w, r, actor)
	}))
	mux.Handle("POST /api/notifications/test", wrap(func(w http.ResponseWriter, r *http.Request) {
		s.testNotification(w, r, actor)
	}))
}

func (s *Server) getTelemetryOverview(w http.ResponseWriter, r *http.Request) {
	hostStats := telemetry.ReadHostTelemetry(r.Context(), s.Paths.DataDir)

	var activeApps, totalApps int
	if list, err := s.Apps.List(r.Context()); err == nil {
		totalApps = len(list)
		for _, a := range list {
			if a.Live != nil {
				activeApps++
			}
		}
	}

	var totalDBs int
	if dbs, err := s.Databases.List(r.Context()); err == nil {
		totalDBs = len(dbs)
	}

	writeJSON(w, http.StatusOK, TelemetryOverview{
		Host:        hostStats,
		DaemonRSSKB: readProcessRSSKB(),
		DaemonPID:   os.Getpid(),
		Uptime:      time.Since(s.Started).Round(time.Second).String(),
		ActiveApps:  activeApps,
		TotalApps:   totalApps,
		TotalDBs:    totalDBs,
	})
}

func (s *Server) getTelemetryMetrics(w http.ResponseWriter, r *http.Request) {
	container := r.URL.Query().Get("container")
	sinceStr := r.URL.Query().Get("since")

	var since time.Time
	if sinceStr != "" {
		if d, err := time.ParseDuration(sinceStr); err == nil {
			since = time.Now().Add(-d)
		} else if t, err := time.Parse(time.RFC3339, sinceStr); err == nil {
			since = t
		}
	}
	if since.IsZero() {
		since = time.Now().Add(-1 * time.Hour)
	}

	// If within last 1 hour and ring buffer is available, serve from in-memory ring
	if s.Telemetry != nil && s.Telemetry.Ring != nil && time.Since(since) <= 70*time.Minute {
		if container != "" {
			items := s.Telemetry.Ring.Get(container, since)
			writeJSON(w, http.StatusOK, map[string][]telemetry.ContainerMetric{container: items})
			return
		}
		all := s.Telemetry.Ring.GetAll(since)
		writeJSON(w, http.StatusOK, all)
		return
	}

	// Wider query falls back to SQLite metrics table
	metrics, err := telemetry.GetContainerMetrics(r.Context(), s.DB, container, since)
	if err != nil {
		internalError(w, err)
		return
	}

	res := make(map[string][]telemetry.ContainerMetric)
	for _, m := range metrics {
		res[m.Container] = append(res[m.Container], m)
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) getTelemetryTraffic(w http.ResponseWriter, r *http.Request) {
	app := r.URL.Query().Get("app")
	sinceStr := r.URL.Query().Get("since")

	var since time.Time
	if sinceStr != "" {
		if d, err := time.ParseDuration(sinceStr); err == nil {
			since = time.Now().Add(-d)
		} else if t, err := time.Parse(time.RFC3339, sinceStr); err == nil {
			since = t
		}
	}
	if since.IsZero() {
		since = time.Now().Add(-24 * time.Hour)
	}

	if s.Telemetry == nil || s.Telemetry.Aggregator == nil {
		writeJSON(w, http.StatusOK, []telemetry.RequestBucket{})
		return
	}

	buckets, err := s.Telemetry.Aggregator.GetRecentTraffic(r.Context(), s.DB, app, since)
	if err != nil {
		internalError(w, err)
		return
	}
	if buckets == nil {
		buckets = []telemetry.RequestBucket{}
	}
	writeJSON(w, http.StatusOK, buckets)
}

func (s *Server) getNotificationSettings(w http.ResponseWriter, r *http.Request) {
	cfg, err := notify.LoadSettings(r.Context(), s.DB, s.Box)
	if err != nil {
		internalError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, NotificationSettingsView{
		Provider:   string(cfg.Provider),
		Target:     cfg.Target,
		Configured: cfg.Token != "" || (cfg.Provider == notify.ProviderDiscord && cfg.Target != "") || (cfg.Provider == notify.ProviderNtfy && cfg.Target != ""),
		Events:     cfg.Events,
	})
}

func (s *Server) updateNotificationSettings(w http.ResponseWriter, r *http.Request, actor string) {
	var req UpdateNotificationSettingsRequest
	if !readJSON(w, r, &req) {
		return
	}

	current, err := notify.LoadSettings(r.Context(), s.DB, s.Box)
	if err != nil {
		internalError(w, err)
		return
	}

	token := req.Token
	if token == "" && req.Provider == string(current.Provider) {
		token = current.Token // Preserve existing secret token if not supplied
	}

	events := req.Events
	if events == "" {
		events = "deploy.failed,backup.failed,disk.warning"
	}

	updated := notify.Settings{
		Provider: notify.Provider(req.Provider),
		Target:   strings.TrimSpace(req.Target),
		Token:    token,
		Events:   events,
	}

	if err := notify.SaveSettings(r.Context(), s.DB, s.Box, updated); err != nil {
		internalError(w, err)
		return
	}

	_ = audit.Log(s.DB, actor, "settings.notifications", string(updated.Provider), updated.Target)
	writeJSON(w, http.StatusOK, map[string]string{"status": "saved"})
}

func (s *Server) testNotification(w http.ResponseWriter, r *http.Request, _ string) {
	var req TestNotificationRequest
	_ = readJSON(w, r, &req)

	cfg, err := notify.LoadSettings(r.Context(), s.DB, s.Box)
	if err != nil {
		internalError(w, err)
		return
	}

	if req.Provider != "" {
		cfg.Provider = notify.Provider(req.Provider)
	}
	if req.Target != "" {
		cfg.Target = req.Target
	}
	if req.Token != "" {
		cfg.Token = req.Token
	}

	if cfg.Provider == notify.ProviderDisabled || cfg.Provider == "" {
		writeError(w, http.StatusBadRequest, "no notification provider configured")
		return
	}

	dispatcher := s.Notifier
	if dispatcher == nil {
		dispatcher = notify.NewDispatcher(s.Box)
	}

	msg := notify.Message{
		Event:     "test",
		Title:     "Dermaga Test Alert",
		Body:      fmt.Sprintf("Test alert delivered successfully from Dermaga at %s.", time.Now().Format(time.RFC3339)),
		Level:     "info",
		Timestamp: time.Now(),
	}

	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()

	if err := dispatcher.Dispatch(ctx, cfg, msg); err != nil {
		writeJSON(w, http.StatusOK, TestNotificationResponse{
			Success: false,
			Detail:  err.Error(),
		})
		return
	}

	writeJSON(w, http.StatusOK, TestNotificationResponse{
		Success: true,
		Detail:  "Notification delivered successfully to " + string(cfg.Provider),
	})
}

// readProcessRSSKB reads VmRSS from /proc/self/status.
func readProcessRSSKB() uint64 {
	b, err := os.ReadFile("/proc/self/status")
	if err != nil {
		return 0
	}
	for _, l := range strings.Split(string(b), "\n") {
		if strings.HasPrefix(l, "VmRSS:") {
			fields := strings.Fields(l)
			if len(fields) >= 2 {
				val, _ := strconv.ParseUint(fields[1], 10, 64)
				return val
			}
		}
	}
	return 0
}
