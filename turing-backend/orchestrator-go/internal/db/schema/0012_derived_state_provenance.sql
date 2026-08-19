-- SQLite permits NULL in a non-INTEGER PRIMARY KEY unless NOT NULL is stated
-- explicitly. These columns are also the ownership links for derived rows, so
-- NULL would bypass their ON DELETE CASCADE constraints.
--
-- Fail before rebuilding if a legacy database contains such a row. Silently
-- deleting it would hide provenance loss; the named constraints tell the
-- operator which ownership link must be inspected, and the migration
-- transaction preserves the original tables and rows on failure.

CREATE TABLE derived_state_provenance_preflight (
  session_owner_null_count INTEGER NOT NULL
    CONSTRAINT derived_state_provenance_session_owner_required
    CHECK (session_owner_null_count = 0),
  run_owner_null_count INTEGER NOT NULL
    CONSTRAINT derived_state_provenance_run_owner_required
    CHECK (run_owner_null_count = 0)
);

INSERT INTO derived_state_provenance_preflight (session_owner_null_count, run_owner_null_count)
SELECT
  (SELECT COUNT(*) FROM session_external_agent WHERE session_id IS NULL),
  (SELECT COUNT(*) FROM automation_runs WHERE run_id IS NULL);

DROP TABLE derived_state_provenance_preflight;

CREATE TABLE session_external_agent_0012 (
  session_id TEXT NOT NULL PRIMARY KEY REFERENCES sessions(id) ON DELETE CASCADE,
  agent_id TEXT NOT NULL REFERENCES external_agents(id) ON DELETE CASCADE,
  routed_at TEXT NOT NULL
);

INSERT INTO session_external_agent_0012 (session_id, agent_id, routed_at)
SELECT session_id, agent_id, routed_at FROM session_external_agent;

DROP TABLE session_external_agent;
ALTER TABLE session_external_agent_0012 RENAME TO session_external_agent;

CREATE INDEX session_external_agent_by_agent
  ON session_external_agent (agent_id);

CREATE TABLE automation_runs_0012 (
  run_id TEXT NOT NULL PRIMARY KEY REFERENCES agent_runs(id) ON DELETE CASCADE,
  automation_id TEXT NOT NULL,
  automation_name TEXT NOT NULL,
  allowed_tools_json TEXT NOT NULL,
  fired_at TEXT NOT NULL
);

INSERT INTO automation_runs_0012 (run_id, automation_id, automation_name, allowed_tools_json, fired_at)
SELECT run_id, automation_id, automation_name, allowed_tools_json, fired_at FROM automation_runs;

DROP TABLE automation_runs;
ALTER TABLE automation_runs_0012 RENAME TO automation_runs;

CREATE INDEX automation_runs_by_automation
  ON automation_runs (automation_id, fired_at);

CREATE INDEX idx_audit_target
  ON audit_logs (target);
