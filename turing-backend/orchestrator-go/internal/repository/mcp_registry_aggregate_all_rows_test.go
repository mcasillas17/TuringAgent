package repository

import (
	"context"
	"errors"
	"fmt"
	"testing"
)

// TestReplaceServerToolsTxRepeatedDisjointRediscoveryEventuallyExceedsBudget
// is the exact-rows proof for the aggregate budget: ListMCPServerTools (and
// therefore ListMcpServers/toolDescriptor) returns every row attributed to
// a server regardless of its `present` flag — a withdrawn tool's policy is
// deliberately preserved, never deleted, so a client can see it was once
// configured — so the byte budget that bounds that same response must
// count every one of those rows too, not just the currently-present ones.
// This repeatedly rediscovers a single server's tools, each round naming a
// tool disjoint from every previous round's (so no row is ever reused via
// upsert; every round leaves its predecessor behind, withdrawn but never
// deleted), and requires the budget to eventually refuse a round once the
// accumulated total (all rows, present and withdrawn combined) would
// exceed MaxMCPRegistryToolBytes — even though any *one* round's own
// present tool, alone, is comfortably within budget. Before this fix, the
// budget was computed only over present=1 rows, so a withdrawn
// predecessor's bytes were excluded the instant it was superseded: this
// loop would never fail at all, no matter how many rounds ran, because
// each round's own withdrawal always reset the "existing" total back
// toward zero regardless of how much of the table's real, still-persisted
// content was actually excluded from that count.
func TestReplaceServerToolsTxRepeatedDisjointRediscoveryEventuallyExceedsBudget(t *testing.T) {
	repo := New(openTestDB(t))
	ctx := context.Background()

	server, err := repo.RegisterMCPServer(ctx, ImportedMCPServer{
		Name: "vendor", URL: "http://vendor:9000/mcp", Tier: MCPServerTierLocalContainer,
	})
	if err != nil {
		t.Fatal(err)
	}

	// MaxMCPRegistryToolBytes is 262144; five rounds of 50000 raw bytes
	// each accumulate to 250000 (still within budget), and a sixth round
	// would bring the withdrawn-plus-current total to 300000 — over
	// budget — even though the sixth round's own tool, alone, is nowhere
	// near the cap.
	const roundSize = 50_000
	const maxRounds = 10
	round := 0
	var lastErr error
	for ; round < maxRounds; round++ {
		tool := toolOfExactRawSize(t, fmt.Sprintf("round-%d", round), roundSize)
		lastErr = repo.ReplaceMCPServerTools(ctx, server.Server.ID, []MCPServerTool{tool})
		if lastErr != nil {
			break
		}
	}
	if !errors.Is(lastErr, ErrMCPRegistryToolBudgetExceeded) {
		t.Fatalf("after %d rounds of disjoint-name rediscovery (err=%v), want ErrMCPRegistryToolBudgetExceeded eventually rather than unlimited growth", round, lastErr)
	}
	if round == 0 {
		t.Fatal("test setup is broken: the very first round must not itself exceed the budget")
	}

	// No partial state from the refused round: its own tool must not
	// exist at all (the withdrawal it also attempted must have rolled
	// back with it), and the *previous* round's tool — the last one that
	// actually committed — must remain present and completely unchanged.
	tools, err := repo.ListMCPServerTools(ctx, server.Server.ID)
	if err != nil {
		t.Fatal(err)
	}
	failedName := fmt.Sprintf("round-%d", round)
	lastGoodName := fmt.Sprintf("round-%d", round-1)
	presentCount := 0
	for _, tool := range tools {
		if tool.Name == failedName {
			t.Fatalf("tools = %+v: the refused round's tool %q must not exist at all", tools, failedName)
		}
		if tool.Present {
			presentCount++
			if tool.Name != lastGoodName {
				t.Fatalf("present tool = %q, want only %q (the last successfully committed round) present", tool.Name, lastGoodName)
			}
		}
	}
	if presentCount != 1 {
		t.Fatalf("present tool count = %d, want exactly 1 (the refused round must leave the previous round's tool present, not withdrawn)", presentCount)
	}
	// Every prior round's now-withdrawn row is still there (present=0),
	// never deleted — this is exactly what "preserve absent policies"
	// means, and exactly why the budget must count them: round rows
	// created equals round (the loop's own 0-based counter at the moment
	// of refusal, i.e. exactly the number of rounds that ran before it).
	if len(tools) != round {
		t.Fatalf("total tool rows = %d, want exactly %d (one per round that ran, every one of them still persisted)", len(tools), round)
	}
}

// TestUpsertToolsRepeatedDisjointRediscoveryEventuallyExceedsBudget is the
// UpsertTools counterpart (the bundled/skills/legacy path) of the test
// above: the same accumulation-of-withdrawn-rows behavior must hold
// identically for it, not just for replaceServerToolsTx.
func TestUpsertToolsRepeatedDisjointRediscoveryEventuallyExceedsBudget(t *testing.T) {
	repo := New(openTestDB(t))
	ctx := context.Background()

	const roundSize = 50_000
	const maxRounds = 10
	round := 0
	var lastErr error
	for ; round < maxRounds; round++ {
		tool := discoveredToolOfExactRawSize(t, "skills", fmt.Sprintf("skills.round-%d", round), roundSize)
		lastErr = repo.UpsertTools(ctx, []DiscoveredTool{tool})
		if lastErr != nil {
			break
		}
	}
	if !errors.Is(lastErr, ErrMCPRegistryToolBudgetExceeded) {
		t.Fatalf("after %d rounds of disjoint-name rediscovery (err=%v), want ErrMCPRegistryToolBudgetExceeded eventually", round, lastErr)
	}
	if round == 0 {
		t.Fatal("test setup is broken: the very first round must not itself exceed the budget")
	}

	failedName := fmt.Sprintf("skills.round-%d", round)
	if _, _, found, err := repo.GetToolPolicy(ctx, "skills", failedName); err != nil || found {
		t.Fatalf("found=%v err=%v, want no row at all for the refused round %q", found, err, failedName)
	}
	lastGoodName := fmt.Sprintf("skills.round-%d", round-1)
	_, enabled, found, err := repo.GetToolPolicy(ctx, "skills", lastGoodName)
	if err != nil || !found || !enabled {
		t.Fatalf("policy lookup for %q: found=%v enabled=%v err=%v, want present and enabled, unchanged by the refused round", lastGoodName, found, enabled, err)
	}
}
