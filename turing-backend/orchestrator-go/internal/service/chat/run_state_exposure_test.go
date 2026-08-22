package chat

import (
	"context"
	"strings"
	"testing"
	"time"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/runoutcome"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/service/runstate"
	"google.golang.org/protobuf/proto"
)

// liveChatRun is a real run watched through a real ChatService stream.
//
// Everything asserted about it is read from what the server sent over that
// stream. The repository below is a driver — it moves the run the way the
// runtime would — and never an acceptance proxy: no assertion in this file
// reads a repository struct and calls that "what the client sees".
type liveChatRun struct {
	seeder *restartHarness
	chat   *harness
	stream turingv1.ChatService_SendMessageClient
	run    seededRun
}

// startLiveChatRun opens the stream, lets a connected worker claim the run, and
// hands back the run once it is genuinely running — which is the only state
// from which the terminal transitions below are legal.
func startLiveChatRun(t *testing.T, content string) *liveChatRun {
	t.Helper()
	seeder := newRestartHarness(t)
	chatHarness := newHarnessWithDatabase(t, seeder.database)
	worker := connectChatTestWorker(t, chatHarness, defaultChatWorkerCapabilities(false))
	t.Cleanup(func() { _ = worker.CloseSend() })
	session := chatHarness.createSession(t)
	stream, err := chatHarness.chatClient.SendMessage(chatHarness.clientContext(),
		&turingv1.SendMessageRequest{SessionId: session, Content: content})
	if err != nil {
		t.Fatalf("SendMessage: %v", err)
	}
	first, err := stream.Recv()
	if err != nil {
		t.Fatalf("recv queued: %v", err)
	}
	queued := first.GetRunQueued()
	if queued == nil {
		t.Fatalf("first event = %T, want run_queued", first.Event)
	}
	run := seededRun{sessionID: session, runID: queued.GetRunId()}
	runRow, err := seeder.repo.GetRun(context.Background(), run.runID)
	if err != nil {
		t.Fatalf("GetRun: %v", err)
	}
	run.assistantMessageID = runRow.AssistantMessageID
	running := seeder.awaitLifecycle(t, run.runID, "running")
	run.userMessageID = running.UserMessageID
	run.stateVersion = running.StateVersion
	return &liveChatRun{seeder: seeder, chat: chatHarness, stream: stream, run: run}
}

// runningState is the committed snapshot the claim wrote, read back from the
// durable row so the live event can be compared against what was committed
// rather than against a second copy of the same guess.
func (l *liveChatRun) runningState(t *testing.T) repository.RunState {
	t.Helper()
	state, err := l.seeder.repo.GetRunState(context.Background(), l.run.runID)
	if err != nil {
		t.Fatalf("GetRunState: %v", err)
	}
	return state
}

// appendLegacy writes one durable row for this run exactly as an unmigrated or
// hand-edited writer would have left it.
func (l *liveChatRun) appendLegacy(t *testing.T, eventType string, payloadJSON string) repository.Event {
	t.Helper()
	event, err := l.seeder.repo.AppendEvent(context.Background(), repository.AppendEventInput{
		SessionID:   l.run.sessionID,
		RunID:       l.run.runID,
		TraceID:     "trace_legacy",
		Type:        eventType,
		PayloadJSON: payloadJSON,
	})
	if err != nil {
		t.Fatalf("append %s: %v", eventType, err)
	}
	return event
}

// await reads the public stream until the server sends an event the predicate
// accepts. A stream that ends or stalls first is the failure, not a skip.
func (l *liveChatRun) await(t *testing.T, what string, match func(*turingv1.ChatStreamEvent) bool) *turingv1.ChatStreamEvent {
	t.Helper()
	matched := make(chan *turingv1.ChatStreamEvent, 1)
	failed := make(chan error, 1)
	go func() {
		for {
			event, err := l.stream.Recv()
			if err != nil {
				failed <- err
				return
			}
			if match(event) {
				matched <- event
				return
			}
		}
	}()
	select {
	case event := <-matched:
		return event
	case err := <-failed:
		t.Fatalf("stream ended before %s: %v", what, err)
	case <-time.After(10 * time.Second):
		t.Fatalf("timed out waiting for %s", what)
	}
	return nil
}

