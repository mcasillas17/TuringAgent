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
	}
	cfg, err := LoadFromMap(env)
	if err != nil {
		t.Fatalf("LoadFromMap returned error: %v", err)
	}
	if cfg.PublicPort != 3000 || cfg.InternalPort != 3001 {
		t.Fatalf("ports = %d/%d, want 3000/3001", cfg.PublicPort, cfg.InternalPort)
	}
	if cfg.OllamaModel != "llama3.2" {
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
	}
}

func cloneEnv(source map[string]string) map[string]string {
	cloned := make(map[string]string, len(source))
	for key, value := range source {
		cloned[key] = value
	}
	return cloned
}
