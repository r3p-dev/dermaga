package telemetry

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func TestRingBuffer(t *testing.T) {
	rb := NewRingBuffer(3)

	now := time.Now()
	rb.Add(ContainerMetric{Timestamp: now.Add(-30 * time.Second), Container: "c1", CPUPercent: 1.0})
	rb.Add(ContainerMetric{Timestamp: now.Add(-20 * time.Second), Container: "c1", CPUPercent: 2.0})
	rb.Add(ContainerMetric{Timestamp: now.Add(-10 * time.Second), Container: "c1", CPUPercent: 3.0})

	items := rb.Get("c1", time.Time{})
	if len(items) != 3 {
		t.Fatalf("expected 3 items, got %d", len(items))
	}
	if items[0].CPUPercent != 1.0 || items[1].CPUPercent != 2.0 || items[2].CPUPercent != 3.0 {
		t.Fatalf("unexpected order or values: %+v", items)
	}

	// Add 4th item, should evict oldest (1.0)
	rb.Add(ContainerMetric{Timestamp: now, Container: "c1", CPUPercent: 4.0})
	items = rb.Get("c1", time.Time{})
	if len(items) != 3 {
		t.Fatalf("expected 3 items after overwrite, got %d", len(items))
	}
	if items[0].CPUPercent != 2.0 || items[1].CPUPercent != 3.0 || items[2].CPUPercent != 4.0 {
		t.Fatalf("unexpected items after wrap-around: %+v", items)
	}

	// Filter with since
	filtered := rb.Get("c1", now.Add(-5*time.Second))
	if len(filtered) != 1 || filtered[0].CPUPercent != 4.0 {
		t.Fatalf("expected 1 filtered item, got %+v", filtered)
	}

	// Test multiple containers
	rb.Add(ContainerMetric{Timestamp: now, Container: "c2", CPUPercent: 10.0})
	conts := rb.Containers()
	slices.Sort(conts)
	if !slices.Equal(conts, []string{"c1", "c2"}) {
		t.Fatalf("expected [c1 c2], got %v", conts)
	}
}

func TestParseHumanBytes(t *testing.T) {
	tests := []struct {
		in   string
		want int64
	}{
		{"", 0},
		{"--", 0},
		{"0B", 0},
		{"1024B", 1024},
		{"1kB", 1024},
		{"32.65kB", 33434},
		{"1MB", 1024 * 1024},
		{"103.3MB", 108317901},
		{"1GB", 1024 * 1024 * 1024},
		{"536.9MB", 562980454},
	}

	for _, tc := range tests {
		got := ParseHumanBytes(tc.in)
		if got != tc.want {
			t.Errorf("ParseHumanBytes(%q) = %d; want %d", tc.in, got, tc.want)
		}
	}
}

func TestParseSlashPair(t *testing.T) {
	a, b := ParseSlashPair("103.3MB / 536.9MB")
	if a < 100*1024*1024 || b < 500*1024*1024 {
		t.Errorf("ParseSlashPair returned %d, %d", a, b)
	}
}

func TestParsePodmanStatsJSON(t *testing.T) {
	raw := `[
		{
			"id": "3c5cda89a179",
			"name": "dg-kuma-dde3709e",
			"cpu_percent": "0.01%",
			"mem_usage": "103.3MB / 536.9MB",
			"net_io": "32.65kB / 2.028kB",
			"block_io": "116.6MB / 0B"
		},
		{
			"id": "997e629e3ef8",
			"name": "dg-portfolio-6ab5ef63",
			"cpu_percent": "0.50%",
			"mem_usage": "16.44MB / 536.9MB",
			"net_io": "15.41kB / 7.872kB",
			"block_io": "0B / 0B"
		}
	]`

	metrics, err := ParsePodmanStatsJSON([]byte(raw), time.Now())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(metrics) != 2 {
		t.Fatalf("expected 2 metrics, got %d", len(metrics))
	}
	if metrics[0].Container != "dg-kuma-dde3709e" || metrics[0].CPUPercent != 0.01 {
		t.Errorf("unexpected metric 0: %+v", metrics[0])
	}
	if metrics[1].Container != "dg-portfolio-6ab5ef63" || metrics[1].CPUPercent != 0.50 {
		t.Errorf("unexpected metric 1: %+v", metrics[1])
	}
}