// drain reads the stream to its end and returns everything the server sent.
func (l *liveChatRun) drain(t *testing.T) []*turingv1.ChatStreamEvent {
	t.Helper()
	collected := make(chan []*turingv1.ChatStreamEvent, 1)
	go func() {
		events := make([]*turingv1.ChatStreamEvent, 0, 16)
		for {
			event, err := l.stream.Recv()
			if err != nil {
				collected <- events
				return
			}
			events = append(events, event)
		}
	}()
	select {
	case events := <-collected:
		return events
	case <-time.After(10 * time.Second):
		t.Fatal("timed out draining the chat stream")
	}
	return nil
}

// failNow and cancelNow terminalize the run and return the committed state, so
// a live event can be checked against the exact version the transition wrote.
func (l *liveChatRun) failNow(t *testing.T, failure runoutcome.Failure) repository.RunState {
	t.Helper()
	result, err := l.seeder.repo.FailRunCanonical(context.Background(), repository.FailRunInput{
		RunID:                l.run.runID,
		AssistantMessageID:   l.run.assistantMessageID,
		ExpectedStateVersion: l.runningState(t).StateVersion,
		Failure:              failure,
	})
	if err != nil {
		t.Fatalf("FailRunCanonical: %v", err)
	}
	return result.State
}

func (l *liveChatRun) cancelNow(t *testing.T, cancellation runoutcome.Cancellation) repository.RunState {
	t.Helper()
	result, err := l.seeder.repo.CancelRunCanonical(context.Background(), repository.CancelRunInput{
		RunID:                l.run.runID,
		AssistantMessageID:   l.run.assistantMessageID,
		ExpectedStateVersion: l.runningState(t).StateVersion,
		Cancellation:         cancellation,
	})
	if err != nil {
		t.Fatalf("CancelRunCanonical: %v", err)
	}
	return result.State
}

// TestChatLiveRunStartedCarriesCommittedRunState covers the lifecycle event a
// live watcher gets first. The claim is the queued-to-running transition, so
// the state on the wire has to be the version that transition committed — not
// the queued one it replaced, and not nothing.
func TestChatLiveRunStartedCarriesCommittedRunState(t *testing.T) {
	live := startLiveChatRun(t, "started state")
	event := live.await(t, "run_started", func(event *turingv1.ChatStreamEvent) bool {
		return event.GetRunStarted() != nil
	})
	started := event.GetRunStarted()
	committed := live.runningState(t)
	state := started.GetRunState()
	if state == nil {
		t.Fatal("the live run_started carried no run state")
	}
	assertRunStateShape(t, state, live.run, committed.StateVersion,
		turingv1.RunLifecycle_RUN_LIFECYCLE_RUNNING,
		turingv1.RunOutcomeReason_RUN_OUTCOME_REASON_NONE, false, false)
	if !proto.Equal(state, runstate.Project(committed)) {
		t.Fatalf("live run_started state %+v is not the committed projection %+v",
			state, runstate.Project(committed))
	}
	if started.GetRunId() != live.run.runID {
		t.Fatalf("run_started run id = %q, want %q", started.GetRunId(), live.run.runID)
	}
	// The claim's own payload carries the assignment attempt that holds this
	// run. The dedicated union publishes identity and state, never that.
	assertChatEventCarriesNoExecutionIdentity(t, event)
	replayed := latestEventState(t, live.seeder, live.run)
	if !proto.Equal(state, replayed) {
		t.Fatalf("live run_started state %+v and replayed state %+v disagree", state, replayed)
	}
}

