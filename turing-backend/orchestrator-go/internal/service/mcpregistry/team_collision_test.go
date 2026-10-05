package mcpregistry

import (
	"context"
	"strings"
	"testing"

	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
)

// The MCP settings say why delegation is off, on the server that turns it off.
func TestServerDescriptorExplainsATeamNameCollision(t *testing.T) {
	present := []repository.MCPServerTool{{Name: "team.delegate", Present: true, SchemaJSON: `{}`}}
	withdrawn := []repository.MCPServerTool{{Name: "team.delegate", SchemaJSON: `{}`}}
	for _, test := range []struct {
		name   string
		server repository.MCPServerRecord
		tools  []repository.MCPServerTool
		want   string
	}{
		{"server named Team", repository.MCPServerRecord{Name: "Team", StatusError: "timeout"}, nil, repository.TeamServerCollisionReason},
		{"vendor team tool", repository.MCPServerRecord{Name: "vendor", StatusError: "timeout"}, present, repository.TeamToolCollisionReason("vendor", "team.delegate")},
		{"withdrawn team tool", repository.MCPServerRecord{Name: "vendor", StatusError: "timeout"}, withdrawn, "timeout"},
		{"ordinary server", repository.MCPServerRecord{Name: "vendor", StatusError: "timeout"}, nil, "timeout"},
	} {
		t.Run(test.name, func(t *testing.T) {
			descriptor, err := buildServerDescriptor(test.server, test.tools)
			if err != nil {
				t.Fatal(err)
			}
			if descriptor.GetStatusMessage() != test.want {
				t.Fatalf("status message = %q, want %q", descriptor.GetStatusMessage(), test.want)
			}
		})
	}
}

// A user server named team in any case keeps its row, but nothing calls it.
func TestCallToolRefusesAServerNamedTeam(t *testing.T) {
	_, repo := newRegistryTestService(t)
	service := New(repo, nil, nil)
	result, err := repo.ImportMCPServer(context.Background(), repository.ImportedMCPServer{
		Name: "Team", URL: "http://team.example.test/mcp", Tier: repository.MCPServerTierLocalContainer,
		Tools: []repository.MCPServerTool{{Name: "lookup", Policy: "safe", SchemaJSON: `{"type":"object"}`}},
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.CallTool(context.Background(), CallInput{ServerID: result.Server.ID, RunID: "run_any", ToolName: "lookup"})
	if err == nil || !strings.Contains(err.Error(), "named `team`") {
		t.Fatalf("CallTool on a server named Team = %v, want the collision refusal", err)
	}
}
