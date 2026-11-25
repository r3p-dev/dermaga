CREATE TABLE github_installations (
    id INTEGER PRIMARY KEY,
    account_login TEXT NOT NULL,
    account_type TEXT NOT NULL,
    repository_selection TEXT NOT NULL DEFAULT 'selected',
    suspended_at TEXT,
    html_url TEXT NOT NULL DEFAULT '',
    updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
);

CREATE TABLE github_repositories (
    id INTEGER PRIMARY KEY,
    installation_id INTEGER NOT NULL REFERENCES github_installations(id) ON DELETE CASCADE,
    full_name TEXT NOT NULL UNIQUE,
    owner TEXT NOT NULL,
    name TEXT NOT NULL,
    private INTEGER NOT NULL DEFAULT 0,
    default_branch TEXT NOT NULL DEFAULT 'main',
    html_url TEXT NOT NULL DEFAULT '',
    updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
);

CREATE TABLE webhook_deliveries (
    delivery_id TEXT PRIMARY KEY,
    event TEXT NOT NULL,
    action TEXT NOT NULL DEFAULT '',
    repository_full_name TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL,
    received_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
);