// TestChatLiveRunFailedCarriesCommittedRunStateAndFixedLegacyFields is the
// terminal half. The typed state is the committed one; the deprecated string
// fields an older client still reads are fixed generic values, because those
// fields are how a failing provider's own sentence used to reach a user.
func TestChatLiveRunFailedCarriesCommittedRunStateAndFixedLegacyFields(t *testing.T) {
	live := startLiveChatRun(t, "failed state")
	committed := live.failNow(t, runoutcome.NormalizeFailure(
		runoutcome.OriginProviderTransport, "model_timeout", runoutcome.RetryClassNever))
	event := live.await(t, "run_failed", func(event *turingv1.ChatStreamEvent) bool {
		return event.GetRunFailed() != nil
	})
	failed := event.GetRunFailed()
	state := failed.GetRunState()
	if state == nil {
		t.Fatal("the live run_failed carried no run state")
	}
	assertRunStateShape(t, state, live.run, committed.StateVersion,
		turingv1.RunLifecycle_RUN_LIFECYCLE_FAILED,
		turingv1.RunOutcomeReason_RUN_OUTCOME_REASON_PROVIDER_FAILURE, true, false)
	if !proto.Equal(state, runstate.Project(committed)) {
		t.Fatalf("live run_failed state %+v is not the committed projection %+v",
			state, runstate.Project(committed))
	}
	if failed.GetCode() != legacyFailureCode {
		t.Fatalf("run_failed code = %q, want the fixed generic %q", failed.GetCode(), legacyFailureCode)
	}
	if failed.GetMessage() != "" {
		t.Fatalf("run_failed message = %q, want nothing", failed.GetMessage())
	}
	// Read through the descriptor rather than the deprecated accessor: the
	// field is deprecated precisely because it is fixed now, and asserting that
	// is the point.
	retryable := failed.ProtoReflect().Descriptor().Fields().ByNumber(4)
	if retryable == nil || failed.ProtoReflect().Get(retryable).Bool() {
		t.Fatalf("deprecated retryable = %v, want the fixed false", failed)
	}
	assertChatEventCarriesNoRawDiagnostics(t, event)
	replayed := latestEventState(t, live.seeder, live.run)
	if !proto.Equal(state, replayed) {
		t.Fatalf("live run_failed state %+v and replayed state %+v disagree", state, replayed)
	}
}

// TestChatLiveRunCancelledCarriesCommittedRunStateAndFixedLegacyReason is the
// same rule for the cancellation union, whose deprecated reason field used to
// carry whichever word the writer chose.
func TestChatLiveRunCancelledCarriesCommittedRunStateAndFixedLegacyReason(t *testing.T) {
	live := startLiveChatRun(t, "cancelled state")
	committed := live.cancelNow(t, runoutcome.AbandonedCancellation())
	event := live.await(t, "run_cancelled", func(event *turingv1.ChatStreamEvent) bool {
		return event.GetRunCancelled() != nil
	})
	cancelled := event.GetRunCancelled()
	state := cancelled.GetRunState()
	if state == nil {
		t.Fatal("the live run_cancelled carried no run state")
	}
	assertRunStateShape(t, state, live.run, committed.StateVersion,
		turingv1.RunLifecycle_RUN_LIFECYCLE_CANCELLED,
		turingv1.RunOutcomeReason_RUN_OUTCOME_REASON_ABANDONED, true, false)
	if !proto.Equal(state, runstate.Project(committed)) {
		t.Fatalf("live run_cancelled state %+v is not the committed projection %+v",
			state, runstate.Project(committed))
	}
	if cancelled.GetReason() != legacyCancellationReason {
		t.Fatalf("run_cancelled reason = %q, want the fixed generic %q",
			cancelled.GetReason(), legacyCancellationReason)
	}
	assertChatEventCarriesNoRawDiagnostics(t, event)
	replayed := latestEventState(t, live.seeder, live.run)
	if !proto.Equal(state, replayed) {
		t.Fatalf("live run_cancelled state %+v and replayed state %+v disagree", state, replayed)
	}
}

