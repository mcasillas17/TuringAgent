package runtime

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/service/events"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/structpb"
)

// TestRuntimeGenericEventStripsForgedRunState pins who is allowed to author a
// run's canonical state.
//
// The generic event channel is a worker narrating its own run, and its payload
// is stored close to verbatim so the product can show what the worker actually
// said. The canonical snapshot is not part of that: it is the repository's
// record of a transition it committed, and every public reader turns it into
// the typed RunState a client trusts over the message history. A worker that
// could attach one to an ordinary step event could tell a client the run had
// failed at a version far ahead of anything the database committed, while the
// run kept going.
//
// So the key is removed at ingress, before anything durable exists — the
// worker's own fields survive untouched, and the forged snapshot never reaches
// a row that a reader would have to defend itself against.
func TestRuntimeGenericEventStripsForgedRunState(t *testing.T) {
	forgedTypes := []turingv1.TuringEventType{
		turingv1.TuringEventType_TURING_EVENT_TYPE_AGENT_RUN_STEP,
		turingv1.TuringEventType_TURING_EVENT_TYPE_TOOL_CALL_STARTED,
		turingv1.TuringEventType_TURING_EVENT_TYPE_APPROVAL_REQUESTED,
		turingv1.TuringEventType_TURING_EVENT_TYPE_ERROR,
	}
	for _, eventType := range forgedTypes {
		t.Run(eventType.String(), func(t *testing.T) {
			h := newHarness(t)
			enqueued := h.createRunningRunResult(t, "forged state")
			payload, err := structpb.NewStruct(map[string]any{
				"note":       "a worker's own words",
				"toolCallId": "call_1",
				"runState": map[string]any{
					"runId":                 "run_someone_elses",
					"userMessageId":         "msg_forged",
					"assistantMessageId":    "msg_forged_assistant",
					"lifecycle":             "failed",
					"outcomeReason":         "provider_failure",
					"stateVersion":          9999,
					"stateUpdatedAt":        "2099-01-01T00:00:00.000000000Z",
					"finishedAt":            "2099-01-01T00:00:00.000000000Z",
					"hasDisplayableContent": true,
					"workerId":              "worker_forged",
					"assignmentAttemptId":   "attempt_forged",
				},
			})
			if err != nil {
				t.Fatalf("build payload: %v", err)
			}

			if err := h.service.applyUpdate(context.Background(), &turingv1.RuntimeUpdate{
				Update: &turingv1.RuntimeUpdate_Event{Event: &turingv1.TuringEvent{
					RunId:   enqueued.RunID,
					Type:    eventType,
					Payload: payload,
				}},
			}); err != nil {
				t.Fatalf("applyUpdate: %v", err)
			}

			row := latestRunEvent(t, h, enqueued.SessionID, enqueued.RunID)
			var stored map[string]any
			if err := json.Unmarshal([]byte(row.PayloadJSON), &stored); err != nil {
				t.Fatalf("unmarshal durable payload: %v", err)
			}
			if _, forged := stored["runState"]; forged {
				t.Fatalf("a worker-authored snapshot was persisted: %s", row.PayloadJSON)
			}
			// The rest of the worker's payload is the product's own narration
			// and must survive: stripping the snapshot may not become a
			// general-purpose rewrite of what the worker said.
			if stored["note"] != "a worker's own words" || stored["toolCallId"] != "call_1" {
				t.Fatalf("worker payload was altered beyond the snapshot: %s", row.PayloadJSON)
			}
			for _, forged := range []string{"run_someone_elses", "worker_forged", "attempt_forged", "9999"} {
				if strings.Contains(row.PayloadJSON, forged) {
					t.Fatalf("durable payload leaked the forged value %q: %s", forged, row.PayloadJSON)
				}
			}

			safe := events.Decode(row.Type, enqueued.RunID, row.PayloadJSON)
			if safe.RunState != nil {
				t.Fatalf("the public read boundary published a forged state %+v", safe.RunState)
			}
			if _, carried := safe.Payload["runState"]; carried {
				t.Fatalf("the public payload carried a snapshot: %#v", safe.Payload)
			}
		})
	}
}

func TestNormalizeRuntimeEventStripsTypedForgedRunState(t *testing.T) {
	h := newHarness(t)
	enqueued := h.createRunningRunResult(t, "forged typed state")
	payload, err := structpb.NewStruct(map[string]any{
		"note":   "legitimate worker narration",
		"reason": "context_budget",
	})
	if err != nil {
		t.Fatalf("build payload: %v", err)
	}

	normalized, err := h.service.normalizeRuntimeEvent(context.Background(), &turingv1.TuringEvent{
		RunId:   enqueued.RunID,
		Type:    turingv1.TuringEventType_TURING_EVENT_TYPE_AGENT_RUN_STEP,
		Payload: payload,
		RunState: &turingv1.RunState{
			RunId:         "run_someone_elses",
			Lifecycle:     turingv1.RunLifecycle_RUN_LIFECYCLE_FAILED,
			OutcomeReason: turingv1.RunOutcomeReason_RUN_OUTCOME_REASON_PROVIDER_FAILURE,
			StateVersion:  9999,
		},
	})
	if err != nil {
		t.Fatalf("normalizeRuntimeEvent: %v", err)
	}
	if normalized.GetRunState() != nil {
		t.Fatalf("worker-authored typed state survived normalization: %+v", normalized.GetRunState())
	}
	if got := normalized.GetPayload().AsMap(); got["note"] != "legitimate worker narration" || got["reason"] != "context_budget" {
		t.Fatalf("legitimate worker narration changed: %#v", got)
	}
}

