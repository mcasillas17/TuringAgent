package runtime

import (
	"context"
	"testing"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/runoutcome"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/structpb"
)

// A late terminal update is only a duplicate when every part of the canonical
// identity matches. Each row here changes exactly one of them, so a comparison
// that quietly stopped checking that part fails on its own terms.
func TestMatchingTerminalUpdateRequiresCompleteCanonicalIdentity(t *testing.T) {
	const content = "the persisted answer"
	completedRun := repository.Run{
		RunID: "run_1", Status: "completed", TerminalEventType: "agent.run.completed",
		AssistantMessageID: "message_1", AssistantContent: content,
		ContentSHA256: runoutcome.ContentSHA256(content), StateVersion: 4,
		OutcomeReason: string(runoutcome.ReasonNone),
	}
	matchingCompletion := func() *turingv1.RuntimeRunCompleted {
		return &turingv1.RuntimeRunCompleted{
			RunId: "run_1", AssistantMessageId: "message_1", Content: content, ExpectedStateVersion: 3,
		}
	}
	if !isMatchingTerminalUpdate(completedRun, &turingv1.RuntimeUpdate{
		Update: &turingv1.RuntimeUpdate_RunCompleted{RunCompleted: matchingCompletion()},
	}) {
		t.Fatal("the exact persisted completion was not recognized as a duplicate")
	}

	completionTests := map[string]func(*turingv1.RuntimeRunCompleted){
		"different bytes":            func(c *turingv1.RuntimeRunCompleted) { c.Content = content + " " },
		"different assistant":        func(c *turingv1.RuntimeRunCompleted) { c.AssistantMessageId = "message_2" },
		"stale expected version":     func(c *turingv1.RuntimeRunCompleted) { c.ExpectedStateVersion = 2 },
		"resulting version expected": func(c *turingv1.RuntimeRunCompleted) { c.ExpectedStateVersion = 4 },
		"empty content":              func(c *turingv1.RuntimeRunCompleted) { c.Content = "" },
	}
	for name, mutate := range completionTests {
		t.Run(name, func(t *testing.T) {
			completed := matchingCompletion()
			mutate(completed)
			if isMatchingTerminalUpdate(completedRun, &turingv1.RuntimeUpdate{
				Update: &turingv1.RuntimeUpdate_RunCompleted{RunCompleted: completed},
			}) {
				t.Fatalf("a completion differing in %s matched: %+v", name, completed)
			}
		})
	}

	t.Run("hash disagreeing with the persisted bytes", func(t *testing.T) {
		run := completedRun
		run.ContentSHA256 = runoutcome.ContentSHA256("something else entirely")
		if isMatchingTerminalUpdate(run, &turingv1.RuntimeUpdate{
			Update: &turingv1.RuntimeUpdate_RunCompleted{RunCompleted: matchingCompletion()},
		}) {
			t.Fatal("a completion matched a run whose hash describes other bytes")
		}
	})

	t.Run("empty success outcome", func(t *testing.T) {
		run := repository.Run{
			RunID: "run_1", Status: "completed", TerminalEventType: "agent.run.completed",
			AssistantMessageID: "message_1", ContentSHA256: runoutcome.ContentSHA256(""),
			StateVersion: 4, OutcomeReason: string(runoutcome.ReasonCompletedNoContent),
		}
		empty := &turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_RunCompleted{
			RunCompleted: &turingv1.RuntimeRunCompleted{
				RunId: "run_1", AssistantMessageId: "message_1", Content: "", ExpectedStateVersion: 3,
			},
		}}
		if !isMatchingTerminalUpdate(run, empty) {
			t.Fatal("an explicit empty success was not recognized as its own duplicate")
		}
		run.OutcomeReason = string(runoutcome.ReasonNone)
		if isMatchingTerminalUpdate(run, empty) {
			t.Fatal("an empty completion matched a run claiming it produced content")
		}
	})

	failedRun := repository.Run{
		RunID: "run_1", Status: "failed", TerminalEventType: "agent.run.failed", StateVersion: 4,
		OutcomeReason:        string(runoutcome.ReasonProviderFailure),
		TerminalEventPayload: `{"runId":"run_1","code":"model_stream_failed","retryable":false}`,
	}
	matchingFailure := func() *turingv1.RuntimeRunFailed {
		return &turingv1.RuntimeRunFailed{
			RunId: "run_1", Code: "model_stream_failed", ExpectedStateVersion: 3,
			FailureOrigin: turingv1.FailureOrigin_FAILURE_ORIGIN_PROVIDER_TRANSPORT,
		}
	}
	if !isMatchingTerminalUpdate(failedRun, &turingv1.RuntimeUpdate{
		Update: &turingv1.RuntimeUpdate_RunFailed{RunFailed: matchingFailure()},
	}) {
		t.Fatal("the exact persisted failure was not recognized as a duplicate")
	}
	failureTests := map[string]func(*turingv1.RuntimeRunFailed){
		"different code":         func(f *turingv1.RuntimeRunFailed) { f.Code = "model_timeout" },
		"stale expected version": func(f *turingv1.RuntimeRunFailed) { f.ExpectedStateVersion = 2 },
		"different origin": func(f *turingv1.RuntimeRunFailed) {
			f.FailureOrigin = turingv1.FailureOrigin_FAILURE_ORIGIN_TOOL_EXECUTION
		},
	}
	for name, mutate := range failureTests {
		t.Run(name, func(t *testing.T) {
			failed := matchingFailure()
			mutate(failed)
			if isMatchingTerminalUpdate(failedRun, &turingv1.RuntimeUpdate{
				Update: &turingv1.RuntimeUpdate_RunFailed{RunFailed: failed},
			}) {
				t.Fatalf("a failure differing in %s matched: %+v", name, failed)
			}
		})
	}
	t.Run("reason disagreeing with the persisted outcome", func(t *testing.T) {
		run := failedRun
		run.OutcomeReason = string(runoutcome.ReasonToolFailure)
		if isMatchingTerminalUpdate(run, &turingv1.RuntimeUpdate{
			Update: &turingv1.RuntimeUpdate_RunFailed{RunFailed: matchingFailure()},
		}) {
			t.Fatal("a provider failure matched a run that ended on a tool failure")
		}
	})
}

