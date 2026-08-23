package mcpregistry

import (
	"bytes"
	"context"
	"strings"
	"testing"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/db"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/secretbox"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// TestListMcpServersRefusesWhenAggregateToolBudgetIsPreexistingOversized
// is the defense-in-depth proof for the aggregate-budget guard: every
// tool-reconciliation write path (replaceServerToolsTx and, since this
// fix, UpsertTools too) now refuses to create a registry-wide aggregate
// over repository.MaxMCPRegistryToolBytes, so this state should be
// unreachable through any of them. But ListMcpServers must not silently
// trust that invariant forever — a database written before every path
// enforced it (this branch has not shipped, so no real deployment can
// carry one today, but a future regression reintroducing an unguarded
// write path could) must still be refused with a fixed, generic status
// rather than attempting to marshal and send an oversized response. The
// oversized row here is inserted directly via raw SQL, bypassing every
// repository method that would itself refuse such a write, precisely to
// simulate that "somehow already oversized" state.
func TestListMcpServersRefusesWhenAggregateToolBudgetIsPreexistingOversized(t *testing.T) {
	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if err := db.ApplyMigrations(context.Background(), database); err != nil {
		t.Fatal(err)
	}
	repo := repository.New(database)
	sealer, err := secretbox.New(bytes.Repeat([]byte{0x41}, secretbox.KeySize))
	if err != nil {
		t.Fatal(err)
	}
	service := New(repo, sealer, nil)
	ctx := context.Background()

	oversizedSchema := `{"type":"object","d":"` + strings.Repeat("x", repository.MaxMCPRegistryToolBytes) + `"}`
	if _, err := database.ExecContext(ctx, `
		INSERT INTO tools (id, server_name, tool_name, policy, schema_json, enabled, discovered_at, mcp_server_id, present)
		VALUES ('tool_test_oversized', 'skills', 'skills.oversized', 'safe', ?, 1, datetime('now'), NULL, 1)
	`, oversizedSchema); err != nil {
		t.Fatal(err)
	}

	_, err = service.ListMcpServers(ctx, &turingv1.ListMcpServersRequest{})
	if err == nil {
		t.Fatal("ListMcpServers succeeded despite a preexisting oversized aggregate tool budget")
	}
	if status.Code(err) != codes.ResourceExhausted {
		t.Fatalf("code = %v, want ResourceExhausted", status.Code(err))
	}
}

// TestListMcpServersSucceedsAtExactAggregateBudgetBoundary proves the
// guard is not off-by-one: the registry's own enforced boundary
// (repository.MaxMCPRegistryToolBytes exactly, the largest total every
// write path already allows) must still list successfully.
func TestListMcpServersSucceedsAtExactAggregateBudgetBoundary(t *testing.T) {
	service, repo := newRegistryTestService(t)
	ctx := context.Background()

	tool := discoveredToolAtExactSize(t, repository.MaxMCPRegistryToolBytes)
	if err := repo.UpsertTools(ctx, []repository.DiscoveredTool{tool}); err != nil {
		t.Fatalf("a snapshot at exactly MaxMCPRegistryToolBytes must not be refused: %v", err)
	}

	if _, err := service.ListMcpServers(ctx, &turingv1.ListMcpServersRequest{}); err != nil {
		t.Fatalf("ListMcpServers at exactly the aggregate budget boundary must succeed: %v", err)
	}
}

// discoveredToolAtExactSize mirrors the repository package's own
// toolOfExactRawSize/discoveredToolOfExactRawSize helpers (unreachable
// from this package, which is why this is redefined here rather than
// imported) for a single "skills" DiscoveredTool whose raw
// (len(ToolName)+len(SchemaJSON)) size is exactly n.
func discoveredToolAtExactSize(t *testing.T, n int) repository.DiscoveredTool {
	t.Helper()
	const name = "skills.sized"
	const prefix = `{"type":"object","d":"`
	const suffix = `"}`
	pad := n - len(name) - len(prefix) - len(suffix)
	if pad < 0 {
		t.Fatalf("discoveredToolAtExactSize(%d): target size too small", n)
	}
	return repository.DiscoveredTool{
		ServerName: "skills", ToolName: name, Policy: "safe",
		SchemaJSON: prefix + strings.Repeat("x", pad) + suffix,
	}
}
