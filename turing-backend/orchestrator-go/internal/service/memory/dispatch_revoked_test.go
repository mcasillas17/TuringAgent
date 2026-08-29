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

type approvalEnforcerFunc func(ctx context.Context, approvalID, runID, serverName, serverID, toolName string, args map[string]any) error

func (f approvalEnforcerFunc) ConsumeApprovalForThirdParty(ctx context.Context, approvalID, runID, serverName, serverID, toolName string, args map[string]any) error {
	return f(ctx, approvalID, runID, serverName, serverID, toolName, args)
}

// A run the user has stopped must not touch the vault, however many gates it
// had already passed. The identity gate only asks whether the run exists;
// without a liveness re-read immediately before dispatch, a run cancelled
// after its BEFORE beacon could still file a proposal or serve belief text
// into an answer nobody is waiting for. An implementation that checked
// liveness anywhere earlier — or not at all — answers these calls instead of
// refusing them.
func TestMemoryDispatchRefusesARunCancelledBeforeDispatch(t *testing.T) {
	service, repo, vault, database, ctx := newMemoryServiceStack(t, filepath.Join(t.TempDir(), "turing.db"), newVaultRoot(t), nil)
	runID, sessionID := newRun(t, repo, ctx)
	setPolicies(t, repo, ctx, "safe")
	note := mustPromoteBelief(t, repo, ctx, sessionID, "Coffee", "They take their coffee black.")

	if _, err := database.ExecContext(ctx, `UPDATE agent_runs SET execution_active=0,status='cancelled' WHERE id=?`, runID); err != nil {
		t.Fatal(err)
	}

	for tool, args := range map[string]map[string]any{
		ToolSearch:   {"query": "coffee"},
		ToolRead:     {"belief_id": note.NoteID},
		ToolRemember: {"title": "Coffee", "body": "They drink it black."},
	} {
		t.Run(tool, func(t *testing.T) {
			response, err := service.CallMemoryTool(ctx, &turingv1.CallMemoryToolRequest{
				RunId: runID, ToolName: tool, Args: callArgs(t, args),
			})
			if status.Code(err) != codes.FailedPrecondition {
				t.Fatalf("%s on a cancelled run error = %v, want FailedPrecondition", tool, err)
			}
			if response.GetResult() != nil {
				t.Fatalf("%s on a cancelled run returned %v, want nothing at all", tool, response.GetResult().AsMap())
			}
		})
	}

	// Nothing was written on the way to any of those refusals.
	candidates, err := repo.ListMemoryCandidates(ctx, repository.MemoryCandidateQuery{Limit: 10})
	if err != nil {
		t.Fatalf("ListMemoryCandidates: %v", err)
	}
	if len(candidates) != 0 {
		t.Fatalf("candidates = %d, want a cancelled run to file nothing", len(candidates))
	}
	inbox, err := os.ReadDir(filepath.Join(vault.Root(), memoryfiles.InboxDirName))
	if err != nil {
		t.Fatalf("read inbox: %v", err)
	}
	if len(inbox) != 0 {
		t.Fatalf("inbox = %d entries, want a cancelled run to reach no file", len(inbox))
	}
}

// For an approval-gated tool, the window between the policy read and the
// dispatch is however long the user takes to answer, and everything read
// before the wait can go stale during it. Each mutation here happens inside
// the approval consumption itself — the latest instant the caller-side wait
// can end — and each must still be seen. A guard that ran before the approval
// was consumed, or that trusted the policy read from before the wait, passes
// none of these.
func TestMemoryDispatchRevalidatesChangesMadeDuringApprovalWait(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(ctx context.Context, repo *repository.Repository, database *db.DB, runID string) error
	}{
		{name: "cancelled run", mutate: func(ctx context.Context, _ *repository.Repository, database *db.DB, runID string) error {
			_, err := database.ExecContext(ctx, `UPDATE agent_runs SET execution_active=0,status='cancelled' WHERE id=?`, runID)
			return err
		}},
		{name: "disabled tool", mutate: func(ctx context.Context, repo *repository.Repository, _ *db.DB, _ string) error {
			return repo.SetToolPolicyByName(ctx, ServerName, ToolRemember, "disabled")
		}},
		{name: "deleting session", mutate: func(ctx context.Context, _ *repository.Repository, database *db.DB, runID string) error {
			_, err := database.ExecContext(ctx, `UPDATE sessions SET deletion_state='deleting' WHERE id=(SELECT session_id FROM agent_runs WHERE id=?)`, runID)
			return err
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			service, repo, vault, database, ctx := newMemoryServiceStack(t, filepath.Join(t.TempDir(), "turing.db"), newVaultRoot(t), nil)
			runID, _ := newRun(t, repo, ctx)
			setPolicies(t, repo, ctx, "approval_required")
			var mutateErr error
			service.SetApprovalEnforcer(approvalEnforcerFunc(func(ctx context.Context, _, gotRunID, _, _, _ string, _ map[string]any) error {
				mutateErr = test.mutate(ctx, repo, database, gotRunID)
				return nil
			}))

			response, err := service.CallMemoryTool(ctx, &turingv1.CallMemoryToolRequest{
				RunId: runID, ToolName: ToolRemember,
				Args: callArgs(t, map[string]any{"title": "Coffee", "body": "They drink it black."}),
			})
			if mutateErr != nil {
				t.Fatalf("mutate: %v", mutateErr)
			}
			if status.Code(err) != codes.FailedPrecondition {
				t.Fatalf("dispatch after %s error = %v, want FailedPrecondition", test.name, err)
			}
			if response.GetResult() != nil {
				t.Fatalf("dispatch after %s returned %v, want nothing at all", test.name, response.GetResult().AsMap())
			}
			inbox, err := os.ReadDir(filepath.Join(vault.Root(), memoryfiles.InboxDirName))
			if err != nil {
				t.Fatalf("read inbox: %v", err)
			}
			if len(inbox) != 0 {
				t.Fatalf("inbox = %d entries after %s, want the vault untouched", len(inbox), test.name)
			}
		})
	}
}
