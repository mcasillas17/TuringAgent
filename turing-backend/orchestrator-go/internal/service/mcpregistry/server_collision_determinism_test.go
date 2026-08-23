package mcpregistry

import (
	"context"
	"testing"

	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
)

// When a single mcp.json document has two brand-new entries that both
// claim the same tool name, ImportJSON must process document.Servers in
// sorted name order — never Go's randomized map iteration order — so the
// same one of the two always wins the repository's inter-server collision
// check (see replaceServerToolsTx) and the other is always refused. This
// runs the same document many times, each against a fresh repository, to
// prove the winner is the lexicographically first server name every time
// rather than a coin flip that would only occasionally disagree with
// itself.
func TestImportJSONServerCollisionWinnerIsDeterministicAcrossRepeatedImports(t *testing.T) {
	const document = `{
		"mcpServers": {
			"beta": {
				"url": "https://beta.example/mcp",
				"tools": [{"name": "shared.tool", "inputSchema": {"type": "object"}}]
			},
			"alpha": {
				"url": "https://alpha.example/mcp",
				"tools": [{"name": "shared.tool", "inputSchema": {"type": "object"}}]
			}
		}
	}`
	const trials = 25
	for trial := 0; trial < trials; trial++ {
		service, repo := newRegistryTestService(t)
		ctx := context.Background()

		report, err := service.ImportJSON(ctx, []byte(document))
		if err != nil {
			t.Fatalf("trial %d: %v", trial, err)
		}
		if len(report.Imported) != 1 || report.Imported[0] != "alpha" {
			t.Fatalf("trial %d: Imported = %v, want [alpha] (the lexicographically first name) to consistently win the collision", trial, report.Imported)
		}
		reason, refused := report.Unsupported["beta"]
		if !refused || reason == "" {
			t.Fatalf("trial %d: Unsupported = %+v, want beta refused with a non-empty reason", trial, report.Unsupported)
		}

		if _, err := repo.GetMCPServerByName(ctx, "beta"); err != repository.ErrMCPServerNotFound {
			t.Fatalf("trial %d: err = %v, want ErrMCPServerNotFound: the losing collision must leave no row", trial, err)
		}

		alpha, err := repo.GetMCPServerByName(ctx, "alpha")
		if err != nil {
			t.Fatalf("trial %d: %v", trial, err)
		}
		tools, err := repo.ListMCPServerTools(ctx, alpha.ID)
		if err != nil {
			t.Fatalf("trial %d: %v", trial, err)
		}
		if len(tools) != 1 || tools[0].Name != "shared.tool" || !tools[0].Present {
			t.Fatalf("trial %d: tools = %+v, want alpha to own shared.tool", trial, tools)
		}
	}
}
