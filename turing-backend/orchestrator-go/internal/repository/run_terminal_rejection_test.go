package repository

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/runoutcome"
)

// A refused terminal command has to say which terminal command was refused.
//
// ErrRunNotCompletable, ErrRunNotFailable, and ErrRunNotCancellable are the
// sentinels the gRPC boundary already maps onto FailedPrecondition, and callers
// branch on them to decide whether telling a worker "stop" is enough. A generic
// transition conflict in their place turns a precondition the caller can
// understand into an unknown internal error, so the sentinel is part of each
// wrapper's contract rather than an implementation detail of terminal rows.
//
// The matrix below covers both refused shapes: a nonterminal source the command
// simply does not accept — a queued run cannot complete, because nothing ran —
// and a terminal source that is immutable. Both go through the canonical
// methods and through the temporary raw adapters, because the adapters are what
// the unconverted services still call.

// terminalSource builds a run sitting in one lifecycle, using the real
// transitions rather than writing a status into the row.
type terminalSource func(t *testing.T, repo *Repository) (EnqueueUserMessageResult, RunState)

const rejectionSourceContent = "the first answer"

func queuedTerminalSource(t *testing.T, repo *Repository) (EnqueueUserMessageResult, RunState) {
	t.Helper()
	enqueued := enqueueRun(t, repo, "Rejection source")
	state, err := repo.GetRunState(context.Background(), enqueued.RunID)
	if err != nil {
		t.Fatalf("GetRunState: %v", err)
	}
	return enqueued, state
}

func completedTerminalSource(t *testing.T, repo *Repository) (EnqueueUserMessageResult, RunState) {
	t.Helper()
	enqueued, _, running := runningRun(t, repo, "worker-rejection-completed")
	result, err := repo.CompleteRunCanonical(context.Background(), CompleteRunInput{
		RunID: enqueued.RunID, AssistantMessageID: enqueued.AssistantMessageID,
		Content: rejectionSourceContent, ExpectedStateVersion: running.StateVersion,
	})
	if err != nil {
		t.Fatalf("CompleteRunCanonical: %v", err)
	}
	return enqueued, result.State
}

func failedTerminalSource(t *testing.T, repo *Repository) (EnqueueUserMessageResult, RunState) {
	t.Helper()
	enqueued, _, running := runningRun(t, repo, "worker-rejection-failed")
	result, err := repo.FailRunCanonical(context.Background(), FailRunInput{
		RunID: enqueued.RunID, AssistantMessageID: enqueued.AssistantMessageID,
		ExpectedStateVersion: running.StateVersion, Failure: providerFailureForTest(),
	})
	if err != nil {
		t.Fatalf("FailRunCanonical: %v", err)
	}
	return enqueued, result.State
}

func cancelledTerminalSource(t *testing.T, repo *Repository) (EnqueueUserMessageResult, RunState) {
	t.Helper()
	enqueued, _, running := runningRun(t, repo, "worker-rejection-cancelled")
	result, err := repo.CancelRunCanonical(context.Background(), CancelRunInput{
		RunID: enqueued.RunID, AssistantMessageID: enqueued.AssistantMessageID,
		ExpectedStateVersion: running.StateVersion, Cancellation: abandonedCancellationForTest(),
	})
	if err != nil {
		t.Fatalf("CancelRunCanonical: %v", err)
	}
	return enqueued, result.State
}

// terminalCommand is one exported terminal writer, described by the contract it
// owes rather than by the code it happens to run.
type terminalCommand struct {
	name string
	// to is the lifecycle this command commits, and allowedFrom is where it
	// accepts arriving from. Both are restated here on purpose: the test says
	// what the state machine promises instead of asking the state machine.
	to          string
	allowedFrom []string
	// rejection is the sentinel a refused call owes its caller.
	rejection error
	// replaysExactly marks a raw adapter that carries neither content identity
	// nor an expected version, so an identical repeat onto the run it already
	// terminalized is the documented write-free duplicate rather than a refusal.
	replaysExactly bool
	call           func(ctx context.Context, repo *Repository, enqueued EnqueueUserMessageResult, state RunState) error
}

