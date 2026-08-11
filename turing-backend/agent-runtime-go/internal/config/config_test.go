package config

import (
	"math"
	"strconv"
	"strings"
	"testing"
	"time"
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

func TestLoadFromEnvDefaultsTimeouts(t *testing.T) {
	cfg, err := LoadFromEnv(mapEnv(map[string]string{
		"TURING_INTERNAL_TOKEN": "internal",
	}))
	if err != nil {
		t.Fatalf("LoadFromEnv failed: %v", err)
	}
	if cfg.ModelTimeout != 120*time.Second {
		t.Fatalf("ModelTimeout = %v, want 120s", cfg.ModelTimeout)
	}
	if cfg.ToolTimeout != 30*time.Second {
		t.Fatalf("ToolTimeout = %v, want 30s", cfg.ToolTimeout)
	}
	if cfg.ApprovalTimeout != 65*time.Second {
		t.Fatalf("ApprovalTimeout = %v, want 65s", cfg.ApprovalTimeout)
	}
	if cfg.TotalToolTimeout != 180*time.Second {
		t.Fatalf("TotalToolTimeout = %v, want 180s", cfg.TotalToolTimeout)
	}
}

func TestLoadFromEnvRequiresTotalToolTimeoutBeyondApprovalWait(t *testing.T) {
	_, err := LoadFromEnv(mapEnv(map[string]string{
		"TURING_INTERNAL_TOKEN":        "internal",
		"TURING_APPROVAL_TIMEOUT_MS":   "1000",
		"TURING_TOOL_TOTAL_TIMEOUT_MS": "1000",
	}))
	if err == nil || !strings.Contains(err.Error(), "TURING_TOOL_TOTAL_TIMEOUT_MS") {
		t.Fatalf("LoadFromEnv error = %v, want total-tool timeout safety error", err)
	}
}

func TestLoadFromEnvRequiresTotalToolBudgetForApprovalLifecycle(t *testing.T) {
	_, err := LoadFromEnv(mapEnv(map[string]string{
		"TURING_INTERNAL_TOKEN":        "internal",
		"TURING_TOOL_TIMEOUT_MS":       "30000",
		"TURING_APPROVAL_TIMEOUT_MS":   "65000",
		"TURING_TOOL_TOTAL_TIMEOUT_MS": "70000",
	}))
	if err == nil || !strings.Contains(err.Error(), "TURING_TOOL_TOTAL_TIMEOUT_MS") {
		t.Fatalf("LoadFromEnv error = %v, want complete approval lifecycle budget error", err)
	}
}

func TestLoadFromEnvRejectsNonPositiveTimeouts(t *testing.T) {
	for _, test := range []struct {
		name string
		env  string
	}{
		{name: "model zero", env: "TURING_MODEL_TIMEOUT_MS"},
		{name: "tool zero", env: "TURING_TOOL_TIMEOUT_MS"},
		{name: "approval zero", env: "TURING_APPROVAL_TIMEOUT_MS"},
		{name: "total tool zero", env: "TURING_TOOL_TOTAL_TIMEOUT_MS"},
	} {
		t.Run(test.name, func(t *testing.T) {
			for _, value := range []string{"0", "-1"} {
				_, err := LoadFromEnv(mapEnv(map[string]string{
					"TURING_INTERNAL_TOKEN": "internal",
					test.env:                value,
				}))
				if err == nil || !strings.Contains(err.Error(), test.env) ||
					!strings.Contains(err.Error(), "greater than 0") {
					t.Fatalf("LoadFromEnv(%s=%s) error = %v, want clear positive-value validation", test.env, value, err)
				}
			}
		})
	}
}

func TestLoadFromEnvAcceptsLargestRepresentableTimeoutMilliseconds(t *testing.T) {
	maxMilliseconds := int64(math.MaxInt64 / int64(time.Millisecond))
	value := strconv.FormatInt(maxMilliseconds, 10)
	for _, env := range []string{
		"TURING_MODEL_TIMEOUT_MS",
		"TURING_TOOL_TOTAL_TIMEOUT_MS",
	} {
		t.Run(env, func(t *testing.T) {
			cfg, err := LoadFromEnv(mapEnv(map[string]string{
				"TURING_INTERNAL_TOKEN": "internal",
				env:                     value,
			}))
			if err != nil {
				t.Fatalf("LoadFromEnv(%s=%s) failed: %v", env, value, err)
			}
			got := map[string]time.Duration{
				"TURING_MODEL_TIMEOUT_MS":      cfg.ModelTimeout,
				"TURING_TOOL_TIMEOUT_MS":       cfg.ToolTimeout,
				"TURING_APPROVAL_TIMEOUT_MS":   cfg.ApprovalTimeout,
				"TURING_TOOL_TOTAL_TIMEOUT_MS": cfg.TotalToolTimeout,
			}[env]
			want := time.Duration(maxMilliseconds) * time.Millisecond
			if got != want {
				t.Fatalf("%s duration = %v, want %v", env, got, want)
			}
		})
	}
}

func TestLoadFromEnvRejectsTimeoutMillisecondsThatOverflowDuration(t *testing.T) {
	maxMilliseconds := int64(math.MaxInt64 / int64(time.Millisecond))
	values := []string{
		strconv.FormatInt(maxMilliseconds+1, 10),
		"9223372036854775808",
	}
	for _, value := range values {
		for _, env := range []string{
			"TURING_MODEL_TIMEOUT_MS",
			"TURING_TOOL_TIMEOUT_MS",
			"TURING_APPROVAL_TIMEOUT_MS",
			"TURING_TOOL_TOTAL_TIMEOUT_MS",
		} {
			t.Run(env+"/"+value, func(t *testing.T) {
				_, err := LoadFromEnv(mapEnv(map[string]string{
					"TURING_INTERNAL_TOKEN": "internal",
					env:                     value,
				}))
				if err == nil || !strings.Contains(err.Error(), env) ||
					!strings.Contains(err.Error(), "maximum") {
					t.Fatalf("LoadFromEnv(%s=%s) error = %v, want clear overflow validation", env, value, err)
				}
			})
		}
	}
}

func TestLoadFromEnvRejectsInvalidEndpointURLs(t *testing.T) {
	for _, env := range []string{
		"OLLAMA_BASE_URL",
		"OPENAI_BASE_URL",
		"MCP_SYSTEM_BASE_URL",
		"MCP_FILES_BASE_URL",
	} {
		for _, value := range []string{
			"ftp://provider.example/v1",
			"http:///missing-host",
			"http://:8080/missing-host",
			"provider.example/v1",
			"https://provider.example/v1?tenant=one",
			"https://provider.example/v1#fragment",
		} {
			t.Run(env+"/"+value, func(t *testing.T) {
				_, err := LoadFromEnv(mapEnv(map[string]string{
					"TURING_INTERNAL_TOKEN": "internal",
					env:                     value,
				}))

				if err == nil || !strings.Contains(err.Error(), env) ||
					!strings.Contains(err.Error(), "absolute http or https URL with a non-empty host") {
					t.Fatalf("LoadFromEnv(%s=%q) error = %v, want env-specific URL validation", env, value, err)
				}
			})
		}
	}
}

func TestLoadFromEnvAcceptsHTTPAndHTTPSEndpointURLs(t *testing.T) {
	for _, env := range []string{
		"OLLAMA_BASE_URL",
		"OPENAI_BASE_URL",
		"MCP_SYSTEM_BASE_URL",
		"MCP_FILES_BASE_URL",
	} {
		for _, value := range []string{
			"http://provider.example/v1",
			"https://provider.example/v1",
			"HTTP://provider.example/v1",
		} {
			t.Run(env+"/"+value, func(t *testing.T) {
				_, err := LoadFromEnv(mapEnv(map[string]string{
					"TURING_INTERNAL_TOKEN": "internal",
					env:                     value,
				}))
				if err != nil {
					t.Fatalf("LoadFromEnv(%s=%q) failed: %v", env, value, err)
				}
			})
		}
	}
}

func mapEnv(values map[string]string) func(string) string {
	return func(name string) string {
		return values[name]
	}
}
