package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/db"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/runcorrelation"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/runoutcome"
	"google.golang.org/protobuf/types/known/structpb"
)

func openTestDB(t *testing.T) *db.DB {
	t.Helper()
	database, err := db.Open(filepath.Join(t.TempDir(), "turing.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if err := db.ApplyMigrations(context.Background(), database); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}
	return database
}

func TestSessionMessageRunJobTransaction(t *testing.T) {
	database := openTestDB(t)
	repo := New(database)
	ctx := context.Background()
	session, err := repo.CreateSession(ctx, "Test chat")
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	result, err := repo.EnqueueUserMessage(ctx, EnqueueUserMessageInput{
		SessionID:     session.SessionID,
		Content:       "hello",
		AgentID:       "general_assistant",
		ModelProvider: "ollama",
		Model:         "llama3.2",
	})
	if err != nil {
		t.Fatalf("EnqueueUserMessage: %v", err)
	}
	messages, err := repo.ListMessages(ctx, session.SessionID, 50)
	if err != nil {
		t.Fatalf("ListMessages: %v", err)
	}
	if len(messages) != 2 {
		t.Fatalf("message count = %d, want 2", len(messages))
	}
	if result.Status != "queued" || result.RunID == "" || result.JobID == "" || result.TraceID == "" {
		t.Fatalf("bad enqueue result: %+v", result)
	}
	if messages[0].MessageID != result.UserMessageID || messages[0].Role != "user" || messages[0].Content != "hello" {
		t.Fatalf("bad user message: %+v", messages[0])
	}
	if messages[1].MessageID != result.AssistantMessageID || messages[1].Role != "assistant" || messages[1].Content != "" {
		t.Fatalf("bad assistant message: %+v", messages[1])
	}
	if messages[1].RunID != result.RunID {
		t.Fatalf("assistant message run ID = %q, want %q", messages[1].RunID, result.RunID)
	}
	var runStatus, runUserMessageID, runAssistantMessageID, runTraceID, runAgentID, runProvider, runModel string
	if err := database.QueryRowContext(ctx, `
		SELECT status, user_message_id, assistant_message_id, trace_id, agent_id, model_provider, model_name
		FROM agent_runs
		WHERE id = ?
	`, result.RunID).Scan(&runStatus, &runUserMessageID, &runAssistantMessageID, &runTraceID, &runAgentID, &runProvider, &runModel); err != nil {
		t.Fatalf("query run: %v", err)
	}
	if runStatus != "queued" || runUserMessageID != result.UserMessageID || runAssistantMessageID != result.AssistantMessageID || runTraceID != result.TraceID || runAgentID != "general_assistant" || runProvider != "ollama" || runModel != "llama3.2" {
		t.Fatalf("bad run row: status=%q user=%q assistant=%q trace=%q agent=%q provider=%q model=%q", runStatus, runUserMessageID, runAssistantMessageID, runTraceID, runAgentID, runProvider, runModel)
	}
	var jobRunID, jobAgentID, jobStatus, payloadJSON string
	if err := database.QueryRowContext(ctx, `
		SELECT run_id, agent_id, status, payload_json
		FROM jobs
		WHERE id = ?
	`, result.JobID).Scan(&jobRunID, &jobAgentID, &jobStatus, &payloadJSON); err != nil {
		t.Fatalf("query job: %v", err)
	}
	if jobRunID != result.RunID || jobAgentID != "general_assistant" || jobStatus != "pending" {
		t.Fatalf("bad job row: run=%q agent=%q status=%q", jobRunID, jobAgentID, jobStatus)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(payloadJSON), &payload); err != nil {
		t.Fatalf("decode job payload: %v", err)
	}
	if payload["userMessageId"] != result.UserMessageID || payload["assistantMessageId"] != result.AssistantMessageID || payload["traceId"] != result.TraceID {
		t.Fatalf("bad job payload: %+v", payload)
	}
	if result.QueuedEvent.Type != "agent.run.queued" || !result.QueuedEvent.RunID.Valid || result.QueuedEvent.RunID.String != result.RunID || result.QueuedEvent.TraceID != result.TraceID {
		t.Fatalf("bad queued event result: %+v", result.QueuedEvent)
	}
	var queuedPayload map[string]any
	if err := json.Unmarshal([]byte(result.QueuedEvent.PayloadJSON), &queuedPayload); err != nil {
		t.Fatalf("decode queued event payload: %v", err)
	}
	if queuedPayload["runId"] != result.RunID || queuedPayload["jobId"] != result.JobID || queuedPayload["status"] != "queued" || queuedPayload["agentId"] != "general_assistant" {
		t.Fatalf("bad queued event payload: %+v", queuedPayload)
	}
	replayed, latest, err := repo.ReplayEvents(ctx, session.SessionID, 0, 10)
	if err != nil {
		t.Fatal(err)
	}
	if latest != result.QueuedEvent.Sequence || len(replayed) != 2 {
		t.Fatalf("enqueue event replay latest=%d events=%+v", latest, replayed)
	}
	if replayed[0].EventID != result.SessionUpdatedEvent.EventID || replayed[0].Type != "session.updated" {
		t.Fatalf("bad session update replay event: %+v", replayed[0])
	}
	if replayed[1].EventID != result.QueuedEvent.EventID || replayed[1].Type != "agent.run.queued" {
		t.Fatalf("bad queued replay event: %+v", replayed[1])
	}
}

func TestListMessagesBeforeReturnsOnlyStablePredecessors(t *testing.T) {
	database := openTestDB(t)
	repo := New(database)
	ctx := context.Background()
	session, err := repo.CreateSession(ctx, "Causal messages")
	if err != nil {
		t.Fatal(err)
	}
	for _, message := range []struct {
		id       string
		role     string
		content  string
		sequence int
	}{
		{id: "msg_a", role: "system", content: "instructions", sequence: 1},
		{id: "msg_b", role: "user", content: "earlier turn", sequence: 2},
		{id: "msg_c", role: "user", content: "current turn", sequence: 3},
		{id: "msg_d", role: "assistant", content: "future placeholder", sequence: 4},
	} {
		if _, err := database.ExecContext(ctx, `
			INSERT INTO messages (id, session_id, role, content, content_type, sequence, created_at)
			VALUES (?, ?, ?, ?, 'text', ?, '2026-08-10T22:42:30.000000000Z')
		`, message.id, session.SessionID, message.role, message.content, message.sequence); err != nil {
			t.Fatal(err)
		}
	}

	messages, err := repo.ListMessagesBefore(ctx, session.SessionID, "msg_c", 50)
	if err != nil {
		t.Fatalf("ListMessagesBefore: %v", err)
	}
	if len(messages) != 2 || messages[0].MessageID != "msg_a" || messages[1].MessageID != "msg_b" {
		t.Fatalf("causal messages = %+v, want msg_a then msg_b", messages)
	}
}

func TestEventsAreSequencedPerSession(t *testing.T) {
	database := openTestDB(t)
	repo := New(database)
	ctx := context.Background()
	session, err := repo.CreateSession(ctx, "Events")
	if err != nil {
		t.Fatal(err)
	}
	first, err := repo.AppendEvent(ctx, AppendEventInput{SessionID: session.SessionID, TraceID: "trace_1", Type: "system", PayloadJSON: `{"a":1}`})
	if err != nil {
		t.Fatal(err)
	}
	second, err := repo.AppendEvent(ctx, AppendEventInput{SessionID: session.SessionID, TraceID: "trace_1", Type: "system", PayloadJSON: `{"b":2}`})
	if err != nil {
		t.Fatal(err)
	}
	if first.Sequence != 1 || second.Sequence != 2 {
		t.Fatalf("sequences = %d/%d", first.Sequence, second.Sequence)
	}
	replayed, latest, err := repo.ReplayEvents(ctx, session.SessionID, 1, 500)
	if err != nil {
		t.Fatal(err)
	}
	if latest != 2 || len(replayed) != 1 || replayed[0].Sequence != 2 {
		t.Fatalf("replay latest=%d events=%+v", latest, replayed)
	}
}

func TestCancelRunUpdatesRunAndJob(t *testing.T) {
	database := openTestDB(t)
	repo := New(database)
	ctx := context.Background()
	session, err := repo.CreateSession(ctx, "Cancel")
	if err != nil {
		t.Fatal(err)
	}
	enqueued, err := repo.EnqueueUserMessage(ctx, EnqueueUserMessageInput{
		SessionID: session.SessionID, Content: "cancel me", AgentID: "general_assistant", ModelProvider: "ollama", Model: "llama3.2",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.MarkRunRunning(ctx, enqueued.RunID); err != nil {
		t.Fatal(err)
	}
	if _, err := cancelRunAtCurrentVersion(t, repo, enqueued.RunID); err != nil {
		t.Fatal(err)
	}
	run, err := repo.GetRun(ctx, enqueued.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if run.Status != "cancelled" {
		t.Fatalf("run status = %q", run.Status)
	}
	var jobStatus, errorCode, errorMessage string
	if err := database.QueryRowContext(ctx, `
		SELECT status, error_code, error_message
		FROM jobs
		WHERE id = ?
	`, enqueued.JobID).Scan(&jobStatus, &errorCode, &errorMessage); err != nil {
		t.Fatalf("query cancelled job: %v", err)
	}
	if jobStatus != "cancelled" || errorCode != "cancelled" || errorMessage != "client_cancelled" {
		t.Fatalf("bad cancelled job: status=%q error_code=%q error_message=%q", jobStatus, errorCode, errorMessage)
	}
}

func TestCancelRunWithEventRollsBackWhenEventAppendFails(t *testing.T) {
	database := openTestDB(t)
	repo := New(database)
	ctx := context.Background()
	session, err := repo.CreateSession(ctx, "Cancel run rollback")
	if err != nil {
		t.Fatal(err)
	}
	enqueued, err := repo.EnqueueUserMessage(ctx, EnqueueUserMessageInput{
		SessionID: session.SessionID, Content: "cancel me", AgentID: "general_assistant", ModelProvider: "ollama", Model: "llama3.2",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.MarkRunRunning(ctx, enqueued.RunID); err != nil {
		t.Fatal(err)
	}
	if _, err := database.ExecContext(ctx, `
		CREATE TRIGGER fail_cancelled_event
		BEFORE INSERT ON events
		WHEN NEW.type = 'agent.run.cancelled'
		BEGIN
			SELECT RAISE(ABORT, 'cancel event insert failed');
		END;
	`); err != nil {
		t.Fatal(err)
	}
	_, err = cancelRunEvents(t, repo, enqueued.RunID)
	if err == nil {
		t.Fatal("CancelRunWithEvent succeeded, want trigger failure")
	}
	run, err := repo.GetRun(ctx, enqueued.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if run.Status != "running" {
		t.Fatalf("run status = %q, want running after rollback", run.Status)
	}
	var jobStatus string
	if err := database.QueryRowContext(ctx, `SELECT status FROM jobs WHERE id = ?`, enqueued.JobID).Scan(&jobStatus); err != nil {
		t.Fatal(err)
	}
	if jobStatus != "in_progress" {
		t.Fatalf("job status = %q, want in_progress after rollback", jobStatus)
	}
}

func TestCancelRunFailsForTerminalRun(t *testing.T) {
	database := openTestDB(t)
	repo := New(database)
	ctx := context.Background()
	session, err := repo.CreateSession(ctx, "Cancel terminal")
	if err != nil {
		t.Fatal(err)
	}
	enqueued, err := repo.EnqueueUserMessage(ctx, EnqueueUserMessageInput{
		SessionID: session.SessionID, Content: "already done", AgentID: "general_assistant", ModelProvider: "ollama", Model: "llama3.2",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.ExecContext(ctx, `UPDATE agent_runs SET status = 'completed' WHERE id = ?`, enqueued.RunID); err != nil {
		t.Fatal(err)
	}
	if _, err := cancelRunAtCurrentVersion(t, repo, enqueued.RunID); err == nil {
		t.Fatal("expected cancel run to fail for completed run")
	}
	run, err := repo.GetRun(ctx, enqueued.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if run.Status != "completed" {
		t.Fatalf("run status = %q, want completed", run.Status)
	}
}

func TestAcknowledgeExecutionExitClearsTerminalGateIdempotently(t *testing.T) {
	database := openTestDB(t)
	repo := New(database)
	ctx := context.Background()
	session, err := repo.CreateSession(ctx, "Acknowledge execution exit")
	if err != nil {
		t.Fatal(err)
	}
	enqueued, err := repo.EnqueueUserMessage(ctx, EnqueueUserMessageInput{
		SessionID: session.SessionID, Content: "terminalized elsewhere", AgentID: "general_assistant", ModelProvider: "ollama", Model: "llama3.2",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.ExecContext(ctx, `
		UPDATE agent_runs
		SET status = 'failed', execution_active = 1, execution_exit_acknowledged_at = NULL
		WHERE id = ?
	`, enqueued.RunID); err != nil {
		t.Fatal(err)
	}
	if err := repo.AcknowledgeExecutionExit(ctx, enqueued.RunID); err != nil {
		t.Fatalf("first AcknowledgeExecutionExit: %v", err)
	}
	if err := repo.AcknowledgeExecutionExit(ctx, enqueued.RunID); err != nil {
		t.Fatalf("idempotent AcknowledgeExecutionExit: %v", err)
	}
	var active int
	var acknowledgedAt sql.NullString
	if err := database.QueryRowContext(ctx, `SELECT execution_active, execution_exit_acknowledged_at FROM agent_runs WHERE id = ?`, enqueued.RunID).Scan(&active, &acknowledgedAt); err != nil {
		t.Fatal(err)
	}
	if active != 0 || !acknowledgedAt.Valid {
		t.Fatalf("execution exit state active=%d acknowledged=%q, want inactive acknowledged", active, acknowledgedAt.String)
	}
}

func TestFailRunWithEventPreservingExecutionHoldsGlobalCapacityUntilExitAck(t *testing.T) {
	database := openTestDB(t)
	repo := New(database)
	ctx := context.Background()
	firstSession, err := repo.CreateSession(ctx, "Preserved execution")
	if err != nil {
		t.Fatal(err)
	}
	first, err := repo.EnqueueUserMessage(ctx, EnqueueUserMessageInput{
		SessionID: firstSession.SessionID, Content: "first", AgentID: "general_assistant", ModelProvider: "ollama", Model: "llama3.2",
	})
	if err != nil {
		t.Fatal(err)
	}
	secondSession, err := repo.CreateSession(ctx, "Later global work")
	if err != nil {
		t.Fatal(err)
	}
	second, err := repo.EnqueueUserMessage(ctx, EnqueueUserMessageInput{
		SessionID: secondSession.SessionID, Content: "second", AgentID: "general_assistant", ModelProvider: "ollama", Model: "llama3.2",
	})
	if err != nil {
		t.Fatal(err)
	}
	claimed, err := repo.ClaimNextJobWithLimit(ctx, "general_assistant", "worker-1", 1, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if claimed.RunID != first.RunID {
		t.Fatalf("claimed run = %q, want %q", claimed.RunID, first.RunID)
	}
	if _, err := failRunPreservingExecutionAtCurrentVersion(t, repo, first.RunID, testFailure("approval_delivery_failed")); err != nil {
		t.Fatalf("FailRunWithEventPreservingExecution: %v", err)
	}
	run, err := repo.GetRun(ctx, first.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if run.Status != "failed" || !run.ExecutionActive || run.WorkerID != "worker-1" || run.ExecutionAttemptID != claimed.AssignmentAttemptID {
		t.Fatalf("preserved terminal run = %+v, want failed active worker-owned attempt", run)
	}
	blocked, err := repo.ClaimNextJobWithLimit(ctx, "general_assistant", "worker-2", 1, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if blocked.JobID != "" {
		t.Fatalf("claimed global work %+v before execution exit acknowledgement", blocked)
	}
	if err := repo.AcknowledgeExecutionExit(ctx, first.RunID); err != nil {
		t.Fatal(err)
	}
	released, err := repo.ClaimNextJobWithLimit(ctx, "general_assistant", "worker-2", 1, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if released.RunID != second.RunID {
		t.Fatalf("claim after execution exit = %+v, want %q", released, second.RunID)
	}
}

func TestFailRunWithEventPreservingExecutionFinalizesInactiveRun(t *testing.T) {
	database := openTestDB(t)
	repo := New(database)
	ctx := context.Background()
	session, err := repo.CreateSession(ctx, "Inactive terminalization")
	if err != nil {
		t.Fatal(err)
	}
	enqueued, err := repo.EnqueueUserMessage(ctx, EnqueueUserMessageInput{
		SessionID: session.SessionID, Content: "inactive", AgentID: "general_assistant", ModelProvider: "ollama", Model: "llama3.2",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.MarkRunRunning(ctx, enqueued.RunID); err != nil {
		t.Fatal(err)
	}
	if _, err := failRunPreservingExecutionAtCurrentVersion(t, repo, enqueued.RunID, testFailure("approval_delivery_failed")); err != nil {
		t.Fatal(err)
	}
	run, err := repo.GetRun(ctx, enqueued.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if run.ExecutionActive || run.ExecutionState != "exited" {
		t.Fatalf("inactive terminal run = %+v, want inactive exited run", run)
	}
}

func TestClaimNextJobMarksRunAndJobRunning(t *testing.T) {
	database := openTestDB(t)
	repo := New(database)
	ctx := context.Background()
	session, err := repo.CreateSession(ctx, "Claim job")
	if err != nil {
		t.Fatal(err)
	}
	enqueued, err := repo.EnqueueUserMessage(ctx, EnqueueUserMessageInput{
		SessionID: session.SessionID, Content: "claim me", AgentID: "general_assistant", ModelProvider: "ollama", Model: "llama3.2",
	})
	if err != nil {
		t.Fatal(err)
	}
	job, err := repo.ClaimNextJob(ctx, "general_assistant", "worker-1")
	if err != nil {
		t.Fatal(err)
	}
	if job.JobID != enqueued.JobID || job.RunID != enqueued.RunID || job.SessionID != session.SessionID {
		t.Fatalf("claimed wrong job: %+v", job)
	}
	if job.UserMessageID != enqueued.UserMessageID || job.AssistantMessageID != enqueued.AssistantMessageID || job.TraceID != enqueued.TraceID {
		t.Fatalf("bad job identifiers: %+v", job)
	}
	if job.ModelProvider != "ollama" || job.Model != "llama3.2" || job.UserText != "claim me" || job.Attempt != 1 {
		t.Fatalf("bad job payload: %+v", job)
	}
	run, err := repo.GetRun(ctx, enqueued.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if run.Status != "running" {
		t.Fatalf("run status = %q, want running", run.Status)
	}
	var jobStatus, leaseOwner string
	var pickedUpAt sql.NullString
	if err := database.QueryRowContext(ctx, `
		SELECT status, lease_owner, picked_up_at
		FROM jobs
		WHERE id = ?
	`, enqueued.JobID).Scan(&jobStatus, &leaseOwner, &pickedUpAt); err != nil {
		t.Fatalf("query claimed job: %v", err)
	}
	if jobStatus != "in_progress" || leaseOwner != "worker-1" || !pickedUpAt.Valid || pickedUpAt.String == "" {
		t.Fatalf("bad claimed job row: status=%q lease_owner=%q picked_up_at=%q", jobStatus, leaseOwner, pickedUpAt.String)
	}
	replayed, latest, err := repo.ReplayEvents(ctx, session.SessionID, enqueued.QueuedEvent.Sequence, 10)
	if err != nil {
		t.Fatal(err)
	}
	if latest != enqueued.QueuedEvent.Sequence+1 || len(replayed) != 1 {
		t.Fatalf("started event replay latest=%d events=%+v", latest, replayed)
	}
	started := replayed[0]
	if started.Type != "agent.run.started" || !started.RunID.Valid || started.RunID.String != enqueued.RunID || started.TraceID != enqueued.TraceID {
		t.Fatalf("bad started event: %+v", started)
	}
	var startedPayload map[string]any
	if err := json.Unmarshal([]byte(started.PayloadJSON), &startedPayload); err != nil {
		t.Fatalf("decode started event payload: %v", err)
	}
	if startedPayload["runId"] != enqueued.RunID || startedPayload["jobId"] != enqueued.JobID || startedPayload["status"] != "running" || startedPayload["agentId"] != "general_assistant" || startedPayload["attempt"] != float64(1) {
		t.Fatalf("bad started event payload: %+v", startedPayload)
	}
}

func TestClaimNextJobWaitsForEarlierSessionRunToTerminalize(t *testing.T) {
	database := openTestDB(t)
	repo := New(database)
	ctx := context.Background()
	session, err := repo.CreateSession(ctx, "Causal job claims")
	if err != nil {
		t.Fatal(err)
	}
	first, err := repo.EnqueueUserMessage(ctx, EnqueueUserMessageInput{
		SessionID: session.SessionID, Content: "first", AgentID: "general_assistant", ModelProvider: "ollama", Model: "llama3.2",
	})
	if err != nil {
		t.Fatal(err)
	}
	second, err := repo.EnqueueUserMessage(ctx, EnqueueUserMessageInput{
		SessionID: session.SessionID, Content: "second", AgentID: "general_assistant", ModelProvider: "ollama", Model: "llama3.2",
	})
	if err != nil {
		t.Fatal(err)
	}

	claimedFirst, err := repo.ClaimNextJob(ctx, "general_assistant", "worker-1")
	if err != nil {
		t.Fatal(err)
	}
	if claimedFirst.RunID != first.RunID {
		t.Fatalf("first claim run = %q, want %q", claimedFirst.RunID, first.RunID)
	}
	blocked, err := repo.ClaimNextJob(ctx, "general_assistant", "worker-2")
	if err != nil {
		t.Fatal(err)
	}
	if blocked.JobID != "" {
		t.Fatalf("claimed later same-session job while earlier run was active: %+v", blocked)
	}

	if _, err := completeRunAtCurrentVersion(t, repo, first.RunID, first.AssistantMessageID, "first done", nil); err != nil {
		t.Fatal(err)
	}
	claimedSecond, err := repo.ClaimNextJob(ctx, "general_assistant", "worker-2")
	if err != nil {
		t.Fatal(err)
	}
	if claimedSecond.RunID != second.RunID {
		t.Fatalf("second claim run = %q, want %q", claimedSecond.RunID, second.RunID)
	}
}

func TestClaimNextJobRollsBackWhenStartedEventAppendFails(t *testing.T) {
	database := openTestDB(t)
	repo := New(database)
	ctx := context.Background()
	session, err := repo.CreateSession(ctx, "Claim rollback")
	if err != nil {
		t.Fatal(err)
	}
	enqueued, err := repo.EnqueueUserMessage(ctx, EnqueueUserMessageInput{
		SessionID: session.SessionID, Content: "claim me", AgentID: "general_assistant", ModelProvider: "ollama", Model: "llama3.2",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.ExecContext(ctx, `
		CREATE TRIGGER fail_started_event
		BEFORE INSERT ON events
		WHEN NEW.type = 'agent.run.started'
		BEGIN
			SELECT RAISE(ABORT, 'started event insert failed');
		END;
	`); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.ClaimNextJob(ctx, "general_assistant", "worker-1"); err == nil {
		t.Fatal("ClaimNextJob succeeded, want started event append failure")
	}
	run, err := repo.GetRun(ctx, enqueued.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if run.Status != "queued" {
		t.Fatalf("run status = %q, want queued after rollback", run.Status)
	}
	var jobStatus string
	if err := database.QueryRowContext(ctx, `SELECT status FROM jobs WHERE id = ?`, enqueued.JobID).Scan(&jobStatus); err != nil {
		t.Fatal(err)
	}
	if jobStatus != "pending" {
		t.Fatalf("job status = %q, want pending after rollback", jobStatus)
	}
}

func TestRequeueClaimedJobIncrementsAttempt(t *testing.T) {
	database := openTestDB(t)
	repo := New(database)
	ctx := context.Background()
	session, err := repo.CreateSession(ctx, "Retry attempts")
	if err != nil {
		t.Fatal(err)
	}
	enqueued, err := repo.EnqueueUserMessage(ctx, EnqueueUserMessageInput{
		SessionID: session.SessionID, Content: "retry me", AgentID: "general_assistant", ModelProvider: "ollama", Model: "llama3.2",
	})
	if err != nil {
		t.Fatal(err)
	}
	first, err := repo.ClaimNextJob(ctx, "general_assistant", "worker-1")
	if err != nil {
		t.Fatal(err)
	}
	if first.Attempt != 1 {
		t.Fatalf("first attempt = %d, want 1", first.Attempt)
	}
	if err := repo.RequeueClaimedJob(ctx, enqueued.JobID, enqueued.RunID); err != nil {
		t.Fatal(err)
	}
	second, err := repo.ClaimNextJob(ctx, "general_assistant", "worker-2")
	if err != nil {
		t.Fatal(err)
	}
	if second.Attempt != 2 {
		t.Fatalf("second attempt = %d, want 2", second.Attempt)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(second.StartedEvent.PayloadJSON), &payload); err != nil {
		t.Fatal(err)
	}
	if payload["attempt"] != float64(2) {
		t.Fatalf("started retry payload = %+v", payload)
	}
}

func TestCompleteRunUpdatesRunJobAndAssistantMessage(t *testing.T) {
	database := openTestDB(t)
	repo := New(database)
	ctx := context.Background()
	session, err := repo.CreateSession(ctx, "Complete run")
	if err != nil {
		t.Fatal(err)
	}
	enqueued, err := repo.EnqueueUserMessage(ctx, EnqueueUserMessageInput{
		SessionID: session.SessionID, Content: "complete me", AgentID: "general_assistant", ModelProvider: "ollama", Model: "llama3.2",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.MarkRunRunning(ctx, enqueued.RunID); err != nil {
		t.Fatal(err)
	}
	if _, err := completeRunAtCurrentVersion(t, repo, enqueued.RunID, enqueued.AssistantMessageID, "done", nil); err != nil {
		t.Fatal(err)
	}
	run, err := repo.GetRun(ctx, enqueued.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if run.Status != "completed" {
		t.Fatalf("run status = %q, want completed", run.Status)
	}
	var jobStatus, assistantContent string
	if err := database.QueryRowContext(ctx, `SELECT status FROM jobs WHERE id = ?`, enqueued.JobID).Scan(&jobStatus); err != nil {
		t.Fatalf("query completed job: %v", err)
	}
	if err := database.QueryRowContext(ctx, `SELECT content FROM messages WHERE id = ?`, enqueued.AssistantMessageID).Scan(&assistantContent); err != nil {
		t.Fatalf("query assistant message: %v", err)
	}
	if jobStatus != "completed" || assistantContent != "done" {
		t.Fatalf("completion status=%q assistant_content=%q", jobStatus, assistantContent)
	}
}

func TestCompleteRunWithEventRollsBackWhenEventAppendFails(t *testing.T) {
	database := openTestDB(t)
	repo := New(database)
	ctx := context.Background()
	session, err := repo.CreateSession(ctx, "Complete run rollback")
	if err != nil {
		t.Fatal(err)
	}
	enqueued, err := repo.EnqueueUserMessage(ctx, EnqueueUserMessageInput{
		SessionID: session.SessionID, Content: "complete me", AgentID: "general_assistant", ModelProvider: "ollama", Model: "llama3.2",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.MarkRunRunning(ctx, enqueued.RunID); err != nil {
		t.Fatal(err)
	}
	if _, err := database.ExecContext(ctx, `
		CREATE TRIGGER fail_completed_event
		BEFORE INSERT ON events
		WHEN NEW.type = 'agent.run.completed'
		BEGIN
			SELECT RAISE(ABORT, 'terminal event insert failed');
		END;
	`); err != nil {
		t.Fatal(err)
	}
	_, err = completeRunEvents(t, repo, enqueued.RunID, enqueued.AssistantMessageID, "done", nil)
	if err == nil {
		t.Fatal("CompleteRunWithEvent succeeded, want trigger failure")
	}
	run, err := repo.GetRun(ctx, enqueued.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if run.Status != "running" {
		t.Fatalf("run status = %q, want running after rollback", run.Status)
	}
	var jobStatus, assistantContent string
	if err := database.QueryRowContext(ctx, `SELECT status FROM jobs WHERE id = ?`, enqueued.JobID).Scan(&jobStatus); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRowContext(ctx, `SELECT content FROM messages WHERE id = ?`, enqueued.AssistantMessageID).Scan(&assistantContent); err != nil {
		t.Fatal(err)
	}
	if jobStatus != "in_progress" || assistantContent != "" {
		t.Fatalf("rollback state: job_status=%q assistant_content=%q", jobStatus, assistantContent)
	}
}

func TestCompleteRunWithEventAppendsMessageCompletedBeforeRunCompleted(t *testing.T) {
	database := openTestDB(t)
	repo := New(database)
	ctx := context.Background()
	session, err := repo.CreateSession(ctx, "Complete run events")
	if err != nil {
		t.Fatal(err)
	}
	enqueued, err := repo.EnqueueUserMessage(ctx, EnqueueUserMessageInput{
		SessionID: session.SessionID, Content: "complete me", AgentID: "general_assistant", ModelProvider: "ollama", Model: "llama3.2",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.MarkRunRunning(ctx, enqueued.RunID); err != nil {
		t.Fatal(err)
	}
	completedEvents, err := completeRunEvents(t, repo, enqueued.RunID, enqueued.AssistantMessageID, "done", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(completedEvents) == 0 {
		t.Fatal("CompleteRunWithEvent returned no events")
	}
	runCompleted := completedEvents[len(completedEvents)-1]
	replayed, _, err := repo.ReplayEvents(ctx, session.SessionID, enqueued.QueuedEvent.Sequence, 10)
	if err != nil {
		t.Fatal(err)
	}
	var messageCompleted, terminal Event
	for _, event := range replayed {
		if event.Type == "message.completed" {
			messageCompleted = event
		}
		if event.EventID == runCompleted.EventID {
			terminal = event
		}
	}
	if messageCompleted.EventID == "" || terminal.EventID == "" {
		t.Fatalf("events missing message_completed=%+v terminal=%+v replayed=%+v", messageCompleted, terminal, replayed)
	}
	if messageCompleted.Sequence >= terminal.Sequence {
		t.Fatalf("message.completed sequence=%d, want before run_completed sequence=%d", messageCompleted.Sequence, terminal.Sequence)
	}
	var payload map[string]string
	if err := json.Unmarshal([]byte(messageCompleted.PayloadJSON), &payload); err != nil {
		t.Fatal(err)
	}
	if payload["messageId"] != enqueued.AssistantMessageID || payload["content"] != "done" {
		t.Fatalf("message.completed payload = %+v", payload)
	}
}

func TestCompleteRunWithEventAppendsAuthoritativeMessageCompleted(t *testing.T) {
	database := openTestDB(t)
	repo := New(database)
	ctx := context.Background()
	session, err := repo.CreateSession(ctx, "Complete run authoritative event")
	if err != nil {
		t.Fatal(err)
	}
	enqueued, err := repo.EnqueueUserMessage(ctx, EnqueueUserMessageInput{
		SessionID: session.SessionID, Content: "complete me", AgentID: "general_assistant", ModelProvider: "ollama", Model: "llama3.2",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.MarkRunRunning(ctx, enqueued.RunID); err != nil {
		t.Fatal(err)
	}
	earlyPayload, err := structpb.NewStruct(map[string]any{
		"messageId": enqueued.AssistantMessageID,
		"content":   "early",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.AppendRuntimeEvent(ctx, &turingv1.TuringEvent{
		RunId:   enqueued.RunID,
		Type:    turingv1.TuringEventType_TURING_EVENT_TYPE_MESSAGE_COMPLETED,
		Payload: earlyPayload,
	}); err != nil {
		t.Fatal(err)
	}

	completedEvents, err := completeRunEvents(t, repo, enqueued.RunID, enqueued.AssistantMessageID, "authoritative", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(completedEvents) != 2 || completedEvents[0].Type != "message.completed" || completedEvents[1].Type != "agent.run.completed" {
		t.Fatalf("completed events = %+v", completedEvents)
	}
	var payload map[string]string
	if err := json.Unmarshal([]byte(completedEvents[0].PayloadJSON), &payload); err != nil {
		t.Fatal(err)
	}
	if payload["messageId"] != enqueued.AssistantMessageID || payload["content"] != "authoritative" {
		t.Fatalf("authoritative message.completed payload = %+v", payload)
	}
}

func TestAppendRuntimeEventRejectsNonActiveRun(t *testing.T) {
	database := openTestDB(t)
	repo := New(database)
	ctx := context.Background()
	session, err := repo.CreateSession(ctx, "Runtime event status")
	if err != nil {
		t.Fatal(err)
	}
	enqueued, err := repo.EnqueueUserMessage(ctx, EnqueueUserMessageInput{
		SessionID: session.SessionID, Content: "cancel then event", AgentID: "general_assistant", ModelProvider: "ollama", Model: "llama3.2",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.MarkRunRunning(ctx, enqueued.RunID); err != nil {
		t.Fatal(err)
	}
	if _, err := cancelRunEvents(t, repo, enqueued.RunID); err != nil {
		t.Fatal(err)
	}
	payload, err := structpb.NewStruct(map[string]any{"delta": "late"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.AppendRuntimeEvent(ctx, &turingv1.TuringEvent{
		RunId:   enqueued.RunID,
		Type:    turingv1.TuringEventType_TURING_EVENT_TYPE_MESSAGE_DELTA,
		Payload: payload,
	}); err == nil {
		t.Fatal("AppendRuntimeEvent succeeded for cancelled run, want error")
	}
}

func TestFailRunUpdatesRunAndJobError(t *testing.T) {
	database := openTestDB(t)
	repo := New(database)
	ctx := context.Background()
	session, err := repo.CreateSession(ctx, "Fail run")
	if err != nil {
		t.Fatal(err)
	}
	enqueued, err := repo.EnqueueUserMessage(ctx, EnqueueUserMessageInput{
		SessionID: session.SessionID, Content: "fail me", AgentID: "general_assistant", ModelProvider: "ollama", Model: "llama3.2",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.MarkRunRunning(ctx, enqueued.RunID); err != nil {
		t.Fatal(err)
	}
	if _, err := failRunAtCurrentVersion(t, repo, enqueued.RunID, testFailure("model_error")); err != nil {
		t.Fatal(err)
	}
	run, err := repo.GetRun(ctx, enqueued.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if run.Status != "failed" {
		t.Fatalf("run status = %q, want failed", run.Status)
	}
	var jobStatus, runCode, jobCode string
	var runMessage, jobMessage sql.NullString
	if err := database.QueryRowContext(ctx, `SELECT error_code, error_message FROM agent_runs WHERE id = ?`, enqueued.RunID).Scan(&runCode, &runMessage); err != nil {
		t.Fatalf("query failed run: %v", err)
	}
	if err := database.QueryRowContext(ctx, `SELECT status, error_code, error_message FROM jobs WHERE id = ?`, enqueued.JobID).Scan(&jobStatus, &jobCode, &jobMessage); err != nil {
		t.Fatalf("query failed job: %v", err)
	}
	if jobStatus != "failed" || runCode != "model_error" || jobCode != "model_error" {
		t.Fatalf("bad failure state: job_status=%q run=%q job=%q", jobStatus, runCode, jobCode)
	}
	// The normalized code is the whole diagnostic. Both message columns stay
	// NULL, because they were the channel a provider's sentence used to reach a
	// client through.
	if runMessage.Valid || jobMessage.Valid {
		t.Fatalf("failure persisted messages: run=%q job=%q", runMessage.String, jobMessage.String)
	}
}

func TestApprovalLifecycleRecordsTokenAndUpdatesRun(t *testing.T) {
	database := openTestDB(t)
	repo := New(database)
	ctx := context.Background()
	session, err := repo.CreateSession(ctx, "Approvals")
	if err != nil {
		t.Fatal(err)
	}
	enqueued, err := repo.EnqueueUserMessage(ctx, EnqueueUserMessageInput{
		SessionID: session.SessionID, Content: "needs approval", AgentID: "general_assistant", ModelProvider: "ollama", Model: "llama3.2",
	})
	if err != nil {
		t.Fatal(err)
	}
	// A run only reaches an approval by running: a queued run has no worker to
	// have requested the tool, and waiting-approval is reachable only from
	// running.
	if err := repo.MarkRunRunning(ctx, enqueued.RunID); err != nil {
		t.Fatal(err)
	}
	if err := repo.RecordToolCallBefore(ctx, ToolCallRecord{ToolCallID: "tool_1", RunID: enqueued.RunID}, "general_assistant", "mcp-files", "write_file", `{"path":"notes.txt"}`, "args_hash_1"); err != nil {
		t.Fatal(err)
	}
	approval, err := repo.CreateApproval(ctx, enqueued.RunID, "tool_1", "general_assistant", "write_file", `{"path":"notes.txt"}`, "args_hash_1", "2099-01-01T00:00:00Z")
	if err != nil {
		t.Fatal(err)
	}
	comment := sql.NullString{String: "Reviewed the exact note update", Valid: true}
	approved, err := repo.ApproveApproval(ctx, approval.ApprovalID, "approval_token_1", comment, "2026-05-15T00:00:00Z")
	if err != nil {
		t.Fatal(err)
	}
	if approved.Status != "approved" || approved.ApprovalToken != "approval_token_1" {
		t.Fatalf("bad approval record: %+v", approved)
	}
	if approved.ApprovalComment != comment || approved.DenialReason.Valid {
		t.Fatalf("approval rationale = comment %#v reason %#v", approved.ApprovalComment, approved.DenialReason)
	}
	retried, err := repo.ApproveApproval(
		ctx,
		approval.ApprovalID,
		"replacement_token",
		sql.NullString{String: "replacement comment", Valid: true},
		"2026-05-15T00:00:01Z",
	)
	if err != nil {
		t.Fatal(err)
	}
	if retried.ApprovalToken != "approval_token_1" || retried.ApprovalComment != comment {
		t.Fatalf("approval retry rewrote committed decision: %+v", retried)
	}
	var toolCallStatus, toolCallApprovalID string
	if err := database.QueryRowContext(ctx, `SELECT status, approval_id FROM tool_calls WHERE id = ?`, "tool_1").Scan(&toolCallStatus, &toolCallApprovalID); err != nil {
		t.Fatalf("query approval tool call: %v", err)
	}
	if toolCallStatus != "approval_required" || toolCallApprovalID != approval.ApprovalID {
		t.Fatalf("bad approval tool call: status=%q approval_id=%q", toolCallStatus, toolCallApprovalID)
	}
	run, err := repo.GetRun(ctx, enqueued.RunID)
	if err != nil {
		t.Fatal(err)
	}
	// The decision is recorded and its token minted, and neither of those is
	// the worker proving it can act on them. The run leaves waiting-approval
	// only when the resume commits.
	if run.Status != "waiting_approval" {
		t.Fatalf("run status = %q", run.Status)
	}
	var approvalJTI, approvalToken string
	var storedComment, storedReason sql.NullString
	if err := database.QueryRowContext(ctx, `
		SELECT approval_jti, approval_token, approval_comment, denial_reason
		FROM approvals
		WHERE id = ?
	`, approval.ApprovalID).Scan(&approvalJTI, &approvalToken, &storedComment, &storedReason); err != nil {
		t.Fatalf("query approval token fields: %v", err)
	}
	if approvalJTI != approval.ApprovalID || approvalToken != "approval_token_1" || storedComment != comment || storedReason.Valid {
		t.Fatalf(
			"bad approval fields: approval_jti=%q approval_token=%q comment=%#v reason=%#v",
			approvalJTI,
			approvalToken,
			storedComment,
			storedReason,
		)
	}
	consumed, err := repo.ConsumeApproval(ctx, approval.ApprovalID, "2026-05-15T00:01:00Z")
	if err != nil {
		t.Fatal(err)
	}
	if consumed.Status != "consumed" {
		t.Fatalf("approval status after consume = %q", consumed.Status)
	}
	if _, err := repo.ConsumeApproval(ctx, approval.ApprovalID, "2026-05-15T00:01:01Z"); !errors.Is(err, ErrApprovalAlreadyConsumed) {
		t.Fatalf("second consume error = %v, want ErrApprovalAlreadyConsumed", err)
	}
}

func TestCreateApprovalWithEventIsIdempotentUnderConcurrency(t *testing.T) {
	database := openTestDB(t)
	repo := New(database)
	ctx := context.Background()
	session, err := repo.CreateSession(ctx, "Concurrent approval creation")
	if err != nil {
		t.Fatal(err)
	}
	enqueued, err := repo.EnqueueUserMessage(ctx, EnqueueUserMessageInput{
		SessionID: session.SessionID, Content: "approval tool", AgentID: "general_assistant", ModelProvider: "ollama", Model: "llama3.2",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.MarkRunRunning(ctx, enqueued.RunID); err != nil {
		t.Fatal(err)
	}
	const toolCallID = "call_concurrent_approval"
	if err := repo.RecordToolCallBefore(ctx, ToolCallRecord{
		ToolCallID: toolCallID, RunID: enqueued.RunID,
	}, "general_assistant", "files", "files.update", `{"path":"note.txt"}`, "sha256:concurrent"); err != nil {
		t.Fatal(err)
	}

	const callers = 8
	start := make(chan struct{})
	type result struct {
		approval ApprovalRecord
		event    Event
		err      error
	}
	results := make(chan result, callers)
	for range callers {
		go func() {
			<-start
			approval, event, err := repo.CreateApprovalWithEvent(
				ctx,
				enqueued.RunID,
				toolCallID,
				"general_assistant",
				"files.update",
				`{"path":"note.txt"}`,
				"sha256:concurrent",
				"2099-01-01T00:00:00Z",
			)
			results <- result{approval: approval, event: event, err: err}
		}()
	}
	close(start)

	approvalID := ""
	createdEvents := 0
	for range callers {
		result := <-results
		if result.err != nil {
			t.Fatalf("concurrent approval creation: %v", result.err)
		}
		if approvalID == "" {
			approvalID = result.approval.ApprovalID
		} else if result.approval.ApprovalID != approvalID {
			t.Fatalf("concurrent approval IDs = %q and %q", approvalID, result.approval.ApprovalID)
		}
		if result.event.EventID != "" {
			createdEvents++
		}
	}
	if createdEvents != 1 {
		t.Fatalf("created approval events = %d, want 1", createdEvents)
	}
	var approvalCount, eventCount int
	if err := database.QueryRowContext(ctx, `SELECT COUNT(*) FROM approvals WHERE tool_call_id = ?`, toolCallID).Scan(&approvalCount); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRowContext(ctx, `SELECT COUNT(*) FROM events WHERE run_id = ? AND type = 'approval.requested'`, enqueued.RunID).Scan(&eventCount); err != nil {
		t.Fatal(err)
	}
	if approvalCount != 1 || eventCount != 1 {
		t.Fatalf("approval/event counts = %d/%d, want 1/1", approvalCount, eventCount)
	}
}

func TestRecordToolCallAfterRejectsApprovedButUnconsumedCompletion(t *testing.T) {
	database := openTestDB(t)
	repo := New(database)
	ctx := context.Background()
	session, err := repo.CreateSession(ctx, "Unconsumed approval completion")
	if err != nil {
		t.Fatal(err)
	}
	enqueued, err := repo.EnqueueUserMessage(ctx, EnqueueUserMessageInput{
		SessionID: session.SessionID, Content: "approval tool", AgentID: "general_assistant", ModelProvider: "ollama", Model: "llama3.2",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.MarkRunRunning(ctx, enqueued.RunID); err != nil {
		t.Fatal(err)
	}
	const toolCallID = "call_approved_not_consumed"
	if err := repo.RecordToolCallBefore(ctx, ToolCallRecord{
		ToolCallID: toolCallID, RunID: enqueued.RunID, Status: "approval_required",
	}, "general_assistant", "files", "files.update", `{}`, "sha256:approved"); err != nil {
		t.Fatal(err)
	}
	approval, err := repo.CreateApproval(ctx, enqueued.RunID, toolCallID, "general_assistant", "files.update", `{}`, "sha256:approved", "2099-01-01T00:00:00Z")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.ApproveApproval(ctx, approval.ApprovalID, "approved-token", sql.NullString{}, ""); err != nil {
		t.Fatal(err)
	}

	changed, _, err := repo.RecordToolCallAfterWithEvent(ctx, ToolCallAfterRecord{
		ToolCallID: toolCallID, RunID: enqueued.RunID, ServerName: "files", ToolName: "files.update", Status: "completed",
	}, "tool.call.completed", `{"toolCallId":"call_approved_not_consumed","toolName":"files.update","serverName":"files"}`)
	if !errors.Is(err, ErrToolCallInvalidTransition) {
		t.Fatalf("approved-but-unconsumed completion error = %v, want ErrToolCallInvalidTransition", err)
	}
	if changed {
		t.Fatal("approved-but-unconsumed completion changed the tool call")
	}
	var status string
	if err := database.QueryRowContext(ctx, `SELECT status FROM tool_calls WHERE id = ?`, toolCallID).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "approval_required" {
		t.Fatalf("tool call status = %q, want approval_required", status)
	}
}

func TestRecordToolCallAfterRejectsCompletionFromRequested(t *testing.T) {
	database := openTestDB(t)
	repo := New(database)
	ctx := context.Background()
	session, err := repo.CreateSession(ctx, "Requested completion")
	if err != nil {
		t.Fatal(err)
	}
	enqueued, err := repo.EnqueueUserMessage(ctx, EnqueueUserMessageInput{
		SessionID: session.SessionID, Content: "safe tool", AgentID: "general_assistant", ModelProvider: "ollama", Model: "llama3.2",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.MarkRunRunning(ctx, enqueued.RunID); err != nil {
		t.Fatal(err)
	}
	if err := repo.RecordToolCallBefore(ctx, ToolCallRecord{ToolCallID: "call_requested", RunID: enqueued.RunID, Status: "requested"}, "general_assistant", "system", "system.echo", `{"value":"hello"}`, "sha256:requested"); err != nil {
		t.Fatal(err)
	}

	changed, _, err := repo.RecordToolCallAfterWithEvent(ctx, ToolCallAfterRecord{
		ToolCallID: "call_requested",
		RunID:      enqueued.RunID,
		ServerName: "system",
		ToolName:   "system.echo",
		Status:     "completed",
	}, "tool.call.completed", `{"toolCallId":"call_requested"}`)
	if err != ErrToolCallInvalidTransition {
		t.Fatalf("RecordToolCallAfterWithEvent error = %v, want ErrToolCallInvalidTransition", err)
	}
	if changed {
		t.Fatal("RecordToolCallAfterWithEvent changed a requested call to completed")
	}
	var toolCallStatus string
	if err := database.QueryRowContext(ctx, `SELECT status FROM tool_calls WHERE id = ?`, "call_requested").Scan(&toolCallStatus); err != nil {
		t.Fatal(err)
	}
	if toolCallStatus != "requested" {
		t.Fatalf("tool call status = %q, want requested", toolCallStatus)
	}
}

func TestRecordToolCallBeforeRejectsConflictingID(t *testing.T) {
	database := openTestDB(t)
	repo := New(database)
	ctx := context.Background()
	firstSession, err := repo.CreateSession(ctx, "First")
	if err != nil {
		t.Fatal(err)
	}
	first, err := repo.EnqueueUserMessage(ctx, EnqueueUserMessageInput{
		SessionID: firstSession.SessionID, Content: "first", AgentID: "general_assistant", ModelProvider: "ollama", Model: "llama3.2",
	})
	if err != nil {
		t.Fatal(err)
	}
	secondSession, err := repo.CreateSession(ctx, "Second")
	if err != nil {
		t.Fatal(err)
	}
	second, err := repo.EnqueueUserMessage(ctx, EnqueueUserMessageInput{
		SessionID: secondSession.SessionID, Content: "second", AgentID: "general_assistant", ModelProvider: "ollama", Model: "llama3.2",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.RecordToolCallBefore(ctx, ToolCallRecord{ToolCallID: "tool_conflict", RunID: first.RunID}, "general_assistant", "files", "files.update", `{"path":"a.txt"}`, "sha256:first"); err != nil {
		t.Fatal(err)
	}
	err = repo.RecordToolCallBefore(ctx, ToolCallRecord{ToolCallID: "tool_conflict", RunID: second.RunID}, "general_assistant", "files", "files.update", `{"path":"a.txt"}`, "sha256:first")
	if err == nil {
		t.Fatal("RecordToolCallBefore allowed same tool_call_id for a different run")
	}
}

func TestApprovalFailsWithoutMatchingToolCall(t *testing.T) {
	database := openTestDB(t)
	repo := New(database)
	ctx := context.Background()
	session, err := repo.CreateSession(ctx, "Approval failure")
	if err != nil {
		t.Fatal(err)
	}
	enqueued, err := repo.EnqueueUserMessage(ctx, EnqueueUserMessageInput{
		SessionID: session.SessionID, Content: "needs approval", AgentID: "general_assistant", ModelProvider: "ollama", Model: "llama3.2",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.CreateApproval(ctx, enqueued.RunID, "missing_tool_call", "general_assistant", "write_file", `{}`, "args_hash_1", "2099-01-01T00:00:00Z"); err == nil {
		t.Fatal("expected missing tool call error")
	}
	var approvalCount int
	if err := database.QueryRowContext(ctx, `SELECT COUNT(*) FROM approvals`).Scan(&approvalCount); err != nil {
		t.Fatal(err)
	}
	if approvalCount != 0 {
		t.Fatalf("approval count = %d, want 0", approvalCount)
	}
}

func TestDenyApprovalDoesNotMutateNonWaitingRun(t *testing.T) {
	database := openTestDB(t)
	repo := New(database)
	ctx := context.Background()
	session, err := repo.CreateSession(ctx, "Deny invalid")
	if err != nil {
		t.Fatal(err)
	}

	enqueued, err := repo.EnqueueUserMessage(ctx, EnqueueUserMessageInput{
		SessionID: session.SessionID, Content: "needs approval", AgentID: "general_assistant", ModelProvider: "ollama", Model: "llama3.2",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.MarkRunRunning(ctx, enqueued.RunID); err != nil {
		t.Fatal(err)
	}
	approval, err := repo.CreateApproval(ctx, enqueued.RunID, "", "general_assistant", "write_file", `{}`, "args_hash_1", "2099-01-01T00:00:00Z")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.ExecContext(ctx, `UPDATE agent_runs SET status = 'completed' WHERE id = ?`, enqueued.RunID); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.DenyApproval(ctx, approval.ApprovalID, sql.NullString{}, "2026-05-15T00:00:00Z"); err == nil {
		t.Fatal("expected deny approval to fail for completed run")
	}
	run, err := repo.GetRun(ctx, enqueued.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if run.Status != "completed" {
		t.Fatalf("run status = %q, want completed", run.Status)
	}
	var approvalStatus string
	if err := database.QueryRowContext(ctx, `SELECT status FROM approvals WHERE id = ?`, approval.ApprovalID).Scan(&approvalStatus); err != nil {
		t.Fatal(err)
	}
	if approvalStatus != "pending" {
		t.Fatalf("approval status = %q, want pending", approvalStatus)
	}
}

func TestDenyApprovalAtomicallyTerminalizesToolRunJobAndEvent(t *testing.T) {
	database := openTestDB(t)
	repo := New(database)
	ctx := context.Background()
	session, err := repo.CreateSession(ctx, "Deny terminalization")
	if err != nil {
		t.Fatal(err)
	}
	enqueued, err := repo.EnqueueUserMessage(ctx, EnqueueUserMessageInput{
		SessionID: session.SessionID, Content: "needs approval", AgentID: "general_assistant", ModelProvider: "ollama", Model: "llama3.2",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.MarkRunRunning(ctx, enqueued.RunID); err != nil {
		t.Fatal(err)
	}
	if err := repo.RecordToolCallBefore(ctx, ToolCallRecord{ToolCallID: "call_denied", RunID: enqueued.RunID}, "general_assistant", "files", "files.update", `{"path":"note.txt"}`, "sha256:args"); err != nil {
		t.Fatal(err)
	}
	approval, err := repo.CreateApproval(ctx, enqueued.RunID, "call_denied", "general_assistant", "files.update", `{"path":"note.txt"}`, "sha256:args", "2099-01-01T00:00:00Z")
	if err != nil {
		t.Fatal(err)
	}

	reason := sql.NullString{String: "The destination is not the one I intended", Valid: true}
	if _, err := repo.DenyApproval(ctx, approval.ApprovalID, reason, "2026-05-15T00:00:00Z"); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.DenyApproval(
		ctx,
		approval.ApprovalID,
		sql.NullString{String: "replacement reason", Valid: true},
		"2026-05-15T00:00:01Z",
	); err != nil {
		t.Fatalf("repeated denial: %v", err)
	}

	var approvalStatus, toolCallStatus, runStatus, jobStatus string
	var storedComment, storedReason sql.NullString
	if err := database.QueryRowContext(ctx, `
		SELECT status, approval_comment, denial_reason
		FROM approvals
		WHERE id = ?
	`, approval.ApprovalID).Scan(&approvalStatus, &storedComment, &storedReason); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRowContext(ctx, `SELECT status FROM tool_calls WHERE id = 'call_denied'`).Scan(&toolCallStatus); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRowContext(ctx, `SELECT status FROM agent_runs WHERE id = ?`, enqueued.RunID).Scan(&runStatus); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRowContext(ctx, `SELECT status FROM jobs WHERE id = ?`, enqueued.JobID).Scan(&jobStatus); err != nil {
		t.Fatal(err)
	}
	if approvalStatus != "denied" || storedComment.Valid || storedReason != reason ||
		toolCallStatus != "denied" || runStatus != "failed" || jobStatus != "failed" {
		t.Fatalf(
			"terminal state approval=%q comment=%#v reason=%#v tool=%q run=%q job=%q",
			approvalStatus,
			storedComment,
			storedReason,
			toolCallStatus,
			runStatus,
			jobStatus,
		)
	}
	var terminalEvents int
	if err := database.QueryRowContext(ctx, `SELECT COUNT(*) FROM events WHERE run_id = ? AND type = 'agent.run.failed'`, enqueued.RunID).Scan(&terminalEvents); err != nil {
		t.Fatal(err)
	}
	if terminalEvents != 1 {
		t.Fatalf("agent.run.failed events = %d, want 1", terminalEvents)
	}
}

func TestDenyApprovalTerminalizesRunWhenToolCallAlreadyFailed(t *testing.T) {
	database := openTestDB(t)
	repo := New(database)
	ctx := context.Background()
	session, err := repo.CreateSession(ctx, "Deny after tool failure")
	if err != nil {
		t.Fatal(err)
	}
	enqueued, err := repo.EnqueueUserMessage(ctx, EnqueueUserMessageInput{
		SessionID: session.SessionID, Content: "needs approval", AgentID: "general_assistant", ModelProvider: "ollama", Model: "llama3.2",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.MarkRunRunning(ctx, enqueued.RunID); err != nil {
		t.Fatal(err)
	}
	if err := repo.RecordToolCallBefore(ctx, ToolCallRecord{ToolCallID: "call_already_failed", RunID: enqueued.RunID}, "general_assistant", "files", "files.update", `{"path":"note.txt"}`, "sha256:args"); err != nil {
		t.Fatal(err)
	}
	approval, err := repo.CreateApproval(ctx, enqueued.RunID, "call_already_failed", "general_assistant", "files.update", `{"path":"note.txt"}`, "sha256:args", "2099-01-01T00:00:00Z")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.RecordToolCallAfter(ctx, ToolCallAfterRecord{
		ToolCallID: "call_already_failed", RunID: enqueued.RunID, ServerName: "files", ToolName: "files.update",
		Status: "failed", ErrorCode: "approval_wait_failed", ErrorMessage: "approval polling timed out",
	}); err != nil {
		t.Fatal(err)
	}

	if _, err := repo.DenyApproval(ctx, approval.ApprovalID, sql.NullString{}, "2026-05-15T00:00:00Z"); err != nil {
		t.Fatalf("DenyApproval after failed tool call: %v", err)
	}
	var approvalStatus, toolCallStatus, runStatus, jobStatus string
	if err := database.QueryRowContext(ctx, `SELECT status FROM approvals WHERE id = ?`, approval.ApprovalID).Scan(&approvalStatus); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRowContext(ctx, `SELECT status FROM tool_calls WHERE id = 'call_already_failed'`).Scan(&toolCallStatus); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRowContext(ctx, `SELECT status FROM agent_runs WHERE id = ?`, enqueued.RunID).Scan(&runStatus); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRowContext(ctx, `SELECT status FROM jobs WHERE id = ?`, enqueued.JobID).Scan(&jobStatus); err != nil {
		t.Fatal(err)
	}
	if approvalStatus != "denied" || toolCallStatus != "failed" || runStatus != "failed" || jobStatus != "failed" {
		t.Fatalf("terminal states approval=%q tool=%q run=%q job=%q", approvalStatus, toolCallStatus, runStatus, jobStatus)
	}
}

func TestRuntimeFailureTerminalizesPendingApprovalBeforeLateResolution(t *testing.T) {
	tests := []struct {
		name        string
		expectError bool
		terminalize func(*Repository, context.Context, string) (ApprovalTerminalization, error)
	}{
		{
			name:        "conflicting denial",
			expectError: true,
			terminalize: func(repo *Repository, ctx context.Context, approvalID string) (ApprovalTerminalization, error) {
				return repo.DenyApprovalWithEvent(ctx, approvalID, sql.NullString{}, "2026-05-15T00:00:00Z")
			},
		},
		{
			name: "idempotent expiry",
			terminalize: func(repo *Repository, ctx context.Context, approvalID string) (ApprovalTerminalization, error) {
				return repo.ExpireApprovalWithEvent(ctx, approvalID, "2026-05-15T00:00:00Z")
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			database := openTestDB(t)
			repo := New(database)
			ctx := context.Background()
			session, err := repo.CreateSession(ctx, "Late approval terminalization")
			if err != nil {
				t.Fatal(err)
			}
			enqueued, err := repo.EnqueueUserMessage(ctx, EnqueueUserMessageInput{
				SessionID: session.SessionID, Content: "needs approval", AgentID: "general_assistant", ModelProvider: "ollama", Model: "llama3.2",
			})
			if err != nil {
				t.Fatal(err)
			}
			if err := repo.MarkRunRunning(ctx, enqueued.RunID); err != nil {
				t.Fatal(err)
			}
			const toolCallID = "call_late_terminal"
			if err := repo.RecordToolCallBefore(ctx, ToolCallRecord{ToolCallID: toolCallID, RunID: enqueued.RunID}, "general_assistant", "files", "files.update", `{"path":"note.txt"}`, "sha256:args"); err != nil {
				t.Fatal(err)
			}
			approval, err := repo.CreateApproval(ctx, enqueued.RunID, toolCallID, "general_assistant", "files.update", `{"path":"note.txt"}`, "sha256:args", "2099-01-01T00:00:00Z")
			if err != nil {
				t.Fatal(err)
			}
			if _, _, err := repo.RecordToolCallAfterWithEvent(ctx, ToolCallAfterRecord{
				ToolCallID: toolCallID, RunID: enqueued.RunID, ServerName: "files", ToolName: "files.update",
				Status: "failed", ErrorCode: "approval_wait_failed", ErrorMessage: "approval transport failed",
			}, "tool.call.failed", `{"code":"approval_wait_failed"}`); err != nil {
				t.Fatal(err)
			}
			if _, err := failRunAtCurrentVersion(t, repo, enqueued.RunID, testFailure("runtime_error")); err != nil {
				t.Fatal(err)
			}

			transition, err := test.terminalize(repo, ctx, approval.ApprovalID)
			if test.expectError {
				if err == nil {
					t.Fatal("conflicting late resolution succeeded")
				}
			} else if err != nil {
				t.Fatalf("terminalize late approval: %v", err)
			}
			if !test.expectError && (transition.Changed || transition.Approval.Status != "expired") {
				t.Fatalf("late terminalization = %+v, want idempotent expired approval", transition)
			}
			if !test.expectError && transition.RunFailedEvent.EventID != "" {
				t.Fatalf("late terminalization appended duplicate run failure event: %+v", transition.RunFailedEvent)
			}
			if !test.expectError {
				repeated, err := test.terminalize(repo, ctx, approval.ApprovalID)
				if err != nil {
					t.Fatalf("repeated late terminalization: %v", err)
				}
				if repeated.Changed {
					t.Fatalf("repeated late terminalization changed state: %+v", repeated)
				}
			}

			var approvalStatus, toolCallStatus, runStatus, jobStatus string
			if err := database.QueryRowContext(ctx, `SELECT status FROM approvals WHERE id = ?`, approval.ApprovalID).Scan(&approvalStatus); err != nil {
				t.Fatal(err)
			}
			if err := database.QueryRowContext(ctx, `SELECT status FROM tool_calls WHERE id = ?`, toolCallID).Scan(&toolCallStatus); err != nil {
				t.Fatal(err)
			}
			if err := database.QueryRowContext(ctx, `SELECT status FROM agent_runs WHERE id = ?`, enqueued.RunID).Scan(&runStatus); err != nil {
				t.Fatal(err)
			}
			if err := database.QueryRowContext(ctx, `SELECT status FROM jobs WHERE id = ?`, enqueued.JobID).Scan(&jobStatus); err != nil {
				t.Fatal(err)
			}
			if approvalStatus != "expired" || toolCallStatus != "failed" || runStatus != "failed" || jobStatus != "failed" {
				t.Fatalf("terminal states approval=%q tool=%q run=%q job=%q", approvalStatus, toolCallStatus, runStatus, jobStatus)
			}
			var runFailures int
			if err := database.QueryRowContext(ctx, `SELECT COUNT(*) FROM events WHERE run_id = ? AND type = 'agent.run.failed'`, enqueued.RunID).Scan(&runFailures); err != nil {
				t.Fatal(err)
			}
			if runFailures != 1 {
				t.Fatalf("agent.run.failed events = %d, want 1", runFailures)
			}
		})
	}
}

// -----------------------------------------------------------------------------
// A corrupt active run must block its session rather than be stepped over
//
// Same-session claim ordering used to be decided by joining each run to its
// assistant message and comparing sequences. That join silently drops a run
// whose assistant link is missing, so a run that is still active on paper stops
// counting as an earlier blocker and the next turn in the same conversation is
// dispatched around it. Combined with a correlation gate that refuses to
// transition that same run, the result is the worst pair available: the stuck
// run can never be cleared, and the session keeps moving without it.
// -----------------------------------------------------------------------------

// corruptActiveSameSessionRuns enqueues two runs in one session, claims the
// first, and then removes both directions of the first run's assistant link.
// The second run is untouched: its link is intact, so nothing about it explains
// a claim it should not get.
func corruptActiveSameSessionRuns(t *testing.T) (*Repository, EnqueueUserMessageResult, EnqueueUserMessageResult) {
	t.Helper()
	ctx := context.Background()
	repo := New(openTestDB(t))
	session, err := repo.CreateSession(ctx, "Corrupt active run")
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	first, err := repo.EnqueueUserMessage(ctx, EnqueueUserMessageInput{
		SessionID: session.SessionID, Content: "first", AgentID: "general_assistant",
		ModelProvider: "ollama", Model: "llama3.2",
	})
	if err != nil {
		t.Fatalf("enqueue first: %v", err)
	}
	second, err := repo.EnqueueUserMessage(ctx, EnqueueUserMessageInput{
		SessionID: session.SessionID, Content: "second", AgentID: "general_assistant",
		ModelProvider: "ollama", Model: "llama3.2",
	})
	if err != nil {
		t.Fatalf("enqueue second: %v", err)
	}
	claimed, err := repo.ClaimNextJob(ctx, "general_assistant", "worker-corrupt")
	if err != nil {
		t.Fatalf("ClaimNextJob: %v", err)
	}
	if claimed.RunID != first.RunID {
		t.Fatalf("first claim run = %q, want %q", claimed.RunID, first.RunID)
	}
	breakAssistantLinkBothDirections(t, repo, first.RunID)
	return repo, first, second
}

func TestCorruptActiveRunCannotBeTransitionedOrLeapfrogged(t *testing.T) {
	t.Run("cancelling the corrupt run is refused without writing", func(t *testing.T) {
		ctx := context.Background()
		repo, first, _ := corruptActiveSameSessionRuns(t)
		before := snapshotRunWrites(t, repo, first.RunID)

		// No assistant message is named, so the terminal identity guard cannot
		// be what refuses this: only the correlation gate can.
		_, err := repo.CancelRunCanonical(ctx, CancelRunInput{
			RunID:                first.RunID,
			ExpectedStateVersion: before.state.StateVersion,
			Cancellation:         runoutcome.AbandonedCancellation(),
		})
		if !errors.Is(err, runcorrelation.ErrConflict) {
			t.Fatalf("cancel of a corrupt active run = %v, want runcorrelation.ErrConflict", err)
		}
		if got := err.Error(); got != "run/message correlation conflict" {
			t.Fatalf("cancel error = %q, want exactly the value-free correlation sentinel", got)
		}
		after := snapshotRunWrites(t, repo, first.RunID)
		if after.state != before.state || !reflect.DeepEqual(after.jobs, before.jobs) || after.events != before.events {
			t.Fatalf("refused cancel wrote something: %+v, want %+v", after, before)
		}
	})

	t.Run("later same-session work does not leapfrog it", func(t *testing.T) {
		ctx := context.Background()
		repo, _, second := corruptActiveSameSessionRuns(t)

		claimed, err := repo.ClaimNextJob(ctx, "general_assistant", "worker-later")
		if err != nil {
			t.Fatalf("ClaimNextJob: %v", err)
		}
		if claimed.JobID != "" {
			t.Fatalf("claimed %+v while an earlier active run in the same session was unprogressable", claimed)
		}
		var jobStatus string
		if err := repo.db.QueryRowContext(ctx,
			`SELECT status FROM jobs WHERE run_id = ?`, second.RunID).Scan(&jobStatus); err != nil {
			t.Fatalf("read second job: %v", err)
		}
		if jobStatus != "pending" {
			t.Fatalf("second job status = %q, want pending", jobStatus)
		}
		state, err := repo.GetRunState(ctx, second.RunID)
		if err != nil {
			t.Fatalf("GetRunState: %v", err)
		}
		if state.Lifecycle != lifecycleQueued || state.StateVersion != 1 {
			t.Fatalf("second run = %s at version %d, want queued at version 1", state.Lifecycle, state.StateVersion)
		}
		var started int
		if err := repo.db.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM events WHERE run_id = ? AND type = 'agent.run.started'`,
			second.RunID).Scan(&started); err != nil {
			t.Fatalf("count started events: %v", err)
		}
		if started != 0 {
			t.Fatalf("second run has %d started events, want none", started)
		}
	})
}
