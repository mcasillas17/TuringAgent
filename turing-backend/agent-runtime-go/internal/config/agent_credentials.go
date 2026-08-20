package config

import (
	"encoding/json"
	"fmt"
)

// agentAPIKeysVar holds every third-party API key an external agent can use,
// as a JSON object of {"<credential name>": "<api key>"}.
//
// One variable rather than one per agent, so adding an agent from the client
// never requires editing docker-compose.yml — compose passes an explicit list
// of names, and a per-agent variable would make each new agent a code change.
//
// The orchestrator parses the same variable, but keeps only the names: it uses
// them to tell a client whether an agent will work, and never needs a key
// because it never calls a vendor. That is why this parser is duplicated
// rather than shared — the two sides deliberately keep different halves of the
// same value, and `internal/` prevents importing across the two trees anyway.
const agentAPIKeysVar = "TURING_AGENT_API_KEYS"

const maxAgentCredentialNameRunes = 64

// parseAgentAPIKeys decodes agentAPIKeysVar. Unset or empty is not an error:
// it is the normal state of an install with no cloud agents configured.
func parseAgentAPIKeys(raw string) (map[string]string, error) {
	if raw == "" {
		return map[string]string{}, nil
	}
	keys := map[string]string{}
	if err := json.Unmarshal([]byte(raw), &keys); err != nil {
		// Deliberately not wrapping the decoder's error: a JSON syntax error
		// quotes the offending input, and the offending input here is a list
		// of API keys that would then be printed to the container's log.
		return nil, fmt.Errorf("%s must be a JSON object of {\"name\": \"api key\"}", agentAPIKeysVar)
	}
	for name, key := range keys {
		if name == "" || key == "" {
			return nil, fmt.Errorf("%s has an entry with an empty name or key", agentAPIKeysVar)
		}
		if !validAgentCredentialName(name) {
			return nil, fmt.Errorf("%s has an entry with an invalid credential name", agentAPIKeysVar)
		}
	}
	return keys, nil
}

func validAgentCredentialName(name string) bool {
	if name == "" || len([]rune(name)) > maxAgentCredentialNameRunes {
		return false
	}
	for _, character := range name {
		switch {
		case character >= 'a' && character <= 'z',
			character >= 'A' && character <= 'Z',
			character >= '0' && character <= '9',
			character == '_', character == '-', character == '.':
			continue
		default:
			return false
		}
	}
	return true
}
