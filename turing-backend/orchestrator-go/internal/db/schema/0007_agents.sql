-- External agents: assistants that do not run on this machine, which a
-- conversation can be deliberately routed to instead of the local assistant.
--
-- DELIBERATELY ABSENT: any column holding an API key. These are third-party
-- credentials, and this file is the point where storing one would be easy and
-- wrong. `credential_ref` is a NAME; the key it names is read from the
-- backend's environment (TURING_AGENT_API_KEYS in turing-backend/.env, which
-- init.sh creates chmod 600 and .gitignore excludes). The consequence is that
-- a database copied off this machine cannot be used to spend the user's money,
-- and a client that can read every row still cannot read a secret.
CREATE TABLE IF NOT EXISTS external_agents (
  id TEXT PRIMARY KEY,
  display_name TEXT NOT NULL,
  provider TEXT NOT NULL,
  base_url TEXT NOT NULL,
  model TEXT NOT NULL,
  credential_ref TEXT NOT NULL,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

-- The display name is how a person picks a destination for a conversation and
-- how the transcript attributes a reply, so two agents sharing one would make
-- both ambiguous at exactly the moment it matters. Compared case-insensitively:
-- "Claude" and "claude" are the same name to a person.
CREATE UNIQUE INDEX IF NOT EXISTS external_agents_name_unique
  ON external_agents (display_name COLLATE NOCASE);

-- A conversation has at most one destination, so this is keyed by session
-- rather than being a join table: routing somewhere new replaces where it went
-- before, it does not add a second recipient.
--
-- Deleting an agent drops the routing rows with it, which returns those
-- conversations to the local assistant. That is the safe direction to fail in:
-- the fallback keeps messages on this machine rather than sending them to an
-- endpoint whose configuration no longer exists.
CREATE TABLE IF NOT EXISTS session_external_agent (
  session_id TEXT PRIMARY KEY REFERENCES sessions(id) ON DELETE CASCADE,
  agent_id TEXT NOT NULL REFERENCES external_agents(id) ON DELETE CASCADE,
  routed_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS session_external_agent_by_agent
  ON session_external_agent (agent_id);
