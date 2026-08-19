package db

import (
	"context"
	"database/sql"
	"fmt"
	"reflect"
	"testing"
)

func TestMessagesFTSStaysInSync(t *testing.T) {
	ctx := context.Background()
	database, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := ApplyMigrations(ctx, database); err != nil {
		t.Fatal(err)
	}
	insertTestSession(t, ctx, database, "s1")

	if _, err := database.ExecContext(ctx, `
		INSERT INTO messages (id, session_id, role, content, content_type, sequence, created_at)
		VALUES ('m-insert', 's1', 'assistant', 'an inserted aurora', 'text', 1, datetime('now'))`); err != nil {
		t.Fatal(err)
	}
	assertFTSMessageIDs(t, ctx, database, "aurora", 1, []string{"m-insert"})

	if _, err := database.ExecContext(ctx, `
		INSERT INTO messages (id, session_id, role, content, content_type, sequence, created_at)
		VALUES ('m-update', 's1', 'assistant', '', 'text', 2, datetime('now'))`); err != nil {
		t.Fatal(err)
	}
	if _, err := database.ExecContext(ctx,
		`UPDATE messages SET content = 'the mitochondria is the powerhouse' WHERE id = 'm-update'`); err != nil {
		t.Fatal(err)
	}
	assertFTSMessageIDs(t, ctx, database, "mitochondria", 1, []string{"m-update"})
	if _, err := database.ExecContext(ctx,
		`UPDATE messages SET content = 'cellular respiration produces energy' WHERE id = 'm-update'`); err != nil {
		t.Fatal(err)
	}
	assertFTSMessageIDs(t, ctx, database, "mitochondria", 0, []string{})
	assertFTSMessageIDs(t, ctx, database, "respiration", 1, []string{"m-update"})

	if _, err := database.ExecContext(ctx, `DELETE FROM messages WHERE id = 'm-update'`); err != nil {
		t.Fatal(err)
	}
	assertFTSMessageIDs(t, ctx, database, "respiration", 0, []string{})
}

func TestMessagesFTSCascadeDeletePreservesOtherSessions(t *testing.T) {
	ctx := context.Background()
	database, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := ApplyMigrations(ctx, database); err != nil {
		t.Fatal(err)
	}
	insertTestSession(t, ctx, database, "delete-me")
	insertTestSession(t, ctx, database, "keep-me")
	for _, row := range []struct{ id, sessionID string }{{"m-delete", "delete-me"}, {"m-keep", "keep-me"}} {
		if _, err := database.ExecContext(ctx, `
			INSERT INTO messages (id, session_id, role, content, content_type, sequence, created_at)
			VALUES (?, ?, 'user', 'shared cascade token', 'text', 1, datetime('now'))`, row.id, row.sessionID); err != nil {
			t.Fatal(err)
		}
	}
	assertFTSMessageIDs(t, ctx, database, "cascade", 2, []string{"m-delete", "m-keep"})
	if _, err := database.ExecContext(ctx, `DELETE FROM sessions WHERE id = 'delete-me'`); err != nil {
		t.Fatal(err)
	}
	assertFTSMessageIDs(t, ctx, database, "cascade", 1, []string{"m-keep"})
}

func TestApplyMigrationsIsIdempotentWithFTSData(t *testing.T) {
	ctx := context.Background()
	database, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := ApplyMigrations(ctx, database); err != nil {
		t.Fatal(err)
	}
	insertTestSession(t, ctx, database, "s1")
	if _, err := database.ExecContext(ctx, `
		INSERT INTO messages (id, session_id, role, content, content_type, sequence, created_at)
		VALUES ('m-idempotent', 's1', 'user', 'idempotent quasar', 'text', 1, datetime('now'))`); err != nil {
		t.Fatal(err)
	}
	if err := ApplyMigrations(ctx, database); err != nil {
		t.Fatalf("second ApplyMigrations: %v", err)
	}
	assertFTSMessageIDs(t, ctx, database, "quasar", 1, []string{"m-idempotent"})
}

