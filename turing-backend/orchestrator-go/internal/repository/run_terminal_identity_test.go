package repository

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/runoutcome"
)

// runningRun leaves a run running under a claimed assignment, which is the
// state every terminal report arrives in.
func runningRun(t *testing.T, repo *Repository, worker string) (EnqueueUserMessageResult, Job, RunState) {
	t.Helper()
	ctx := context.Background()
	enqueued := enqueueRun(t, repo, "Terminal identity")
	claimed, err := repo.ClaimNextJob(ctx, "general_assistant", worker)
	if err != nil {
		t.Fatalf("ClaimNextJob: %v", err)
	}
	state, err := repo.GetRunState(ctx, enqueued.RunID)
	if err != nil {
		t.Fatal(err)
	}
	return enqueued, claimed, state
}

func assistantContent(t *testing.T, repo *Repository, messageID string) string {
	t.Helper()
	var content string
	if err := repo.db.QueryRowContext(context.Background(),
		`SELECT content FROM messages WHERE id = ?`, messageID).Scan(&content); err != nil {
		t.Fatalf("read assistant content: %v", err)
	}
	return content
}

// TestCompleteRunPersistsExactContentIdentityAndDisplayability pins the three
// things a completion has to agree on at once: the bytes on the message, the
// digest duplicate detection compares, and the boolean a client renders from.
func TestCompleteRunPersistsExactContentIdentityAndDisplayability(t *testing.T) {
	for name, content := range map[string]struct {
		text           string
		wantDisplayble bool
		wantReason     string
	}{
		"ordinary text":            {"the answer", true, "none"},
		"text with surrounding ws": {"  padded  ", true, "none"},
		"zero width space":         {"\u200b", true, "none"},
		"replacement character":    {"\ufffd", true, "none"},
		"ideographic space only":   {"\u3000", false, "completed_no_content"},
		"non breaking space only":  {"\u00a0", false, "completed_no_content"},
		"empty":                    {"", false, "completed_no_content"},
	} {
		t.Run(name, func(t *testing.T) {
			repo := New(openTestDB(t))
			ctx := context.Background()
			enqueued, _, running := runningRun(t, repo, "worker-content")

			result, err := repo.CompleteRunCanonical(ctx, CompleteRunInput{
				RunID: enqueued.RunID, AssistantMessageID: enqueued.AssistantMessageID,
				Content: content.text, ExpectedStateVersion: running.StateVersion,
			})
			if err != nil {
				t.Fatalf("CompleteRunCanonical: %v", err)
			}
			if result.State.Lifecycle != lifecycleCompleted || result.State.OutcomeReason != content.wantReason {
				t.Fatalf("completed state = %s/%s, want completed/%s",
					result.State.Lifecycle, result.State.OutcomeReason, content.wantReason)
			}
			if result.State.HasDisplayableContent != content.wantDisplayble {
				t.Fatalf("hasDisplayableContent = %t, want %t", result.State.HasDisplayableContent, content.wantDisplayble)
			}
			// The exact bytes, not a trimmed or normalized rendering of them.
			if got := assistantContent(t, repo, enqueued.AssistantMessageID); got != content.text {
				t.Fatalf("persisted content = %q, want the exact reported bytes %q", got, content.text)
			}
			if want := runoutcome.ContentSHA256(content.text); result.State.ContentSHA256 != want {
				t.Fatalf("content digest = %q, want %q", result.State.ContentSHA256, want)
			}
			var stored string
			if err := repo.db.QueryRowContext(ctx,
				`SELECT assistant_content_sha256 FROM agent_runs WHERE id = ?`, enqueued.RunID).Scan(&stored); err != nil {
				t.Fatal(err)
			}
			if stored != runoutcome.ContentSHA256(content.text) {
				t.Fatalf("stored digest = %q, want the digest of the persisted bytes", stored)
			}
		})
	}
}

