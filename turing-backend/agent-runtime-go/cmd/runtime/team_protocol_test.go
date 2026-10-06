package main

import (
	"testing"

	"github.com/mcasillas17/TuringAgent/turing-backend/agent-runtime-go/internal/agent"
	"github.com/mcasillas17/TuringAgent/turing-backend/agent-runtime-go/internal/config"
	"github.com/mcasillas17/TuringAgent/turing-backend/agent-runtime-go/internal/mcp"
	"github.com/mcasillas17/TuringAgent/turing-backend/agent-runtime-go/internal/orchestrator"
	"github.com/mcasillas17/TuringAgent/turing-backend/agent-runtime-go/internal/tools"
)

// The production worker runs GeneralAssistant, which honors the specialist-job
// contract, so it advertises that protocol.
func TestTheRuntimeWorkerAdvertisesTheTeamProtocol(t *testing.T) {
	options := workerOptions(config.Config{WorkerID: "worker-1", MaxConcurrentRuns: 1}, nil)
	if options.TeamProtocolVersion != agent.TeamProtocolVersion {
		t.Fatalf("team protocol version = %d, want %d", options.TeamProtocolVersion, agent.TeamProtocolVersion)
	}
}

// GeneralAssistant delegates through the orchestrator connection: without the
// team client it would neither advertise team.delegate nor offer it.
func TestTheRuntimeDelegatesThroughTheOrchestrator(t *testing.T) {
	client := orchestrator.New(nil, "token")
	toolset := generalAssistantTools(config.Config{}, client, &tools.Runner{})
	if toolset.Team != mcp.TeamRPC(client) {
		t.Fatalf("team client = %v, want the orchestrator client", toolset.Team)
	}
}
