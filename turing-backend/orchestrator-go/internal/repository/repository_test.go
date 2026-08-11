package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"path/filepath"
	"testing"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/db"
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
	var payload map[string]string
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
	if latest != 1 || len(replayed) != 1 {
		t.Fatalf("queued event replay latest=%d events=%+v", latest, replayed)
	}
	if replayed[0].EventID != result.QueuedEvent.EventID || replayed[0].Type != "agent.run.queued" {
		t.Fatalf("bad queued replay event: %+v", replayed[0])
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
	if err := repo.CancelRun(ctx, enqueued.RunID, "client_cancelled"); err != nil {
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
	_, err = repo.CancelRunWithEvent(ctx, enqueued.RunID, "client_cancelled", `{"reason":"client_cancelled"}`)
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
	if err := repo.CancelRun(ctx, enqueued.RunID, "client_cancelled"); err == nil {
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

	if err := repo.CompleteRun(ctx, first.RunID, first.AssistantMessageID, "first done"); err != nil {
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
	if err := repo.CompleteRun(ctx, enqueued.RunID, enqueued.AssistantMessageID, "done"); err != nil {
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
	_, err = repo.CompleteRunWithEvent(ctx, enqueued.RunID, enqueued.AssistantMessageID, "done", `{"assistantMessageId":"`+enqueued.AssistantMessageID+`"}`)
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
	completedEvents, err := repo.CompleteRunWithEvent(ctx, enqueued.RunID, enqueued.AssistantMessageID, "done", `{"assistantMessageId":"`+enqueued.AssistantMessageID+`"}`)
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

	completedEvents, err := repo.CompleteRunWithEvent(ctx, enqueued.RunID, enqueued.AssistantMessageID, "authoritative", `{"assistantMessageId":"`+enqueued.AssistantMessageID+`"}`)
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
	if _, err := repo.CancelRunWithEvent(ctx, enqueued.RunID, "client_cancelled", `{"reason":"client_cancelled"}`); err != nil {
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
	if err := repo.FailRun(ctx, enqueued.RunID, "model_error", "model failed"); err != nil {
		t.Fatal(err)
	}
	run, err := repo.GetRun(ctx, enqueued.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if run.Status != "failed" {
		t.Fatalf("run status = %q, want failed", run.Status)
	}
	var jobStatus, runCode, runMessage, jobCode, jobMessage string
	if err := database.QueryRowContext(ctx, `SELECT error_code, error_message FROM agent_runs WHERE id = ?`, enqueued.RunID).Scan(&runCode, &runMessage); err != nil {
		t.Fatalf("query failed run: %v", err)
	}
	if err := database.QueryRowContext(ctx, `SELECT status, error_code, error_message FROM jobs WHERE id = ?`, enqueued.JobID).Scan(&jobStatus, &jobCode, &jobMessage); err != nil {
		t.Fatalf("query failed job: %v", err)
	}
	if jobStatus != "failed" || runCode != "model_error" || runMessage != "model failed" || jobCode != "model_error" || jobMessage != "model failed" {
		t.Fatalf("bad failure state: job_status=%q run=%q/%q job=%q/%q", jobStatus, runCode, runMessage, jobCode, jobMessage)
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
	if err := repo.RecordToolCallBefore(ctx, ToolCallRecord{ToolCallID: "tool_1", RunID: enqueued.RunID}, "general_assistant", "mcp-files", "write_file", `{"path":"notes.txt"}`, "args_hash_1"); err != nil {
		t.Fatal(err)
	}
	approval, err := repo.CreateApproval(ctx, enqueued.RunID, "tool_1", "general_assistant", "write_file", `{"path":"notes.txt"}`, "args_hash_1", "2099-01-01T00:00:00Z")
	if err != nil {
		t.Fatal(err)
	}
	approved, err := repo.ApproveApproval(ctx, approval.ApprovalID, "approval_token_1", "2026-05-15T00:00:00Z")
	if err != nil {
		t.Fatal(err)
	}
	if approved.Status != "approved" || approved.ApprovalToken != "approval_token_1" {
		t.Fatalf("bad approval record: %+v", approved)
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
	if run.Status != "running" {
		t.Fatalf("run status = %q", run.Status)
	}
	var approvalJTI, approvalToken string
	if err := database.QueryRowContext(ctx, `SELECT approval_jti, approval_token FROM approvals WHERE id = ?`, approval.ApprovalID).Scan(&approvalJTI, &approvalToken); err != nil {
		t.Fatalf("query approval token fields: %v", err)
	}
	if approvalJTI != approval.ApprovalID || approvalToken != "approval_token_1" {
		t.Fatalf("bad token fields: approval_jti=%q approval_token=%q", approvalJTI, approvalToken)
	}
	consumed, err := repo.ConsumeApproval(ctx, approval.ApprovalID, "2026-05-15T00:01:00Z")
	if err != nil {
		t.Fatal(err)
	}
	if consumed.Status != "consumed" {
		t.Fatalf("approval status after consume = %q", consumed.Status)
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
	approval, err := repo.CreateApproval(ctx, enqueued.RunID, "", "general_assistant", "write_file", `{}`, "args_hash_1", "2099-01-01T00:00:00Z")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.ExecContext(ctx, `UPDATE agent_runs SET status = 'completed' WHERE id = ?`, enqueued.RunID); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.DenyApproval(ctx, approval.ApprovalID, "2026-05-15T00:00:00Z"); err == nil {
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

	if _, err := repo.DenyApproval(ctx, approval.ApprovalID, "2026-05-15T00:00:00Z"); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.DenyApproval(ctx, approval.ApprovalID, "2026-05-15T00:00:00Z"); err != nil {
		t.Fatalf("repeated denial: %v", err)
	}

	var approvalStatus, toolCallStatus, runStatus, jobStatus string
	if err := database.QueryRowContext(ctx, `SELECT status FROM approvals WHERE id = ?`, approval.ApprovalID).Scan(&approvalStatus); err != nil {
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
	if approvalStatus != "denied" || toolCallStatus != "denied" || runStatus != "failed" || jobStatus != "failed" {
		t.Fatalf("terminal states approval=%q tool=%q run=%q job=%q", approvalStatus, toolCallStatus, runStatus, jobStatus)
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

	if _, err := repo.DenyApproval(ctx, approval.ApprovalID, "2026-05-15T00:00:00Z"); err != nil {
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
				return repo.DenyApprovalWithEvent(ctx, approvalID, "2026-05-15T00:00:00Z")
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
			if _, err := repo.FailRunWithEvent(ctx, enqueued.RunID, "runtime_error", "approval transport failed", `{"code":"runtime_error"}`); err != nil {
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