func terminalCommands() []terminalCommand {
	completionFrom := []string{lifecycleRunning, lifecycleWaitingApproval, lifecycleRecovering}
	terminalizeFrom := []string{lifecycleQueued, lifecycleRunning, lifecycleWaitingApproval, lifecycleRecovering}
	// A failure whose reason differs from the one the failed source committed,
	// so a refusal is never confused with an exact duplicate replay.
	toolFailure := func() runoutcome.Failure {
		return runoutcome.NormalizeFailure(runoutcome.OriginToolExecution, "tool_call_failed", runoutcome.RetryClassNever)
	}
	return []terminalCommand{
		{
			name: "CompleteRunCanonical", to: lifecycleCompleted, allowedFrom: completionFrom, rejection: ErrRunNotCompletable,
			call: func(ctx context.Context, repo *Repository, enqueued EnqueueUserMessageResult, state RunState) error {
				_, err := repo.CompleteRunCanonical(ctx, CompleteRunInput{
					RunID: enqueued.RunID, AssistantMessageID: enqueued.AssistantMessageID,
					Content: "a later answer", ExpectedStateVersion: state.StateVersion,
				})
				return err
			},
		},
		{
			name: "CompleteRun", to: lifecycleCompleted, allowedFrom: completionFrom, rejection: ErrRunNotCompletable,
			call: func(ctx context.Context, repo *Repository, enqueued EnqueueUserMessageResult, _ RunState) error {
				return repo.CompleteRun(ctx, enqueued.RunID, enqueued.AssistantMessageID, "a later answer")
			},
		},
		{
			name: "CompleteRunWithEvent", to: lifecycleCompleted, allowedFrom: completionFrom, rejection: ErrRunNotCompletable,
			call: func(ctx context.Context, repo *Repository, enqueued EnqueueUserMessageResult, _ RunState) error {
				_, err := repo.CompleteRunWithEvent(ctx, enqueued.RunID, enqueued.AssistantMessageID, "a later answer", `{"runId":"raw"}`, nil)
				return err
			},
		},
		{
			name: "FailRunCanonical", to: lifecycleFailed, allowedFrom: terminalizeFrom, rejection: ErrRunNotFailable,
			call: func(ctx context.Context, repo *Repository, enqueued EnqueueUserMessageResult, state RunState) error {
				_, err := repo.FailRunCanonical(ctx, FailRunInput{
					RunID: enqueued.RunID, AssistantMessageID: enqueued.AssistantMessageID,
					ExpectedStateVersion: state.StateVersion, Failure: toolFailure(),
				})
				return err
			},
		},
		{
			name: "FailRun", to: lifecycleFailed, allowedFrom: terminalizeFrom, rejection: ErrRunNotFailable,
			call: func(ctx context.Context, repo *Repository, enqueued EnqueueUserMessageResult, _ RunState) error {
				return repo.FailRun(ctx, enqueued.RunID, "tool_call_failed", "a later failure")
			},
		},
		{
			name: "FailRunWithEvent", to: lifecycleFailed, allowedFrom: terminalizeFrom, rejection: ErrRunNotFailable,
			call: func(ctx context.Context, repo *Repository, enqueued EnqueueUserMessageResult, _ RunState) error {
				_, err := repo.FailRunWithEvent(ctx, enqueued.RunID, "tool_call_failed", "a later failure", `{"runId":"raw"}`)
				return err
			},
		},
		{
			name: "FailRunWithEventPreservingExecution", to: lifecycleFailed, allowedFrom: terminalizeFrom, rejection: ErrRunNotFailable,
			call: func(ctx context.Context, repo *Repository, enqueued EnqueueUserMessageResult, _ RunState) error {
				_, err := repo.FailRunWithEventPreservingExecution(ctx, enqueued.RunID, "tool_call_failed", "a later failure", `{"runId":"raw"}`)
				return err
			},
		},
		{
			name: "CancelRunCanonical", to: lifecycleCancelled, allowedFrom: terminalizeFrom, rejection: ErrRunNotCancellable,
			call: func(ctx context.Context, repo *Repository, enqueued EnqueueUserMessageResult, state RunState) error {
				_, err := repo.CancelRunCanonical(ctx, CancelRunInput{
					RunID: enqueued.RunID, AssistantMessageID: enqueued.AssistantMessageID,
					ExpectedStateVersion: state.StateVersion, Cancellation: abandonedCancellationForTest(),
				})
				return err
			},
		},
		{
			name: "CancelRun", to: lifecycleCancelled, allowedFrom: terminalizeFrom, rejection: ErrRunNotCancellable, replaysExactly: true,
			call: func(ctx context.Context, repo *Repository, enqueued EnqueueUserMessageResult, _ RunState) error {
				return repo.CancelRun(ctx, enqueued.RunID, "client_cancelled")
			},
		},
		{
			name: "CancelRunWithEvent", to: lifecycleCancelled, allowedFrom: terminalizeFrom, rejection: ErrRunNotCancellable, replaysExactly: true,
			call: func(ctx context.Context, repo *Repository, enqueued EnqueueUserMessageResult, _ RunState) error {
				_, err := repo.CancelRunWithEvent(ctx, enqueued.RunID, "client_cancelled", `{"runId":"raw"}`)
				return err
			},
		},
	}
}

