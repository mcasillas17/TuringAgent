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
		"TURING_RUNTIME_TOKEN":           "internal",
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
		"TURING_RUNTIME_TOKEN":   "internal",
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
		"TURING_RUNTIME_TOKEN": "internal",
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
		"TURING_RUNTIME_TOKEN":               "internal",
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
				"TURING_RUNTIME_TOKEN":               "internal",
				"TURING_MAX_CONCURRENT_RUNS_GENERAL": value,
			}))
			if err == nil || !strings.Contains(err.Error(), "between 1 and 128") {
				t.Fatalf("LoadFromEnv max concurrent %s error = %v, want bounded validation", value, err)
			}
		})
	}
}

func TestLoadFromEnvLoadsBoundedAdvertisedContextLimits(t *testing.T) {
	cfg, err := LoadFromEnv(mapEnv(map[string]string{
		"TURING_RUNTIME_TOKEN":         "internal",
		"OLLAMA_CONTEXT_WINDOW_TOKENS": "32768",
		"OPENAI_CONTEXT_WINDOW_TOKENS": "128000",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.LocalContextWindowTokens != 32768 || cfg.OpenAIContextWindowTokens != 128000 {
		t.Fatalf("context limits = %d/%d", cfg.LocalContextWindowTokens, cfg.OpenAIContextWindowTokens)
	}
	for _, value := range []string{"-1", "2147483648"} {
		t.Run(value, func(t *testing.T) {
			_, err := LoadFromEnv(mapEnv(map[string]string{
				"TURING_RUNTIME_TOKEN":         "internal",
				"OLLAMA_CONTEXT_WINDOW_TOKENS": value,
			}))
			if err == nil || !strings.Contains(err.Error(), "OLLAMA_CONTEXT_WINDOW_TOKENS") {
				t.Fatalf("context limit %s error = %v", value, err)
			}
		})
	}
}

func TestLoadFromEnvRejectsNonPositiveMaxToolCallsPerRun(t *testing.T) {
	for _, value := range []string{"0", "-1"} {
		t.Run(value, func(t *testing.T) {
			_, err := LoadFromEnv(mapEnv(map[string]string{
				"TURING_RUNTIME_TOKEN":          "internal",
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
		"TURING_RUNTIME_TOKEN": "internal",
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
		"TURING_RUNTIME_TOKEN":  "internal",
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
		"TURING_RUNTIME_TOKEN":            "internal",
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
		"TURING_RUNTIME_TOKEN":         "internal",
		"TURING_APPROVAL_TIMEOUT_MS":   "1000",
		"TURING_TOOL_TOTAL_TIMEOUT_MS": "1000",
	}))
	if err == nil || !strings.Contains(err.Error(), "TURING_TOOL_TOTAL_TIMEOUT_MS") {
		t.Fatalf("LoadFromEnv error = %v, want total-tool timeout safety error", err)
	}
}

func TestLoadFromEnvRequiresTotalToolBudgetForApprovalLifecycle(t *testing.T) {
	_, err := LoadFromEnv(mapEnv(map[string]string{
		"TURING_RUNTIME_TOKEN":         "internal",
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
					"TURING_RUNTIME_TOKEN": "internal",
					test.env:               value,
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
				"TURING_RUNTIME_TOKEN": "internal",
				env:                    value,
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
					"TURING_RUNTIME_TOKEN": "internal",
					env:                    value,
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
			"http://[::1",
		} {
			t.Run(env+"/"+value, func(t *testing.T) {
				_, err := LoadFromEnv(mapEnv(map[string]string{
					"TURING_RUNTIME_TOKEN": "internal",
					env:                    value,
				}))

				if err == nil || !strings.Contains(err.Error(), env) ||
					!strings.Contains(err.Error(), "absolute http or https URL with a non-empty host") {
					t.Fatalf("LoadFromEnv(%s=%q) error = %v, want env-specific URL validation", env, value, err)
				}

			})
		}
	}
}

func TestLoadFromEnvRejectsRemoteOllamaEndpoint(t *testing.T) {
	_, err := LoadFromEnv(mapEnv(map[string]string{
		"TURING_RUNTIME_TOKEN": "runtime",
		"OLLAMA_BASE_URL":      "https://ollama.example.com",
	}))
	if err == nil || !strings.Contains(err.Error(), "OLLAMA_BASE_URL") {
		t.Fatalf("LoadFromEnv error = %v, want local Ollama endpoint rejection", err)
	}
}

func TestLoadFromEnvAcceptsHTTPAndHTTPSEndpointURLs(t *testing.T) {
	for _, env := range []string{
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
					"TURING_RUNTIME_TOKEN": "internal",
					env:                    value,
				}))
				if err != nil {
					t.Fatalf("LoadFromEnv(%s=%q) failed: %v", env, value, err)
				}
			})
		}
	}
}

func TestLoadFromEnvRejectsPlaintextKeyedRemoteOpenAIEndpoint(t *testing.T) {
	_, err := LoadFromEnv(mapEnv(map[string]string{
		"TURING_RUNTIME_TOKEN": "runtime",
		"OPENAI_API_KEY":       "configured",
		"OPENAI_BASE_URL":      "http://host.docker.internal:8080/v1",
	}))
	if err == nil || !strings.Contains(err.Error(), "OPENAI_BASE_URL") {
		t.Fatalf("LoadFromEnv error = %v, want keyed endpoint rejection", err)
	}
}

func TestLoadFromEnvCanonicalizesKeyedOpenAIEndpoint(t *testing.T) {
	cfg, err := LoadFromEnv(mapEnv(map[string]string{
		"TURING_RUNTIME_TOKEN": "runtime",
		"OPENAI_API_KEY":       "configured",
		"OPENAI_BASE_URL":      "HTTPS://Example.COM:443/v1/",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.OpenAIBaseURL != "https://example.com/v1" {
		t.Fatalf("OpenAIBaseURL = %q", cfg.OpenAIBaseURL)
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
		"TURING_RUNTIME_TOKEN": "internal",
	}))
	if err != nil {
		t.Fatalf("LoadFromEnv failed: %v", err)
	}
	if cfg.LocalModel != "qwen2.5:7b" {
		t.Fatalf("LocalModel = %q, want qwen2.5:7b", cfg.LocalModel)
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
		"TURING_RUNTIME_TOKEN": "internal",
		"OLLAMA_KEEP_ALIVE":    "forever",
	})); err == nil {
		t.Fatal("an unparseable keep-alive was accepted; it would 400 every request instead")
	}
	cfg, err := LoadFromEnv(mapEnv(map[string]string{
		"TURING_RUNTIME_TOKEN": "internal",
		"OLLAMA_KEEP_ALIVE":    "-1",
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
		"TURING_RUNTIME_TOKEN": "internal",
		"OLLAMA_MODEL":         "llama3.2",
		"OLLAMA_KEEP_ALIVE":    "5m",
	}))
	if err != nil {
		t.Fatalf("LoadFromEnv failed: %v", err)
	}
	if cfg.LocalModel != "llama3.2" || cfg.OllamaKeepAlive != "5m" {
		t.Fatalf("overrides ignored: model=%q keepAlive=%q", cfg.LocalModel, cfg.OllamaKeepAlive)
	}
}

