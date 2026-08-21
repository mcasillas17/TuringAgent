package agent

import (
	"context"
	"testing"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
)

func TestLocalRemoteMCPRunExposesOnlyFrozenTools(t *testing.T) {
	registry, err := BuildToolRegistry(context.Background(), map[string]ToolLister{
		"system": &registryTestClient{tools: []map[string]any{{
			"name": "system.time", "inputSchema": map[string]any{"type": "object"},
		}}},
		"vendor": &registryTestClient{tools: []map[string]any{{
			"name": "vendor.lookup", "inputSchema": map[string]any{"type": "object"},
		}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	job := &turingv1.AgentJob{
		ModelProvider: turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA,
		SelectedTools: []string{"vendor/vendor.lookup"},
		EgressDecision: &turingv1.RunEgressDecision{
			DecisionId: "egress_remote_mcp",
		},
	}

	definitions, err := toolDefinitionsForJob(registry, job)
	if err != nil {
		t.Fatal(err)
	}
	if len(definitions) != 1 || definitions[0].Name != "vendor.lookup" {
		t.Fatalf("definitions = %+v, want only frozen vendor tool", definitions)
	}
}
