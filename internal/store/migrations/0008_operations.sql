ALTER TABLE runs ADD COLUMN project TEXT NOT NULL DEFAULT 'default';
ALTER TABLE runs ADD COLUMN environment TEXT NOT NULL DEFAULT '';
ALTER TABLE runs ADD COLUMN release TEXT NOT NULL DEFAULT '';
ALTER TABLE runs ADD COLUMN session_id TEXT NOT NULL DEFAULT '';
ALTER TABLE runs ADD COLUMN user_id TEXT NOT NULL DEFAULT '';

CREATE INDEX runs_project_started ON runs (project, started_at DESC, id DESC);

CREATE TABLE trace_logs (
    id TEXT PRIMARY KEY,
    run_id TEXT NOT NULL REFERENCES runs (id),
    span_id TEXT NOT NULL DEFAULT '',
    observed_at INTEGER,
    severity TEXT NOT NULL DEFAULT '',
    body TEXT NOT NULL DEFAULT '',
    attrs_json TEXT NOT NULL DEFAULT '{}'
);
CREATE INDEX trace_logs_run_time ON trace_logs (run_id, observed_at, id);
CREATE INDEX trace_logs_span ON trace_logs (span_id);
