package memory

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/db"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/memoryfiles"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// freezeDecision writes the run's egress decision row directly, the way the
// integrations tests do: what matters to the gate under test is only which
// qualified tool names the user's consent froze.
func freezeDecision(t *testing.T, database *db.DB, runID string, selectedTools []string) {
	t.Helper()
	selected, err := json.Marshal(selectedTools)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.ExecContext(context.Background(), `
		INSERT INTO run_egress_decisions (
			decision_id, decision_version, run_id, challenge_nonce,
			challenge_fingerprint, request_digest, provider, model_name,
			external_credential_ref_hash, endpoint, endpoint_host,
			data_categories_json, selected_tools_json,
			skill_snapshot_fingerprint, recall_applicable,
			memory_profile_applicable, consent_granted_at,
			remote_mcp_servers_json, integration_endpoints_json
		) VALUES (?, 3, ?, ?, 'fingerprint', 'digest', 'openai_compatible',
			'gpt-test', '', 'https://api.example.test/v1', 'api.example.test',
			'["EGRESS_DATA_CATEGORY_MESSAGES"]', ?, '', 0, 0,
			datetime('now'), '[]', '[]')
	`, "decision-"+runID, runID, "nonce-"+runID, string(selected)); err != nil {
		t.Fatal(err)
	}
}

// A run that carries a frozen egress decision was consented to with a named
// set of tools; a memory tool outside that set was never part of what the
// user agreed could shape the run. The runtime already restricts a
// decision-bearing run's tool definitions to the frozen set, but that is the
// runtime enforcing its own honesty claim — the same arrangement the
// integrations pseudo-server was written to distrust, which is why it
// re-validates its decision on the orchestrator side. This pins the memory
// mirror of that check: dispatch itself refuses, whatever the runtime asked.
func TestMemoryDispatchRefusesAToolTheFrozenDecisionNeverSelected(t *testing.T) {
	service, repo, vault, database, ctx := newMemoryServiceStack(t, filepath.Join(t.TempDir(), "turing.db"), newVaultRoot(t), nil)
	runID, sessionID := newRun(t, repo, ctx)
	setPolicies(t, repo, ctx, "safe")
	note := mustPromoteBelief(t, repo, ctx, sessionID, "Coffee", "They take their coffee black.")
	freezeDecision(t, database, runID, []string{"mcp_files/read_file"})

	for tool, args := range map[string]map[string]any{
		ToolSearch:   {"query": "coffee"},
		ToolRead:     {"belief_id": note.NoteID},
		ToolRemember: {"title": "Coffee", "body": "They drink it black."},
	} {
		t.Run(tool, func(t *testing.T) {
			response, err := service.CallMemoryTool(ctx, &turingv1.CallMemoryToolRequest{
				RunId: runID, ToolName: tool, Args: callArgs(t, args),
			})
			if status.Code(err) != codes.PermissionDenied {
				t.Fatalf("%s outside the frozen decision = %v, want PermissionDenied", tool, err)
			}
			// The message pins WHICH site refused: the pre-policy check, not
			// the post-liveness re-check — deleting the first site would
			// otherwise hide behind the second answering the same code.
			if got := status.Convert(err).Message(); !strings.Contains(got, "not covered by the run egress decision") {
				t.Fatalf("%s refusal = %q, want the pre-policy site's message", tool, got)
			}
			if response.GetResult() != nil {
				t.Fatalf("%s outside the frozen decision returned %v, want nothing at all", tool, response.GetResult().AsMap())
			}
		})
	}

	// Nothing was written on the way to any of those refusals.
	candidates, err := repo.ListMemoryCandidates(ctx, repository.MemoryCandidateQuery{Limit: 10})
	if err != nil {
		t.Fatalf("ListMemoryCandidates: %v", err)
	}
	if len(candidates) != 0 {
		t.Fatalf("candidates = %d, want a refused run to file nothing", len(candidates))
	}
	inbox, err := os.ReadDir(filepath.Join(vault.Root(), memoryfiles.InboxDirName))
	if err != nil {
		t.Fatalf("read inbox: %v", err)
	}
	if len(inbox) != 0 {
		t.Fatalf("inbox = %d entries, want a refused run to reach no file", len(inbox))
	}
}

