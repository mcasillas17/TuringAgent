-- TUR-009 canonical run outcomes.
--
-- This file is executed in named sections rather than as one statement batch.
-- The runner splits it on the marker comments below, runs each section inside
-- the same transaction as the Go Before/After hooks, and offers a test seam
-- after each marker so a rollback can be proven at every boundary. The markers
-- must appear exactly once, in this order.
--
-- The rebuild runs on a dedicated connection with PRAGMA foreign_keys=OFF,
-- because agent_runs is the parent of every run-owned child table and dropping
-- it with cascades armed would delete the conversation this migration exists to
-- preserve. PRAGMA foreign_key_check runs before commit, and the runner proves
-- foreign keys are back on afterwards.

-- Rebuilt rather than ALTERed: status has to admit 'recovering', and the new
-- state columns have to be NOT NULL with no default, which ALTER TABLE ADD
-- COLUMN cannot express. Every pre-existing column, default, nullability,
-- foreign key, and check is restated verbatim; the only intentional
-- differences are the widened status set and the appended canonical state.
CREATE TABLE agent_runs_0016 (
  id TEXT PRIMARY KEY,
  session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
  user_message_id TEXT NOT NULL REFERENCES messages(id),
  assistant_message_id TEXT REFERENCES messages(id),
  agent_id TEXT NOT NULL,
  trace_id TEXT NOT NULL,
  -- 'recovering' is the honest name for a run whose worker ownership is
  -- uncertain or fenced. Calling that interval 'running' claimed forward
  -- progress nobody was making.
  status TEXT NOT NULL CHECK (status IN ('queued','running','waiting_approval','recovering','completed','failed','cancelled')),
  model_provider TEXT NOT NULL,
  model_name TEXT NOT NULL,
  error_code TEXT,
  error_message TEXT,
  created_at TEXT NOT NULL,
  started_at TEXT,
  finished_at TEXT,
  cancellation_reason TEXT,
  worker_id TEXT,
  execution_active INTEGER NOT NULL DEFAULT 0 CHECK (execution_active IN (0, 1)),
  execution_exit_acknowledged_at TEXT,
  execution_attempt_id TEXT,
  execution_state TEXT NOT NULL DEFAULT 'none',
  execution_lease_expires_at TEXT,
  execution_lease_expires_at_ns INTEGER,
  input_tokens INTEGER,
  output_tokens INTEGER,
  external_agent_name TEXT,
  external_agent_host TEXT,
  -- Per-run monotonic version. Zero is protobuf absence, never a stored value,
  -- and the upper bound is SQLite's signed maximum: a transition at that value
  -- must fail closed rather than wrap into a version clients would accept as
  -- older.
  state_version INTEGER NOT NULL CHECK (state_version BETWEEN 1 AND 9223372036854775807),
  -- Fixed-width canonical UTC nanoseconds. The length check is the schema's
  -- share of the ordering guarantee: this column is compared as text, so a
  -- variable-width or offset-bearing value would sort wrongly against its
  -- neighbours.
  state_updated_at TEXT NOT NULL CHECK (length(state_updated_at) = 30),
  outcome_reason TEXT NOT NULL CHECK (outcome_reason IN (
    'none','completed_no_content','user_cancelled','abandoned','expired','context_limit',
    'provider_failure','tool_failure','policy_denied','retries_exhausted','recovery_interrupted',
    'side_effect_uncertain','approval_delivery_failed','internal_failure'
  )),
  -- Internal identity of the exact persisted assistant bytes. Never returned
  -- publicly; it exists so a duplicate terminal report can be recognized as the
  -- same report rather than a conflicting one.
  assistant_content_sha256 TEXT NOT NULL CHECK (length(assistant_content_sha256) = 64)
  -- The lifecycle/outcome matrix is NOT a column constraint here. Existing
  -- writers terminalize a run without yet touching outcome_reason, and pairing
  -- the two atomically is the versioned-transition work; a cross-column check
  -- added now would reject writes this change does not own. The closed
  -- vocabulary above is what the schema can honestly enforce today.
);

-- LEFT JOIN on purpose. The state columns are NOT NULL with no default, so a
-- run the Before hook failed to classify aborts the migration here instead of
-- being silently dropped by an inner join.
INSERT INTO agent_runs_0016 (
  id, session_id, user_message_id, assistant_message_id, agent_id, trace_id, status,
  model_provider, model_name, error_code, error_message, created_at, started_at, finished_at,
  cancellation_reason, worker_id, execution_active, execution_exit_acknowledged_at,
  execution_attempt_id, execution_state, execution_lease_expires_at, execution_lease_expires_at_ns,
  input_tokens, output_tokens, external_agent_name, external_agent_host,
  state_version, state_updated_at, outcome_reason, assistant_content_sha256
)
SELECT
  r.id, r.session_id, r.user_message_id, r.assistant_message_id, r.agent_id, r.trace_id,
  b.lifecycle,
  r.model_provider, r.model_name, r.error_code, r.error_message, r.created_at, r.started_at, r.finished_at,
  r.cancellation_reason, r.worker_id, r.execution_active, r.execution_exit_acknowledged_at,
  r.execution_attempt_id, r.execution_state, r.execution_lease_expires_at, r.execution_lease_expires_at_ns,
  r.input_tokens, r.output_tokens, r.external_agent_name, r.external_agent_host,
  b.state_version, b.state_updated_at, b.outcome_reason, b.assistant_content_sha256
FROM agent_runs r
LEFT JOIN run_outcomes_backfill b ON b.run_id = r.id;

