CREATE TABLE ingest_events (
    event_id TEXT PRIMARY KEY,
    payload_hash TEXT NOT NULL,
    run_id TEXT NOT NULL REFERENCES runs (id) ON DELETE CASCADE,
    span_id TEXT NOT NULL,
    received_at INTEGER NOT NULL
);

CREATE INDEX ingest_events_run_id ON ingest_events (run_id);