func TestLoadFromEnvDefaultsProviderContextWindows(t *testing.T) {
	cfg, err := LoadFromEnv(mapEnv(map[string]string{
		"TURING_RUNTIME_TOKEN": "internal",
	}))
	if err != nil {
		t.Fatalf("LoadFromEnv failed: %v", err)
	}
	if cfg.LocalContextWindowTokens != 32768 {
		t.Fatalf("LocalContextWindowTokens = %d, want 32768", cfg.LocalContextWindowTokens)
	}
	if cfg.OpenAIContextWindowTokens != 32768 {
		t.Fatalf("OpenAIContextWindowTokens = %d, want 32768", cfg.OpenAIContextWindowTokens)
	}
	if cfg.LocalMaxOutputTokens != 2048 {
		t.Fatalf("LocalMaxOutputTokens = %d, want 2048", cfg.LocalMaxOutputTokens)
	}
	if cfg.OpenAIMaxOutputTokens != 2048 {
		t.Fatalf("OpenAIMaxOutputTokens = %d, want 2048", cfg.OpenAIMaxOutputTokens)
	}
}

func TestLoadFromEnvHonoursProviderContextWindowOverrides(t *testing.T) {
	cfg, err := LoadFromEnv(mapEnv(map[string]string{
		"TURING_RUNTIME_TOKEN":         "internal",
		"OLLAMA_CONTEXT_WINDOW_TOKENS": "4096",
		"OPENAI_CONTEXT_WINDOW_TOKENS": "65536",
		"OLLAMA_MAX_OUTPUT_TOKENS":     "512",
		"OPENAI_MAX_OUTPUT_TOKENS":     "4096",
	}))
	if err != nil {
		t.Fatalf("LoadFromEnv failed: %v", err)
	}
	if cfg.LocalContextWindowTokens != 4096 {
		t.Fatalf("LocalContextWindowTokens = %d, want 4096", cfg.LocalContextWindowTokens)
	}
	if cfg.OpenAIContextWindowTokens != 65536 {
		t.Fatalf("OpenAIContextWindowTokens = %d, want 65536", cfg.OpenAIContextWindowTokens)
	}
	if cfg.LocalMaxOutputTokens != 512 || cfg.OpenAIMaxOutputTokens != 4096 {
		t.Fatalf("output limits = %d/%d, want 512/4096", cfg.LocalMaxOutputTokens, cfg.OpenAIMaxOutputTokens)
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
					"TURING_RUNTIME_TOKEN": "internal",
					name:                   value,
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
			test.values["TURING_RUNTIME_TOKEN"] = "internal"
			_, err := LoadFromEnv(mapEnv(test.values))
			if err == nil || !strings.Contains(err.Error(), "OUTPUT_TOKENS") {
				t.Fatalf("LoadFromEnv error = %v, want output reservation validation", err)
			}
		})
	}
}

