package db

import (
	"context"
	"database/sql"
	"strings"
	"testing"
)

// The pseudo-server carve-out is a whitelist recreated in full by every
// migration that touches it. Adding "memory" must not silently drop "skills"
// or "integrations", and must not turn the trigger into a pass-through that
// accepts any unregistered name.
func TestMemoryVaultMigrationReservesMemoryPseudoServerWithoutLosingCarveOuts(t *testing.T) {
	ctx := context.Background()
	database := openMemoryVaultDB(t, ctx)

	for _, pseudoServer := range []string{"skills", "integrations", "memory"} {
		if _, err := database.ExecContext(ctx, `
			INSERT INTO tools (id, server_name, tool_name, policy, schema_json, enabled, discovered_at)
			VALUES (?, ?, ?, 'safe', '{}', 1, '2026-08-24T00:00:00Z')`,
			"tool_"+pseudoServer, pseudoServer, pseudoServer+".probe",
		); err != nil {
			t.Fatalf("pseudo-server %q was rejected on insert: %v", pseudoServer, err)
		}
	}

	if _, err := database.ExecContext(ctx, `
		INSERT INTO tools (id, server_name, tool_name, policy, schema_json, enabled, discovered_at)
		VALUES ('tool_unregistered', 'not-a-server', 'nope.probe', 'safe', '{}', 1, '2026-08-24T00:00:00Z')`,
	); err == nil {
		t.Fatal("an unregistered server name was accepted on insert")
	}

	if _, err := database.ExecContext(ctx, `
		UPDATE tools SET server_name = 'memory' WHERE id = 'tool_skills'`,
	); err != nil {
		t.Fatalf("update to the memory pseudo-server was rejected: %v", err)
	}
	if _, err := database.ExecContext(ctx, `
		UPDATE tools SET server_name = 'not-a-server' WHERE id = 'tool_integrations'`,
	); err == nil {
		t.Fatal("an unregistered server name was accepted on update")
	}
}