// TestChatLiveStreamSanitizesEveryFailureTypeSpelling is the ChatService half
// of the one-normalization rule. A failure row that arrived spelled as the
// generated enum's own name is still a failure, and the union a client receives
// and the allowlist its payload went through have to agree about that.
func TestChatLiveStreamSanitizesEveryFailureTypeSpelling(t *testing.T) {
	live := startLiveChatRun(t, "type spellings")
	const poisonedPayload = `{"approvalId":"appr_1","toolCallId":"call_1","toolName":"system.shell","serverName":"system",` +
		`"code":"model_error","message":"connection refused by ollama at 127.0.0.1:11434",` +
		`"reason":"denied because this would email the whole company",` +
		`"args":{"command":"rm -rf /Users/someone"},"approvalToken":"******"}`
	for _, stored := range []string{
		"APPROVAL_DENIED", "approval_denied", "TURING_EVENT_TYPE_APPROVAL_EXPIRED",
		"TOOL_CALL_FAILED", "tool_call_denied", "Approval.Denied",
	} {
		live.appendLegacy(t, stored, poisonedPayload)
	}
	// The terminal spelling ends the stream, which is itself part of the rule:
	// a row a client is told is a cancellation has to close the stream whatever
	// spelling the row holds.
	live.appendLegacy(t, "AGENT_RUN_CANCELLED", poisonedPayload)

	events := live.drain(t)
	var sawCancellation bool
	for _, event := range events {
		assertChatEventCarriesNoRawDiagnostics(t, event)
		if cancelled := event.GetRunCancelled(); cancelled != nil {
			sawCancellation = true
			if cancelled.GetReason() != legacyCancellationReason {
				t.Fatalf("uppercased cancellation reason = %q, want the fixed generic value", cancelled.GetReason())
			}
		}
	}
	if !sawCancellation {
		t.Fatalf("the uppercased cancellation never reached the stream as one (%d events)", len(events))
	}
}

// TestChatLivePassThroughPayloadsDropExecutionIdentity covers the keys that say
// who is executing a run rather than what happened to it. The durable log keeps
// them for recovery; a client has no business knowing which worker or which
// assignment attempt holds its run.
func TestChatLivePassThroughPayloadsDropExecutionIdentity(t *testing.T) {
	live := startLiveChatRun(t, "execution identity")
	for _, eventType := range []string{
		"system", "agent.run.step", "agent.run.state_changed", "message.started", "approval.requested",
	} {
		live.appendLegacy(t, eventType, `{"note":"still working",`+
			`"assignmentAttemptId":"attempt_7","workerId":"worker-1",`+
			`"leaseOwner":"worker-1","executionState":"pending_send"}`)
	}
	live.cancelNow(t, runoutcome.AbandonedCancellation())
	for _, event := range live.drain(t) {
		assertChatEventCarriesNoExecutionIdentity(t, event)
	}
}

// hostileSelfNamingRunState builds the payload shape a newer server, a
// restored backup, or a hand edit could leave behind: a writer's own keys
// alongside a runState snapshot that correctly names this run's own row and
// assistant message, but whose lifecycle/outcome words ("hibernating" /
// "sunspots") this build has never heard of.
func hostileSelfNamingRunState(runID string, assistantMessageID string, version string) string {
	return `{"jobId":"job_1","runState":{` +
		`"runId":"` + runID + `",` +
		`"userMessageId":"msg_user",` +
		`"assistantMessageId":"` + assistantMessageID + `",` +
		`"lifecycle":"hibernating",` +
		`"outcomeReason":"sunspots",` +
		`"stateVersion":` + version + `,` +
		`"stateUpdatedAt":"2026-08-20T00:00:00.000000000Z",` +
		`"hasDisplayableContent":true}}`
}

