package audit

import (
	"context"
	"database/sql"
	"fmt"
	"math"
	"strings"
	"testing"

	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/db"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
)

func openAuditTestDB(t *testing.T) *db.DB {
	t.Helper()
	name := strings.NewReplacer("/", "_", " ", "_", ":", "_").Replace(t.Name())
	sqlDB, err := sql.Open("sqlite3", fmt.Sprintf("file:%s?mode=memory&cache=shared&_foreign_keys=on", name))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	sqlDB.SetMaxOpenConns(1)
	database := &db.DB{DB: sqlDB}
	t.Cleanup(func() { _ = database.Close() })
	if err := db.ApplyMigrations(context.Background(), database); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}
	return database
}

func TestRecordStoresCanonicalPayload(t *testing.T) {
	database := openAuditTestDB(t)
	service := New(repository.New(database))

	if err := service.Record(context.Background(), "run_1", "client", "user_1", "approval.approved", "appr_1", map[string]any{"b": float64(2), "a": "one"}); err != nil {
		t.Fatal(err)
	}

	var payloadJSON string
	if err := database.QueryRowContext(context.Background(), `SELECT payload_json FROM audit_logs WHERE action = 'approval.approved'`).Scan(&payloadJSON); err != nil {
		t.Fatal(err)
	}
	if payloadJSON != `{"a":"one","b":2}` {
		t.Fatalf("payload_json = %s", payloadJSON)
	}
}

func TestRecordRejectsUnsafeDynamicPayload(t *testing.T) {
	database := openAuditTestDB(t)
	service := New(repository.New(database))

	err := service.Record(context.Background(), "run_1", "runtime", "worker_1", "tool.call.started", "call_1", map[string]any{"bad": math.NaN()})
	if err == nil {
		t.Fatal("Record succeeded with NaN payload, want error")
	}
}

func TestRecordForExistingRunDoesNotRecreatePayloadAfterSessionDeletion(t *testing.T) {
	ctx := context.Background()
	database := openAuditTestDB(t)
	repo := repository.New(database)
	service := New(repo)
	session, err := repo.CreateSession(ctx, "Delete audited session")
	if err != nil {
		t.Fatal(err)
	}
	enqueued, err := repo.EnqueueUserMessage(ctx, repository.EnqueueUserMessageInput{
		SessionID:     session.SessionID,
		Content:       "private content",
		AgentID:       "general_assistant",
		ModelProvider: "ollama",
		Model:         "llama3.2",
	})
	if err != nil {
		t.Fatal(err)
	}
	inserted, err := service.RecordForExistingRun(
		ctx,
		enqueued.RunID,
		"runtime",
		"",
		"tool.call.before",
		"call_before_delete",
		map[string]any{"content": "private content"},
	)
	if err != nil {
		t.Fatal(err)
	}
	if !inserted {
		t.Fatal("existing-run audit was not inserted")
	}
	var beforeCount int
	if err := database.QueryRowContext(
		ctx,
		`SELECT COUNT(*) FROM audit_logs WHERE action = 'tool.call.before' AND correlation_id = ?`,
		enqueued.RunID,
	).Scan(&beforeCount); err != nil {
		t.Fatal(err)
	}
	if beforeCount != 1 {
		t.Fatalf("existing-run audit count = %d, want 1", beforeCount)
	}
	if _, err := database.ExecContext(ctx, `UPDATE jobs SET status = 'completed' WHERE run_id = ?`, enqueued.RunID); err != nil {
		t.Fatal(err)
	}
	if _, err := database.ExecContext(
		ctx,
		`UPDATE agent_runs SET status = 'completed', execution_active = 0 WHERE id = ?`,
		enqueued.RunID,
	); err != nil {
		t.Fatal(err)
	}
	if err := repo.DeleteSession(ctx, session.SessionID); err != nil {
		t.Fatal(err)
	}

	inserted, err = service.RecordForExistingRun(
		ctx,
		enqueued.RunID,
		"runtime",
		"",
		"tool.call.after",
		"call_after_delete",
		map[string]any{"content": "late private content"},
	)
	if err != nil {
		t.Fatal(err)
	}
	if inserted {
		t.Fatal("deleted-run audit reported an insertion")
	}
	var lateCount int
	if err := database.QueryRowContext(
		ctx,
		`SELECT COUNT(*) FROM audit_logs WHERE action = 'tool.call.after' AND correlation_id = ?`,
		enqueued.RunID,
	).Scan(&lateCount); err != nil {
		t.Fatal(err)
	}
	if lateCount != 0 {
		t.Fatalf("late deleted-run audit count = %d, want 0", lateCount)
	}
}
