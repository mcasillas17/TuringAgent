package worker

import (
	"context"
	"fmt"
	"testing"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
)

// The team protocol is opt-in like the egress-decision version: a worker
// claims it only when its executor honors the contract, and it says so both
// when it connects and when its capabilities are refreshed.
func TestWorkerAdvertisesItsTeamProtocolVersion(t *testing.T) {
	for _, version := range []int32{0, 1} {
		t.Run(fmt.Sprintf("version %d", version), func(t *testing.T) {
			stream := newFakeStream()
			worker := New(Options{
				WorkerID: "worker-team", AgentID: turingv1.AgentId_AGENT_ID_GENERAL_ASSISTANT, MaxConcurrentRuns: 1,
				TeamProtocolVersion: version,
				Models: []*turingv1.ModelCapability{{
					Provider: turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA, Model: "qwen2.5:7b", MaxContextTokens: 32768,
				}},
				NewRegistrationID: func() (string, error) { return "registration-team", nil },
				DiscoverTools: func(context.Context) ([]*turingv1.DiscoveredTool, error) {
					return []*turingv1.DiscoveredTool{{ServerName: "system", ToolName: "system.time"}}, nil
				},
			}, &fakeRuntimeClient{stream: stream}, &registryRefreshExecutor{})
			ctx, cancel := context.WithCancel(context.Background())
			done := make(chan error, 1)
			go func() { done <- worker.Run(ctx) }()

			ready := nextSent(t, stream).GetWorkerReady()
			cancel()
			<-done
			if got := ready.GetCapabilities().GetTeamProtocolVersion(); got != version {
				t.Fatalf("ready team_protocol_version = %d, want %d", got, version)
			}

			update, err := worker.refreshMCPRegistry(context.Background(), &turingv1.RuntimeMcpRegistryChanged{RegistrationId: "registration-team"})
			if err != nil {
				t.Fatal(err)
			}
			if got := update.GetWorkerCapabilitiesUpdated().GetCapabilities().GetTeamProtocolVersion(); got != version {
				t.Fatalf("refreshed team_protocol_version = %d, want %d", got, version)
			}
		})
	}
}
