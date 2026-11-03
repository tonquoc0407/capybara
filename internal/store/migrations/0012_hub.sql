CREATE TABLE hub_tokens (
    id TEXT PRIMARY KEY,
    token_hash TEXT NOT NULL UNIQUE,
    project TEXT NOT NULL,
    role TEXT NOT NULL CHECK (role IN ('admin', 'editor', 'viewer')),
    created_at INTEGER NOT NULL,
    revoked_at INTEGER
);
CREATE INDEX hub_tokens_project ON hub_tokens (project, revoked_at);

CREATE TABLE audit_events (
    id INTEGER PRIMARY KEY,
    at INTEGER NOT NULL,
    actor_token_id TEXT,
    action TEXT NOT NULL,
    project TEXT NOT NULL,
    detail TEXT NOT NULL DEFAULT ''
);
CREATE INDEX audit_events_project_at ON audit_events (project, at DESC);
