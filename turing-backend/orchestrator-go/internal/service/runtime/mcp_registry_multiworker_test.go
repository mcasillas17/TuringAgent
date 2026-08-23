package runtime

import (
	"context"
	"testing"
	"time"

	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
)

func TestRegistryNotificationPrunesDisabledToolsBeforeWorkerRefreshes(t *testing.T) {
	h := newHarness(t)
	servers, err := h.repo.ListMCPServers(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var customID string
	for _, server := range servers {
		if server.Name == "custom" {
			customID = server.ID
		}
	}
	if customID == "" {
		t.Fatal("custom test server is missing")
	}
	if err := h.repo.UpsertTools(context.Background(), []repository.DiscoveredTool{{
		ServerName: "custom", ToolName: "custom.inspect",
		SchemaJSON: `{"type":"object"}`, Policy: "approval_required",
	}}); err != nil {
		t.Fatal(err)
	}
	workers := []*worker{
		{
			commands: make(chan workerCommand, 1), done: make(chan struct{}),
			registrationID: "registration-one", assignments: map[string]assignment{},
			lastHeartbeat: time.Now().UTC(),
			capabilities: &registeredWorkerCapabilities{
				tools: map[string]struct{}{"custom/custom.inspect": {}},
			},
		},
		{
			commands: make(chan workerCommand, 1), done: make(chan struct{}),
			registrationID: "registration-two", assignments: map[string]assignment{},
			lastHeartbeat: time.Now().UTC(),
			capabilities: &registeredWorkerCapabilities{
				tools: map[string]struct{}{"custom/custom.inspect": {}},
			},
		},
	}
	originalCapabilities := []*registeredWorkerCapabilities{
		workers[0].capabilities,
		workers[1].capabilities,
	}
	h.service.mu.Lock()
	h.service.workers["worker-one"] = workers[0]
	h.service.workers["worker-two"] = workers[1]
	h.service.mu.Unlock()
	if err := h.repo.SetMCPServerEnabled(context.Background(), customID, false); err != nil {
		t.Fatal(err)
	}

	if err := h.service.NotifyMCPRegistryChanged(context.Background()); err != nil {
		t.Fatal(err)
	}
	for index, connected := range workers {
		if _, stale := connected.capabilities.tools["custom/custom.inspect"]; stale {
			t.Fatalf("worker %d retained disabled tool before refresh", index)
		}
		if _, preserved := originalCapabilities[index].tools["custom/custom.inspect"]; !preserved {
			t.Fatalf("worker %d mutated published capability map in place", index)
		}
	}
}
