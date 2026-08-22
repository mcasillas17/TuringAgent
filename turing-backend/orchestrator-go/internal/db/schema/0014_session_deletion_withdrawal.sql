ALTER TABLE sessions
  ADD COLUMN deletion_state TEXT NOT NULL DEFAULT 'active'
  CHECK (deletion_state IN ('active', 'deleting'));

CREATE TABLE session_deletions (
  session_id TEXT PRIMARY KEY,
  lifecycle_version INTEGER NOT NULL,
  state TEXT NOT NULL CHECK (state IN ('quiescing', 'artifacts', 'failed_external', 'completed')),
  quiesce_deadline_at TEXT NOT NULL,
  terminal_sequence INTEGER NOT NULL,
  terminal_at TEXT,
  deleted_at TEXT,
  error_code TEXT,
  retryable INTEGER NOT NULL CHECK (retryable IN (0, 1)),
  run_count INTEGER NOT NULL DEFAULT 0,
  message_count INTEGER NOT NULL DEFAULT 0,
  retained_legacy_artifact_count INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE sandbox_artifacts (
  id TEXT PRIMARY KEY,
  session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
  run_id TEXT NOT NULL REFERENCES agent_runs(id) ON DELETE CASCADE,
  logical_path_hash TEXT NOT NULL,
  physical_path TEXT NOT NULL,
  state TEXT NOT NULL CHECK (state IN ('writing', 'ready', 'delete_failed')),
  policy TEXT NOT NULL CHECK (policy IN ('delete_on_session_delete', 'retain_legacy_unowned')),
  deletion_generation INTEGER NOT NULL,
  created_at TEXT NOT NULL,
  finalized_at TEXT,
  UNIQUE(session_id, run_id, physical_path)
);

CREATE INDEX idx_sandbox_artifacts_session_state
  ON sandbox_artifacts (session_id, state);
