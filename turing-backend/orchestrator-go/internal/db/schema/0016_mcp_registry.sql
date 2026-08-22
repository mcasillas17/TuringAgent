CREATE TABLE mcp_servers (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL UNIQUE,
  transport TEXT NOT NULL CHECK (transport IN ('http')),
  url TEXT NOT NULL,
  sealed_token BLOB,
  tier TEXT NOT NULL CHECK (tier IN ('bundled','local_container','remote_url')),
  enabled INTEGER NOT NULL DEFAULT 0 CHECK (enabled IN (0,1)),
  created_at TEXT NOT NULL
);

INSERT INTO mcp_servers (id, name, transport, url, tier, enabled, created_at)
VALUES
  ('mcp_bundled_system', 'system', 'http', 'http://turing-mcp-system:7100/mcp', 'bundled', 1, datetime('now')),
  ('mcp_bundled_files', 'files', 'http', 'http://turing-mcp-files:7110/mcp', 'bundled', 1, datetime('now'));

-- A pre-registry runtime could report a custom server. Preserve its policy and
-- schema, but fail it closed until the operator imports a real endpoint.
INSERT INTO mcp_servers (id, name, transport, url, tier, enabled, created_at)
SELECT 'mcp_legacy_' || lower(hex(randomblob(16))), server_name, 'http', '', 'local_container', 0, datetime('now')
FROM tools
WHERE server_name NOT IN ('system', 'files')
  AND server_name != 'skills'
GROUP BY server_name;

ALTER TABLE tools
  ADD COLUMN mcp_server_id TEXT REFERENCES mcp_servers(id) ON DELETE CASCADE;

ALTER TABLE tools
  ADD COLUMN present INTEGER NOT NULL DEFAULT 1 CHECK (present IN (0,1));

UPDATE tools
SET mcp_server_id = (
  SELECT id FROM mcp_servers WHERE mcp_servers.name = tools.server_name
);

UPDATE tools
SET enabled = 0
WHERE mcp_server_id IN (
  SELECT id FROM mcp_servers WHERE tier != 'bundled'
);

CREATE UNIQUE INDEX tools_mcp_server_tool
  ON tools(mcp_server_id, tool_name);

CREATE TRIGGER tools_require_registered_server_insert
BEFORE INSERT ON tools
WHEN (NEW.mcp_server_id IS NULL AND NEW.server_name != 'skills') OR
  (NEW.mcp_server_id IS NOT NULL AND NOT EXISTS (
  SELECT 1
  FROM mcp_servers
  WHERE id = NEW.mcp_server_id AND name = NEW.server_name
))
BEGIN
  SELECT RAISE(ABORT, 'tool MCP server is not registered');
END;

CREATE TRIGGER tools_require_registered_server_update
BEFORE UPDATE OF mcp_server_id, server_name ON tools
WHEN (NEW.mcp_server_id IS NULL AND NEW.server_name != 'skills') OR
  (NEW.mcp_server_id IS NOT NULL AND NOT EXISTS (
  SELECT 1
  FROM mcp_servers
  WHERE id = NEW.mcp_server_id AND name = NEW.server_name
))
BEGIN
  SELECT RAISE(ABORT, 'tool MCP server is not registered');
END;

CREATE TABLE mcp_server_status (
  mcp_server_id TEXT NOT NULL PRIMARY KEY REFERENCES mcp_servers(id) ON DELETE CASCADE,
  status TEXT NOT NULL CHECK (status IN ('unknown','up','down')),
  error TEXT NOT NULL DEFAULT '',
  checked_at TEXT
);

INSERT INTO mcp_server_status (mcp_server_id, status)
SELECT id, 'unknown' FROM mcp_servers;

CREATE TABLE mcp_import_issues (
  name TEXT PRIMARY KEY,
  reason TEXT NOT NULL,
  reported_at TEXT NOT NULL
);

CREATE TABLE mcp_import_tombstones (
  name TEXT PRIMARY KEY,
  deleted_at TEXT NOT NULL
);

ALTER TABLE run_egress_decisions RENAME TO run_egress_decisions_before_mcp_registry;

CREATE TABLE run_egress_decisions (
  decision_id TEXT PRIMARY KEY,
  decision_version INTEGER NOT NULL CHECK (decision_version > 0),
  run_id TEXT NOT NULL UNIQUE REFERENCES agent_runs(id) ON DELETE CASCADE,
  challenge_nonce TEXT NOT NULL UNIQUE,
  challenge_fingerprint TEXT NOT NULL,
  request_digest TEXT NOT NULL,
  provider TEXT NOT NULL CHECK (provider IN ('ollama', 'openai_compatible')),
  model_name TEXT NOT NULL,
  external_agent_id TEXT,
  external_credential_ref_hash TEXT NOT NULL,
  endpoint TEXT NOT NULL,
  endpoint_host TEXT NOT NULL,
  data_categories_json TEXT NOT NULL,
  selected_tools_json TEXT NOT NULL,
  skill_snapshot_fingerprint TEXT NOT NULL,
  recall_applicable INTEGER NOT NULL CHECK (recall_applicable IN (0, 1)),
  memory_profile_applicable INTEGER NOT NULL CHECK (memory_profile_applicable IN (0, 1)),
  consent_granted_at TEXT NOT NULL,
  remote_mcp_servers_json TEXT NOT NULL
    CHECK (json_valid(remote_mcp_servers_json) AND json_type(remote_mcp_servers_json) = 'array'),
  CHECK (
    (
      provider = 'openai_compatible' AND
      endpoint <> '' AND endpoint_host <> ''
    ) OR (
      provider = 'ollama' AND
      endpoint = '' AND endpoint_host = '' AND
      external_agent_id IS NULL AND external_credential_ref_hash = '' AND
      json_array_length(remote_mcp_servers_json) > 0
    )
  ),
  CHECK (
    (external_agent_id IS NULL AND external_credential_ref_hash = '') OR
    (external_agent_id IS NOT NULL AND external_credential_ref_hash <> '')
  )
);

INSERT INTO run_egress_decisions (
  decision_id, decision_version, run_id, challenge_nonce,
  challenge_fingerprint, request_digest, provider, model_name,
  external_agent_id, external_credential_ref_hash, endpoint, endpoint_host,
  data_categories_json, selected_tools_json, skill_snapshot_fingerprint,
  recall_applicable, memory_profile_applicable, consent_granted_at,
  remote_mcp_servers_json
)
SELECT
  decision_id, decision_version, run_id, challenge_nonce,
  challenge_fingerprint, request_digest, provider, model_name,
  external_agent_id, external_credential_ref_hash, endpoint, endpoint_host,
  data_categories_json, selected_tools_json, skill_snapshot_fingerprint,
  recall_applicable, memory_profile_applicable, consent_granted_at,
  '[]'
FROM run_egress_decisions_before_mcp_registry;

DROP TABLE run_egress_decisions_before_mcp_registry;

CREATE INDEX idx_run_egress_decisions_provider_created
  ON run_egress_decisions(provider, consent_granted_at);
