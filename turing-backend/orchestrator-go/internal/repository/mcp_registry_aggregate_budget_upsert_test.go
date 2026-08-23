package repository

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// discoveredToolOfExactRawSize is toolOfExactRawSize's DiscoveredTool
// counterpart (see mcp_registry_aggregate_budget_test.go): a single
// DiscoveredTool whose raw (len(ToolName) + len(SchemaJSON)) byte size is
// exactly n, using the same fixed `{"type":"object","d":"<padding>"}`
// shape so tests can hit an exact aggregate byte target without fiddly
// arithmetic per call site.
func discoveredToolOfExactRawSize(t *testing.T, serverName, name string, n int) DiscoveredTool {
	t.Helper()
	const prefix = `{"type":"object","d":"`
	const suffix = `"}`
	pad := n - len(name) - len(prefix) - len(suffix)
	if pad < 0 {
		t.Fatalf("discoveredToolOfExactRawSize(%q, %q, %d): target size too small for name+fixed shape overhead", serverName, name, n)
	}
	return DiscoveredTool{
		ServerName: serverName,
		ToolName:   name,
		SchemaJSON: prefix + strings.Repeat("x", pad) + suffix,
		Policy:     "safe",
	}
}

func rawDiscoveredToolBytes(tool DiscoveredTool) int {
	return len(tool.ToolName) + len(tool.SchemaJSON)
}

// A UpsertTools snapshot at exactly the aggregate cap (as the only tenant
// of the budget) must succeed: the boundary must not be off-by-one
// against a legitimate, in-bounds aggregate total. UpsertTools is the
// bundled/skills/legacy tools path (called by the runtime for worker
// capabilities such as "skills"), a completely separate write path from
// replaceServerToolsTx (used by ImportMCPServer/RegisterMCPServer/
// ReplaceMCPServerTools) — before this fix it enforced no aggregate
// budget of its own at all.
func TestUpsertToolsAggregateBudgetAtExactCapAloneSucceeds(t *testing.T) {
	repo := New(openTestDB(t))
	ctx := context.Background()

	tool := discoveredToolOfExactRawSize(t, "skills", "skills.a", MaxMCPRegistryToolBytes)
	if err := repo.UpsertTools(ctx, []DiscoveredTool{tool}); err != nil {
		t.Fatalf("a snapshot at exactly MaxMCPRegistryToolBytes must not be refused: %v", err)
	}
	policy, enabled, found, err := repo.GetToolPolicy(ctx, "skills", "skills.a")
	if err != nil || !found || !enabled || policy != "safe" {
		t.Fatalf("policy=%q enabled=%v found=%v err=%v, want the tool present and enabled", policy, enabled, found, err)
	}
}

// One byte over the cap, as the only tenant of the budget, must be
// refused with no row created — the precise boundary case, isolated from
// any other server's own contribution.
func TestUpsertToolsAggregateBudgetOneByteOverCapAloneIsRefused(t *testing.T) {
	repo := New(openTestDB(t))
	ctx := context.Background()

	tool := discoveredToolOfExactRawSize(t, "skills", "skills.a", MaxMCPRegistryToolBytes+1)
	err := repo.UpsertTools(ctx, []DiscoveredTool{tool})
	if !errors.Is(err, ErrMCPRegistryToolBudgetExceeded) {
		t.Fatalf("err = %v, want ErrMCPRegistryToolBudgetExceeded", err)
	}
	if _, _, found, lookupErr := repo.GetToolPolicy(ctx, "skills", "skills.a"); lookupErr != nil || found {
		t.Fatalf("found=%v err=%v, want no row for a budget-refused snapshot", found, lookupErr)
	}
}