func loadLocal(t *testing.T, env map[string]string) Config {
	t.Helper()
	env["TURING_RUNTIME_TOKEN"] = "internal"
	cfg, err := LoadFromEnv(mapEnv(env))
	if err != nil {
		t.Fatalf("LoadFromEnv failed: %v", err)
	}
	return cfg
}

func wantDefaultLocalEngine(t *testing.T, cfg Config) {
	t.Helper()
	if cfg.LocalEngine != "ollama" || cfg.LocalEngineURL != "http://host.docker.internal:11434" ||
		cfg.LocalModel != "qwen2.5:7b" || cfg.LocalEngineLabel != "Ollama" || cfg.LocalEngineAPIKey != "" ||
		cfg.LocalContextWindowTokens != 32768 || cfg.LocalMaxOutputTokens != 2048 || len(cfg.DeprecatedKeys) != 0 {
		t.Fatalf("local engine = %+v", cfg)
	}
}

func TestLoadFromEnvDefaultsTheLocalEngine(t *testing.T) {
	wantDefaultLocalEngine(t, loadLocal(t, map[string]string{}))
}

// Compose passes every key through, empty when the operator set nothing, and
// the service owns the defaults: an empty value is no value.
func TestLoadFromEnvTreatsEmptyValuesAsUnset(t *testing.T) {
	env := map[string]string{}
	for _, key := range []string{
		"TURING_LOCAL_ENGINE", "TURING_LOCAL_ENGINE_URL", "TURING_LOCAL_ENGINE_API_KEY", "TURING_LOCAL_ENGINE_LABEL",
		"TURING_LOCAL_MODEL", "TURING_LOCAL_CONTEXT_WINDOW_TOKENS", "TURING_LOCAL_MAX_OUTPUT_TOKENS",
		"OLLAMA_BASE_URL", "OLLAMA_MODEL", "OLLAMA_CONTEXT_WINDOW_TOKENS", "OLLAMA_MAX_OUTPUT_TOKENS",
	} {
		env[key] = ""
	}
	wantDefaultLocalEngine(t, loadLocal(t, env))
}

