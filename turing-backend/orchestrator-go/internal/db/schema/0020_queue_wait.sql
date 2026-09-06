-- TUR-010 durable queue-wait truth.
--
-- Four appended columns rather than a table rebuild. The 0017 rebuild existed
-- because that migration had to widen a CHECK on an existing column and make
-- new columns NOT NULL with no default; nothing here does either. Every column
-- below is nullable or has a constant default, and the one CHECK belongs to a
-- column that does not exist yet, which SQLite's ALTER TABLE ADD COLUMN
-- accepts. agent_runs is the parent of every run-owned child table, so not
-- rebuilding it is worth the deliberate choice to reuse the existing public
-- outcome vocabulary (a queue bound expires; expired is what it is) instead of
-- adding outcome_reason values that would have forced one.

-- Accumulated time this run has already spent in the queue across every prior
-- queued interval, in nanoseconds. It is what a requeue, a reconnect, or a
-- restart cannot reset: a run that flaps between recovering and queued keeps
-- adding to this, so the overall queue-age bound still converges.
ALTER TABLE agent_runs ADD COLUMN queue_waited_ns INTEGER NOT NULL DEFAULT 0;

-- Start of the CURRENT queued interval, or NULL when the run is not queued.
-- Total queue age is queue_waited_ns plus (now - queued_since_ns).
ALTER TABLE agent_runs ADD COLUMN queued_since_ns INTEGER;

-- When the orchestrator first observed that no live worker satisfies this
-- queued run's frozen route. NULL means either "a compatible worker exists" or
-- "not observed yet". It survives a restart on purpose: a queue that was
-- already unserved when the process went down was still unserved while it was
-- down, and the acceptance criterion is that restart preserves the same
-- authoritative state rather than handing every waiting run a fresh clock.
ALTER TABLE agent_runs ADD COLUMN queue_unroutable_since_ns INTEGER;

-- The public half of the same fact, and the only queue column a client ever
-- sees. Crossing the queue boundary in either direction resets it to 'none', so
-- a terminal run carries a value only because the bound that ended it asserted
-- one -- which is what lets a reopened conversation say which bound that was
-- without describing a run that did start as one that never started.
ALTER TABLE agent_runs ADD COLUMN queue_wait_reason TEXT NOT NULL DEFAULT 'none'
  CHECK (queue_wait_reason IN ('none', 'no_compatible_worker', 'queue_timeout'));

-- A run that is queued right now has been queued since it was created: nothing
-- before this migration could have moved it out of and back into the queue
-- without also leaving it somewhere other than queued. The expression matches
-- the one 0012 used for jobs.created_at_ns, so the two columns are the same
-- clock read the same way.
UPDATE agent_runs
SET queued_since_ns =
  CAST(strftime('%s', substr(created_at, 1, 19) || 'Z') AS INTEGER) * 1000000000 +
  CASE
    WHEN instr(created_at, '.') = 0 THEN 0
    ELSE CAST(substr(
      substr(created_at, instr(created_at, '.') + 1, length(created_at) - instr(created_at, '.') - 1) || '000000000',
      1,
      9
    ) AS INTEGER)
  END
WHERE status = 'queued';
