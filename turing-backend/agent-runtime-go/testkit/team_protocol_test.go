package testkit

import (
	"testing"

	"github.com/mcasillas17/TuringAgent/turing-backend/agent-runtime-go/internal/agent"
)

// RunWorker's GeneralAssistant honors the contract, so it always advertises
// it; a custom executor advertises only what its config opts into.
func TestTestkitWorkersAdvertiseTheTeamProtocolTheyHonor(t *testing.T) {
	if got := generalAssistantWorkerOptions(WorkerConfig{}, nil).TeamProtocolVersion; got != agent.TeamProtocolVersion {
		t.Fatalf("RunWorker team protocol = %d, want %d", got, agent.TeamProtocolVersion)
	}
	if got := executorWorkerOptions(WorkerConfig{}, nil).TeamProtocolVersion; got != 0 {
		t.Fatalf("custom executor without opting in = %d, want 0", got)
	}
	if got := executorWorkerOptions(WorkerConfig{TeamProtocolVersion: 1}, nil).TeamProtocolVersion; got != 1 {
		t.Fatalf("custom executor opting in = %d, want 1", got)
	}
}
