-- TUR: vault-backed memory foundations (Turing's Brain, Phase 1).
--
-- Memory is a vault the user can open: notes are files, and the database holds
-- only the index, the lifecycle, and the provenance needed to withdraw content
-- when a session is deleted. Nothing here implements repository or service
-- behavior; this migration only establishes the shape those layers commit to.

-- The tools table's pseudo-server whitelist is a full replacement, not an
-- append: SQLite cannot amend a trigger's WHEN clause in place, so both
-- triggers are dropped and recreated verbatim from
-- 0017_integrations_consumer.sql with 'memory' added. Losing 'skills' or
-- 'integrations' here would orphan every already-registered pseudo-server tool
-- on the next write, so both carve-outs are restated explicitly rather than
-- assumed. 'memory' joins them because memory tools are served by the
-- orchestrator itself and are never backed by an mcp_servers row.
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

-- A vault note. The file on disk is authoritative; this row is the index that
-- makes it findable and the record of whether Turing is allowed to rewrite it.
--
-- Deliberately keeps its implicit rowid (no WITHOUT ROWID): memory_notes_fts
-- below is an external-content FTS5 index keyed by content_rowid='rowid', and
-- a WITHOUT ROWID table has no stable rowid for it to key on at all.
--
-- status is the managed/unmanaged distinction the client renders: 'managed'
-- notes were written by Turing and may be rewritten by it, 'unmanaged' notes
-- were hand-edited in the vault and are read-only to Turing, and 'withdrawn'
-- marks a note whose supporting evidence is gone.
CREATE TABLE memory_notes (
  id TEXT PRIMARY KEY,
  path TEXT NOT NULL UNIQUE,
  content TEXT NOT NULL,
  content_hash TEXT NOT NULL,
  status TEXT NOT NULL CHECK (status IN ('managed', 'unmanaged', 'withdrawn')),
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE VIRTUAL TABLE memory_notes_fts USING fts5(
  content,
  content='memory_notes',
  content_rowid='rowid'
);

CREATE TRIGGER memory_notes_fts_ai AFTER INSERT ON memory_notes BEGIN
  INSERT INTO memory_notes_fts(rowid, content) VALUES (new.rowid, new.content);
END;

CREATE TRIGGER memory_notes_fts_ad AFTER DELETE ON memory_notes BEGIN
  INSERT INTO memory_notes_fts(memory_notes_fts, rowid, content) VALUES ('delete', old.rowid, old.content);
END;

-- Without this trigger an edited note keeps matching its previous text forever:
-- external-content FTS5 stores its own copy of the tokens, so an UPDATE that is
-- not mirrored here leaves search serving text the vault no longer contains.
CREATE TRIGGER memory_notes_fts_au AFTER UPDATE ON memory_notes BEGIN
  INSERT INTO memory_notes_fts(memory_notes_fts, rowid, content) VALUES ('delete', old.rowid, old.content);
  INSERT INTO memory_notes_fts(rowid, content) VALUES (new.rowid, new.content);
END;

-- A proposal waiting in the vault inbox, owned by the session that produced it.
--
-- Candidates are unreviewed model output about the user, so they are session
-- state and nothing else: deleting the session deletes them, and they are
-- deliberately NOT projected into any FTS index. A candidate the user never
-- accepted must never turn up in a search over their memory.
CREATE TABLE memory_candidates (
  id TEXT PRIMARY KEY,
  source_session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
  kind TEXT NOT NULL CHECK (kind IN ('belief', 'profile_edit')),
  inbox_path TEXT NOT NULL,
  content_hash TEXT NOT NULL,
  -- Bounded on purpose: a candidate is a claim, not a transcript.
  body TEXT NOT NULL CHECK (length(body) > 0 AND length(body) <= 4096),
  evidence_refs_json TEXT NOT NULL
    CHECK (json_valid(evidence_refs_json) AND json_type(evidence_refs_json) = 'array'),
  state TEXT NOT NULL CHECK (state IN ('pending', 'promoted', 'rejected', 'withdrawn')),
  -- Set only once the candidate leaves 'pending'; promoted_note_id may be set
  -- only by a promotion, and is severed rather than dangling if the note it
  -- produced is later deleted from the vault.
  promoted_note_id TEXT REFERENCES memory_notes(id) ON DELETE SET NULL,
  decided_at TEXT,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  UNIQUE (source_session_id, inbox_path),
  CHECK (
    (state = 'pending' AND decided_at IS NULL) OR
    (state <> 'pending' AND decided_at IS NOT NULL)
  ),
  CHECK (state = 'promoted' OR promoted_note_id IS NULL)
);

CREATE INDEX idx_memory_candidates_state
  ON memory_candidates (state, created_at);
-- No separate index on source_session_id: UNIQUE (source_session_id, inbox_path)
-- already leaves a covering index with that column as its prefix, which is what
-- the cascade and the per-session listing use.

-- What a note is grounded in. Both owners cascade: deleting the note drops its
-- evidence, and deleting the session the evidence came from withdraws it, so a
-- deleted conversation cannot keep justifying a retained claim. Only a hash of
-- the supporting excerpt is kept — never the excerpt itself.
CREATE TABLE memory_evidence (
  id TEXT PRIMARY KEY,
  note_id TEXT NOT NULL REFERENCES memory_notes(id) ON DELETE CASCADE,
  session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
  excerpt_hash TEXT NOT NULL,
  created_at TEXT NOT NULL
);

CREATE INDEX idx_memory_evidence_note ON memory_evidence (note_id);
CREATE INDEX idx_memory_evidence_session ON memory_evidence (session_id);

-- Files a run wrote into the vault, tracked per session so they can be removed
-- with it.
--
-- Its own table rather than a kind column on sandbox_artifacts: a sandbox
-- artifact is scratch output owned by a run inside the tool sandbox, while a
-- vault artifact is user-visible content inside the vault the user opens, with
-- a different root, a different retention answer, and no run ownership at all.
-- Sharing one table would mean one lifecycle for two different promises.
CREATE TABLE vault_artifacts (
  id TEXT PRIMARY KEY,
  session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
  vault_path TEXT NOT NULL,
  physical_path TEXT NOT NULL,
  state TEXT NOT NULL CHECK (state IN ('writing', 'ready', 'delete_failed')),
  created_at TEXT NOT NULL,
  finalized_at TEXT,
  UNIQUE (session_id, physical_path)
);

CREATE INDEX idx_vault_artifacts_session_state
  ON vault_artifacts (session_id, state);

-- run_egress_decisions gains memory_snapshot_fingerprint. SQLite cannot add a
-- NOT NULL column to a table carrying table-level CHECKs without restating
-- them, so the table is rebuilt exactly as 0014/0016/0017 did, under a fresh
-- temporary name so a re-run of an older migration can never collide with it.
-- Every column, constraint, cascade and index is restated verbatim; only the
-- new column is added, and existing rows get '' because a decision frozen
-- before memory existed disclosed no memory snapshot and must not be
-- retroactively credited with one.
ALTER TABLE run_egress_decisions RENAME TO run_egress_decisions_before_memory_vault;

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
  -- Defaulted, not nullable: a decision that pins no memory records the empty
  -- string. NULL would make "no memory" indistinguishable from "not recorded",
  -- and the fail-closed reader must be able to tell those apart.
  memory_snapshot_fingerprint TEXT NOT NULL DEFAULT '',
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
  memory_snapshot_fingerprint, recall_applicable, memory_profile_applicable,
  consent_granted_at, remote_mcp_servers_json, integration_endpoints_json
)
SELECT
  decision_id, decision_version, run_id, challenge_nonce,
  challenge_fingerprint, request_digest, provider, model_name,
  external_agent_id, external_credential_ref_hash, endpoint, endpoint_host,
  data_categories_json, selected_tools_json, skill_snapshot_fingerprint,
  '', recall_applicable, memory_profile_applicable,
  consent_granted_at, remote_mcp_servers_json, integration_endpoints_json
FROM run_egress_decisions_before_memory_vault;

DROP TABLE run_egress_decisions_before_memory_vault;

CREATE INDEX idx_run_egress_decisions_provider_created
  ON run_egress_decisions(provider, consent_granted_at);