func TestLoadFromEnvSelectsTheOpenAIEngine(t *testing.T) {
	cfg := loadLocal(t, map[string]string{
		"TURING_LOCAL_ENGINE":         "openai",
		"TURING_LOCAL_ENGINE_URL":     "http://host.docker.internal:8000/v1",
		"TURING_LOCAL_MODEL":          "deepseek-chat",
		"TURING_LOCAL_ENGINE_API_KEY": "dsv4-local",
		"TURING_LOCAL_ENGINE_LABEL":   "ds4",
	})
	if cfg.LocalEngine != "openai" || cfg.LocalEngineURL != "http://host.docker.internal:8000/v1" ||
		cfg.LocalModel != "deepseek-chat" || cfg.LocalEngineAPIKey != "dsv4-local" || cfg.LocalEngineLabel != "ds4" {
		t.Fatalf("local engine = %+v", cfg)
	}
	if cfg := loadLocal(t, map[string]string{
		"TURING_LOCAL_ENGINE": "openai", "TURING_LOCAL_ENGINE_URL": "http://localhost:8000/v1",
	}); cfg.LocalEngineLabel != "OpenAI-compatible" {
		t.Fatalf("default label = %q, want OpenAI-compatible", cfg.LocalEngineLabel)
	}
}

func wantConfigError(t *testing.T, env map[string]string, key string) {
	t.Helper()
	env["TURING_RUNTIME_TOKEN"] = "internal"
	if _, err := LoadFromEnv(mapEnv(env)); err == nil || !strings.Contains(err.Error(), key) {
		t.Fatalf("LoadFromEnv(%v) error = %v, want one naming %s", env, err, key)
	}
}

// Given everything else an engine needs, an unknown one is still refused, by
// name.
func TestLoadFromEnvRefusesAnUnknownEngine(t *testing.T) {
	wantConfigError(t, map[string]string{
		"TURING_LOCAL_ENGINE": "vllm", "TURING_LOCAL_ENGINE_URL": "http://localhost:8000/v1",
	}, "TURING_LOCAL_ENGINE must be")
}

// An OpenAI-compatible engine has no default address, and an Ollama URL left
// over from before is not one.
func TestLoadFromEnvRequiresAURLForTheOpenAIEngine(t *testing.T) {
	wantConfigError(t, map[string]string{
		"TURING_LOCAL_ENGINE": "openai", "OLLAMA_BASE_URL": "http://host.docker.internal:11434",
	}, "TURING_LOCAL_ENGINE_URL")
}

// The local route is consent-free because its engine is on this machine, so
// an address anywhere else is refused, whatever the engine.
func TestLoadFromEnvRefusesANonLocalEngineURL(t *testing.T) {
	for _, engine := range []string{"ollama", "openai"} {
		for _, url := range []string{"http://192.168.1.20:8000/v1", "https://api.example.com/v1"} {
			wantConfigError(t, map[string]string{
				"TURING_LOCAL_ENGINE": engine, "TURING_LOCAL_ENGINE_URL": url,
			}, "TURING_LOCAL_ENGINE_URL")
		}
	}
}

