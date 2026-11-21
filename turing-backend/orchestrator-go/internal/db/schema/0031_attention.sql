-- UX-002a: the Needs-you queue (attention projection).
--
-- The occurrence key of the one failure the owner dismissed from the queue:
-- the id of the automation's latest automation.remote_egress_blocked audit
-- record newer than last_run_at when there is one, otherwise last_run_id.
-- The queue skips the automation while its latest occurrence key equals this
-- value, so the next failure, with or without a run, raises it again. NULL
-- means nothing was dismissed.
ALTER TABLE automations ADD COLUMN attention_dismissed_occurrence TEXT;

-- Tasks waiting for the owner's consent to tools that leave the machine.
CREATE INDEX idx_delegations_consent_pending
  ON delegations(created_at, id) WHERE egress_consent = 'pending';

-- One automation's latest remote-egress block is one seek here, whatever
-- another automation's history holds. idx_audit_action(action, created_at)
-- walked every automation's blocks newer than this one's last run.
CREATE INDEX idx_audit_remote_egress_blocked
  ON audit_logs(actor_id, created_at) WHERE action = 'automation.remote_egress_blocked';
