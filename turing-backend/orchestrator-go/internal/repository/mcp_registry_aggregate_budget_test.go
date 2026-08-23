package repository

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// toolOfExactRawSize returns a single MCPServerTool whose raw
// (len(Name) + len(SchemaJSON)) byte size is exactly n, using a fixed
// `{"type":"object","d":"<padding>"}` shape so tests can hit an exact
// aggregate byte target without fiddly arithmetic per call site.
func toolOfExactRawSize(t *testing.T, name string, n int) MCPServerTool {
	t.Helper()
	const prefix = `{"type":"object","d":"`
	const suffix = `"}`
	pad := n - len(name) - len(prefix) - len(suffix)
	if pad < 0 {
		t.Fatalf("toolOfExactRawSize(%q, %d): target size too small for name+fixed shape overhead", name, n)
	}
	return MCPServerTool{
		Name:       name,
		Policy:     "safe",
		SchemaJSON: prefix + strings.Repeat("x", pad) + suffix,
		Enabled:    true,
		Present:    true,
	}
}

func rawToolBytes(tool MCPServerTool) int {
	return len(tool.Name) + len(tool.SchemaJSON)
}

// The aggregate present-tool byte budget (MaxMCPRegistryToolBytes) is
// enforced across *all* servers combined, not per server: two servers
// whose own individual tool snapshots are each comfortably under the
// (much larger) per-server maxMCPToolBytes cap must still be refused once
// their *combined* total would exceed MaxMCPRegistryToolBytes. This fills
// the budget across two servers to the exact byte, requiring both to
// succeed, then proves the very next byte — on a third server — is
// refused, and that the refusal is atomic: no row for the third server at
// all, and the first two servers' own tools are completely unaffected.
func TestReplaceServerToolsTxAggregateBudgetExactBoundaryAcrossMultipleServers(t *testing.T) {
	repo := New(openTestDB(t))
	ctx := context.Background()

	toolA := toolOfExactRawSize(t, "a", MaxMCPRegistryToolBytes-100)
	resultA, err := repo.ImportMCPServer(ctx, ImportedMCPServer{
		Name: "vendor-a", URL: "http://vendor-a:9000/mcp", Tier: MCPServerTierLocalContainer,
		Tools: []MCPServerTool{toolA},
	})
	if err != nil {
		t.Fatalf("server A (comfortably under budget alone): %v", err)
	}
	if !resultA.Created {
		t.Fatal("test setup is broken: server A must be newly created")
	}

	// Exactly the remaining 100 bytes: A + B lands exactly at
	// MaxMCPRegistryToolBytes.
	toolB := toolOfExactRawSize(t, "b", 100)
	if rawToolBytes(toolA)+rawToolBytes(toolB) != MaxMCPRegistryToolBytes {
		t.Fatalf("test setup is broken: A+B = %d, want exactly MaxMCPRegistryToolBytes (%d)",
			rawToolBytes(toolA)+rawToolBytes(toolB), MaxMCPRegistryToolBytes)
	}
	resultB, err := repo.ImportMCPServer(ctx, ImportedMCPServer{
		Name: "vendor-b", URL: "http://vendor-b:9000/mcp", Tier: MCPServerTierLocalContainer,
		Tools: []MCPServerTool{toolB},
	})
	if err != nil {
		t.Fatalf("server B (exactly at the aggregate budget with A): %v", err)
	}
	if !resultB.Created {
		t.Fatal("test setup is broken: server B must be newly created")
	}

	// A third server adding even a single additional byte must be
	// refused: A+B+C would be MaxMCPRegistryToolBytes+1.
	toolC := MCPServerTool{Name: "c", Policy: "safe", SchemaJSON: `{"type":"object"}`, Enabled: true, Present: true}
	_, err = repo.ImportMCPServer(ctx, ImportedMCPServer{
		Name: "vendor-c", URL: "http://vendor-c:9000/mcp", Tier: MCPServerTierLocalContainer,
		Tools: []MCPServerTool{toolC},
	})
	if !errors.Is(err, ErrMCPRegistryToolBudgetExceeded) {
		t.Fatalf("err = %v, want ErrMCPRegistryToolBudgetExceeded for the byte beyond the aggregate budget", err)
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

// The budget refusal must roll back the *whole* transaction atomically:
// for ImportMCPServer, that means the new server row itself must not
// exist afterward either — not merely its tools reconciliation reverted
// while somehow leaving a bare row behind.
func TestImportMCPServerBudgetRefusalCreatesNoRowAtAll(t *testing.T) {
	repo := New(openTestDB(t))
	ctx := context.Background()

	if _, err := repo.ImportMCPServer(ctx, ImportedMCPServer{
		Name: "vendor-a", URL: "http://vendor-a:9000/mcp", Tier: MCPServerTierLocalContainer,
		Tools: []MCPServerTool{toolOfExactRawSize(t, "a", MaxMCPRegistryToolBytes)},
	}); err != nil {
		t.Fatalf("server A (exactly at budget alone): %v", err)
	}

	_, err := repo.ImportMCPServer(ctx, ImportedMCPServer{
		Name: "vendor-over", URL: "http://vendor-over:9000/mcp", Tier: MCPServerTierLocalContainer,
		Tools: []MCPServerTool{{Name: "x", Policy: "safe", SchemaJSON: `{"type":"object"}`, Enabled: true, Present: true}},
	})
	if !errors.Is(err, ErrMCPRegistryToolBudgetExceeded) {
		t.Fatalf("err = %v, want ErrMCPRegistryToolBudgetExceeded", err)
	}
	if _, err := repo.GetMCPServerByName(ctx, "vendor-over"); err != ErrMCPServerNotFound {
		t.Fatalf("err = %v, want ErrMCPServerNotFound: the server row itself must not exist", err)
	}
}

// A snapshot at exactly the aggregate cap (as the *only* tenant of the
// budget) must still succeed: the boundary must not be off-by-one against
// a legitimate, in-bounds aggregate total.
func TestReplaceServerToolsTxAggregateBudgetAtExactCapAloneSucceeds(t *testing.T) {
	repo := New(openTestDB(t))
	ctx := context.Background()

	tool := toolOfExactRawSize(t, "a", MaxMCPRegistryToolBytes)
	result, err := repo.ImportMCPServer(ctx, ImportedMCPServer{
		Name: "vendor-a", URL: "http://vendor-a:9000/mcp", Tier: MCPServerTierLocalContainer,
		Tools: []MCPServerTool{tool},
	})
	if err != nil {
		t.Fatalf("a snapshot at exactly MaxMCPRegistryToolBytes must not be refused: %v", err)
	}
	if !result.Created {
		t.Fatal("want the server created")
	}
}

// One byte over the cap, as the only tenant of the budget, must be
// refused with no row created — the precise boundary case, isolated from
// any other server's own contribution.
func TestReplaceServerToolsTxAggregateBudgetOneByteOverCapAloneIsRefused(t *testing.T) {
	repo := New(openTestDB(t))
	ctx := context.Background()

	tool := toolOfExactRawSize(t, "a", MaxMCPRegistryToolBytes+1)
	_, err := repo.ImportMCPServer(ctx, ImportedMCPServer{
		Name: "vendor-a", URL: "http://vendor-a:9000/mcp", Tier: MCPServerTierLocalContainer,
		Tools: []MCPServerTool{tool},
	})
	if !errors.Is(err, ErrMCPRegistryToolBudgetExceeded) {
		t.Fatalf("err = %v, want ErrMCPRegistryToolBudgetExceeded", err)
	}
	if _, err := repo.GetMCPServerByName(ctx, "vendor-a"); err != ErrMCPServerNotFound {
		t.Fatalf("err = %v, want ErrMCPServerNotFound", err)
	}
}

// The aggregate budget must be enforced the same way for live discovery
// (ReplaceMCPServerTools, the path RecordDiscovery uses), not merely for a
// static mcp.json snapshot via ImportMCPServer: both funnel through the
// same replaceServerToolsTx. This fills almost the whole aggregate budget
// with one server's static snapshot, gives a second server a small
// existing tool while there is still just enough room for it, and then
// proves that server's *rediscovery* — attempting to replace that tool
// with a larger one that would push the aggregate over budget — is
// refused, atomically: the second server's original tool is left exactly
// as it was, present and unchanged, never withdrawn. A discovery must
// never leave a server with no tools where it had some just because a
// vendor's response grew too large for the registry-wide budget.
func TestReplaceServerToolsTxAggregateBudgetEnforcedForLiveDiscoveryToo(t *testing.T) {
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

	// Fill the rest of the budget with a different server's static
	// snapshot, leaving zero room for server B to grow.
	if _, err := repo.ImportMCPServer(ctx, ImportedMCPServer{
		Name: "vendor-a", URL: "http://vendor-a:9000/mcp", Tier: MCPServerTierLocalContainer,
		Tools: []MCPServerTool{toolOfExactRawSize(t, "a", MaxMCPRegistryToolBytes-64)},
	}); err != nil {
		t.Fatal(err)
	}

	// A rediscovery for server B that would grow its own tool by even one
	// byte must now be refused: the aggregate total is already exactly at
	// the cap.
	largerTool := toolOfExactRawSize(t, "vendor-b.existing", 65)
	if err := repo.ReplaceMCPServerTools(ctx, serverB.Server.ID, []MCPServerTool{largerTool}); !errors.Is(err, ErrMCPRegistryToolBudgetExceeded) {
		t.Fatalf("err = %v, want ErrMCPRegistryToolBudgetExceeded for live discovery over budget", err)
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
