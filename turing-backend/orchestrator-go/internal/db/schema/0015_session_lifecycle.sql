DROP INDEX IF EXISTS idx_sessions_updated;
DROP INDEX IF EXISTS idx_sessions_status_updated;

CREATE INDEX idx_sessions_updated
  ON sessions(updated_at DESC, id DESC);

CREATE INDEX idx_sessions_status_updated
  ON sessions(status, updated_at DESC, id DESC);
