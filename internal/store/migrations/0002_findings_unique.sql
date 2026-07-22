CREATE UNIQUE INDEX findings_unique
    ON findings (run_id, coalesce(span_id, ''), type, detail_json);