// TestChatLiveHostileRunStateSnapshotsBecomeUnknownOrNothing walks the rows a
// newer server, a restored backup or a hand edit could leave behind, restricted
// to the exact set of canonical types this repository's own writers ever
// commit a RunState under (the carriers: the dedicated agent.run.started/
// state_changed unions and the persisted-arm approval.requested and
// agent.run.queued). For a carrier, the typed state is either the honest
// unknown or absent, and the stored words never reach the wire in any form.
// TestChatLiveNonCarrierHostileRunStateSnapshotsNeverProjectState below is the
// same setup run against every OTHER type, where the rule is stricter: no
// typed state at all, not even the honest unknown, however well-formed the
// snapshot looks.
func TestChatLiveHostileRunStateSnapshotsBecomeUnknownOrNothing(t *testing.T) {
	live := startLiveChatRun(t, "hostile snapshots")
	hostile := func(version string) string {
		return hostileSelfNamingRunState(live.run.runID, live.run.assistantMessageID, version)
	}
	// A pass-through lifecycle type keeps its writer's payload, so it is the
	// arm where the snapshot has to be dropped rather than merely ignored, and
	// the arm where a stored word could still be republished verbatim. The
	// dedicated unions carry no payload at all, so the carrier types without
	// one — approval.requested, agent.run.queued — are where that drop is
	// observable to a chat client.
	firstSeeded := live.appendLegacy(t, "agent.run.started", hostile("7")).Sequence
	live.appendLegacy(t, "agent.run.state_changed", hostile("7"))
	live.appendLegacy(t, "agent.run.started", hostile("0"))
	live.appendLegacy(t, "agent.run.state_changed", hostile("0"))
	// Only carriers here: approval.approved is the same writer
	// (appendApprovalRunStateEventTx) as approval.requested, so it is not
	// separately exercised, but every type in this arm really is one whose
	// committer merges a RunState in.
	persistedArm := []string{"approval.requested", "agent.run.queued"}
	for _, eventType := range persistedArm {
		live.appendLegacy(t, eventType, hostile("7"))
	}
	live.cancelNow(t, runoutcome.AbandonedCancellation())

	var namedStates, absentStates, persistedStates int
	for _, event := range live.drain(t) {
		assertChatEventCarriesNoStoredRunStateWords(t, event)
		// The claim wrote a genuine start before any of this; only the seeded
		// rows are the subject here.
		if event.GetSequence() < firstSeeded {
			continue
		}
		var state *turingv1.RunState
		switch {
		case event.GetRunStarted() != nil:
			state = event.GetRunStarted().GetRunState()
		case event.GetRunStateChanged() != nil:
			state = event.GetRunStateChanged().GetRunState()
		case event.GetPersistedEvent() != nil:
			persisted := event.GetPersistedEvent()
			if _, republished := persisted.GetPayload().AsMap()["runState"]; republished {
				t.Fatalf("the persisted arm republished the stored snapshot: %s", persisted.GetPayload())
			}
			if got := persisted.GetPayload().GetFields()["jobId"].GetStringValue(); got != "job_1" {
				t.Fatalf("the drop took the writer's own payload with it: %s", persisted.GetPayload())
			}
			persistedStates++
			state = persisted.GetRunState()
		default:
			continue
		}
		if state == nil {
			absentStates++
			continue
		}
		namedStates++
		if state.GetLifecycle() != turingv1.RunLifecycle_RUN_LIFECYCLE_UNKNOWN ||
			state.GetOutcomeReason() != turingv1.RunOutcomeReason_RUN_OUTCOME_REASON_UNKNOWN {
			t.Fatalf("hostile snapshot projected %v/%v, want the honest unknown",
				state.GetLifecycle(), state.GetOutcomeReason())
		}
		if state.GetStateVersion() != 7 {
			t.Fatalf("published state version = %d, want the stored 7", state.GetStateVersion())
		}
	}
	// Two rows carry a version this build can reconcile and two carry protobuf
	// absence, so both halves of the rule are exercised, and every carrier
	// type without a dedicated union went through the arm that publishes a
	// payload.
	if namedStates != 2+len(persistedArm) || absentStates != 2 {
		t.Fatalf("named=%d absent=%d, want %d and 2", namedStates, absentStates, 2+len(persistedArm))
	}
	if persistedStates != len(persistedArm) {
		t.Fatalf("persisted arm saw %d rows, want %d", persistedStates, len(persistedArm))
	}
}

