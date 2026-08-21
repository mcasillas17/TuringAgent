package runtime

import (
	"context"
	"encoding/json"
	"testing"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/runoutcome"
	"google.golang.org/protobuf/proto"
)

// toolBeaconPoison is what a failing tool actually says: a path, a key, a
// provider's prose. The runtime's own tool package reports none of it, but the
// beacon is a wire message from a separate process, so the orchestrator is the
// boundary that has to hold — not the sender's good manners.
const toolBeaconPoison = "open /Users/someone/.ssh/id_rsa: token sk-live-SECRET rejected by provider"

// TestToolAfterFailureIngestionPersistsNoRawText covers every terminal shape a
// tool-after beacon can take. In each, the beacon carries a hostile message and
// an unallowlisted code, and neither may reach the tool call row or the durable
// event.
func TestToolAfterFailureIngestionPersistsNoRawText(t *testing.T) {
	for _, test := range []struct {
		name         string
		status       turingv1.ToolCallStatus
		eventType    string
		beaconError  *turingv1.ToolCallError
		wantCategory runoutcome.Reason
		wantCode     string
	}{
		{
			name:         "failed with an allowlisted code",
			status:       turingv1.ToolCallStatus_TOOL_CALL_STATUS_FAILED,
			eventType:    "tool.call.failed",
			beaconError:  &turingv1.ToolCallError{Code: "mcp_call_failed", Message: toolBeaconPoison},
			wantCategory: runoutcome.ReasonToolFailure,
			wantCode:     "mcp_call_failed",
		},
		{
			name:         "failed with an approval transport code",
			status:       turingv1.ToolCallStatus_TOOL_CALL_STATUS_FAILED,
			eventType:    "tool.call.failed",
			beaconError:  &turingv1.ToolCallError{Code: "approval_wait_failed", Message: toolBeaconPoison},
			wantCategory: runoutcome.ReasonToolFailure,
			wantCode:     "approval_wait_failed",
		},
		{
			name:         "failed with an off-matrix code",
			status:       turingv1.ToolCallStatus_TOOL_CALL_STATUS_FAILED,
			eventType:    "tool.call.failed",
			beaconError:  &turingv1.ToolCallError{Code: toolBeaconPoison, Message: toolBeaconPoison},
			wantCategory: runoutcome.ReasonToolFailure,
			wantCode:     runoutcome.CodeUnknown,
		},
		{
			name:         "failed with no error object at all",
			status:       turingv1.ToolCallStatus_TOOL_CALL_STATUS_FAILED,
			eventType:    "tool.call.failed",
			wantCategory: runoutcome.ReasonToolFailure,
			wantCode:     runoutcome.CodeUnknown,
		},
		{
			name:         "denied by the runtime",
			status:       turingv1.ToolCallStatus_TOOL_CALL_STATUS_DENIED,
			eventType:    "tool.call.denied",
			beaconError:  &turingv1.ToolCallError{Code: "unknown_tool", Message: toolBeaconPoison},
			wantCategory: runoutcome.ReasonPolicyDenied,
			wantCode:     "unknown_tool",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			h := newHarness(t)
			enqueued := h.createRunningRunResult(t, test.name)
			stream, unsubscribe := h.bus.Subscribe(enqueued.SessionID)
			defer unsubscribe()
			toolCallID := "call_redaction_" + test.eventType
			before := &turingv1.ToolCallBeacon{
				RunId:      enqueued.RunID,
				TraceId:    enqueued.TraceID,
				ToolCallId: toolCallID,
				AgentId:    turingv1.AgentId_AGENT_ID_GENERAL_ASSISTANT,
				ServerName: "system",
				ToolName:   "system.time",
				Phase:      turingv1.ToolCallPhase_TOOL_CALL_PHASE_BEFORE,
			}
			applyToolBeaconForContract(t, h, before)
			after := proto.Clone(before).(*turingv1.ToolCallBeacon)
			after.Phase = turingv1.ToolCallPhase_TOOL_CALL_PHASE_AFTER
			after.Status = test.status
			after.ResultSummary = toolBeaconPoison
			after.Error = test.beaconError
			applyToolBeaconForContract(t, h, after)

			persisted := persistedToolEventForContract(t, h, enqueued, test.eventType)
			streamed := streamedToolEventForContract(t, stream, test.eventType)
			for name, payloadJSON := range map[string]string{
				"persisted": persisted.PayloadJSON,
				"streamed":  streamed.PayloadJSON,
			} {
				assertToolTerminalPayload(t, name, payloadJSON, toolCallID, "system.time", test.wantCategory)
			}

			var errorCode, errorMessage, resultSummary string
			if err := h.database.QueryRowContext(context.Background(),
				`SELECT COALESCE(error_code, ''), COALESCE(error_message, ''), COALESCE(result_summary, '')
				 FROM tool_calls WHERE id = ?`, toolCallID,
			).Scan(&errorCode, &errorMessage, &resultSummary); err != nil {
				t.Fatalf("read tool call row: %v", err)
			}
			if errorMessage != "" {
				t.Fatalf("tool_calls.error_message persisted %q", errorMessage)
			}
			if errorCode != test.wantCode {
				t.Fatalf("tool_calls.error_code = %q, want %q", errorCode, test.wantCode)
			}
		})
	}
}

// assertToolTerminalPayload holds the durable shape a tool-call terminal event
// is allowed to have: the identity a client was already promised, plus one
// allowlisted category. Nothing else, under any key.
func assertToolTerminalPayload(t *testing.T, source string, payloadJSON string, toolCallID string, toolName string, category runoutcome.Reason) {
	t.Helper()
	var payload map[string]any
	if err := json.Unmarshal([]byte(payloadJSON), &payload); err != nil {
		t.Fatalf("decode %s payload %q: %v", source, payloadJSON, err)
	}
	want := map[string]any{
		"toolCallId": toolCallID,
		"serverName": "system",
		"toolName":   toolName,
		"category":   string(category),
	}
	for key, expected := range want {
		if payload[key] != expected {
			t.Fatalf("%s payload[%q] = %#v, want %#v", source, key, payload[key], expected)
		}
	}
	if len(payload) != len(want) {
		t.Fatalf("%s payload = %#v, want only %v", source, payload, sortedPayloadKeys(want))
	}
}

func sortedPayloadKeys(payload map[string]any) []string {
	keys := make([]string, 0, len(payload))
	for key := range payload {
		keys = append(keys, key)
	}
	return keys
}
