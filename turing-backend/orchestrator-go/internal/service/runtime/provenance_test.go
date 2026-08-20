package runtime

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
	"google.golang.org/protobuf/types/known/structpb"
)

func provenancePayloadOf(t *testing.T, token string) map[string]any {
	t.Helper()
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		t.Fatalf("provenance token = %q, want a JWT", token)
	}
	payloadJSON, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal(payloadJSON, &payload); err != nil {
		t.Fatal(err)
	}
	return payload
}

func fileBeacon(t *testing.T, runID string, traceID string, toolCallID string, toolName string, args map[string]any) *turingv1.ToolCallBeacon {
	t.Helper()
	structArgs, err := structpb.NewStruct(args)
	if err != nil {
		t.Fatal(err)
	}
	return &turingv1.ToolCallBeacon{
		RunId:      runID,
		TraceId:    traceID,
		ToolCallId: toolCallID,
		AgentId:    turingv1.AgentId_AGENT_ID_GENERAL_ASSISTANT,
		ServerName: "files",
		ToolName:   toolName,
		Phase:      turingv1.ToolCallPhase_TOOL_CALL_PHASE_BEFORE,
		Args:       structArgs,
	}
}

func TestSafeFileToolBeforeDecisionCarriesProvenanceCapability(t *testing.T) {
	h := newHarness(t)
	enqueued := h.createRunningRunResult(t, "write a note")

	decision, err := h.service.handleToolBeacon(context.Background(), fileBeacon(t, enqueued.RunID, enqueued.TraceID, "call_read", "files.read", map[string]any{"path": "notes/todo.txt"}))
	if err != nil {
		t.Fatalf("handleToolBeacon: %v", err)
	}

	if decision.GetDecision() != turingv1.ToolPolicyDecision_DECISION_ALLOW {
		t.Fatalf("decision = %v, want allow", decision.GetDecision())
	}
	if decision.GetProvenanceToken() == "" {
		t.Fatal("safe file tool decision carried no provenance capability")
	}
	payload := provenancePayloadOf(t, decision.GetProvenanceToken())
	if payload["sid"] != enqueued.SessionID || payload["rid"] != enqueued.RunID {
		t.Fatalf("payload = %+v, want session %q run %q", payload, enqueued.SessionID, enqueued.RunID)
	}
	if payload["tool"] != "files.read" || payload["path"] != "notes/todo.txt" {
		t.Fatalf("payload = %+v, want the tool and path scope bound", payload)
	}
}

func TestApprovalRequiredFileToolBeforeDecisionCarriesProvenanceCapability(t *testing.T) {
	h := newHarness(t)
	enqueued := h.createRunningRunResult(t, "write a note")

	decision, err := h.service.handleToolBeacon(context.Background(), fileBeacon(t, enqueued.RunID, enqueued.TraceID, "call_update", "files.update", map[string]any{"path": "notes/todo.txt", "content": "hello"}))
	if err != nil {
		t.Fatalf("handleToolBeacon: %v", err)
	}

	if decision.GetDecision() != turingv1.ToolPolicyDecision_DECISION_APPROVAL_REQUIRED {
		t.Fatalf("decision = %v, want approval required", decision.GetDecision())
	}
	if decision.GetProvenanceToken() == "" {
		t.Fatal("approval-required decision carried no provenance capability")
	}
	payload := provenancePayloadOf(t, decision.GetProvenanceToken())
	if payload["tool"] != "files.update" || payload["path"] != "notes/todo.txt" {
		t.Fatalf("payload = %+v, want the tool and path scope bound", payload)
	}
	// The capability has to match what the approval is about, or the consume
	// that spends the approval will refuse it.
	approval, err := h.repo.GetApprovalByToolCall(context.Background(), enqueued.RunID, "call_update")
	if err != nil {
		t.Fatal(err)
	}
	if payload["args_hash"] != approval.ArgsHash {
		t.Fatalf("args_hash = %#v, want the approval's %q", payload["args_hash"], approval.ArgsHash)
	}
}

func TestNonFileToolBeforeDecisionCarriesNoProvenanceCapability(t *testing.T) {
	h := newHarness(t)
	enqueued := h.createRunningRunResult(t, "write a note")
	beacon := fileBeacon(t, enqueued.RunID, enqueued.TraceID, "call_time", "system.time", map[string]any{})
	beacon.ServerName = "system"

	decision, err := h.service.handleToolBeacon(context.Background(), beacon)
	if err != nil {
		t.Fatalf("handleToolBeacon: %v", err)
	}

	if decision.GetDecision() != turingv1.ToolPolicyDecision_DECISION_ALLOW {
		t.Fatalf("decision = %v, want allow", decision.GetDecision())
	}
	if decision.GetProvenanceToken() != "" {
		t.Fatalf("provenance token = %q, want none for a server that does not write sandbox artifacts", decision.GetProvenanceToken())
	}
}

