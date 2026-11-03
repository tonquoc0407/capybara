ALTER TABLE findings ADD COLUMN origin TEXT NOT NULL DEFAULT 'import';

-- Earlier versions did not record provenance. The built-in importers only
-- produce parse_error; these detector types came from the analyzer.
UPDATE findings SET origin = 'analyzer_span' WHERE type IN
    ('malformed_arguments', 'empty_payload', 'drift', 'tool_error', 'malformed', 'rate_limited');
UPDATE findings SET origin = 'analyzer_run' WHERE type IN
    ('improvised', 'prompt_injection', 'unsupported_claim', 'secret_leak',
     'no_progress', 'truncated', 'loop', 'cost_spike', 'oscillation');
UPDATE findings SET origin = 'analyzer_orphan' WHERE type = 'orphaned_span';

CREATE INDEX findings_run_origin ON findings (run_id, origin);
