-- M9: metrics, requests_minute, and notification settings.
ALTER TABLE settings ADD COLUMN notify_provider TEXT NOT NULL DEFAULT '';
ALTER TABLE settings ADD COLUMN notify_target   TEXT NOT NULL DEFAULT ''; -- URL / chat_id / topic
ALTER TABLE settings ADD COLUMN notify_token    TEXT NOT NULL DEFAULT ''; -- secrets.Box ciphertext
ALTER TABLE settings ADD COLUMN notify_events   TEXT NOT NULL DEFAULT 'deploy.failed,backup.failed,disk.warning';

CREATE TABLE metrics (
    ts          INTEGER NOT NULL,
    container   TEXT NOT NULL,
    cpu_percent REAL NOT NULL DEFAULT 0.0,
    mem_bytes   INTEGER NOT NULL DEFAULT 0,
    mem_limit   INTEGER NOT NULL DEFAULT 0,
    net_in      INTEGER NOT NULL DEFAULT 0,
    net_out     INTEGER NOT NULL DEFAULT 0,
    block_in    INTEGER NOT NULL DEFAULT 0,
    block_out   INTEGER NOT NULL DEFAULT 0
) STRICT;

CREATE INDEX idx_metrics_ts ON metrics(ts);
CREATE INDEX idx_metrics_container_ts ON metrics(container, ts);

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
) STRICT;

CREATE INDEX idx_requests_minute_ts ON requests_minute(minute);
