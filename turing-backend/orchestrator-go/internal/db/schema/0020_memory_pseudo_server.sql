-- The memory pseudo-server joins skills and integrations: its tools are
-- dispatched by the orchestrator itself, so there is no mcp_servers row for a
-- tools.mcp_server_id to point at. The guard trigger has to name it, or every
-- memory tool row is refused at insert and memory can never be registered.
--
-- The triggers are recreated in full rather than patched: SQLite has no ALTER
-- TRIGGER, and leaving the old pair in place beside a new one would let a
-- third-party tool with a NULL server slip past whichever fired second.

DROP TRIGGER tools_require_registered_server_insert;
DROP TRIGGER tools_require_registered_server_update;

CREATE TRIGGER tools_require_registered_server_insert
BEFORE INSERT ON tools
WHEN (NEW.mcp_server_id IS NULL AND NEW.server_name NOT IN ('skills', 'integrations', 'memory')) OR
  (NEW.mcp_server_id IS NOT NULL AND NOT EXISTS (
  SELECT 1 FROM mcp_servers
  WHERE id = NEW.mcp_server_id AND name = NEW.server_name
))
BEGIN
  SELECT RAISE(ABORT, 'tool MCP server is not registered');
END;

CREATE TRIGGER tools_require_registered_server_update
BEFORE UPDATE OF mcp_server_id, server_name ON tools
WHEN (NEW.mcp_server_id IS NULL AND NEW.server_name NOT IN ('skills', 'integrations', 'memory')) OR
  (NEW.mcp_server_id IS NOT NULL AND NOT EXISTS (
  SELECT 1 FROM mcp_servers
  WHERE id = NEW.mcp_server_id AND name = NEW.server_name
))
BEGIN
  SELECT RAISE(ABORT, 'tool MCP server is not registered');
END;
