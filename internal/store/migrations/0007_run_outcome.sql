-- Refresh outcomes recorded before terminal model and agent failures were
-- included. Tool failures alone remain non-fatal.
UPDATE runs SET status = CASE
  WHEN EXISTS (SELECT 1 FROM spans
               WHERE run_id = runs.id AND parent_id IS NULL AND status = 'error')
    THEN 'error'
  WHEN EXISTS (SELECT 1 FROM spans s
               WHERE s.run_id = runs.id AND s.kind = 'llm'
                 AND s.status = 'error' AND s.ended_at IS NOT NULL
                 AND NOT EXISTS (SELECT 1 FROM spans later
                                 WHERE later.run_id = s.run_id
                                   AND later.kind = 'llm' AND later.status = 'ok'
                                   AND later.ended_at > s.ended_at))
    THEN 'error'
  WHEN EXISTS (SELECT 1 FROM spans s
               WHERE s.run_id = runs.id AND s.kind = 'agent'
                 AND s.status = 'error' AND s.ended_at IS NOT NULL
                 AND NOT EXISTS (SELECT 1 FROM spans later
                                 WHERE later.run_id = s.run_id
                                   AND later.kind = 'agent' AND later.status = 'ok'
                                   AND later.parent_id IS s.parent_id
                                   AND later.ended_at > s.ended_at))
    THEN 'error'
  WHEN EXISTS (SELECT 1 FROM spans
               WHERE run_id = runs.id AND parent_id IS NULL
                 AND ended_at IS NOT NULL)
    THEN 'ok'
  ELSE 'running'
END;
