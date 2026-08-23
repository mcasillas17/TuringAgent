package repository

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/runoutcome"
)

// runStepNoticeVersion reads the state version a failure-like run-step notice
// published. A notice without one is a fatal failure rather than a zero,
// because zero is protobuf absence: publishing it would tell a client
// reconciling by version that this notice is older than every state the run
// ever had.
func runStepNoticeVersion(t *testing.T, event Event) int64 {
	t.Helper()
	var payload struct {
		StateVersion *int64 `json:"stateVersion"`
	}
	if err := json.Unmarshal([]byte(event.PayloadJSON), &payload); err != nil {
		t.Fatalf("decode %s payload %q: %v", event.Type, event.PayloadJSON, err)
	}
	if payload.StateVersion == nil {
		t.Fatalf("%s notice carries no stateVersion: %s", event.Type, event.PayloadJSON)
	}
	if *payload.StateVersion < 1 {
		t.Fatalf("%s notice published state version %d, want a real stored version",
			event.Type, *payload.StateVersion)
	}
	return *payload.StateVersion
}

// terminalSnapshotVersion reads the version the terminal projection committed,
// so a notice can be checked against the run state that actually followed it.
func terminalSnapshotVersion(t *testing.T, events []Event, eventType string) int64 {
	t.Helper()
	for _, event := range events {
		if event.Type == eventType {
			return decodeRunStateSnapshot(t, event).StateVersion
		}
	}
	t.Fatalf("no %s event in %+v", eventType, events)
	return 0
}