DROP TABLE agent_runs;

ALTER TABLE agent_runs_0016 RENAME TO agent_runs;

-- Recreated because dropping the table dropped them with it.
CREATE INDEX IF NOT EXISTS idx_runs_session_created ON agent_runs(session_id, created_at);
CREATE INDEX IF NOT EXISTS idx_runs_status ON agent_runs(status, created_at);
CREATE INDEX IF NOT EXISTS idx_runs_session_execution_active ON agent_runs(session_id, execution_active);
CREATE INDEX IF NOT EXISTS idx_runs_execution_recovery ON agent_runs(status, execution_state, execution_lease_expires_at);
CREATE INDEX IF NOT EXISTS idx_runs_execution_recovery_ns ON agent_runs(status, execution_state, execution_lease_expires_at_ns);

-- marker: after-rebuild

-- Every raw diagnostic string this system ever stored came from a provider, a
-- tool, or a worker, and several of them reached public responses. The
-- canonical run row now carries the outcome, so the strings have no remaining
-- job and are removed rather than left for a future reader to surface.
UPDATE agent_runs SET error_message = NULL WHERE error_message IS NOT NULL;
UPDATE jobs SET error_message = NULL WHERE error_message IS NOT NULL;
UPDATE tool_calls SET error_message = NULL WHERE error_message IS NOT NULL;

-- Codes are server-chosen constants, but only the approved vocabulary is
-- allowlisted. Anything else is replaced by a safe code chosen from trusted row
-- context: a run routed off this machine reads as a provider failure, a tool
-- call as a tool failure or a policy denial, and everything else as internal.
UPDATE agent_runs
SET error_code = CASE WHEN external_agent_name IS NOT NULL THEN 'provider_failure' ELSE 'internal_failure' END
WHERE error_code IS NOT NULL
  AND error_code NOT IN (
    'message_fetch_failed','external_agent_unavailable','model_provider_unavailable',
    'tool_discovery_failed','context_budget_exceeded','model_timeout','model_stream_failed',
    'model_output_limit_exceeded','model_unavailable','model_auth_failed','model_request_failed',
    'model_error','model_quota_exceeded','model_bad_chunk','model_stream_error','tool_call_failed',
    'tool_call_limit_exceeded','tool_result_limit_exceeded','runtime_error','retries_exhausted',
    'job_timeout','side_effect_uncertain','approval_delivery_failed','approval_expired',
    'automation_approval_failed','automation_tool_not_allowlisted','worker_busy','worker_unavailable',
    'tool_policy_decision_failed','tool_policy_decision_invalid','approval_wait_failed',
    'mcp_call_failed','unknown_tool','tool_runner_unavailable','client_cancelled','cancelled',
    'run_cancelled','approval_denied'
  );

UPDATE jobs
SET error_code = 'internal_failure'
WHERE error_code IS NOT NULL
  AND error_code NOT IN (
    'message_fetch_failed','external_agent_unavailable','model_provider_unavailable',
    'tool_discovery_failed','context_budget_exceeded','model_timeout','model_stream_failed',
    'model_output_limit_exceeded','model_unavailable','model_auth_failed','model_request_failed',
    'model_error','model_quota_exceeded','model_bad_chunk','model_stream_error','tool_call_failed',
    'tool_call_limit_exceeded','tool_result_limit_exceeded','runtime_error','retries_exhausted',
    'job_timeout','side_effect_uncertain','approval_delivery_failed','approval_expired',
    'automation_approval_failed','automation_tool_not_allowlisted','worker_busy','worker_unavailable',
    'tool_policy_decision_failed','tool_policy_decision_invalid','approval_wait_failed',
    'mcp_call_failed','unknown_tool','tool_runner_unavailable','client_cancelled','cancelled',
    'run_cancelled','approval_denied'
  );

UPDATE tool_calls
SET error_code = CASE WHEN status = 'denied' THEN 'policy_denied' ELSE 'tool_failure' END
WHERE error_code IS NOT NULL
  AND error_code NOT IN (
    'message_fetch_failed','external_agent_unavailable','model_provider_unavailable',
    'tool_discovery_failed','context_budget_exceeded','model_timeout','model_stream_failed',
    'model_output_limit_exceeded','model_unavailable','model_auth_failed','model_request_failed',
    'model_error','model_quota_exceeded','model_bad_chunk','model_stream_error','tool_call_failed',
    'tool_call_limit_exceeded','tool_result_limit_exceeded','runtime_error','retries_exhausted',
    'job_timeout','side_effect_uncertain','approval_delivery_failed','approval_expired',
    'automation_approval_failed','automation_tool_not_allowlisted','worker_busy','worker_unavailable',
    'tool_policy_decision_failed','tool_policy_decision_invalid','approval_wait_failed',
    'mcp_call_failed','unknown_tool','tool_runner_unavailable','client_cancelled','cancelled',
    'run_cancelled','approval_denied'
  );

-- marker: after-scrub

-- One run owns at most one assistant message, and one assistant message belongs
-- to at most one run. The schema enforced neither direction before, which is
-- why the Before hook has to preflight for duplicates; from here the database
-- itself refuses to create a second claimant.
CREATE UNIQUE INDEX IF NOT EXISTS idx_runs_assistant_message_unique
  ON agent_runs(assistant_message_id)
  WHERE assistant_message_id IS NOT NULL;

CREATE UNIQUE INDEX IF NOT EXISTS idx_messages_assistant_run_unique
  ON messages(run_id)
  WHERE run_id IS NOT NULL AND role = 'assistant';

-- marker: after-indexes
