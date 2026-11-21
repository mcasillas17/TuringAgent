-- TUR-028a: the owner's quiet switch for unattended work.
--
-- The switch is a settings row (unattended_quiet) written compare-and-set: a
-- write names the revision it read, and a stale one is refused, so a write
-- from an old view can never turn quiet off. The revision lives beside the
-- value. Only that key uses it until OPS-002 gives settings groups their own
-- revisions; every other row keeps 0.
ALTER TABLE settings ADD COLUMN revision INTEGER NOT NULL DEFAULT 0 CHECK (revision >= 0);
