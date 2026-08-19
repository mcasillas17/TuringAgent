-- Automations: a saved prompt the orchestrator sends on a schedule, without
-- the user starting it.
--
-- next_due_at is the whole of the scheduler's state. It lives here rather than
-- in memory so that restarting the process cannot re-fire something that
-- already ran, and so that two ticks racing each other resolve on a row the
-- database serialises rather than on a lock one process holds.
CREATE TABLE IF NOT EXISTS automations (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  prompt TEXT NOT NULL,
  -- 'interval' or 'daily'. Deliberately not a cron expression: two shapes that
  -- can be checked by reading them beat one that needs a parser to be trusted.
  schedule_kind TEXT NOT NULL,
  -- Set for 'interval', NULL for 'daily'.
  interval_seconds INTEGER,
  -- Set for 'daily': minutes past midnight UTC. NULL for 'interval'.
  daily_minute_utc INTEGER,
  enabled INTEGER NOT NULL DEFAULT 1,
  -- NULL exactly when the automation is disabled. A disabled automation has no
  -- next run, and a stale timestamp sitting here would be a claim about the
  -- future that is false.
  next_due_at TEXT,
  last_run_at TEXT,
  last_run_id TEXT,
  -- The one conversation this automation's runs land in, created on first
  -- fire. ON DELETE SET NULL rather than CASCADE: deleting the conversation is
  -- a request to forget what was said, not to delete the schedule that said
  -- it. The next fire makes a new one.
  session_id TEXT REFERENCES sessions(id) ON DELETE SET NULL,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

-- Names identify an automation to the user and appear in the audit trail of
-- every approval it grants itself, so two sharing a name would make both
-- records ambiguous. Case-insensitive, because "Digest" and "digest" are the
-- same name to a person.
CREATE UNIQUE INDEX IF NOT EXISTS automations_name_unique ON automations (name COLLATE NOCASE);

-- The scheduler's only query: enabled rows that have come due.
CREATE INDEX IF NOT EXISTS automations_due ON automations (enabled, next_due_at);

-- The tools an automation may run without stopping to ask.
--
-- A row per tool, never a wildcard column: the schema itself should make
-- "allow everything" un-expressible, so no later code path can quietly grant
-- it. Keyed by (server, tool) to match the orchestrator's policy lookup, so an
-- entry cannot match a same-named tool on a different server.
CREATE TABLE IF NOT EXISTS automation_allowed_tools (
  automation_id TEXT NOT NULL REFERENCES automations(id) ON DELETE CASCADE,
  server_name TEXT NOT NULL,
  tool_name TEXT NOT NULL,
  PRIMARY KEY (automation_id, server_name, tool_name)
);

-- What an unattended run was permitted to do, frozen at the moment it fired.
--
-- The allowlist is snapshotted here rather than read live from
-- automation_allowed_tools for the same reason a job payload freezes its
-- skills: editing the allowlist while a run is in flight must not change what
-- that run may already be doing.
--
-- automation_id carries no foreign key on purpose. A cascade from a deleted
-- automation would remove this row out from under a run still executing, and
-- that run would stop looking like an automation run — meaning its next
-- approval-required tool would sit waiting for a person who is not there.
-- Deleting an automation stops future fires; it does not retract consent
-- already given to a run underway, and it does not erase the record of it.
CREATE TABLE IF NOT EXISTS automation_runs (
  run_id TEXT PRIMARY KEY REFERENCES agent_runs(id) ON DELETE CASCADE,
  automation_id TEXT NOT NULL,
  automation_name TEXT NOT NULL,
  allowed_tools_json TEXT NOT NULL,
  fired_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS automation_runs_by_automation ON automation_runs (automation_id, fired_at);

-- An automation is a new kind of actor, and the audit trail's CHECK
-- constraint enumerates the kinds it will accept.
--
-- Widening it is the point of this half of the migration: an approval granted
-- by an automation's allowlist and one granted by a person are the same
-- transition on the same approvals row, so actor_type is the only place they
-- can be told apart afterwards. Recording an unattended approval as 'client'
-- would make the record say a person decided, which is exactly the thing an
-- operator needs to be able to disbelieve.
--
-- SQLite cannot alter a CHECK in place, so the table is rebuilt. The copy
-- preserves rowid, which is what audit reads order by.
CREATE TABLE IF NOT EXISTS audit_logs_0009 (
  id TEXT PRIMARY KEY,
  correlation_id TEXT,
  actor_type TEXT NOT NULL CHECK (actor_type IN ('client','runtime','mcp','system','automation')),
  actor_id TEXT,
  action TEXT NOT NULL,
  target TEXT,
  payload_json TEXT,
  created_at TEXT NOT NULL
);

INSERT INTO audit_logs_0009 (rowid, id, correlation_id, actor_type, actor_id, action, target, payload_json, created_at)
SELECT rowid, id, correlation_id, actor_type, actor_id, action, target, payload_json, created_at FROM audit_logs;

DROP TABLE audit_logs;

ALTER TABLE audit_logs_0009 RENAME TO audit_logs;

CREATE INDEX IF NOT EXISTS idx_audit_action ON audit_logs(action, created_at);
CREATE INDEX IF NOT EXISTS idx_audit_correlation ON audit_logs(correlation_id, created_at);
