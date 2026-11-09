-- M5: databases and app linking.
-- Databases run in dedicated dg-db-<name> networks, never on web.
-- Credentials are encrypted with secrets.Box (AAD "databases.credentials").

CREATE TABLE databases (
    id               TEXT PRIMARY KEY,
    name             TEXT NOT NULL UNIQUE,
    engine           TEXT NOT NULL CHECK (engine IN ('postgres', 'mariadb', 'valkey')),
    version          TEXT NOT NULL,
    volume           TEXT NOT NULL,
    credentials      TEXT NOT NULL,
    backup_schedule  TEXT NOT NULL DEFAULT '03:00',
    backup_retention TEXT NOT NULL DEFAULT '--keep-daily 7 --keep-weekly 4 --keep-monthly 6',
    created_at       INTEGER NOT NULL
) STRICT;

CREATE TABLE db_links (
    database_id TEXT NOT NULL REFERENCES databases (id) ON DELETE CASCADE,
    app_id      TEXT NOT NULL REFERENCES apps (id) ON DELETE CASCADE,
    env_names   TEXT NOT NULL DEFAULT '{}',
    created_at  INTEGER NOT NULL,
    PRIMARY KEY (database_id, app_id)
) STRICT;