func TestMemoryVaultMigrationPreservesRunEgressDecisions(t *testing.T) {
	ctx := context.Background()
	database := databaseBeforeMigration(t, ctx, "0019_memory_vault.sql")
	seedEgressDecisionRun(t, ctx, database)

	if _, err := database.ExecContext(ctx, `
		INSERT INTO run_egress_decisions (
			decision_id, decision_version, run_id, challenge_nonce,
			challenge_fingerprint, request_digest, provider, model_name,
			external_agent_id, external_credential_ref_hash, endpoint, endpoint_host,
			data_categories_json, selected_tools_json, skill_snapshot_fingerprint,
			recall_applicable, memory_profile_applicable, consent_granted_at,
			remote_mcp_servers_json, integration_endpoints_json
		) VALUES (
			'decision_pre_memory', 2, 'run_pre_memory', 'nonce_pre_memory',
			'fingerprint_pre_memory', 'digest_pre_memory', 'openai_compatible', 'gpt-4o-mini',
			'agent_pre_memory', 'credhash_pre_memory', 'https://api.example.test/v1', 'api.example.test',
			'["EGRESS_DATA_CATEGORY_CURRENT_MESSAGE"]', '["files/files.read"]', 'skillprint_pre_memory',
			1, 0, '2026-08-24T00:00:00Z',
			'[]', '[]'
		)`); err != nil {
		t.Fatal(err)
	}

	if err := ApplyMigrations(ctx, database); err != nil {
		t.Fatal(err)
	}

	var (
		version                                            int
		nonce, fingerprint, digest, provider, model        string
		agentID                                            sql.NullString
		credentialHash, endpoint, endpointHost, categories string
		selectedTools, skillPrint                          string
		recall, memoryProfile                              int
		consentGrantedAt, remoteMCP, integrations          string
		memoryPrint                                        string
	)
	if err := database.QueryRowContext(ctx, `
		SELECT decision_version, challenge_nonce, challenge_fingerprint, request_digest,
			provider, model_name, external_agent_id, external_credential_ref_hash,
			endpoint, endpoint_host, data_categories_json, selected_tools_json,
			skill_snapshot_fingerprint, recall_applicable, memory_profile_applicable,
			consent_granted_at, remote_mcp_servers_json, integration_endpoints_json,
			memory_snapshot_fingerprint
		FROM run_egress_decisions WHERE decision_id = 'decision_pre_memory'`,
	).Scan(
		&version, &nonce, &fingerprint, &digest, &provider, &model, &agentID,
		&credentialHash, &endpoint, &endpointHost, &categories, &selectedTools,
		&skillPrint, &recall, &memoryProfile, &consentGrantedAt, &remoteMCP,
		&integrations, &memoryPrint,
	); err != nil {
		t.Fatal(err)
	}
	for _, check := range []struct{ name, got, want string }{
		{"challenge_nonce", nonce, "nonce_pre_memory"},
		{"challenge_fingerprint", fingerprint, "fingerprint_pre_memory"},
		{"request_digest", digest, "digest_pre_memory"},
		{"provider", provider, "openai_compatible"},
		{"model_name", model, "gpt-4o-mini"},
		{"external_agent_id", agentID.String, "agent_pre_memory"},
		{"external_credential_ref_hash", credentialHash, "credhash_pre_memory"},
		{"endpoint", endpoint, "https://api.example.test/v1"},
		{"endpoint_host", endpointHost, "api.example.test"},
		{"data_categories_json", categories, `["EGRESS_DATA_CATEGORY_CURRENT_MESSAGE"]`},
		{"selected_tools_json", selectedTools, `["files/files.read"]`},
		{"skill_snapshot_fingerprint", skillPrint, "skillprint_pre_memory"},
		{"consent_granted_at", consentGrantedAt, "2026-08-24T00:00:00Z"},
		{"remote_mcp_servers_json", remoteMCP, "[]"},
		{"integration_endpoints_json", integrations, "[]"},
		// A decision frozen before memory existed carries no memory snapshot,
		// and must never be backfilled with one it never consented to.
		{"memory_snapshot_fingerprint", memoryPrint, ""},
	} {
		if check.got != check.want {
			t.Fatalf("%s = %q, want %q", check.name, check.got, check.want)
		}
	}
	if version != 2 || recall != 1 || memoryProfile != 0 {
		t.Fatalf("decision_version/recall/memory = %d/%d/%d, want 2/1/0", version, recall, memoryProfile)
	}

	var indexExists int
	if err := database.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM sqlite_master
		WHERE type = 'index' AND name = 'idx_run_egress_decisions_provider_created'
		  AND tbl_name = 'run_egress_decisions'`).Scan(&indexExists); err != nil {
		t.Fatal(err)
	}
	if indexExists != 1 {
		t.Fatal("idx_run_egress_decisions_provider_created is missing after the rebuild")
	}

	// The rename/copy must not have dropped the run cascade.
	if _, err := database.ExecContext(ctx, `DELETE FROM sessions WHERE id = 'session_pre_memory'`); err != nil {
		t.Fatal(err)
	}
	var remaining int
	if err := database.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM run_egress_decisions WHERE decision_id = 'decision_pre_memory'`,
	).Scan(&remaining); err != nil {
		t.Fatal(err)
	}
	if remaining != 0 {
		t.Fatal("run_egress_decisions survived its owning run's deletion")
	}
}

