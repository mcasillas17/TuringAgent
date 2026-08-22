package runtime

import (
	"context"
	"testing"

	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
)

func TestBundledToolCanReenterAfterLastWorkerDisconnects(t *testing.T) {
	h := newHarness(t)
	tool := repository.DiscoveredTool{
		ServerName: "files", ToolName: "files.create",
		SchemaJSON: `{"type":"object"}`, Policy: "approval_required",
	}
	if err := h.repo.UpsertTools(context.Background(), []repository.DiscoveredTool{tool}); err != nil {
		t.Fatal(err)
	}
	if err := h.repo.UpsertTools(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	capabilities := &registeredWorkerCapabilities{
		tools: map[string]struct{}{"files/files.create": {}},
	}
	filteredCapabilities, filtered, err := h.service.filterRegisteredWorkerTools(
		context.Background(), capabilities, []repository.DiscoveredTool{tool},
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(filteredCapabilities.tools) != 1 || len(filtered) != 1 {
		t.Fatalf("bundled reconnect was filtered: %v %+v", filteredCapabilities.tools, filtered)
	}
}
