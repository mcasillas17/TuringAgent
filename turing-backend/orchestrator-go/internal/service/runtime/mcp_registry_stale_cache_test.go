package runtime

import (
	"context"
	"testing"

	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
)

func TestStaleDisabledRegistryToolIsDroppedFromWorkerSnapshot(t *testing.T) {
	h := newHarness(t)
	servers, err := h.repo.ListMCPServers(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, server := range servers {
		if server.Name == "custom" {
			if err := h.repo.SetMCPServerEnabled(context.Background(), server.ID, false); err != nil {
				t.Fatal(err)
			}
		}
	}
	capabilities := &registeredWorkerCapabilities{
		tools: map[string]struct{}{"custom/custom.inspect": {}},
	}
	discovered := []repository.DiscoveredTool{{
		ServerName: "custom", ToolName: "custom.inspect",
		SchemaJSON: `{"type":"object"}`, Policy: "approval_required",
	}}

	filteredCapabilities, filteredTools, err := h.service.filterRegisteredWorkerTools(
		context.Background(), capabilities, discovered,
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(filteredCapabilities.tools) != 0 || len(filteredTools) != 0 {
		t.Fatalf("stale snapshot survived: capabilities=%v tools=%v", filteredCapabilities.tools, filteredTools)
	}
}