// TestRefusedTerminalCommandsReturnTheirOwnSentinel walks every exported
// terminal writer against every source lifecycle that writer refuses.
func TestRefusedTerminalCommandsReturnTheirOwnSentinel(t *testing.T) {
	sources := map[string]terminalSource{
		lifecycleQueued:    queuedTerminalSource,
		lifecycleCompleted: completedTerminalSource,
		lifecycleFailed:    failedTerminalSource,
		lifecycleCancelled: cancelledTerminalSource,
	}
	for sourceName, build := range sources {
		for _, command := range terminalCommands() {
			if slices.Contains(command.allowedFrom, sourceName) {
				continue
			}
			t.Run(sourceName+"/"+command.name, func(t *testing.T) {
				repo := New(openTestDB(t))
				ctx := context.Background()
				enqueued, state := build(t, repo)
				if state.Lifecycle != sourceName {
					t.Fatalf("source lifecycle = %q, want %q", state.Lifecycle, sourceName)
				}
				events := countRunEvents(t, repo, enqueued.RunID)

				err := command.call(ctx, repo, enqueued, state)
				if command.replaysExactly && sourceName == command.to {
					// The raw cancel adapters carry no content identity and no
					// expected version, so repeating one onto the run it already
					// cancelled is the write-free duplicate, not a refusal.
					if err != nil {
						t.Fatalf("identical raw replay = %v, want a write-free duplicate", err)
					}
				} else if !errors.Is(err, command.rejection) {
					t.Fatalf("%s from %s = %v, want %v", command.name, sourceName, err, command.rejection)
				}
				after, err := repo.GetRunState(ctx, enqueued.RunID)
				if err != nil {
					t.Fatal(err)
				}
				if after != state {
					t.Fatalf("refused %s changed the run: %+v, want %+v", command.name, after, state)
				}
				if count := countRunEvents(t, repo, enqueued.RunID); count != events {
					t.Fatalf("refused %s appended %d events", command.name, count-events)
				}
			})
		}
	}
}

// TestTerminalVersionConflictsStayTransitionConflicts is the other half of the
// contract. A command that names an allowed source but the wrong version has
// not hit a precondition on the run's lifecycle — it lost a race — and saying
// "not failable" there would tell a caller the run is finished when it is
// still going.
func TestTerminalVersionConflictsStayTransitionConflicts(t *testing.T) {
	for name, call := range map[string]func(ctx context.Context, repo *Repository, enqueued EnqueueUserMessageResult, version int64) error{
		"CompleteRunCanonical": func(ctx context.Context, repo *Repository, enqueued EnqueueUserMessageResult, version int64) error {
			_, err := repo.CompleteRunCanonical(ctx, CompleteRunInput{
				RunID: enqueued.RunID, AssistantMessageID: enqueued.AssistantMessageID,
				Content: "the answer", ExpectedStateVersion: version,
			})
			return err
		},
		"FailRunCanonical": func(ctx context.Context, repo *Repository, enqueued EnqueueUserMessageResult, version int64) error {
			_, err := repo.FailRunCanonical(ctx, FailRunInput{
				RunID: enqueued.RunID, AssistantMessageID: enqueued.AssistantMessageID,
				ExpectedStateVersion: version, Failure: providerFailureForTest(),
			})
			return err
		},
		"CancelRunCanonical": func(ctx context.Context, repo *Repository, enqueued EnqueueUserMessageResult, version int64) error {
			_, err := repo.CancelRunCanonical(ctx, CancelRunInput{
				RunID: enqueued.RunID, AssistantMessageID: enqueued.AssistantMessageID,
				ExpectedStateVersion: version, Cancellation: abandonedCancellationForTest(),
			})
			return err
		},
	} {
		t.Run(name, func(t *testing.T) {
			repo := New(openTestDB(t))
			ctx := context.Background()
			enqueued, _, running := runningRun(t, repo, "worker-version-conflict")

			err := call(ctx, repo, enqueued, running.StateVersion+1)
			if !errors.Is(err, ErrRunTransitionConflict) {
				t.Fatalf("%s at the wrong version = %v, want %v", name, err, ErrRunTransitionConflict)
			}
			for _, sentinel := range []error{ErrRunNotCompletable, ErrRunNotFailable, ErrRunNotCancellable} {
				if errors.Is(err, sentinel) {
					t.Fatalf("%s at the wrong version reported %v, which reads as a finished run", name, sentinel)
				}
			}
			state, err := repo.GetRunState(ctx, enqueued.RunID)
			if err != nil {
				t.Fatal(err)
			}
			if state != running {
				t.Fatalf("a lost race changed the run: %+v", state)
			}
		})
	}
}
