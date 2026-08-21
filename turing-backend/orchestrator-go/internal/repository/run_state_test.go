package repository

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"reflect"
	"testing"

	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/db"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/persisttime"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/runcorrelation"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/runoutcome"
)

// providerFailureForTest and abandonedCancellationForTest build the normalized
// values the canonical terminal writers accept. They go through the real
// constructors so a test cannot express an outcome production code could not.
func providerFailureForTest() runoutcome.Failure {
	return runoutcome.NormalizeFailure(runoutcome.OriginProviderTransport, "model_stream_failed", runoutcome.RetryClassNever)
}

func abandonedCancellationForTest() runoutcome.Cancellation {
	return runoutcome.AbandonedCancellation()
}

// runLinkForTest reads the run/message pairing through the same reader enqueue
// validates with, so these tests cannot assert a link shape the writer never
// produces.
func runLinkForTest(ctx context.Context, database *db.DB, runID string) (runcorrelation.Link, error) {
	return runCorrelationLink(ctx, database, runID)
}

// eventSnapshot is the run-state projection carried by a durable lifecycle
// event. It is decoded from JSON rather than compared as a string so a payload
// that gains an unrelated key does not fail these tests, and so a payload that
// silently drops the snapshot does.
type eventSnapshot struct {
	RunID                 string `json:"runId"`
	UserMessageID         string `json:"userMessageId"`
	AssistantMessageID    string `json:"assistantMessageId"`
	Lifecycle             string `json:"lifecycle"`
	OutcomeReason         string `json:"outcomeReason"`
	StateVersion          int64  `json:"stateVersion"`
	StateUpdatedAt        string `json:"stateUpdatedAt"`
	FinishedAt            string `json:"finishedAt"`
	HasDisplayableContent bool   `json:"hasDisplayableContent"`
}

func decodeRunStateSnapshot(t *testing.T, event Event) eventSnapshot {
	t.Helper()
	var payload struct {
		RunState *eventSnapshot `json:"runState"`
	}
	if err := json.Unmarshal([]byte(event.PayloadJSON), &payload); err != nil {
		t.Fatalf("decode %s payload %q: %v", event.Type, event.PayloadJSON, err)
	}
	if payload.RunState == nil {
		t.Fatalf("%s payload %q carries no runState snapshot", event.Type, event.PayloadJSON)
	}
	// The content digest is internal identity for duplicate detection. It is
	// never public, so its absence is asserted on the raw bytes rather than
	// inferred from the decoded struct.
	var raw map[string]json.RawMessage
	if err := json.Unmarshal([]byte(event.PayloadJSON), &raw); err != nil {
		t.Fatalf("decode %s payload map: %v", event.Type, err)
	}
	var state map[string]any
	if err := json.Unmarshal(raw["runState"], &state); err != nil {
		t.Fatalf("decode %s runState map: %v", event.Type, err)
	}
	for _, forbidden := range []string{"contentSha256", "contentSHA256", "assistantContentSha256", "workerId"} {
		if _, present := state[forbidden]; present {
			t.Fatalf("%s runState leaked internal field %q: %s", event.Type, forbidden, event.PayloadJSON)
		}
	}
	return *payload.RunState
}

// decodeRunStateMap returns the snapshot as it was actually encoded, so a test
// can tell an absent key apart from a key carrying the empty string. The typed
// decode above cannot: both produce the zero value.
func decodeRunStateMap(t *testing.T, payloadJSON string) map[string]any {
	t.Helper()
	var payload struct {
		RunState map[string]any `json:"runState"`
	}
	if err := json.Unmarshal([]byte(payloadJSON), &payload); err != nil {
		t.Fatalf("decode payload %q: %v", payloadJSON, err)
	}
	if payload.RunState == nil {
		t.Fatalf("payload %q carries no runState snapshot", payloadJSON)
	}
	return payload.RunState
}

func assertSnapshotMatchesState(t *testing.T, snapshot eventSnapshot, state RunState) {
	t.Helper()
	if snapshot.RunID != state.RunID ||
		snapshot.UserMessageID != state.UserMessageID ||
		snapshot.AssistantMessageID != state.AssistantMessageID ||
		snapshot.Lifecycle != state.Lifecycle ||
		snapshot.OutcomeReason != state.OutcomeReason ||
		snapshot.StateVersion != state.StateVersion ||
		snapshot.StateUpdatedAt != state.StateUpdatedAt ||
		snapshot.HasDisplayableContent != state.HasDisplayableContent {
		t.Fatalf("event snapshot = %+v, want the committed state %+v", snapshot, state)
	}
	if snapshot.FinishedAt != state.FinishedAt.String {
		t.Fatalf("event snapshot finishedAt = %q, want %q", snapshot.FinishedAt, state.FinishedAt.String)
	}
}

func countRunEvents(t *testing.T, repo *Repository, runID string) int {
	t.Helper()
	var count int
	if err := repo.db.QueryRowContext(context.Background(),
		`SELECT COUNT(*) FROM events WHERE run_id = ?`, runID).Scan(&count); err != nil {
		t.Fatalf("count run events: %v", err)
	}
	return count
}

