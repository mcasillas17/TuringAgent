package main

import (
	"testing"

	"github.com/mcasillas17/TuringAgent/turing-backend/agent-runtime-go/internal/agent"
	"github.com/mcasillas17/TuringAgent/turing-backend/agent-runtime-go/internal/config"
)

// The production worker runs GeneralAssistant, which honors the specialist-job
// contract, so it advertises that protocol.
func TestTheRuntimeWorkerAdvertisesTheTeamProtocol(t *testing.T) {
	options := workerOptions(config.Config{WorkerID: "worker-1", MaxConcurrentRuns: 1}, nil)
	if options.TeamProtocolVersion != agent.TeamProtocolVersion {
		t.Fatalf("team protocol version = %d, want %d", options.TeamProtocolVersion, agent.TeamProtocolVersion)
	}
}
