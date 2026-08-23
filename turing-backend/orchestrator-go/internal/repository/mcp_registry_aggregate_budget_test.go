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

// The full registry-wide aggregate byte budget (MaxMCPRegistryToolBytes)
// is enforced across *all* servers combined — bundled/skills (via
// UpsertTools) and non-bundled (via replaceServerToolsTx) alike — not
// per server and not per tier: two non-bundled servers whose own
// combined tools stay comfortably within their own, narrower
// MaxThirdPartyMCPRegistryToolBytes sub-budget must still be refused
// once the *full* aggregate — including a separate bundled/skills
// contribution — would be exceeded. This anchors most of the full
// aggregate with a "skills" snapshot via UpsertTools (leaving exactly
// 1000 bytes of aggregate headroom, far more than either non-bundled
// server below will ever use on its own), fills that remaining headroom
// across two non-bundled servers to the exact byte, requiring both to
// succeed, then proves the very next byte — on a third, non-bundled
// server — is refused with the full-aggregate reason (never the
// third-party-specific one: the three non-bundled servers' own combined
// share stays nowhere near their own sub-budget throughout) — and that
// the refusal is atomic: no row for the third server at all, and the
// first two servers' own tools are completely unaffected.
func TestReplaceServerToolsTxAggregateBudgetExactBoundaryAcrossMultipleServers(t *testing.T) {
	repo := New(openTestDB(t))
	ctx := context.Background()

	const aggregateHeadroom = 1000
	if err := repo.UpsertTools(ctx, []DiscoveredTool{
		discoveredToolOfExactRawSize(t, "skills", "skills.anchor", MaxMCPRegistryToolBytes-aggregateHeadroom),
	}); err != nil {
		t.Fatalf("bundled/skills anchor snapshot: %v", err)
	}

	toolA := toolOfExactRawSize(t, "a", aggregateHeadroom-100)
	resultA, err := repo.ImportMCPServer(ctx, ImportedMCPServer{
		Name: "vendor-a", URL: "http://vendor-a:9000/mcp", Tier: MCPServerTierLocalContainer,
		Tools: []MCPServerTool{toolA},
	})
	if err != nil {
		t.Fatalf("server A (comfortably under both its own sub-budget and the remaining aggregate headroom): %v", err)
	}
	if !resultA.Created {
		t.Fatal("test setup is broken: server A must be newly created")
	}

	// Exactly the remaining 100 bytes: anchor + A + B lands exactly at
	// MaxMCPRegistryToolBytes.
	toolB := toolOfExactRawSize(t, "b", 100)
	if rawToolBytes(toolA)+rawToolBytes(toolB) != aggregateHeadroom {
		t.Fatalf("test setup is broken: A+B = %d, want exactly the aggregate headroom (%d)",
			rawToolBytes(toolA)+rawToolBytes(toolB), aggregateHeadroom)
	}
	resultB, err := repo.ImportMCPServer(ctx, ImportedMCPServer{
		Name: "vendor-b", URL: "http://vendor-b:9000/mcp", Tier: MCPServerTierLocalContainer,
		Tools: []MCPServerTool{toolB},
	})
	if err != nil {
		t.Fatalf("server B (exactly at the full aggregate budget with the anchor and A): %v", err)
	}
	if !resultB.Created {
		t.Fatal("test setup is broken: server B must be newly created")
	}

	// A third, non-bundled server adding even a single additional byte
	// must be refused: anchor+A+B+C would be MaxMCPRegistryToolBytes+1 —
	// while the three non-bundled servers' own combined third-party
	// share (aggregateHeadroom plus C's few bytes) stays nowhere near
	// MaxThirdPartyMCPRegistryToolBytes.
	toolC := MCPServerTool{Name: "c", Policy: "safe", SchemaJSON: `{"type":"object"}`, Enabled: true, Present: true}
	_, err = repo.ImportMCPServer(ctx, ImportedMCPServer{
		Name: "vendor-c", URL: "http://vendor-c:9000/mcp", Tier: MCPServerTierLocalContainer,
		Tools: []MCPServerTool{toolC},
	})
	if !errors.Is(err, ErrMCPRegistryToolBudgetExceeded) {
		t.Fatalf("err = %v, want ErrMCPRegistryToolBudgetExceeded for the byte beyond the full aggregate budget", err)
	}
	if errors.Is(err, ErrMCPThirdPartyToolBudgetExceeded) {
		t.Fatalf("err = %v, must not be ErrMCPThirdPartyToolBudgetExceeded: the three non-bundled servers' combined share is nowhere near that narrower cap", err)
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
// while somehow leaving a bare row behind. The full aggregate is
// anchored mostly by a bundled/skills snapshot (via UpsertTools) so the
// one non-bundled server below can sit exactly at the aggregate boundary
// while staying comfortably within its own, separate third-party
// sub-budget.
func TestImportMCPServerBudgetRefusalCreatesNoRowAtAll(t *testing.T) {
	repo := New(openTestDB(t))
	ctx := context.Background()

	const thirdPartyBytes = 1000
	if err := repo.UpsertTools(ctx, []DiscoveredTool{
		discoveredToolOfExactRawSize(t, "skills", "skills.anchor", MaxMCPRegistryToolBytes-thirdPartyBytes),
	}); err != nil {
		t.Fatalf("bundled/skills anchor snapshot: %v", err)
	}
	if _, err := repo.ImportMCPServer(ctx, ImportedMCPServer{
		Name: "vendor-a", URL: "http://vendor-a:9000/mcp", Tier: MCPServerTierLocalContainer,
		Tools: []MCPServerTool{toolOfExactRawSize(t, "a", thirdPartyBytes)},
	}); err != nil {
		t.Fatalf("server A (exactly at the full aggregate budget, comfortably under its own sub-budget): %v", err)
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

// A snapshot at exactly the full aggregate cap must still succeed: the
// boundary must not be off-by-one against a legitimate, in-bounds
// aggregate total, even when most of it belongs to a different tier
// entirely (a bundled/skills anchor via UpsertTools).
func TestReplaceServerToolsTxAggregateBudgetAtExactCapAloneSucceeds(t *testing.T) {
	repo := New(openTestDB(t))
	ctx := context.Background()

	const thirdPartyBytes = 1000
	if err := repo.UpsertTools(ctx, []DiscoveredTool{
		discoveredToolOfExactRawSize(t, "skills", "skills.anchor", MaxMCPRegistryToolBytes-thirdPartyBytes),
	}); err != nil {
		t.Fatalf("bundled/skills anchor snapshot: %v", err)
	}

	tool := toolOfExactRawSize(t, "a", thirdPartyBytes)
	result, err := repo.ImportMCPServer(ctx, ImportedMCPServer{
		Name: "vendor-a", URL: "http://vendor-a:9000/mcp", Tier: MCPServerTierLocalContainer,
		Tools: []MCPServerTool{tool},
	})
	if err != nil {
		t.Fatalf("a snapshot at exactly the full aggregate cap must not be refused: %v", err)
	}
	if !result.Created {
		t.Fatal("want the server created")
	}
}

// One byte over the full aggregate cap must be refused with no row
// created — the precise boundary case, with most of the aggregate
// anchored by an unrelated bundled/skills contribution rather than the
// one non-bundled server under test.
func TestReplaceServerToolsTxAggregateBudgetOneByteOverCapAloneIsRefused(t *testing.T) {
	repo := New(openTestDB(t))
	ctx := context.Background()

	const thirdPartyBytes = 1000
	if err := repo.UpsertTools(ctx, []DiscoveredTool{
		discoveredToolOfExactRawSize(t, "skills", "skills.anchor", MaxMCPRegistryToolBytes-thirdPartyBytes),
	}); err != nil {
		t.Fatalf("bundled/skills anchor snapshot: %v", err)
	}

	tool := toolOfExactRawSize(t, "a", thirdPartyBytes+1)
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

// The full aggregate budget must be enforced the same way for live
// discovery (ReplaceMCPServerTools, the path RecordDiscovery uses), not
// merely for a static mcp.json snapshot via ImportMCPServer: both funnel
// through the same replaceServerToolsTx. This anchors most of the full
// aggregate with a bundled/skills snapshot via UpsertTools, gives a
// second (non-bundled) server a small existing tool while there is still
// just enough aggregate room for it, and then proves that server's
// *rediscovery* — attempting to replace that tool with a larger one that
// would push the full aggregate over budget, while staying nowhere near
// its own third-party sub-budget — is refused, atomically: the second
// server's original tool is left exactly as it was, present and
// unchanged, never withdrawn. A discovery must never leave a server with
// no tools where it had some just because a vendor's response grew too
// large for the registry-wide budget.
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

	// Anchor most of the full aggregate with a bundled/skills snapshot,
	// then fill the rest with a different (non-bundled) server's static
	// snapshot, leaving zero aggregate room for server B to grow — while
	// every non-bundled server's own combined share stays nowhere near
	// MaxThirdPartyMCPRegistryToolBytes.
	if err := repo.UpsertTools(ctx, []DiscoveredTool{
		discoveredToolOfExactRawSize(t, "skills", "skills.anchor", MaxMCPRegistryToolBytes-1000),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.ImportMCPServer(ctx, ImportedMCPServer{
		Name: "vendor-a", URL: "http://vendor-a:9000/mcp", Tier: MCPServerTierLocalContainer,
		Tools: []MCPServerTool{toolOfExactRawSize(t, "a", 1000-64)},
	}); err != nil {
		t.Fatal(err)
	}

	// A rediscovery for server B that would grow its own tool by even one
	// byte must now be refused: the full aggregate total is already
	// exactly at the cap.
	largerTool := toolOfExactRawSize(t, "vendor-b.existing", 65)
	err = repo.ReplaceMCPServerTools(ctx, serverB.Server.ID, []MCPServerTool{largerTool})
	if !errors.Is(err, ErrMCPRegistryToolBudgetExceeded) {
		t.Fatalf("err = %v, want ErrMCPRegistryToolBudgetExceeded for live discovery over the full aggregate budget", err)
	}
	if errors.Is(err, ErrMCPThirdPartyToolBudgetExceeded) {
		t.Fatalf("err = %v, must not be ErrMCPThirdPartyToolBudgetExceeded: the non-bundled servers' combined share is nowhere near that narrower cap", err)
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
