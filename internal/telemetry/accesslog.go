package telemetry

import (
	"bufio"
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"os"
	"slices"
	"strings"
	"sync"
	"time"
)

// RequestBucket holds 1-minute aggregated traffic stats for an application.
type RequestBucket struct {
	Minute        int64   `json:"minute"` // unix seconds aligned to 60s
	App           string  `json:"app"`
	TotalRequests int64   `json:"totalRequests"`
	Status2xx     int64   `json:"status2xx"`
	Status3xx     int64   `json:"status3xx"`
	Status4xx     int64   `json:"status4xx"`
	Status5xx     int64   `json:"status5xx"`
	P50Ms         float64 `json:"p50Ms"`
	P95Ms         float64 `json:"p95Ms"`
}

type traefikLogEntry struct {
	RouterName       string `json:"RouterName"`
	ServiceName      string `json:"ServiceName"`
	DownstreamStatus int    `json:"DownstreamStatus"`
	Duration         int64  `json:"Duration"` // nanoseconds
	StartUTC         string `json:"StartUTC"`
	Time             string `json:"time"`
}

// ExtractAppName derives the dermaga app name from Traefik RouterName or ServiceName.
// Example: "app-portfolio@file" -> "portfolio", "app-blog-redirect@file" -> "blog".
func ExtractAppName(router, service string) string {
	candidate := router
	if candidate == "" {
		candidate = service
	}
	candidate = strings.TrimSuffix(candidate, "@file")
	candidate = strings.TrimPrefix(candidate, "app-")
	candidate = strings.TrimSuffix(candidate, "-redirect")
	candidate = strings.TrimSuffix(candidate, "-verify")

	// If it doesn't match an app prefix or is internal, return empty
	if strings.Contains(candidate, "@") || strings.HasPrefix(candidate, "dermaga-") {
		return ""
	}
	return strings.TrimSpace(candidate)
}

// LogAggregator incrementally reads Traefik access log and aggregates metrics.
type LogAggregator struct {
	mu         sync.Mutex
	path       string
	lastOffset int64
	recent     map[string]map[int64]*inMemoryBucket // app -> minute -> bucket
}

type inMemoryBucket struct {
	bucket    RequestBucket
	durations []float64 // ms durations for percentile computation
}

// NewLogAggregator initializes a Traefik access log aggregator.
func NewLogAggregator(path string) *LogAggregator {
	return &LogAggregator{
		path:   path,
		recent: make(map[string]map[int64]*inMemoryBucket),
	}
}

// ProcessIncremental reads new lines from access.log, aggregates them, and writes to SQLite.
func (la *LogAggregator) ProcessIncremental(ctx context.Context, db *sql.DB) error {
	la.mu.Lock()
	defer la.mu.Unlock()

	f, err := os.Open(la.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	defer f.Close()

	fi, err := f.Stat()
	if err != nil {
		return err
	}

	// If file was truncated or rotated, reset offset
	if fi.Size() < la.lastOffset {
		la.lastOffset = 0
	}

	if _, err := f.Seek(la.lastOffset, io.SeekStart); err != nil {
		return err
	}

	scanner := bufio.NewScanner(f)
	// Allow up to 64KB log lines
	buf := make([]byte, 64*1024)
	scanner.Buffer(buf, 64*1024)

	currentBuckets := make(map[string]map[int64]*inMemoryBucket)

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		var entry traefikLogEntry
		if err := json.Unmarshal(line, &entry); err != nil {
			continue
		}

		appName := ExtractAppName(entry.RouterName, entry.ServiceName)
		if appName == "" {
			continue
		}

		var ts time.Time
		if entry.StartUTC != "" {
			ts, _ = time.Parse(time.RFC3339Nano, entry.StartUTC)
		}
		if ts.IsZero() && entry.Time != "" {
			ts, _ = time.Parse(time.RFC3339, entry.Time)
		}
		if ts.IsZero() {
			ts = time.Now()
		}

		minute := ts.Unix() - (ts.Unix() % 60)
		durMs := float64(entry.Duration) / 1e6
		if durMs < 0 {
			durMs = 0
		}

		appBuckets, ok := currentBuckets[appName]
		if !ok {
			appBuckets = make(map[int64]*inMemoryBucket)
			currentBuckets[appName] = appBuckets
		}

		b, ok := appBuckets[minute]
		if !ok {
			b = &inMemoryBucket{
				bucket: RequestBucket{
					Minute: minute,
					App:    appName,
				},
				durations: make([]float64, 0, 16),
			}
			appBuckets[minute] = b
		}

		b.bucket.TotalRequests++
		status := entry.DownstreamStatus
		switch {
		case status >= 200 && status < 300:
			b.bucket.Status2xx++
		case status >= 300 && status < 400:
			b.bucket.Status3xx++
		case status >= 400 && status < 500:
			b.bucket.Status4xx++
		case status >= 500 && status < 600:
			b.bucket.Status5xx++
		}
		b.durations = append(b.durations, durMs)
	}

	newOffset, err := f.Seek(0, io.SeekCurrent)
	if err == nil {
		la.lastOffset = newOffset
	}

	// Compute percentiles and merge into database and recent cache
	for app, minutes := range currentBuckets {
		for min, b := range minutes {
			if len(b.durations) > 0 {
				slices.Sort(b.durations)
				n := len(b.durations)
				b.bucket.P50Ms = b.durations[int(0.50*float64(n-1))]
				b.bucket.P95Ms = b.durations[int(0.95*float64(n-1))]
			}

			// Save to recent memory cache
			if _, ok := la.recent[app]; !ok {
				la.recent[app] = make(map[int64]*inMemoryBucket)
			}
			la.recent[app][min] = b

			// Persist to SQLite
			if db != nil {
				_, _ = db.ExecContext(ctx, `
					INSERT INTO requests_minute (
						minute, app, total_requests, status_2xx, status_3xx, status_4xx, status_5xx, p50_ms, p95_ms
					) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
					ON CONFLICT(app, minute) DO UPDATE SET
						total_requests = total_requests + excluded.total_requests,
						status_2xx = status_2xx + excluded.status_2xx,
						status_3xx = status_3xx + excluded.status_3xx,
						status_4xx = status_4xx + excluded.status_4xx,
						status_5xx = status_5xx + excluded.status_5xx,
						p50_ms = excluded.p50_ms,
						p95_ms = excluded.p95_ms
				`, b.bucket.Minute, b.bucket.App, b.bucket.TotalRequests,
					b.bucket.Status2xx, b.bucket.Status3xx, b.bucket.Status4xx, b.bucket.Status5xx,
					b.bucket.P50Ms, b.bucket.P95Ms)
			}
		}
	}

	// Prune in-memory cache older than 2 hours
	cutoff := time.Now().Add(-2 * time.Hour).Unix()
	for app, minutes := range la.recent {
		for min := range minutes {
			if min < cutoff {
				delete(minutes, min)
			}
		}
		if len(minutes) == 0 {
			delete(la.recent, app)
		}
	}

	return nil
}

