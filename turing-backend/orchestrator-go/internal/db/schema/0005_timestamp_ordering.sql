ALTER TABLE agent_runs ADD COLUMN execution_lease_expires_at_ns INTEGER;
ALTER TABLE jobs ADD COLUMN lease_expires_at_ns INTEGER;

UPDATE agent_runs
SET execution_lease_expires_at_ns = CASE
  WHEN execution_lease_expires_at IS NULL THEN NULL
  ELSE
    CAST(strftime('%s', substr(execution_lease_expires_at, 1, 19) || 'Z') AS INTEGER) * 1000000000 +
    CASE
      WHEN instr(execution_lease_expires_at, '.') = 0 THEN 0
      ELSE CAST(substr(
        substr(execution_lease_expires_at, instr(execution_lease_expires_at, '.') + 1, length(execution_lease_expires_at) - instr(execution_lease_expires_at, '.') - 1) || '000000000',
        1,
        9
      ) AS INTEGER)
    END
END;

UPDATE jobs
SET lease_expires_at_ns = CASE
  WHEN lease_expires_at IS NULL THEN NULL
  ELSE
    CAST(strftime('%s', substr(lease_expires_at, 1, 19) || 'Z') AS INTEGER) * 1000000000 +
    CASE
      WHEN instr(lease_expires_at, '.') = 0 THEN 0
      ELSE CAST(substr(
        substr(lease_expires_at, instr(lease_expires_at, '.') + 1, length(lease_expires_at) - instr(lease_expires_at, '.') - 1) || '000000000',
        1,
        9
      ) AS INTEGER)
    END
END;

CREATE INDEX IF NOT EXISTS idx_runs_execution_recovery_ns ON agent_runs(status, execution_state, execution_lease_expires_at_ns);
CREATE INDEX IF NOT EXISTS idx_jobs_lease_ns ON jobs(status, lease_expires_at_ns);