func TestMessagesFTSBackfillsMessagesFromBeforeMigration(t *testing.T) {
	ctx := context.Background()
	database, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	applyMigration(t, ctx, database, "0001_initial.sql")
	applyMigration(t, ctx, database, "0002_go_runtime.sql")
	insertTestSession(t, ctx, database, "s1")
	if _, err := database.ExecContext(ctx, `
		INSERT INTO messages (id, session_id, role, content, content_type, sequence, created_at)
		VALUES ('m-before-migration', 's1', 'user', 'preexisting nebula', 'text', 1, datetime('now'))`); err != nil {
		t.Fatal(err)
	}

	if err := ApplyMigrations(ctx, database); err != nil {
		t.Fatal(err)
	}
	assertFTSMessageIDs(t, ctx, database, "nebula", 1, []string{"m-before-migration"})
}

func TestLatestSchemaVersionMatchesHighestRecordedMigration(t *testing.T) {
	ctx := context.Background()
	database, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := ApplyMigrations(ctx, database); err != nil {
		t.Fatal(err)
	}

	var want string
	if err := database.QueryRowContext(ctx, `
		SELECT CASE WHEN instr(version, '_') > 0 THEN substr(version, 1, instr(version, '_') - 1) ELSE version END
		FROM schema_migrations
		ORDER BY version DESC
		LIMIT 1`).Scan(&want); err != nil {
		t.Fatal(err)
	}
	got, err := LatestSchemaVersion()
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("latest schema version = %q, want %q", got, want)
	}
}

func TestApplyMigrationsRecordsEmbeddedMigrationsInLexicalOrder(t *testing.T) {
	ctx := context.Background()
	database, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := ApplyMigrations(ctx, database); err != nil {
		t.Fatal(err)
	}

	rows, err := database.QueryContext(ctx, `SELECT version FROM schema_migrations ORDER BY rowid`)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	var got []string
	for rows.Next() {
		var version string
		if err := rows.Scan(&version); err != nil {
			t.Fatal(err)
		}
		got = append(got, version)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	want := []string{
		"0001_initial",
		"0002_go_runtime",
		"0003_messages_fts",
		"0003_tool_call_model_identity",
		"0004_execution_exit_gate",
		"0005_timestamp_ordering",
		"0006_skills",
		"0007_agents",
		"0008_integrations",
		"0009_automations",
		"0010_session_title_origin",
		"0010_telemetry",
		"0011_approval_rationale",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("applied migrations = %v, want %v", got, want)
	}
}

// 0009 rebuilds audit_logs to widen its actor CHECK. A rebuild that dropped
// rows would erase the record of what already happened, which is the one thing
// an audit table must never do.
func TestAuditRebuildKeepsExistingRowsAndAcceptsAutomationActors(t *testing.T) {
	ctx := context.Background()
	database, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	for _, name := range []string{
		"0001_initial.sql", "0002_go_runtime.sql", "0003_messages_fts.sql",
		"0003_tool_call_model_identity.sql", "0004_execution_exit_gate.sql",
		"0005_timestamp_ordering.sql", "0006_skills.sql",
	} {
		applyMigration(t, ctx, database, name)
	}
	if _, err := database.ExecContext(ctx, `
		INSERT INTO audit_logs (id, correlation_id, actor_type, actor_id, action, target, payload_json, created_at)
		VALUES ('audit_first', 'run_1', 'client', 'actor_1', 'approval.approved', 'appr_1', '{"toolName":"files.update"}', '2026-01-01T00:00:00Z'),
		       ('audit_second', 'run_2', 'runtime', NULL, 'tool.call.before', 'call_1', '{"toolName":"files.read"}', '2026-01-02T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	var firstRowID, secondRowID int64
	if err := database.QueryRowContext(ctx, `SELECT rowid FROM audit_logs WHERE id = 'audit_first'`).Scan(&firstRowID); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRowContext(ctx, `SELECT rowid FROM audit_logs WHERE id = 'audit_second'`).Scan(&secondRowID); err != nil {
		t.Fatal(err)
	}

	if err := ApplyMigrations(ctx, database); err != nil {
		t.Fatal(err)
	}

	// Every column, not just the one the migration widened: an INSERT…SELECT
	// with two TEXT columns transposed would corrupt the whole table and still
	// leave actor_type looking right.
	var correlationID, actorType, actorID, action, target, payload, createdAt string
	var rowID int64
	if err := database.QueryRowContext(ctx, `
		SELECT rowid, correlation_id, actor_type, COALESCE(actor_id, ''), action, target, payload_json, created_at
		FROM audit_logs WHERE id = 'audit_first'`).Scan(
		&rowID, &correlationID, &actorType, &actorID, &action, &target, &payload, &createdAt); err != nil {
		t.Fatalf("the pre-existing audit row did not survive the rebuild: %v", err)
	}
	got := []string{correlationID, actorType, actorID, action, target, payload, createdAt}
	want := []string{"run_1", "client", "actor_1", "approval.approved", "appr_1", `{"toolName":"files.update"}`, "2026-01-01T00:00:00Z"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("preserved row = %q, want %q", got, want)
	}
	// rowid is what audit reads order by, so a rebuild that renumbered would
	// silently reorder history.
	if rowID != firstRowID {
		t.Fatalf("rowid changed from %d to %d", firstRowID, rowID)
	}
	var secondAfter int64
	if err := database.QueryRowContext(ctx, `SELECT rowid FROM audit_logs WHERE id = 'audit_second'`).Scan(&secondAfter); err != nil {
		t.Fatal(err)
	}
	if secondAfter != secondRowID {
		t.Fatalf("second rowid changed from %d to %d", secondRowID, secondAfter)
	}
	// The rename drops the old indexes with the old table; the migration has
	// to put them back or every audit read becomes a scan.
	for _, index := range []string{"idx_audit_action", "idx_audit_correlation"} {
		var exists int
		if err := database.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM sqlite_master WHERE type = 'index' AND name = ? AND tbl_name = 'audit_logs'`,
			index).Scan(&exists); err != nil {
			t.Fatal(err)
		}
		if exists != 1 {
			t.Fatalf("index %s is missing after the rebuild", index)
		}
	}
	if _, err := database.ExecContext(ctx, `
		INSERT INTO audit_logs (id, correlation_id, actor_type, actor_id, action, target, payload_json, created_at)
		VALUES ('audit_auto', 'run_1', 'automation', 'auto_1', 'approval.approved', 'appr_2', '{"unattended":true}', '2026-01-02T00:00:00Z')`); err != nil {
		t.Fatalf("an automation actor was rejected: %v", err)
	}
	// The constraint is widened, not removed.
	if _, err := database.ExecContext(ctx, `
		INSERT INTO audit_logs (id, actor_type, action, created_at)
		VALUES ('audit_bogus', 'whoever', 'approval.approved', '2026-01-03T00:00:00Z')`); err == nil {
		t.Fatal("an unknown actor kind was accepted")
	}
}