// GetRecentTraffic returns chronological 1-minute buckets for an app from memory or database.
func (la *LogAggregator) GetRecentTraffic(ctx context.Context, db *sql.DB, app string, since time.Time) ([]RequestBucket, error) {
	la.mu.Lock()
	defer la.mu.Unlock()

	sinceSec := since.Unix()
	if db != nil {
		rows, err := db.QueryContext(ctx, `
			SELECT minute, app, total_requests, status_2xx, status_3xx, status_4xx, status_5xx, p50_ms, p95_ms
			FROM requests_minute
			WHERE (? = '' OR app = ?) AND minute >= ?
			ORDER BY minute ASC
		`, app, app, sinceSec)
		if err != nil {
			return nil, err
		}
		defer rows.Close()

		var res []RequestBucket
		for rows.Next() {
			var b RequestBucket
			if err := rows.Scan(&b.Minute, &b.App, &b.TotalRequests, &b.Status2xx, &b.Status3xx, &b.Status4xx, &b.Status5xx, &b.P50Ms, &b.P95Ms); err != nil {
				return nil, err
			}
			res = append(res, b)
		}
		return res, nil
	}

	// Fallback to in-memory if db is nil
	var res []RequestBucket
	if app != "" {
		if minutes, ok := la.recent[app]; ok {
			for min, b := range minutes {
				if min >= sinceSec {
					res = append(res, b.bucket)
				}
			}
		}
	} else {
		for _, minutes := range la.recent {
			for min, b := range minutes {
				if min >= sinceSec {
					res = append(res, b.bucket)
				}
			}
		}
	}
	slices.SortFunc(res, func(a, b RequestBucket) int {
		if a.Minute < b.Minute {
			return -1
		} else if a.Minute > b.Minute {
			return 1
		}
		return strings.Compare(a.App, b.App)
	})
	return res, nil
}

// PruneTraffic deletes request minutes older than retention (e.g. 14 days) from SQLite.
func PruneTraffic(ctx context.Context, db *sql.DB, retention time.Duration) (int64, error) {
	cutoff := time.Now().Add(-retention).Unix()
	res, err := db.ExecContext(ctx, "DELETE FROM requests_minute WHERE minute < ?", cutoff)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// RotateLogIfNeeded truncates or rotates access.log if it exceeds maxSize bytes.
func (la *LogAggregator) RotateLogIfNeeded(maxSize int64) error {
	la.mu.Lock()
	defer la.mu.Unlock()

	if maxSize <= 0 {
		maxSize = 10 * 1024 * 1024 // 10MB default
	}

	fi, err := os.Stat(la.path)
	if err != nil {
		return nil
	}
	if fi.Size() <= maxSize {
		return nil
	}

	// Truncate file and reset offset
	if err := os.Truncate(la.path, 0); err != nil {
		return err
	}
	la.lastOffset = 0
	return nil
}
