ALTER TABLE agent_runs ADD COLUMN execution_active INTEGER NOT NULL DEFAULT 0 CHECK (execution_active IN (0, 1));
ALTER TABLE agent_runs ADD COLUMN execution_exit_acknowledged_at TEXT;
ALTER TABLE agent_runs ADD COLUMN execution_attempt_id TEXT;
ALTER TABLE agent_runs ADD COLUMN execution_state TEXT NOT NULL DEFAULT 'none';
ALTER TABLE agent_runs ADD COLUMN execution_lease_expires_at TEXT;
ALTER TABLE jobs ADD COLUMN assignment_attempt_id TEXT;

CREATE INDEX IF NOT EXISTS idx_runs_session_execution_active ON agent_runs(session_id, execution_active);
CREATE INDEX IF NOT EXISTS idx_runs_execution_recovery ON agent_runs(status, execution_state, execution_lease_expires_at);
CREATE INDEX IF NOT EXISTS idx_jobs_assignment_attempt ON jobs(assignment_attempt_id);

-- A pre-0004 running row has no live stream that can acknowledge its exit. Keep
-- it fenced until the startup recovery transaction requeues or terminalizes it.
UPDATE agent_runs
SET execution_active = 1,
    execution_state = 'fenced'
WHERE status IN ('running', 'waiting_approval');

UPDATE agent_runs
SET execution_state = 'exited'
WHERE status IN ('completed', 'failed', 'cancelled');
