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

func TestLoadFromEnvValidatesMaxConcurrentRunsWithinServerAndProtobufBounds(t *testing.T) {
	cfg, err := LoadFromEnv(mapEnv(map[string]string{
		"TURING_INTERNAL_TOKEN":              "internal",
		"TURING_MAX_CONCURRENT_RUNS_GENERAL": "128",
	}))
	if err != nil {
		t.Fatalf("LoadFromEnv upper bound failed: %v", err)
	}
	if cfg.MaxConcurrentRuns != 128 {
		t.Fatalf("MaxConcurrentRuns = %d, want 128", cfg.MaxConcurrentRuns)
	}
	for _, value := range []string{"0", "-1", "129", "2147483648"} {
		t.Run(value, func(t *testing.T) {
			_, err := LoadFromEnv(mapEnv(map[string]string{
				"TURING_INTERNAL_TOKEN":              "internal",
				"TURING_MAX_CONCURRENT_RUNS_GENERAL": value,
			}))
			if err == nil || !strings.Contains(err.Error(), "between 1 and 128") {
				t.Fatalf("LoadFromEnv max concurrent %s error = %v, want bounded validation", value, err)
			}
		})
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
	if cfg.ApprovalTimeout != 71*time.Second {
		t.Fatalf("ApprovalTimeout = %v, want 71s", cfg.ApprovalTimeout)
	}
	if cfg.TotalToolTimeout != 180*time.Second {
		t.Fatalf("TotalToolTimeout = %v, want 180s", cfg.TotalToolTimeout)
	}
	if cfg.HeartbeatInterval != 30*time.Second {
		t.Fatalf("HeartbeatInterval = %v, want capped default 30s", cfg.HeartbeatInterval)
	}
}

func TestLoadFromEnvDerivesHeartbeatBelowConfiguredJobLease(t *testing.T) {
	cfg, err := LoadFromEnv(mapEnv(map[string]string{
		"TURING_INTERNAL_TOKEN": "internal",
		"TURING_JOB_TIMEOUT_MS": "30",
	}))
	if err != nil {
		t.Fatalf("LoadFromEnv failed: %v", err)
	}
	if cfg.HeartbeatInterval != 10*time.Millisecond {
		t.Fatalf("HeartbeatInterval = %v, want one third of 30ms lease", cfg.HeartbeatInterval)
	}
}

func TestLoadFromEnvKeepsApprovalWaitBeyondEffectiveApprovalExpiry(t *testing.T) {
	baseEnv := map[string]string{
		"TURING_INTERNAL_TOKEN":           "internal",
		"TURING_TOOL_TIMEOUT_MS":          "1000",
		"TURING_APPROVAL_TIMEOUT_MS":      "1000",
		"TURING_TOOL_TOTAL_TIMEOUT_MS":    "12000",
		"TURING_APPROVAL_WAIT_TIMEOUT_MS": "7000",
	}
	cfg, err := LoadFromEnv(mapEnv(baseEnv))
	if err != nil {
		t.Fatalf("LoadFromEnv failed: %v", err)
	}
	if cfg.ApprovalTimeout != 7*time.Second {
		t.Fatalf("ApprovalTimeout = %v, want 7s effective expiry plus margin", cfg.ApprovalTimeout)
	}

	baseEnv["TURING_APPROVAL_WAIT_TIMEOUT_MS"] = "6999"
	_, err = LoadFromEnv(mapEnv(baseEnv))
	if err == nil || !strings.Contains(err.Error(), "TURING_APPROVAL_WAIT_TIMEOUT_MS") {
		t.Fatalf("LoadFromEnv error = %v, want approval wait ordering validation", err)
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

// The agent runtime is the side that actually calls Ollama, so its defaults are
// the ones that decide which model runs and how long it stays resident. Without
// these, reverting either default would break nothing in the suite — and a
// runtime defaulting to a different model than the orchestrator advertises is a
// silent mismatch rather than a loud failure.
func TestLoadFromEnvDefaultsTheLocalModelAndKeepAlive(t *testing.T) {
	cfg, err := LoadFromEnv(mapEnv(map[string]string{
		"TURING_INTERNAL_TOKEN": "internal",
	}))
	if err != nil {
		t.Fatalf("LoadFromEnv failed: %v", err)
	}
	if cfg.OllamaModel != "qwen2.5:7b" {
		t.Fatalf("OllamaModel = %q, want qwen2.5:7b", cfg.OllamaModel)
	}
	// Must stay above TURING_APPROVAL_WAIT_TIMEOUT_MS (71s) or the model is
	// evicted mid-run while the user decides on an approval, costing a reload
	// and the whole conversation's KV cache on the resumed answer.
	if cfg.OllamaKeepAlive != "2m" {
		t.Fatalf("OllamaKeepAlive = %q, want 2m", cfg.OllamaKeepAlive)
	}
	approvalWait := 71 * time.Second
	keepAlive, err := time.ParseDuration(cfg.OllamaKeepAlive)
	if err != nil {
		t.Fatalf("default keep-alive %q is not a duration: %v", cfg.OllamaKeepAlive, err)
	}
	if keepAlive <= approvalWait {
		t.Fatalf("keep-alive %s <= approval wait %s: the model would unload mid-run", keepAlive, approvalWait)
	}
}

// A bad value must fail at startup with a readable message rather than 400ing
// every chat request. -1 in particular is Ollama's documented "forever" and is
// also its own server env var, so it must be accepted, not rejected.
func TestLoadFromEnvValidatesKeepAlive(t *testing.T) {
	if _, err := LoadFromEnv(mapEnv(map[string]string{
		"TURING_INTERNAL_TOKEN": "internal",
		"OLLAMA_KEEP_ALIVE":     "forever",
	})); err == nil {
		t.Fatal("an unparseable keep-alive was accepted; it would 400 every request instead")
	}
	cfg, err := LoadFromEnv(mapEnv(map[string]string{
		"TURING_INTERNAL_TOKEN": "internal",
		"OLLAMA_KEEP_ALIVE":     "-1",
	}))
	if err != nil {
		t.Fatalf("-1 (Ollama's documented \"forever\") was rejected: %v", err)
	}
	if cfg.OllamaKeepAlive != "-1" {
		t.Fatalf("OllamaKeepAlive = %q, want -1 preserved", cfg.OllamaKeepAlive)
	}
}

func TestLoadFromEnvHonoursExplicitModelAndKeepAlive(t *testing.T) {
	cfg, err := LoadFromEnv(mapEnv(map[string]string{
		"TURING_INTERNAL_TOKEN": "internal",
		"OLLAMA_MODEL":          "llama3.2",
		"OLLAMA_KEEP_ALIVE":     "5m",
	}))
	if err != nil {
		t.Fatalf("LoadFromEnv failed: %v", err)
	}
	if cfg.OllamaModel != "llama3.2" || cfg.OllamaKeepAlive != "5m" {
		t.Fatalf("overrides ignored: model=%q keepAlive=%q", cfg.OllamaModel, cfg.OllamaKeepAlive)
	}
}

func TestLoadFromEnvDefaultsProviderContextWindows(t *testing.T) {
	cfg, err := LoadFromEnv(mapEnv(map[string]string{
		"TURING_INTERNAL_TOKEN": "internal",
	}))
	if err != nil {
		t.Fatalf("LoadFromEnv failed: %v", err)
	}
	if cfg.OllamaContextWindowTokens != 32768 {
		t.Fatalf("OllamaContextWindowTokens = %d, want 32768", cfg.OllamaContextWindowTokens)
	}
	if cfg.OpenAIContextWindowTokens != 32768 {
		t.Fatalf("OpenAIContextWindowTokens = %d, want 32768", cfg.OpenAIContextWindowTokens)
	}
	if cfg.OllamaMaxOutputTokens != 2048 {
		t.Fatalf("OllamaMaxOutputTokens = %d, want 2048", cfg.OllamaMaxOutputTokens)
	}
	if cfg.OpenAIMaxOutputTokens != 2048 {
		t.Fatalf("OpenAIMaxOutputTokens = %d, want 2048", cfg.OpenAIMaxOutputTokens)
	}
}

func TestLoadFromEnvHonoursProviderContextWindowOverrides(t *testing.T) {
	cfg, err := LoadFromEnv(mapEnv(map[string]string{
		"TURING_INTERNAL_TOKEN":        "internal",
		"OLLAMA_CONTEXT_WINDOW_TOKENS": "4096",
		"OPENAI_CONTEXT_WINDOW_TOKENS": "65536",
		"OLLAMA_MAX_OUTPUT_TOKENS":     "512",
		"OPENAI_MAX_OUTPUT_TOKENS":     "4096",
	}))
	if err != nil {
		t.Fatalf("LoadFromEnv failed: %v", err)
	}
	if cfg.OllamaContextWindowTokens != 4096 {
		t.Fatalf("OllamaContextWindowTokens = %d, want 4096", cfg.OllamaContextWindowTokens)
	}
	if cfg.OpenAIContextWindowTokens != 65536 {
		t.Fatalf("OpenAIContextWindowTokens = %d, want 65536", cfg.OpenAIContextWindowTokens)
	}
	if cfg.OllamaMaxOutputTokens != 512 || cfg.OpenAIMaxOutputTokens != 4096 {
		t.Fatalf("output limits = %d/%d, want 512/4096", cfg.OllamaMaxOutputTokens, cfg.OpenAIMaxOutputTokens)
	}
}

func TestLoadFromEnvValidatesProviderContextWindows(t *testing.T) {
	for _, name := range []string{
		"OLLAMA_CONTEXT_WINDOW_TOKENS",
		"OPENAI_CONTEXT_WINDOW_TOKENS",
	} {
		for _, value := range []string{"0", "-1", "not-a-number", "16777217"} {
			t.Run(name+"/"+value, func(t *testing.T) {
				_, err := LoadFromEnv(mapEnv(map[string]string{
					"TURING_INTERNAL_TOKEN": "internal",
					name:                    value,
				}))
				if err == nil || !strings.Contains(err.Error(), name) {
					t.Fatalf("LoadFromEnv error = %v, want %s validation", err, name)
				}
			})
		}
	}
}

func TestLoadFromEnvValidatesProviderOutputReservations(t *testing.T) {
	for _, test := range []struct {
		name   string
		values map[string]string
	}{
		{
			name: "Ollama zero",
			values: map[string]string{
				"OLLAMA_MAX_OUTPUT_TOKENS": "0",
			},
		},
		{
			name: "OpenAI negative",
			values: map[string]string{
				"OPENAI_MAX_OUTPUT_TOKENS": "-1",
			},
		},
		{
			name: "Ollama reserve equals window",
			values: map[string]string{
				"OLLAMA_CONTEXT_WINDOW_TOKENS": "4096",
				"OLLAMA_MAX_OUTPUT_TOKENS":     "4096",
			},
		},
		{
			name: "OpenAI reserve exceeds window",
			values: map[string]string{
				"OPENAI_CONTEXT_WINDOW_TOKENS": "4096",
				"OPENAI_MAX_OUTPUT_TOKENS":     "4097",
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			test.values["TURING_INTERNAL_TOKEN"] = "internal"
			_, err := LoadFromEnv(mapEnv(test.values))
			if err == nil || !strings.Contains(err.Error(), "OUTPUT_TOKENS") {
				t.Fatalf("LoadFromEnv error = %v, want output reservation validation", err)
			}
		})
	}
}