func TestCurrentSchemaVersionUsesLatestEmbeddedMigrationPrefix(t *testing.T) {
	got, err := CurrentSchemaVersion()
	if err != nil {
		t.Fatal(err)
	}
	if got != "0011" {
		t.Fatalf("CurrentSchemaVersion = %q, want 0011", got)
	}
}

func TestApprovalRationaleMigrationPreservesExistingApprovals(t *testing.T) {
	ctx := context.Background()
	database, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	applyMigration(t, ctx, database, "0001_initial.sql")
	if _, err := database.ExecContext(ctx, `
		INSERT INTO sessions (id, title, status, created_at, updated_at)
		VALUES ('session_before_rationale', 'Existing', 'active', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z');
		INSERT INTO messages (id, session_id, role, content, content_type, sequence, created_at)
		VALUES ('message_before_rationale', 'session_before_rationale', 'user', 'hello', 'text', 1, '2026-01-01T00:00:00Z');
		INSERT INTO agent_runs (id, session_id, user_message_id, agent_id, trace_id, status, model_provider, model_name, created_at)
		VALUES ('run_before_rationale', 'session_before_rationale', 'message_before_rationale', 'general_assistant', 'trace_before_rationale', 'waiting_approval', 'ollama', 'llama3.2', '2026-01-01T00:00:00Z');
		INSERT INTO tool_calls (id, run_id, agent_id, server_name, tool_name, args_json, args_hash, status, created_at)
		VALUES ('call_before_rationale', 'run_before_rationale', 'general_assistant', 'files', 'files.update', '{"path":"note.txt"}', 'sha256:args', 'approval_required', '2026-01-01T00:00:00Z');
		INSERT INTO approvals (id, run_id, tool_call_id, agent_id, tool_name, args_json, args_hash, status, expires_at, created_at)
		VALUES ('approval_before_rationale', 'run_before_rationale', 'call_before_rationale', 'general_assistant', 'files.update', '{"path":"note.txt"}', 'sha256:args', 'pending', '2026-01-01T00:01:00Z', '2026-01-01T00:00:00Z');
	`); err != nil {
		t.Fatal(err)
	}

	applyMigration(t, ctx, database, "0011_approval_rationale.sql")

	var status string
	var approvalComment, denialReason sql.NullString
	if err := database.QueryRowContext(ctx, `
		SELECT status, approval_comment, denial_reason
		FROM approvals
		WHERE id = 'approval_before_rationale'
	`).Scan(&status, &approvalComment, &denialReason); err != nil {
		t.Fatal(err)
	}
	if status != "pending" || approvalComment.Valid || denialReason.Valid {
		t.Fatalf(
			"upgraded approval = status %q comment %#v reason %#v, want pending and both NULL",
			status,
			approvalComment,
			denialReason,
		)
	}
}

