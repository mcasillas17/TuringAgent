-- Deleting a conversation withdraws each specialist session it delegated to
-- under that child's own deletion receipt, before the conversation's row goes.
--
-- A child's receipt names its parent so the parent's deletion can find it
-- again after the child's session row, and its delegations row with it, are
-- already gone. There is no foreign key: like the rest of this table, a
-- receipt outlives the sessions it names.
ALTER TABLE session_deletions ADD COLUMN parent_session_id TEXT;
CREATE INDEX idx_session_deletions_parent
  ON session_deletions(parent_session_id) WHERE parent_session_id IS NOT NULL;
