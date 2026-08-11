ALTER TABLE agent_runs ADD COLUMN execution_active INTEGER NOT NULL DEFAULT 0 CHECK (execution_active IN (0, 1));
ALTER TABLE agent_runs ADD COLUMN execution_exit_acknowledged_at TEXT;

CREATE INDEX IF NOT EXISTS idx_runs_session_execution_active ON agent_runs(session_id, execution_active);