func TestLoadFromEnvFallsBackToTheOllamaNames(t *testing.T) {
	cfg := loadLocal(t, map[string]string{
		"OLLAMA_BASE_URL":              "http://localhost:11500",
		"OLLAMA_MODEL":                 "llama3.2",
		"OLLAMA_CONTEXT_WINDOW_TOKENS": "16384",
		"OLLAMA_MAX_OUTPUT_TOKENS":     "1024",
	})
	if cfg.LocalEngineURL != "http://localhost:11500" || cfg.LocalModel != "llama3.2" ||
		cfg.LocalContextWindowTokens != 16384 || cfg.LocalMaxOutputTokens != 1024 {
		t.Fatalf("local engine = %+v", cfg)
	}
	want := []string{
		"OLLAMA_BASE_URL is deprecated; set TURING_LOCAL_ENGINE_URL instead",
		"OLLAMA_MODEL is deprecated; set TURING_LOCAL_MODEL instead",
		"OLLAMA_CONTEXT_WINDOW_TOKENS is deprecated; set TURING_LOCAL_CONTEXT_WINDOW_TOKENS instead",
		"OLLAMA_MAX_OUTPUT_TOKENS is deprecated; set TURING_LOCAL_MAX_OUTPUT_TOKENS instead",
	}
	if strings.Join(cfg.DeprecatedKeys, "|") != strings.Join(want, "|") {
		t.Fatalf("deprecated = %q, want %q", cfg.DeprecatedKeys, want)
	}
}

func TestLoadFromEnvPrefersTheNewNames(t *testing.T) {
	cfg := loadLocal(t, map[string]string{
		"OLLAMA_BASE_URL": "http://localhost:11500", "TURING_LOCAL_ENGINE_URL": "http://localhost:11600",
		"OLLAMA_MODEL": "llama3.2", "TURING_LOCAL_MODEL": "qwen3:8b",
		"OLLAMA_CONTEXT_WINDOW_TOKENS": "16384", "TURING_LOCAL_CONTEXT_WINDOW_TOKENS": "65536",
		"OLLAMA_MAX_OUTPUT_TOKENS": "1024", "TURING_LOCAL_MAX_OUTPUT_TOKENS": "4096",
	})
	if cfg.LocalEngineURL != "http://localhost:11600" || cfg.LocalModel != "qwen3:8b" ||
		cfg.LocalContextWindowTokens != 65536 || cfg.LocalMaxOutputTokens != 4096 || len(cfg.DeprecatedKeys) != 0 {
		t.Fatalf("local engine = %+v", cfg)
	}
}

// A key only an OpenAI-compatible server takes is refused for Ollama, and the
// refusal never repeats the key.
func TestLoadFromEnvRefusesAnAPIKeyForTheOllamaEngine(t *testing.T) {
	env := map[string]string{"TURING_RUNTIME_TOKEN": "internal", "TURING_LOCAL_ENGINE_API_KEY": "secret-xyz"}
	_, err := LoadFromEnv(mapEnv(env))
	if err == nil || !strings.Contains(err.Error(), "TURING_LOCAL_ENGINE_API_KEY") || strings.Contains(err.Error(), "secret-xyz") {
		t.Fatalf("LoadFromEnv error = %v, want a refusal naming the key but not its value", err)
	}
}

func TestLoadFromEnvValidatesTheEngineLabel(t *testing.T) {
	for _, label := range []string{"a\nb", strings.Repeat("a", 65), "\xff"} {
		wantConfigError(t, map[string]string{"TURING_LOCAL_ENGINE_LABEL": label}, "TURING_LOCAL_ENGINE_LABEL")
	}
}

// Ollama's keep-alive is Ollama's alone: an OpenAI-compatible engine never
// reads it, so a value only Ollama would reject does not stop that engine.
func TestLoadFromEnvIgnoresOllamaKeepAliveForTheOpenAIEngine(t *testing.T) {
	cfg := loadLocal(t, map[string]string{
		"TURING_LOCAL_ENGINE": "openai", "TURING_LOCAL_ENGINE_URL": "http://localhost:8000/v1",
		"OLLAMA_KEEP_ALIVE": "forever",
	})
	if cfg.OllamaKeepAlive != "" {
		t.Fatalf("keep-alive = %q, want none for the OpenAI engine", cfg.OllamaKeepAlive)
	}
	wantConfigError(t, map[string]string{"OLLAMA_KEEP_ALIVE": "forever"}, "OLLAMA_KEEP_ALIVE")
}