// TestWhitespaceOnlyExplicitSuccessCompletesWithoutContent says the thing this
// product got wrong before: a model that answered with nothing answered with
// nothing. The run completed, no fallback sentence is invented, and the empty
// content survives.
func TestWhitespaceOnlyExplicitSuccessCompletesWithoutContent(t *testing.T) {
	repo := New(openTestDB(t))
	ctx := context.Background()
	enqueued, _, running := runningRun(t, repo, "worker-silent")

	const whitespace = " \t\n"
	result, err := repo.CompleteRunCanonical(ctx, CompleteRunInput{
		RunID: enqueued.RunID, AssistantMessageID: enqueued.AssistantMessageID,
		Content: whitespace, ExpectedStateVersion: running.StateVersion,
	})
	if err != nil {
		t.Fatalf("CompleteRunCanonical: %v", err)
	}
	if result.State.Lifecycle != lifecycleCompleted || result.State.OutcomeReason != "completed_no_content" {
		t.Fatalf("state = %s/%s, want completed/completed_no_content",
			result.State.Lifecycle, result.State.OutcomeReason)
	}
	if got := assistantContent(t, repo, enqueued.AssistantMessageID); got != whitespace {
		t.Fatalf("persisted content = %q, want the original whitespace preserved", got)
	}
	// No message.completed event: there is no message to complete.
	for _, event := range result.Events {
		if event.Type == "message.completed" {
			t.Fatalf("a silent completion announced a message: %s", event.PayloadJSON)
		}
	}
}

// TestFailedAndCancelledRunsPreserveExistingAssistantContent covers the rule
// that failure never edits what the run already said, and never writes
// success-shaped text of its own.
func TestFailedAndCancelledRunsPreserveExistingAssistantContent(t *testing.T) {
	for name, terminalize := range map[string]func(context.Context, *Repository, EnqueueUserMessageResult, int64) (RunTransitionResult, error){
		"failed": func(ctx context.Context, repo *Repository, run EnqueueUserMessageResult, version int64) (RunTransitionResult, error) {
			return repo.FailRunCanonical(ctx, FailRunInput{
				RunID: run.RunID, AssistantMessageID: run.AssistantMessageID,
				ExpectedStateVersion: version, Failure: providerFailureForTest(),
			})
		},
		"cancelled": func(ctx context.Context, repo *Repository, run EnqueueUserMessageResult, version int64) (RunTransitionResult, error) {
			return repo.CancelRunCanonical(ctx, CancelRunInput{
				RunID: run.RunID, AssistantMessageID: run.AssistantMessageID,
				ExpectedStateVersion: version, Cancellation: abandonedCancellationForTest(),
			})
		},
	} {
		t.Run(name, func(t *testing.T) {
			repo := New(openTestDB(t))
			ctx := context.Background()
			enqueued, _, _ := runningRun(t, repo, "worker-partial")

			// Content already durable when the run goes wrong.
			const durable = "here is what I found so far"
			if _, err := repo.db.ExecContext(ctx,
				`UPDATE messages SET content = ? WHERE id = ?`, durable, enqueued.AssistantMessageID); err != nil {
				t.Fatal(err)
			}
			digest := runoutcome.ContentSHA256(durable)
			if _, err := repo.db.ExecContext(ctx,
				`UPDATE agent_runs SET assistant_content_sha256 = ? WHERE id = ?`, digest, enqueued.RunID); err != nil {
				t.Fatal(err)
			}
			before, err := repo.GetRunState(ctx, enqueued.RunID)
			if err != nil {
				t.Fatal(err)
			}

			result, err := terminalize(ctx, repo, enqueued, before.StateVersion)
			if err != nil {
				t.Fatalf("terminalize: %v", err)
			}
			if got := assistantContent(t, repo, enqueued.AssistantMessageID); got != durable {
				t.Fatalf("assistant content = %q, want the durable content preserved byte for byte", got)
			}
			if result.State.ContentSHA256 != digest {
				t.Fatalf("content digest = %q, want the preserved content's digest %q", result.State.ContentSHA256, digest)
			}
			if !result.State.HasDisplayableContent {
				t.Fatal("a terminal run with durable content reported none")
			}
		})
	}
}

