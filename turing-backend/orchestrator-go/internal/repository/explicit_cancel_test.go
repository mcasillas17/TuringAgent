package repository

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/db"
)

func TestExplicitCancelQueued(t *testing.T) {
	repo := New(openTestDB(t))
	run, before := queuedTerminalSource(t, repo)
	result, err := repo.CancelUserRun(context.Background(), run.SessionID, run.RunID, "cancel:"+run.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Accepted || result.State.OutcomeReason != "user_cancelled" || result.State.StateVersion != before.StateVersion+1 {
		t.Fatalf("cancel = %+v", result)
	}
}

func TestExplicitCancelLifecycleAndExecutionFences(t *testing.T) {
	for _, phase := range []string{"queued", "pending_send", "sending", "delivered", "waiting_approval", "recovering", "completed", "failed", "cancelled"} {
		t.Run(phase, func(t *testing.T) {
			repo := New(openTestDB(t))
			ctx := context.Background()
			var run EnqueueUserMessageResult
			var state RunState
			switch phase {
			case "completed":
				run, state = completedTerminalSource(t, repo)
			case "failed":
				run, state = failedTerminalSource(t, repo)
			case "cancelled":
				run, state = cancelledTerminalSource(t, repo)
			case "recovering":
				run, _, state = recoveringRun(t, repo, "worker-cancel")
			case "waiting_approval":
				run, _ = explicitApprovalFixture(t, repo)
				state, _ = repo.GetRunState(ctx, run.RunID)
			default:
				run, state = queuedTerminalSource(t, repo)
				if phase != "queued" {
					job, err := repo.ClaimNextJob(ctx, "general_assistant", "worker-cancel")
					if err != nil {
						t.Fatal(err)
					}
					assignment := Assignment{RunID: run.RunID, JobID: run.JobID, WorkerID: "worker-cancel", AttemptID: job.AssignmentAttemptID}
					if phase != "pending_send" {
						if err := repo.BeginAssignmentSend(ctx, assignment); err != nil {
							t.Fatal(err)
						}
					}
					if phase == "delivered" {
						if err := repo.MarkAssignmentDelivered(ctx, assignment); err != nil {
							t.Fatal(err)
						}
					}
					state, err = repo.GetRunState(ctx, run.RunID)
					if err != nil {
						t.Fatal(err)
					}
				}
			}
			before, err := repo.GetRun(ctx, run.RunID)
			if err != nil {
				t.Fatal(err)
			}
			result, err := repo.CancelUserRun(ctx, run.SessionID, run.RunID, "cancel:"+run.RunID)
			if err != nil {
				t.Fatal(err)
			}
			terminal := isTerminalLifecycle(state.Lifecycle)
			if result.Accepted == terminal {
				t.Fatalf("accepted = %v for %s", result.Accepted, phase)
			}
			if terminal {
				if !reflect.DeepEqual(result.State, state) || len(result.Events) != 0 {
					t.Fatalf("terminal rewritten: %+v -> %+v", state, result)
				}
			} else if result.State.Lifecycle != "cancelled" || result.State.OutcomeReason != "user_cancelled" || result.State.StateVersion != state.StateVersion+1 {
				t.Fatalf("cancel = %+v", result)
			}
			after, err := repo.GetRun(ctx, run.RunID)
			if err != nil {
				t.Fatal(err)
			}
			if after.ExecutionActive != before.ExecutionActive || after.ExecutionState != before.ExecutionState ||
				after.ExecutionAttemptID != before.ExecutionAttemptID || after.WorkerID != before.WorkerID {
				t.Fatalf("cancellation changed containment: %+v -> %+v", before, after)
			}
			if !terminal {
				if _, err := repo.CompleteRunCanonical(ctx, CompleteRunInput{RunID: run.RunID, AssistantMessageID: run.AssistantMessageID, ExpectedStateVersion: result.State.StateVersion, Content: "late replacement"}); err == nil {
					t.Fatal("late completion revived cancelled run")
				}
				if after.AssistantContent == "late replacement" {
					t.Fatal("late output corrupted message")
				}
			}
		})
	}
}

func TestExplicitCancelConcurrentDuplicateAndRestartReceipt(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "cancel.db")
	database, err := db.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if err := db.ApplyMigrations(ctx, database); err != nil {
		t.Fatal(err)
	}
	repo := New(database)
	run, _ := queuedTerminalSource(t, repo)
	const callers = 12
	results := make(chan CancelUserRunResult, callers)
	errs := make(chan error, callers)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for range callers {
		wg.Go(func() {
			<-start
			result, err := New(database).CancelUserRun(ctx, run.SessionID, run.RunID, "same-operation")
			results <- result
			errs <- err
		})
	}
	close(start)
	wg.Wait()
	close(results)
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	var saved RunState
	transitions := 0
	for result := range results {
		if !result.Accepted {
			t.Fatal("duplicate did not replay accepted result")
		}
		if saved.RunID == "" {
			saved = result.State
		}
		if !reflect.DeepEqual(saved, result.State) {
			t.Fatal("duplicate result changed")
		}
		transitions += len(result.Events)
	}
	if transitions != 1 {
		t.Fatalf("events = %d, want 1", transitions)
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := db.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = reopened.Close() }()
	receipt, err := New(reopened).CancelUserRun(ctx, run.SessionID, run.RunID, "same-operation")
	if err != nil || !receipt.Accepted || !reflect.DeepEqual(saved, receipt.State) || len(receipt.Events) != 0 {
		t.Fatalf("restart receipt = %+v, %v", receipt, err)
	}
	var audits int
	if err := reopened.QueryRow(`SELECT COUNT(*) FROM audit_logs WHERE action = 'run.cancel'`).Scan(&audits); err != nil || audits != 1 {
		t.Fatalf("audits = %d, %v", audits, err)
	}
}

