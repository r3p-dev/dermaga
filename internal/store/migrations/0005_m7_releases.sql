-- Migration 0005_m7_releases.sql
-- M7: immutable encrypted env versions that releases reference (DEP-4, DEC-32), the rolled_back
-- status (DEP-9), each release's non-secret runtime config, the rollback source, and watch paths (APP-8).

-- One row per saved runtime environment. Never updated; a release keeps pointing at the version it
-- was built with. env_encrypted uses the same AES-256-GCM column binding as apps.env_encrypted.
CREATE TABLE env_versions (
    id TEXT PRIMARY KEY,                       -- e.g. "env_5f3a..."
    app_id TEXT NOT NULL REFERENCES apps(id) ON DELETE CASCADE,
    version INTEGER NOT NULL,                  -- 1, 2, 3 ... per app
    env_encrypted TEXT NOT NULL,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
    UNIQUE(app_id, version)
);

-- Every existing app's current environment becomes its version 1 (same ciphertext, same binding).
INSERT INTO env_versions (id, app_id, version, env_encrypted)
    SELECT 'env_' || lower(hex(randomblob(12))), id, 1, env_encrypted FROM apps;

ALTER TABLE apps ADD COLUMN env_version_id TEXT REFERENCES env_versions(id);
UPDATE apps SET env_version_id = (SELECT v.id FROM env_versions v WHERE v.app_id = apps.id AND v.version = 1);
ALTER TABLE apps ADD COLUMN watch_paths TEXT NOT NULL DEFAULT '';

-- SQLite cannot change a CHECK constraint in place: rebuild deployments with 'rolled_back'.
CREATE TABLE deployments_new (
    id TEXT PRIMARY KEY,
    app_id TEXT NOT NULL REFERENCES apps(id) ON DELETE CASCADE,
    release_number INTEGER NOT NULL,
    status TEXT NOT NULL CHECK (status IN (
        'queued', 'building', 'starting', 'probing', 'switching',
        'active', 'inactive', 'rolled_back', 'failed', 'cancelled', 'superseded'
    )),
    trigger TEXT NOT NULL,                     -- push, manual, rollback
    commit_hash TEXT NOT NULL,
    commit_message TEXT NOT NULL,
    env_snapshot_encrypted TEXT NOT NULL DEFAULT '', -- pre-0005 releases only; converted to env_versions by the daemon
    env_version_id TEXT REFERENCES env_versions(id) ON DELETE SET NULL,
    runtime_config TEXT NOT NULL DEFAULT '',   -- JSON, no secrets: port, health check, command, limits
    source_deployment_id TEXT,                 -- the release a rollback restores
    build_logs TEXT NOT NULL DEFAULT '',
    error_message TEXT NOT NULL DEFAULT '',
    image_ref TEXT NOT NULL DEFAULT '',
    started_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
    finished_at TEXT,
    UNIQUE(app_id, release_number)
);

INSERT INTO deployments_new (
    id, app_id, release_number, status, trigger, commit_hash, commit_message,
    env_snapshot_encrypted, build_logs, error_message, image_ref, started_at, finished_at
)
SELECT
    id, app_id, release_number, status, trigger, commit_hash, commit_message,
    env_snapshot_encrypted, build_logs, error_message, image_ref, started_at, finished_at
FROM deployments;

DROP TABLE deployments;
ALTER TABLE deployments_new RENAME TO deployments;