// TestChatLiveNonCarrierHostileRunStateSnapshotsNeverProjectState is the other
// half of the split: the exact same hostile, self-naming, version-7 snapshot
// attached instead to types this repository's writers never commit a
// RunState under — a tool call, a generic system notice, a message event, a
// worker's own run-step narration. None of these may project a typed state at
// all, honest-unknown or otherwise, because nothing about their own writer
// ever committed one; the only thing that would have made this snapshot
// believable is which event type carries it, not how well-formed it looks.
// The writer's own (non-runState) payload content still has to survive —
// this is a narrower gate, not a blanket scrub of legacy payload content.
func TestChatLiveNonCarrierHostileRunStateSnapshotsNeverProjectState(t *testing.T) {
	live := startLiveChatRun(t, "non-carrier hostile snapshots")
	hostile := hostileSelfNamingRunState(live.run.runID, live.run.assistantMessageID, "7")
	nonCarrierArm := []string{"tool.call.started", "system", "message.started", "agent.run.step"}
	var firstSeeded int64
	for _, eventType := range nonCarrierArm {
		seeded := live.appendLegacy(t, eventType, hostile)
		if firstSeeded == 0 {
			firstSeeded = seeded.Sequence
		}
	}
	live.cancelNow(t, runoutcome.AbandonedCancellation())

	var checked int
	for _, event := range live.drain(t) {
		assertChatEventCarriesNoStoredRunStateWords(t, event)
		if event.GetSequence() < firstSeeded {
			continue
		}
		persisted := event.GetPersistedEvent()
		if persisted == nil {
			continue
		}
		if _, republished := persisted.GetPayload().AsMap()["runState"]; republished {
			t.Fatalf("a non-carrier row republished the stored snapshot: %s", persisted.GetPayload())
		}
		if got := persisted.GetPayload().GetFields()["jobId"].GetStringValue(); got != "job_1" {
			t.Fatalf("the drop took the writer's own payload with it: %s", persisted.GetPayload())
		}
		if state := persisted.GetRunState(); state != nil {
			t.Fatalf("a non-carrier type projected a typed RunState from a self-naming snapshot it never authored: %+v", state)
		}
		checked++
	}
	if checked != len(nonCarrierArm) {
		t.Fatalf("checked %d non-carrier rows, want every seeded one (%d)", checked, len(nonCarrierArm))
	}
}

// TestChatLiveZeroVersionSnapshotsNeverBecomeTypedState pins the version rule
// on its own. Version zero is protobuf absence, so a snapshot carrying it names
// no state a client could reconcile against — and publishing it would read as a
// version older than every stored one rather than as "no version known".
func TestChatLiveZeroVersionSnapshotsNeverBecomeTypedState(t *testing.T) {
	live := startLiveChatRun(t, "zero version")
	snapshot := func(version string) string {
		return `{"runId":"` + live.run.runID + `","runState":{` +
			`"runId":"` + live.run.runID + `",` +
			`"userMessageId":"` + live.run.userMessageID + `",` +
			`"assistantMessageId":"` + live.run.assistantMessageID + `",` +
			`"lifecycle":"completed","outcomeReason":"none",` +
			`"stateVersion":` + version + `,` +
			`"stateUpdatedAt":"2026-08-20T00:00:00.000000000Z",` +
			`"hasDisplayableContent":true}}`
	}
	var firstSeeded int64
	for _, version := range []string{"0", "-1"} {
		started := live.appendLegacy(t, "agent.run.started", snapshot(version))
		if firstSeeded == 0 {
			firstSeeded = started.Sequence
		}
		live.appendLegacy(t, "agent.run.state_changed", snapshot(version))
	}
	live.appendLegacy(t, "agent.run.failed", snapshot("0"))

	var checked int
	for _, event := range live.drain(t) {
		// The claim wrote a genuine start with a real version before any of
		// this; only the seeded rows are the subject here.
		if event.GetSequence() < firstSeeded {
			continue
		}
		var state *turingv1.RunState
		switch {
		case event.GetRunStateChanged() != nil:
			state = event.GetRunStateChanged().GetRunState()
		case event.GetRunFailed() != nil:
			state = event.GetRunFailed().GetRunState()
		case event.GetRunStarted() != nil:
			state = event.GetRunStarted().GetRunState()
		default:
			continue
		}
		checked++
		if state != nil {
			t.Fatalf("a zero-version snapshot became the typed state %+v", state)
		}
	}
	if checked != 5 {
		t.Fatalf("checked %d zero-version rows, want every seeded one", checked)
	}
}

