package agent

import (
	"context"
	"errors"
	"strings"
	"testing"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"github.com/mcasillas17/TuringAgent/turing-backend/agent-runtime-go/internal/llm"
	"github.com/mcasillas17/TuringAgent/turing-backend/agent-runtime-go/internal/tools"
	"google.golang.org/protobuf/encoding/protojson"
)

// toolFailurePoison is what a runtime-originated tool failure error actually
// carries: an operator's home path, a live-looking credential, and the model's
// own argument text. A generic runtime event is persisted verbatim by
// RuntimeService and returned to clients, so none of it may travel on one.
const toolFailurePoison = "/Users/someone/.ssh/id_rsa sk-live-SECRET-TOKEN"

// runtimeToolFailureBranches are the Runner failures that reach the agent
// BEFORE a beacon was posted — the only ones for which the agent, rather than
// the orchestrator, writes the tool.call.failed event.
//
// Reachability is part of what this table asserts. A failure the beacon boundary
// already owns (a send that failed, a policy denial, an MCP error) is reported
// by the orchestrator from the after-beacon and must produce no agent-written
// terminal event at all; the send case below pins that, so a future refactor
// cannot quietly reopen the agent's raw-text path through it.
func runtimeToolFailureBranches() []struct {
	name            string
	args            map[string]any
	runner          *tools.Runner
	wantFailedEvent bool
} {
	return []struct {
		name            string
		args            map[string]any
		runner          *tools.Runner
		wantFailedEvent bool
	}{
		{
			// safejson names the offending key, and the key is the model's. An
			// unencodable value under a poisoned key is the shortest honest path
			// from provider-controlled text to a durable public payload.
			name:            "argument_serialization",
			args:            map[string]any{toolFailurePoison: map[string]string{"nested": toolFailurePoison}},
			runner:          &tools.Runner{PostBeacon: allowToolCall},
			wantFailedEvent: true,
		},
		{
			name: "metadata_fetch",
			args: map[string]any{"ok": true},
			runner: &tools.Runner{
				PostBeacon: allowToolCall,
				MetadataFetchers: []func(context.Context) error{
					func(context.Context) error { return errors.New(toolFailurePoison) },
				},
			},
			wantFailedEvent: true,
		},
		{
			name: "beacon_send",
			args: map[string]any{"ok": true},
			runner: &tools.Runner{
				PostBeacon: func(context.Context, *turingv1.ToolCallBeacon) (*turingv1.ToolPolicyDecision, error) {
					return nil, errors.New(toolFailurePoison)
				},
			},
			wantFailedEvent: false,
		},
	}
}

func TestRuntimeToolFailureEventCarriesOnlyTheSafeCategory(t *testing.T) {
	for _, test := range runtimeToolFailureBranches() {
		t.Run(test.name, func(t *testing.T) {
			// One tool round, then an ordinary answer: the failure has to be
			// observed on the call the model actually made, not on a synthesized
			// retry ID from a later round.
			provider := &queuedProvider{responses: [][]llm.StreamEvent{
				{{Type: "tool_call", ToolCalls: []llm.ToolCall{{
					ID: "call_model", Name: "system.time", Arguments: test.args,
				}}}},
				{{Type: "delta", Text: "done"}},
			}}
			client := &assistantTestToolLister{
				definitions: []map[string]any{{"name": "system.time"}},
				result:      map[string]any{"ok": true},
			}
			assistant := NewGeneralAssistant(
				map[turingv1.ModelProvider]llm.Provider{turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA: provider},
				fakeMessageClient{},
				&GeneralAssistantTools{SystemMCP: client, Runner: test.runner},
			)

			updates := collectUpdatesIgnoringError(t, assistant, testJob())

			assertNoPoisonInUpdates(t, updates, toolFailurePoison)
			failedEvents := eventsOfType(updates, turingv1.TuringEventType_TURING_EVENT_TYPE_TOOL_CALL_FAILED)
			if !test.wantFailedEvent {
				if len(failedEvents) != 0 {
					t.Fatalf("beacon-owned failure wrote %d agent tool.call.failed events, want none", len(failedEvents))
				}
				return
			}
			if len(failedEvents) != 1 {
				t.Fatalf("tool.call.failed events = %d, want exactly one", len(failedEvents))
			}
			assertSafeToolFailurePayload(t, failedEvents[0], "system.time", "call_model")
		})
	}
}

