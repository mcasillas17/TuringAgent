package config

import (
	"math"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/runoutcome"
)

func TestLoadFromEnvRequiresSecretsAndDefaultsPorts(t *testing.T) {
	env := requiredEnv()
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
	env := requiredEnv()
	env["ORCHESTRATOR_PUBLIC_PORT"] = "abc"

	_, err := LoadFromMap(env)
	if err == nil {
		t.Fatal("expected invalid integer error")
	}
}

func TestLoadFromEnvUsesApprovalTTL(t *testing.T) {
	env := requiredEnv()
	env["TURING_APPROVAL_TIMEOUT_MS"] = "75000"

	cfg, err := LoadFromMap(env)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ApprovalTTLMS != 75000 {
		t.Fatalf("ApprovalTTLMS = %d, want 75000", cfg.ApprovalTTLMS)
	}
}

func TestLoadFromMapRejectsPlaintextKeyedRemoteEndpoint(t *testing.T) {
	env := requiredEnv()
	env["OPENAI_ENABLED"] = "true"
	env["OPENAI_BASE_URL"] = "http://host.docker.internal:8080/v1"

	_, err := LoadFromMap(env)
	if err == nil || !strings.Contains(err.Error(), "OPENAI_BASE_URL") {
		t.Fatalf("LoadFromMap error = %v, want keyed endpoint rejection", err)
	}
}

func TestLoadFromMapRequiresRemoteEgressSigningSecret(t *testing.T) {
	env := requiredEnv()
	delete(env, "TURING_EGRESS_SIGNING_SECRET")
	_, err := LoadFromMap(env)
	if err == nil || !strings.Contains(err.Error(), "TURING_EGRESS_SIGNING_SECRET") {
		t.Fatalf("LoadFromMap error = %v, want signing secret requirement", err)
	}
}

func TestLoadFromMapCanonicalizesKeyedEndpoint(t *testing.T) {
	env := requiredEnv()
	env["OPENAI_ENABLED"] = "true"
	env["OPENAI_BASE_URL"] = "HTTPS://Example.COM:443/v1/"

	cfg, err := LoadFromMap(env)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.OpenAIBaseURL != "https://example.com/v1" {
		t.Fatalf("OpenAIBaseURL = %q", cfg.OpenAIBaseURL)
	}
}

func TestLoadFromMapRejectsCredentialBearingDisabledEndpoint(t *testing.T) {
	env := requiredEnv()
	env["OPENAI_BASE_URL"] = "https://user:secret@example.com/v1"
	_, err := LoadFromMap(env)
	if err == nil || !strings.Contains(err.Error(), "OPENAI_BASE_URL") {
		t.Fatalf("LoadFromMap error = %v, want credential-bearing endpoint rejection", err)
	}

}

func TestLoadFromMapRejectsRemoteOllamaEndpoint(t *testing.T) {
	env := requiredEnv()
	env["OLLAMA_BASE_URL"] = "https://ollama.example.com"
	_, err := LoadFromMap(env)
	if err == nil || !strings.Contains(err.Error(), "OLLAMA_BASE_URL") {
		t.Fatalf("LoadFromMap error = %v, want local Ollama endpoint rejection", err)
	}
}

