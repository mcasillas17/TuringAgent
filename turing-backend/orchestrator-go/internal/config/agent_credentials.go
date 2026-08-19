package config

import (
	"encoding/json"
	"fmt"
	"sort"
)

// AgentAPIKeysVar is the single environment variable holding every third-party
// API key an external agent can use, as a JSON object of
// {"<credential name>": "<api key>"}.
//
// One variable rather than one per agent, because adding an agent in the UI
// must not require editing docker-compose.yml: compose passes an explicit list
// of names, so a per-agent variable would make every new agent a code change.
// It lives in turing-backend/.env, which init.sh creates chmod 600 and
// .gitignore excludes.
const AgentAPIKeysVar = "TURING_AGENT_API_KEYS"

// ParseAgentAPIKeys decodes AgentAPIKeysVar. An empty or unset value is not an
// error — it is the normal state of an install with no cloud agents.
func ParseAgentAPIKeys(raw string) (map[string]string, error) {
	if raw == "" {
		return map[string]string{}, nil
	}
	keys := map[string]string{}
	if err := json.Unmarshal([]byte(raw), &keys); err != nil {
		// The error deliberately does not wrap the decoder's message: a JSON
		// syntax error quotes the offending input, and the offending input
		// here is a file full of API keys.
		return nil, fmt.Errorf("%s must be a JSON object of {\"name\": \"api key\"}", AgentAPIKeysVar)
	}
	for name, key := range keys {
		if name == "" || key == "" {
			return nil, fmt.Errorf("%s has an entry with an empty name or key", AgentAPIKeysVar)
		}
	}
	return keys, nil
}

// AgentCredentialNames returns just the names, sorted, discarding the keys.
//
// This is what the orchestrator keeps. It needs to answer "will this agent
// work?" for the client, and it can answer that from names alone — so it never
// holds a third-party key in a field anything could later serialise.
func AgentCredentialNames(keys map[string]string) []string {
	names := make([]string, 0, len(keys))
	for name := range keys {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