// The aggregate budget must be enforced across UpsertTools and
// replaceServerToolsTx together, not as two independently-budgeted
// halves: a third-party server's own tools (populated via ImportMCPServer,
// which funnels through replaceServerToolsTx) must count against how much
// room UpsertTools's own bundled/skills snapshot has left, and vice
// versa. This fills almost the whole budget with a third-party server's
// static snapshot, then proves a "skills" tool that would push the
// aggregate over budget is refused — and that the third-party server's
// own tool is left completely untouched by the refusal.
func TestUpsertToolsAggregateBudgetCountsExistingThirdPartyServerTools(t *testing.T) {
	repo := New(openTestDB(t))
	ctx := context.Background()

	thirdPartyTool := toolOfExactRawSize(t, "a", MaxMCPRegistryToolBytes-64)
	result, err := repo.ImportMCPServer(ctx, ImportedMCPServer{
		Name: "vendor-a", URL: "http://vendor-a:9000/mcp", Tier: MCPServerTierLocalContainer,
		Tools: []MCPServerTool{thirdPartyTool},
	})
	if err != nil {
		t.Fatalf("third-party server (comfortably under budget alone): %v", err)
	}

	// Exactly 64 bytes remain; ask UpsertTools for 65.
	skillsTool := discoveredToolOfExactRawSize(t, "skills", "skills.a", 65)
	err = repo.UpsertTools(ctx, []DiscoveredTool{skillsTool})
	if !errors.Is(err, ErrMCPRegistryToolBudgetExceeded) {
		t.Fatalf("err = %v, want ErrMCPRegistryToolBudgetExceeded: the third-party server's own tool must count against UpsertTools' own budget", err)
	}
	if _, _, found, lookupErr := repo.GetToolPolicy(ctx, "skills", "skills.a"); lookupErr != nil || found {
		t.Fatalf("found=%v err=%v, want no row for a budget-refused UpsertTools snapshot", found, lookupErr)
	}

	// The third-party server's own tool is completely unaffected.
	toolsA, err := repo.ListMCPServerTools(ctx, result.Server.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(toolsA) != 1 || toolsA[0].Name != "a" || !toolsA[0].Present || toolsA[0].SchemaJSON != thirdPartyTool.SchemaJSON {
		t.Fatalf("third-party server tools after the refused UpsertTools call = %+v, want unchanged", toolsA)
	}
}

// A budget refusal inside UpsertTools must roll back the *whole*
// transaction atomically, exactly like replaceServerToolsTx's own
// refusal does for ImportMCPServer/ReplaceMCPServerTools: UpsertTools's
// own withdrawal UPDATE (present=0 for every bundled/skills/legacy tool)
// must not survive a refused replacement snapshot. This first commits a
// small, legitimate "skills" tool, then forces the aggregate budget to be
// essentially exhausted by a third-party server, then attempts a second,
// larger UpsertTools snapshot that must be refused — and proves the
// original small tool is still present, unchanged, rather than left
// withdrawn (present=0) with nothing to replace it.
func TestUpsertToolsBudgetRefusalRollsBackWithdrawalLeavingPriorToolsUnchanged(t *testing.T) {
	repo := New(openTestDB(t))
	ctx := context.Background()

	original := DiscoveredTool{ServerName: "skills", ToolName: "skills.original", SchemaJSON: `{"type":"object"}`, Policy: "safe"}
	if err := repo.UpsertTools(ctx, []DiscoveredTool{original}); err != nil {
		t.Fatalf("initial UpsertTools snapshot: %v", err)
	}

	// Fill the rest of the budget with a third-party server's static
	// snapshot, leaving only a few bytes of room.
	thirdPartyTool := toolOfExactRawSize(t, "a", MaxMCPRegistryToolBytes-rawDiscoveredToolBytes(original)-10)
	if _, err := repo.ImportMCPServer(ctx, ImportedMCPServer{
		Name: "vendor-a", URL: "http://vendor-a:9000/mcp", Tier: MCPServerTierLocalContainer,
		Tools: []MCPServerTool{thirdPartyTool},
	}); err != nil {
		t.Fatal(err)
	}

	// A replacement "skills" snapshot larger than the remaining ~10-byte
	// margin must be refused.
	oversized := discoveredToolOfExactRawSize(t, "skills", "skills.original", 4096)
	err := repo.UpsertTools(ctx, []DiscoveredTool{oversized})
	if !errors.Is(err, ErrMCPRegistryToolBudgetExceeded) {
		t.Fatalf("err = %v, want ErrMCPRegistryToolBudgetExceeded", err)
	}

	// The original tool must still be present with its original schema —
	// never left withdrawn by the rolled-back replacement attempt.
	policy, enabled, found, lookupErr := repo.GetToolPolicy(ctx, "skills", "skills.original")
	if lookupErr != nil || !found || !enabled || policy != "safe" {
		t.Fatalf("policy=%q enabled=%v found=%v err=%v, want the original tool still present and enabled", policy, enabled, found, lookupErr)
	}
	var schemaJSON string
	var present bool
	if err := openTestDBQueryRow(t, repo, ctx,
		`SELECT schema_json, present FROM tools WHERE server_name = 'skills' AND tool_name = 'skills.original'`,
		&schemaJSON, &present,
	); err != nil {
		t.Fatal(err)
	}
	if !present || schemaJSON != original.SchemaJSON {
		t.Fatalf("skills.original present=%v schemaJSON=%q, want present=true and the original schema unchanged", present, schemaJSON)
	}
	if schemaJSON == oversized.SchemaJSON {
		t.Fatal("skills.original carries the refused replacement's oversized schema: the withdrawal+replace was not rolled back")
	}
}

// openTestDBQueryRow is a small helper so these tests can assert on raw
// column values the repository's own exported methods do not surface
// (schema_json/present together for a single row), reusing the same
// *db.DB the repository itself was constructed with.
func openTestDBQueryRow(t *testing.T, repo *Repository, ctx context.Context, query string, dest ...any) error {
	t.Helper()
	return repo.db.QueryRowContext(ctx, query).Scan(dest...)
}
