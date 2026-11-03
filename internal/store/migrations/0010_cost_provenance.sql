CREATE TABLE span_cost_provenance (
    span_id TEXT PRIMARY KEY REFERENCES spans (id) ON DELETE CASCADE,
    kind TEXT NOT NULL CHECK (kind IN ('estimate', 'provider')),
    price_source TEXT NOT NULL,
    price_version TEXT NOT NULL,
    price_effective_at INTEGER,
    checked_at INTEGER NOT NULL,
    usage_source TEXT NOT NULL
);
