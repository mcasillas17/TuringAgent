package config

import (
	"strings"
	"testing"
)

func TestParseAgentAPIKeysAcceptsAnAbsentValue(t *testing.T) {
	keys, err := ParseAgentAPIKeys("")
	if err != nil {
		t.Fatalf("empty value: %v", err)
	}
	if len(keys) != 0 {
		t.Fatalf("keys = %v, want empty", keys)
	}
}

// A JSON syntax error quotes the offending input, and the offending input here
// is a file full of API keys.
func TestParseAgentAPIKeysErrorDoesNotEchoTheValue(t *testing.T) {
	_, err := ParseAgentAPIKeys(`{"claude":"sk-ant-supersecret"`)
	if err == nil {
		t.Fatal("malformed JSON parsed successfully")
	}
	if strings.Contains(err.Error(), "sk-ant-supersecret") {
		t.Fatalf("error echoed a key: %v", err)
	}
}

func TestAgentCredentialNamesDropsTheKeys(t *testing.T) {
	names := AgentCredentialNames(map[string]string{"openai": "sk-2", "claude": "sk-1"})

	// Sorted, so a config dump or a log line reads the same every start.
	if strings.Join(names, ",") != "claude,openai" {
		t.Fatalf("names = %v, want [claude openai]", names)
	}
	for _, name := range names {
		if strings.HasPrefix(name, "sk-") {
			t.Fatalf("a key leaked into the name list: %v", names)
		}
	}
}

// The orchestrator never calls a vendor, so holding a key would buy nothing
// and risk it reaching a log or a response. This pins that Config carries the
// names and not the values.
func TestLoadFromMapKeepsOnlyCredentialNames(t *testing.T) {
	cfg, err := LoadFromMap(map[string]string{
		"TURING_CLIENT_API_KEY":          "client",
		"TURING_RUNTIME_TOKEN":           "runtime",
		"TURING_APPROVAL_CONSUMER_TOKEN": "approval-consumer",
		"MCP_FILES_ENABLED":              "true",
		"TURING_APPROVAL_JWT_SECRET":     "secret",
		AgentAPIKeysVar:                  `{"claude":"sk-ant-1"}`,
	})
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if strings.Join(cfg.AgentCredentialNames, ",") != "claude" {
		t.Fatalf("AgentCredentialNames = %v, want [claude]", cfg.AgentCredentialNames)
	}
	for _, name := range cfg.AgentCredentialNames {
		if name == "sk-ant-1" {
			t.Fatal("the key itself reached Config")
		}
	}
}

func TestLoadFromMapRefusesMalformedAgentKeys(t *testing.T) {
	_, err := LoadFromMap(map[string]string{
		"TURING_CLIENT_API_KEY":          "client",
		"TURING_RUNTIME_TOKEN":           "runtime",
		"TURING_APPROVAL_CONSUMER_TOKEN": "approval-consumer",
		"MCP_FILES_ENABLED":              "true",
		"TURING_APPROVAL_JWT_SECRET":     "secret",
		AgentAPIKeysVar:                  "not json",
	})
	if err == nil {
		t.Fatal("malformed agent keys did not fail startup")
	}
}