// A late terminal update releases the execution fence only when the worker
// reporting it still owns the exact attempt the run is executing.
func TestLateTerminalAcknowledgementRequiresTheOwnedAttempt(t *testing.T) {
	h := newHarness(t)
	enqueued := h.enqueueRun(t, "owned attempt acknowledgement")
	_, assigned := h.connectAssignedWorker(t, "worker-owned-ack", enqueued.RunID)
	owner := h.service.registeredWorker("worker-owned-ack")
	if owner == nil {
		t.Fatal("connected worker was not registered")
	}
	failRunFixture(t, h, enqueued.RunID, toolExecutionFailure())
	late := &turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_RunFailed{RunFailed: &turingv1.RuntimeRunFailed{
		RunId: enqueued.RunID, Code: "tool_call_failed",
		FailureOrigin:        turingv1.FailureOrigin_FAILURE_ORIGIN_TOOL_EXECUTION,
		ExpectedStateVersion: h.runState(t, enqueued.RunID).StateVersion - 1,
	}}}

	// A worker holding some other attempt for the same run cannot acknowledge
	// an exit it was not executing.
	owner.mu.Lock()
	owner.assignments[enqueued.RunID] = assignment{
		jobID: assigned.GetJobId(), runID: enqueued.RunID, attemptID: "some-other-attempt",
	}
	owner.mu.Unlock()
	if _, err := h.service.reconcileLateAssignedUpdate(context.Background(), owner, "worker-owned-ack", late); err != nil {
		t.Fatalf("reconcileLateAssignedUpdate: %v", err)
	}
	run, err := h.repo.GetRun(context.Background(), enqueued.RunID)
	if err != nil {
		t.Fatalf("GetRun: %v", err)
	}
	if !run.ExecutionActive {
		t.Fatal("an unowned attempt released the execution fence")
	}

	owner.mu.Lock()
	owner.assignments[enqueued.RunID] = assignment{
		jobID: assigned.GetJobId(), runID: enqueued.RunID, attemptID: assigned.GetAssignmentAttemptId(),
	}
	owner.mu.Unlock()
	if _, err := h.service.reconcileLateAssignedUpdate(context.Background(), owner, "worker-owned-ack", late); err != nil {
		t.Fatalf("reconcileLateAssignedUpdate: %v", err)
	}
	run, err = h.repo.GetRun(context.Background(), enqueued.RunID)
	if err != nil {
		t.Fatalf("GetRun: %v", err)
	}
	if run.ExecutionActive || run.ExecutionState != "exited" {
		t.Fatalf("the owning attempt's report did not acknowledge the exit: %+v", run)
	}
}

