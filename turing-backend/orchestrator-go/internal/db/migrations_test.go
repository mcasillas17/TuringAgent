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
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("applied migrations = %v, want %v", got, want)
	}
}

func TestCurrentSchemaVersionUsesLatestEmbeddedMigrationPrefix(t *testing.T) {
	got, err := CurrentSchemaVersion()
	if err != nil {
		t.Fatal(err)
	}
	if got != "0006" {
		t.Fatalf("CurrentSchemaVersion = %q, want 0006", got)
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
