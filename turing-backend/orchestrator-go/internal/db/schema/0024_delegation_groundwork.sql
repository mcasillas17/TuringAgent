-- Delegation groundwork: the parts of the team design's Migration B that the
-- guards and the team pseudo-server need before delegation itself lands.
--
-- A session is either a chat the user talks in, or a hidden delegation
-- session a specialist runs in. Every existing session is a chat. Read paths
-- that list, search, recall, title or publish sessions keep only chats, and
-- every public RPC that mutates a session refuses a delegation session.
ALTER TABLE sessions ADD COLUMN kind TEXT NOT NULL DEFAULT 'chat'
  CHECK (kind IN ('chat', 'delegation'));

-- The tools table's pseudo-server whitelist is a full replacement, as in
-- 0019_memory_vault.sql: SQLite cannot amend a trigger's WHEN clause, so both
-- triggers are dropped and recreated with every earlier carve-out restated.
-- 'team' joins them because team.delegate is served by the orchestrator
-- itself and is never backed by an mcp_servers row. A user server already
-- named team is left as it is; the orchestrator withdraws its tools by an
-- explicit gate rather than by rewriting its rows here.
DROP TRIGGER tools_require_registered_server_insert;
DROP TRIGGER tools_require_registered_server_update;

CREATE TRIGGER tools_require_registered_server_insert
BEFORE INSERT ON tools
WHEN (NEW.mcp_server_id IS NULL AND NEW.server_name NOT IN ('skills', 'integrations', 'memory', 'team')) OR
  (NEW.mcp_server_id IS NOT NULL AND NOT EXISTS (
  SELECT 1 FROM mcp_servers
  WHERE id = NEW.mcp_server_id AND name = NEW.server_name
))
BEGIN
  SELECT RAISE(ABORT, 'tool MCP server is not registered');
END;

CREATE TRIGGER tools_require_registered_server_update
BEFORE UPDATE OF mcp_server_id, server_name ON tools
WHEN (NEW.mcp_server_id IS NULL AND NEW.server_name NOT IN ('skills', 'integrations', 'memory', 'team')) OR
  (NEW.mcp_server_id IS NOT NULL AND NOT EXISTS (
  SELECT 1 FROM mcp_servers
  WHERE id = NEW.mcp_server_id AND name = NEW.server_name
))
BEGIN
  SELECT RAISE(ABORT, 'tool MCP server is not registered');
END;