func TestTerminalizedAssignedRunUsesLateTerminalUpdateAsExitAcknowledgement(t *testing.T) {
	h := newHarness(t)
	first := h.enqueueRun(t, "terminalized assigned run")
	second := h.enqueueRun(t, "assigned after exit acknowledgement")
	client := h.runtimeClient(t)
	stream, err := client.ConnectWorker(h.internalContext())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = stream.CloseSend() }()
	if err := stream.Send(workerReady("worker-terminalized-assigned")); err != nil {
		t.Fatal(err)
	}
	recvUntil(t, stream, func(command *turingv1.RuntimeCommand) bool {
		return command.GetRunAssigned() != nil && command.GetRunAssigned().GetRunId() == first.RunID
	})

	cancelRunFixture(t, h, first.RunID)
	payload, err := structpb.NewStruct(map[string]any{"delta": "late"})
	if err != nil {
		t.Fatal(err)
	}
	if err := stream.Send(&turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_Event{Event: &turingv1.TuringEvent{
		RunId: first.RunID, Type: turingv1.TuringEventType_TURING_EVENT_TYPE_MESSAGE_DELTA, Payload: payload,
	}}}); err != nil {
		t.Fatalf("send late delta: %v", err)
	}
	if err := stream.Send(&turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_Event{Event: &turingv1.TuringEvent{
		RunId: first.RunID, Type: turingv1.TuringEventType_TURING_EVENT_TYPE_AGENT_RUN_CANCELLED,
	}}}); err != nil {
		t.Fatalf("send late generic terminal event: %v", err)
	}
	if err := stream.Send(&turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_RunCompleted{RunCompleted: &turingv1.RuntimeRunCompleted{
		RunId: first.RunID, AssistantMessageId: "msg_conflicting", Content: "too late",
	}}}); err != nil {
		t.Fatalf("send conflicting late completion: %v", err)
	}
	recvUntil(t, stream, func(command *turingv1.RuntimeCommand) bool {
		return command.GetRunAssigned() != nil && command.GetRunAssigned().GetRunId() == second.RunID
	})

	run, err := h.repo.GetRun(context.Background(), first.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if run.Status != "cancelled" || run.ExecutionActive || run.ExecutionState != "exited" {
		t.Fatalf("late updates mutated terminal execution fence: %+v", run)
	}
}

