DROP TRIGGER tools_require_registered_server_insert;
DROP TRIGGER tools_require_registered_server_update;

CREATE TRIGGER tools_require_registered_server_insert
BEFORE INSERT ON tools
WHEN (NEW.mcp_server_id IS NULL AND NEW.server_name NOT IN ('skills', 'integrations')) OR
  (NEW.mcp_server_id IS NOT NULL AND NOT EXISTS (
  SELECT 1 FROM mcp_servers
  WHERE id = NEW.mcp_server_id AND name = NEW.server_name
))
BEGIN
  SELECT RAISE(ABORT, 'tool MCP server is not registered');
END;

CREATE TRIGGER tools_require_registered_server_update
BEFORE UPDATE OF mcp_server_id, server_name ON tools
WHEN (NEW.mcp_server_id IS NULL AND NEW.server_name NOT IN ('skills', 'integrations')) OR
  (NEW.mcp_server_id IS NOT NULL AND NOT EXISTS (
  SELECT 1 FROM mcp_servers
  WHERE id = NEW.mcp_server_id AND name = NEW.server_name
))
BEGIN
  SELECT RAISE(ABORT, 'tool MCP server is not registered');
END;

ALTER TABLE run_egress_decisions RENAME TO run_egress_decisions_before_integrations;

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
  integration_endpoints_json TEXT NOT NULL
    CHECK (json_valid(integration_endpoints_json) AND json_type(integration_endpoints_json) = 'array'),
  CHECK (
    (
      provider = 'openai_compatible' AND
      endpoint <> '' AND endpoint_host <> ''
    ) OR (
      provider = 'ollama' AND
      endpoint = '' AND endpoint_host = '' AND
      external_agent_id IS NULL AND external_credential_ref_hash = '' AND
      (json_array_length(remote_mcp_servers_json) > 0 OR
       json_array_length(integration_endpoints_json) > 0)
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
  remote_mcp_servers_json, integration_endpoints_json
)
SELECT
  decision_id, decision_version, run_id, challenge_nonce,
  challenge_fingerprint, request_digest, provider, model_name,
  external_agent_id, external_credential_ref_hash, endpoint, endpoint_host,
  data_categories_json, selected_tools_json, skill_snapshot_fingerprint,
  recall_applicable, memory_profile_applicable, consent_granted_at,
  remote_mcp_servers_json, '[]'
FROM run_egress_decisions_before_integrations;

DROP TABLE run_egress_decisions_before_integrations;

CREATE INDEX idx_run_egress_decisions_provider_created
  ON run_egress_decisions(provider, consent_granted_at);
