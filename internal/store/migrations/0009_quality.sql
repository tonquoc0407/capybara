CREATE TABLE datasets (
    id TEXT PRIMARY KEY,
    project TEXT NOT NULL DEFAULT 'default',
    name TEXT NOT NULL,
    created_at INTEGER NOT NULL,
    UNIQUE (project, name)
);

CREATE TABLE dataset_items (
    id TEXT PRIMARY KEY,
    dataset_id TEXT NOT NULL REFERENCES datasets (id) ON DELETE CASCADE,
    source_run_id TEXT NOT NULL REFERENCES runs (id),
    input_json TEXT NOT NULL,
    expected_json TEXT NOT NULL,
    created_at INTEGER NOT NULL,
    UNIQUE (dataset_id, source_run_id)
);
CREATE INDEX dataset_items_dataset ON dataset_items (dataset_id);

CREATE TABLE scores (
    id INTEGER PRIMARY KEY,
    run_id TEXT NOT NULL REFERENCES runs (id),
    item_id TEXT REFERENCES dataset_items (id),
    name TEXT NOT NULL,
    value REAL NOT NULL CHECK (value >= 0 AND value <= 1),
    source TEXT NOT NULL DEFAULT 'human',
    comment TEXT NOT NULL DEFAULT '',
    created_at INTEGER NOT NULL
);
CREATE INDEX scores_run_name ON scores (run_id, name);

CREATE TABLE experiments (
    id TEXT PRIMARY KEY,
    dataset_id TEXT NOT NULL REFERENCES datasets (id),
    name TEXT NOT NULL,
    created_at INTEGER NOT NULL,
    UNIQUE (dataset_id, name)
);

CREATE TABLE experiment_results (
    experiment_id TEXT NOT NULL REFERENCES experiments (id) ON DELETE CASCADE,
    item_id TEXT NOT NULL REFERENCES dataset_items (id),
    run_id TEXT NOT NULL REFERENCES runs (id),
    score_name TEXT NOT NULL,
    score REAL NOT NULL CHECK (score >= 0 AND score <= 1),
    PRIMARY KEY (experiment_id, item_id, score_name)
);
