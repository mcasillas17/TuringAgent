-- Runs queued before explicit egress decisions existed cannot be executed
-- safely. Fail them durably rather than leaving them to strand behind the new
-- egress-aware worker capability gate.
UPDATE jobs
SET status = 'failed',
    error_code = 'egress_decision_required',
    error_message = 'remote run was queued before explicit egress consent',
    finished_at = created_at
WHERE status = 'pending'
  AND run_id IN (
    SELECT id
    FROM agent_runs
    WHERE status = 'queued' AND model_provider = 'openai_compatible'
  );

INSERT INTO events (
  id, session_id, run_id, trace_id, sequence, type, payload_json, created_at
)
SELECT
  'evt_egress_required_' || r.id,
  r.session_id,
  r.id,
  r.trace_id,
  COALESCE((
    SELECT MAX(existing.sequence)
    FROM events existing
    WHERE existing.session_id = r.session_id
  ), 0) + ROW_NUMBER() OVER (
    PARTITION BY r.session_id ORDER BY r.created_at, r.id
  ),
  'agent.run.failed',
  json_object(
    'runId', r.id,
    'code', 'egress_decision_required',
    'message', 'remote run was queued before explicit egress consent',
    'retryable', json('false')
  ),
  r.created_at
FROM agent_runs r
WHERE r.status = 'queued' AND r.model_provider = 'openai_compatible';

DELETE FROM send_message_idempotency
WHERE run_id IN (
  SELECT id
  FROM agent_runs
  WHERE status = 'queued' AND model_provider = 'openai_compatible'
);

UPDATE agent_runs
SET status = 'failed',
    error_code = 'egress_decision_required',
    error_message = 'remote run was queued before explicit egress consent',
    finished_at = created_at
WHERE status = 'queued' AND model_provider = 'openai_compatible';

CREATE TABLE run_egress_decisions (
  decision_id TEXT PRIMARY KEY,
  decision_version INTEGER NOT NULL CHECK (decision_version > 0),
  run_id TEXT NOT NULL UNIQUE REFERENCES agent_runs(id) ON DELETE CASCADE,
  challenge_nonce TEXT NOT NULL UNIQUE,
  challenge_fingerprint TEXT NOT NULL,
  request_digest TEXT NOT NULL,
  provider TEXT NOT NULL CHECK (provider = 'openai_compatible'),
  model_name TEXT NOT NULL,
  external_agent_id TEXT,
  external_credential_ref_hash TEXT NOT NULL,
  endpoint TEXT NOT NULL,
  endpoint_host TEXT NOT NULL,
  data_categories_json TEXT NOT NULL,
  selected_tools_json TEXT NOT NULL,
  skill_snapshot_fingerprint TEXT NOT NULL,
  recall_applicable INTEGER NOT NULL CHECK (recall_applicable IN (0, 1)),
  memory_profile_applicable INTEGER NOT NULL CHECK (memory_profile_applicable IN (0, 1)),
  consent_granted_at TEXT NOT NULL,
  CHECK (
    (external_agent_id IS NULL AND external_credential_ref_hash = '') OR
    (external_agent_id IS NOT NULL AND external_credential_ref_hash <> '')
  )
);

CREATE INDEX idx_run_egress_decisions_provider_created
  ON run_egress_decisions(provider, consent_granted_at);
