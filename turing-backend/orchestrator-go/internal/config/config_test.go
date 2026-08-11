package config

import (
	"strings"
	"testing"
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
