CREATE INDEX spans_unpriced_llm_run ON spans (run_id)
WHERE kind = 'llm' AND cost_usd IS NULL;
