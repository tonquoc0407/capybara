CREATE TABLE IF NOT EXISTS incident_annotations (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    run_id     TEXT NOT NULL REFERENCES runs(id) ON DELETE CASCADE,
    project    TEXT NOT NULL,
    status     TEXT NOT NULL CHECK (status IN ('open', 'investigating', 'resolved', 'ignored')),
    note       TEXT NOT NULL,
    author     TEXT NOT NULL,
    created_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS incident_annotations_run_created
    ON incident_annotations(run_id, created_at, id);
