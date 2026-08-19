-- Skill files own metadata and instructions. SQLite retains only the user's
-- enablement and per-capability consent decisions.
CREATE TABLE skill_settings (
  skill_id TEXT PRIMARY KEY,
  enabled INTEGER NOT NULL DEFAULT 0 CHECK (enabled IN (0, 1))
);

CREATE TABLE skill_capability_grants (
  skill_id TEXT NOT NULL,
  capability TEXT NOT NULL,
  -- Consent is scoped to the declaration revision the user approved. This is
  -- grant provenance, not a cache of file-owned display metadata.
  grant_scope TEXT NOT NULL,
  granted_at TEXT NOT NULL,
  PRIMARY KEY (skill_id, capability)
);

-- Filesystem and SQLite cannot commit atomically. Upgraded databases retain a
-- migration-only recovery copy so replacing SKILLS_ROOT in the final export /
-- commit window cannot destroy the last copy of a legacy skill. Every startup
-- re-exports nonempty recovery rows, but application code never deletes them:
-- cleanup is an offline operator action after the files are verified. Runtime
-- skill code never reads this table. Fresh installs drop it empty after commit.
CREATE TABLE legacy_skill_export_recovery (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  instructions TEXT NOT NULL
);

INSERT INTO legacy_skill_export_recovery (id, name, instructions)
SELECT id, name, instructions FROM skills;

DROP TABLE session_skills;
DROP TABLE skills;