// TestChatLiveCategoryComesFromTheEventTypeNotThePayload covers a row whose
// stored category disagrees with the event it is attached to. The category is
// read off the type this server chose, so a hand-edited or unmigrated row
// cannot relabel a refused approval as an uncertain side effect.
func TestChatLiveCategoryComesFromTheEventTypeNotThePayload(t *testing.T) {
	live := startLiveChatRun(t, "hostile categories")
	hostile := `{"approvalId":"appr_1","toolCallId":"call_1","toolName":"system.shell","serverName":"system",` +
		`"category":"side_effect_uncertain","attempt":2,"maxAttempts":3,` +
		`"message":"connection refused by ollama at 127.0.0.1:11434"}`
	want := map[string]string{
		"approval.denied":  "policy_denied",
		"approval.expired": "expired",
		"tool.call.failed": "tool_failure",
		"tool.call.denied": "policy_denied",
		// A run step has no type-derived category, so the shape of the notice
		// decides — and a category outside the notice vocabulary is replaced
		// rather than republished.
		"agent.run.step": "dispatch_retry",
	}
	for eventType := range want {
		live.appendLegacy(t, eventType, hostile)
	}
	live.cancelNow(t, runoutcome.AbandonedCancellation())

	seen := map[string]string{}
	for _, event := range live.drain(t) {
		persisted := event.GetPersistedEvent()
		if persisted == nil {
			continue
		}
		category, ok := persisted.GetPayload().AsMap()["category"].(string)
		if !ok {
			continue
		}
		assertChatEventCarriesNoRawDiagnostics(t, event)
		seen[persisted.GetType().String()] = category
	}
	byType := map[string]string{
		"TURING_EVENT_TYPE_APPROVAL_DENIED":  want["approval.denied"],
		"TURING_EVENT_TYPE_APPROVAL_EXPIRED": want["approval.expired"],
		"TURING_EVENT_TYPE_TOOL_CALL_FAILED": want["tool.call.failed"],
		"TURING_EVENT_TYPE_TOOL_CALL_DENIED": want["tool.call.denied"],
		"TURING_EVENT_TYPE_AGENT_RUN_STEP":   want["agent.run.step"],
	}
	for publicType, expected := range byType {
		got, published := seen[publicType]
		if !published {
			t.Fatalf("%s published no category at all (saw %v)", publicType, seen)
		}
		if got != expected {
			t.Fatalf("%s published category %q, want the type-derived %q", publicType, got, expected)
		}
	}
}

// executionIdentityKeys are the payload keys that name who is executing a run.
var executionIdentityKeys = []string{"assignmentAttemptId", "workerId", "leaseOwner", "executionState"}

func assertChatEventCarriesNoExecutionIdentity(t *testing.T, event *turingv1.ChatStreamEvent) {
	t.Helper()
	rendered := event.String()
	for _, key := range executionIdentityKeys {
		if strings.Contains(rendered, key) {
			t.Fatalf("chat event carries the execution identity key %q: %s", key, rendered)
		}
	}
	for _, value := range []string{"attempt_7", "worker-1", "pending_send"} {
		if strings.Contains(rendered, value) {
			t.Fatalf("chat event republished the execution identity %q: %s", value, rendered)
		}
	}
}

func assertChatEventCarriesNoStoredRunStateWords(t *testing.T, event *turingv1.ChatStreamEvent) {
	t.Helper()
	rendered := event.String()
	for _, stored := range []string{"hibernating", "sunspots", "runState"} {
		if strings.Contains(rendered, stored) {
			t.Fatalf("chat event rendered the stored word %q: %s", stored, rendered)
		}
	}
}
