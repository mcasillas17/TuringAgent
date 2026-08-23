package mcpregistry

import (
	"context"
	"fmt"
	"testing"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
	"google.golang.org/protobuf/proto"
)

// TestListMcpServersStaysUnderGRPCMessageSizeAfterRepeatedDisjointRediscovery
// is the end-to-end, service-level counterpart of the repository package's
// own TestReplaceServerToolsTxRepeatedDisjointRediscoveryEventuallyExceedsBudget:
// it repeatedly rediscovers a single server's tools through the real
// RecordDiscovery path (the one SetMcpServerEnabled/worker rediscovery
// itself uses), each round naming a tool disjoint from every previous
// round's, until the registry-wide aggregate — which counts every row,
// present and withdrawn, not just currently-present ones — refuses a
// round. It then proves ListMcpServers still succeeds at that last
// successfully committed state and that its real, marshaled response
// (server descriptors and tool descriptors built the same way a real
// client would receive them) stays comfortably under the 4 MiB gRPC
// message cap: the aggregate budget bounds the byte total, not merely a
// count, specifically so the wire size stays bounded even as withdrawn
// history accumulates.
func TestListMcpServersStaysUnderGRPCMessageSizeAfterRepeatedDisjointRediscovery(t *testing.T) {
	service, repo := newRegistryTestService(t)
	ctx := context.Background()

	server, err := repo.RegisterMCPServer(ctx, repository.ImportedMCPServer{
		Name: "vendor", URL: "http://vendor:9000/mcp", Tier: repository.MCPServerTierLocalContainer,
	})
	if err != nil {
		t.Fatal(err)
	}

	const roundSize = 50_000
	const maxRounds = 10
	round := 0
	var lastErr error
	for ; round < maxRounds; round++ {
		name := fmt.Sprintf("round-%d", round)
		lastErr = service.RecordDiscovery(ctx, server.Server.ID, []DiscoveredTool{{
			Name:       name,
			SchemaJSON: numberArraySchemaJSON(name, roundSize),
		}})
		if lastErr != nil {
			break
		}
	}
	if lastErr == nil {
		t.Fatal("test setup is broken: expected the aggregate budget to eventually refuse a round")
	}

	response, err := service.ListMcpServers(ctx, &turingv1.ListMcpServersRequest{})
	if err != nil {
		t.Fatalf("ListMcpServers at the last successfully committed round must still succeed: %v", err)
	}
	encoded, err := proto.Marshal(response)
	if err != nil {
		t.Fatalf("marshal ListMcpServersResponse: %v", err)
	}
	assertUnderGRPCMessageSizeWithMargin(t, "ListMcpServersResponse after repeated disjoint rediscovery", encoded, 512*1024)
}
