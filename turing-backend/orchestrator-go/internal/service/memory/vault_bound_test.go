package memory

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/memoryfiles"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// A vault past the scan bound is a bounded feature saying no, and the model
// asking the question is the one component that can act on the answer — by
// stopping, and by telling the user what to do. Internal says none of that: it
// is what this server returns for a failure it does not recognise, and a model
// that reads it retries the same search forever.
func TestMemorySearchAnswersAnOverBoundVaultWithItsBound(t *testing.T) {
	service, repo, vault, callCtx := newMemoryService(t)
	setPolicies(t, repo, callCtx, "safe")
	runID, _ := newRun(t, repo, callCtx)
	beliefs := filepath.Join(vault.Root(), memoryfiles.BeliefsDirName)
	for index := 0; index <= memoryfiles.MaxVaultIndexedFiles; index++ {
		body := managedBeliefFile(newNoteID(t), "note")
		name := filepath.Join(beliefs, fmt.Sprintf("note-%05d.md", index))
		if err := os.WriteFile(name, []byte(body), 0o600); err != nil {
			t.Fatalf("fill the vault: %v", err)
		}
	}

	_, err := service.CallMemoryTool(callCtx, &turingv1.CallMemoryToolRequest{
		RunId:    runID,
		ToolName: ToolSearch,
		Args:     callArgs(t, map[string]any{"query": "note"}),
	})
	if err == nil {
		t.Fatal("a search over an over-bound vault answered as though it had searched it")
	}
	answer := status.Convert(err)
	if answer.Code() != codes.ResourceExhausted {
		t.Fatalf("code = %v, want ResourceExhausted; message = %q", answer.Code(), answer.Message())
	}
	want := fmt.Sprintf(
		"this vault holds more than %d notes, which is past the bound memory indexing and search run within; prune or split the vault",
		memoryfiles.MaxVaultIndexedFiles,
	)
	if answer.Message() != want {
		t.Fatalf("message = %q, want %q", answer.Message(), want)
	}
	if strings.Contains(answer.Message(), vault.Root()) {
		t.Fatalf("the refusal carries a filesystem path: %q", answer.Message())
	}
}