// The two answered shapes around that refusal. A decision that names the tool
// is honored — this is the consent working, not a bypass. And a run with no
// decision row at all is the ordinary local run: unlike integrations, whose
// tools exist only behind egress consent, memory serves runs that never
// egress anything, so "no frozen consent" means "nothing to violate", never
// "refuse everything".
func TestMemoryDispatchHonorsTheFrozenDecisionInBothDirections(t *testing.T) {
	service, repo, _, database, ctx := newMemoryServiceStack(t, filepath.Join(t.TempDir(), "turing.db"), newVaultRoot(t), nil)
	setPolicies(t, repo, ctx, "safe")

	selectedRun, sessionID := newRun(t, repo, ctx)
	mustPromoteBelief(t, repo, ctx, sessionID, "Coffee", "They take their coffee black.")
	freezeDecision(t, database, selectedRun, []string{ServerName + "/" + ToolSearch})
	if _, err := service.CallMemoryTool(ctx, &turingv1.CallMemoryToolRequest{
		RunId: selectedRun, ToolName: ToolSearch,
		Args: callArgs(t, map[string]any{"query": "coffee"}),
	}); err != nil {
		t.Fatalf("search inside the frozen decision: %v", err)
	}

	bareRun, _ := newRun(t, repo, ctx)
	if _, err := service.CallMemoryTool(ctx, &turingv1.CallMemoryToolRequest{
		RunId: bareRun, ToolName: ToolSearch,
		Args: callArgs(t, map[string]any{"query": "coffee"}),
	}); err != nil {
		t.Fatalf("search on a run with no decision: %v", err)
	}
}

// The decision is among the things an approval wait can outlast: the
// pre-policy check reads it before the user is asked, and a decision replaced
// under the wait — a re-prepared run whose fresh consent dropped the memory
// tools — must be judged as it stands at dispatch. This drives the mutation
// through the approval seam, so only the post-liveness re-check can see it;
// an implementation that validates the decision once, before the policy,
// answers this call.
func TestMemoryDispatchRevalidatesTheDecisionAfterTheApprovalWait(t *testing.T) {
	service, repo, _, database, ctx := newMemoryServiceStack(t, filepath.Join(t.TempDir(), "turing.db"), newVaultRoot(t), nil)
	runID, _ := newRun(t, repo, ctx)
	setPolicies(t, repo, ctx, "approval_required")
	freezeDecision(t, database, runID, []string{ServerName + "/" + ToolRemember})

	var mutateErr error
	service.SetApprovalEnforcer(approvalEnforcerFunc(func(ctx context.Context, _, gotRunID, _, _, _ string, _ map[string]any) error {
		selected, err := json.Marshal([]string{"mcp_files/read_file"})
		if err != nil {
			mutateErr = err
			return nil
		}
		_, mutateErr = database.ExecContext(ctx, `UPDATE run_egress_decisions SET selected_tools_json=? WHERE run_id=?`, string(selected), gotRunID)
		return nil
	}))

	response, err := service.CallMemoryTool(ctx, &turingv1.CallMemoryToolRequest{
		RunId: runID, ToolName: ToolRemember,
		Args: callArgs(t, map[string]any{"title": "Coffee", "body": "They drink it black."}),
	})
	if mutateErr != nil {
		t.Fatalf("mutate: %v", mutateErr)
	}
	if status.Code(err) != codes.PermissionDenied {
		t.Fatalf("dispatch after the consent moved = %v, want PermissionDenied", err)
	}
	if got := status.Convert(err).Message(); !strings.Contains(got, "egress consent changed before dispatch") {
		t.Fatalf("refusal = %q, want the post-liveness site's message", got)
	}
	if response.GetResult() != nil {
		t.Fatalf("dispatch after the consent moved returned %v, want nothing at all", response.GetResult().AsMap())
	}
}
