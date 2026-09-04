ALTER TABLE resource_samples ADD COLUMN gpu_util REAL;
ALTER TABLE resource_samples ADD COLUMN gpu_mem_bytes INTEGER;

-- The span a crash interrupted is never exported, so its name arrives on the
-- reading or not at all.
ALTER TABLE resource_samples ADD COLUMN span_name TEXT;
