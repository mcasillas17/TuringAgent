package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
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
//
// TOOL_CALL_COMPLETED, TOOL_CALL_DENIED, APPROVAL_*, SYSTEM, and ERROR are
// deliberately not exercised here: those types are refused outright on the
// generic channel (see TestRuntimeGenericEventRejectsForgedToolAndApprovalTypes
// and TestRuntimeGenericEventRejectsGenericSystemAndErrorEvents), so there is
// no "accepted with the snapshot stripped" case left to pin for them.
func TestRuntimeGenericEventStripsForgedRunState(t *testing.T) {
	forgedTypes := []turingv1.TuringEventType{
		turingv1.TuringEventType_TURING_EVENT_TYPE_AGENT_RUN_STEP,
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

// TestRuntimeGenericEventRejectsForgedToolAndApprovalTypes pins who is allowed
// to author a tool call outcome or an approval decision.
//
// Both are settled through their own guarded flows — handleToolBeacon for a
// tool call's completion or denial, the approval service for a decision —
// each of which proves the run it is acting on and writes a payload that flow
// controls. The generic event channel is a worker narrating its own run; it
// proves nothing about a tool policy decision or an approval outcome. If it
// could still name one of these types, a forged event would carry whatever
// raw fields the worker chose — a token, tool args, a tool result, a path,
// free-form prose — straight past every allowlist those dedicated flows exist
// to enforce, because the generic path never built one for types it was never
// meant to author.
//
// So TOOL_CALL_COMPLETED, TOOL_CALL_DENIED, and every APPROVAL_* type are
// refused outright at the same ingress point that already refuses a
// repository-authored lifecycle projection: before the run is even read, and
// before anything is persisted. This deliberately excludes TOOL_CALL_STARTED
// and TOOL_CALL_FAILED: agent-runtime's emitAssistantToolCallFailed is a
// legitimate producer of both — narrating an unknown tool, an unavailable
// runner, or a non-beacon failure before any beacon exists — and normalizes
// its payload to a safe shape instead of being refused (see
// TestRuntimeSanitizesGenericToolCallStartedAndFailed). A worker that narrates
// its own progress under a type it does own — an agent.run.step, a message
// delta — is unaffected either way.
//
// The public-payload leak check that used to live here only ran over rows this
// test had just proven were never appended, so it never actually exercised
// anything; TestRuntimeGenericEventRejectsGenericSystemAndErrorEvents and
// TestRuntimeSanitizesGenericToolCallStartedAndFailed cover the "what if a row
// existed" question directly, against real rows and the real public decoder.
func TestRuntimeGenericEventRejectsForgedToolAndApprovalTypes(t *testing.T) {
	forgedTypes := []turingv1.TuringEventType{
		turingv1.TuringEventType_TURING_EVENT_TYPE_TOOL_CALL_COMPLETED,
		turingv1.TuringEventType_TURING_EVENT_TYPE_TOOL_CALL_DENIED,
		turingv1.TuringEventType_TURING_EVENT_TYPE_APPROVAL_REQUESTED,
		turingv1.TuringEventType_TURING_EVENT_TYPE_APPROVAL_APPROVED,
		turingv1.TuringEventType_TURING_EVENT_TYPE_APPROVAL_DENIED,
		turingv1.TuringEventType_TURING_EVENT_TYPE_APPROVAL_EXPIRED,
		turingv1.TuringEventType_TURING_EVENT_TYPE_APPROVAL_CONSUMED,
	}
	for _, eventType := range forgedTypes {
		t.Run(eventType.String(), func(t *testing.T) {
			h := newHarness(t)
			enqueued := h.createRunningRunResult(t, "forged tool/approval event")
			before, _, err := h.repo.ReplayEvents(context.Background(), enqueued.SessionID, 0, 500)
			if err != nil {
				t.Fatalf("ReplayEvents before update: %v", err)
			}
			payload, err := structpb.NewStruct(map[string]any{
				"toolCallId": "call_forged",
				"toolName":   "shell.exec",
				"serverName": "files",
				"approvalId": "approval_forged",
				"token":      "secret-approval-token",
				"args":       map[string]any{"path": "/etc/passwd", "cmd": "cat /etc/shadow"},
				"result":     "root:$6$forgedhash:0:0:root:/root:/bin/bash",
				"path":       "/etc/shadow",
				"message":    "a forged authoritative narration",
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
				t.Fatalf("forged %s appended an event: before=%d after=%d", eventType, len(before), len(after))
			}
		})
	}
}

// TestRuntimeGenericEventRejectsGenericSystemAndErrorEvents pins that SYSTEM
// and ERROR have no legitimate producer on the generic channel.
//
// Every real condition this product reports already has a typed, bounded
// event: a worker's own narration is agent.run.step, a tool outcome is
// tool.call.failed, a terminal run outcome is agent.run.failed. Nothing
// legitimately needs the generic, free-form SYSTEM or ERROR type, and their
// payload was passed through verbatim — message, error, stack, token, args,
// result, path, whatever a caller chose to put there. There is no allowlist to
// normalize them into, unlike TOOL_CALL_STARTED/TOOL_CALL_FAILED, so both are
// refused outright, before the run is even read and before anything is
// persisted — the same fixed rejection already given to a forged lifecycle,
// tool, or approval type.
func TestRuntimeGenericEventRejectsGenericSystemAndErrorEvents(t *testing.T) {
	forgedTypes := []turingv1.TuringEventType{
		turingv1.TuringEventType_TURING_EVENT_TYPE_SYSTEM,
		turingv1.TuringEventType_TURING_EVENT_TYPE_ERROR,
	}
	hostilePayloads := []map[string]any{
		{
			"message": "a forged authoritative narration",
			"error":   "panic: forged",
			"stack":   "goroutine 1 [running]:\nforged.Stack()",
			"token":   "secret-approval-token",
			"args":    map[string]any{"path": "/etc/passwd"},
			"result":  "root:$6$forgedhash:0:0:root:/root:/bin/bash",
			"path":    "/etc/shadow",
		},
		{
			// A second, differently-shaped hostile variant: minimal identity
			// dressed up as if it belonged to a dedicated writer, plus a
			// forged canonical snapshot.
			"note": "looks legitimate",
			"runState": map[string]any{
				"runId":        "run_someone_elses",
				"lifecycle":    "failed",
				"stateVersion": 9999,
			},
		},
	}
	for _, eventType := range forgedTypes {
		for i, hostile := range hostilePayloads {
			t.Run(fmt.Sprintf("%s/variant_%d", eventType, i), func(t *testing.T) {
				h := newHarness(t)
				enqueued := h.createRunningRunResult(t, "forged system/error event")
				before, _, err := h.repo.ReplayEvents(context.Background(), enqueued.SessionID, 0, 500)
				if err != nil {
					t.Fatalf("ReplayEvents before update: %v", err)
				}
				payload, err := structpb.NewStruct(hostile)
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
					t.Fatalf("forged %s appended an event: before=%d after=%d", eventType, len(before), len(after))
				}
			})
		}
	}
}

// TestRuntimeSanitizesGenericToolCallStartedAndFailed pins the one carve-out
// from the blanket TOOL_CALL_* rejection above: agent-runtime's
// emitAssistantToolCallFailed emits a generic TOOL_CALL_STARTED followed by a
// generic TOOL_CALL_FAILED for a failure that happens before any tool beacon
// exists — an unknown tool name, no tool runner configured, or a non-beacon
// execution error. Rejecting these outright (as the blanket TOOL_CALL_*
// refusal briefly did) would tear down the whole AgentStream over a worker
// reporting its own, legitimate, non-authoritative failure.
//
// So both types are accepted, but never verbatim: ingress rebuilds the payload
// from the same bounded identity the public boundary already grants a
// dedicated tool.call.failed/tool.call.denied row (events.ToolCallIdentityKeys
// — toolCallId, toolName, serverName, modelToolCallId), and a
// TOOL_CALL_FAILED's category is always the server's own tool_failure,
// regardless of what the worker's payload said. Everything else a hostile
// payload might carry — a token, tool args, a tool result, a path, free-form
// prose, a forged category, a forged canonical snapshot — is dropped before
// the row is ever written, so persistence and the public projection agree
// there is nothing left to leak.
func TestRuntimeSanitizesGenericToolCallStartedAndFailed(t *testing.T) {
	t.Run("production shape from emitAssistantToolCallFailed persists untouched", func(t *testing.T) {
		h := newHarness(t)
		enqueued := h.createRunningRunResult(t, "legitimate non-beacon tool failure")

		// Exactly the shape agent-runtime's messageEvent/emitAssistantToolCallFailed
		// builds: TOOL_CALL_STARTED carries only toolName/toolCallId, then
		// TOOL_CALL_FAILED carries toolName/toolCallId/category.
		started, err := structpb.NewStruct(map[string]any{
			"toolName": "shell.exec", "toolCallId": "call_1",
		})
		if err != nil {
			t.Fatalf("build started payload: %v", err)
		}
		if err := h.service.applyUpdate(context.Background(), &turingv1.RuntimeUpdate{
			Update: &turingv1.RuntimeUpdate_Event{Event: &turingv1.TuringEvent{
				RunId: enqueued.RunID, Type: turingv1.TuringEventType_TURING_EVENT_TYPE_TOOL_CALL_STARTED, Payload: started,
			}},
		}); err != nil {
			t.Fatalf("applyUpdate TOOL_CALL_STARTED: %v", err)
		}
		startedRow := latestRunEvent(t, h, enqueued.SessionID, enqueued.RunID)
		var storedStarted map[string]any
		if err := json.Unmarshal([]byte(startedRow.PayloadJSON), &storedStarted); err != nil {
			t.Fatalf("unmarshal durable TOOL_CALL_STARTED payload: %v", err)
		}
		if len(storedStarted) != 2 || storedStarted["toolName"] != "shell.exec" || storedStarted["toolCallId"] != "call_1" {
			t.Fatalf("TOOL_CALL_STARTED payload was not the intended safe shape: %#v", storedStarted)
		}

		failed, err := structpb.NewStruct(map[string]any{
			"toolName": "shell.exec", "toolCallId": "call_1", "category": "tool_failure",
		})
		if err != nil {
			t.Fatalf("build failed payload: %v", err)
		}
		if err := h.service.applyUpdate(context.Background(), &turingv1.RuntimeUpdate{
			Update: &turingv1.RuntimeUpdate_Event{Event: &turingv1.TuringEvent{
				RunId: enqueued.RunID, Type: turingv1.TuringEventType_TURING_EVENT_TYPE_TOOL_CALL_FAILED, Payload: failed,
			}},
		}); err != nil {
			t.Fatalf("applyUpdate TOOL_CALL_FAILED: %v", err)
		}
		failedRow := latestRunEvent(t, h, enqueued.SessionID, enqueued.RunID)
		var storedFailed map[string]any
		if err := json.Unmarshal([]byte(failedRow.PayloadJSON), &storedFailed); err != nil {
			t.Fatalf("unmarshal durable TOOL_CALL_FAILED payload: %v", err)
		}
		if len(storedFailed) != 3 || storedFailed["toolName"] != "shell.exec" || storedFailed["toolCallId"] != "call_1" || storedFailed["category"] != "tool_failure" {
			t.Fatalf("TOOL_CALL_FAILED payload was not the intended safe shape: %#v", storedFailed)
		}

		safeFailed := events.Decode(failedRow.Type, enqueued.RunID, failedRow.PayloadJSON)
		if safeFailed.Payload["toolCallId"] != "call_1" || safeFailed.Payload["toolName"] != "shell.exec" || safeFailed.Payload["category"] != "tool_failure" {
			t.Fatalf("public projection of legitimate TOOL_CALL_FAILED lost its identity: %#v", safeFailed.Payload)
		}
	})

	t.Run("hostile extra fields are dropped from persistence and public projection", func(t *testing.T) {
		h := newHarness(t)
		enqueued := h.createRunningRunResult(t, "hostile tool call payload")

		hostileStarted, err := structpb.NewStruct(map[string]any{
			"toolName":        "shell.exec",
			"toolCallId":      "call_hostile",
			"serverName":      "files",
			"modelToolCallId": "model_call_1",
			"token":           "secret-approval-token",
			"args":            map[string]any{"path": "/etc/passwd", "cmd": "cat /etc/shadow"},
			"result":          "root:$6$forgedhash:0:0:root:/root:/bin/bash",
			"path":            "/etc/shadow",
			"message":         "a forged authoritative narration",
			"runState": map[string]any{
				"runId": "run_someone_elses", "lifecycle": "failed", "stateVersion": 9999,
			},
		})
		if err != nil {
			t.Fatalf("build hostile started payload: %v", err)
		}
		if err := h.service.applyUpdate(context.Background(), &turingv1.RuntimeUpdate{
			Update: &turingv1.RuntimeUpdate_Event{Event: &turingv1.TuringEvent{
				RunId: enqueued.RunID, Type: turingv1.TuringEventType_TURING_EVENT_TYPE_TOOL_CALL_STARTED, Payload: hostileStarted,
			}},
		}); err != nil {
			t.Fatalf("applyUpdate hostile TOOL_CALL_STARTED: %v", err)
		}
		startedRow := latestRunEvent(t, h, enqueued.SessionID, enqueued.RunID)
		var storedStarted map[string]any
		if err := json.Unmarshal([]byte(startedRow.PayloadJSON), &storedStarted); err != nil {
			t.Fatalf("unmarshal durable hostile TOOL_CALL_STARTED payload: %v", err)
		}
		wantStarted := map[string]any{"toolName": "shell.exec", "toolCallId": "call_hostile", "serverName": "files", "modelToolCallId": "model_call_1"}
		if !reflect.DeepEqual(storedStarted, wantStarted) {
			t.Fatalf("hostile TOOL_CALL_STARTED payload was not reduced to the safe identity shape: got %#v, want %#v", storedStarted, wantStarted)
		}

		hostileFailed, err := structpb.NewStruct(map[string]any{
			"toolName":   "shell.exec",
			"toolCallId": "call_hostile",
			"serverName": "files",
			"category":   "a_lie_the_worker_chose",
			"token":      "secret-approval-token",
			"args":       map[string]any{"path": "/etc/passwd"},
			"result":     "leaked result",
			"path":       "/etc/shadow",
			"message":    "a forged authoritative narration",
			"approvalId": "approval_forged",
		})
		if err != nil {
			t.Fatalf("build hostile failed payload: %v", err)
		}
		if err := h.service.applyUpdate(context.Background(), &turingv1.RuntimeUpdate{
			Update: &turingv1.RuntimeUpdate_Event{Event: &turingv1.TuringEvent{
				RunId: enqueued.RunID, Type: turingv1.TuringEventType_TURING_EVENT_TYPE_TOOL_CALL_FAILED, Payload: hostileFailed,
			}},
		}); err != nil {
			t.Fatalf("applyUpdate hostile TOOL_CALL_FAILED: %v", err)
		}
		failedRow := latestRunEvent(t, h, enqueued.SessionID, enqueued.RunID)
		var storedFailed map[string]any
		if err := json.Unmarshal([]byte(failedRow.PayloadJSON), &storedFailed); err != nil {
			t.Fatalf("unmarshal durable hostile TOOL_CALL_FAILED payload: %v", err)
		}
		wantFailed := map[string]any{"toolName": "shell.exec", "toolCallId": "call_hostile", "serverName": "files", "category": "tool_failure"}
		if !reflect.DeepEqual(storedFailed, wantFailed) {
			t.Fatalf("hostile TOOL_CALL_FAILED payload was not reduced to the safe identity shape: got %#v, want %#v", storedFailed, wantFailed)
		}
		for _, forged := range []string{"secret-approval-token", "cat /etc/shadow", "leaked result", "/etc/shadow", "forged authoritative narration", "approval_forged", "a_lie_the_worker_chose", "run_someone_elses"} {
			if strings.Contains(failedRow.PayloadJSON, forged) {
				t.Fatalf("durable hostile TOOL_CALL_FAILED payload leaked %q: %s", forged, failedRow.PayloadJSON)
			}
		}

		safeStarted := events.Decode(startedRow.Type, enqueued.RunID, startedRow.PayloadJSON)
		safeFailed := events.Decode(failedRow.Type, enqueued.RunID, failedRow.PayloadJSON)
		for _, safe := range []events.SafeEvent{safeStarted, safeFailed} {
			for _, leaked := range []string{"token", "args", "result", "path", "message", "approvalId", "runState"} {
				if _, ok := safe.Payload[leaked]; ok {
					t.Fatalf("public projection carried raw hostile field %q: %#v", leaked, safe.Payload)
				}
			}
			if safe.RunState != nil {
				t.Fatalf("public projection carried a forged canonical run state: %+v", safe.RunState)
			}
		}
		if safeFailed.Payload["category"] != "tool_failure" {
			t.Fatalf("public projection did not force the server-chosen category: %#v", safeFailed.Payload)
		}
	})
}

// TestRuntimeGenericStepAndMessageEventsStillSucceed pins the legitimate
// counterpart to the rejection above: a worker's own narration is not caught
// by the same ingress guard just because it shares a stream with tool and
// approval traffic.
func TestRuntimeGenericStepAndMessageEventsStillSucceed(t *testing.T) {
	legitimateTypes := []turingv1.TuringEventType{
		turingv1.TuringEventType_TURING_EVENT_TYPE_MESSAGE_STARTED,
		turingv1.TuringEventType_TURING_EVENT_TYPE_MESSAGE_DELTA,
		turingv1.TuringEventType_TURING_EVENT_TYPE_AGENT_RUN_STEP,
	}
	for _, eventType := range legitimateTypes {
		t.Run(eventType.String(), func(t *testing.T) {
			h := newHarness(t)
			enqueued := h.createRunningRunResult(t, "legitimate worker narration")
			payload, err := structpb.NewStruct(map[string]any{
				"note": "a worker's own words",
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
			if stored["note"] != "a worker's own words" {
				t.Fatalf("legitimate worker narration was altered: %s", row.PayloadJSON)
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
