package runtime

import (
	"context"
	"testing"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
	"google.golang.org/protobuf/types/known/structpb"
)

// read_only is a statement about what a tool does, not about when it is
// allowed. A user who raises memory.search to approval_required has changed
// when Turing may look, not what looking costs — so the decision on the wire
// still says read-only, and the runtime may treat a failed search as something
// it can retry rather than a side effect it has to assume happened.
func TestRaisedMemoryReadPolicyStillDecidesReadOnlyOnTheWire(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	if err := h.repo.UpsertTools(ctx, []repository.DiscoveredTool{
		{ServerName: "memory", ToolName: "memory.search", SchemaJSON: `{}`, Policy: "safe"},
		{ServerName: "memory", ToolName: "memory.remember", SchemaJSON: `{}`, Policy: "approval_required"},
	}); err != nil {
		t.Fatal(err)
	}
	// The user raises it: looking now stops and asks.
	if err := h.repo.SetToolPolicyByName(ctx, "memory", "memory.search", "approval_required"); err != nil {
		t.Fatal(err)
	}

	enqueued := h.createRunningRunResult(t, "recall")
	args, err := structpb.NewStruct(map[string]any{"query": "coffee"})
	if err != nil {
		t.Fatal(err)
	}
	decision, err := h.service.handleToolBeacon(ctx, &turingv1.ToolCallBeacon{
		RunId: enqueued.RunID, TraceId: enqueued.TraceID, ToolCallId: "call_memory_search",
		AgentId: turingv1.AgentId_AGENT_ID_GENERAL_ASSISTANT, ToolName: "memory.search",
		Phase: turingv1.ToolCallPhase_TOOL_CALL_PHASE_BEFORE, Args: args,
	})
	if err != nil {
		t.Fatal(err)
	}
	if decision.GetDecision() != turingv1.ToolPolicyDecision_DECISION_APPROVAL_REQUIRED {
		t.Fatalf("decision = %+v, want the raised policy to stop and ask", decision)
	}
	if !decision.GetReadOnly() {
		t.Fatal("a raised memory.search decision dropped read_only; a failed search would be classified as a side effect")
	}

	// The writing tool is the other half of the same claim.
	rememberArgs, err := structpb.NewStruct(map[string]any{"title": "Coffee", "body": "black"})
	if err != nil {
		t.Fatal(err)
	}
	writeDecision, err := h.service.handleToolBeacon(ctx, &turingv1.ToolCallBeacon{
		RunId: enqueued.RunID, TraceId: enqueued.TraceID, ToolCallId: "call_memory_remember",
		AgentId: turingv1.AgentId_AGENT_ID_GENERAL_ASSISTANT, ToolName: "memory.remember",
		Phase: turingv1.ToolCallPhase_TOOL_CALL_PHASE_BEFORE, Args: rememberArgs,
	})
	if err != nil {
		t.Fatal(err)
	}
	if writeDecision.GetReadOnly() {
		t.Fatal("memory.remember was decided read-only; it writes a file into the user's vault")
	}
}

// A memory tool the user disabled must not survive a worker reconnecting: the
// capability filter is what takes it off the list the runtime is handed.
func TestDisabledMemoryToolIsFilteredOutOfWorkerCapabilities(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	discovered := []repository.DiscoveredTool{
		{ServerName: "memory", ToolName: "memory.search", SchemaJSON: `{}`, Policy: "safe"},
		{ServerName: "memory", ToolName: "memory.read", SchemaJSON: `{}`, Policy: "safe"},
	}
	if err := h.repo.UpsertTools(ctx, discovered); err != nil {
		t.Fatal(err)
	}
	if err := h.repo.SetToolPolicyByName(ctx, "memory", "memory.read", "disabled"); err != nil {
		t.Fatal(err)
	}

	capabilities := &registeredWorkerCapabilities{tools: map[string]struct{}{
		"memory/memory.search": {}, "memory/memory.read": {},
	}}
	filteredCapabilities, filtered, err := h.service.filterRegisteredWorkerTools(ctx, capabilities, discovered)
	if err != nil {
		t.Fatal(err)
	}
	if len(filtered) != 1 || filtered[0].ToolName != "memory.search" {
		t.Fatalf("filtered = %+v, want only the tool that is still allowed", filtered)
	}
	if _, present := filteredCapabilities.tools["memory/memory.read"]; present {
		t.Fatal("a disabled memory tool stayed in the worker's capability set")
	}
	if _, present := filteredCapabilities.tools["memory/memory.search"]; !present {
		t.Fatal("an allowed memory tool was filtered out")
	}
}
