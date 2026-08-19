-- Before this migration, created_at could hold rows written by the old
-- now() (time.Now().UTC().Format(time.RFC3339Nano), replaced by
-- repository.FormatTimestamp's fixed 9-digit fraction in commit ad935cb,
-- "fix: close lifecycle recovery gaps"). RFC3339Nano trims a fraction that is
-- exactly zero down to nothing at all — no digits, no '.' — while every other
-- row keeps its '.'. Because '.' (0x2E) sorts below every digit and below 'Z'
-- (0x5A), a legacy row landed exactly on a whole second serializes as
-- "...:05Z" and sorts *after* "...:05.000000001Z" in a plain text ORDER BY,
-- even though 05.000000001 is one nanosecond *later*. Audit reads order and
-- filter on raw created_at text (see repository.ListAuditRecords), so this
-- reversal is real, not theoretical.
--
-- Rewrite every row into the canonical fixed-width form before the index
-- below is built, so the index — and every comparison against it — sees only
-- one format. The rewrite is idempotent: substr(1, 19) is the fixed
-- "YYYY-MM-DDTHH:MM:SS" prefix RFC3339(Nano) always produces, and padding
-- whatever fraction follows (or none) out to 9 digits with trailing zeros
-- reconstructs repository.FormatTimestamp's exact output — including for
-- rows that are already in that form.
UPDATE audit_logs
SET created_at = substr(created_at, 1, 19) || '.' || substr(
  (CASE
    WHEN instr(created_at, '.') = 0 THEN ''
    ELSE substr(created_at, instr(created_at, '.') + 1, length(created_at) - instr(created_at, '.') - 1)
  END) || '000000000',
  1, 9
) || 'Z';

-- Audit reads order by (created_at, rowid): 0009's two indexes are
-- (action, created_at) and (correlation_id, created_at), neither of which
-- helps a scan that has no action/correlation filter at all. Without this,
-- the unfiltered page a user opens first — "show me everything recent" — is
-- a full table scan.
--
-- rowid is not listed here because SQLite already keeps every index sorted by
-- rowid as an implicit tie-breaker for equal key values, which is exactly the
-- ordering the keyset cursor relies on for rows sharing a created_at.
--
-- The UPDATE above runs before this CREATE INDEX so the index is built once,
-- directly on the normalized values, rather than needing a REINDEX after.
CREATE INDEX IF NOT EXISTS idx_audit_created_at ON audit_logs(created_at);