// enqueueRun creates a session and queues one run in it.
func enqueueRun(t *testing.T, repo *Repository, title string) EnqueueUserMessageResult {
	t.Helper()
	ctx := context.Background()
	session, err := repo.CreateSession(ctx, title)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	enqueued, err := repo.EnqueueUserMessage(ctx, EnqueueUserMessageInput{
		SessionID: session.SessionID, Content: "hello", AgentID: "general_assistant",
		ModelProvider: "ollama", Model: "llama3.2",
	})
	if err != nil {
		t.Fatalf("EnqueueUserMessage: %v", err)
	}
	return enqueued
}

// recoveringRun drives a fresh run all the way to recovering through the real
// transitions, so its version is whatever those transitions actually produced
// rather than a number a test asserted into the row.
func recoveringRun(t *testing.T, repo *Repository, worker string) (EnqueueUserMessageResult, Job, RunState) {
	t.Helper()
	ctx := context.Background()
	enqueued := enqueueRun(t, repo, "Recovering run")
	claimed, err := repo.ClaimNextJob(ctx, "general_assistant", worker)
	if err != nil {
		t.Fatalf("ClaimNextJob: %v", err)
	}
	running, err := repo.GetRunState(ctx, enqueued.RunID)
	if err != nil {
		t.Fatalf("GetRunState: %v", err)
	}
	fenced, err := repo.FenceRunOwnership(ctx, FenceRunOwnershipInput{
		RunID:                enqueued.RunID,
		ExpectedStateVersion: running.StateVersion,
		WorkerID:             worker,
		AssignmentAttemptID:  claimed.AssignmentAttemptID,
	})
	if err != nil {
		t.Fatalf("FenceRunOwnership: %v", err)
	}
	return enqueued, claimed, fenced.State
}

// TestEnqueueCreatesQueuedVersionOneWithMatchingEventSnapshot pins the one
// place a run comes into existence. The row already carried version 1 before
// this task; what was missing is that the queued event told a client the same
// thing, so a reopened session and a live stream could disagree about the very
// first state.
func TestEnqueueCreatesQueuedVersionOneWithMatchingEventSnapshot(t *testing.T) {
	repo := New(openTestDB(t))
	ctx := context.Background()
	enqueued := enqueueRun(t, repo, "Queued snapshot")

	state, err := repo.GetRunState(ctx, enqueued.RunID)
	if err != nil {
		t.Fatalf("GetRunState: %v", err)
	}
	if state.Lifecycle != "queued" || state.OutcomeReason != "none" || state.StateVersion != 1 {
		t.Fatalf("queued state = %+v, want queued/none at version 1", state)
	}
	if state.FinishedAt.Valid {
		t.Fatalf("queued state carries finished_at %q, want none", state.FinishedAt.String)
	}
	if state.HasDisplayableContent {
		t.Fatal("queued state claims displayable assistant content before the run produced any")
	}
	assertSnapshotMatchesState(t, decodeRunStateSnapshot(t, enqueued.QueuedEvent), state)

	// The snapshot must be durable, not only returned: a client that reopens
	// reads the stored payload.
	var storedPayload string
	if err := repo.db.QueryRowContext(ctx,
		`SELECT payload_json FROM events WHERE id = ?`, enqueued.QueuedEvent.EventID).Scan(&storedPayload); err != nil {
		t.Fatalf("read stored queued event: %v", err)
	}
	assertSnapshotMatchesState(t, decodeRunStateSnapshot(t, Event{
		Type: "agent.run.queued", PayloadJSON: storedPayload,
	}), state)
}

