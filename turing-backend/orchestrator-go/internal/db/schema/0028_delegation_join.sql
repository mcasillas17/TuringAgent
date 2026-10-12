-- Join: when every task a Turing run delegated has finished, the results go
-- back into its conversation and a continuation run answers from them.
--
-- A continuation names the run it continues. The unique index makes a second
-- continuation of the same run impossible, whatever the code that inserts it
-- believes; the foreign key removes a continuation with that run.
ALTER TABLE agent_runs ADD COLUMN continues_run_id TEXT
  REFERENCES agent_runs(id) ON DELETE CASCADE;
CREATE UNIQUE INDEX idx_agent_runs_continues_run
  ON agent_runs(continues_run_id) WHERE continues_run_id IS NOT NULL;
