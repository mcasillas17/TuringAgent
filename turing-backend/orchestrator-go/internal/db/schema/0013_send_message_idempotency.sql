CREATE TABLE IF NOT EXISTS send_message_idempotency (
  idempotency_key TEXT PRIMARY KEY,
  session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
  request_fingerprint TEXT NOT NULL,
  user_message_id TEXT NOT NULL REFERENCES messages(id) ON DELETE CASCADE,
  assistant_message_id TEXT NOT NULL REFERENCES messages(id) ON DELETE CASCADE,
  run_id TEXT NOT NULL REFERENCES agent_runs(id) ON DELETE CASCADE,
  job_id TEXT NOT NULL REFERENCES jobs(id) ON DELETE CASCADE,
  trace_id TEXT NOT NULL,
  queued_event_sequence INTEGER NOT NULL,
  created_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_send_message_idempotency_session
  ON send_message_idempotency(session_id);