// TestEnqueueValidatesBothDirectionsOfRunMessageCorrelation covers the writer
// boundary the whole design rests on: enqueue is the only creator of the
// circular run/message link, so it is the only place that can prove both
// directions agree before anything downstream reads them.
func TestEnqueueValidatesBothDirectionsOfRunMessageCorrelation(t *testing.T) {
	database := openTestDB(t)
	repo := New(database)
	ctx := context.Background()
	enqueued := enqueueRun(t, repo, "Correlation")

	link, err := runLinkForTest(ctx, database, enqueued.RunID)
	if err != nil {
		t.Fatalf("read enqueued link: %v", err)
	}
	if err := runcorrelation.Validate(link); err != nil {
		t.Fatalf("enqueue produced a link its own validator rejects: %v", err)
	}
	if link.RunAssistantMessageID != enqueued.AssistantMessageID || link.MessageRunID != enqueued.RunID {
		t.Fatalf("link = %+v, want both directions naming the enqueued pair", link)
	}

	// Both directions, plus the session and role the validator also requires.
	// Each mutation is applied to the values enqueue itself reads back inside
	// its transaction, so a writer that stopped validating would accept them.
	for name, corrupt := range map[string]func(runcorrelation.Link) runcorrelation.Link{
		"message points at another run": func(l runcorrelation.Link) runcorrelation.Link {
			l.MessageRunID = "run_other"
			return l
		},
		"run points at another message": func(l runcorrelation.Link) runcorrelation.Link {
			l.RunAssistantMessageID = "msg_other"
			return l
		},
		"message belongs to another session": func(l runcorrelation.Link) runcorrelation.Link {
			l.MessageSessionID = "sess_other"
			return l
		},
		"linked message is not the assistant turn": func(l runcorrelation.Link) runcorrelation.Link {
			l.MessageRole = "user"
			return l
		},
		"link is half written": func(l runcorrelation.Link) runcorrelation.Link {
			l.MessageRunID = ""
			return l
		},
	} {
		t.Run(name, func(t *testing.T) {
			if err := validateRunCorrelationLink(corrupt(link)); !errors.Is(err, runcorrelation.ErrConflict) {
				t.Fatalf("validate(%s) = %v, want runcorrelation.ErrConflict", name, err)
			}
		})
	}

	// The queued event and the job are the two rows enqueue writes after the
	// link exists; both must be present on the success path.
	var jobs, queuedEvents int
	if err := database.QueryRowContext(ctx, `SELECT COUNT(*) FROM jobs WHERE run_id = ?`, enqueued.RunID).Scan(&jobs); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM events WHERE run_id = ? AND type = 'agent.run.queued'`, enqueued.RunID).Scan(&queuedEvents); err != nil {
		t.Fatal(err)
	}
	if jobs != 1 || queuedEvents != 1 {
		t.Fatalf("enqueue wrote %d jobs and %d queued events, want exactly one of each", jobs, queuedEvents)
	}
}

// TestRealLifecycleTransitionIncrementsVersionAndAppendsOneProjection pins the
// core rule: one real transition, one version, one event.
func TestRealLifecycleTransitionIncrementsVersionAndAppendsOneProjection(t *testing.T) {
	repo := New(openTestDB(t))
	ctx := context.Background()
	enqueued := enqueueRun(t, repo, "Assignment start")
	before := countRunEvents(t, repo, enqueued.RunID)

	claimed, err := repo.ClaimNextJob(ctx, "general_assistant", "worker-start")
	if err != nil {
		t.Fatalf("ClaimNextJob: %v", err)
	}
	state, err := repo.GetRunState(ctx, enqueued.RunID)
	if err != nil {
		t.Fatalf("GetRunState: %v", err)
	}
	if state.Lifecycle != "running" || state.OutcomeReason != "none" || state.StateVersion != 2 {
		t.Fatalf("started state = %+v, want running/none at version 2", state)
	}
	if after := countRunEvents(t, repo, enqueued.RunID); after != before+1 {
		t.Fatalf("assignment start appended %d events, want exactly 1", after-before)
	}
	// The start already had a lifecycle event, so it must carry the snapshot
	// rather than gain a second state-changed event beside it.
	if claimed.StartedEvent.Type != "agent.run.started" {
		t.Fatalf("start event = %q, want agent.run.started", claimed.StartedEvent.Type)
	}
	assertSnapshotMatchesState(t, decodeRunStateSnapshot(t, claimed.StartedEvent), state)

	// state_updated_at must move with the transition; leaving it at the queue
	// time would make the row's own history unreadable.
	queuedAt, err := persisttime.ParseLegacy(enqueued.QueuedEvent.CreatedAt)
	if err != nil {
		t.Fatalf("parse queued time: %v", err)
	}
	updatedAt, err := persisttime.ParseLegacy(state.StateUpdatedAt)
	if err != nil {
		t.Fatalf("parse state_updated_at: %v", err)
	}
	if !updatedAt.After(queuedAt) {
		t.Fatalf("state_updated_at %q did not advance past the queued time %q", state.StateUpdatedAt, enqueued.QueuedEvent.CreatedAt)
	}
}

// TestSemanticDuplicatePreservesVersionTimestampAndEventCount covers a retried
// command from a worker that never saw the first acknowledgement. Repeating it
// must be free: the same identity at the same expected version is the same
// transition, not a second one.
func TestSemanticDuplicatePreservesVersionTimestampAndEventCount(t *testing.T) {
	repo := New(openTestDB(t))
	ctx := context.Background()
	enqueued, claimed, recovering := recoveringRun(t, repo, "worker-replay")

	resume := ResumeRecoveringRunInput{
		RunID:                enqueued.RunID,
		ExpectedStateVersion: recovering.StateVersion,
		WorkerID:             "worker-replay",
		AssignmentAttemptID:  claimed.AssignmentAttemptID,
	}
	first, err := repo.ResumeRecoveringRun(ctx, resume)
	if err != nil {
		t.Fatalf("ResumeRecoveringRun: %v", err)
	}
	if first.Duplicate {
		t.Fatal("the first resume reported itself as a duplicate")
	}
	if first.State.Lifecycle != "running" || first.State.StateVersion != recovering.StateVersion+1 {
		t.Fatalf("resumed state = %+v, want running at version %d", first.State, recovering.StateVersion+1)
	}
	eventsAfterFirst := countRunEvents(t, repo, enqueued.RunID)

	second, err := repo.ResumeRecoveringRun(ctx, resume)
	if err != nil {
		t.Fatalf("replayed ResumeRecoveringRun: %v", err)
	}
	if !second.Duplicate {
		t.Fatal("the replayed resume was not recognized as a semantic duplicate")
	}
	if len(second.Events) != 0 {
		t.Fatalf("replayed resume appended %d events, want none", len(second.Events))
	}
	if second.State != first.State {
		t.Fatalf("replayed resume state = %+v, want the identical committed state %+v", second.State, first.State)
	}
	if after := countRunEvents(t, repo, enqueued.RunID); after != eventsAfterFirst {
		t.Fatalf("replayed resume appended %d events to the log, want none", after-eventsAfterFirst)
	}
}

// TestConflictingNonterminalDuplicateIsFenced is the other half of the replay
// rule. Only the exact identity replays; a different owner, attempt, or
// version expectation is a competing command and must lose.
func TestConflictingNonterminalDuplicateIsFenced(t *testing.T) {
	repo := New(openTestDB(t))
	ctx := context.Background()
	enqueued, claimed, recovering := recoveringRun(t, repo, "worker-owner")

	accepted := ResumeRecoveringRunInput{
		RunID:                enqueued.RunID,
		ExpectedStateVersion: recovering.StateVersion,
		WorkerID:             "worker-owner",
		AssignmentAttemptID:  claimed.AssignmentAttemptID,
	}
	if _, err := repo.ResumeRecoveringRun(ctx, accepted); err != nil {
		t.Fatalf("ResumeRecoveringRun: %v", err)
	}
	committed, err := repo.GetRunState(ctx, enqueued.RunID)
	if err != nil {
		t.Fatalf("GetRunState: %v", err)
	}
	events := countRunEvents(t, repo, enqueued.RunID)

	for name, conflicting := range map[string]ResumeRecoveringRunInput{
		"another worker": {
			RunID: enqueued.RunID, ExpectedStateVersion: recovering.StateVersion,
			WorkerID: "worker-intruder", AssignmentAttemptID: claimed.AssignmentAttemptID,
		},
		"another attempt": {
			RunID: enqueued.RunID, ExpectedStateVersion: recovering.StateVersion,
			WorkerID: "worker-owner", AssignmentAttemptID: "attempt_other",
		},
		"a stale version": {
			RunID: enqueued.RunID, ExpectedStateVersion: recovering.StateVersion - 1,
			WorkerID: "worker-owner", AssignmentAttemptID: claimed.AssignmentAttemptID,
		},
		"a version from the future": {
			RunID: enqueued.RunID, ExpectedStateVersion: recovering.StateVersion + 5,
			WorkerID: "worker-owner", AssignmentAttemptID: claimed.AssignmentAttemptID,
		},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := repo.ResumeRecoveringRun(ctx, conflicting); !errors.Is(err, ErrRunTransitionConflict) {
				t.Fatalf("resume with %s = %v, want ErrRunTransitionConflict", name, err)
			}
			state, err := repo.GetRunState(ctx, enqueued.RunID)
			if err != nil {
				t.Fatal(err)
			}
			if state != committed {
				t.Fatalf("fenced resume changed the row: %+v, want %+v", state, committed)
			}
			if after := countRunEvents(t, repo, enqueued.RunID); after != events {
				t.Fatalf("fenced resume appended %d events, want none", after-events)
			}
		})
	}
}

// TestTerminalRowsRejectEveryLaterTransition states the immutability rule
// exhaustively rather than for the one path that happened to be tested.
func TestTerminalRowsRejectEveryLaterTransition(t *testing.T) {
	for terminalName, terminalize := range map[string]func(context.Context, *Repository, EnqueueUserMessageResult, int64) error{
		"completed": func(ctx context.Context, repo *Repository, run EnqueueUserMessageResult, version int64) error {
			_, err := repo.CompleteRunCanonical(ctx, CompleteRunInput{
				RunID: run.RunID, AssistantMessageID: run.AssistantMessageID,
				Content: "done", ExpectedStateVersion: version,
			})
			return err
		},
		"failed": func(ctx context.Context, repo *Repository, run EnqueueUserMessageResult, version int64) error {
			_, err := repo.FailRunCanonical(ctx, FailRunInput{
				RunID: run.RunID, AssistantMessageID: run.AssistantMessageID,
				ExpectedStateVersion: version, Failure: providerFailureForTest(),
			})
			return err
		},
		"cancelled": func(ctx context.Context, repo *Repository, run EnqueueUserMessageResult, version int64) error {
			_, err := repo.CancelRunCanonical(ctx, CancelRunInput{
				RunID: run.RunID, AssistantMessageID: run.AssistantMessageID,
				ExpectedStateVersion: version, Cancellation: abandonedCancellationForTest(),
			})
			return err
		},
	} {
		t.Run(terminalName, func(t *testing.T) {
			repo := New(openTestDB(t))
			ctx := context.Background()
			enqueued, claimed, _ := recoveringRun(t, repo, "worker-terminal")
			state, err := repo.GetRunState(ctx, enqueued.RunID)
			if err != nil {
				t.Fatal(err)
			}
			if err := terminalize(ctx, repo, enqueued, state.StateVersion); err != nil {
				t.Fatalf("terminalize %s: %v", terminalName, err)
			}
			terminal, err := repo.GetRunState(ctx, enqueued.RunID)
			if err != nil {
				t.Fatal(err)
			}
			events := countRunEvents(t, repo, enqueued.RunID)

			later := map[string]func() error{
				"resume": func() error {
					_, err := repo.ResumeRecoveringRun(ctx, ResumeRecoveringRunInput{
						RunID: enqueued.RunID, ExpectedStateVersion: terminal.StateVersion,
						WorkerID: "worker-terminal", AssignmentAttemptID: claimed.AssignmentAttemptID,
					})
					return err
				},
				"requeue": func() error {
					_, err := repo.RequeueRecoveringRun(ctx, RequeueRecoveringRunInput{
						RunID: enqueued.RunID, ExpectedStateVersion: terminal.StateVersion,
						AssignmentAttemptID: claimed.AssignmentAttemptID,
					})
					return err
				},
				"fence": func() error {
					_, err := repo.FenceRunOwnership(ctx, FenceRunOwnershipInput{
						RunID: enqueued.RunID, ExpectedStateVersion: terminal.StateVersion,
						WorkerID: "worker-terminal", AssignmentAttemptID: claimed.AssignmentAttemptID,
					})
					return err
				},
				"start": func() error { return repo.MarkRunRunning(ctx, enqueued.RunID) },
				"wait for approval": func() error {
					_, err := repo.CreateApproval(ctx, enqueued.RunID, "", "general_assistant",
						"files.update", `{}`, "sha256:x", "2099-01-01T00:00:00Z")
					return err
				},
			}
			// A different terminal outcome from the one already committed is a
			// conflicting report, not a replay, whichever terminal it is.
			for otherName, otherTerminal := range map[string]func(context.Context, *Repository, EnqueueUserMessageResult, int64) error{
				"completed": func(ctx context.Context, repo *Repository, run EnqueueUserMessageResult, version int64) error {
					_, err := repo.CompleteRunCanonical(ctx, CompleteRunInput{
						RunID: run.RunID, AssistantMessageID: run.AssistantMessageID,
						Content: "different", ExpectedStateVersion: version,
					})
					return err
				},
				"failed": func(ctx context.Context, repo *Repository, run EnqueueUserMessageResult, version int64) error {
					_, err := repo.FailRunCanonical(ctx, FailRunInput{
						RunID: run.RunID, AssistantMessageID: run.AssistantMessageID,
						ExpectedStateVersion: version, Failure: providerFailureForTest(),
					})
					return err
				},
				"cancelled": func(ctx context.Context, repo *Repository, run EnqueueUserMessageResult, version int64) error {
					_, err := repo.CancelRunCanonical(ctx, CancelRunInput{
						RunID: run.RunID, AssistantMessageID: run.AssistantMessageID,
						ExpectedStateVersion: version, Cancellation: abandonedCancellationForTest(),
					})
					return err
				},
			} {
				if otherName == terminalName {
					continue
				}
				later["terminalize as "+otherName] = func() error {
					return otherTerminal(ctx, repo, enqueued, terminal.StateVersion)
				}
			}

			for name, attempt := range later {
				t.Run(name, func(t *testing.T) {
					if err := attempt(); err == nil {
						t.Fatalf("%s succeeded on a %s run", name, terminalName)
					}
					state, err := repo.GetRunState(ctx, enqueued.RunID)
					if err != nil {
						t.Fatal(err)
					}
					if state != terminal {
						t.Fatalf("%s mutated a terminal row: %+v, want %+v", name, state, terminal)
					}
					if after := countRunEvents(t, repo, enqueued.RunID); after != events {
						t.Fatalf("%s appended %d events to a terminal run, want none", name, after-events)
					}
				})
			}
		})
	}
}

// TestTransitionRejectsZeroNegativeAndMaxInt64Version pins the stored range for
// every exported transition that takes an expectation.
//
// Zero is protobuf absence and must never be accepted as a version, a negative
// version cannot be produced by any real transition, and the signed maximum has
// no successor to write. Each command is aimed at a run whose lifecycle would
// otherwise have accepted it, so a rejection here is the version guard rather
// than an unrelated lifecycle conflict: absence must not be readable as
// "whatever the row happens to say".
func TestTransitionRejectsZeroNegativeAndMaxInt64Version(t *testing.T) {
	// Each command is paired with the fixture that makes it legal, so nothing
	// but the version can be the reason it is refused.
	for command, invoke := range map[string]func(*testing.T, *Repository, int64) (RunState, error){
		"fence": func(t *testing.T, repo *Repository, version int64) (RunState, error) {
			t.Helper()
			ctx := context.Background()
			enqueued := enqueueRun(t, repo, "Fence version range")
			claimed, err := repo.ClaimNextJob(ctx, "general_assistant", "worker-range")
			if err != nil {
				t.Fatalf("ClaimNextJob: %v", err)
			}
			running, err := repo.GetRunState(ctx, enqueued.RunID)
			if err != nil {
				t.Fatal(err)
			}
			if running.Lifecycle != lifecycleRunning {
				t.Fatalf("fixture lifecycle = %q, want running", running.Lifecycle)
			}
			_, err = repo.FenceRunOwnership(ctx, FenceRunOwnershipInput{
				RunID: enqueued.RunID, ExpectedStateVersion: version,
				WorkerID: "worker-range", AssignmentAttemptID: claimed.AssignmentAttemptID,
			})
			return running, err
		},
		"requeue": func(t *testing.T, repo *Repository, version int64) (RunState, error) {
			t.Helper()
			ctx := context.Background()
			enqueued, claimed, recovering := recoveringRun(t, repo, "worker-range")
			_, err := repo.RequeueRecoveringRun(ctx, RequeueRecoveringRunInput{
				RunID: enqueued.RunID, ExpectedStateVersion: version,
				AssignmentAttemptID: claimed.AssignmentAttemptID,
			})
			return recovering, err
		},
		"resume": func(t *testing.T, repo *Repository, version int64) (RunState, error) {
			t.Helper()
			ctx := context.Background()
			enqueued, claimed, recovering := recoveringRun(t, repo, "worker-range")
			_, err := repo.ResumeRecoveringRun(ctx, ResumeRecoveringRunInput{
				RunID: enqueued.RunID, ExpectedStateVersion: version,
				WorkerID: "worker-range", AssignmentAttemptID: claimed.AssignmentAttemptID,
			})
			return recovering, err
		},
	} {
		t.Run(command, func(t *testing.T) {
			for name, version := range map[string]int64{
				"absent (zero)": 0,
				"negative":      -1,
				"minimum int64": math.MinInt64,
			} {
				t.Run(name, func(t *testing.T) {
					repo := New(openTestDB(t))
					ctx := context.Background()
					before, err := invoke(t, repo, version)
					if !errors.Is(err, ErrRunStateVersionInvalid) {
						t.Fatalf("%s at version %d = %v, want ErrRunStateVersionInvalid", command, version, err)
					}
					state, err := repo.GetRunState(ctx, before.RunID)
					if err != nil {
						t.Fatal(err)
					}
					if state != before {
						t.Fatalf("rejected version changed the row: %+v, want %+v", state, before)
					}
				})
			}
		})
	}

	// The maximum is a legal stored value but not a legal source for another
	// transition, so it is rejected here too.
	repo := New(openTestDB(t))
	ctx := context.Background()
	enqueued, claimed, _ := recoveringRun(t, repo, "worker-range-max")
	if _, err := repo.db.ExecContext(ctx,
		`UPDATE agent_runs SET state_version = ? WHERE id = ?`, int64(math.MaxInt64), enqueued.RunID); err != nil {
		t.Fatalf("seed maximum version: %v", err)
	}
	if _, err := repo.ResumeRecoveringRun(ctx, ResumeRecoveringRunInput{
		RunID: enqueued.RunID, ExpectedStateVersion: math.MaxInt64,
		WorkerID: "worker-range-max", AssignmentAttemptID: claimed.AssignmentAttemptID,
	}); err == nil {
		t.Fatal("a transition at the signed maximum succeeded")
	}
}

// TestTransitionAtMaxInt64ReturnsRunStateVersionExhausted pins the exact
// sentinel. It is value-free on purpose: an exhaustion message that quoted the
// version would be the first place a row value leaked into an error string.
func TestTransitionAtMaxInt64ReturnsRunStateVersionExhausted(t *testing.T) {
	repo := New(openTestDB(t))
	ctx := context.Background()
	enqueued, claimed, _ := recoveringRun(t, repo, "worker-exhausted")
	if _, err := repo.db.ExecContext(ctx,
		`UPDATE agent_runs SET state_version = ? WHERE id = ?`, int64(math.MaxInt64), enqueued.RunID); err != nil {
		t.Fatalf("seed maximum version: %v", err)
	}
	exhausted, err := repo.GetRunState(ctx, enqueued.RunID)
	if err != nil {
		t.Fatal(err)
	}
	events := countRunEvents(t, repo, enqueued.RunID)

	_, err = repo.ResumeRecoveringRun(ctx, ResumeRecoveringRunInput{
		RunID: enqueued.RunID, ExpectedStateVersion: math.MaxInt64,
		WorkerID: "worker-exhausted", AssignmentAttemptID: claimed.AssignmentAttemptID,
	})
	if !errors.Is(err, ErrRunStateVersionExhausted) {
		t.Fatalf("resume at the maximum = %v, want ErrRunStateVersionExhausted", err)
	}
	if err.Error() != "run state version exhausted" {
		t.Fatalf("exhaustion error = %q, want the exact value-free sentinel %q", err.Error(), "run state version exhausted")
	}
	state, err := repo.GetRunState(ctx, enqueued.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if state != exhausted {
		t.Fatalf("exhausted transition changed the row: %+v, want %+v", state, exhausted)
	}
	if after := countRunEvents(t, repo, enqueued.RunID); after != events {
		t.Fatalf("exhausted transition appended %d events, want none", after-events)
	}
}

// TestTransitionTimeAdvancesWhenClockRegresses covers a host clock that steps
// backwards. state_updated_at is compared as text, so a row that went backwards
// would sort before its own predecessor.
func TestTransitionTimeAdvancesWhenClockRegresses(t *testing.T) {
	repo := New(openTestDB(t))
	ctx := context.Background()
	enqueued, claimed, _ := recoveringRun(t, repo, "worker-clock")

	// A prior timestamp far ahead of any real clock reading forces the
	// monotonic branch: the next value can only be prior + 1ns.
	const future = "2999-01-01T00:00:00.000000000Z"
	if _, err := repo.db.ExecContext(ctx,
		`UPDATE agent_runs SET state_updated_at = ? WHERE id = ?`, future, enqueued.RunID); err != nil {
		t.Fatalf("seed future timestamp: %v", err)
	}
	ahead, err := repo.GetRunState(ctx, enqueued.RunID)
	if err != nil {
		t.Fatal(err)
	}
	resumed, err := repo.ResumeRecoveringRun(ctx, ResumeRecoveringRunInput{
		RunID: enqueued.RunID, ExpectedStateVersion: ahead.StateVersion,
		WorkerID: "worker-clock", AssignmentAttemptID: claimed.AssignmentAttemptID,
	})
	if err != nil {
		t.Fatalf("ResumeRecoveringRun: %v", err)
	}
	const wantNext = "2999-01-01T00:00:00.000000001Z"
	if resumed.State.StateUpdatedAt != wantNext {
		t.Fatalf("state_updated_at = %q, want %q", resumed.State.StateUpdatedAt, wantNext)
	}
	if len(resumed.Events) != 1 {
		t.Fatalf("resume appended %d events, want exactly 1", len(resumed.Events))
	}
	assertSnapshotMatchesState(t, decodeRunStateSnapshot(t, resumed.Events[0]), resumed.State)
}

// TestTransitionTimeOverflowRollsBackRowAndEvent covers the other end of the
// range. There is no representable next instant, so the transition must leave
// the row and the log exactly as it found them rather than write a wrapped or
// truncated value.
func TestTransitionTimeOverflowRollsBackRowAndEvent(t *testing.T) {
	repo := New(openTestDB(t))
	ctx := context.Background()
	enqueued, claimed, _ := recoveringRun(t, repo, "worker-overflow")

	const last = "9999-12-31T23:59:59.999999999Z"
	if _, err := repo.db.ExecContext(ctx,
		`UPDATE agent_runs SET state_updated_at = ? WHERE id = ?`, last, enqueued.RunID); err != nil {
		t.Fatalf("seed final instant: %v", err)
	}
	before, err := repo.GetRunState(ctx, enqueued.RunID)
	if err != nil {
		t.Fatal(err)
	}
	events := countRunEvents(t, repo, enqueued.RunID)

	if _, err := repo.ResumeRecoveringRun(ctx, ResumeRecoveringRunInput{
		RunID: enqueued.RunID, ExpectedStateVersion: before.StateVersion,
		WorkerID: "worker-overflow", AssignmentAttemptID: claimed.AssignmentAttemptID,
	}); !errors.Is(err, persisttime.ErrTimestampRangeExhausted) {
		t.Fatalf("resume at the final instant = %v, want persisttime.ErrTimestampRangeExhausted", err)
	}
	after, err := repo.GetRunState(ctx, enqueued.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if after != before {
		t.Fatalf("overflowing transition changed the row: %+v, want %+v", after, before)
	}
	if got := countRunEvents(t, repo, enqueued.RunID); got != events {
		t.Fatalf("overflowing transition appended %d events, want none", got-events)
	}
}

// TestRepeatedRequeueIsAWriteFreeDuplicateDespiteClearedOwnership covers the
// one transition whose own effect erases the identity it was fenced on.
//
// A requeue releases the worker and the attempt, so a replay of the same
// command cannot be recognized by matching them — the row has neither any more.
// It is recognized by the absence the requeue itself produced, which is why
// this needs its own test: the obvious identity comparison would report a
// conflict for a command that already succeeded.
func TestRepeatedRequeueIsAWriteFreeDuplicateDespiteClearedOwnership(t *testing.T) {
	repo := New(openTestDB(t))
	ctx := context.Background()
	enqueued, claimed, recovering := recoveringRun(t, repo, "worker-requeue-replay")

	requeue := RequeueRecoveringRunInput{
		RunID:                enqueued.RunID,
		ExpectedStateVersion: recovering.StateVersion,
		AssignmentAttemptID:  claimed.AssignmentAttemptID,
	}
	first, err := repo.RequeueRecoveringRun(ctx, requeue)
	if err != nil {
		t.Fatalf("RequeueRecoveringRun: %v", err)
	}
	events := countRunEvents(t, repo, enqueued.RunID)

	second, err := repo.RequeueRecoveringRun(ctx, requeue)
	if err != nil {
		t.Fatalf("replayed RequeueRecoveringRun: %v", err)
	}
	if !second.Duplicate || len(second.Events) != 0 {
		t.Fatalf("replayed requeue = %+v, want a write-free duplicate", second)
	}
	if second.State != first.State {
		t.Fatalf("replayed requeue state = %+v, want %+v", second.State, first.State)
	}
	if after := countRunEvents(t, repo, enqueued.RunID); after != events {
		t.Fatalf("replayed requeue appended %d events", after-events)
	}
}

// TestCanonicalEventOmitsAbsentAssistantIdentityFromRunState carries the rule
// the run-outcomes migration already follows into the live writer. The
// migration refuses to publish an assistant message ID it could not prove, and
// drops the key rather than publishing an empty one, because a client that
// reads assistantMessageId as present-but-empty would treat "" as an identity
// and look for a message nobody has.
//
// The live repository can reach the same shape: a run whose assistant link is
// absent — a migrated legacy run, or one whose message did not survive — still
// terminalizes, because the guarded transition only demands correlation of a
// run that claims a link at all. Everything else the projection knows is still
// canonical and must survive.
func TestCanonicalEventOmitsAbsentAssistantIdentityFromRunState(t *testing.T) {
	database := openTestDB(t)
	repo := New(database)
	ctx := context.Background()
	unlinked := enqueueRun(t, repo, "Absent assistant link")
	// Strip the link the way legacy history arrives: neither side names the
	// other, so there is nothing to prove rather than something that disagrees.
	if _, err := database.ExecContext(ctx,
		`UPDATE agent_runs SET assistant_message_id = NULL WHERE id = ?`, unlinked.RunID); err != nil {
		t.Fatalf("clear the run's assistant pointer: %v", err)
	}
	if _, err := database.ExecContext(ctx,
		`DELETE FROM messages WHERE id = ?`, unlinked.AssistantMessageID); err != nil {
		t.Fatalf("delete the assistant message: %v", err)
	}
	queued, err := repo.GetRunState(ctx, unlinked.RunID)
	if err != nil {
		t.Fatalf("GetRunState: %v", err)
	}
	if queued.AssistantMessageID != "" {
		t.Fatalf("unlinked run still names assistant message %q", queued.AssistantMessageID)
	}

	cancelled, err := repo.CancelRunCanonical(ctx, CancelRunInput{
		RunID:                unlinked.RunID,
		ExpectedStateVersion: queued.StateVersion,
		Cancellation:         abandonedCancellationForTest(),
	})
	if err != nil {
		t.Fatalf("CancelRunCanonical: %v", err)
	}
	// The durable payload is asserted alongside the returned one: a client that
	// reopens the session reads the stored bytes, not the returned struct.
	terminal := onlyCancelledEvent(t, cancelled.Events)
	storedPayload := storedEventPayload(t, database, terminal.EventID)
	wantState := map[string]any{
		"runId":                 unlinked.RunID,
		"userMessageId":         unlinked.UserMessageID,
		"lifecycle":             "cancelled",
		"outcomeReason":         string(runoutcome.ReasonAbandoned),
		"stateVersion":          float64(cancelled.State.StateVersion),
		"stateUpdatedAt":        cancelled.State.StateUpdatedAt,
		"finishedAt":            cancelled.State.FinishedAt.String,
		"hasDisplayableContent": false,
	}
	for name, payloadJSON := range map[string]string{
		"returned": terminal.PayloadJSON,
		"durable":  storedPayload,
	} {
		t.Run(name, func(t *testing.T) {
			state := decodeRunStateMap(t, payloadJSON)
			// Named separately from the comparison below so a regression that
			// publishes the empty identity reports what it actually did.
			if published, present := state["assistantMessageId"]; present {
				t.Fatalf("%s snapshot published assistantMessageId = %#v for a run with no assistant message",
					name, published)
			}
			if !reflect.DeepEqual(state, wantState) {
				t.Fatalf("%s snapshot = %#v, want %#v", name, state, wantState)
			}
		})
	}

	// The omission is absence, not a blanket removal: a run that does own an
	// assistant message still publishes its identity, because that is the ID a
	// client needs to attach the answer to.
	linked := enqueueRun(t, repo, "Present assistant link")
	linkedState, err := repo.GetRunState(ctx, linked.RunID)
	if err != nil {
		t.Fatalf("GetRunState: %v", err)
	}
	linkedCancelled, err := repo.CancelRunCanonical(ctx, CancelRunInput{
		RunID:                linked.RunID,
		ExpectedStateVersion: linkedState.StateVersion,
		Cancellation:         abandonedCancellationForTest(),
	})
	if err != nil {
		t.Fatalf("CancelRunCanonical: %v", err)
	}
	linkedTerminal := onlyCancelledEvent(t, linkedCancelled.Events)
	for name, payloadJSON := range map[string]string{
		"returned": linkedTerminal.PayloadJSON,
		"durable":  storedEventPayload(t, database, linkedTerminal.EventID),
	} {
		t.Run("linked "+name, func(t *testing.T) {
			if got := decodeRunStateMap(t, payloadJSON)["assistantMessageId"]; got != linked.AssistantMessageID {
				t.Fatalf("linked %s snapshot assistantMessageId = %#v, want %q",
					name, got, linked.AssistantMessageID)
			}
		})
	}
}

// onlyCancelledEvent returns the single cancellation projection a terminal
// transition appended. It fails rather than returning a zero Event, so an
// assertion about the projection cannot pass by never running.
func onlyCancelledEvent(t *testing.T, events []Event) Event {
	t.Helper()
	var found []Event
	for _, event := range events {
		if event.Type == "agent.run.cancelled" {
			found = append(found, event)
		}
	}
	if len(found) != 1 {
		t.Fatalf("cancellation appended %d agent.run.cancelled projections in %+v, want exactly 1",
			len(found), events)
	}
	return found[0]
}

func storedEventPayload(t *testing.T, database *db.DB, eventID string) string {
	t.Helper()
	var payloadJSON string
	if err := database.QueryRowContext(context.Background(),
		`SELECT payload_json FROM events WHERE id = ?`, eventID).Scan(&payloadJSON); err != nil {
		t.Fatalf("read stored event %q: %v", eventID, err)
	}
	return payloadJSON
}