// TestEveryAgentToolFailureEventIsSafe covers the two failures the agent reports
// without ever reaching the Runner. Their codes were already fixed strings, so
// nothing leaked — but they are tool.call.failed producers, and the payload
// vocabulary has to be one vocabulary, not "safe here and different there".
func TestEveryAgentToolFailureEventIsSafe(t *testing.T) {
	for _, test := range []struct {
		name      string
		toolName  string
		agentTool *GeneralAssistantTools
	}{
		{
			name:      "unknown_tool",
			toolName:  "system.nonexistent",
			agentTool: &GeneralAssistantTools{SystemMCP: &assistantTestToolLister{definitions: []map[string]any{{"name": "system.time"}}}, Runner: &tools.Runner{PostBeacon: allowToolCall}},
		},
		{
			name:      "tool_runner_unavailable",
			toolName:  "system.time",
			agentTool: &GeneralAssistantTools{SystemMCP: &assistantTestToolLister{definitions: []map[string]any{{"name": "system.time"}}}},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			provider := &queuedProvider{responses: [][]llm.StreamEvent{
				{{Type: "tool_call", ToolCalls: []llm.ToolCall{{ID: "call_model", Name: test.toolName}}}},
				{{Type: "delta", Text: "done"}},
			}}
			assistant := NewGeneralAssistant(
				map[turingv1.ModelProvider]llm.Provider{turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA: provider},
				fakeMessageClient{},
				test.agentTool,
			)

			updates := collectUpdatesIgnoringError(t, assistant, testJob())

			failedEvents := eventsOfType(updates, turingv1.TuringEventType_TURING_EVENT_TYPE_TOOL_CALL_FAILED)
			if len(failedEvents) != 1 {
				t.Fatalf("tool.call.failed events = %d, want exactly one", len(failedEvents))
			}
			assertSafeToolFailurePayload(t, failedEvents[0], test.toolName, "call_model")
		})
	}
}

// assertSafeToolFailurePayload holds the shape an agent-written tool.call.failed
// event is allowed to have: the identity the model and the client were already
// promised, plus one allowlisted category. Nothing else, under any key — and in
// particular no error, message, result, or reason.
func assertSafeToolFailurePayload(t *testing.T, event *turingv1.TuringEvent, toolName string, toolCallID string) {
	t.Helper()
	payload := event.GetPayload().AsMap()
	want := map[string]any{
		"toolName":   toolName,
		"toolCallId": toolCallID,
		"category":   toolFailureCategory,
	}
	for key, expected := range want {
		if payload[key] != expected {
			t.Fatalf("payload[%q] = %#v, want %#v", key, payload[key], expected)
		}
	}
	if len(payload) != len(want) {
		t.Fatalf("payload = %#v, want only toolName, toolCallId and category", payload)
	}
}

func eventsOfType(updates []*turingv1.RuntimeUpdate, eventType turingv1.TuringEventType) []*turingv1.TuringEvent {
	var matched []*turingv1.TuringEvent
	for _, update := range updates {
		if event := update.GetEvent(); event != nil && event.GetType() == eventType {
			matched = append(matched, event)
		}
	}
	return matched
}

// assertNoPoisonInUpdates checks the whole wire form of every update, so a leak
// through a key this test did not think to name still fails.
func assertNoPoisonInUpdates(t *testing.T, updates []*turingv1.RuntimeUpdate, poison string) {
	t.Helper()
	for _, update := range updates {
		encoded, err := protojson.Marshal(update)
		if err != nil {
			t.Fatalf("encode update: %v", err)
		}
		if strings.Contains(string(encoded), poison) {
			t.Fatalf("runtime update carried the poison: %s", encoded)
		}
	}
}

