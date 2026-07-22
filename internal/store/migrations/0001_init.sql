CREATE TABLE runs (
    id         TEXT PRIMARY KEY,
    source     TEXT NOT NULL,
    started_at INTEGER,
    ended_at   INTEGER,
    model_main TEXT NOT NULL DEFAULT '',
    tokens_in  INTEGER NOT NULL DEFAULT 0,
    tokens_out INTEGER NOT NULL DEFAULT 0,
    cost_usd   REAL,
    status     TEXT NOT NULL DEFAULT 'running',
    label      TEXT NOT NULL DEFAULT ''
);

CREATE TABLE spans (
    id         TEXT PRIMARY KEY,
    run_id     TEXT NOT NULL REFERENCES runs (id),
    parent_id  TEXT,
    kind       TEXT NOT NULL,
    name       TEXT NOT NULL,
    started_at INTEGER,
    ended_at   INTEGER,
    tokens_in  INTEGER NOT NULL DEFAULT 0,
    tokens_out INTEGER NOT NULL DEFAULT 0,
    cost_usd   REAL,
    status     TEXT NOT NULL,
    attrs_json TEXT NOT NULL DEFAULT '{}'
);

CREATE INDEX spans_run_id ON spans (run_id);

CREATE TABLE contents (
    span_id    TEXT NOT NULL REFERENCES spans (id),
    role       TEXT NOT NULL,
    seq        INTEGER NOT NULL,
    body       TEXT NOT NULL,
    media_type TEXT NOT NULL,
    PRIMARY KEY (span_id, seq)
);

CREATE TABLE tool_schemas (
    tool_name        TEXT NOT NULL,
    version          INTEGER NOT NULL,
    schema_json      TEXT NOT NULL,
    learned_from_run TEXT,
    first_seen       INTEGER,
    last_seen        INTEGER,
    PRIMARY KEY (tool_name, version)
);

CREATE TABLE findings (
    id          INTEGER PRIMARY KEY,
    run_id      TEXT NOT NULL REFERENCES runs (id),
    span_id     TEXT REFERENCES spans (id),
    type        TEXT NOT NULL,
    severity    TEXT NOT NULL,
    detail_json TEXT NOT NULL DEFAULT '{}'
);

CREATE INDEX findings_run_id ON findings (run_id);

CREATE TABLE taints (
    run_id         TEXT NOT NULL REFERENCES runs (id),
    span_id        TEXT NOT NULL REFERENCES spans (id),
    source_span_id TEXT NOT NULL REFERENCES spans (id),
    PRIMARY KEY (run_id, span_id, source_span_id)
);

CREATE TABLE llm_cache (
    run_id        TEXT NOT NULL REFERENCES runs (id),
    span_id       TEXT NOT NULL REFERENCES spans (id),
    request_hash  TEXT NOT NULL,
    response_json TEXT NOT NULL,
    PRIMARY KEY (run_id, span_id)
);
