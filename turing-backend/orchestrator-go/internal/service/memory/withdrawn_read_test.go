package memory

import (
	"strings"
	"testing"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Deleting a conversation withdraws the memories it grounded. Search stops
// answering with them immediately — but a model that already has the belief id
// in its context could hand it straight to memory.read, and the withdrawal
// would have removed the claim from discovery while leaving it fully readable.
//
// So the refusal is on the read as well as on the search, and the frame carries
// no content at all.
func TestMemoryReadRefusesABeliefWhoseEvidenceWasWithdrawn(t *testing.T) {
	service, repo, _, ctx := newMemoryService(t)
	grounding := newMemorySession(t, repo, ctx)
	note := mustPromoteBelief(t, repo, ctx, grounding, "Spare key", "The spare key is under the flowerpot.")
	// A second conversation, so the run this tool call belongs to survives the
	// deletion that withdraws the belief.
	runID, _ := newRun(t, repo, ctx)
	setPolicies(t, repo, ctx, "safe")

	read, err := service.CallMemoryTool(ctx, &turingv1.CallMemoryToolRequest{
		RunId: runID, ToolName: ToolRead, Args: callArgs(t, map[string]any{"belief_id": note.NoteID}),
	})
	if err != nil {
		t.Fatalf("memory.read before the withdrawal: %v", err)
	}
	if !strings.Contains(frameBody(t, read.GetResult()), "flowerpot") {
		t.Fatal("the belief was not readable before its evidence was withdrawn")
	}

	if err := repo.DeleteSession(ctx, grounding); err != nil {
		t.Fatalf("DeleteSession: %v", err)
	}
	if _, err := service.ListMemoryState(ctx, &turingv1.ListMemoryStateRequest{}); err != nil {
		t.Fatalf("ListMemoryState: %v", err)
	}

	withheld, err := service.CallMemoryTool(ctx, &turingv1.CallMemoryToolRequest{
		RunId: runID, ToolName: ToolRead, Args: callArgs(t, map[string]any{"belief_id": note.NoteID}),
	})
	if err == nil {
		t.Fatalf("memory.read served a withdrawn belief: %q", frameBody(t, withheld.GetResult()))
	}
	if code := status.Code(err); code != codes.FailedPrecondition && code != codes.NotFound {
		t.Fatalf("memory.read of a withdrawn belief = %v (code %v), want a refusal", err, code)
	}
	if withheld.GetResult() != nil {
		t.Fatalf("a refused read still carried a frame: %v", withheld.GetResult().AsMap())
	}
	if strings.Contains(status.Convert(err).Message(), "flowerpot") {
		t.Fatalf("the refusal leaked the claim: %q", status.Convert(err).Message())
	}
}
