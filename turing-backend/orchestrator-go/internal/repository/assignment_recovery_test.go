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
