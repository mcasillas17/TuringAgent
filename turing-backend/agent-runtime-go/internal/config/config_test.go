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
}

func TestLoadFromEnvRejectsNonPositiveTimeouts(t *testing.T) {
	for _, test := range []struct {
		name string
		env  string
	}{
		{name: "model zero", env: "TURING_MODEL_TIMEOUT_MS"},
		{name: "tool zero", env: "TURING_TOOL_TIMEOUT_MS"},
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
	for _, env := range []string{"TURING_MODEL_TIMEOUT_MS", "TURING_TOOL_TIMEOUT_MS"} {
		t.Run(env, func(t *testing.T) {
			cfg, err := LoadFromEnv(mapEnv(map[string]string{
				"TURING_INTERNAL_TOKEN": "internal",
				env:                     value,
			}))
			if err != nil {
				t.Fatalf("LoadFromEnv(%s=%s) failed: %v", env, value, err)
			}
			got := cfg.ModelTimeout
			if env == "TURING_TOOL_TIMEOUT_MS" {
				got = cfg.ToolTimeout
			}
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
		for _, env := range []string{"TURING_MODEL_TIMEOUT_MS", "TURING_TOOL_TIMEOUT_MS"} {
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

func mapEnv(values map[string]string) func(string) string {
	return func(name string) string {
		return values[name]
	}
}
