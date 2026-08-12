package agent_test

import (
	"context"
	"testing"

	"github.com/mcasillas17/TuringAgent/turing-backend/agent-runtime-go/internal/agent"
)

func TestBuildToolRegistryAcceptsExportedToolListerMap(t *testing.T) {
	var servers map[string]agent.ToolLister

	registry, err := agent.BuildToolRegistry(context.Background(), servers)
	if err != nil {
		t.Fatalf("BuildToolRegistry returned error: %v", err)
	}
	if registry == nil {
		t.Fatal("BuildToolRegistry returned nil registry")
	}
}
