-- TUR-027: attended work first.
--
-- A job is unattended when its run is: work nobody asked for when it runs,
-- today a run with an automation_runs row (repository.RunUnattended). The
-- caller that creates such a run writes the flag at insert, in the
-- transaction that writes the automation_runs row, and nothing changes it
-- afterwards. The claim reads it to take attended work first.
ALTER TABLE jobs ADD COLUMN unattended INTEGER NOT NULL DEFAULT 0 CHECK (unattended IN (0, 1));

-- Every job an automation already queued, in any state, so the flag agrees
-- with the definition for history too.
UPDATE jobs SET unattended = 1
WHERE run_id IN (SELECT run_id FROM automation_runs);

-- The claim's first probe walks attended pending jobs alone, in claim order,
-- so a backlog of unattended work costs it nothing. The key is the claim
-- index's own, idx_jobs_capability_claim's: status is constant here, but
-- without it the planner, which keeps no statistics, prefers the claim
-- index for its second equality column and walks the backlog. With it the
-- two weigh alike and the planner takes this smaller one. The second probe,
-- the claim as it was, keeps idx_jobs_capability_claim.
CREATE INDEX idx_jobs_attended_claim
  ON jobs(agent_id, status, created_at_ns, id)
  WHERE status = 'pending' AND unattended = 0;
