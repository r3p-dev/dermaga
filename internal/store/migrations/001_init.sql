-- M1: settings, panel sessions, audit log. Times are unix seconds.

CREATE TABLE settings (
    id                  INTEGER PRIMARY KEY CHECK (id = 1),
    admin_password_hash TEXT NOT NULL DEFAULT '',  -- argon2id PHC string; '' = not set, login impossible
    totp_secret         TEXT NOT NULL DEFAULT ''   -- secrets.Box ciphertext of the base32 secret; '' = TOTP off
) STRICT;

INSERT INTO settings (id) VALUES (1);

CREATE TABLE sessions (
    id_hash      BLOB PRIMARY KEY,  -- SHA-256 of the cookie value; the value itself is never stored
    created_at   INTEGER NOT NULL,
    last_seen_at INTEGER NOT NULL
) STRICT;

CREATE TABLE audit_log (
    id      INTEGER PRIMARY KEY,
    ts      INTEGER NOT NULL,
    actor   TEXT NOT NULL CHECK (actor IN ('panel', 'cli', 'webhook', 'timer')),
    action  TEXT NOT NULL,
    target  TEXT NOT NULL DEFAULT '',
    details TEXT NOT NULL DEFAULT ''  -- never contains secrets
) STRICT;
