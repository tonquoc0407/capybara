-- span_id deliberately carries no foreign key: a sample's whole purpose is to
-- outlive the span it was taken under. A span is only exported once it ends, so
-- the process that dies mid-call never writes the row this would point at.
CREATE TABLE resource_samples (
    run_id    TEXT    NOT NULL REFERENCES runs (id),
    span_id   TEXT    NOT NULL,
    ts        INTEGER NOT NULL,
    cpu_util  REAL,
    rss_bytes INTEGER,
    PRIMARY KEY (span_id, ts)
);

CREATE INDEX resource_samples_run ON resource_samples (run_id, ts);
