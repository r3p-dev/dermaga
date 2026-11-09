package telemetry

import (
	"context"
	"database/sql"
	"log/slog"
	"os/exec"
	"sync"
	"time"
)

// AlertHandler receives triggered system telemetry alerts (e.g. disk usage).
type AlertHandler func(ctx context.Context, event, title, details string)

// Collector coordinates sampling of container metrics and host telemetry.
type Collector struct {
	mu           sync.Mutex
	DB           *sql.DB
	Ring         *RingBuffer
	Aggregator   *LogAggregator
	DataDir      string
	OnAlert      AlertHandler
	lastDiskWarn time.Time
	stopCh       chan struct{}
}

// NewCollector constructs a Collector instance with default 1-hour in-memory buffers.
func NewCollector(db *sql.DB, accessLogPath string, dataDir string, alertFn AlertHandler) *Collector {
	return &Collector{
		DB:         db,
		Ring:       NewRingBuffer(120), // 120 * 30s = 60 minutes
		Aggregator: NewLogAggregator(accessLogPath),
		DataDir:    dataDir,
		OnAlert:    alertFn,
		stopCh:     make(chan struct{}),
	}
}

// SamplePodmanStats invokes `podman stats --no-stream --format json`.
func SamplePodmanStats(ctx context.Context) ([]ContainerMetric, error) {
	if _, err := exec.LookPath("podman"); err != nil {
		return nil, nil // Podman not present (e.g. in test env)
	}

	cmdCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	cmd := exec.CommandContext(cmdCtx, "podman", "stats", "--no-stream", "--format", "json")
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}

	return ParsePodmanStatsJSON(out, time.Now())
}

// SaveContainerMetrics persists a batch of container metrics to SQLite.
func SaveContainerMetrics(ctx context.Context, db *sql.DB, metrics []ContainerMetric) error {
	if db == nil || len(metrics) == 0 {
		return nil
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO metrics (ts, container, cpu_percent, mem_bytes, mem_limit, net_in, net_out, block_in, block_out)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, m := range metrics {
		_, err := stmt.ExecContext(ctx,
			m.Timestamp.Unix(),
			m.Container,
			m.CPUPercent,
			m.MemBytes,
			m.MemLimit,
			m.NetIn,
			m.NetOut,
			m.BlockIn,
			m.BlockOut,
		)
		if err != nil {
			return err
		}
	}

	return tx.Commit()
}

// GetContainerMetrics retrieves metrics for a container since the given time.
func GetContainerMetrics(ctx context.Context, db *sql.DB, container string, since time.Time) ([]ContainerMetric, error) {
	if db == nil {
		return nil, nil
	}

	sinceSec := since.Unix()
	rows, err := db.QueryContext(ctx, `
		SELECT ts, container, cpu_percent, mem_bytes, mem_limit, net_in, net_out, block_in, block_out
		FROM metrics
		WHERE (? = '' OR container = ?) AND ts >= ?
		ORDER BY ts ASC
	`, container, container, sinceSec)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var res []ContainerMetric
	for rows.Next() {
		var m ContainerMetric
		var tsSec int64
		if err := rows.Scan(&tsSec, &m.Container, &m.CPUPercent, &m.MemBytes, &m.MemLimit, &m.NetIn, &m.NetOut, &m.BlockIn, &m.BlockOut); err != nil {
			return nil, err
		}
		m.Timestamp = time.Unix(tsSec, 0)
		res = append(res, m)
	}
	return res, nil
}

// PruneMetrics removes container metrics older than retention (e.g. 7 days).
func PruneMetrics(ctx context.Context, db *sql.DB, retention time.Duration) (int64, error) {
	if db == nil {
		return 0, nil
	}
	cutoff := time.Now().Add(-retention).Unix()
	res, err := db.ExecContext(ctx, "DELETE FROM metrics WHERE ts < ?", cutoff)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// CollectOnce executes a single sampling tick for metrics and logs.
func (c *Collector) CollectOnce(ctx context.Context) {
	// 1. Container metrics
	metrics, err := SamplePodmanStats(ctx)
	if err != nil {
		slog.Debug("podman stats sampling failed", "err", err)
	} else if len(metrics) > 0 {
		for _, m := range metrics {
			c.Ring.Add(m)
		}
		if err := SaveContainerMetrics(ctx, c.DB, metrics); err != nil {
			slog.Warn("saving container metrics failed", "err", err)
		}
	}

	// 2. Traefik access log incremental aggregation
	if err := c.Aggregator.ProcessIncremental(ctx, c.DB); err != nil {
		slog.Debug("incremental access log processing", "err", err)
	}

	// 3. Host telemetry & disk threshold check
	hostStats := ReadHostTelemetry(ctx, c.DataDir)
	if hostStats.DiskUsedPercent >= 85.0 && c.OnAlert != nil {
		c.mu.Lock()
		shouldAlert := c.lastDiskWarn.IsZero() || time.Since(c.lastDiskWarn) >= 4*time.Hour
		if shouldAlert {
			c.lastDiskWarn = time.Now()
		}
		c.mu.Unlock()

		if shouldAlert {
			c.OnAlert(ctx, "disk.warning", "Host Storage Warning",
				"Disk usage has reached critical threshold: "+time.Now().Format(time.RFC3339))
		}
	}
}

// Start launches non-blocking polling loops for metrics (every 30s) and retention (hourly).
func (c *Collector) Start(ctx context.Context) {
	sampleTicker := time.NewTicker(30 * time.Second)
	pruneTicker := time.NewTicker(1 * time.Hour)

	// Run an immediate initial sample
	go c.CollectOnce(ctx)

	go func() {
		defer sampleTicker.Stop()
		defer pruneTicker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-c.stopCh:
				return
			case <-sampleTicker.C:
				c.CollectOnce(ctx)
			case <-pruneTicker.C:
				// Retain container metrics for 7 days
				if _, err := PruneMetrics(ctx, c.DB, 7*24*time.Hour); err != nil {
					slog.Warn("pruning container metrics", "err", err)
				}
				// Retain requests_minute for 14 days
				if _, err := PruneTraffic(ctx, c.DB, 14*24*time.Hour); err != nil {
					slog.Warn("pruning requests_minute", "err", err)
				}
				// Rotate access.log if > 10MB
				if err := c.Aggregator.RotateLogIfNeeded(10 * 1024 * 1024); err != nil {
					slog.Warn("rotating access log", "err", err)
				}
			}
		}
	}()
}

// Stop halts background collection.
func (c *Collector) Stop() {
	close(c.stopCh)
}
