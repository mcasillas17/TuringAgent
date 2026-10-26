package agent

import (
	"testing"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"github.com/mcasillas17/TuringAgent/turing-backend/agent-runtime-go/internal/llm"
)

func localOpenAIEngine(t *testing.T) llm.Provider {
	t.Helper()
	provider, err := llm.NewLocal(llm.LocalOptions{
		Engine: llm.LocalEngineOpenAI, BaseURL: "http://host.docker.internal:8000/v1",
		ContextWindowTokens: 8192, MaxOutputTokens: 512,
	})
	if err != nil {
		t.Fatal(err)
	}
	return provider
}

func TestALocalJobMayRunOnALocalOpenAIEngine(t *testing.T) {
	job := &turingv1.AgentJob{ModelProvider: turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA}
	if err := validateEgressProvider(job, localOpenAIEngine(t)); err != nil {
		t.Fatalf("validateEgressProvider = %v, want a local job allowed", err)
	}
}

// A run consented to as remote must reach the destination its decision names;
// an engine on this machine is not one.
func TestARemoteJobIsRefusedOnALocalOpenAIEngine(t *testing.T) {
	job := &turingv1.AgentJob{ModelProvider: turingv1.ModelProvider_MODEL_PROVIDER_OPENAI_COMPATIBLE}
	if err := validateEgressProvider(job, localOpenAIEngine(t)); err == nil {
		t.Fatal("a remote job was allowed to run on the local engine")
	}
}
