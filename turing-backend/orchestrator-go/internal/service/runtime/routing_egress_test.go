package runtime

import (
	"testing"

	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
)

// egressAwareCapabilities is a worker that can validate the current decision.
func egressAwareCapabilities(version int) *registeredWorkerCapabilities {
	return &registeredWorkerCapabilities{
		agentIDs: map[string]struct{}{"general_assistant": {}},
		models: []registeredModelCapability{{
			provider: "ollama", model: "llama3.2", maxContextTokens: 8192,
		}},
		tools:                       map[string]struct{}{"vendor/vendor.lookup": {}},
		externalAgentCredentialRefs: map[string]struct{}{},
		maxConcurrentRuns:           1,
		remoteEgressDecisionVersion: version,
	}
}

// localModelRemoteToolRoute is a run whose model stays on the machine and whose
// tools do not. It carries a frozen egress decision, so the worker that runs it
// has to be one that validates decisions.
func localModelRemoteToolRoute() repository.RoutingRequirements {
	return repository.RoutingRequirements{
		AgentID:              "general_assistant",
		ModelProvider:        "ollama",
		Model:                "llama3.2",
		SelectedTools:        []string{"vendor/vendor.lookup"},
		RemoteEgressDecision: true,
	}
}

// The gate is on the decision, not on where the model lives. A worker built
// before the current decision existed would claim this run and fail it at
// execution time; refusing it here is what turns that terminal failure into a
// job that waits for a worker which can honour it.
func TestRoutingRefusesAnEgressBearingLocalRunOnAStaleWorker(t *testing.T) {
	// The literal pre-memory number, not the constant, so the refusal keeps
	// standing after the constant moves again.
	stale := egressAwareCapabilities(2)
	if workerCapabilitiesSupportRoute(stale, localModelRemoteToolRoute()) {
		t.Fatal("a literal-v2 worker was offered a run carrying an egress decision")
	}
}

func TestRoutingAcceptsAnEgressBearingLocalRunOnACurrentWorker(t *testing.T) {
	current := egressAwareCapabilities(repository.RunEgressDecisionVersion)
	if !workerCapabilitiesSupportRoute(current, localModelRemoteToolRoute()) {
		t.Fatal("a current worker was refused a run carrying an egress decision")
	}
}

// A run that never leaves the machine is unaffected: the gate must not become
// a version floor on ordinary local work.
func TestRoutingStillAcceptsAPurelyLocalRunOnAStaleWorker(t *testing.T) {
	stale := egressAwareCapabilities(2)
	route := localModelRemoteToolRoute()
	route.RemoteEgressDecision = false
	route.SelectedTools = nil
	if !workerCapabilitiesSupportRoute(stale, route) {
		t.Fatal("a purely local run was refused on a worker that cannot validate decisions")
	}
}
