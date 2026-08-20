-- TUR-006 splits the single internal identity into two: the runtime, and the
-- approval consumer (mcp-files, and any future MCP server that consumes
-- approvals). An authorization failure on the internal gRPC port is now
-- attributed to whichever of those resolved — or to no identity at all — and
-- the audit trail's CHECK constraint enumerates the actor kinds it accepts.
--
-- Without this widening, auth.recordIdentityFailure's two new values
-- ('approval-consumer' for an over-reaching but real identity, and
-- 'internal-unknown' for an unrecognized or malformed bearer) violate the
-- existing CHECK and the INSERT silently fails — the audit trail loses
-- exactly the two events this task most needs it to keep: proof a specific
-- service over-reached, and proof something presented no valid internal
-- credential at all. Only the pre-existing 'runtime' value happened to keep
-- working, which made the gap easy to miss.
--
-- SQLite cannot alter a CHECK in place, so the table is rebuilt. The copy
-- preserves rowid, which is what audit reads order by.
CREATE TABLE IF NOT EXISTS audit_logs_0013 (
  id TEXT PRIMARY KEY,
  correlation_id TEXT,
  actor_type TEXT NOT NULL CHECK (actor_type IN ('client','runtime','mcp','system','automation','approval-consumer','internal-unknown')),
  actor_id TEXT,
  action TEXT NOT NULL,
  target TEXT,
  payload_json TEXT,
  created_at TEXT NOT NULL
);

INSERT INTO audit_logs_0013 (rowid, id, correlation_id, actor_type, actor_id, action, target, payload_json, created_at)
SELECT rowid, id, correlation_id, actor_type, actor_id, action, target, payload_json, created_at FROM audit_logs;

DROP TABLE audit_logs;

ALTER TABLE audit_logs_0013 RENAME TO audit_logs;

CREATE INDEX IF NOT EXISTS idx_audit_action ON audit_logs(action, created_at);
CREATE INDEX IF NOT EXISTS idx_audit_correlation ON audit_logs(correlation_id, created_at);
CREATE INDEX IF NOT EXISTS idx_audit_created_at ON audit_logs(created_at);
CREATE INDEX IF NOT EXISTS idx_audit_target ON audit_logs(target);
