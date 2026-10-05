package memory

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/db"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/memoryfiles"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func enforceRunSelection(t *testing.T, ctx context.Context, database *db.DB, runID, selected string) {
	t.Helper()
	if _, err := database.ExecContext(ctx, `
		UPDATE jobs SET payload_json = json_set(payload_json,
			'$.enforceSelectedTools', json('true'), '$.selectedTools', json(?))
		WHERE run_id = ?`, selected, runID); err != nil {
		t.Fatal(err)
	}
}

// A job that enforces its frozen set reaches only the memory tools in it,
// whatever their policy says, and an out-of-set call writes nothing.
func TestMemoryToolsOutsideAnEnforcedSetAreRefused(t *testing.T) {
	service, repo, _, database, ctx := newMemoryServiceStack(t, filepath.Join(t.TempDir(), "turing.db"), newVaultRoot(t), nil)
	runID, _ := newRun(t, repo, ctx)
	setPolicies(t, repo, ctx, "safe")
	enforceRunSelection(t, ctx, database, runID, `["memory/memory.search"]`)

	for tool, args := range everyToolCall() {
		t.Run(tool, func(t *testing.T) {
			_, err := service.CallMemoryTool(ctx, &turingv1.CallMemoryToolRequest{RunId: runID, ToolName: tool, Args: callArgs(t, args)})
			if tool == ToolSearch {
				if err != nil {
					t.Fatalf("in-set %s: %v", tool, err)
				}
				return
			}
			if status.Code(err) != codes.PermissionDenied || status.Convert(err).Message() != "memory tool is not selected for this run" {
				t.Fatalf("out-of-set %s = %v, want PermissionDenied not selected", tool, err)
			}
		})
	}
	candidates, err := repo.ListMemoryCandidates(ctx, repository.MemoryCandidateQuery{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(candidates) != 0 {
		t.Fatalf("candidates = %d, want an out-of-set remember to file nothing", len(candidates))
	}
}

// The selection is checked before an approval is spent: an approval-gated
// memory tool outside the enforced set is refused without consuming the
// approval the user granted, and without touching the vault.
func TestAnApprovalGatedMemoryToolOutsideAnEnforcedSetConsumesNoApproval(t *testing.T) {
	service, repo, vault, database, ctx := newMemoryServiceStack(t, filepath.Join(t.TempDir(), "turing.db"), newVaultRoot(t), nil)
	runID, _ := newRun(t, repo, ctx)
	setPolicies(t, repo, ctx, "approval_required")
	consumed := map[string]int{}
	service.SetApprovalEnforcer(approvalEnforcerFunc(func(_ context.Context, _, _, _, _, toolName string, _ map[string]any) error {
		consumed[toolName]++
		return nil
	}))
	enforceRunSelection(t, ctx, database, runID, `["memory/memory.search"]`)

	_, err := service.CallMemoryTool(ctx, &turingv1.CallMemoryToolRequest{
		RunId: runID, ApprovalId: "approval_1", ToolName: ToolRemember,
		Args: callArgs(t, map[string]any{"title": "Coffee", "body": "They drink it black."}),
	})
	if status.Code(err) != codes.PermissionDenied || status.Convert(err).Message() != "memory tool is not selected for this run" {
		t.Fatalf("out-of-set remember = %v, want PermissionDenied not selected", err)
	}
	if len(consumed) != 0 {
		t.Fatalf("approvals consumed = %v, want none for an out-of-set call", consumed)
	}
	inbox, err := os.ReadDir(filepath.Join(vault.Root(), memoryfiles.InboxDirName))
	if err != nil {
		t.Fatalf("read inbox: %v", err)
	}
	if len(inbox) != 0 {
		t.Fatalf("inbox = %d entries, want the vault untouched", len(inbox))
	}

	if _, err := service.CallMemoryTool(ctx, &turingv1.CallMemoryToolRequest{
		RunId: runID, ApprovalId: "approval_2", ToolName: ToolSearch, Args: callArgs(t, everyToolCall()[ToolSearch]),
	}); err != nil {
		t.Fatalf("in-set search: %v", err)
	}
	if consumed[ToolSearch] != 1 || len(consumed) != 1 {
		t.Fatalf("approvals consumed = %v, want exactly the in-set search's", consumed)
	}
}
