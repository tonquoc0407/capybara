ALTER TABLE runs ADD COLUMN parent_run_id TEXT REFERENCES runs (id);

CREATE INDEX llm_cache_lookup ON llm_cache (run_id, request_hash);
