package tests

import (
	"strings"
	"testing"

	runtimetestkit "github.com/mcasillas17/TuringAgent/turing-backend/agent-runtime-go/testkit"
	orchestratortestkit "github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/testkit"
)

// The orchestrator names its default local model and the runtime advertises
// one; if they resolve differently from the same .env, a send names a model no
// worker serves and waits.
func TestBothServicesResolveTheSameLocalModel(t *testing.T) {
	for name, env := range map[string]map[string]string{
		"nothing set": {},
		"an old .env": {"OLLAMA_MODEL": "llama3.2", "OLLAMA_CONTEXT_WINDOW_TOKENS": "16384"},
		"both names":  {"OLLAMA_MODEL": "llama3.2", "TURING_LOCAL_MODEL": "deepseek-chat", "TURING_LOCAL_CONTEXT_WINDOW_TOKENS": "65536"},
	} {
		t.Run(name, func(t *testing.T) {
			orchestratorEnv := map[string]string{
				"TURING_CLIENT_API_KEY": "client-key", "TURING_RUNTIME_TOKEN": "runtime-token",
				"TURING_APPROVAL_CONSUMER_TOKEN": "approval-consumer-token", "MCP_FILES_ENABLED": "true",
				"TURING_APPROVAL_JWT_SECRET": "approval-secret", "TURING_EGRESS_SIGNING_SECRET": "egress-secret",
				"TURING_CURSOR_HMAC_SECRET": strings.Repeat("ab", 32),
			}
			runtimeEnv := map[string]string{"TURING_RUNTIME_TOKEN": "runtime-token"}
			for key, value := range env {
				orchestratorEnv[key], runtimeEnv[key] = value, value
			}
			orchestratorModel, orchestratorWindow, err := orchestratortestkit.ResolveLocalModel(orchestratorEnv)
			if err != nil {
				t.Fatal(err)
			}
			runtimeModel, runtimeWindow, err := runtimetestkit.ResolveLocalModel(runtimeEnv)
			if err != nil {
				t.Fatal(err)
			}
			if orchestratorModel != runtimeModel || orchestratorWindow != runtimeWindow {
				t.Fatalf("orchestrator %q/%d, runtime %q/%d", orchestratorModel, orchestratorWindow, runtimeModel, runtimeWindow)
			}
		})
	}
}
