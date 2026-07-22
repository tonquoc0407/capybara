ALTER TABLE spans ADD COLUMN analyzed INTEGER NOT NULL DEFAULT 0;

CREATE INDEX spans_unanalyzed ON spans (run_id) WHERE analyzed = 0;