func TestMemoryVaultMigrationRejectsUnversionedEgressDecisionColumnLoss(t *testing.T) {
	ctx := context.Background()
	database := openMemoryVaultDB(t, ctx)
	columns := tableColumnSet(t, ctx, database, "run_egress_decisions")
	for _, column := range []string{
		"decision_id", "decision_version", "run_id", "challenge_nonce",
		"challenge_fingerprint", "request_digest", "provider", "model_name",
		"external_agent_id", "external_credential_ref_hash", "endpoint",
		"endpoint_host", "data_categories_json", "selected_tools_json",
		"skill_snapshot_fingerprint", "recall_applicable",
		"memory_profile_applicable", "consent_granted_at",
		"remote_mcp_servers_json", "integration_endpoints_json",
		"memory_snapshot_fingerprint",
	} {
		if _, exists := columns[column]; !exists {
			t.Fatalf("run_egress_decisions lost column %q in the 0019 rebuild", column)
		}
	}
	var leftover int
	if err := database.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM sqlite_master
		WHERE type = 'table' AND name LIKE 'run_egress_decisions_%'`).Scan(&leftover); err != nil {
		t.Fatal(err)
	}
	if leftover != 0 {
		t.Fatal("the 0019 rebuild left its temporary run_egress_decisions table behind")
	}
}

// Without memory_notes_fts_au an edited note keeps matching its old text and
// never matches its new text, so search silently serves withdrawn content.
func TestMemoryNotesFTSStaysInSyncOnUpdate(t *testing.T) {
	ctx := context.Background()
	database := openMemoryVaultDB(t, ctx)

	if _, err := database.ExecContext(ctx, `
		INSERT INTO memory_notes (id, path, content, content_hash, status, created_at, updated_at)
		VALUES ('note_edit', 'notes/edit.md', 'aurora borealis', 'hash_before', 'managed',
			'2026-08-24T00:00:00Z', '2026-08-24T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	assertMemoryNoteMatches(t, ctx, database, "aurora", 1)

	if _, err := database.ExecContext(ctx, `
		UPDATE memory_notes SET content = 'zephyr crossing', content_hash = 'hash_after'
		WHERE id = 'note_edit'`); err != nil {
		t.Fatal(err)
	}
	assertMemoryNoteMatches(t, ctx, database, "aurora", 0)
	assertMemoryNoteMatches(t, ctx, database, "zephyr", 1)

	if _, err := database.ExecContext(ctx,
		`DELETE FROM memory_notes WHERE id = 'note_edit'`); err != nil {
		t.Fatal(err)
	}
	assertMemoryNoteMatches(t, ctx, database, "zephyr", 0)
}

func TestMemoryNotesKeepImplicitRowidForExternalContentFTS(t *testing.T) {
	ctx := context.Background()
	database := openMemoryVaultDB(t, ctx)
	var createSQL string
	if err := database.QueryRowContext(ctx,
		`SELECT sql FROM sqlite_master WHERE type = 'table' AND name = 'memory_notes'`,
	).Scan(&createSQL); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(strings.ToUpper(strings.Join(strings.Fields(createSQL), " ")), "WITHOUT ROWID") {
		t.Fatal("memory_notes is WITHOUT ROWID; external-content FTS5 needs a real rowid")
	}
	if _, err := database.ExecContext(ctx, `SELECT rowid FROM memory_notes LIMIT 1`); err != nil {
		t.Fatalf("memory_notes has no usable rowid: %v", err)
	}
}

func TestMemoryCandidatesAreSessionOwnedAndUnindexed(t *testing.T) {
	ctx := context.Background()
	database := openMemoryVaultDB(t, ctx)
	insertTestSession(t, ctx, database, "session_candidates")

	if _, err := database.ExecContext(ctx, `
		INSERT INTO memory_candidates (
			id, source_session_id, kind, inbox_path, content_hash, body,
			evidence_refs_json, state, created_at, updated_at
		) VALUES (
			'cand_1', 'session_candidates', 'belief', 'inbox/cand_1.md', 'hash_1',
			'the user prefers metric units', '[]', 'pending',
			'2026-08-24T00:00:00Z', '2026-08-24T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	if _, err := database.ExecContext(ctx, `
		INSERT INTO memory_candidates (
			id, source_session_id, kind, inbox_path, content_hash, body,
			evidence_refs_json, state, created_at, updated_at
		) VALUES (
			'cand_bad_kind', 'session_candidates', 'rumour', 'inbox/bad.md', 'hash_2',
			'nope', '[]', 'pending', '2026-08-24T00:00:00Z', '2026-08-24T00:00:00Z')`,
	); err == nil {
		t.Fatal("an unknown candidate kind was accepted")
	}
	if _, err := database.ExecContext(ctx, `
		INSERT INTO memory_candidates (
			id, source_session_id, kind, inbox_path, content_hash, body,
			evidence_refs_json, state, created_at, updated_at
		) VALUES (
			'cand_bad_state', 'session_candidates', 'belief', 'inbox/bad2.md', 'hash_3',
			'nope', '[]', 'settled', '2026-08-24T00:00:00Z', '2026-08-24T00:00:00Z')`,
	); err == nil {
		t.Fatal("an unknown candidate lifecycle state was accepted")
	}
	if _, err := database.ExecContext(ctx, `
		INSERT INTO memory_candidates (
			id, source_session_id, kind, inbox_path, content_hash, body,
			evidence_refs_json, state, created_at, updated_at
		) VALUES (
			'cand_null_session', NULL, 'belief', 'inbox/bad3.md', 'hash_4',
			'nope', '[]', 'pending', '2026-08-24T00:00:00Z', '2026-08-24T00:00:00Z')`,
	); err == nil {
		t.Fatal("a candidate with no owning session was accepted")
	}

	var indexed int
	if err := database.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM sqlite_master
		WHERE sql LIKE '%content=''memory_candidates''%'`).Scan(&indexed); err != nil {
		t.Fatal(err)
	}
	if indexed != 0 {
		t.Fatal("memory_candidates is projected into an FTS index; candidate text must not be searchable")
	}

	if _, err := database.ExecContext(ctx,
		`DELETE FROM sessions WHERE id = 'session_candidates'`); err != nil {
		t.Fatal(err)
	}
	var remaining int
	if err := database.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM memory_candidates`).Scan(&remaining); err != nil {
		t.Fatal(err)
	}
	if remaining != 0 {
		t.Fatal("memory_candidates survived their source session's deletion")
	}
}

func TestMemoryEvidenceCascadesFromBothOwners(t *testing.T) {
	ctx := context.Background()
	database := openMemoryVaultDB(t, ctx)
	insertTestSession(t, ctx, database, "session_evidence_a")
	insertTestSession(t, ctx, database, "session_evidence_b")
	insertMemoryNote(t, ctx, database, "note_a", "notes/a.md", "alpha")
	insertMemoryNote(t, ctx, database, "note_b", "notes/b.md", "beta")

	for _, evidence := range []struct{ id, note, session string }{
		{"ev_a", "note_a", "session_evidence_a"},
		{"ev_b", "note_b", "session_evidence_b"},
	} {
		if _, err := database.ExecContext(ctx, `
			INSERT INTO memory_evidence (id, note_id, session_id, excerpt_hash, created_at)
			VALUES (?, ?, ?, 'excerpt_hash', '2026-08-24T00:00:00Z')`,
			evidence.id, evidence.note, evidence.session,
		); err != nil {
			t.Fatal(err)
		}
	}
	for _, nullField := range []string{
		`('ev_null_note', NULL, 'session_evidence_a', 'h', '2026-08-24T00:00:00Z')`,
		`('ev_null_session', 'note_a', NULL, 'h', '2026-08-24T00:00:00Z')`,
	} {
		if _, err := database.ExecContext(ctx, `
			INSERT INTO memory_evidence (id, note_id, session_id, excerpt_hash, created_at)
			VALUES `+nullField); err == nil {
			t.Fatalf("memory_evidence accepted a null owner: %s", nullField)
		}
	}

	if _, err := database.ExecContext(ctx,
		`DELETE FROM sessions WHERE id = 'session_evidence_a'`); err != nil {
		t.Fatal(err)
	}
	if _, err := database.ExecContext(ctx,
		`DELETE FROM memory_notes WHERE id = 'note_b'`); err != nil {
		t.Fatal(err)
	}
	var remaining int
	if err := database.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM memory_evidence`).Scan(&remaining); err != nil {
		t.Fatal(err)
	}
	if remaining != 0 {
		t.Fatalf("memory_evidence rows = %d, want both cascades to have fired", remaining)
	}

	// note_a was promoted independent of session_evidence_a: deleting the
	// session that supplied its evidence must cascade the evidence row, not
	// the note itself. A note is only removed by deleting memory_notes
	// directly (as note_b was above).
	var noteCount int
	if err := database.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM memory_notes WHERE id = 'note_a'`).Scan(&noteCount); err != nil {
		t.Fatal(err)
	}
	if noteCount != 1 {
		t.Fatalf("memory_notes note_a count = %d, want the promoted note to survive its evidence session's deletion", noteCount)
	}
}

func TestVaultArtifactsAreSessionOwnedAndDistinctFromSandboxArtifacts(t *testing.T) {
	ctx := context.Background()
	database := openMemoryVaultDB(t, ctx)
	insertTestSession(t, ctx, database, "session_vault")

	if _, err := database.ExecContext(ctx, `
		INSERT INTO vault_artifacts (
			id, session_id, vault_path, physical_path, state, created_at
		) VALUES (
			'vault_1', 'session_vault', 'inbox/cand_1.md', '/skills/../vault/inbox/cand_1.md',
			'writing', '2026-08-24T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	if _, err := database.ExecContext(ctx, `
		UPDATE vault_artifacts SET state = 'shredded' WHERE id = 'vault_1'`,
	); err == nil {
		t.Fatal("an unknown vault artifact state was accepted")
	}

	// One physical file is one tracked artifact per session; a second row would
	// let a cleanup delete a path another row still believes it owns.
	if _, err := database.ExecContext(ctx, `
		INSERT INTO vault_artifacts (
			id, session_id, vault_path, physical_path, state, created_at
		) VALUES (
			'vault_dupe', 'session_vault', 'inbox/other.md', '/skills/../vault/inbox/cand_1.md',
			'writing', '2026-08-24T00:00:00Z')`,
	); err == nil {
		t.Fatal("a second vault artifact for the same session and physical path was accepted")
	}

	// vault_artifacts is its own table, never a discriminator on sandbox_artifacts.
	sandboxColumns := tableColumnSet(t, ctx, database, "sandbox_artifacts")
	if _, exists := sandboxColumns["kind"]; exists {
		t.Fatal("sandbox_artifacts grew a kind discriminator instead of a separate vault table")
	}

	if _, err := database.ExecContext(ctx,
		`DELETE FROM sessions WHERE id = 'session_vault'`); err != nil {
		t.Fatal(err)
	}
	var remaining int
	if err := database.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM vault_artifacts`).Scan(&remaining); err != nil {
		t.Fatal(err)
	}
	if remaining != 0 {
		t.Fatal("vault_artifacts survived their owning session's deletion")
	}
}

