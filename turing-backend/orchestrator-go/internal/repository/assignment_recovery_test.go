package repository

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"
	"time"
)

func TestRecoverStaleUncertainAssignmentFencesAndRequeuesAtInjectedCutoff(t *testing.T) {
	database := openTestDB(t)
	repo := New(database)
	ctx := context.Background()
	session, err := repo.CreateSession(ctx, "Stale assignment")
	if err != nil {
		t.Fatal(err)
	}
	enqueued, err := repo.EnqueueUserMessage(ctx, EnqueueUserMessageInput{
		SessionID: session.SessionID, Content: "recover stale attempt", AgentID: "general_assistant", ModelProvider: "ollama", Model: "llama3.2",
	})
	if err != nil {
		t.Fatal(err)
	}
	claimed, err := repo.ClaimNextJob(ctx, "general_assistant", "worker-lost")
	if err != nil {
		t.Fatal(err)
	}
	assignment := Assignment{JobID: claimed.JobID, RunID: claimed.RunID, WorkerID: "worker-lost", AttemptID: claimed.AssignmentAttemptID}
	if err := repo.BeginAssignmentSend(ctx, assignment); err != nil {
		t.Fatal(err)
	}
	if err := repo.MarkAssignmentDeliveryUncertain(ctx, assignment); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.ReconcileAssignment(ctx, assignment); err != nil {
		t.Fatal(err)
	}
	run, err := repo.GetRun(ctx, enqueued.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if run.Status != "running" || !run.ExecutionActive {
		t.Fatalf("ambiguous attempt was released before recovery cutoff: %+v", run)
	}

	cutoff := time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)
	expiredLease := cutoff.Add(-time.Second)
	if _, err := database.ExecContext(ctx, `UPDATE agent_runs SET execution_lease_expires_at = ?, execution_lease_expires_at_ns = ? WHERE id = ?`, FormatTimestamp(expiredLease), expiredLease.UnixNano(), enqueued.RunID); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.RecoverStaleAssignments(ctx, cutoff); err != nil {
		t.Fatal(err)
	}
	run, err = repo.GetRun(ctx, enqueued.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if run.Status != "queued" || run.ExecutionActive {
		t.Fatalf("stale recovery run = %+v, want queued inactive", run)
	}
	recovered, err := repo.ClaimNextJob(ctx, "general_assistant", "worker-fresh")
	if err != nil {
		t.Fatal(err)
	}
	if recovered.RunID != enqueued.RunID || recovered.Attempt != 2 {
		t.Fatalf("recovered assignment = %+v, want second attempt for %q", recovered, enqueued.RunID)
	}
}

func TestPendingSendRecoveryDoesNotConsumeExecutionAttempt(t *testing.T) {
	for _, testCase := range []struct {
		name      string
		reconcile func(*Repository, context.Context, Assignment) (AssignmentReconciliation, error)
	}{
		{
			name: "connected stream teardown",
			reconcile: func(repo *Repository, ctx context.Context, assignment Assignment) (AssignmentReconciliation, error) {
				return repo.ReconcileAssignmentWithLimit(ctx, assignment, 1)
			},
		},
		{
			name: "stale lease recovery",
			reconcile: func(repo *Repository, ctx context.Context, assignment Assignment) (AssignmentReconciliation, error) {
				return repo.RecoverAssignmentWithLimit(ctx, assignment, 1)
			},
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			database := openTestDB(t)
			repo := New(database)
			ctx := context.Background()
			session, err := repo.CreateSession(ctx, testCase.name)
			if err != nil {
				t.Fatal(err)
			}
			enqueued, err := repo.EnqueueUserMessage(ctx, EnqueueUserMessageInput{
				SessionID: session.SessionID, Content: "not delivered", AgentID: "general_assistant",
				ModelProvider: "ollama", Model: "llama3.2",
			})
			if err != nil {
				t.Fatal(err)
			}
			claimed, err := repo.ClaimNextJob(ctx, "general_assistant", "worker-pending-send")
			if err != nil {
				t.Fatal(err)
			}
			reconciliation, err := testCase.reconcile(repo, ctx, Assignment{
				JobID: claimed.JobID, RunID: claimed.RunID,
				WorkerID: "worker-pending-send", AttemptID: claimed.AssignmentAttemptID,
			})
			if err != nil {
				t.Fatal(err)
			}
			if !reconciliation.Requeued || reconciliation.Cleared || len(reconciliation.Events) != 0 {
				t.Fatalf("pending-send reconciliation = %+v, want silent requeue", reconciliation)
			}
			var status string
			var attempt int
			if err := database.QueryRowContext(ctx,
				`SELECT status, attempt FROM jobs WHERE id = ?`, enqueued.JobID,
			).Scan(&status, &attempt); err != nil {
				t.Fatal(err)
			}
			if status != "pending" || attempt != 1 {
				t.Fatalf("pending-send job = status %q attempt %d, want pending attempt 1", status, attempt)
			}
		})
	}
}

