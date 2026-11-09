-- M6: restic, backup settings, and backup history.
ALTER TABLE settings ADD COLUMN restic_repository TEXT NOT NULL DEFAULT '';
ALTER TABLE settings ADD COLUMN restic_password   TEXT NOT NULL DEFAULT ''; -- secrets.Box ciphertext
ALTER TABLE settings ADD COLUMN s3_endpoint       TEXT NOT NULL DEFAULT '';
ALTER TABLE settings ADD COLUMN s3_bucket         TEXT NOT NULL DEFAULT '';
ALTER TABLE settings ADD COLUMN s3_region         TEXT NOT NULL DEFAULT '';
ALTER TABLE settings ADD COLUMN s3_access_key     TEXT NOT NULL DEFAULT '';
ALTER TABLE settings ADD COLUMN s3_secret_key     TEXT NOT NULL DEFAULT ''; -- secrets.Box ciphertext

CREATE TABLE backups (
    id          TEXT PRIMARY KEY,
    target_type TEXT NOT NULL,
    target_name TEXT NOT NULL,
    snapshot_id TEXT NOT NULL,
    tags        TEXT NOT NULL DEFAULT '',
    size_bytes  INTEGER NOT NULL DEFAULT 0,
    created_at  INTEGER NOT NULL
) STRICT;
