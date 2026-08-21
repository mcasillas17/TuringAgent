package worker

import (
	"context"
	"errors"
	"testing"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
)

func TestMCPRegistryChangeInvalidatesDiscoveryBeforeUpdatingCapabilities(t *testing.T) {
	executor := &registryRefreshExecutor{}
	worker := New(Options{
		WorkerID:          "worker-refresh",
		AgentID:           turingv1.AgentId_AGENT_ID_GENERAL_ASSISTANT,
		MaxConcurrentRuns: 2,
		DiscoverTools: func(context.Context) ([]*turingv1.DiscoveredTool, error) {
			if !executor.invalidated {
				return nil, errors.New("registry was not invalidated before discovery")
			}
			return []*turingv1.DiscoveredTool{{
				ServerName: "vendor", ToolName: "vendor.lookup",
			}}, nil
		},
	}, nil, executor)

	update, err := worker.refreshMCPRegistry(context.Background(), &turingv1.RuntimeMcpRegistryChanged{
		RegistrationId: "registration-refresh",
	})
	if err != nil {
		t.Fatal(err)
	}
	capabilities := update.GetWorkerCapabilitiesUpdated()
	if capabilities.GetRegistrationId() != "registration-refresh" ||
		len(capabilities.GetCapabilities().GetTools()) != 1 ||
		capabilities.GetCapabilities().GetTools()[0].GetToolName() != "vendor.lookup" {
		t.Fatalf("capabilities update = %+v", capabilities)
	}
}

type registryRefreshExecutor struct {
	invalidated bool
}

func (e *registryRefreshExecutor) Execute(
	context.Context,
	*turingv1.AgentJob,
	func(*turingv1.RuntimeUpdate) error,
) error {
	return nil
}

func (e *registryRefreshExecutor) InvalidateToolRegistry() {
	e.invalidated = true
}