// collectUpdatesIgnoringError runs the agent and keeps whatever it emitted.
// Unlike collectUpdates it tolerates a returned error, because a reporting
// failure legitimately exits the executor — and what this file asserts is what
// crossed the wire on the way out, not whether the run survived.
func collectUpdatesIgnoringError(t *testing.T, assistant *GeneralAssistant, job *turingv1.AgentJob) []*turingv1.RuntimeUpdate {
	t.Helper()
	var updates []*turingv1.RuntimeUpdate
	_ = assistant.Execute(context.Background(), job, func(update *turingv1.RuntimeUpdate) error {
		updates = append(updates, update)
		return nil
	})
	return updates
}

// A stream that closes without a terminal event has said nothing about whether
// the turn finished — and that includes the tool calls it carried. Their
// arguments may be half a JSON object; the model may have been about to retract
// them. Executing one is irreversible in a way completing an unfinished
// sentence is not, so EOF must fail the turn before any tool runs, exactly as
// it already does when no calls arrived.
func TestProviderEOFWithPendingToolCallsNeitherExecutesNorCompletes(t *testing.T) {
	for _, test := range []struct {
		name   string
		events []llm.StreamEvent
	}{
		{
			name: "tool_call_only",
			events: []llm.StreamEvent{
				{Type: "tool_call", ToolCalls: []llm.ToolCall{{ID: "call_cut", Name: "system.time"}}},
			},
		},
		{
			name: "delta_then_tool_call",
			events: []llm.StreamEvent{
				{Type: "delta", Text: "let me check "},
				{Type: "tool_call", ToolCalls: []llm.ToolCall{{ID: "call_cut", Name: "system.time"}}},
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			provider := &scriptedProvider{events: test.events, endsWithoutTerminalEvent: true}
			client := &assistantTestToolLister{
				definitions: []map[string]any{{"name": "system.time"}},
				result:      map[string]any{"ok": true},
			}
			beacons := 0
			runner := &tools.Runner{PostBeacon: func(_ context.Context, beacon *turingv1.ToolCallBeacon) (*turingv1.ToolPolicyDecision, error) {
				beacons++
				return allowToolCall(context.Background(), beacon)
			}}
			assistant := NewGeneralAssistant(
				map[turingv1.ModelProvider]llm.Provider{turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA: provider},
				fakeMessageClient{},
				&GeneralAssistantTools{SystemMCP: client, Runner: runner},
			)

			updates := collectUpdates(t, assistant, testJob())

			if beacons != 0 {
				t.Fatalf("a cut-off stream posted %d tool beacons, want none", beacons)
			}
			if len(client.calls) != 0 {
				t.Fatalf("a cut-off stream executed %d tools: %+v", len(client.calls), client.calls)
			}
			for _, update := range updates {
				if update.GetRunCompleted() != nil {
					t.Fatalf("a stream that never finished was completed: %+v", update)
				}
				if event := update.GetEvent(); event != nil &&
					event.GetType() == turingv1.TuringEventType_TURING_EVENT_TYPE_TOOL_CALL_STARTED {
					t.Fatalf("a cut-off stream narrated a tool call: %+v", event)
				}
			}
			failed := updates[len(updates)-1].GetRunFailed()
			if failed == nil || failed.GetCode() != "model_stream_error" {
				t.Fatalf("terminal update = %+v, want a typed stream failure", updates[len(updates)-1])
			}
			if failed.GetFailureOrigin() != turingv1.FailureOrigin_FAILURE_ORIGIN_PROVIDER_TRANSPORT {
				t.Fatalf("origin = %v, want provider transport", failed.GetFailureOrigin())
			}
			if failed.GetMessage() != "" {
				t.Fatalf("provider text crossed the runtime boundary as %q", failed.GetMessage())
			}
		})
	}
}