// A requeue commits two real transitions, and the notice is appended after
// both. The version it publishes is therefore the version the run actually
// holds when a client reads the notice — not the version it left behind.
func TestDispatchRetryNoticeCarriesTheRequeuedStateVersion(t *testing.T) {
	repo := New(openTestDB(t))
	ctx := context.Background()
	enqueued, _ := claimRetryRun(t, repo, "worker-retry-version")
	running, err := repo.GetRunState(ctx, enqueued.RunID)
	if err != nil {
		t.Fatal(err)
	}

	decision, err := repo.RequeueOrFailRetryableRun(ctx, RetryableRunFailureInput{
		RunID: enqueued.RunID, Failure: dispatchCondition("worker_busy"), MaxAttempts: 3,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !decision.Requeued {
		t.Fatalf("decision = %+v, want requeued", decision)
	}
	notice := onlyRunStepEvent(t, decision.Events)
	requeued, err := repo.GetRunState(ctx, enqueued.RunID)
	if err != nil {
		t.Fatal(err)
	}
	version := runStepNoticeVersion(t, notice)
	if version != requeued.StateVersion || version != running.StateVersion+2 {
		t.Fatalf("retry notice version = %d, want the committed queued version %d (two past %d)",
			version, requeued.StateVersion, running.StateVersion)
	}
	assertStepNotice(t, notice, runoutcome.NoticeDispatchRetry, 2, 3, version)
	// The notice must never claim a version the durable log has not reached at
	// that point in the event order.
	for _, event := range decision.Events {
		if event.Type != runStateChangedEventType {
			continue
		}
		if event.Sequence < notice.Sequence && decodeRunStateSnapshot(t, event).StateVersion > version {
			t.Fatalf("notice at sequence %d publishes version %d, behind an earlier projection",
				notice.Sequence, version)
		}
	}
}

// Losing a worker requeues through the same two transitions from a different
// caller, and owes the same answer.
func TestRecoveryRetryNoticeCarriesTheRequeuedStateVersion(t *testing.T) {
	repo := New(openTestDB(t))
	ctx := context.Background()
	enqueued, assignment := runningAssignment(t, repo, "worker-recovery-version")
	running, err := repo.GetRunState(ctx, enqueued.RunID)
	if err != nil {
		t.Fatal(err)
	}

	reconciliation, err := repo.RecoverAssignmentWithLimit(ctx, assignment, 3)
	if err != nil {
		t.Fatal(err)
	}
	if !reconciliation.Requeued {
		t.Fatalf("reconciliation = %+v, want requeued", reconciliation)
	}
	notice := onlyRunStepEvent(t, reconciliation.Events)
	requeued, err := repo.GetRunState(ctx, enqueued.RunID)
	if err != nil {
		t.Fatal(err)
	}
	version := runStepNoticeVersion(t, notice)
	if version != requeued.StateVersion || version != running.StateVersion+2 {
		t.Fatalf("recovery retry notice version = %d, want the committed queued version %d (two past %d)",
			version, requeued.StateVersion, running.StateVersion)
	}
	assertStepNotice(t, notice, runoutcome.NoticeRecoveryRetry, 2, 3, version)
}

// The give-up notice is written before the terminal transition, deliberately,
// so the explanation precedes the failure it explains. Its version is therefore
// the version the run still holds where the notice sits in the log — the
// terminal projection that follows is exactly one further on.
func TestDispatchGiveUpNoticeCarriesTheStateVersionItWasWrittenAt(t *testing.T) {
	database := openTestDB(t)
	repo := New(database)
	ctx := context.Background()
	enqueued, claimed := claimRetryRun(t, repo, "worker-giveup-version")
	if _, err := database.ExecContext(ctx, `UPDATE jobs SET attempt = 3 WHERE id = ?`, claimed.JobID); err != nil {
		t.Fatal(err)
	}
	active, err := repo.GetRunState(ctx, enqueued.RunID)
	if err != nil {
		t.Fatal(err)
	}

	decision, err := repo.RequeueOrFailRetryableRun(ctx, RetryableRunFailureInput{
		RunID: enqueued.RunID, Failure: dispatchCondition("worker_busy"), MaxAttempts: 3,
	})
	if err != nil {
		t.Fatal(err)
	}
	if decision.Requeued {
		t.Fatalf("decision = %+v, want a terminal give-up", decision)
	}
	notice := onlyRunStepEvent(t, decision.Events)
	version := runStepNoticeVersion(t, notice)
	if version != active.StateVersion {
		t.Fatalf("give-up notice version = %d, want the version durable when it was written, %d",
			version, active.StateVersion)
	}
	assertStepNotice(t, notice, runoutcome.NoticeRecoveryExhausted, 3, 3, version)
	terminalVersion := terminalSnapshotVersion(t, decision.Events, "agent.run.failed")
	if terminalVersion != version+1 {
		t.Fatalf("terminal version = %d, want exactly one past the notice's %d", terminalVersion, version)
	}
	for _, event := range decision.Events {
		if event.Type == "agent.run.failed" && event.Sequence <= notice.Sequence {
			t.Fatalf("terminal event at sequence %d precedes its own explanation at %d",
				event.Sequence, notice.Sequence)
		}
	}
}

// Recovery gives up through its own writer, with its own attempt bookkeeping,
// and owes the same version discipline.
func TestRecoveryGiveUpNoticeCarriesTheStateVersionItWasWrittenAt(t *testing.T) {
	repo := New(openTestDB(t))
	ctx := context.Background()
	enqueued, assignment := runningAssignment(t, repo, "worker-recovery-giveup")
	active, err := repo.GetRunState(ctx, enqueued.RunID)
	if err != nil {
		t.Fatal(err)
	}

	// maxAttempts=1 with the job on attempt 1 exhausts the budget immediately.
	reconciliation, err := repo.RecoverAssignmentWithLimit(ctx, assignment, 1)
	if err != nil {
		t.Fatal(err)
	}
	if reconciliation.Requeued {
		t.Fatalf("reconciliation = %+v, want a terminal give-up", reconciliation)
	}
	notice := onlyRunStepEvent(t, reconciliation.Events)
	version := runStepNoticeVersion(t, notice)
	if version != active.StateVersion {
		t.Fatalf("recovery give-up notice version = %d, want the version durable when it was written, %d",
			version, active.StateVersion)
	}
	assertStepNotice(t, notice, runoutcome.NoticeRecoveryExhausted, 1, 1, version)
	if terminalVersion := terminalSnapshotVersion(t, reconciliation.Events, "agent.run.failed"); terminalVersion != version+1 {
		t.Fatalf("terminal version = %d, want exactly one past the notice's %d", terminalVersion, version)
	}
}

// A notice with no version is refused rather than published with the protobuf
// absence value. A run-step notice a client cannot place against a run state is
// worse than no notice: it reconciles as older than everything.
func TestStepNoticeRefusesAnUnresolvedStateVersion(t *testing.T) {
	database := openTestDB(t)
	repo := New(database)
	ctx := context.Background()
	session, err := repo.CreateSession(ctx, "Notice version guard")
	if err != nil {
		t.Fatal(err)
	}
	enqueued, err := repo.EnqueueUserMessage(ctx, EnqueueUserMessageInput{
		SessionID: session.SessionID, Content: "guard", AgentID: "general_assistant",
		ModelProvider: "ollama", Model: "llama3.2",
	})
	if err != nil {
		t.Fatal(err)
	}
	notice, err := runoutcome.NewStepNotice(runoutcome.NoticeDispatchRetry, 2, 3)
	if err != nil {
		t.Fatal(err)
	}

	for _, version := range []int64{unresolvedStateVersion, -1} {
		tx, err := database.BeginTx(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		_, err = appendStepNoticeTx(ctx, tx, session.SessionID, enqueued.RunID, enqueued.TraceID, notice, version, now())
		if rollbackErr := tx.Rollback(); rollbackErr != nil {
			t.Fatal(rollbackErr)
		}
		if !errors.Is(err, ErrRunStateVersionInvalid) {
			t.Fatalf("appendStepNoticeTx at version %d error = %v, want ErrRunStateVersionInvalid", version, err)
		}
	}
	var notices int
	if err := database.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM events WHERE run_id = ? AND type = 'agent.run.step'`, enqueued.RunID).Scan(&notices); err != nil {
		t.Fatal(err)
	}
	if notices != 0 {
		t.Fatalf("refused notices persisted %d rows, want none", notices)
	}
}
