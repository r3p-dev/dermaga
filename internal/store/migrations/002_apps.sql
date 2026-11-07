-- M2: apps, their domains and releases, as far as the generators and startup
-- reconciliation read them. M3/M4 add the source, trigger and log columns.

CREATE TABLE apps (
    id         TEXT PRIMARY KEY,           -- random; used in the webhook URL (M3)
    name       TEXT NOT NULL UNIQUE,
    spec       TEXT NOT NULL DEFAULT '{}', -- JSON of spec.App without name and domains
    created_at INTEGER NOT NULL
) STRICT;

CREATE TABLE domains (
    host       TEXT PRIMARY KEY,           -- a host belongs to one app
    app_id     TEXT NOT NULL REFERENCES apps (id) ON DELETE CASCADE,
    is_primary INTEGER NOT NULL DEFAULT 0 CHECK (is_primary IN (0, 1)),
    redirect   INTEGER NOT NULL DEFAULT 0 CHECK (redirect IN (0, 1)),  -- 301 to the primary domain
    CHECK (NOT (is_primary AND redirect))
) STRICT;

CREATE UNIQUE INDEX domains_one_primary ON domains (app_id) WHERE is_primary = 1;

CREATE TABLE releases (
    id         INTEGER PRIMARY KEY,
    app_id     TEXT NOT NULL REFERENCES apps (id) ON DELETE CASCADE,
    rel8       TEXT NOT NULL CHECK (length(rel8) = 8),  -- container dg-<app>-<rel8>
    image      TEXT NOT NULL,
    status     TEXT NOT NULL CHECK (status IN
                 ('queued', 'building', 'starting', 'healthy', 'live', 'failed', 'rolled_back', 'superseded')),
    created_at INTEGER NOT NULL,
    UNIQUE (app_id, rel8)
) STRICT;

CREATE UNIQUE INDEX releases_one_live ON releases (app_id) WHERE status = 'live';
