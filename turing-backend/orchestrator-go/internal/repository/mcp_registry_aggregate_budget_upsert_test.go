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
// versa. The third-party contribution here (100000 bytes) stays
// comfortably under its own, narrower MaxThirdPartyMCPRegistryToolBytes
// sub-cap (128KiB) on its own — this test is specifically about the
// full, shared aggregate the two paths still have in common, not about
// the third-party sub-cap (see mcp_registry_third_party_budget_test.go
// for that). This fills most of the remaining full-aggregate room with
// that third-party server's static snapshot, then proves a "skills"
// tool that would push the aggregate one byte over is refused — and
// that the third-party server's own tool is left completely untouched
// by the refusal.
func TestUpsertToolsAggregateBudgetCountsExistingThirdPartyServerTools(t *testing.T) {
	repo := New(openTestDB(t))
	ctx := context.Background()

	const thirdPartyBytes = 100_000
	thirdPartyTool := toolOfExactRawSize(t, "a", thirdPartyBytes)
	result, err := repo.ImportMCPServer(ctx, ImportedMCPServer{
		Name: "vendor-a", URL: "http://vendor-a:9000/mcp", Tier: MCPServerTierLocalContainer,
		Tools: []MCPServerTool{thirdPartyTool},
	})
	if err != nil {
		t.Fatalf("third-party server (comfortably under both its own sub-budget and the full aggregate alone): %v", err)
	}

	// Exactly MaxMCPRegistryToolBytes-thirdPartyBytes remain; ask
	// UpsertTools for one byte more than that.
	remaining := MaxMCPRegistryToolBytes - thirdPartyBytes
	skillsTool := discoveredToolOfExactRawSize(t, "skills", "skills.a", remaining+1)
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
// small, legitimate "skills" tool, then adds a third-party server's own
// tool — comfortably under its own MaxThirdPartyMCPRegistryToolBytes
// sub-cap — leaving only a small amount of full-aggregate room, then
// attempts a second, larger UpsertTools snapshot (replacing the same
// "skills.original" name with an oversized schema) that must be refused
// — and proves the original tool is still present with its exact
// original schema, rather than left withdrawn (present=0) or replaced
// with the oversized one.
func TestUpsertToolsBudgetRefusalRollsBackWithdrawalLeavingPriorToolsUnchanged(t *testing.T) {
	repo := New(openTestDB(t))
	ctx := context.Background()

	original := discoveredToolOfExactRawSize(t, "skills", "skills.original", 50_000)
	if err := repo.UpsertTools(ctx, []DiscoveredTool{original}); err != nil {
		t.Fatalf("initial UpsertTools snapshot: %v", err)
	}

	// A third-party server's own tool, comfortably under its own
	// sub-cap, leaves only a small amount of full-aggregate room:
	// original's own prior 50000 bytes are irrelevant to that room, since
	// the replacement below fully supersedes (not adds to) the same key.
	const thirdPartyBytes = 100_000
	if _, err := repo.ImportMCPServer(ctx, ImportedMCPServer{
		Name: "vendor-a", URL: "http://vendor-a:9000/mcp", Tier: MCPServerTierLocalContainer,
		Tools: []MCPServerTool{toolOfExactRawSize(t, "a", thirdPartyBytes)},
	}); err != nil {
		t.Fatal(err)
	}

	// A replacement "skills.original" one byte larger than the room
	// remaining after the third-party contribution must be refused.
	remaining := MaxMCPRegistryToolBytes - thirdPartyBytes
	oversized := discoveredToolOfExactRawSize(t, "skills", "skills.original", remaining+1)
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
		t.Fatalf("skills.original present=%v schemaJSON has length %d, want present=true and the original schema unchanged", present, len(schemaJSON))
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
