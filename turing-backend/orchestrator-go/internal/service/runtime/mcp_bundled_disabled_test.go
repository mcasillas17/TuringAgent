package runtime

import (
	"context"
	"testing"

	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
)

func TestDisabledBundledToolIsFilteredFromWorkerRefresh(t *testing.T) {
	h := newHarness(t)
	if err := h.repo.UpsertTools(context.Background(), []repository.DiscoveredTool{{
		ServerName: "files", ToolName: "files.create",
		SchemaJSON: `{"type":"object"}`, Policy: "approval_required",
	}}); err != nil {
		t.Fatal(err)
	}
	servers, err := h.repo.ListMCPServers(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var filesID string
	for _, server := range servers {
		if server.Name == "files" {
			filesID = server.ID
		}
	}
	if err := h.repo.SetMCPToolPolicy(context.Background(), filesID, "files.create", "disabled"); err != nil {
		t.Fatal(err)
	}
	capabilities := &registeredWorkerCapabilities{
		tools: map[string]struct{}{"files/files.create": {}},
	}
	discovered := []repository.DiscoveredTool{{
		ServerName: "files", ToolName: "files.create",
		SchemaJSON: `{"type":"object"}`, Policy: "approval_required",
	}}
	filteredCapabilities, filtered, err := h.service.filterRegisteredWorkerTools(
		context.Background(), capabilities, discovered,
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(filteredCapabilities.tools) != 0 || len(filtered) != 0 {
		t.Fatalf("disabled bundled tool survived refresh: %v %+v", filteredCapabilities.tools, filtered)
	}
}