// TestExactDuplicateTerminalReportIsAWriteFreeSuccess covers the worker that
// reported success, lost the acknowledgement, and reported it again.
func TestExactDuplicateTerminalReportIsAWriteFreeSuccess(t *testing.T) {
	repo := New(openTestDB(t))
	ctx := context.Background()
	enqueued, _, running := runningRun(t, repo, "worker-duplicate")

	report := CompleteRunInput{
		RunID: enqueued.RunID, AssistantMessageID: enqueued.AssistantMessageID,
		Content: "the answer", ExpectedStateVersion: running.StateVersion,
	}
	first, err := repo.CompleteRunCanonical(ctx, report)
	if err != nil {
		t.Fatalf("CompleteRunCanonical: %v", err)
	}
	events := countRunEvents(t, repo, enqueued.RunID)

	second, err := repo.CompleteRunCanonical(ctx, report)
	if err != nil {
		t.Fatalf("duplicate CompleteRunCanonical: %v", err)
	}
	if !second.Duplicate {
		t.Fatal("an identical terminal report was not recognized as a duplicate")
	}
	if len(second.Events) != 0 {
		t.Fatalf("duplicate report returned %d events, want none", len(second.Events))
	}
	if second.State != first.State {
		t.Fatalf("duplicate report state = %+v, want the original %+v", second.State, first.State)
	}
	// Spelled out rather than left to the struct comparison above: these are
	// the parts of the canonical identity a duplicate has to reproduce, and a
	// field quietly dropped from the projection would still compare equal to
	// itself.
	if first.State.StateVersion != report.ExpectedStateVersion+1 {
		t.Fatalf("resulting version = %d, want one past the expected %d",
			first.State.StateVersion, report.ExpectedStateVersion)
	}
	if first.State.Lifecycle != lifecycleCompleted || first.State.OutcomeReason != string(runoutcome.ReasonNone) {
		t.Fatalf("terminal state = %s/%s, want completed with no outcome to report",
			first.State.Lifecycle, first.State.OutcomeReason)
	}
	if first.State.AssistantMessageID != enqueued.AssistantMessageID {
		t.Fatalf("assistant message = %q, want %q", first.State.AssistantMessageID, enqueued.AssistantMessageID)
	}
	if first.State.ContentSHA256 != runoutcome.ContentSHA256(report.Content) || !first.State.HasDisplayableContent {
		t.Fatalf("content identity = %q/%v, want the hash of %q and displayable",
			first.State.ContentSHA256, first.State.HasDisplayableContent, report.Content)
	}
	if got := assistantContent(t, repo, enqueued.AssistantMessageID); got != report.Content {
		t.Fatalf("persisted bytes = %q, want the reported %q", got, report.Content)
	}
	if after := countRunEvents(t, repo, enqueued.RunID); after != events {
		t.Fatalf("duplicate report appended %d events", after-events)
	}
}

