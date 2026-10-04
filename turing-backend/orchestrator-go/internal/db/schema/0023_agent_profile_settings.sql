-- Specialist profiles live in team/<id>/AGENT.md. SQLite retains only the
-- user's enablement and the declaration revision the user granted.
CREATE TABLE agent_profile_settings (
  profile_id TEXT PRIMARY KEY,
  enabled INTEGER NOT NULL DEFAULT 0 CHECK (enabled IN (0, 1)),
  granted_revision TEXT,
  granted_at TEXT,
  updated_at TEXT NOT NULL
);