func TestExtractAppName(t *testing.T) {
	tests := []struct {
		router  string
		service string
		want    string
	}{
		{"app-portfolio@file", "app-portfolio@file", "portfolio"},
		{"app-portfolio-redirect@file", "app-portfolio@file", "portfolio"},
		{"app-blog-verify@file", "app-blog@file", "blog"},
		{"web-to-websecure@entrypoint", "", ""},
		{"dermaga-hooks@file", "dermaga-hooks@file", ""},
		{"", "app-store@file", "store"},
	}

	for _, tc := range tests {
		got := ExtractAppName(tc.router, tc.service)
		if got != tc.want {
			t.Errorf("ExtractAppName(%q, %q) = %q; want %q", tc.router, tc.service, got, tc.want)
		}
	}
}

func TestAccessLogAggregation(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "access.log")
	dbPath := filepath.Join(dir, "test.db")

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	_, err = db.Exec(`
		CREATE TABLE requests_minute (
			minute         INTEGER NOT NULL,
			app            TEXT NOT NULL,
			total_requests INTEGER NOT NULL DEFAULT 0,
			status_2xx     INTEGER NOT NULL DEFAULT 0,
			status_3xx     INTEGER NOT NULL DEFAULT 0,
			status_4xx     INTEGER NOT NULL DEFAULT 0,
			status_5xx     INTEGER NOT NULL DEFAULT 0,
			p50_ms         REAL NOT NULL DEFAULT 0.0,
			p95_ms         REAL NOT NULL DEFAULT 0.0,
			PRIMARY KEY (app, minute)
		);
	`)
	if err != nil {
		t.Fatal(err)
	}

	agg := NewLogAggregator(logPath)

	logData := `{"RouterName":"app-shop@file","DownstreamStatus":200,"Duration":10000000,"StartUTC":"2026-11-09T08:00:05Z"}
{"RouterName":"app-shop@file","DownstreamStatus":200,"Duration":20000000,"StartUTC":"2026-11-09T08:00:15Z"}
{"RouterName":"app-shop@file","DownstreamStatus":404,"Duration":50000000,"StartUTC":"2026-11-09T08:00:25Z"}
{"RouterName":"app-shop@file","DownstreamStatus":500,"Duration":100000000,"StartUTC":"2026-11-09T08:00:35Z"}
`
	if err := os.WriteFile(logPath, []byte(logData), 0644); err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	if err := agg.ProcessIncremental(ctx, db); err != nil {
		t.Fatalf("ProcessIncremental error: %v", err)
	}

	buckets, err := agg.GetRecentTraffic(ctx, db, "shop", time.Time{})
	if err != nil {
		t.Fatalf("GetRecentTraffic error: %v", err)
	}
	if len(buckets) != 1 {
		t.Fatalf("expected 1 bucket, got %d", len(buckets))
	}

	b := buckets[0]
	if b.TotalRequests != 4 {
		t.Errorf("expected 4 requests, got %d", b.TotalRequests)
	}
	if b.Status2xx != 2 || b.Status4xx != 1 || b.Status5xx != 1 {
		t.Errorf("status counts mismatch: 2xx=%d, 4xx=%d, 5xx=%d", b.Status2xx, b.Status4xx, b.Status5xx)
	}
	// Durations: 10ms, 20ms, 50ms, 100ms
	// p50 should be ~20ms or ~50ms, p95 should be ~100ms
	if b.P50Ms <= 0 || b.P95Ms <= 0 {
		t.Errorf("percentiles not computed: p50=%f, p95=%f", b.P50Ms, b.P95Ms)
	}

	// Test log rotation
	if err := agg.RotateLogIfNeeded(10); err != nil {
		t.Fatal(err)
	}
	fi, _ := os.Stat(logPath)
	if fi.Size() != 0 {
		t.Errorf("expected rotated log size 0, got %d", fi.Size())
	}
}

func TestHostTelemetryReading(t *testing.T) {
	ctx := context.Background()
	stats := ReadHostTelemetry(ctx, os.TempDir())
	if stats.MemTotalBytes == 0 {
		t.Error("expected non-zero MemTotalBytes")
	}
	if stats.DiskTotalBytes == 0 {
		t.Error("expected non-zero DiskTotalBytes")
	}
	if stats.DiskUsedPercent < 0 || stats.DiskUsedPercent > 100 {
		t.Errorf("invalid DiskUsedPercent: %f", stats.DiskUsedPercent)
	}
}