func TestTerminalizedAssignedRunReconcilesMatchingLateTerminalUpdate(t *testing.T) {
	h := newHarness(t)
	first := h.enqueueRun(t, "failed while assigned")
	second := h.enqueueRun(t, "assigned after matching late failure")
	client := h.runtimeClient(t)
	stream, err := client.ConnectWorker(h.internalContext())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = stream.CloseSend() }()
	if err := stream.Send(workerReady("worker-terminalized-matching")); err != nil {
		t.Fatal(err)
	}
	recvUntil(t, stream, func(command *turingv1.RuntimeCommand) bool {
		return command.GetRunAssigned() != nil && command.GetRunAssigned().GetRunId() == first.RunID
	})

	failRunFixture(t, h, first.RunID, toolExecutionFailure())
	// The worker's own report normalizes to the same failure the run already
	// committed, which is what makes this a late acknowledgement rather than a
	// conflicting second outcome.
	if err := stream.Send(&turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_RunFailed{RunFailed: &turingv1.RuntimeRunFailed{
		RunId: first.RunID, Code: "tool_call_failed",
		FailureOrigin: turingv1.FailureOrigin_FAILURE_ORIGIN_TOOL_EXECUTION,
	}}}); err != nil {
		t.Fatalf("send matching late failure: %v", err)
	}
	recvUntil(t, stream, func(command *turingv1.RuntimeCommand) bool {
		return command.GetRunAssigned() != nil && command.GetRunAssigned().GetRunId() == second.RunID
	})

	run, err := h.repo.GetRun(context.Background(), first.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if run.Status != "failed" || run.ExecutionActive || run.ExecutionState != "exited" {
		t.Fatalf("matching terminal update did not reconcile execution exit: %+v", run)
	}
	var failedEvents int
	if err := h.database.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM events WHERE run_id = ? AND type = 'agent.run.failed'`, first.RunID).Scan(&failedEvents); err != nil {
		t.Fatal(err)
	}
	if failedEvents != 1 {
		t.Fatalf("late matching failure appended %d terminal events, want one", failedEvents)
	}
}

func TestLateTerminalUpdatesForReleasedRunKeepWorkerStreamUsable(t *testing.T) {
	tests := []struct {
		name                    string
		cancelFirst             bool
		lateExitAck             bool
		duplicateFirst          bool
		duplicateNamesCommitted bool
	}{
		{name: "duplicate_cancelled_ack", cancelFirst: true, duplicateFirst: true},
		// A duplicate naming the run's real, nonzero committed version —
		// not the zero a legacy worker leaves absent — so this pins the
		// same acknowledgedVersionMatches acceptance a live worker's own
		// retry would exercise, not just the legacy-absence case above.
		{name: "duplicate_cancelled_ack_committed_version", cancelFirst: true, duplicateFirst: true, duplicateNamesCommitted: true},
		{name: "duplicate_completed", duplicateFirst: true},
		{name: "completed_then_exit_ack", lateExitAck: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			h := newHarness(t)
			first := h.enqueueRun(t, "first terminal update")
			second := h.enqueueRun(t, "second remains assigned")
			third := h.enqueueRun(t, "third proves stream remains usable")
			client := h.runtimeClient(t)
			stream, err := client.ConnectWorker(h.internalContext())
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = stream.CloseSend() }()
			if err := stream.Send(workerReady("worker-late-terminal-" + test.name)); err != nil {
				t.Fatal(err)
			}
			recvUntil(t, stream, func(command *turingv1.RuntimeCommand) bool {
				return command.GetRunAssigned() != nil && command.GetRunAssigned().GetRunId() == first.RunID
			})

			completedFirst := &turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_RunCompleted{RunCompleted: &turingv1.RuntimeRunCompleted{
				RunId: first.RunID, AssistantMessageId: first.AssistantMessageID, Content: "first complete",
			}}}
			ackFirst := &turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_RunCancelledAck{RunCancelledAck: &turingv1.RuntimeCancelledAck{
				RunId: first.RunID,
			}}}
			var committedVersion int64
			if test.cancelFirst {
				cancelRunFixture(t, h, first.RunID)
				committedVersion = h.runState(t, first.RunID).StateVersion
				if committedVersion <= 0 {
					t.Fatalf("committed version %d is not the nonzero durable version this case needs", committedVersion)
				}
				if err := stream.Send(ackFirst); err != nil {
					t.Fatal(err)
				}
			} else if err := stream.Send(completedFirst); err != nil {
				t.Fatal(err)
			}

			recvUntil(t, stream, func(command *turingv1.RuntimeCommand) bool {
				return command.GetRunAssigned() != nil && command.GetRunAssigned().GetRunId() == second.RunID
			})

			var late *turingv1.RuntimeUpdate
			switch {
			case test.lateExitAck:
				late = ackFirst
			case test.duplicateNamesCommitted:
				late = &turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_RunCancelledAck{RunCancelledAck: &turingv1.RuntimeCancelledAck{
					RunId: first.RunID, ObservedStateVersion: committedVersion,
				}}}
			case test.cancelFirst:
				late = ackFirst
			default:
				late = completedFirst
			}
			if err := stream.Send(late); err != nil {
				t.Fatalf("send late terminal update: %v", err)
			}

			if err := stream.Send(&turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_RunCompleted{RunCompleted: &turingv1.RuntimeRunCompleted{
				RunId: second.RunID, AssistantMessageId: second.AssistantMessageID, Content: "second complete",
			}}}); err != nil {
				t.Fatalf("send second completion: %v", err)
			}
			recvUntil(t, stream, func(command *turingv1.RuntimeCommand) bool {
				return command.GetRunAssigned() != nil && command.GetRunAssigned().GetRunId() == third.RunID
			})

			// The late duplicate did not tear the stream down: second's own
			// completion (sent right behind it, on the same connection) went
			// through and released normally, rather than second being forced
			// into recovering the way TestDuplicateCancelledAckWithImpossibleVersionAfterReleaseClosesWorkerStream's
			// unassigned, impossible-version duplicate leaves every run a
			// closed stream was still holding.
			secondRun, err := h.repo.GetRun(context.Background(), second.RunID)
			if err != nil {
				t.Fatal(err)
			}
			if secondRun.Status != "completed" || secondRun.ExecutionActive {
				t.Fatalf("an unrelated run did not settle normally after the late update: %+v", secondRun)
			}
		})
	}
}

// TestDuplicateCancelledAckWithImpossibleVersionAfterReleaseClosesWorkerStream
// pins the consequence of holding a run_cancelled_ack's version to
// acknowledgedVersionMatches once the worker no longer holds the assignment at
// all.
//
// The first ack here is the legitimate one: it names no version, matches, and
// releases the fence — moving the worker on to a second run. A second ack for
// the same, now-unassigned run that names a version the run never reached is
// no longer a report reconcileLateAssignedUpdate's ownership check can filter
// out (the worker has nothing to check ownership against, since the
// assignment is gone) — it falls to isLateMatchingTerminalUpdate instead,
// which this fix also holds to the same version rule. That makes it behave
// exactly like an equally impossible late RunCompleted or RunFailed already
// does today: a report about a run this stream cannot back up is a protocol
// violation, not silent noise, and it takes the whole stream down with it —
// including every other run the worker was still holding.
func TestDuplicateCancelledAckWithImpossibleVersionAfterReleaseClosesWorkerStream(t *testing.T) {
	h := newHarness(t)
	first := h.enqueueRun(t, "released before the impossible duplicate")
	second := h.enqueueRun(t, "still held when the stream closes")
	client := h.runtimeClient(t)
	stream, err := client.ConnectWorker(h.internalContext())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = stream.CloseSend() }()
	if err := stream.Send(workerReady("worker-impossible-duplicate-ack")); err != nil {
		t.Fatal(err)
	}
	recvUntil(t, stream, func(command *turingv1.RuntimeCommand) bool {
		return command.GetRunAssigned() != nil && command.GetRunAssigned().GetRunId() == first.RunID
	})

	cancelRunFixture(t, h, first.RunID)
	if err := stream.Send(&turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_RunCancelledAck{
		RunCancelledAck: &turingv1.RuntimeCancelledAck{RunId: first.RunID},
	}}); err != nil {
		t.Fatal(err)
	}
	// The worker only has capacity for one run, so this proves the legitimate
	// ack above already released first and freed the worker for second.
	recvUntil(t, stream, func(command *turingv1.RuntimeCommand) bool {
		return command.GetRunAssigned() != nil && command.GetRunAssigned().GetRunId() == second.RunID
	})

	committed := h.runState(t, first.RunID).StateVersion
	if err := stream.Send(&turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_RunCancelledAck{
		RunCancelledAck: &turingv1.RuntimeCancelledAck{RunId: first.RunID, ObservedStateVersion: committed + 99},
	}}); err != nil {
		t.Fatal(err)
	}

	if _, recvErr := stream.Recv(); recvErr == nil {
		t.Fatal("duplicate ack naming an impossible version did not close the worker stream")
	} else if status.Code(recvErr) != codes.PermissionDenied {
		t.Fatalf("stream close code = %v, want PermissionDenied", recvErr)
	}

	firstRun, err := h.repo.GetRun(context.Background(), first.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if firstRun.ExecutionActive || firstRun.ExecutionState != "exited" {
		t.Fatalf("the already-settled release was disturbed: %+v", firstRun)
	}
	// second was still held by the worker whose stream just closed, so its
	// ownership becomes uncertain the same way any other disconnect does.
	secondRun, err := h.repo.GetRun(context.Background(), second.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if secondRun.Status != "recovering" {
		t.Fatalf("second run status = %q, want recovering after the stream closed under it", secondRun.Status)
	}
}

// TestMatchingTerminalPayloadIgnoresTheDerivedRunState pins the one thing the
// late-duplicate comparison must not compare.
//
// The durable payload carries the canonical run state the repository merges
// into every lifecycle projection. That part is derived from the row, not from
// the worker's report, so a byte comparison would call every late duplicate a
// conflict the moment a version changed. The rest of the payload still has to
// match exactly.
func TestMatchingTerminalPayloadIgnoresTheDerivedRunState(t *testing.T) {
	reported := map[string]any{
		"runId": "run_1", "code": "model_stream_failed", "message": "the stream died", "retryable": false,
	}
	durable := `{"runId":"run_1","code":"model_stream_failed","message":"the stream died","retryable":false,` +
		`"runState":{"runId":"run_1","lifecycle":"failed","outcomeReason":"provider_failure","stateVersion":4}}`
	if !matchesReportedTerminalPayload(durable, reported) {
		t.Fatal("a durable payload differing only by its derived run state was treated as a conflict")
	}

	for name, conflicting := range map[string]string{
		"a different code": `{"runId":"run_1","code":"model_timeout","message":"the stream died","retryable":false,` +
			`"runState":{"stateVersion":4}}`,
		"a different message": `{"runId":"run_1","code":"model_stream_failed","message":"something else","retryable":false,` +
			`"runState":{"stateVersion":4}}`,
		"a different run": `{"runId":"run_2","code":"model_stream_failed","message":"the stream died","retryable":false,` +
			`"runState":{"stateVersion":4}}`,
		"a missing key": `{"runId":"run_1","code":"model_stream_failed","retryable":false,"runState":{"stateVersion":4}}`,
		"not an object": `"just a string"`,
	} {
		if matchesReportedTerminalPayload(conflicting, reported) {
			t.Fatalf("a payload with %s was treated as a match", name)
		}
	}
}