func TestExplicitCancelAtomicAuditEventAndReceipt(t *testing.T) {
	for _, table := range []string{"audit_logs", "events", "run_cancellation_receipts"} {
		t.Run(table, func(t *testing.T) {
			repo := New(openTestDB(t))
			run, before := queuedTerminalSource(t, repo)
			if _, err := repo.db.Exec(`CREATE TRIGGER reject_cancel BEFORE INSERT ON ` + table + ` BEGIN SELECT RAISE(ABORT, 'injected persistence failure'); END`); err != nil {
				t.Fatal(err)
			}
			if _, err := repo.CancelUserRun(context.Background(), run.SessionID, run.RunID, "cancel"); err == nil {
				t.Fatal("injected transaction failure disappeared")
			}
			after, err := repo.GetRunState(context.Background(), run.RunID)
			if err != nil || !reflect.DeepEqual(before, after) {
				t.Fatalf("partial terminal transition: %+v, %v", after, err)
			}
			for _, query := range []string{
				`SELECT COUNT(*) FROM run_cancellation_receipts`,
				`SELECT COUNT(*) FROM audit_logs WHERE action='run.cancel'`,
				`SELECT COUNT(*) FROM events WHERE type='agent.run.cancelled'`,
			} {
				var count int
				if err := repo.db.QueryRow(query).Scan(&count); err != nil || count != 0 {
					t.Fatalf("partial transaction: %s = %d, %v", query, count, err)
				}
			}
			if _, err := repo.db.Exec(`DROP TRIGGER reject_cancel`); err != nil {
				t.Fatal(err)
			}
			if _, err := repo.CancelUserRun(context.Background(), run.SessionID, run.RunID, "cancel"); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestExplicitCancelApprovalPendingApprovedAndConsumedProvenance(t *testing.T) {
	for _, phase := range []string{"pending", "approved", "consumed"} {
		t.Run(phase, func(t *testing.T) {
			repo := New(openTestDB(t))
			ctx := context.Background()
			run, approval := explicitApprovalFixture(t, repo)
			approvalID := approval.ApprovalID
			if phase != "pending" {
				if _, err := repo.ApproveApprovalWithEvent(ctx, approvalID, "test-consumed-token", sql.NullString{}, now()); err != nil {
					t.Fatal(err)
				}
			}
			if phase == "consumed" {
				if _, err := repo.ConsumeApprovalWithEvent(ctx, approvalID, now()); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := repo.CancelUserRun(ctx, run.SessionID, run.RunID, "cancel"); err != nil {
				t.Fatal(err)
			}
			var status string
			var token sql.NullString
			if err := repo.db.QueryRow(`SELECT status, approval_token FROM approvals WHERE id=?`, approvalID).Scan(&status, &token); err != nil {
				t.Fatal(err)
			}
			if phase == "consumed" && (status != "consumed" || token.String != "test-consumed-token") {
				t.Fatalf("consumed provenance erased: %s %v", status, token)
			}
			if phase != "consumed" && (status != "expired" || token.Valid) {
				t.Fatalf("pending approval = %s", status)
			}
			if _, err := repo.ConsumeApprovalWithEvent(ctx, approvalID, now()); err == nil {
				t.Fatal("cancelled approval could be newly consumed")
			}
		})
	}
}

func TestExplicitCancelVisibilityPrecedesReceiptConflict(t *testing.T) {
	repo := New(openTestDB(t))
	ctx := context.Background()
	first, _ := queuedTerminalSource(t, repo)
	other, _ := queuedTerminalSource(t, repo)
	if _, err := repo.CancelUserRun(ctx, first.SessionID, first.RunID, "one-key"); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.CancelUserRun(ctx, other.SessionID, other.RunID, "one-key"); !errors.Is(err, ErrCancelKeyConflict) {
		t.Fatalf("visible conflict = %v", err)
	}
	if _, err := repo.CancelUserRun(ctx, other.SessionID, first.RunID, "one-key"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("hidden target revealed receipt: %v", err)
	}
	if _, err := repo.BeginSessionDeletion(ctx, first.SessionID); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.CancelUserRun(ctx, first.SessionID, first.RunID, "one-key"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("withdrawn replay = %v", err)
	}
	if _, err := repo.ReadRunCancellation(ctx, first.SessionID, first.RunID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("withdrawn read = %v", err)
	}
}

func TestExplicitCancelFencesPendingSendAndLateCompletion(t *testing.T) {
	repo := New(openTestDB(t))
	ctx := context.Background()
	run, job, state := runningRun(t, repo, "worker-race")
	assigned := Assignment{RunID: run.RunID, JobID: run.JobID, WorkerID: "worker-race", AttemptID: job.AssignmentAttemptID}
	committed := make(chan struct{})
	result := make(chan error, 1)
	go func() {
		<-committed
		result <- repo.BeginAssignmentSend(ctx, assigned)
	}()
	if _, err := repo.CancelUserRun(ctx, run.SessionID, run.RunID, "cancel"); err != nil {
		t.Fatal(err)
	}
	close(committed)
	if err := <-result; !errors.Is(err, ErrAssignmentFenced) {
		t.Fatalf("send after cancellation = %v", err)
	}
	if _, err := repo.CompleteRunCanonical(ctx, CompleteRunInput{RunID: run.RunID, AssistantMessageID: run.AssistantMessageID, ExpectedStateVersion: state.StateVersion, Content: "late"}); err == nil {
		t.Fatal("completion overtook cancellation")
	}
}

func TestExplicitCancelReadObservesReconciliationWithoutRewritingReceipt(t *testing.T) {
	repo := New(openTestDB(t))
	ctx := context.Background()
	run, _, _ := runningRun(t, repo, "worker-reconcile")
	receipt, err := repo.CancelUserRun(ctx, run.SessionID, run.RunID, "cancel")
	if err != nil || !receipt.ExecutionActive {
		t.Fatalf("receipt = %+v, %v", receipt, err)
	}
	if err := repo.AcknowledgeExecutionExit(ctx, run.RunID); err != nil {
		t.Fatal(err)
	}
	replay, err := repo.CancelUserRun(ctx, run.SessionID, run.RunID, "cancel")
	if err != nil || replay.ExecutionActive || !reflect.DeepEqual(replay.State, receipt.State) {
		t.Fatalf("reconciled replay = %+v, %v", replay, err)
	}
}

func explicitApprovalFixture(t *testing.T, repo *Repository) (EnqueueUserMessageResult, ApprovalRecord) {
	t.Helper()
	run, _ := approvalPairFixture(t, repo, "worker-approval-cancel")
	approval, err := repo.CreateApproval(context.Background(), run.RunID, "call_approval_pair", "general_assistant",
		"files.update", `{"path":"note.txt"}`, "sha256:pair", "2099-01-01T00:00:00Z")
	if err != nil {
		t.Fatal(err)
	}
	return run, approval
}

func TestExplicitCancelRestartPreservesLeaseUntilExistingRecoveryCutoff(t *testing.T) {
	database := openTestDB(t)
	repo := New(database)
	ctx := context.Background()
	run, job, _ := runningRun(t, repo, "worker-lost-on-restart")
	assignment := Assignment{RunID: run.RunID, JobID: run.JobID, WorkerID: "worker-lost-on-restart", AttemptID: job.AssignmentAttemptID}
	if err := repo.BeginAssignmentSend(ctx, assignment); err != nil {
		t.Fatal(err)
	}
	if err := repo.MarkAssignmentDelivered(ctx, assignment); err != nil {
		t.Fatal(err)
	}
	receipt, err := repo.CancelUserRun(ctx, run.SessionID, run.RunID, "cancel")
	if err != nil {
		t.Fatal(err)
	}
	restarted := New(database)
	if _, err := restarted.RecoverAllActiveAssignments(ctx); err != nil {
		t.Fatal(err)
	}
	waiting, err := restarted.GetRun(ctx, run.RunID)
	if err != nil || !waiting.ExecutionActive || waiting.ExecutionState != "uncertain" || waiting.StateVersion != receipt.State.StateVersion {
		t.Fatalf("restart prematurely released stop: %+v, %v", waiting, err)
	}
	if _, err := restarted.RecoverStaleAssignments(ctx, time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	finished, err := restarted.GetRun(ctx, run.RunID)
	if err != nil || finished.ExecutionActive || finished.Status != "cancelled" || finished.OutcomeReason != "user_cancelled" || finished.StateVersion != receipt.State.StateVersion {
		t.Fatalf("recovery rewrote stop or failed to reconcile: %+v, %v", finished, err)
	}
}

func TestExplicitCancelResolvesVersionAfterCompetingClaimOrCompletion(t *testing.T) {
	for _, complete := range []bool{false, true} {
		t.Run(map[bool]string{false: "claim", true: "completion"}[complete], func(t *testing.T) {
			repo := New(openTestDB(t))
			ctx := context.Background()
			run, _ := queuedTerminalSource(t, repo)
			unlock, err := lockSessionDecision(ctx, run.SessionID)
			if err != nil {
				t.Fatal(err)
			}
			defer unlock()
			started := make(chan struct{})
			done := make(chan CancelUserRunResult, 1)
			errors := make(chan error, 1)
			go func() {
				close(started)
				result, err := repo.CancelUserRun(ctx, run.SessionID, run.RunID, "cancel")
				done <- result
				errors <- err
			}()
			<-started
			if _, err := repo.ClaimNextJob(ctx, "general_assistant", "worker-competing"); err != nil {
				t.Fatal(err)
			}
			current, err := repo.GetRunState(ctx, run.RunID)
			if err != nil {
				t.Fatal(err)
			}
			if complete {
				terminal, err := repo.CompleteRunCanonical(ctx, CompleteRunInput{
					RunID: run.RunID, AssistantMessageID: run.AssistantMessageID, ExpectedStateVersion: current.StateVersion, Content: "committed answer",
				})
				if err != nil {
					t.Fatal(err)
				}
				current = terminal.State
			}
			unlock()
			result := <-done
			if err := <-errors; err != nil {
				t.Fatal(err)
			}
			if complete {
				if result.Accepted || !reflect.DeepEqual(result.State, current) {
					t.Fatalf("completed result rewritten: %+v", result)
				}
			} else if !result.Accepted || result.State.StateVersion != current.StateVersion+1 {
				t.Fatalf("cancel did not resolve post-claim version: %+v", result)
			}
		})
	}
}
