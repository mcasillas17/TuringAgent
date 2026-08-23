-- TUR: immutable MCP approval server-identity binding.
--
-- tool_calls.mcp_server_id records which mcp_servers row a tool call was
-- actually dispatched against, at insert time, so an approval joined to
-- that tool call (see repository.ApprovalRecord.MCPServerID/approvalByID)
-- can be bound to a specific server identity rather than only its
-- server_name. A server name can be freely reused after its original row
-- is deleted (DeleteMcpServer) and a new, unrelated server registered
-- under that same name; before this column existed, an approval created
-- and approved against the original server could still be consumed
-- against the new one, since ConsumeApprovalForThirdParty only ever
-- compared server *names*. ON DELETE SET NULL (not CASCADE): deleting a
-- server must never delete the tool_calls history of a run that already
-- called it, only sever this specific binding — see
-- ApprovalEnforcer.ConsumeApprovalForThirdParty, which fails closed
-- (refuses third-party consumption) whenever this is NULL, exactly the
-- state a deleted server's tool calls are left in.
ALTER TABLE tool_calls
  ADD COLUMN mcp_server_id TEXT REFERENCES mcp_servers(id) ON DELETE SET NULL;

-- Backfill from every existing tool_calls row's own server_name, resolved
-- against whichever mcp_servers row currently carries that name. This is
-- necessarily a one-time, current-state approximation: a row whose
-- server_name no longer matches any current mcp_servers row (the server
-- was since renamed or deleted) is left NULL, the same fail-closed state
-- a genuinely deleted server's rows are left in going forward (see
-- ON DELETE SET NULL above) — there is no way to recover a historical
-- binding a from-scratch column never recorded. "skills" and
-- "integrations" (pseudo-servers with no mcp_servers row at all — see
-- schema/0016_mcp_registry.sql/0017_integrations_consumer.sql) resolve to
-- NULL here too, which is their permanent, correct state: they are never
-- bound to a real mcp_servers row at all, at insert time or otherwise.
UPDATE tool_calls
SET mcp_server_id = (
  SELECT id FROM mcp_servers WHERE mcp_servers.name = tool_calls.server_name
);

CREATE INDEX idx_tool_calls_mcp_server_id ON tool_calls(mcp_server_id);
