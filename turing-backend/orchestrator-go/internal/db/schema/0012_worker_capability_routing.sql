ALTER TABLE jobs ADD COLUMN created_at_ns INTEGER;

UPDATE jobs
SET created_at_ns =
  CAST(strftime('%s', substr(created_at, 1, 19) || 'Z') AS INTEGER) * 1000000000 +
  CASE
    WHEN instr(created_at, '.') = 0 THEN 0
    ELSE CAST(substr(
      substr(created_at, instr(created_at, '.') + 1, length(created_at) - instr(created_at, '.') - 1) || '000000000',
      1,
      9
    ) AS INTEGER)
  END;

CREATE INDEX IF NOT EXISTS idx_jobs_capability_claim
  ON jobs(agent_id, status, created_at_ns, id);

CREATE INDEX IF NOT EXISTS idx_jobs_pending_routing
  ON jobs(status, created_at_ns, id);
