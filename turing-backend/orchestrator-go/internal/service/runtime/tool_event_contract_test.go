package runtime

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/service/events"
	"google.golang.org/protobuf/proto"
)

func TestToolLifecyclePersistedAndStreamedEventContract(t *testing.T) {
	tests := []struct {
		name      string
		eventType string
		toolName  string
		errorText string
		withError bool
		terminal  turingv1.ToolCallStatus
	}{
		{name: "started", eventType: "tool.call.started", toolName: "system.time"},
		{name: "completed", eventType: "tool.call.completed", toolName: "system.time", terminal: turingv1.ToolCallStatus_TOOL_CALL_STATUS_COMPLETED},
		{name: "failed", eventType: "tool.call.failed", toolName: "system.time", errorText: "tool exploded", withError: true, terminal: turingv1.ToolCallStatus_TOOL_CALL_STATUS_FAILED},
		{name: "failed without error", eventType: "tool.call.failed", toolName: "system.time", errorText: "Tool call failed", terminal: turingv1.ToolCallStatus_TOOL_CALL_STATUS_FAILED},
		{name: "denied", eventType: "tool.call.denied", toolName: "system.shell", errorText: "unknown_tool"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			h := newHarness(t)
			enqueued := h.createRunningRunResult(t, test.name)
			stream, unsubscribe := h.bus.Subscribe(enqueued.SessionID)
			defer unsubscribe()
			toolCallID := "call_contract_" + test.name
			before := &turingv1.ToolCallBeacon{
				RunId:           enqueued.RunID,
				TraceId:         enqueued.TraceID,
				ToolCallId:      toolCallID,
				AgentId:         turingv1.AgentId_AGENT_ID_GENERAL_ASSISTANT,
				ServerName:      "system",
				ToolName:        test.toolName,
				Phase:           turingv1.ToolCallPhase_TOOL_CALL_PHASE_BEFORE,
				ModelToolCallId: "provider_" + test.name,
				Args:            mustStruct(t, map[string]any{"secret": "not public"}),
			}
			applyToolBeaconForContract(t, h, before)
			if test.terminal != turingv1.ToolCallStatus_TOOL_CALL_STATUS_UNSPECIFIED {
				after := proto.Clone(before).(*turingv1.ToolCallBeacon)
				after.Phase = turingv1.ToolCallPhase_TOOL_CALL_PHASE_AFTER
				after.Status = test.terminal
				after.ResultSummary = "private result"
				after.DurationMs = 42
				if test.withError {
					after.Error = &turingv1.ToolCallError{Code: "mcp_call_failed", Message: test.errorText}
				}
				applyToolBeaconForContract(t, h, after)
			}

			persisted := persistedToolEventForContract(t, h, enqueued, test.eventType)
			streamed := streamedToolEventForContract(t, stream, test.eventType)
			assertToolEventPayloadContract(t, persisted.PayloadJSON, toolCallID, test.toolName, test.errorText)
			assertToolEventPayloadContract(t, streamed.PayloadJSON, toolCallID, test.toolName, test.errorText)
		})
	}
}

func applyToolBeaconForContract(t *testing.T, h *harness, beacon *turingv1.ToolCallBeacon) {
	t.Helper()
	if err := h.service.applyUpdate(context.Background(), &turingv1.RuntimeUpdate{
		Update: &turingv1.RuntimeUpdate_ToolBeacon{ToolBeacon: beacon},
	}); err != nil {
		t.Fatalf("apply %s beacon: %v", beacon.Phase, err)
	}
}

func persistedToolEventForContract(t *testing.T, h *harness, enqueued repository.EnqueueUserMessageResult, eventType string) repository.Event {
	t.Helper()
	replayed, _, err := h.repo.ReplayEvents(context.Background(), enqueued.SessionID, 0, 20)
	if err != nil {
		t.Fatal(err)
	}
	for _, event := range replayed {
		if event.Type == eventType && event.RunID.Valid && event.RunID.String == enqueued.RunID {
			return event
		}
	}
	t.Fatalf("persisted event %q not found", eventType)
	return repository.Event{}
}

func streamedToolEventForContract(t *testing.T, stream <-chan events.Event, eventType string) events.Event {
	t.Helper()
	timer := time.NewTimer(time.Second)
	defer timer.Stop()
	for {
		select {
		case event := <-stream:
			if event.Type == eventType {
				return event
			}
		case <-timer.C:
			t.Fatalf("streamed event %q not found", eventType)
		}
	}
}

func assertToolEventPayloadContract(t *testing.T, payloadJSON, toolCallID, toolName, errorText string) {
	t.Helper()
	var payload map[string]any
	if err := json.Unmarshal([]byte(payloadJSON), &payload); err != nil {
		t.Fatalf("decode payload %q: %v", payloadJSON, err)
	}
	if got, ok := payload["toolCallId"].(string); !ok || got != toolCallID {
		t.Fatalf("toolCallId = %#v, want %q string", payload["toolCallId"], toolCallID)
	}
	if got, ok := payload["toolName"].(string); !ok || got != toolName {
		t.Fatalf("toolName = %#v, want %q string", payload["toolName"], toolName)
	}
	if serverName, exists := payload["serverName"]; exists {
		if got, ok := serverName.(string); !ok || got != "system" {
			t.Fatalf("serverName = %#v, want system string", serverName)
		}
	}
	for _, forbidden := range []string{
		"tool_call_id", "tool_name", "server_name", "args", "status", "resultSummary",
		"durationMs", "errorCode", "reason", "modelToolCallId",
	} {
		if _, exists := payload[forbidden]; exists {
			t.Fatalf("payload uses forbidden key %q: %#v", forbidden, payload)
		}
	}
	if errorText == "" {
		if _, exists := payload["error"]; exists {
			t.Fatalf("successful lifecycle payload has error: %#v", payload["error"])
		}
		return
	}
	if got, ok := payload["error"].(string); !ok || got != errorText {
		t.Fatalf("error = %#v, want %q string", payload["error"], errorText)
	}
	if len(payload) != 4 {
		t.Fatalf("terminal error payload keys = %#v, want only toolCallId/toolName/serverName/error", payload)
	}
}