func TestRecoverAssignmentAtCutoffDoesNotRequeueRenewedLease(t *testing.T) {
	database := openTestDB(t)
	repo := New(database)
	ctx := context.Background()
	session, err := repo.CreateSession(ctx, "Renewed recovery candidate")
	if err != nil {
		t.Fatal(err)
	}
	enqueued, err := repo.EnqueueUserMessage(ctx, EnqueueUserMessageInput{
		SessionID: session.SessionID, Content: "keep renewed attempt", AgentID: "general_assistant",
		ModelProvider: "ollama", Model: "llama3.2",
	})
	if err != nil {
		t.Fatal(err)
	}
	claimed, err := repo.ClaimNextJob(ctx, "general_assistant", "worker-renewed")
	if err != nil {
		t.Fatal(err)
	}
	assignment := Assignment{
		JobID: claimed.JobID, RunID: claimed.RunID, WorkerID: "worker-renewed", AttemptID: claimed.AssignmentAttemptID,
	}
	if err := repo.BeginAssignmentSend(ctx, assignment); err != nil {
		t.Fatal(err)
	}
	if err := repo.MarkAssignmentDelivered(ctx, assignment); err != nil {
		t.Fatal(err)
	}
	cutoff := time.Now().UTC()
	expired := cutoff.Add(-time.Second)
	if _, err := database.ExecContext(ctx, `
		UPDATE agent_runs
		SET execution_lease_expires_at = ?, execution_lease_expires_at_ns = ?
		WHERE id = ?
	`, FormatTimestamp(expired), expired.UnixNano(), enqueued.RunID); err != nil {
		t.Fatal(err)
	}
	candidates, err := repo.RecoverableAssignments(ctx, cutoff)
	if err != nil {
		t.Fatal(err)
	}
	if len(candidates) != 1 || candidates[0] != assignment {
		t.Fatalf("recovery candidates = %+v, want %+v", candidates, assignment)
	}
	if renewed, err := repo.RenewAssignments(ctx, []Assignment{assignment}, cutoff.Add(time.Minute)); err != nil {
		t.Fatal(err)
	} else if len(renewed) != 1 {
		t.Fatalf("renewed assignments = %+v, want one", renewed)
	}

	reconciliation, err := repo.RecoverAssignmentAtCutoff(ctx, assignment, cutoff)
	if err != nil {
		t.Fatal(err)
	}
	if reconciliation.Requeued || reconciliation.Cleared {
		t.Fatalf("renewed assignment reconciliation = %+v, want no mutation", reconciliation)
	}
	run, err := repo.GetRun(ctx, enqueued.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if run.Status != "running" || !run.ExecutionActive || run.ExecutionAttemptID != assignment.AttemptID {
		t.Fatalf("renewed assignment was recovered: %+v", run)
	}
}

func TestRecoverAssignmentAtCutoffFailsAtConfiguredMaximumAttempt(t *testing.T) {
	database := openTestDB(t)
	repo := New(database)
	ctx := context.Background()
	session, err := repo.CreateSession(ctx, "Exhausted assignment")
	if err != nil {
		t.Fatal(err)
	}
	enqueued, err := repo.EnqueueUserMessage(ctx, EnqueueUserMessageInput{
		SessionID: session.SessionID, Content: "exhaust retries", AgentID: "general_assistant",
		ModelProvider: "ollama", Model: "llama3.2",
	})
	if err != nil {
		t.Fatal(err)
	}
	claimed, err := repo.ClaimNextJob(ctx, "general_assistant", "worker-expired")
	if err != nil {
		t.Fatal(err)
	}
	assignment := Assignment{
		JobID: claimed.JobID, RunID: claimed.RunID, WorkerID: "worker-expired", AttemptID: claimed.AssignmentAttemptID,
	}
	if err := repo.BeginAssignmentSend(ctx, assignment); err != nil {
		t.Fatal(err)
	}
	if err := repo.MarkAssignmentDelivered(ctx, assignment); err != nil {
		t.Fatal(err)
	}
	cutoff := time.Now().UTC()
	expired := cutoff.Add(-time.Second)
	if _, err := database.ExecContext(ctx, `
		UPDATE agent_runs
		SET execution_lease_expires_at = ?, execution_lease_expires_at_ns = ?
		WHERE id = ?
	`, FormatTimestamp(expired), expired.UnixNano(), enqueued.RunID); err != nil {
		t.Fatal(err)
	}

	reconciliation, err := repo.RecoverAssignmentAtCutoffWithLimit(ctx, assignment, cutoff, 1)
	if err != nil {
		t.Fatal(err)
	}
	if reconciliation.Requeued || !reconciliation.Cleared {
		t.Fatalf("exhausted reconciliation = %+v, want terminal cleanup", reconciliation)
	}
	var eventTypes []string
	for _, event := range reconciliation.Events {
		eventTypes = append(eventTypes, event.Type)
	}
	// The notice must precede the terminal event it explains: the client's
	// agent.run.failed card communicates the terminal failure and its reason,
	// not that retries were attempted and exhausted, so a user who keeps
	// losing their worker still needs this notice's attempt count.
	if want := []string{"agent.run.step", "agent.run.failed"}; !reflect.DeepEqual(eventTypes, want) {
		t.Fatalf("exhausted recovery events = %v, want %v", eventTypes, want)
	}
	if got := runStepNote(t, reconciliation.Events[0]); got != "Gave up after 1 attempt" {
		t.Fatalf("exhausted recovery note = %q, want %q", got, "Gave up after 1 attempt")
	}
	run, err := repo.GetRun(ctx, enqueued.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if run.Status != "failed" || run.ExecutionActive {
		t.Fatalf("exhausted recovery run = %+v, want failed inactive", run)
	}
	var jobStatus, jobErrorCode, runErrorCode string
	var attempt int
	if err := database.QueryRowContext(ctx, `
		SELECT j.status, j.attempt, j.error_code, r.error_code
		FROM jobs j
		JOIN agent_runs r ON r.id = j.run_id
		WHERE j.id = ?
	`, claimed.JobID).Scan(&jobStatus, &attempt, &jobErrorCode, &runErrorCode); err != nil {
		t.Fatal(err)
	}
	if jobStatus != "failed" || attempt != 1 || jobErrorCode != "job_timeout" || runErrorCode != "job_timeout" {
		t.Fatalf("exhausted recovery job = status:%q attempt:%d job code:%q run code:%q", jobStatus, attempt, jobErrorCode, runErrorCode)
	}
}

func TestReconcileWaitingApprovalReturnsExactLifecycleEventsInOrder(t *testing.T) {
	database := openTestDB(t)
	repo := New(database)
	ctx := context.Background()
	session, err := repo.CreateSession(ctx, "Waiting approval recovery")
	if err != nil {
		t.Fatal(err)
	}
	enqueued, err := repo.EnqueueUserMessage(ctx, EnqueueUserMessageInput{
		SessionID: session.SessionID, Content: "recover approval", AgentID: "general_assistant",
		ModelProvider: "ollama", Model: "llama3.2",
	})
	if err != nil {
		t.Fatal(err)
	}
	claimed, err := repo.ClaimNextJob(ctx, "general_assistant", "worker-lost")
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.RecordToolCallBefore(ctx, ToolCallRecord{
		ToolCallID: "call_recovery", RunID: enqueued.RunID, ModelToolCallID: "model_recovery",
		Status: "approval_required",
	}, "general_assistant", "files", "files.update", `{"path":"note.txt"}`, "sha256:test"); err != nil {
		t.Fatal(err)
	}
	approval, err := repo.CreateApproval(ctx, enqueued.RunID, "call_recovery", "general_assistant", "files.update", `{"path":"note.txt"}`, "sha256:test", "2099-01-01T00:00:00Z")
	if err != nil {
		t.Fatal(err)
	}

	reconciliation, err := repo.ReconcileAssignment(ctx, Assignment{
		JobID: claimed.JobID, RunID: claimed.RunID, WorkerID: "worker-lost", AttemptID: claimed.AssignmentAttemptID,
	})
	if err != nil {
		t.Fatal(err)
	}
	var eventTypes []string
	for _, event := range reconciliation.Events {
		eventTypes = append(eventTypes, event.Type)
	}
	if want := []string{"approval.denied", "tool.call.failed", "agent.run.failed"}; !reflect.DeepEqual(eventTypes, want) {
		t.Fatalf("reconciliation events = %v, want %v", eventTypes, want)
	}
	var approvalPayload map[string]any
	if err := json.Unmarshal([]byte(reconciliation.Events[0].PayloadJSON), &approvalPayload); err != nil {
		t.Fatal(err)
	}
	wantApprovalPayload := map[string]any{
		"approvalId": approval.ApprovalID, "toolCallId": "call_recovery", "toolName": "files.update",
		"runId": enqueued.RunID, "traceId": enqueued.TraceID, "modelToolCallId": "model_recovery",
	}
	if !reflect.DeepEqual(approvalPayload, wantApprovalPayload) {
		t.Fatalf("approval payload = %#v, want %#v", approvalPayload, wantApprovalPayload)
	}
	var toolPayload map[string]any
	if err := json.Unmarshal([]byte(reconciliation.Events[1].PayloadJSON), &toolPayload); err != nil {
		t.Fatal(err)
	}
	wantToolPayload := map[string]any{
		"toolCallId": "call_recovery", "toolName": "files.update", "serverName": "files",
		"error": "Worker disconnected while waiting for approval",
	}
	if !reflect.DeepEqual(toolPayload, wantToolPayload) {
		t.Fatalf("tool payload = %#v, want %#v", toolPayload, wantToolPayload)
	}
}
