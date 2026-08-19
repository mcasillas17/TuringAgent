-- SQLite permits NULL in a non-INTEGER PRIMARY KEY unless NOT NULL is stated
-- explicitly. These columns are also the ownership links for derived rows, so
-- NULL would bypass their ON DELETE CASCADE constraints.

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