func TestApplyMigrationsUpgradesPopulated0002DatabaseWithNullableModelToolCallID(t *testing.T) {
	ctx := context.Background()
	sqlDB, err := sql.Open("sqlite3", fmt.Sprintf("file:%s?mode=memory&cache=shared&_foreign_keys=on", t.Name()))
	if err != nil {
		t.Fatal(err)
	}
	sqlDB.SetMaxOpenConns(1)
	database := &DB{DB: sqlDB}
	t.Cleanup(func() { _ = database.Close() })

	applyThrough := func(name string) {
		t.Helper()
		sqlText, err := migrationFS.ReadFile("schema/" + name + ".sql")
		if err != nil {
			t.Fatal(err)
		}
		tx, err := database.BeginTx(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = tx.Rollback() }()
		if _, err := tx.ExecContext(ctx, string(sqlText)); err != nil {
			t.Fatal(err)
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO schema_migrations (version, applied_at) VALUES (?, datetime('now'))`, name); err != nil {
			t.Fatal(err)
		}
		if err := tx.Commit(); err != nil {
			t.Fatal(err)
		}
	}
	applyThrough("0001_initial")
	applyThrough("0002_go_runtime")

	if _, err := database.ExecContext(ctx, `
		INSERT INTO sessions (id, title, status, created_at, updated_at)
		VALUES ('session_1', 'Upgrade', 'active', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z');
		INSERT INTO messages (id, session_id, role, content, content_type, sequence, created_at)
		VALUES ('message_1', 'session_1', 'user', 'hello', 'text', 1, '2026-01-01T00:00:00Z');
		INSERT INTO agent_runs (id, session_id, user_message_id, agent_id, trace_id, status, model_provider, model_name, created_at)
		VALUES ('run_1', 'session_1', 'message_1', 'general_assistant', 'trace_1', 'running', 'ollama', 'llama3.2', '2026-01-01T00:00:00Z');
		INSERT INTO tool_calls (id, run_id, agent_id, server_name, tool_name, args_json, args_hash, status, result_summary, created_at)
		VALUES ('call_1', 'run_1', 'general_assistant', 'system', 'system.echo', '{"value":"hello"}', 'sha256:args', 'completed', 'hello', '2026-01-01T00:00:00Z');
	`); err != nil {
		t.Fatal(err)
	}

	if err := ApplyMigrations(ctx, database); err != nil {
		t.Fatal(err)
	}

	var status, resultSummary string
	var modelToolCallID sql.NullString
	if err := database.QueryRowContext(ctx, `SELECT status, result_summary, model_tool_call_id FROM tool_calls WHERE id = 'call_1'`).Scan(&status, &resultSummary, &modelToolCallID); err != nil {
		t.Fatal(err)
	}
	if status != "completed" || resultSummary != "hello" {
		t.Fatalf("upgraded tool call = status %q result %q, want preserved completed/hello", status, resultSummary)
	}
	if modelToolCallID.Valid {
		t.Fatalf("model_tool_call_id = %q, want NULL for pre-upgrade row", modelToolCallID.String)
	}
	var executionActive int
	var executionExitAcknowledgedAt sql.NullString
	if err := database.QueryRowContext(ctx, `SELECT execution_active, execution_exit_acknowledged_at FROM agent_runs WHERE id = 'run_1'`).Scan(&executionActive, &executionExitAcknowledgedAt); err != nil {
		t.Fatal(err)
	}
	if executionActive != 1 || executionExitAcknowledgedAt.Valid {
		t.Fatalf("upgraded execution gate = active %d exit_ack %q, want fenced active attempt and NULL", executionActive, executionExitAcknowledgedAt.String)
	}
	var applied int
	if err := database.QueryRowContext(ctx, `SELECT COUNT(*) FROM schema_migrations WHERE version = '0003_tool_call_model_identity'`).Scan(&applied); err != nil {
		t.Fatal(err)
	}
	if applied != 1 {
		t.Fatalf("0003 migration count = %d, want 1", applied)
	}
	if err := database.QueryRowContext(ctx, `SELECT COUNT(*) FROM schema_migrations WHERE version = '0004_execution_exit_gate'`).Scan(&applied); err != nil {
		t.Fatal(err)
	}
	if applied != 1 {
		t.Fatalf("0004 migration count = %d, want 1", applied)
	}
	if err := database.QueryRowContext(ctx, `SELECT COUNT(*) FROM schema_migrations WHERE version = '0005_timestamp_ordering'`).Scan(&applied); err != nil {
		t.Fatal(err)
	}
	if applied != 1 {
		t.Fatalf("0005 migration count = %d, want 1", applied)
	}
}

func TestSessionTitleOriginMigrationClassifiesLegacyPlaceholders(t *testing.T) {
	ctx := context.Background()
	database, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if _, err := database.ExecContext(ctx,
		`CREATE TABLE schema_migrations (version TEXT PRIMARY KEY, applied_at TEXT NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	names, err := migrationNames()
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range names {
		if name == "0010_session_title_origin.sql" {
			break
		}
		applyMigration(t, ctx, database, name)
	}
	for _, row := range []struct {
		id    string
		title any
	}{
		{id: "legacy", title: "New chat"},
		{id: "blank", title: nil},
		{id: "named", title: "Budget planning"},
		{id: "automation", title: "New chat"},
	} {
		if _, err := database.ExecContext(ctx, `
			INSERT INTO sessions (id, title, created_at, updated_at)
			VALUES (?, ?, datetime('now'), datetime('now'))
		`, row.id, row.title); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := database.ExecContext(ctx, `
		INSERT INTO automations (
			id, name, prompt, schedule_kind, interval_seconds, enabled,
			next_due_at, session_id, created_at, updated_at
		)
		VALUES (
			'auto_new_chat', 'New chat', 'Summarise the sandbox.', 'interval',
			300, 1, '2099-01-01T00:00:00Z', 'automation',
			'2026-08-18T00:00:00Z', '2026-08-18T00:00:00Z'
		)
	`); err != nil {
		t.Fatal(err)
	}

	applyMigration(t, ctx, database, "0010_session_title_origin.sql")

	for id, want := range map[string]string{
		"legacy":     "unset",
		"blank":      "unset",
		"named":      "explicit",
		"automation": "explicit",
	} {
		var got string
		if err := database.QueryRowContext(ctx,
			`SELECT title_origin FROM sessions WHERE id = ?`, id).Scan(&got); err != nil {
			t.Fatal(err)
		}
		if got != want {
			t.Fatalf("session %s title_origin = %q, want %q", id, got, want)
		}
	}
}

func applyMigration(t *testing.T, ctx context.Context, database *DB, name string) {
	t.Helper()
	sqlText, err := migrationFS.ReadFile("schema/" + name)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.ExecContext(ctx, string(sqlText)); err != nil {
		t.Fatal(err)
	}
	version := name[:len(name)-len(".sql")]
	if _, err := database.ExecContext(ctx,
		`INSERT INTO schema_migrations (version, applied_at) VALUES (?, datetime('now'))`, version); err != nil {
		t.Fatal(err)
	}
}

func insertTestSession(t *testing.T, ctx context.Context, database *DB, id string) {
	t.Helper()
	if _, err := database.ExecContext(ctx, `
		INSERT INTO sessions (id, created_at, updated_at)
		VALUES (?, datetime('now'), datetime('now'))`, id); err != nil {
		t.Fatal(err)
	}
}

func assertFTSMessageIDs(t *testing.T, ctx context.Context, database *DB, query string, wantCount int, want []string) {
	t.Helper()
	rows, err := database.QueryContext(ctx, `
		SELECT messages.id
		FROM messages_fts
		JOIN messages ON messages.rowid = messages_fts.rowid
		WHERE messages_fts MATCH ?
		ORDER BY messages.id`, query)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()

	got := make([]string, 0)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatal(err)
		}
		got = append(got, id)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if len(got) != wantCount {
		t.Fatalf("FTS search %q count = %d, want %d (IDs = %v)", query, len(got), wantCount, got)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("FTS search %q IDs = %v, want %v", query, got, want)
	}
}
