CREATE TABLE schema_migrations (
    version INTEGER PRIMARY KEY,
    applied_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
);

CREATE TABLE settings (
    key TEXT PRIMARY KEY,
    value TEXT NOT NULL,
    updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
);

CREATE TABLE apps (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL UNIQUE,
    repo_owner TEXT NOT NULL,
    repo_name TEXT NOT NULL,
    branch TEXT NOT NULL DEFAULT 'main',
    containerfile_path TEXT NOT NULL DEFAULT 'Containerfile',
    build_context TEXT NOT NULL DEFAULT '.',
    command TEXT,
    port INTEGER NOT NULL,
    health_type TEXT NOT NULL CHECK (health_type IN ('http', 'cmd', 'none')),
    health_path TEXT,
    health_cmd TEXT,
    health_timeout_sec INTEGER NOT NULL DEFAULT 30,
    env_encrypted TEXT NOT NULL DEFAULT '',
    build_env_encrypted TEXT NOT NULL DEFAULT '',
    auto_deploy INTEGER NOT NULL DEFAULT 1,
    active_release_id TEXT,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
    updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
);

CREATE TABLE domains (
    id TEXT PRIMARY KEY,
    app_id TEXT NOT NULL REFERENCES apps(id) ON DELETE CASCADE,
    domain TEXT NOT NULL UNIQUE,
    is_primary INTEGER NOT NULL DEFAULT 0,
    redirect_to TEXT,
    path_prefix TEXT,
    strip_prefix INTEGER NOT NULL DEFAULT 0,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
);

CREATE TABLE deployments (
    id TEXT PRIMARY KEY,
    app_id TEXT NOT NULL REFERENCES apps(id) ON DELETE CASCADE,
    release_number INTEGER NOT NULL,
    status TEXT NOT NULL CHECK (status IN (
        'queued', 'building', 'starting', 'probing', 'switching',
        'active', 'inactive', 'failed', 'cancelled', 'superseded'
    )),
    trigger TEXT NOT NULL,
    commit_hash TEXT NOT NULL,
    commit_message TEXT NOT NULL,
    env_snapshot_encrypted TEXT NOT NULL,
    build_logs TEXT NOT NULL DEFAULT '',
    started_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
    finished_at TEXT,
    UNIQUE(app_id, release_number)
);

CREATE TABLE sessions (
    id TEXT PRIMARY KEY,
    user_id TEXT NOT NULL,
    expires_at TEXT NOT NULL,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
);

CREATE TABLE audit_logs (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    origin TEXT NOT NULL CHECK (origin IN ('panel', 'webhook', 'server_command', 'daemon')),
    app_id TEXT REFERENCES apps(id) ON DELETE SET NULL,
    event TEXT NOT NULL,
    actor TEXT NOT NULL,
    description TEXT NOT NULL,
    details TEXT NOT NULL DEFAULT '{}',
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
);
