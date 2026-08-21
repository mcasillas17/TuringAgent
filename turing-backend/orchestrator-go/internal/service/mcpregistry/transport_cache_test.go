package mcpregistry

import (
	"testing"

	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
)

func TestGuardedTransportsAreReusedByTier(t *testing.T) {
	service := New(nil, nil, nil)
	local := repository.MCPServerRecord{Tier: repository.MCPServerTierLocalContainer}
	remote := repository.MCPServerRecord{Tier: repository.MCPServerTierRemoteURL}
	firstLocal := service.clientFor(local)
	secondLocal := service.clientFor(local)
	if firstLocal != secondLocal {
		t.Fatal("local MCP transport was rebuilt instead of reused")
	}
	firstRemote := service.clientFor(remote)
	secondRemote := service.clientFor(remote)
	if firstRemote != secondRemote {
		t.Fatal("remote MCP transport was rebuilt instead of reused")
	}
}
