package config

import (
	"strings"
	"testing"
)

func TestLoadFromEnvDoesNotUseLegacyHTTPOrchestratorBaseURL(t *testing.T) {
	cfg, err := LoadFromEnv(mapEnv(map[string]string{
		"TURING_INTERNAL_TOKEN":          "internal",
		"ORCHESTRATOR_INTERNAL_BASE_URL": "http://legacy-orchestrator:3001/internal",
	}))
	if err != nil {
		t.Fatalf("LoadFromEnv failed: %v", err)
	}
	if cfg.OrchestratorGRPCAddr != "turing-orchestrator:3001" {
		t.Fatalf("expected default gRPC address without legacy HTTP fallback, got %q", cfg.OrchestratorGRPCAddr)
	}
}

func TestLoadFromEnvUsesExplicitOrchestratorGRPCAddress(t *testing.T) {
	cfg, err := LoadFromEnv(mapEnv(map[string]string{
		"TURING_INTERNAL_TOKEN":  "internal",
		"ORCHESTRATOR_GRPC_ADDR": "orchestrator.internal:3001",
	}))
	if err != nil {
		t.Fatalf("LoadFromEnv failed: %v", err)
	}
	if cfg.OrchestratorGRPCAddr != "orchestrator.internal:3001" {
		t.Fatalf("expected explicit gRPC address, got %q", cfg.OrchestratorGRPCAddr)
	}
}

func TestLoadFromEnvDefaultsEmptyMaxToolCallsPerRun(t *testing.T) {
	cfg, err := LoadFromEnv(mapEnv(map[string]string{
		"TURING_INTERNAL_TOKEN": "internal",
	}))
	if err != nil {
		t.Fatalf("LoadFromEnv failed: %v", err)
	}
	if cfg.MaxToolCallsPerRun != 10 {
		t.Fatalf("MaxToolCallsPerRun = %d, want default 10", cfg.MaxToolCallsPerRun)
	}
}

func TestLoadFromEnvRejectsNonPositiveMaxToolCallsPerRun(t *testing.T) {
	for _, value := range []string{"0", "-1"} {
		t.Run(value, func(t *testing.T) {
			_, err := LoadFromEnv(mapEnv(map[string]string{
				"TURING_INTERNAL_TOKEN":         "internal",
				"TURING_MAX_TOOL_CALLS_PER_RUN": value,
			}))

			if err == nil || !strings.Contains(err.Error(), "TURING_MAX_TOOL_CALLS_PER_RUN") ||
				!strings.Contains(err.Error(), "greater than 0") {
				t.Fatalf("LoadFromEnv error = %v, want clear positive-value validation", err)
			}
		})
	}
}

func mapEnv(values map[string]string) func(string) string {
	return func(name string) string {
		return values[name]
	}
}
