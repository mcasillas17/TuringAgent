package repository

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
)

// The recovering lifecycle is new, and the temptation with a new status value
// is to paste it into every predicate that already mentions running. These
// tests exist to say where that is right and where it is exactly wrong.
//
// It is right wherever the question is "does this run still need attention":
// recovery scans, terminalization, cancellation, the execution-exit gate,
// stale-assignment cleanup, and the session-deletion guard.
//
// It is wrong wherever the question is "is this run proven to be owned by a
// live worker": lease renewal, approval creation, and the generic runtime
// ingest predicate. Recovering means precisely that nobody can answer that
// question yet, and answering it "yes" would let a fenced worker keep a lease
// alive, open a second approval nobody is waiting on, or keep narrating a run
// it can no longer prove it owns.

// recoveringApprovalRun leaves a run recovering with a pending approval under
// it, which is the shape a worker loss during an approval wait produces.
func recoveringApprovalRun(t *testing.T, repo *Repository, worker string) (EnqueueUserMessageResult, Job, ApprovalRecord) {
	t.Helper()
	ctx := context.Background()
	enqueued := enqueueRun(t, repo, "Recovering approval")
	claimed, err := repo.ClaimNextJob(ctx, "general_assistant", worker)
	if err != nil {
		t.Fatalf("ClaimNextJob: %v", err)
	}
	if err := repo.RecordToolCallBefore(ctx, ToolCallRecord{
		ToolCallID: "call_recovering", RunID: enqueued.RunID, ModelToolCallID: "model_recovering",
	}, "general_assistant", "files", "files.update", `{"path":"note.txt"}`, "sha256:recovering"); err != nil {
		t.Fatalf("RecordToolCallBefore: %v", err)
	}
	approval, _, err := repo.CreateApprovalWithEvent(ctx, enqueued.RunID, "call_recovering", "general_assistant",
		"files.update", `{"path":"note.txt"}`, "sha256:recovering", "2099-01-01T00:00:00Z")
	if err != nil {
		t.Fatalf("CreateApprovalWithEvent: %v", err)
	}
	waiting, err := repo.GetRunState(ctx, enqueued.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if waiting.Lifecycle != lifecycleWaitingApproval {
		t.Fatalf("run lifecycle = %q, want waiting_approval", waiting.Lifecycle)
	}
	if _, err := repo.FenceRunOwnership(ctx, FenceRunOwnershipInput{
		RunID: enqueued.RunID, ExpectedStateVersion: waiting.StateVersion,
		WorkerID: worker, AssignmentAttemptID: claimed.AssignmentAttemptID,
	}); err != nil {
		t.Fatalf("FenceRunOwnership: %v", err)
	}
	return enqueued, claimed, approval
}

// TestRecoveringRunRemainsVisibleToRecoveryScan is the whole point of making
// recovering durable: the scan that rescues abandoned work must still find it.
// A recovering run that no scan returns is a run that waits forever.
func TestRecoveringRunRemainsVisibleToRecoveryScan(t *testing.T) {
	repo := New(openTestDB(t))
	ctx := context.Background()
	enqueued, claimed, _ := recoveringRun(t, repo, "worker-scan")

	found := func(assignments []Assignment) bool {
		for _, assignment := range assignments {
			if assignment.RunID == enqueued.RunID && assignment.JobID == claimed.JobID {
				return true
			}
		}
		return false
	}

	stale, err := repo.RecoverableAssignments(ctx, time.Now().UTC().Add(time.Hour))
	if err != nil {
		t.Fatalf("RecoverableAssignments: %v", err)
	}
	if !found(stale) {
		t.Fatalf("stale recovery scan lost the recovering run: %+v", stale)
	}

	startup, err := repo.startupRecoveryAssignments(ctx)
	if err != nil {
		t.Fatalf("startupRecoveryAssignments: %v", err)
	}
	if !found(startup) {
		t.Fatalf("startup recovery scan lost the recovering run: %+v", startup)
	}
}

// TestRecoveringRunCanCompleteFailOrCancelAtExpectedVersion covers the other
// half: recovery has to be able to end. A run that can be entered but never
// terminalized is worse than one that was never fenced.
func TestRecoveringRunCanCompleteFailOrCancelAtExpectedVersion(t *testing.T) {
	for name, terminalize := range map[string]struct {
		commit func(context.Context, *Repository, EnqueueUserMessageResult, int64) (RunTransitionResult, error)
		want   string
		reason string
	}{
		"completed": {
			commit: func(ctx context.Context, repo *Repository, run EnqueueUserMessageResult, version int64) (RunTransitionResult, error) {
				return repo.CompleteRunCanonical(ctx, CompleteRunInput{
					RunID: run.RunID, AssistantMessageID: run.AssistantMessageID,
					Content: "recovered output", ExpectedStateVersion: version,
				})
			},
			want: lifecycleCompleted, reason: "none",
		},
		"failed": {
			commit: func(ctx context.Context, repo *Repository, run EnqueueUserMessageResult, version int64) (RunTransitionResult, error) {
				return repo.FailRunCanonical(ctx, FailRunInput{
					RunID: run.RunID, AssistantMessageID: run.AssistantMessageID,
					ExpectedStateVersion: version, Failure: providerFailureForTest(),
				})
			},
			want: lifecycleFailed, reason: "provider_failure",
		},
		"cancelled": {
			commit: func(ctx context.Context, repo *Repository, run EnqueueUserMessageResult, version int64) (RunTransitionResult, error) {
				return repo.CancelRunCanonical(ctx, CancelRunInput{
					RunID: run.RunID, AssistantMessageID: run.AssistantMessageID,
					ExpectedStateVersion: version, Cancellation: abandonedCancellationForTest(),
				})
			},
			want: lifecycleCancelled, reason: "abandoned",
		},
	} {
		t.Run(name, func(t *testing.T) {
			repo := New(openTestDB(t))
			ctx := context.Background()
			enqueued, _, recovering := recoveringRun(t, repo, "worker-terminalize")

			result, err := terminalize.commit(ctx, repo, enqueued, recovering.StateVersion)
			if err != nil {
				t.Fatalf("terminalize a recovering run: %v", err)
			}
			if result.State.Lifecycle != terminalize.want || result.State.OutcomeReason != terminalize.reason {
				t.Fatalf("terminal state = %s/%s, want %s/%s",
					result.State.Lifecycle, result.State.OutcomeReason, terminalize.want, terminalize.reason)
			}
			if result.State.StateVersion != recovering.StateVersion+1 {
				t.Fatalf("terminal version = %d, want %d", result.State.StateVersion, recovering.StateVersion+1)
			}
			if !result.State.FinishedAt.Valid {
				t.Fatal("terminal state carries no finished_at")
			}
		})
	}
}

// TestRecoveringRunDoesNotRenewUnprovenOwnership guards the predicate that must
// NOT learn about recovering. Renewing a lease is the orchestrator asserting
// that a specific worker still owns the run; recovering is the state that says
// it cannot assert that.
func TestRecoveringRunDoesNotRenewUnprovenOwnership(t *testing.T) {
	repo := New(openTestDB(t))
	ctx := context.Background()
	enqueued, claimed, _ := recoveringRun(t, repo, "worker-renew")
	assignment := Assignment{
		JobID: claimed.JobID, RunID: enqueued.RunID,
		WorkerID: "worker-renew", AttemptID: claimed.AssignmentAttemptID,
	}

	var leaseBefore sql.NullString
	if err := repo.db.QueryRowContext(ctx,
		`SELECT execution_lease_expires_at FROM agent_runs WHERE id = ?`, enqueued.RunID).Scan(&leaseBefore); err != nil {
		t.Fatal(err)
	}

	renewed, err := repo.RenewAssignments(ctx, []Assignment{assignment}, time.Now().UTC().Add(time.Hour))
	if err != nil {
		t.Fatalf("RenewAssignments: %v", err)
	}
	if len(renewed) != 0 {
		t.Fatalf("renewed %d leases for a run with unproven ownership, want none", len(renewed))
	}
	var leaseAfter sql.NullString
	if err := repo.db.QueryRowContext(ctx,
		`SELECT execution_lease_expires_at FROM agent_runs WHERE id = ?`, enqueued.RunID).Scan(&leaseAfter); err != nil {
		t.Fatal(err)
	}
	if leaseAfter != leaseBefore {
		t.Fatalf("lease moved from %+v to %+v for a recovering run", leaseBefore, leaseAfter)
	}
}

// TestRecoveringRunCannotCreateASecondApproval guards the other predicate that
// must not learn about recovering. An approval asks a user to authorize a tool
// call a worker is about to make; a worker whose ownership is unproven cannot
// promise to make it, and a second pending approval would outlive the first.
func TestRecoveringRunCannotCreateASecondApproval(t *testing.T) {
	repo := New(openTestDB(t))
	ctx := context.Background()
	enqueued, _, first := recoveringApprovalRun(t, repo, "worker-approval")
	recovering, err := repo.GetRunState(ctx, enqueued.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if recovering.Lifecycle != lifecycleRecovering {
		t.Fatalf("run lifecycle = %q, want recovering", recovering.Lifecycle)
	}
	events := countRunEvents(t, repo, enqueued.RunID)

	if err := repo.RecordToolCallBefore(ctx, ToolCallRecord{
		ToolCallID: "call_second", RunID: enqueued.RunID, ModelToolCallID: "model_second",
	}, "general_assistant", "files", "files.delete", `{"path":"other.txt"}`, "sha256:second"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := repo.CreateApprovalWithEvent(ctx, enqueued.RunID, "call_second", "general_assistant",
		"files.delete", `{"path":"other.txt"}`, "sha256:second", "2099-01-01T00:00:00Z"); err == nil {
		t.Fatal("a recovering run opened a second approval")
	}
	if _, err := repo.CreateApproval(ctx, enqueued.RunID, "call_second", "general_assistant",
		"files.delete", `{"path":"other.txt"}`, "sha256:second", "2099-01-01T00:00:00Z"); err == nil {
		t.Fatal("a recovering run opened a second approval without an event")
	}

	state, err := repo.GetRunState(ctx, enqueued.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if state != recovering {
		t.Fatalf("a rejected approval changed the run: %+v, want %+v", state, recovering)
	}
	if after := countRunEvents(t, repo, enqueued.RunID); after != events {
		t.Fatalf("a rejected approval appended %d events", after-events)
	}
	var pending int
	if err := repo.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM approvals WHERE run_id = ? AND status = 'pending'`, enqueued.RunID).Scan(&pending); err != nil {
		t.Fatal(err)
	}
	if pending != 1 {
		t.Fatalf("recovering run holds %d pending approvals, want only the original %s", pending, first.ApprovalID)
	}
}

// TestRecoveringRunPreservesExecutionExitGating covers both halves of the exit
// gate, which are gated on opposite facts.
//
// The exit acknowledgement may only be claimed for a run that actually reached
// a terminal lifecycle, so a recovering run cannot claim it. A generic runtime
// event is refused for the opposite reason: it is narration from a worker whose
// ownership of the run is exactly what nobody can currently prove, and
// admitting it would let a fenced worker keep writing the run's story. Which
// evidence a recovering run may still contribute is decided by the specific
// recovery, terminal, and approval gates — never by the generic ingest path.
func TestRecoveringRunPreservesExecutionExitGating(t *testing.T) {
	repo := New(openTestDB(t))
	ctx := context.Background()
	enqueued, _, _ := recoveringRun(t, repo, "worker-exit")
	events := countRunEvents(t, repo, enqueued.RunID)

	if _, err := repo.AppendRuntimeEvent(ctx, &turingv1.TuringEvent{
		RunId: enqueued.RunID, Type: turingv1.TuringEventType_TURING_EVENT_TYPE_MESSAGE_DELTA,
	}); !errors.Is(err, ErrRunNotActive) {
		t.Fatalf("AppendRuntimeEvent for a recovering run = %v, want ErrRunNotActive", err)
	}
	if after := countRunEvents(t, repo, enqueued.RunID); after != events {
		t.Fatalf("a refused runtime event appended %d events", after-events)
	}
	if err := repo.AcknowledgeExecutionExit(ctx, enqueued.RunID); !errors.Is(err, ErrRunNotActive) {
		t.Fatalf("AcknowledgeExecutionExit for a recovering run = %v, want ErrRunNotActive", err)
	}
}

// TestRecoveringRunOnlyResumesOrRequeuesThroughGuardedTransitions pins the two
// ways out of recovery. Both require an explicit expectation, so a stale
// caller cannot resurrect a run someone else already moved.
func TestRecoveringRunOnlyResumesOrRequeuesThroughGuardedTransitions(t *testing.T) {
	t.Run("resume", func(t *testing.T) {
		repo := New(openTestDB(t))
		ctx := context.Background()
		enqueued, claimed, recovering := recoveringRun(t, repo, "worker-resume")

		resumed, err := repo.ResumeRecoveringRun(ctx, ResumeRecoveringRunInput{
			RunID: enqueued.RunID, ExpectedStateVersion: recovering.StateVersion,
			WorkerID: "worker-resume", AssignmentAttemptID: claimed.AssignmentAttemptID,
		})
		if err != nil {
			t.Fatalf("ResumeRecoveringRun: %v", err)
		}
		if resumed.State.Lifecycle != lifecycleRunning || resumed.State.StateVersion != recovering.StateVersion+1 {
			t.Fatalf("resumed state = %+v, want running at version %d", resumed.State, recovering.StateVersion+1)
		}
		if len(resumed.Events) != 1 || resumed.Events[0].Type != runStateChangedEventType {
			t.Fatalf("resume events = %+v, want exactly one %s", resumed.Events, runStateChangedEventType)
		}
	})

	t.Run("requeue", func(t *testing.T) {
		repo := New(openTestDB(t))
		ctx := context.Background()
		enqueued, claimed, recovering := recoveringRun(t, repo, "worker-requeue")

		requeued, err := repo.RequeueRecoveringRun(ctx, RequeueRecoveringRunInput{
			RunID: enqueued.RunID, ExpectedStateVersion: recovering.StateVersion,
			AssignmentAttemptID: claimed.AssignmentAttemptID,
		})
		if err != nil {
			t.Fatalf("RequeueRecoveringRun: %v", err)
		}
		if requeued.State.Lifecycle != lifecycleQueued || requeued.State.StateVersion != recovering.StateVersion+1 {
			t.Fatalf("requeued state = %+v, want queued at version %d", requeued.State, recovering.StateVersion+1)
		}
		// Exactly one projection: the run was already recovering, so requeueing
		// it must not also announce a second fence for ownership it had already
		// lost. The version check above would still pass if a same-version
		// event were appended beside it, which is why the count is asserted.
		if len(requeued.Events) != 1 || requeued.Events[0].Type != runStateChangedEventType {
			t.Fatalf("requeue events = %+v, want exactly one %s", requeued.Events, runStateChangedEventType)
		}
		if snapshot := decodeRunStateSnapshot(t, requeued.Events[0]); snapshot.Lifecycle != lifecycleQueued {
			t.Fatalf("requeue projected %q, want queued", snapshot.Lifecycle)
		}
		run, err := repo.GetRun(ctx, enqueued.RunID)
		if err != nil {
			t.Fatal(err)
		}
		if run.ExecutionActive || run.WorkerID != "" || run.ExecutionAttemptID != "" {
			t.Fatalf("requeued run kept execution ownership: %+v", run)
		}
	})

	t.Run("no unguarded running to queued shortcut", func(t *testing.T) {
		repo := New(openTestDB(t))
		ctx := context.Background()
		enqueued := enqueueRun(t, repo, "No shortcut")
		claimed, err := repo.ClaimNextJob(ctx, "general_assistant", "worker-shortcut")
		if err != nil {
			t.Fatal(err)
		}
		running, err := repo.GetRunState(ctx, enqueued.RunID)
		if err != nil {
			t.Fatal(err)
		}
		if running.Lifecycle != lifecycleRunning {
			t.Fatalf("run lifecycle = %q, want running", running.Lifecycle)
		}
		// A running run is not requeueable directly: it has to be fenced into
		// recovering first, so the interval where nobody owned it is durable.
		if _, err := repo.RequeueRecoveringRun(ctx, RequeueRecoveringRunInput{
			RunID: enqueued.RunID, ExpectedStateVersion: running.StateVersion,
			AssignmentAttemptID: claimed.AssignmentAttemptID,
		}); !errors.Is(err, ErrRunTransitionConflict) {
			t.Fatalf("requeue of a running run = %v, want ErrRunTransitionConflict", err)
		}
		state, err := repo.GetRunState(ctx, enqueued.RunID)
		if err != nil {
			t.Fatal(err)
		}
		if state != running {
			t.Fatalf("rejected requeue changed the run: %+v", state)
		}
	})
}

// TestRecoveringRunCanTerminalizeApproval covers the approval side of the same
// rule as the recovery scan: a fenced run's pending approval must still be
// closable, or the user is left holding a decision nobody will ever consume.
func TestRecoveringRunCanTerminalizeApproval(t *testing.T) {
	for name, terminalize := range map[string]func(context.Context, *Repository, string) (ApprovalTerminalization, error){
		"expired": func(ctx context.Context, repo *Repository, approvalID string) (ApprovalTerminalization, error) {
			return repo.ExpireApprovalWithEvent(ctx, approvalID, now())
		},
		"denied": func(ctx context.Context, repo *Repository, approvalID string) (ApprovalTerminalization, error) {
			return repo.DenyApprovalWithEvent(ctx, approvalID, sql.NullString{}, now())
		},
		"delivery failed": func(ctx context.Context, repo *Repository, approvalID string) (ApprovalTerminalization, error) {
			return repo.FailApprovalDeliveryWithEvent(ctx, approvalID, now())
		},
	} {
		t.Run(name, func(t *testing.T) {
			repo := New(openTestDB(t))
			ctx := context.Background()
			enqueued, _, approval := recoveringApprovalRun(t, repo, "worker-approval-close")
			recovering, err := repo.GetRunState(ctx, enqueued.RunID)
			if err != nil {
				t.Fatal(err)
			}

			terminalization, err := terminalize(ctx, repo, approval.ApprovalID)
			if err != nil {
				t.Fatalf("terminalize the approval of a recovering run: %v", err)
			}
			if !terminalization.Changed {
				t.Fatal("approval terminalization reported no change")
			}
			state, err := repo.GetRunState(ctx, enqueued.RunID)
			if err != nil {
				t.Fatal(err)
			}
			if state.Lifecycle != lifecycleFailed {
				t.Fatalf("run lifecycle = %q, want failed after its approval was terminalized", state.Lifecycle)
			}
			if state.StateVersion != recovering.StateVersion+1 {
				t.Fatalf("terminal version = %d, want %d", state.StateVersion, recovering.StateVersion+1)
			}
		})
	}
}

// TestRecoveringRunBlocksSessionDeletionAsActive keeps the deletion guard
// honest. Deleting a session out from under a recovering run cascades away the
// rows a worker may still be holding, which is the exact failure the guard
// exists to prevent.
func TestRecoveringRunBlocksSessionDeletionAsActive(t *testing.T) {
	repo := New(openTestDB(t))
	ctx := context.Background()
	enqueued, _, _ := recoveringRun(t, repo, "worker-delete")

	// Execution is cleared so the guard cannot pass on execution_active alone;
	// only the lifecycle can refuse this deletion.
	if _, err := repo.db.ExecContext(ctx, `
		UPDATE agent_runs
		SET execution_active = 0, execution_state = 'none',
			execution_lease_expires_at = NULL, execution_lease_expires_at_ns = NULL
		WHERE id = ?
	`, enqueued.RunID); err != nil {
		t.Fatal(err)
	}

	if err := repo.DeleteSession(ctx, enqueued.SessionID); !errors.Is(err, ErrSessionHasActiveRun) {
		t.Fatalf("DeleteSession with a recovering run = %v, want ErrSessionHasActiveRun", err)
	}
	var sessions int
	if err := repo.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM sessions WHERE id = ?`, enqueued.SessionID).Scan(&sessions); err != nil {
		t.Fatal(err)
	}
	if sessions != 1 {
		t.Fatal("the refused deletion still removed the session")
	}
}
