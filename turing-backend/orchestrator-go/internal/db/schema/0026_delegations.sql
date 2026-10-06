-- Delegation: a Turing run asks a specialist to do one task.
--
-- A specialist works in a hidden delegation session whose parent is the
-- conversation the delegation was made from. Deleting the parent deletes its
-- children with it; the foreign key is a backstop for that, not the plan.
ALTER TABLE sessions ADD COLUMN parent_session_id TEXT
  REFERENCES sessions(id) ON DELETE CASCADE;
-- Deleting any session looks up its children through this key.
CREATE INDEX idx_sessions_parent ON sessions(parent_session_id) WHERE parent_session_id IS NOT NULL;

-- One row per delegation. It has no state of its own: a delegation's state is
-- its child run's status, read through child_run_id, so there is no second
-- copy to keep in step. Every foreign key cascades, again as a backstop.
CREATE TABLE delegations (
  id                  TEXT PRIMARY KEY,
  parent_session_id   TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
  parent_run_id       TEXT NOT NULL REFERENCES agent_runs(id) ON DELETE CASCADE,
  parent_tool_call_id TEXT NOT NULL,
  child_session_id    TEXT NOT NULL UNIQUE REFERENCES sessions(id) ON DELETE CASCADE,
  child_run_id        TEXT NOT NULL UNIQUE REFERENCES agent_runs(id) ON DELETE CASCADE,
  profile_id          TEXT NOT NULL,
  profile_revision    TEXT NOT NULL,
  -- The approvals' canonical-arguments hash. A retried tool call must match it.
  args_hash           TEXT NOT NULL,
  joined              INTEGER NOT NULL DEFAULT 0 CHECK (joined IN (0, 1)),
  result_bytes        INTEGER,
  error_code          TEXT,
  created_at          TEXT NOT NULL,
  finished_at         TEXT,
  -- One delegation per tool call. Its leftmost column also serves lookups by
  -- parent run.
  UNIQUE (parent_run_id, parent_tool_call_id)
);

CREATE INDEX idx_delegations_parent_session ON delegations(parent_session_id);