// TestDuplicateCompletionWithDifferentBytesIsFenced is the other half. Two
// different answers cannot both be the run's output, and the first one wins
// because it is already the durable history.
func TestDuplicateCompletionWithDifferentBytesIsFenced(t *testing.T) {
	repo := New(openTestDB(t))
	ctx := context.Background()
	enqueued, _, running := runningRun(t, repo, "worker-conflict")

	if _, err := repo.CompleteRunCanonical(ctx, CompleteRunInput{
		RunID: enqueued.RunID, AssistantMessageID: enqueued.AssistantMessageID,
		Content: "the answer", ExpectedStateVersion: running.StateVersion,
	}); err != nil {
		t.Fatalf("CompleteRunCanonical: %v", err)
	}
	committed, err := repo.GetRunState(ctx, enqueued.RunID)
	if err != nil {
		t.Fatal(err)
	}
	events := countRunEvents(t, repo, enqueued.RunID)

	if _, err := repo.CompleteRunCanonical(ctx, CompleteRunInput{
		RunID: enqueued.RunID, AssistantMessageID: enqueued.AssistantMessageID,
		Content: "a different answer", ExpectedStateVersion: running.StateVersion,
	}); err == nil {
		t.Fatal("a second completion with different bytes was accepted")
	}
	if got := assistantContent(t, repo, enqueued.AssistantMessageID); got != "the answer" {
		t.Fatalf("assistant content = %q, want the first committed answer", got)
	}
	state, err := repo.GetRunState(ctx, enqueued.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if state != committed {
		t.Fatalf("fenced duplicate changed the row: %+v", state)
	}
	if after := countRunEvents(t, repo, enqueued.RunID); after != events {
		t.Fatalf("fenced duplicate appended %d events", after-events)
	}
}

// TestDuplicateTerminalWithDifferentReasonAssistantOrVersionIsFenced walks the
// rest of the canonical tuple. Matching the lifecycle is not enough.
func TestDuplicateTerminalWithDifferentReasonAssistantOrVersionIsFenced(t *testing.T) {
	repo := New(openTestDB(t))
	ctx := context.Background()
	enqueued, _, running := runningRun(t, repo, "worker-tuple")

	if _, err := repo.FailRunCanonical(ctx, FailRunInput{
		RunID: enqueued.RunID, AssistantMessageID: enqueued.AssistantMessageID,
		ExpectedStateVersion: running.StateVersion, Failure: providerFailureForTest(),
	}); err != nil {
		t.Fatalf("FailRunCanonical: %v", err)
	}
	committed, err := repo.GetRunState(ctx, enqueued.RunID)
	if err != nil {
		t.Fatal(err)
	}
	events := countRunEvents(t, repo, enqueued.RunID)

	// The identical report replays for free, which is what makes each
	// difference below meaningful rather than incidental.
	replay, err := repo.FailRunCanonical(ctx, FailRunInput{
		RunID: enqueued.RunID, AssistantMessageID: enqueued.AssistantMessageID,
		ExpectedStateVersion: running.StateVersion, Failure: providerFailureForTest(),
	})
	if err != nil || !replay.Duplicate {
		t.Fatalf("identical failure replay = (%+v, %v), want a write-free duplicate", replay, err)
	}

	for name, conflicting := range map[string]FailRunInput{
		"a different reason": {
			RunID: enqueued.RunID, AssistantMessageID: enqueued.AssistantMessageID,
			ExpectedStateVersion: running.StateVersion,
			Failure:              runoutcome.NormalizeFailure(runoutcome.OriginToolExecution, "tool_call_failed", runoutcome.RetryClassNever),
		},
		"a different assistant message": {
			RunID: enqueued.RunID, AssistantMessageID: "msg_someone_else",
			ExpectedStateVersion: running.StateVersion, Failure: providerFailureForTest(),
		},
		"a different expected version": {
			RunID: enqueued.RunID, AssistantMessageID: enqueued.AssistantMessageID,
			ExpectedStateVersion: running.StateVersion + 1, Failure: providerFailureForTest(),
		},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := repo.FailRunCanonical(ctx, conflicting); err == nil {
				t.Fatalf("a duplicate with %s was accepted", name)
			}
			state, err := repo.GetRunState(ctx, enqueued.RunID)
			if err != nil {
				t.Fatal(err)
			}
			if state != committed {
				t.Fatalf("%s changed the row: %+v", name, state)
			}
			if after := countRunEvents(t, repo, enqueued.RunID); after != events {
				t.Fatalf("%s appended %d events", name, after-events)
			}
		})
	}
}