func openMemoryVaultDB(t *testing.T, ctx context.Context) *DB {
	t.Helper()
	database, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if err := ApplyMigrations(ctx, database); err != nil {
		t.Fatal(err)
	}
	return database
}

func insertMemoryNote(t *testing.T, ctx context.Context, database *DB, id, path, content string) {
	t.Helper()
	if _, err := database.ExecContext(ctx, `
		INSERT INTO memory_notes (id, path, content, content_hash, status, created_at, updated_at)
		VALUES (?, ?, ?, 'hash_' || ?, 'managed', '2026-08-24T00:00:00Z', '2026-08-24T00:00:00Z')`,
		id, path, content, id,
	); err != nil {
		t.Fatal(err)
	}
}

func assertMemoryNoteMatches(t *testing.T, ctx context.Context, database *DB, query string, want int) {
	t.Helper()
	var got int
	if err := database.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM memory_notes_fts WHERE memory_notes_fts MATCH ?`, query,
	).Scan(&got); err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("memory_notes_fts MATCH %q = %d, want %d", query, got, want)
	}
}

func seedEgressDecisionRun(t *testing.T, ctx context.Context, database *DB) {
	t.Helper()
	if _, err := database.ExecContext(ctx, `
		INSERT INTO sessions (id, created_at, updated_at)
		VALUES ('session_pre_memory', '2026-08-24T00:00:00.000000000Z', '2026-08-24T00:00:00.000000000Z')`,
	); err != nil {
		t.Fatal(err)
	}
	if _, err := database.ExecContext(ctx, `
		INSERT INTO messages (id, session_id, role, content, content_type, sequence, created_at)
		VALUES ('msg_pre_memory', 'session_pre_memory', 'user', 'remote please', 'text', 1,
			'2026-08-24T00:00:00.000000000Z')`,
	); err != nil {
		t.Fatal(err)
	}
	if _, err := database.ExecContext(ctx, `
		INSERT INTO agent_runs (
			id, session_id, user_message_id, agent_id, trace_id, status,
			model_provider, model_name, created_at, state_version, state_updated_at,
			outcome_reason, assistant_content_sha256
		) VALUES (
			'run_pre_memory', 'session_pre_memory', 'msg_pre_memory', 'general_assistant',
			'trace_pre_memory', 'queued', 'openai_compatible', 'gpt-4o-mini',
			'2026-08-24T00:00:00.000000000Z', 1, '2026-08-24T00:00:00.000000000Z',
			'none', '`+strings.Repeat("0", 64)+`')`,
	); err != nil {
		t.Fatal(err)
	}
}

func tableColumnSet(t *testing.T, ctx context.Context, database *DB, table string) map[string]struct{} {
	t.Helper()
	rows, err := database.QueryContext(ctx, `SELECT name FROM pragma_table_info(?)`, table)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	columns := map[string]struct{}{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatal(err)
		}
		columns[name] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return columns
}

// The lifecycle constraints are the only thing standing between a candidate
// and an incoherent record: a decided candidate with no decision time, an
// undecided one that claims a decision, or a rejected one that nonetheless
// points at a note it supposedly produced. Each is rejected on its own.
func TestMemoryCandidateLifecycleConstraintsRejectIncoherentRows(t *testing.T) {
	ctx := context.Background()
	database := openMemoryVaultDB(t, ctx)
	insertTestSession(t, ctx, database, "session_lifecycle")
	insertMemoryNote(t, ctx, database, "note_lifecycle", "memory/notes/lifecycle.md", "grounded")

	insert := func(id, inboxPath, body, evidence, state, decidedAt, promotedNoteID string) error {
		var decided any
		if decidedAt != "" {
			decided = decidedAt
		}
		var promoted any
		if promotedNoteID != "" {
			promoted = promotedNoteID
		}
		_, err := database.ExecContext(ctx, `
			INSERT INTO memory_candidates (
				id, source_session_id, kind, inbox_path, content_hash, body,
				evidence_refs_json, state, decided_at, promoted_note_id,
				created_at, updated_at
			) VALUES (?, 'session_lifecycle', 'belief', ?, 'hash', ?, ?, ?, ?, ?,
				'2026-08-24T00:00:00Z', '2026-08-24T00:00:00Z')`,
			id, inboxPath, body, evidence, state, decided, promoted)
		return err
	}

	for _, test := range []struct {
		name           string
		id             string
		inboxPath      string
		body           string
		evidence       string
		state          string
		decidedAt      string
		promotedNoteID string
	}{
		{"empty body", "cand_empty", "inbox/empty.md", "", "[]", "pending", "", ""},
		{"oversized body", "cand_big", "inbox/big.md", strings.Repeat("a", 4097), "[]", "pending", "", ""},
		{"evidence refs are not JSON", "cand_bad_json", "inbox/badjson.md", "b", "not json", "pending", "", ""},
		{"evidence refs are a JSON object", "cand_json_object", "inbox/object.md", "b", `{"a":1}`, "pending", "", ""},
		{"decided candidate with no decision time", "cand_undecided", "inbox/undecided.md", "b", "[]", "rejected", "", ""},
		{"pending candidate claiming a decision", "cand_early", "inbox/early.md", "b", "[]", "pending", "2026-08-24T01:00:00Z", ""},
		{"unpromoted candidate pointing at a note", "cand_dangling", "inbox/dangling.md", "b", "[]", "rejected", "2026-08-24T01:00:00Z", "note_lifecycle"},
	} {
		t.Run(test.name, func(t *testing.T) {
			if err := insert(test.id, test.inboxPath, test.body, test.evidence,
				test.state, test.decidedAt, test.promotedNoteID); err == nil {
				t.Fatalf("%s was accepted", test.name)
			}
		})
	}

	// A coherent promotion is accepted, so the constraints above reject only
	// what they are meant to.
	if err := insert("cand_ok", "inbox/ok.md", "the user prefers metric units", "[]",
		"promoted", "2026-08-24T01:00:00Z", "note_lifecycle"); err != nil {
		t.Fatalf("a coherent promoted candidate was rejected: %v", err)
	}
	// One inbox file is one candidate per session; a second row claiming the
	// same file would make the vault and the index disagree about which is real.
	if err := insert("cand_dupe", "inbox/ok.md", "duplicate", "[]", "pending", "", ""); err == nil {
		t.Fatal("a second candidate for the same session and inbox path was accepted")
	}
}

func TestMemoryNotesRejectUnknownStatusAndDuplicatePaths(t *testing.T) {
	ctx := context.Background()
	database := openMemoryVaultDB(t, ctx)
	insertMemoryNote(t, ctx, database, "note_status", "memory/notes/status.md", "body")

	if _, err := database.ExecContext(ctx, `
		INSERT INTO memory_notes (id, path, content, content_hash, status, created_at, updated_at)
		VALUES ('note_bad_status', 'memory/notes/other.md', 'body', 'hash', 'archived',
			'2026-08-24T00:00:00Z', '2026-08-24T00:00:00Z')`,
	); err == nil {
		t.Fatal("an unknown note status was accepted")
	}
	// The path is the note's identity in the vault the user opens; two rows
	// claiming one file would leave the index describing a file it does not own.
	if _, err := database.ExecContext(ctx, `
		INSERT INTO memory_notes (id, path, content, content_hash, status, created_at, updated_at)
		VALUES ('note_dupe', 'memory/notes/status.md', 'body', 'hash', 'managed',
			'2026-08-24T00:00:00Z', '2026-08-24T00:00:00Z')`,
	); err == nil {
		t.Fatal("a second note for the same vault path was accepted")
	}
}

// The rebuilt table has to keep refusing the same rows the pre-0019 one did,
// or the rebuild quietly widened what a frozen decision may claim.
func TestMemoryVaultMigrationPreservesEgressDecisionCheckConstraints(t *testing.T) {
	ctx := context.Background()
	database := openMemoryVaultDB(t, ctx)
	seedEgressDecisionRun(t, ctx, database)

	insert := func(provider, endpoint, endpointHost, externalAgentID, credentialHash, remoteMCP string) error {
		var agent any
		if externalAgentID != "" {
			agent = externalAgentID
		}
		_, err := database.ExecContext(ctx, `
			INSERT INTO run_egress_decisions (
				decision_id, decision_version, run_id, challenge_nonce,
				challenge_fingerprint, request_digest, provider, model_name,
				external_agent_id, external_credential_ref_hash, endpoint, endpoint_host,
				data_categories_json, selected_tools_json, skill_snapshot_fingerprint,
				recall_applicable, memory_profile_applicable, consent_granted_at,
				remote_mcp_servers_json, integration_endpoints_json
			) VALUES ('decision_check', 1, 'run_pre_memory', 'nonce_check', 'fp', 'digest',
				?, 'model', ?, ?, ?, ?, '[]', '[]', '', 0, 0,
				'2026-08-24T00:00:00Z', ?, '[]')`,
			provider, agent, credentialHash, endpoint, endpointHost, remoteMCP)
		return err
	}

	if err := insert("openai_compatible", "", "", "", "", "[]"); err == nil {
		t.Fatal("a remote decision with no endpoint was accepted")
	}
	if err := insert("ollama", "", "", "", "", "[]"); err == nil {
		t.Fatal("a local decision naming no remote destination was accepted")
	}
	if err := insert("openai_compatible", "https://api.example/v1", "api.example", "agent_1", "", "[]"); err == nil {
		t.Fatal("an external-agent decision with no credential reference hash was accepted")
	}
	if err := insert("openai_compatible", "https://api.example/v1", "api.example", "", "", "[]"); err != nil {
		t.Fatalf("a well-formed remote decision was rejected: %v", err)
	}
}
