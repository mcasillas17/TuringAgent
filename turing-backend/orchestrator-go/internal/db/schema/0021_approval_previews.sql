CREATE TABLE approval_previews (
    approval_id TEXT NOT NULL PRIMARY KEY REFERENCES approvals(id) ON DELETE CASCADE,
    preview_hash TEXT NOT NULL,
    snapshot_json TEXT NOT NULL CHECK(length(CAST(snapshot_json AS BLOB)) <= 2097152),
    deletion_generation INTEGER NOT NULL
);

-- Withdrawal removes displayable content immediately, not only when the
-- eventually completed session deletion cascades through the approval.
CREATE TRIGGER withdraw_approval_previews AFTER UPDATE OF deletion_state ON sessions
WHEN NEW.deletion_state <> 'active'
BEGIN
    DELETE FROM approval_previews WHERE approval_id IN (
        SELECT a.id FROM approvals a JOIN agent_runs r ON r.id = a.run_id WHERE r.session_id = NEW.id
    );
END;
