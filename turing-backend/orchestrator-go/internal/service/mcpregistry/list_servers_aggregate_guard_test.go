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
	"google.golang.org/protobuf/proto"
)

// TestListMcpServersDegradesGracefullyWhenAggregateToolBudgetIsPreexistingOversized
// is the recovery-story proof for the aggregate-budget guard: every
// tool-reconciliation write path (replaceServerToolsTx and UpsertTools)
// already refuses to create a registry-wide aggregate over
// repository.MaxMCPRegistryToolBytes, so this state should be
// unreachable through any of them — but a database that somehow already
// carries one (a legacy state predating one of those guards, or a future
// regression reintroducing an unguarded write path) must not leave the
// registry stuck: ListMcpServers must stay usable enough for an operator
// to find and delete whatever is responsible. The oversized row here is
// inserted directly via raw SQL, bypassing every repository method that
// would itself refuse such a write, precisely to simulate that "somehow
// already oversized" state, attributed to a real, deletable non-bundled
// server (never to "skills", which owns no deletable mcp_servers row at
// all).
//
// This proves, in one continuous scenario: the response stays under the
// 4MiB gRPC message cap; every server (the healthy one and the offending
// one alike) is still listed with its own Tools completely empty; the
// explicit RegistryDegraded/RegistryDegradationReason fields explain why;
// DeleteMcpServer still works on the offending server even while the
// aggregate remains over budget (its oversized tool row cascade-deletes
// with it); and a subsequent ListMcpServers call — now back under budget
// — recovers full tool listing automatically, with RegistryDegraded
// cleared.
func TestListMcpServersDegradesGracefullyWhenAggregateToolBudgetIsPreexistingOversized(t *testing.T) {
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

	healthy, err := repo.RegisterMCPServer(ctx, repository.ImportedMCPServer{
		Name: "vendor-healthy", URL: "http://vendor-healthy:9000/mcp", Tier: repository.MCPServerTierLocalContainer,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.ReplaceMCPServerTools(ctx, healthy.Server.ID, []repository.MCPServerTool{
		{Name: "vendor-healthy.tool", Policy: "safe", SchemaJSON: `{"type":"object"}`, Enabled: true, Present: true},
	}); err != nil {
		t.Fatal(err)
	}

	offending, err := repo.RegisterMCPServer(ctx, repository.ImportedMCPServer{
		Name: "vendor-oversized", URL: "http://vendor-oversized:9000/mcp", Tier: repository.MCPServerTierLocalContainer,
	})
	if err != nil {
		t.Fatal(err)
	}
	oversizedSchema := `{"type":"object","d":"` + strings.Repeat("x", repository.MaxMCPRegistryToolBytes) + `"}`
	if _, err := database.ExecContext(ctx, `
		INSERT INTO tools (id, server_name, tool_name, policy, schema_json, enabled, discovered_at, mcp_server_id, present)
		VALUES ('tool_test_oversized', ?, 'vendor-oversized.tool', 'safe', ?, 1, datetime('now'), ?, 1)
	`, offending.Server.Name, oversizedSchema, offending.Server.ID); err != nil {
		t.Fatal(err)
	}

	response, err := service.ListMcpServers(ctx, &turingv1.ListMcpServersRequest{})
	if err != nil {
		t.Fatalf("ListMcpServers must remain usable for management despite a preexisting oversized aggregate, not fail outright: %v", err)
	}
	encoded, err := proto.Marshal(response)
	if err != nil {
		t.Fatalf("marshal ListMcpServersResponse: %v", err)
	}
	if len(encoded) >= maxGRPCMessageSizeForTest {
		t.Fatalf("response marshaled to %d bytes, want strictly under the gRPC message cap (%d)", len(encoded), maxGRPCMessageSizeForTest)
	}

	var foundHealthy, foundOffending bool
	for _, server := range response.GetServers() {
		if len(server.GetTools()) != 0 {
			t.Fatalf("server %q Tools = %+v, want empty while the aggregate is over budget", server.GetName(), server.GetTools())
		}
		if server.GetServerId() == healthy.Server.ID {
			foundHealthy = true
		}
		if server.GetServerId() == offending.Server.ID {
			foundOffending = true
		}
	}
	if !foundHealthy {
		t.Fatalf("Servers = %+v, want vendor-healthy still listed so it stays manageable", response.GetServers())
	}
	if !foundOffending {
		t.Fatalf("Servers = %+v, want vendor-oversized still listed so an operator can find and delete it", response.GetServers())
	}
	if !response.GetRegistryDegraded() {
		t.Fatal("RegistryDegraded = false, want true while the aggregate tool budget is over its cap")
	}
	if response.GetRegistryDegradationReason() != mcpRegistryOverBudgetNoticeMessage {
		t.Fatalf("RegistryDegradationReason = %q, want the fixed notice %q", response.GetRegistryDegradationReason(), mcpRegistryOverBudgetNoticeMessage)
	}
	// Never a synthetic "_registry"-named Unsupported entry: the
	// systemic over-budget condition is reported only through the
	// explicit RegistryDegraded/RegistryDegradationReason fields above.
	if _, present := findUnsupportedReason(response.GetUnsupported(), "_registry"); present {
		t.Fatalf("Unsupported = %+v, want no synthetic _registry entry", response.GetUnsupported())
	}

	// Delete works even while over budget: it targets one server
	// directly and does not depend on MCPRegistrySnapshot at all.
	if _, err := service.DeleteMcpServer(ctx, &turingv1.DeleteMcpServerRequest{ServerId: offending.Server.ID}); err != nil {
		t.Fatalf("DeleteMcpServer must still work while the aggregate is over budget: %v", err)
	}

	// Recovery: the oversized tool row cascade-deleted with its server,
	// so the aggregate is back under budget and a fresh list recovers
	// completely — tools restored, RegistryDegraded cleared.
	recovered, err := service.ListMcpServers(ctx, &turingv1.ListMcpServersRequest{})
	if err != nil {
		t.Fatalf("ListMcpServers after the offending server was deleted: %v", err)
	}
	if recovered.GetRegistryDegraded() {
		t.Fatalf("RegistryDegraded = true, want false once the aggregate is back under budget (reason: %q)", recovered.GetRegistryDegradationReason())
	}
	if recovered.GetRegistryDegradationReason() != "" {
		t.Fatalf("RegistryDegradationReason = %q, want empty once recovered", recovered.GetRegistryDegradationReason())
	}
	var recoveredHealthy *turingv1.McpServerDescriptor
	for _, server := range recovered.GetServers() {
		if server.GetServerId() == offending.Server.ID {
			t.Fatalf("vendor-oversized is still listed after being deleted: %+v", server)
		}
		if server.GetServerId() == healthy.Server.ID {
			recoveredHealthy = server
		}
	}
	if recoveredHealthy == nil {
		t.Fatal("vendor-healthy is missing after recovery")
	}
	if len(recoveredHealthy.GetTools()) != 1 || recoveredHealthy.GetTools()[0].GetToolName() != "vendor-healthy.tool" {
		t.Fatalf("vendor-healthy tools after recovery = %+v, want its one tool restored", recoveredHealthy.GetTools())
	}
}

// TestListMcpServersSucceedsAtExactAggregateBudgetBoundary proves the
// guard is not off-by-one: the registry's own enforced boundary
// (repository.MaxMCPRegistryToolBytes exactly, the largest total every
// write path already allows) must still list successfully, with tools
// intact and RegistryDegraded left false.
func TestListMcpServersSucceedsAtExactAggregateBudgetBoundary(t *testing.T) {
	service, repo := newRegistryTestService(t)
	ctx := context.Background()

	tool := discoveredToolAtExactSize(t, repository.MaxMCPRegistryToolBytes)
	if err := repo.UpsertTools(ctx, []repository.DiscoveredTool{tool}); err != nil {
		t.Fatalf("a snapshot at exactly MaxMCPRegistryToolBytes must not be refused: %v", err)
	}

	response, err := service.ListMcpServers(ctx, &turingv1.ListMcpServersRequest{})
	if err != nil {
		t.Fatalf("ListMcpServers at exactly the aggregate budget boundary must succeed: %v", err)
	}
	if response.GetRegistryDegraded() {
		t.Fatalf("RegistryDegraded = true, want false at the exact (in-budget) boundary (reason: %q)", response.GetRegistryDegradationReason())
	}
	if _, present := findUnsupportedReason(response.GetUnsupported(), "_registry"); present {
		t.Fatalf("Unsupported = %+v, want no _registry notice at the exact (in-budget) boundary", response.GetUnsupported())
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