func TestDeniedFileToolBeforeDecisionCarriesNoProvenanceCapability(t *testing.T) {
	h := newHarness(t)
	enqueued := h.createRunningRunResult(t, "write a note")

	decision, err := h.service.handleToolBeacon(context.Background(), fileBeacon(t, enqueued.RunID, enqueued.TraceID, "call_delete", "files.delete", map[string]any{"path": "notes/todo.txt"}))
	if err != nil {
		t.Fatalf("handleToolBeacon: %v", err)
	}

	if decision.GetDecision() != turingv1.ToolPolicyDecision_DECISION_DENY {
		t.Fatalf("decision = %v, want deny", decision.GetDecision())
	}
	if decision.GetProvenanceToken() != "" {
		t.Fatalf("provenance token = %q, want none for a denied call", decision.GetProvenanceToken())
	}
}

func TestReplayedFileToolBeforeDecisionStillCarriesProvenanceCapability(t *testing.T) {
	h := newHarness(t)
	enqueued := h.createRunningRunResult(t, "write a note")
	beacon := fileBeacon(t, enqueued.RunID, enqueued.TraceID, "call_read", "files.read", map[string]any{"path": "notes/todo.txt"})
	if _, err := h.service.handleToolBeacon(context.Background(), beacon); err != nil {
		t.Fatal(err)
	}

	replayed, err := h.service.handleToolBeacon(context.Background(), beacon)
	if err != nil {
		t.Fatalf("replayed handleToolBeacon: %v", err)
	}

	if replayed.GetDecision() != turingv1.ToolPolicyDecision_DECISION_ALLOW {
		t.Fatalf("replayed decision = %v, want allow", replayed.GetDecision())
	}
	if replayed.GetProvenanceToken() == "" {
		t.Fatal("replayed decision carried no provenance capability, so a retried call could never write")
	}
}

func TestReplayedApprovalRequiredDecisionStillCarriesProvenanceCapability(t *testing.T) {
	h := newHarness(t)
	enqueued := h.createRunningRunResult(t, "write a note")
	beacon := fileBeacon(t, enqueued.RunID, enqueued.TraceID, "call_update", "files.update", map[string]any{"path": "notes/todo.txt", "content": "hello"})
	first, err := h.service.handleToolBeacon(context.Background(), beacon)
	if err != nil {
		t.Fatal(err)
	}

	replayed, err := h.service.handleToolBeacon(context.Background(), beacon)
	if err != nil {
		t.Fatalf("replayed handleToolBeacon: %v", err)
	}

	if replayed.GetApprovalId() != first.GetApprovalId() {
		t.Fatalf("replayed approval id = %q, want %q", replayed.GetApprovalId(), first.GetApprovalId())
	}
	if replayed.GetProvenanceToken() == "" {
		t.Fatal("replayed approval-required decision carried no provenance capability")
	}
}

func TestFileToolProvenanceCapabilityAuthorisesTheConsumeThatFollows(t *testing.T) {
	h := newHarness(t)
	enqueued := h.createRunningRunResult(t, "write a note")
	decision, err := h.service.handleToolBeacon(context.Background(), fileBeacon(t, enqueued.RunID, enqueued.TraceID, "call_update", "files.update", map[string]any{"path": "notes/todo.txt", "content": "hello"}))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.approvals.ApproveApproval(context.Background(), &turingv1.ApproveApprovalRequest{ApprovalId: decision.GetApprovalId()}); err != nil {
		t.Fatal(err)
	}

	consumed, err := h.approvals.ConsumeApproval(context.Background(), &turingv1.ConsumeApprovalRequest{
		ApprovalId:      decision.GetApprovalId(),
		ProvenanceToken: decision.GetProvenanceToken(),
		PhysicalPath:    repository.OwnedSandboxPath(enqueued.SessionID, enqueued.RunID, "notes/todo.txt"),
	})
	if err != nil {
		t.Fatalf("ConsumeApproval with the issued capability: %v", err)
	}
	if consumed.GetReservation().GetArtifactId() == "" {
		t.Fatal("consume reserved no artifact")
	}
}