func TestLoadFromMapValidatesMaxConcurrentRunsWithinRuntimeBound(t *testing.T) {
	base := requiredEnv()
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

func TestLoadFromMapValidatesJobMaxAttemptsWithinNoticeBound(t *testing.T) {
	env := requiredEnv()
	if _, err := LoadFromMap(env); err != nil {
		t.Fatalf("LoadFromMap default TURING_JOB_MAX_ATTEMPTS: %v", err)
	}

	env["TURING_JOB_MAX_ATTEMPTS"] = strconv.Itoa(runoutcome.MaxNoticeAttempts)
	if _, err := LoadFromMap(env); err != nil {
		t.Fatalf("LoadFromMap TURING_JOB_MAX_ATTEMPTS=%d: %v", runoutcome.MaxNoticeAttempts, err)
	}

	env["TURING_JOB_MAX_ATTEMPTS"] = strconv.Itoa(runoutcome.MaxNoticeAttempts + 1)
	_, err := LoadFromMap(env)
	if err == nil || !strings.Contains(err.Error(), "TURING_JOB_MAX_ATTEMPTS") {
		t.Fatalf("LoadFromMap TURING_JOB_MAX_ATTEMPTS=%d error = %v, want bounded validation", runoutcome.MaxNoticeAttempts+1, err)
	}

	env["TURING_JOB_MAX_ATTEMPTS"] = "0"
	_, err = LoadFromMap(env)
	if err == nil || !strings.Contains(err.Error(), "TURING_JOB_MAX_ATTEMPTS") {
		t.Fatalf("LoadFromMap TURING_JOB_MAX_ATTEMPTS=0 error = %v, want bounded validation", err)
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
		"TURING_CLIENT_API_KEY":          "client-key",
		"TURING_RUNTIME_TOKEN":           "runtime-token",
		"TURING_APPROVAL_CONSUMER_TOKEN": "approval-consumer-token",
		"MCP_FILES_ENABLED":              "true",
		"TURING_APPROVAL_JWT_SECRET":     "approval-secret",
		"TURING_EGRESS_SIGNING_SECRET":   "egress-secret",
		"TURING_CURSOR_HMAC_SECRET":      strings.Repeat("ab", 32),
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

// A shared token would let a compromised approval consumer (mcp-files)
// present the runtime's own token and reach RuntimeService/SessionService —
// exactly the privilege escalation TUR-006 removes. This must fail at
// startup, not be discovered as an authorization bug later.
func TestLoadFromMapRejectsEqualRuntimeAndApprovalConsumerTokens(t *testing.T) {
	env := requiredEnv()
	env["TURING_APPROVAL_CONSUMER_TOKEN"] = env["TURING_RUNTIME_TOKEN"]
	_, err := LoadFromMap(env)
	if err == nil {
		t.Fatal("LoadFromMap accepted equal runtime and approval-consumer tokens")
	}
	if !strings.Contains(err.Error(), "TURING_RUNTIME_TOKEN") || !strings.Contains(err.Error(), "TURING_APPROVAL_CONSUMER_TOKEN") {
		t.Fatalf("error = %v, want it to name both variables", err)
	}
}

func TestLoadFromMapRequiresRuntimeAndApprovalConsumerTokens(t *testing.T) {
	for _, name := range []string{"TURING_RUNTIME_TOKEN", "TURING_APPROVAL_CONSUMER_TOKEN"} {
		t.Run(name, func(t *testing.T) {
			env := requiredEnv()
			delete(env, name)
			_, err := LoadFromMap(env)
			if err == nil || !strings.Contains(err.Error(), name) {
				t.Fatalf("LoadFromMap without %s error = %v, want it named", name, err)
			}
		})
	}
}

// MCP_FILES_ENABLED replaces MCP_FILES_TOKEN_GENERAL as the orchestrator's
// only signal for whether mcp-files is provisioned: the orchestrator never
// calls mcp-files itself, so it must never hold that bearer token, only
// whether Compose considers it configured.
func TestLoadFromMapRequiresExplicitFilesMCPEnabled(t *testing.T) {
	env := requiredEnv()
	delete(env, "MCP_FILES_ENABLED")
	if _, err := LoadFromMap(env); err == nil || !strings.Contains(err.Error(), "MCP_FILES_ENABLED") {
		t.Fatalf("LoadFromMap without MCP_FILES_ENABLED error = %v, want it named", err)
	}

	env = requiredEnv()
	env["MCP_FILES_ENABLED"] = "yes"
	if _, err := LoadFromMap(env); err == nil {
		t.Fatal("LoadFromMap accepted a non-boolean MCP_FILES_ENABLED")
	}

	env = requiredEnv()
	env["MCP_FILES_ENABLED"] = "false"
	cfg, err := LoadFromMap(env)
	if err != nil {
		t.Fatalf("LoadFromMap rejected MCP_FILES_ENABLED=false: %v", err)
	}
	if cfg.FilesMCPEnabled {
		t.Fatal("FilesMCPEnabled = true, want false")
	}
}

// OPENAI_ENABLED is optional (default false, matching the previous optional
// OPENAI_API_KEY), but any set value must be an explicit boolean.
func TestLoadFromMapParsesOptionalOpenAIEnabled(t *testing.T) {
	cfg, err := LoadFromMap(requiredEnv())
	if err != nil {
		t.Fatal(err)
	}
	if cfg.OpenAIEnabled {
		t.Fatal("OpenAIEnabled defaulted to true with OPENAI_ENABLED unset")
	}

	env := requiredEnv()
	env["OPENAI_ENABLED"] = "true"
	cfg, err = LoadFromMap(env)
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.OpenAIEnabled {
		t.Fatal("OpenAIEnabled = false, want true")
	}

	env = requiredEnv()
	env["OPENAI_ENABLED"] = "1"
	if _, err := LoadFromMap(env); err == nil {
		t.Fatal("LoadFromMap accepted a non-boolean OPENAI_ENABLED")
	}
}

// The orchestrator's Config type must never carry OPENAI_API_KEY or
// MCP_SYSTEM_TOKEN_GENERAL/MCP_FILES_TOKEN_GENERAL: those secrets belong only
// to the processes that actually call OpenAI or the MCP servers. Setting them
// here must have no effect and must not be required.
func TestLoadFromMapNeverRequiresOrCarriesRemovedProviderSecrets(t *testing.T) {
	cfg, err := LoadFromMap(requiredEnv())
	if err != nil {
		t.Fatal(err)
	}
	if cfg.OpenAIEnabled {
		t.Fatal("OpenAIEnabled = true without OPENAI_ENABLED or OPENAI_API_KEY set")
	}

	env := requiredEnv()
	env["OPENAI_API_KEY"] = "sk-should-not-be-read"
	env["MCP_SYSTEM_TOKEN_GENERAL"] = "system-token-should-not-be-read"
	env["MCP_FILES_TOKEN_GENERAL"] = "files-token-should-not-be-read"
	cfg, err = LoadFromMap(env)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.OpenAIEnabled {
		t.Fatal("setting OPENAI_API_KEY alone must not enable OpenAI; only OPENAI_ENABLED may")
	}
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
		"TURING_CLIENT_API_KEY":          "client-key",
		"TURING_RUNTIME_TOKEN":           "runtime-token",
		"TURING_APPROVAL_CONSUMER_TOKEN": "approval-consumer-token",
		"MCP_FILES_ENABLED":              "true",
		"TURING_APPROVAL_JWT_SECRET":     "approval-secret",
		"TURING_EGRESS_SIGNING_SECRET":   "egress-secret",
		"TURING_CURSOR_HMAC_SECRET":      strings.Repeat("ab", 32),
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

// The vault mounts in the SKILLS_ROOT mold: one name, one variable, and a
// clean absolute path or nothing. The old MEMORY_VAULT_ROOT spelling is gone
// rather than aliased — an install that still sets it must be told by the
// default landing on /memory, not silently obeyed at a second name.
func TestLoadFromMapDefaultsAndOverridesMemoryRoot(t *testing.T) {
	cfg, err := LoadFromMap(requiredEnv())
	if err != nil {
		t.Fatal(err)
	}
	if cfg.MemoryRoot != "/memory" {
		t.Fatalf("MemoryRoot = %q, want /memory", cfg.MemoryRoot)
	}

	env := requiredEnv()
	env["MEMORY_ROOT"] = "/srv/test-memory"
	cfg, err = LoadFromMap(env)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.MemoryRoot != "/srv/test-memory" {
		t.Fatalf("MemoryRoot = %q, want the absolute override", cfg.MemoryRoot)
	}

	for _, bad := range []string{"relative/memory", "memory", "", "/srv/../memory", "/srv/memory/", "/srv//memory", "/srv/./memory"} {
		env := requiredEnv()
		env["MEMORY_ROOT"] = bad
		cfg, err := LoadFromMap(env)
		if bad == "" {
			// An explicitly empty value is the unset case: the default stands.
			if err != nil || cfg.MemoryRoot != "/memory" {
				t.Fatalf("empty MEMORY_ROOT = %q err=%v, want the /memory default", cfg.MemoryRoot, err)
			}
			continue
		}
		if err == nil {
			t.Fatalf("LoadFromMap accepted MEMORY_ROOT = %q", bad)
		}
		if !strings.Contains(err.Error(), "MEMORY_ROOT") {
			t.Fatalf("error = %v, want it to name MEMORY_ROOT", err)
		}
	}
}

// MEMORY_VAULT_ROOT is not a second spelling of the same setting; it is not a
// setting at all.
func TestLoadFromMapIgnoresTheRetiredMemoryVaultRootVariable(t *testing.T) {
	env := requiredEnv()
	env["MEMORY_VAULT_ROOT"] = "/somewhere/else"
	cfg, err := LoadFromMap(env)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.MemoryRoot != "/memory" {
		t.Fatalf("MemoryRoot = %q, want the retired variable to have no effect", cfg.MemoryRoot)
	}
}
