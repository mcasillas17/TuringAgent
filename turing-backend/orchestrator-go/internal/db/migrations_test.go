package db

import (
	"context"
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

	if _, err := database.ExecContext(ctx, `DELETE FROM messages WHERE id = 'm-update'`); err != nil {
		t.Fatal(err)
	}
	assertFTSMessageIDs(t, ctx, database, "mitochondria", 0, []string{})
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
	defer rows.Close()

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
