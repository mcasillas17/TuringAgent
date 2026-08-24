package mcpregistry

import (
	"context"
	"fmt"
	"testing"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Direct registration, looped to the exact repository cap, must all
// succeed — the RPC layer must not be off-by-one against the repository's
// own boundary.
func TestRegisterMcpServerRPCAllowsExactlyTheCap(t *testing.T) {
	service, _ := newRegistryTestService(t)
	ctx := context.Background()

	for i := 0; i < repository.MaxNonBundledMCPServers; i++ {
		_, err := service.RegisterMcpServer(ctx, &turingv1.RegisterMcpServerRequest{
			Name: fmt.Sprintf("vendor-%d", i),
			Url:  fmt.Sprintf("http://vendor-%d:9000/mcp", i),
			Tier: turingv1.McpServerTier_MCP_SERVER_TIER_LOCAL_CONTAINER,
		})
		if err != nil {
			t.Fatalf("register #%d (within the cap) failed: %v", i, err)
		}
	}
}

// The (cap+1)th direct registration must be refused with ResourceExhausted
// — a caller can distinguish "the registry is full" from a validation
// error (InvalidArgument) or a name collision (AlreadyExists) — and must
// create no row.
func TestRegisterMcpServerRPCRefusesOneBeyondTheCapWithResourceExhausted(t *testing.T) {
	service, repo := newRegistryTestService(t)
	ctx := context.Background()

	for i := 0; i < repository.MaxNonBundledMCPServers; i++ {
		_, err := service.RegisterMcpServer(ctx, &turingv1.RegisterMcpServerRequest{
			Name: fmt.Sprintf("vendor-%d", i),
			Url:  fmt.Sprintf("http://vendor-%d:9000/mcp", i),
			Tier: turingv1.McpServerTier_MCP_SERVER_TIER_LOCAL_CONTAINER,
		})
		if err != nil {
			t.Fatalf("register #%d (within the cap) failed: %v", i, err)
		}
	}

	_, err := service.RegisterMcpServer(ctx, &turingv1.RegisterMcpServerRequest{
		Name: "one-too-many", Url: "http://one-too-many:9000/mcp",
		Tier: turingv1.McpServerTier_MCP_SERVER_TIER_LOCAL_CONTAINER,
	})
	if status.Code(err) != codes.ResourceExhausted {
		t.Fatalf("code = %v, want ResourceExhausted for the (cap+1)th registration", status.Code(err))
	}
	if _, gerr := repo.GetMCPServerByName(ctx, "one-too-many"); gerr != repository.ErrMCPServerNotFound {
		t.Fatalf("err = %v, want ErrMCPServerNotFound: the refused registration must create no row", gerr)
	}
}

// A file import hitting the same repository cap must record a bounded,
// per-entry Unsupported reason (not a hard, whole-call error) so the rest
// of the document's own report stays intact and deterministic — matching
// how every other repository refusal (bundled name, suppressed tombstone,
// tool collision) is already surfaced.
func TestImportJSONRefusesEntryBeyondTheCapAsUnsupported(t *testing.T) {
	service, repo := newRegistryTestService(t)
	ctx := context.Background()

	for i := 0; i < repository.MaxNonBundledMCPServers; i++ {
		if _, err := repo.RegisterMCPServer(ctx, repository.ImportedMCPServer{
			Name: fmt.Sprintf("vendor-%d", i),
			URL:  fmt.Sprintf("http://vendor-%d:9000/mcp", i),
			Tier: repository.MCPServerTierLocalContainer,
		}); err != nil {
			t.Fatalf("seed #%d (within the cap): %v", i, err)
		}
	}

	report, err := service.ImportJSON(ctx, []byte(`{
		"mcpServers": {
			"one-too-many": {"url": "https://one-too-many.example/mcp"}
		}
	}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Imported) != 0 {
		t.Fatalf("Imported = %v, want none: the registry is already at the cap", report.Imported)
	}
	if _, refused := report.Unsupported["one-too-many"]; !refused {
		t.Fatalf("Unsupported = %+v, want one-too-many refused", report.Unsupported)
	}
	if _, gerr := repo.GetMCPServerByName(ctx, "one-too-many"); gerr != repository.ErrMCPServerNotFound {
		t.Fatalf("err = %v, want ErrMCPServerNotFound: a refused entry must create no row", gerr)
	}
}
