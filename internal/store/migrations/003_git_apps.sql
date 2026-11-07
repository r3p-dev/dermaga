-- M3: webhook secrets, env vars, deploy details per release, webhook deliveries, hook host.

ALTER TABLE apps ADD COLUMN webhook_secret TEXT NOT NULL DEFAULT '';  -- secrets.Box, AAD "apps.webhook_secret"

CREATE TABLE env_vars (
    app_id TEXT NOT NULL REFERENCES apps (id) ON DELETE CASCADE,
    key    TEXT NOT NULL,
    value  TEXT NOT NULL,                 -- secrets.Box, AAD "env_vars.value"
    PRIMARY KEY (app_id, key)
) STRICT;

ALTER TABLE releases ADD COLUMN trigger TEXT NOT NULL DEFAULT 'manual'
    CHECK (trigger IN ('webhook', 'manual', 'rollback'));
ALTER TABLE releases ADD COLUMN ref TEXT NOT NULL DEFAULT '';         -- what was asked for (branch or SHA)
ALTER TABLE releases ADD COLUMN sha TEXT NOT NULL DEFAULT '';         -- resolved commit (git apps)
ALTER TABLE releases ADD COLUMN message TEXT NOT NULL DEFAULT '';     -- commit subject
ALTER TABLE releases ADD COLUMN error TEXT NOT NULL DEFAULT '';
ALTER TABLE releases ADD COLUMN log_path TEXT NOT NULL DEFAULT '';
ALTER TABLE releases ADD COLUMN finished_at INTEGER NOT NULL DEFAULT 0;

CREATE TABLE webhook_deliveries (
    delivery_id TEXT PRIMARY KEY,          -- X-GitHub-Delivery: dedupe
    app_id      TEXT NOT NULL REFERENCES apps (id) ON DELETE CASCADE,
    received_at INTEGER NOT NULL,
    result      TEXT NOT NULL
) STRICT;

ALTER TABLE settings ADD COLUMN hook_host TEXT NOT NULL DEFAULT '';
