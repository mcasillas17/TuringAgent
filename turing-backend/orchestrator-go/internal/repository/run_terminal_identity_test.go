package repository

import (
	"context"
	"errors"
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
func TestAllSevenTemporaryRawTerminalAdaptersDelegateToCanonicalWriters(t *testing.T) {
	adapters := map[string]struct {
		call          func(context.Context, *Repository, EnqueueUserMessageResult) error
		wantLifecycle string
		wantReason    string
	}{
		"CompleteRun": {
			call: func(ctx context.Context, repo *Repository, run EnqueueUserMessageResult) error {
				return repo.CompleteRun(ctx, run.RunID, run.AssistantMessageID, "done")
			},
			wantLifecycle: lifecycleCompleted, wantReason: "none",
		},
		"CompleteRunWithEvent": {
			call: func(ctx context.Context, repo *Repository, run EnqueueUserMessageResult) error {
				_, err := repo.CompleteRunWithEvent(ctx, run.RunID, run.AssistantMessageID, "done", `{"runId":"x"}`, nil)
				return err
			},
			wantLifecycle: lifecycleCompleted, wantReason: "none",
		},
		"FailRun": {
			call: func(ctx context.Context, repo *Repository, run EnqueueUserMessageResult) error {
				return repo.FailRun(ctx, run.RunID, "model_stream_failed", "the stream died")
			},
			wantLifecycle: lifecycleFailed, wantReason: "provider_failure",
		},
		"FailRunWithEvent": {
			call: func(ctx context.Context, repo *Repository, run EnqueueUserMessageResult) error {
				_, err := repo.FailRunWithEvent(ctx, run.RunID, "model_stream_failed", "the stream died", `{"runId":"x"}`)
				return err
			},
			wantLifecycle: lifecycleFailed, wantReason: "provider_failure",
		},
		"FailRunWithEventPreservingExecution": {
			call: func(ctx context.Context, repo *Repository, run EnqueueUserMessageResult) error {
				_, err := repo.FailRunWithEventPreservingExecution(ctx, run.RunID, "runtime_error", "worker blew up", `{"runId":"x"}`)
				return err
			},
			wantLifecycle: lifecycleFailed, wantReason: "internal_failure",
		},
		"CancelRun": {
			call: func(ctx context.Context, repo *Repository, run EnqueueUserMessageResult) error {
				return repo.CancelRun(ctx, run.RunID, "client_cancelled")
			},
			wantLifecycle: lifecycleCancelled, wantReason: "abandoned",
		},
		"CancelRunWithEvent": {
			call: func(ctx context.Context, repo *Repository, run EnqueueUserMessageResult) error {
				_, err := repo.CancelRunWithEvent(ctx, run.RunID, "client_cancelled", `{"reason":"client_cancelled"}`)
				return err
			},
			wantLifecycle: lifecycleCancelled, wantReason: "abandoned",
		},
	}
	if len(adapters) != 7 {
		t.Fatalf("covering %d raw adapters, want all 7", len(adapters))
	}

	for name, adapter := range adapters {
		t.Run(name, func(t *testing.T) {
			repo := New(openTestDB(t))
			ctx := context.Background()
			enqueued, _, running := runningRun(t, repo, "worker-adapter")

			if err := adapter.call(ctx, repo, enqueued); err != nil {
				t.Fatalf("%s: %v", name, err)
			}
			state, err := repo.GetRunState(ctx, enqueued.RunID)
			if err != nil {
				t.Fatal(err)
			}
			if state.Lifecycle != adapter.wantLifecycle || state.OutcomeReason != adapter.wantReason {
				t.Fatalf("%s produced %s/%s, want %s/%s",
					name, state.Lifecycle, state.OutcomeReason, adapter.wantLifecycle, adapter.wantReason)
			}
			if state.StateVersion != running.StateVersion+1 {
				t.Fatalf("%s produced version %d, want %d — the adapter must resolve the expectation, not skip it",
					name, state.StateVersion, running.StateVersion+1)
			}
			if !state.FinishedAt.Valid {
				t.Fatalf("%s left finished_at unset", name)
			}
			// Every adapter appends exactly one terminal event, whether or not
			// its signature hands it back: a lifecycle change nobody can
			// observe is the defect this task removes.
			var terminalEvents int
			if err := repo.db.QueryRowContext(ctx, `
				SELECT COUNT(*) FROM events
				WHERE run_id = ? AND type IN ('agent.run.completed','agent.run.failed','agent.run.cancelled')
			`, enqueued.RunID).Scan(&terminalEvents); err != nil {
				t.Fatal(err)
			}
			if terminalEvents != 1 {
				t.Fatalf("%s appended %d terminal events, want exactly 1", name, terminalEvents)
			}
		})
	}

	// The adapters carry no expected version, so the guarded update inside their
	// transaction is the only thing that can fence them. Both writers are
	// released together from a barrier.
	t.Run("concurrent raw adapters fence", func(t *testing.T) {
		repo := New(openTestDB(t))
		ctx := context.Background()
		enqueued, _, running := runningRun(t, repo, "worker-adapter-race")

		var release sync.WaitGroup
		release.Add(1)
		var finished sync.WaitGroup
		finished.Add(2)
		var completeErr, cancelErr error
		go func() {
			defer finished.Done()
			release.Wait()
			completeErr = repo.CompleteRun(ctx, enqueued.RunID, enqueued.AssistantMessageID, "done")
		}()
		go func() {
			defer finished.Done()
			release.Wait()
			cancelErr = repo.CancelRun(ctx, enqueued.RunID, "client_cancelled")
		}()
		release.Done()
		finished.Wait()

		if (completeErr == nil) == (cancelErr == nil) {
			t.Fatalf("competing raw adapters both %s (complete=%v cancel=%v)",
				map[bool]string{true: "won", false: "lost"}[completeErr == nil], completeErr, cancelErr)
		}
		state, err := repo.GetRunState(ctx, enqueued.RunID)
		if err != nil {
			t.Fatal(err)
		}
		if state.StateVersion != running.StateVersion+1 {
			t.Fatalf("version = %d, want exactly one increment past %d", state.StateVersion, running.StateVersion)
		}
		if completeErr == nil && state.Lifecycle != lifecycleCompleted {
			t.Fatalf("completion won but lifecycle = %q", state.Lifecycle)
		}
		if cancelErr == nil && state.Lifecycle != lifecycleCancelled {
			t.Fatalf("cancellation won but lifecycle = %q", state.Lifecycle)
		}
	})

	// An adapter racing a versioned fence is the other shape: completing a
	// recovering run is legal, so both may commit — but only in sequence, each
	// against the version it actually observed, and never by losing one of the
	// two updates.
	t.Run("concurrent transitions never lose an update", func(t *testing.T) {
		repo := New(openTestDB(t))
		ctx := context.Background()
		enqueued, claimed, running := runningRun(t, repo, "worker-adapter-fence")

		var release sync.WaitGroup
		release.Add(1)
		var finished sync.WaitGroup
		finished.Add(2)
		var adapterErr, fenceErr error
		go func() {
			defer finished.Done()
			release.Wait()
			adapterErr = repo.CompleteRun(ctx, enqueued.RunID, enqueued.AssistantMessageID, "done")
		}()
		go func() {
			defer finished.Done()
			release.Wait()
			_, fenceErr = repo.FenceRunOwnership(ctx, FenceRunOwnershipInput{
				RunID: enqueued.RunID, ExpectedStateVersion: running.StateVersion,
				WorkerID: "worker-adapter-fence", AssignmentAttemptID: claimed.AssignmentAttemptID,
			})
		}()
		release.Done()
		finished.Wait()

		committed := 0
		for _, err := range []error{adapterErr, fenceErr} {
			if err == nil {
				committed++
			}
		}
		if committed == 0 {
			t.Fatalf("neither writer committed (adapter=%v fence=%v)", adapterErr, fenceErr)
		}
		state, err := repo.GetRunState(ctx, enqueued.RunID)
		if err != nil {
			t.Fatal(err)
		}
		if state.StateVersion != running.StateVersion+int64(committed) {
			t.Fatalf("version = %d after %d committed transitions, want %d — an update was lost or duplicated",
				state.StateVersion, committed, running.StateVersion+int64(committed))
		}
		if adapterErr == nil && state.Lifecycle != lifecycleCompleted {
			t.Fatalf("the adapter committed but lifecycle = %q", state.Lifecycle)
		}
		if adapterErr != nil {
			// It lost, so it must have lost on the guard rather than by
			// silently overwriting a row it never observed.
			if !errors.Is(adapterErr, ErrRunNotCompletable) && !errors.Is(adapterErr, ErrRunTransitionConflict) {
				t.Fatalf("losing adapter = %v, want a fenced terminal error", adapterErr)
			}
		}
		if fenceErr != nil && !errors.Is(fenceErr, ErrRunTransitionConflict) {
			t.Fatalf("losing fence = %v, want ErrRunTransitionConflict", fenceErr)
		}
	})
}

// TestRawEventPayloadThatIsNotAnObjectFailsLoud covers the temporary adapters'
// one decoding assumption.
//
// Returning an empty payload for an undecodable one would publish a terminal
// event missing the code its reader expects, and the caller would never learn
// that its own keys had been dropped on the floor.
func TestRawEventPayloadThatIsNotAnObjectFailsLoud(t *testing.T) {
	repo := New(openTestDB(t))
	ctx := context.Background()
	enqueued, _, running := runningRun(t, repo, "worker-bad-payload")

	if _, err := repo.FailRunWithEvent(ctx, enqueued.RunID, "model_stream_failed", "died", `"just a string"`); !errors.Is(err, ErrRawEventPayload) {
		t.Fatalf("failure with a non-object payload = %v, want ErrRawEventPayload", err)
	}
	state, err := repo.GetRunState(ctx, enqueued.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if state != running {
		t.Fatalf("a rejected payload changed the run: %+v, want %+v", state, running)
	}
	// An empty payload is not the same thing as an undecodable one, and stays
	// accepted: several callers legitimately have nothing to add.
	if err := repo.CompleteRun(ctx, enqueued.RunID, enqueued.AssistantMessageID, "done"); err != nil {
		t.Fatalf("completion with no caller payload: %v", err)
	}
}

// TestClaimingAJobForANonQueuedRunFailsInsteadOfPanicking pins the guard on the
// claim transition's projection. The claim query and the job update make this
// unreachable today; the point is that if that stops being true, the result is
// a refused claim rather than an index out of range.
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
