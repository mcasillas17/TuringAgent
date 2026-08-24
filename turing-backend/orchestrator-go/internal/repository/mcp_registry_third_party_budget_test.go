package repository

import (
	"context"
	"errors"
	"testing"
)

// The third-party sub-budget (MaxThirdPartyMCPRegistryToolBytes) is
// enforced across *all* non-bundled servers combined, exactly the way
// the full aggregate budget already is: two servers whose own individual
// tool snapshots are each comfortably under the sub-budget must still be
// refused once their *combined* total would exceed it — well before
// either would come anywhere near the full, much larger
// MaxMCPRegistryToolBytes. This fills the sub-budget across two servers
// to the exact byte, requiring both to succeed, then proves the very
// next byte — on a third server — is refused, atomically: no row for
// the third server at all, and the first two servers' own tools
// completely unaffected.
func TestReplaceServerToolsTxThirdPartyBudgetExactBoundaryAcrossMultipleServers(t *testing.T) {
	repo := New(openTestDB(t))
	ctx := context.Background()

	toolA := toolOfExactRawSize(t, "a", MaxThirdPartyMCPRegistryToolBytes-100)
	resultA, err := repo.ImportMCPServer(ctx, ImportedMCPServer{
		Name: "vendor-a", URL: "http://vendor-a:9000/mcp", Tier: MCPServerTierLocalContainer,
		Tools: []MCPServerTool{toolA},
	})
	if err != nil {
		t.Fatalf("server A (comfortably under the third-party sub-budget alone): %v", err)
	}
	if !resultA.Created {
		t.Fatal("test setup is broken: server A must be newly created")
	}

	// Exactly the remaining 100 bytes: A + B lands exactly at
	// MaxThirdPartyMCPRegistryToolBytes.
	toolB := toolOfExactRawSize(t, "b", 100)
	if rawToolBytes(toolA)+rawToolBytes(toolB) != MaxThirdPartyMCPRegistryToolBytes {
		t.Fatalf("test setup is broken: A+B = %d, want exactly MaxThirdPartyMCPRegistryToolBytes (%d)",
			rawToolBytes(toolA)+rawToolBytes(toolB), MaxThirdPartyMCPRegistryToolBytes)
	}
	resultB, err := repo.ImportMCPServer(ctx, ImportedMCPServer{
		Name: "vendor-b", URL: "http://vendor-b:9000/mcp", Tier: MCPServerTierLocalContainer,
		Tools: []MCPServerTool{toolB},
	})
	if err != nil {
		t.Fatalf("server B (exactly at the third-party sub-budget with A): %v", err)
	}
	if !resultB.Created {
		t.Fatal("test setup is broken: server B must be newly created")
	}

	// A third server adding even a single additional byte must be
	// refused: A+B+C would be MaxThirdPartyMCPRegistryToolBytes+1 —
	// still nowhere near the much larger full aggregate cap.
	toolC := MCPServerTool{Name: "c", Policy: "safe", SchemaJSON: `{"type":"object"}`, Enabled: true, Present: true}
	_, err = repo.ImportMCPServer(ctx, ImportedMCPServer{
		Name: "vendor-c", URL: "http://vendor-c:9000/mcp", Tier: MCPServerTierLocalContainer,
		Tools: []MCPServerTool{toolC},
	})
	if !errors.Is(err, ErrMCPThirdPartyToolBudgetExceeded) {
		t.Fatalf("err = %v, want ErrMCPThirdPartyToolBudgetExceeded for the byte beyond the third-party sub-budget", err)
	}

	// No partial replacement / no partial write: server C must not exist
	// at all, and A and B's own tools must be completely untouched.
	if _, err := repo.GetMCPServerByName(ctx, "vendor-c"); err != ErrMCPServerNotFound {
		t.Fatalf("err = %v, want ErrMCPServerNotFound: a budget-refused import must create no row", err)
	}
	toolsA, err := repo.ListMCPServerTools(ctx, resultA.Server.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(toolsA) != 1 || toolsA[0].Name != "a" || !toolsA[0].Present {
		t.Fatalf("server A tools after the refused import = %+v, want unchanged", toolsA)
	}
	toolsB, err := repo.ListMCPServerTools(ctx, resultB.Server.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(toolsB) != 1 || toolsB[0].Name != "b" || !toolsB[0].Present {
		t.Fatalf("server B tools after the refused import = %+v, want unchanged", toolsB)
	}
}

// A snapshot at exactly the third-party sub-budget (as the only tenant)
// must still succeed: the boundary must not be off-by-one.
func TestReplaceServerToolsTxThirdPartyBudgetAtExactCapAloneSucceeds(t *testing.T) {
	repo := New(openTestDB(t))
	ctx := context.Background()

	tool := toolOfExactRawSize(t, "a", MaxThirdPartyMCPRegistryToolBytes)
	result, err := repo.ImportMCPServer(ctx, ImportedMCPServer{
		Name: "vendor-a", URL: "http://vendor-a:9000/mcp", Tier: MCPServerTierLocalContainer,
		Tools: []MCPServerTool{tool},
	})
	if err != nil {
		t.Fatalf("a snapshot at exactly MaxThirdPartyMCPRegistryToolBytes must not be refused: %v", err)
	}
	if !result.Created {
		t.Fatal("want the server created")
	}
}

// One byte over the third-party sub-budget, as the only tenant, must be
// refused with no row created.
func TestReplaceServerToolsTxThirdPartyBudgetOneByteOverCapAloneIsRefused(t *testing.T) {
	repo := New(openTestDB(t))
	ctx := context.Background()

	tool := toolOfExactRawSize(t, "a", MaxThirdPartyMCPRegistryToolBytes+1)
	_, err := repo.ImportMCPServer(ctx, ImportedMCPServer{
		Name: "vendor-a", URL: "http://vendor-a:9000/mcp", Tier: MCPServerTierLocalContainer,
		Tools: []MCPServerTool{tool},
	})
	if !errors.Is(err, ErrMCPThirdPartyToolBudgetExceeded) {
		t.Fatalf("err = %v, want ErrMCPThirdPartyToolBudgetExceeded", err)
	}
	if _, err := repo.GetMCPServerByName(ctx, "vendor-a"); err != ErrMCPServerNotFound {
		t.Fatalf("err = %v, want ErrMCPServerNotFound", err)
	}
}

// The third-party sub-budget must be enforced for live discovery
// (ReplaceMCPServerTools) too, not merely a static mcp.json snapshot via
// ImportMCPServer — both funnel through the same replaceServerToolsTx.
func TestReplaceServerToolsTxThirdPartyBudgetEnforcedForLiveDiscoveryToo(t *testing.T) {
	repo := New(openTestDB(t))
	ctx := context.Background()

	serverB, err := repo.RegisterMCPServer(ctx, ImportedMCPServer{
		Name: "vendor-b", URL: "http://vendor-b:9000/mcp", Tier: MCPServerTierLocalContainer,
	})
	if err != nil {
		t.Fatal(err)
	}
	existingTool := toolOfExactRawSize(t, "vendor-b.existing", 64)
	if err := repo.ReplaceMCPServerTools(ctx, serverB.Server.ID, []MCPServerTool{existingTool}); err != nil {
		t.Fatalf("server B's initial (small) tool set must succeed: %v", err)
	}

	// Fill the rest of the third-party sub-budget with a different
	// server's static snapshot, leaving zero room for server B to grow.
	if _, err := repo.ImportMCPServer(ctx, ImportedMCPServer{
		Name: "vendor-a", URL: "http://vendor-a:9000/mcp", Tier: MCPServerTierLocalContainer,
		Tools: []MCPServerTool{toolOfExactRawSize(t, "a", MaxThirdPartyMCPRegistryToolBytes-64)},
	}); err != nil {
		t.Fatal(err)
	}

	// A rediscovery for server B that would grow its own tool by even
	// one byte must now be refused: the third-party total is already
	// exactly at its own sub-cap.
	largerTool := toolOfExactRawSize(t, "vendor-b.existing", 65)
	if err := repo.ReplaceMCPServerTools(ctx, serverB.Server.ID, []MCPServerTool{largerTool}); !errors.Is(err, ErrMCPThirdPartyToolBudgetExceeded) {
		t.Fatalf("err = %v, want ErrMCPThirdPartyToolBudgetExceeded for live discovery over the third-party sub-budget", err)
	}
	toolsB, err := repo.ListMCPServerTools(ctx, serverB.Server.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(toolsB) != 1 || toolsB[0].Name != "vendor-b.existing" || !toolsB[0].Present ||
		toolsB[0].SchemaJSON != existingTool.SchemaJSON {
		t.Fatalf("server B tools after a refused rediscovery = %+v, want the original tool completely unchanged", toolsB)
	}
}

// UpsertTools — the bundled/skills/legacy path — must be completely
// unaffected by the third-party sub-budget: it may still grow all the
// way to the full registry-wide aggregate (MaxMCPRegistryToolBytes) on
// its own, exactly as before this change. This is the "guaranteed
// headroom" promise from the other direction: not only must a
// third-party server never be able to starve UpsertTools of its own
// share, UpsertTools itself must never be narrowed by a cap that only
// ever applies to non-bundled servers.
func TestUpsertToolsUnaffectedByThirdPartySubBudget(t *testing.T) {
	repo := New(openTestDB(t))
	ctx := context.Background()

	tool := discoveredToolOfExactRawSize(t, "skills", "skills.a", MaxMCPRegistryToolBytes)
	if err := repo.UpsertTools(ctx, []DiscoveredTool{tool}); err != nil {
		t.Fatalf("UpsertTools at exactly the full aggregate cap must not be refused by the third-party sub-budget: %v", err)
	}
}

// TestAggregateBudgetStillEnforcedAcrossThirdPartyAndBundledCombined
// proves the full aggregate cap remains a hard backstop even when every
// non-bundled server, combined, stays comfortably within its own
// third-party sub-budget: this fills nearly the entire full aggregate
// with a bundled/skills snapshot via UpsertTools (allowed: UpsertTools
// has no sub-cap of its own), leaves a small amount of third-party
// headroom with one vendor's own small tool (nowhere near the much
// larger third-party sub-budget on its own), and then proves a second
// third-party server — whose own combined third-party share, together
// with the first, is still nowhere near MaxThirdPartyMCPRegistryToolBytes
// — is nonetheless refused once its own addition would push the full
// aggregate over MaxMCPRegistryToolBytes, with the fixed
// ErrMCPRegistryToolBudgetExceeded reason specifically (not the
// third-party-specific one, since the third-party-only share is not what
// is over budget here).
func TestAggregateBudgetStillEnforcedAcrossThirdPartyAndBundledCombined(t *testing.T) {
	repo := New(openTestDB(t))
	ctx := context.Background()

	// Bundled/skills consumes all but 100 bytes of the full aggregate —
	// nowhere near MaxThirdPartyMCPRegistryToolBytes itself (it is not
	// counted as third-party at all), but leaving very little aggregate
	// headroom for anyone else.
	if err := repo.UpsertTools(ctx, []DiscoveredTool{
		discoveredToolOfExactRawSize(t, "skills", "skills.a", MaxMCPRegistryToolBytes-100),
	}); err != nil {
		t.Fatalf("UpsertTools filling nearly the whole full aggregate: %v", err)
	}

	// vendor-a takes 50 of the remaining 100 aggregate bytes — trivially
	// within the third-party sub-budget (128KiB) on its own.
	if _, err := repo.ImportMCPServer(ctx, ImportedMCPServer{
		Name: "vendor-a", URL: "http://vendor-a:9000/mcp", Tier: MCPServerTierLocalContainer,
		Tools: []MCPServerTool{toolOfExactRawSize(t, "a", 50)},
	}); err != nil {
		t.Fatalf("vendor-a (comfortably within the third-party sub-budget): %v", err)
	}

	// vendor-b's own 51-byte tool — combined with vendor-a's, the
	// third-party total is only 101 bytes, nowhere near the 128KiB
	// third-party sub-budget — pushes the full aggregate one byte over
	// its own, separate 256KiB cap.
	_, err := repo.ImportMCPServer(ctx, ImportedMCPServer{
		Name: "vendor-b", URL: "http://vendor-b:9000/mcp", Tier: MCPServerTierLocalContainer,
		Tools: []MCPServerTool{toolOfExactRawSize(t, "b", 51)},
	})
	if !errors.Is(err, ErrMCPRegistryToolBudgetExceeded) {
		t.Fatalf("err = %v, want ErrMCPRegistryToolBudgetExceeded: the combined third-party share (101 bytes) is nowhere near its own sub-budget, but the full aggregate is now exhausted", err)
	}
	if errors.Is(err, ErrMCPThirdPartyToolBudgetExceeded) {
		t.Fatalf("err = %v, must not be ErrMCPThirdPartyToolBudgetExceeded: the combined third-party share is nowhere near that narrower cap", err)
	}
	if _, err := repo.GetMCPServerByName(ctx, "vendor-b"); err != ErrMCPServerNotFound {
		t.Fatalf("err = %v, want ErrMCPServerNotFound: a budget-refused import must create no row", err)
	}
}
