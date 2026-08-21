package mcpregistry

import (
	"testing"

	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
)

func TestLocalContainerTransportIsBoundedAndRejectsRedirects(t *testing.T) {
	service := New(nil, nil, nil)
	client := service.clientFor(repository.MCPServerRecord{
		Tier: repository.MCPServerTierLocalContainer,
	})
	if client.Timeout <= 0 {
		t.Fatal("local-container client has no whole-request timeout")
	}
	if client.CheckRedirect == nil {
		t.Fatal("local-container client accepts redirects")
	}
}
