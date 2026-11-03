CREATE TABLE prompt_versions (
    id TEXT PRIMARY KEY,
    project TEXT NOT NULL DEFAULT 'default',
    name TEXT NOT NULL,
    version INTEGER NOT NULL,
    sha256 TEXT NOT NULL,
    content TEXT NOT NULL,
    source_path TEXT NOT NULL DEFAULT '',
    created_at INTEGER NOT NULL,
    UNIQUE (project, name, version),
    UNIQUE (project, name, sha256)
);
CREATE INDEX prompt_versions_project_name ON prompt_versions (project, name, version DESC);