// TestCompletionCancellationAndRecoveryRacesLinearize proves the guarded row
// update is the ordering authority. The competing writers start together at a
// barrier; exactly one wins, and the losers change nothing.
func TestCompletionCancellationAndRecoveryRacesLinearize(t *testing.T) {
	repo := New(openTestDB(t))
	ctx := context.Background()
	enqueued, claimed, running := runningRun(t, repo, "worker-race")

	type outcome struct {
		name string
		err  error
	}
	competitors := []struct {
		name string
		run  func() error
	}{
		{"complete", func() error {
			_, err := repo.CompleteRunCanonical(ctx, CompleteRunInput{
				RunID: enqueued.RunID, AssistantMessageID: enqueued.AssistantMessageID,
				Content: "raced answer", ExpectedStateVersion: running.StateVersion,
			})
			return err
		}},
		{"cancel", func() error {
			_, err := repo.CancelRunCanonical(ctx, CancelRunInput{
				RunID: enqueued.RunID, AssistantMessageID: enqueued.AssistantMessageID,
				ExpectedStateVersion: running.StateVersion, Cancellation: abandonedCancellationForTest(),
			})
			return err
		}},
		{"fence into recovery", func() error {
			_, err := repo.FenceRunOwnership(ctx, FenceRunOwnershipInput{
				RunID: enqueued.RunID, ExpectedStateVersion: running.StateVersion,
				WorkerID: "worker-race", AssignmentAttemptID: claimed.AssignmentAttemptID,
			})
			return err
		}},
	}

	// A barrier rather than a sleep: every goroutine is released at the same
	// instant, so the transaction boundary is what decides, not the scheduler.
	var release sync.WaitGroup
	release.Add(1)
	var finished sync.WaitGroup
	results := make(chan outcome, len(competitors))
	for _, competitor := range competitors {
		finished.Add(1)
		go func() {
			defer finished.Done()
			release.Wait()
			results <- outcome{name: competitor.name, err: competitor.run()}
		}()
	}
	release.Done()
	finished.Wait()
	close(results)

	var winners []string
	for result := range results {
		if result.err == nil {
			winners = append(winners, result.name)
		}
	}
	if len(winners) != 1 {
		t.Fatalf("competing transitions produced %d winners (%v), want exactly 1", len(winners), winners)
	}
	state, err := repo.GetRunState(ctx, enqueued.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if state.StateVersion != running.StateVersion+1 {
		t.Fatalf("version = %d, want exactly one increment past %d", state.StateVersion, running.StateVersion)
	}
	wantLifecycle := map[string]string{
		"complete":            lifecycleCompleted,
		"cancel":              lifecycleCancelled,
		"fence into recovery": lifecycleRecovering,
	}[winners[0]]
	if state.Lifecycle != wantLifecycle {
		t.Fatalf("lifecycle = %q, want %q from the winning %s", state.Lifecycle, wantLifecycle, winners[0])
	}
}

// TestTerminalTransitionAppendsExactlyOneTerminalEvent guards against the
// projection being written twice — once as the terminal event and once as a
// state-changed event beside it.
func TestTerminalTransitionAppendsExactlyOneTerminalEvent(t *testing.T) {
	for name, terminal := range map[string]struct {
		commit    func(context.Context, *Repository, EnqueueUserMessageResult, int64) (RunTransitionResult, error)
		eventType string
	}{
		"completed": {
			commit: func(ctx context.Context, repo *Repository, run EnqueueUserMessageResult, version int64) (RunTransitionResult, error) {
				return repo.CompleteRunCanonical(ctx, CompleteRunInput{
					RunID: run.RunID, AssistantMessageID: run.AssistantMessageID,
					Content: "done", ExpectedStateVersion: version,
				})
			},
			eventType: "agent.run.completed",
		},
		"failed": {
			commit: func(ctx context.Context, repo *Repository, run EnqueueUserMessageResult, version int64) (RunTransitionResult, error) {
				return repo.FailRunCanonical(ctx, FailRunInput{
					RunID: run.RunID, AssistantMessageID: run.AssistantMessageID,
					ExpectedStateVersion: version, Failure: providerFailureForTest(),
				})
			},
			eventType: "agent.run.failed",
		},
		"cancelled": {
			commit: func(ctx context.Context, repo *Repository, run EnqueueUserMessageResult, version int64) (RunTransitionResult, error) {
				return repo.CancelRunCanonical(ctx, CancelRunInput{
					RunID: run.RunID, AssistantMessageID: run.AssistantMessageID,
					ExpectedStateVersion: version, Cancellation: abandonedCancellationForTest(),
				})
			},
			eventType: "agent.run.cancelled",
		},
	} {
		t.Run(name, func(t *testing.T) {
			repo := New(openTestDB(t))
			ctx := context.Background()
			enqueued, _, running := runningRun(t, repo, "worker-one-event")

			result, err := terminal.commit(ctx, repo, enqueued, running.StateVersion)
			if err != nil {
				t.Fatalf("terminalize: %v", err)
			}
			var terminalEvents, stateChanged int
			for _, event := range result.Events {
				switch event.Type {
				case terminal.eventType:
					terminalEvents++
				case runStateChangedEventType:
					stateChanged++
				}
			}
			if terminalEvents != 1 {
				t.Fatalf("appended %d %s events, want exactly 1", terminalEvents, terminal.eventType)
			}
			if stateChanged != 0 {
				t.Fatalf("a terminal transition also appended %d redundant %s events", stateChanged, runStateChangedEventType)
			}
			var durable int
			if err := repo.db.QueryRowContext(ctx,
				`SELECT COUNT(*) FROM events WHERE run_id = ? AND type IN (?, ?)`,
				enqueued.RunID, terminal.eventType, runStateChangedEventType).Scan(&durable); err != nil {
				t.Fatal(err)
			}
			if durable != 1 {
				t.Fatalf("durable log holds %d terminal/state events, want exactly 1", durable)
			}
			// The terminal event is the projection, so it has to carry the
			// state a reopening client will reconcile against.
			for _, event := range result.Events {
				if event.Type == terminal.eventType {
					assertSnapshotMatchesState(t, decodeRunStateSnapshot(t, event), result.State)
				}
			}
		})
	}
}

// TestAllSevenTemporaryRawTerminalAdaptersDelegateToCanonicalWriters covers the
// compatibility boundary this commit deliberately leaves standing. Each adapter
// still has its old signature, carries no expected version, and must still
// produce a canonical versioned terminal state — resolved inside the guarded
// transaction, never by reading the version first.
func TestClaimingAJobForANonQueuedRunFailsInsteadOfPanicking(t *testing.T) {
	repo := New(openTestDB(t))
	ctx := context.Background()
	enqueued := enqueueRun(t, repo, "Claim guard")

	// A pending job whose run is no longer queued: the inconsistency the guard
	// exists for, reached here by moving the run directly.
	if _, err := repo.db.ExecContext(ctx,
		`UPDATE agent_runs SET status = 'running' WHERE id = ?`, enqueued.RunID); err != nil {
		t.Fatal(err)
	}
	claimed, err := repo.ClaimNextJob(ctx, "general_assistant", "worker-guard")
	if err != nil {
		t.Fatalf("ClaimNextJob: %v", err)
	}
	if claimed.JobID != "" {
		t.Fatalf("claimed %+v for a run that is not queued", claimed)
	}
}

// TestTerminalReportsFenceAForeignAssistantMessage covers the identity a
// failure and a cancellation still carry even though neither writes content.
//
// A terminal report names the assistant message it is finishing. If that name
// does not match the message this run owns, the report is about some other run
// — a stale worker holding an old assignment, or a caller that mixed up two
// runs — and committing it would terminalize the wrong conversation while
// quietly leaving the named message alone. Completion is fenced by the same
// rule, so the check does not depend on whether bytes happen to be written.
func TestTerminalReportsFenceAForeignAssistantMessage(t *testing.T) {
	const foreignMessageID = "msg_belongs_to_another_run"
	for name, report := range map[string]func(ctx context.Context, repo *Repository, runID string, assistantMessageID string, version int64) error{
		"FailRunCanonical": func(ctx context.Context, repo *Repository, runID string, assistantMessageID string, version int64) error {
			_, err := repo.FailRunCanonical(ctx, FailRunInput{
				RunID: runID, AssistantMessageID: assistantMessageID,
				ExpectedStateVersion: version, Failure: providerFailureForTest(),
			})
			return err
		},
		"CancelRunCanonical": func(ctx context.Context, repo *Repository, runID string, assistantMessageID string, version int64) error {
			_, err := repo.CancelRunCanonical(ctx, CancelRunInput{
				RunID: runID, AssistantMessageID: assistantMessageID,
				ExpectedStateVersion: version, Cancellation: abandonedCancellationForTest(),
			})
			return err
		},
		"CompleteRunCanonical": func(ctx context.Context, repo *Repository, runID string, assistantMessageID string, version int64) error {
			_, err := repo.CompleteRunCanonical(ctx, CompleteRunInput{
				RunID: runID, AssistantMessageID: assistantMessageID,
				Content: "the answer", ExpectedStateVersion: version,
			})
			return err
		},
	} {
		t.Run(name, func(t *testing.T) {
			repo := New(openTestDB(t))
			ctx := context.Background()
			enqueued, _, running := runningRun(t, repo, "worker-foreign-message")
			events := countRunEvents(t, repo, enqueued.RunID)

			err := report(ctx, repo, enqueued.RunID, foreignMessageID, running.StateVersion)
			if !errors.Is(err, ErrRunTransitionConflict) {
				t.Fatalf("%s naming another run's assistant message = %v, want %v", name, err, ErrRunTransitionConflict)
			}
			// The run is still running, so nothing may read as finished.
			assertUnchangedTerminalIdentity(t, repo, enqueued, running, events, name, err, foreignMessageID)
		})
	}
}

// TestDuplicateTerminalReplayWithAForeignAssistantMessageIsAConflict is the
// same rule on the replay path. The run is already terminal, so a refusal here
// is easy to answer with "this run cannot fail" — but that sentinel tells a
// caller its report was simply late, when in fact the report was about a
// different run and its own run is still unfinished.
func TestDuplicateTerminalReplayWithAForeignAssistantMessageIsAConflict(t *testing.T) {
	const foreignMessageID = "msg_belongs_to_another_run"
	for name, replay := range map[string]struct {
		terminalize func(ctx context.Context, repo *Repository, enqueued EnqueueUserMessageResult, version int64) error
		repeat      func(ctx context.Context, repo *Repository, runID string, assistantMessageID string, version int64) error
	}{
		"failure": {
			terminalize: func(ctx context.Context, repo *Repository, enqueued EnqueueUserMessageResult, version int64) error {
				_, err := repo.FailRunCanonical(ctx, FailRunInput{
					RunID: enqueued.RunID, AssistantMessageID: enqueued.AssistantMessageID,
					ExpectedStateVersion: version, Failure: providerFailureForTest(),
				})
				return err
			},
			repeat: func(ctx context.Context, repo *Repository, runID string, assistantMessageID string, version int64) error {
				_, err := repo.FailRunCanonical(ctx, FailRunInput{
					RunID: runID, AssistantMessageID: assistantMessageID,
					ExpectedStateVersion: version, Failure: providerFailureForTest(),
				})
				return err
			},
		},
		"cancellation": {
			terminalize: func(ctx context.Context, repo *Repository, enqueued EnqueueUserMessageResult, version int64) error {
				_, err := repo.CancelRunCanonical(ctx, CancelRunInput{
					RunID: enqueued.RunID, AssistantMessageID: enqueued.AssistantMessageID,
					ExpectedStateVersion: version, Cancellation: abandonedCancellationForTest(),
				})
				return err
			},
			repeat: func(ctx context.Context, repo *Repository, runID string, assistantMessageID string, version int64) error {
				_, err := repo.CancelRunCanonical(ctx, CancelRunInput{
					RunID: runID, AssistantMessageID: assistantMessageID,
					ExpectedStateVersion: version, Cancellation: abandonedCancellationForTest(),
				})
				return err
			},
		},
	} {
		t.Run(name, func(t *testing.T) {
			repo := New(openTestDB(t))
			ctx := context.Background()
			enqueued, _, running := runningRun(t, repo, "worker-foreign-replay")
			if err := replay.terminalize(ctx, repo, enqueued, running.StateVersion); err != nil {
				t.Fatalf("terminalize: %v", err)
			}
			committed, err := repo.GetRunState(ctx, enqueued.RunID)
			if err != nil {
				t.Fatal(err)
			}
			events := countRunEvents(t, repo, enqueued.RunID)

			err = replay.repeat(ctx, repo, enqueued.RunID, foreignMessageID, running.StateVersion)
			if !errors.Is(err, ErrRunTransitionConflict) {
				t.Fatalf("%s replay naming another run's assistant message = %v, want %v", name, err, ErrRunTransitionConflict)
			}
			assertUnchangedTerminalIdentity(t, repo, enqueued, committed, events, name, err, foreignMessageID)
		})
	}
}

// assertUnchangedTerminalIdentity checks the two things a fenced report owes:
// it changed nothing, and it said so without naming either message. An error
// carrying the run's own assistant message ID hands a caller who guessed wrong
// the identity it failed to guess.
func assertUnchangedTerminalIdentity(
	t *testing.T,
	repo *Repository,
	enqueued EnqueueUserMessageResult,
	want RunState,
	wantEvents int,
	name string,
	err error,
	foreignMessageID string,
) {
	t.Helper()
	ctx := context.Background()
	state, stateErr := repo.GetRunState(ctx, enqueued.RunID)
	if stateErr != nil {
		t.Fatal(stateErr)
	}
	if state != want {
		t.Fatalf("fenced %s changed the run: %+v, want %+v", name, state, want)
	}
	if after := countRunEvents(t, repo, enqueued.RunID); after != wantEvents {
		t.Fatalf("fenced %s appended %d events", name, after-wantEvents)
	}
	if got := assistantContent(t, repo, enqueued.AssistantMessageID); got != "" {
		t.Fatalf("fenced %s wrote assistant content %q", name, got)
	}
	for _, secret := range []string{enqueued.AssistantMessageID, foreignMessageID} {
		if strings.Contains(err.Error(), secret) {
			t.Fatalf("fenced %s error %q leaks a message identity", name, err)
		}
	}
}

// ---------------------------------------------------------------------------
// Terminal helpers for tests that are exercising something other than version
// identity.
//
// Every production caller names the version its report was computed against,
// because that is what fences a report built from a state the run has left. A
// test setting up a terminalized run has no such report; it reads the run's
// current version here rather than restating it at forty call sites.
// ---------------------------------------------------------------------------

func completeRunAtCurrentVersion(t *testing.T, repo *Repository, runID string, assistantMessageID string, content string, usage *RunTokenUsage) (RunTransitionResult, error) {
	t.Helper()
	return repo.CompleteRunCanonical(context.Background(), CompleteRunInput{
		RunID:                runID,
		AssistantMessageID:   assistantMessageID,
		Content:              content,
		ExpectedStateVersion: currentVersion(t, repo, runID),
		Usage:                usage,
	})
}

func failRunAtCurrentVersion(t *testing.T, repo *Repository, runID string, failure runoutcome.Failure) (RunTransitionResult, error) {
	t.Helper()
	return repo.FailRunCanonical(context.Background(), FailRunInput{
		RunID:                runID,
		ExpectedStateVersion: currentVersion(t, repo, runID),
		Failure:              failure,
	})
}

func failRunPreservingExecutionAtCurrentVersion(t *testing.T, repo *Repository, runID string, failure runoutcome.Failure) (RunTransitionResult, error) {
	t.Helper()
	return repo.FailRunCanonical(context.Background(), FailRunInput{
		RunID:                runID,
		ExpectedStateVersion: currentVersion(t, repo, runID),
		Failure:              failure,
		PreserveExecution:    true,
	})
}

func cancelRunAtCurrentVersion(t *testing.T, repo *Repository, runID string) (RunTransitionResult, error) {
	t.Helper()
	return repo.CancelRunCanonical(context.Background(), CancelRunInput{
		RunID:                runID,
		ExpectedStateVersion: currentVersion(t, repo, runID),
		Cancellation:         runoutcome.AbandonedCancellation(),
	})
}

// currentVersion reads a run's version without failing the test when the run is
// missing: some callers deliberately terminalize a run that is already gone and
// expect the writer's own sentinel, not a helper's.
func currentVersion(t *testing.T, repo *Repository, runID string) int64 {
	t.Helper()
	state, err := repo.GetRunState(context.Background(), runID)
	if err != nil {
		return 1
	}
	return state.StateVersion
}

// testFailure builds the normalized failure a fixture wants from the code it
// already names, pairing it with the origin that code actually comes from.
//
// It goes through the real constructor rather than assembling a value directly,
// so a fixture cannot express an origin/code pair production code could not,
// and an unpaired code fails closed here exactly as it would in production.
func testFailure(code string) runoutcome.Failure {
	origins := map[string]runoutcome.Origin{
		"tool_call_failed":         runoutcome.OriginToolExecution,
		"model_stream_failed":      runoutcome.OriginProviderTransport,
		"model_error":              runoutcome.OriginExternalProvider,
		"runtime_error":            runoutcome.OriginWorkerRuntime,
		"approval_delivery_failed": runoutcome.OriginApprovalTransport,
	}
	origin, known := origins[code]
	if !known {
		origin = runoutcome.OriginOrchestratorInternal
	}
	return runoutcome.NormalizeFailure(origin, code, runoutcome.RetryClassNever)
}

// dispatchCondition is the nonterminal dispatch report a requeue is about. It
// carries the transient retry class, because a condition that cannot be retried
// is not what these fixtures are exercising.
func dispatchCondition(code string) runoutcome.Failure {
	return runoutcome.NormalizeFailure(runoutcome.OriginDispatch, code, runoutcome.RetryClassSameRunTransient)
}

// completeRunEvents and cancelRunEvents keep the shape callers had before the
// terminal writers required a version: the events the transition appended.
func completeRunEvents(t *testing.T, repo *Repository, runID string, assistantMessageID string, content string, usage *RunTokenUsage) ([]Event, error) {
	t.Helper()
	result, err := completeRunAtCurrentVersion(t, repo, runID, assistantMessageID, content, usage)
	return result.Events, err
}

func cancelRunEvents(t *testing.T, repo *Repository, runID string) ([]Event, error) {
	t.Helper()
	result, err := cancelRunAtCurrentVersion(t, repo, runID)
	return result.Events, err
}
