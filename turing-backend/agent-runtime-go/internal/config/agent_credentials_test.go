package config

import (
	"strings"
	"testing"
)

func TestParseAgentAPIKeysAcceptsAnAbsentValue(t *testing.T) {
	// The normal state of an install with no cloud agents. Treating it as a
	// misconfiguration would make the backend refuse to start.
	keys, err := parseAgentAPIKeys("")
	if err != nil {
		t.Fatalf("empty value: %v", err)
	}
	if len(keys) != 0 {
		t.Fatalf("keys = %v, want empty", keys)
	}
}

func TestParseAgentAPIKeysReadsTheMap(t *testing.T) {
	keys, err := parseAgentAPIKeys(`{"claude":"sk-ant-1","openai":"sk-2"}`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if keys["claude"] != "sk-ant-1" || keys["openai"] != "sk-2" {
		t.Fatalf("keys = %v, want both entries", keys)
	}
}

// A JSON syntax error quotes the offending input, and the offending input here
// is a file full of API keys. It would land in the container's log.
func TestParseAgentAPIKeysErrorDoesNotEchoTheValue(t *testing.T) {
	_, err := parseAgentAPIKeys(`{"claude":"sk-ant-supersecret"`)
	if err == nil {
		t.Fatal("malformed JSON parsed successfully")
	}
	if strings.Contains(err.Error(), "sk-ant-supersecret") {
		t.Fatalf("error echoed a key: %v", err)
	}
	if !strings.Contains(err.Error(), "TURING_AGENT_API_KEYS") {
		t.Fatalf("error = %v, want it to name the variable", err)
	}
}

// A blank key would look configured and fail at the vendor with an
// unauthenticated request.
func TestParseAgentAPIKeysRejectsBlankEntries(t *testing.T) {
	if _, err := parseAgentAPIKeys(`{"claude":""}`); err == nil {
		t.Fatal("blank key accepted")
	}
	if _, err := parseAgentAPIKeys(`{"":"sk-1"}`); err == nil {
		t.Fatal("blank name accepted")
	}
}

func TestParseAgentAPIKeysRejectsInvalidCredentialNames(t *testing.T) {
	for _, raw := range []string{
		`{" claude":"sk-1"}`,
		`{"claude ":"sk-1"}`,
		"{\"\\t\":\"sk-1\"}",
		`{"claude/key":"sk-1"}`,
		`{"` + strings.Repeat("a", 65) + `":"sk-1"}`,
	} {
		if _, err := parseAgentAPIKeys(raw); err == nil {
			t.Fatalf("invalid credential name in %q was accepted", raw)
		}
	}
}

func TestLoadFromEnvCarriesTheAgentKeys(t *testing.T) {
	cfg, err := LoadFromEnv(func(name string) string {
		switch name {
		case "TURING_RUNTIME_TOKEN":
			return "internal"
		case agentAPIKeysVar:
			return `{"claude":"sk-ant-1"}`
		default:
			return ""
		}
	})
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.AgentAPIKeys["claude"] != "sk-ant-1" {
		t.Fatalf("AgentAPIKeys = %v, want the claude entry", cfg.AgentAPIKeys)
	}
}

func TestLoadFromEnvRefusesMalformedAgentKeys(t *testing.T) {
	_, err := LoadFromEnv(func(name string) string {
		switch name {
		case "TURING_RUNTIME_TOKEN":
			return "internal"
		case agentAPIKeysVar:
			return "not json"
		default:
			return ""
		}
	})
	// A startup failure with a readable message, rather than every routed run
	// failing later with "no API key configured".
	if err == nil {
		t.Fatal("malformed agent keys did not fail startup")
	}
}
