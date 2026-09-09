CREATE TABLE run_cancellation_receipts (
    idempotency_key TEXT PRIMARY KEY CHECK(length(idempotency_key) BETWEEN 1 AND 128),
    session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    run_id TEXT NOT NULL REFERENCES agent_runs(id) ON DELETE CASCADE,
    accepted INTEGER NOT NULL CHECK(accepted IN (0, 1)),
    run_state_json TEXT NOT NULL
);

CREATE INDEX idx_run_cancellation_receipts_session ON run_cancellation_receipts(session_id);
CREATE INDEX idx_run_cancellation_receipts_run ON run_cancellation_receipts(run_id);
