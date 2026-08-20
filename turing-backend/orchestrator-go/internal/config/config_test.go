package config

import (
	"math"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestLoadFromEnvRequiresSecretsAndDefaultsPorts(t *testing.T) {
	env := map[string]string{
		"TURING_CLIENT_API_KEY":      "client-key",
		"TURING_INTERNAL_TOKEN":      "internal-token",
		"MCP_SYSTEM_TOKEN_GENERAL":   "system-token",
		"MCP_FILES_TOKEN_GENERAL":    "files-token",
		"TURING_APPROVAL_JWT_SECRET": "approval-secret",
		"TURING_CURSOR_HMAC_SECRET":  strings.Repeat("ab", 32),
	}
	cfg, err := LoadFromMap(env)
	if err != nil {
		t.Fatalf("LoadFromMap returned error: %v", err)
	}
	if cfg.PublicPort != 3000 || cfg.InternalPort != 3001 {
		t.Fatalf("ports = %d/%d, want 3000/3001", cfg.PublicPort, cfg.InternalPort)
	}
	if cfg.OllamaModel != "qwen2.5:7b" {
		t.Fatalf("OllamaModel = %q", cfg.OllamaModel)
	}
}

func TestLoadFromEnvRejectsInvalidInteger(t *testing.T) {
	env := map[string]string{
		"TURING_CLIENT_API_KEY":      "client-key",
		"TURING_INTERNAL_TOKEN":      "internal-token",
		"MCP_SYSTEM_TOKEN_GENERAL":   "system-token",
		"MCP_FILES_TOKEN_GENERAL":    "files-token",
		"TURING_APPROVAL_JWT_SECRET": "approval-secret",
		"TURING_CURSOR_HMAC_SECRET":  strings.Repeat("ab", 32),
		"ORCHESTRATOR_PUBLIC_PORT":   "abc",
	}

	_, err := LoadFromMap(env)
	if err == nil {
		t.Fatal("expected invalid integer error")
	}
}

func TestLoadFromEnvUsesApprovalTTL(t *testing.T) {
	env := map[string]string{
		"TURING_CLIENT_API_KEY":      "client-key",
		"TURING_INTERNAL_TOKEN":      "internal-token",
		"MCP_SYSTEM_TOKEN_GENERAL":   "system-token",
		"MCP_FILES_TOKEN_GENERAL":    "files-token",
		"TURING_APPROVAL_JWT_SECRET": "approval-secret",
		"TURING_CURSOR_HMAC_SECRET":  strings.Repeat("ab", 32),
		"TURING_APPROVAL_TIMEOUT_MS": "75000",
	}

	cfg, err := LoadFromMap(env)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ApprovalTTLMS != 75000 {
		t.Fatalf("ApprovalTTLMS = %d, want 75000", cfg.ApprovalTTLMS)
	}
}

func TestLoadFromMapValidatesMaxConcurrentRunsWithinRuntimeBound(t *testing.T) {
	base := map[string]string{
		"TURING_CLIENT_API_KEY":      "client-key",
		"TURING_INTERNAL_TOKEN":      "internal-token",
		"MCP_SYSTEM_TOKEN_GENERAL":   "system-token",
		"MCP_FILES_TOKEN_GENERAL":    "files-token",
		"TURING_APPROVAL_JWT_SECRET": "approval-secret",
		"TURING_CURSOR_HMAC_SECRET":  strings.Repeat("ab", 32),
	}
	for _, value := range []string{"0", "129", "2147483648"} {
		t.Run(value, func(t *testing.T) {
			env := make(map[string]string, len(base)+1)
			for key, item := range base {
				env[key] = item
			}
			env["TURING_MAX_CONCURRENT_RUNS_GENERAL"] = value
			_, err := LoadFromMap(env)
			if err == nil || !strings.Contains(err.Error(), "TURING_MAX_CONCURRENT_RUNS_GENERAL") {
				t.Fatalf("LoadFromMap max concurrent %s error = %v, want bounded validation", value, err)
			}
		})
	}
}

func TestLoadFromMapLoadsBoundedAdvertisedContextLimits(t *testing.T) {
	env := requiredEnv()
	env["OLLAMA_CONTEXT_WINDOW_TOKENS"] = "32768"
	env["OPENAI_CONTEXT_WINDOW_TOKENS"] = "128000"
	cfg, err := LoadFromMap(env)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.OllamaContextWindowTokens != 32768 || cfg.OpenAIContextWindowTokens != 128000 {
		t.Fatalf("context limits = %d/%d", cfg.OllamaContextWindowTokens, cfg.OpenAIContextWindowTokens)
	}
	env["OLLAMA_CONTEXT_WINDOW_TOKENS"] = "16777217"
	if _, err := LoadFromMap(env); err == nil || !strings.Contains(err.Error(), "OLLAMA_CONTEXT_WINDOW_TOKENS") {
		t.Fatalf("oversized context limit error = %v", err)
	}
}

func TestLoadFromMapRejectsZeroSharedRuntimeSettings(t *testing.T) {
	base := requiredEnv()
	for _, name := range []string{
		"TURING_JOB_TIMEOUT_MS",
		"TURING_MAX_TOOL_CALLS_PER_RUN",
		"TURING_MODEL_TIMEOUT_MS",
		"TURING_TOOL_TIMEOUT_MS",
	} {
		t.Run(name, func(t *testing.T) {
			env := cloneEnv(base)
			env[name] = "0"
			_, err := LoadFromMap(env)
			if err == nil || !strings.Contains(err.Error(), name) {
				t.Fatalf("LoadFromMap %s=0 error = %v, want positive-value validation", name, err)
			}
		})
	}
}

func TestLoadFromMapRejectsSharedRuntimeDurationsOutsideTimeRange(t *testing.T) {
	base := requiredEnv()
	tooLarge := strconv.FormatInt(int64(math.MaxInt64/int64(time.Millisecond))+1, 10)
	for _, name := range []string{
		"TURING_JOB_TIMEOUT_MS",
		"TURING_MODEL_TIMEOUT_MS",
		"TURING_TOOL_TIMEOUT_MS",
	} {
		t.Run(name, func(t *testing.T) {
			env := cloneEnv(base)
			env[name] = tooLarge
			_, err := LoadFromMap(env)
			if err == nil || !strings.Contains(err.Error(), name) {
				t.Fatalf("LoadFromMap %s=%s error = %v, want duration range validation", name, tooLarge, err)
			}
		})
	}
}

func requiredEnv() map[string]string {
	return map[string]string{
		"TURING_CLIENT_API_KEY":      "client-key",
		"TURING_INTERNAL_TOKEN":      "internal-token",
		"MCP_SYSTEM_TOKEN_GENERAL":   "system-token",
		"MCP_FILES_TOKEN_GENERAL":    "files-token",
		"TURING_APPROVAL_JWT_SECRET": "approval-secret",
		"TURING_CURSOR_HMAC_SECRET":  strings.Repeat("ab", 32),
	}
}

func TestLoadFromMapRequiresCursorHMACSecret(t *testing.T) {
	env := requiredEnv()
	delete(env, "TURING_CURSOR_HMAC_SECRET")

	_, err := LoadFromMap(env)
	if err == nil || !strings.Contains(err.Error(), "TURING_CURSOR_HMAC_SECRET") {
		t.Fatalf("LoadFromMap error = %v, want missing cursor secret", err)
	}
}

func TestLoadFromMapValidatesCursorHMACSecret(t *testing.T) {
	for _, value := range []string{
		"not-hex",
		strings.Repeat("a", 63),
		strings.Repeat("A", 64),
		strings.Repeat("ab", 33),
	} {
		t.Run(value, func(t *testing.T) {
			env := requiredEnv()
			env["TURING_CURSOR_HMAC_SECRET"] = value
			_, err := LoadFromMap(env)
			if err == nil || !strings.Contains(err.Error(), "TURING_CURSOR_HMAC_SECRET") {
				t.Fatalf("LoadFromMap cursor secret %q error = %v", value, err)
			}
		})
	}

	env := requiredEnv()
	env["TURING_CURSOR_HMAC_SECRET"] = strings.Repeat("ab", 32)
	cfg, err := LoadFromMap(env)
	if err != nil {
		t.Fatalf("LoadFromMap valid cursor secret: %v", err)
	}
	var want [32]byte
	for i := range want {
		want[i] = 0xab
	}
	if cfg.CursorHMACKey != want {
		t.Fatalf("CursorHMACKey = %x, want %x", cfg.CursorHMACKey, want)
	}
}

func cloneEnv(source map[string]string) map[string]string {
	cloned := make(map[string]string, len(source))
	for key, value := range source {
		cloned[key] = value
	}
	return cloned
}

// The integration key is optional: an existing install that has never run the
// updated init.sh still starts, and only connecting an account is refused.
func TestLoadFromMapLeavesTheIntegrationKeyOptional(t *testing.T) {
	cfg, err := LoadFromMap(baseIntegrationEnv(nil))
	if err != nil {
		t.Fatalf("LoadFromMap returned error: %v", err)
	}
	if cfg.IntegrationKey != "" {
		t.Fatalf("IntegrationKey = %q, want empty", cfg.IntegrationKey)
	}
}

func TestLoadFromMapDefaultsAndOverridesSkillsRoot(t *testing.T) {
	cfg, err := LoadFromMap(requiredEnv())
	if err != nil {
		t.Fatal(err)
	}
	if cfg.SkillsRoot != "/skills" {
		t.Fatalf("SkillsRoot = %q, want /skills", cfg.SkillsRoot)
	}
	env := requiredEnv()
	env["SKILLS_ROOT"] = "/tmp/test-skills"
	cfg, err = LoadFromMap(env)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.SkillsRoot != "/tmp/test-skills" {
		t.Fatalf("SkillsRoot = %q", cfg.SkillsRoot)
	}
	env["SKILLS_ROOT"] = "relative/skills"
	if _, err := LoadFromMap(env); err == nil {
		t.Fatal("relative SKILLS_ROOT was accepted")
	}
}

// A key that is present but the wrong shape fails at startup, not while
// somebody is pasting a token into the connect dialog.
func TestLoadFromMapRejectsAMalformedIntegrationKey(t *testing.T) {
	for _, bad := range []string{"not-hex", strings.Repeat("a", 32), strings.Repeat("a", 63)} {
		_, err := LoadFromMap(baseIntegrationEnv(map[string]string{"TURING_INTEGRATION_KEY": bad}))
		if err == nil {
			t.Fatalf("LoadFromMap accepted TURING_INTEGRATION_KEY = %q", bad)
		}
		if !strings.Contains(err.Error(), "TURING_INTEGRATION_KEY") {
			t.Fatalf("error = %v, want it to name the variable", err)
		}
	}

	cfg, err := LoadFromMap(baseIntegrationEnv(map[string]string{
		"TURING_INTEGRATION_KEY": strings.Repeat("ab", 32),
	}))
	if err != nil {
		t.Fatalf("LoadFromMap rejected a valid key: %v", err)
	}
	if cfg.IntegrationKey != strings.Repeat("ab", 32) {
		t.Fatalf("IntegrationKey = %q, want it carried through", cfg.IntegrationKey)
	}
}

func baseIntegrationEnv(extra map[string]string) map[string]string {
	env := map[string]string{
		"TURING_CLIENT_API_KEY":      "client-key",
		"TURING_INTERNAL_TOKEN":      "internal-token",
		"MCP_SYSTEM_TOKEN_GENERAL":   "system-token",
		"MCP_FILES_TOKEN_GENERAL":    "files-token",
		"TURING_APPROVAL_JWT_SECRET": "approval-secret",
		"TURING_CURSOR_HMAC_SECRET":  strings.Repeat("ab", 32),
	}
	for key, value := range extra {
		env[key] = value
	}
	return env
}

// The scheduler is the one thing in this process that creates work nobody
// asked for, so "off" has to be expressible and has to be the literal 0.
func TestAutomationTickDefaultsAndAcceptsZeroAsOff(t *testing.T) {
	cfg, err := LoadFromMap(requiredEnv())
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.AutomationTickMS != 30000 {
		t.Fatalf("default automation tick = %d, want 30000", cfg.AutomationTickMS)
	}

	env := requiredEnv()
	env["TURING_AUTOMATION_TICK_MS"] = "0"
	cfg, err = LoadFromMap(env)
	if err != nil {
		t.Fatalf("load with tick 0: %v", err)
	}
	if cfg.AutomationTickMS != 0 {
		t.Fatalf("automation tick = %d, want 0 to mean off", cfg.AutomationTickMS)
	}

	env = requiredEnv()
	env["TURING_AUTOMATION_TICK_MS"] = "soon"
	if _, err := LoadFromMap(env); err == nil {
		t.Fatal("a non-integer automation tick was accepted")
	}

	env = requiredEnv()
	env["TURING_AUTOMATION_TICK_MS"] = "-1"
	if _, err := LoadFromMap(env); err == nil {
		t.Fatal("a negative automation tick was accepted")
	}
}