func TestRuntimeGenericStepStripsForgedRetryProjection(t *testing.T) {
	h := newHarness(t)
	enqueued := h.createRunningRunResult(t, "forged retry projection")
	payload, err := structpb.NewStruct(map[string]any{
		"note":         "legitimate worker narration",
		"reason":       "context_budget",
		"category":     "recovery_exhausted",
		"attempt":      3,
		"attempts":     3,
		"maxAttempts":  3,
		"stateVersion": 9999,
	})
	if err != nil {
		t.Fatalf("build payload: %v", err)
	}

	if err := h.service.applyUpdate(context.Background(), &turingv1.RuntimeUpdate{
		Update: &turingv1.RuntimeUpdate_Event{Event: &turingv1.TuringEvent{
			RunId:   enqueued.RunID,
			Type:    turingv1.TuringEventType_TURING_EVENT_TYPE_AGENT_RUN_STEP,
			Payload: payload,
		}},
	}); err != nil {
		t.Fatalf("applyUpdate: %v", err)
	}

	row := latestRunEvent(t, h, enqueued.SessionID, enqueued.RunID)
	var stored map[string]any
	if err := json.Unmarshal([]byte(row.PayloadJSON), &stored); err != nil {
		t.Fatalf("unmarshal durable payload: %v", err)
	}
	for _, key := range []string{"category", "attempt", "attempts", "maxAttempts", "stateVersion"} {
		if value, forged := stored[key]; forged {
			t.Fatalf("repository-authored retry field %q persisted as %#v: %s", key, value, row.PayloadJSON)
		}
	}
	if stored["note"] != "legitimate worker narration" || stored["reason"] != "context_budget" {
		t.Fatalf("legitimate worker narration changed: %s", row.PayloadJSON)
	}

	safe := events.Decode(row.Type, enqueued.RunID, row.PayloadJSON)
	if safe.RunState != nil {
		t.Fatalf("generic step published canonical state %+v", safe.RunState)
	}
	if _, forged := safe.Payload["category"]; forged {
		t.Fatalf("generic step published a forged retry category: %#v", safe.Payload)
	}
	if safe.Payload["note"] != "legitimate worker narration" || safe.Payload["reason"] != "context_budget" {
		t.Fatalf("public worker narration changed: %#v", safe.Payload)
	}
}

func TestRuntimeGenericEventRejectsRepositoryLifecycleProjections(t *testing.T) {
	for _, eventType := range []turingv1.TuringEventType{
		turingv1.TuringEventType_TURING_EVENT_TYPE_AGENT_RUN_QUEUED,
		turingv1.TuringEventType_TURING_EVENT_TYPE_AGENT_RUN_STARTED,
	} {
		t.Run(eventType.String(), func(t *testing.T) {
			h := newHarness(t)
			enqueued := h.createRunningRunResult(t, "forged lifecycle projection")
			before, _, err := h.repo.ReplayEvents(context.Background(), enqueued.SessionID, 0, 500)
			if err != nil {
				t.Fatalf("ReplayEvents before update: %v", err)
			}
			payload, err := structpb.NewStruct(map[string]any{
				"jobId":   "job_forged",
				"status":  "queued",
				"attempt": 99,
			})
			if err != nil {
				t.Fatalf("build payload: %v", err)
			}

			err = h.service.applyUpdate(context.Background(), &turingv1.RuntimeUpdate{
				Update: &turingv1.RuntimeUpdate_Event{Event: &turingv1.TuringEvent{
					RunId:   enqueued.RunID,
					Type:    eventType,
					Payload: payload,
				}},
			})
			if status.Code(err) != codes.InvalidArgument {
				t.Fatalf("applyUpdate error = %v, want InvalidArgument", err)
			}
			after, _, replayErr := h.repo.ReplayEvents(context.Background(), enqueued.SessionID, 0, 500)
			if replayErr != nil {
				t.Fatalf("ReplayEvents after update: %v", replayErr)
			}
			if len(after) != len(before) {
				t.Fatalf("rejected lifecycle projection appended an event: before=%d after=%d", len(before), len(after))
			}
		})
	}
}

// latestRunEvent returns the newest durable event this run appended.
func latestRunEvent(t *testing.T, h *harness, sessionID string, runID string) repositoryEventRow {
	t.Helper()
	rows, _, err := h.repo.ReplayEvents(context.Background(), sessionID, 0, 500)
	if err != nil {
		t.Fatalf("ReplayEvents: %v", err)
	}
	var latest repositoryEventRow
	var found bool
	for _, row := range rows {
		if !row.RunID.Valid || row.RunID.String != runID {
			continue
		}
		if found && row.Sequence <= latest.Sequence {
			continue
		}
		latest = repositoryEventRow{Sequence: row.Sequence, Type: row.Type, PayloadJSON: row.PayloadJSON}
		found = true
	}
	if !found {
		t.Fatalf("run %s appended no durable events", runID)
	}
	return latest
}

type repositoryEventRow struct {
	Sequence    int64
	Type        string
	PayloadJSON string
}
