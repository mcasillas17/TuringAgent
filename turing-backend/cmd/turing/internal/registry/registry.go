// Package registry lists every setting the backend reads from .env or Compose:
// where it is changed, what kind of value it takes, and whether it is a
// secret. The command uses it to remove these names from the environment it
// hands to Compose, to hide secret values, and to drive `turing config`.
// Tests keep it in agreement with .env.example and the compose file.
package registry

import (
	"regexp"
	"strings"

	"github.com/mcasillas17/TuringAgent/turing-backend/cmd/turing/internal/envfile"
)

// Kind is the shape of a setting's value.
type Kind string

const (
	Text      Kind = "text"
	Number    Kind = "number"
	Duration  Kind = "duration"
	Boolean   Kind = "boolean"
	Choice    Kind = "choice"
	URL       Kind = "url"
	Secret    Kind = "secret"
	SecretMap Kind = "secret-map"
)

// Key describes one setting. Section is empty unless the key is editable.
// Deprecated lists older names the services still read as a fallback.
type Key struct {
	Name       string
	Section    string
	Kind       Kind
	Editable   bool
	Generated  bool
	Choices    []string
	Deprecated []string
}

const (
	sectionPort   = "App port"
	sectionLocal  = "Local engine"
	sectionRemote = "Remote model"
	sectionAgents = "External agents"
	sectionTeam   = "Agent team"
	sectionQueue  = "Queue, limits and timeouts"
)

// Sections are the editable sections, in menu order.
var Sections = []string{sectionPort, sectionLocal, sectionRemote, sectionAgents, sectionTeam, sectionQueue}

func generated(name string) Key {
	return Key{Name: name, Kind: Secret, Generated: true}
}

func wiring(name string, kind Kind) Key {
	return Key{Name: name, Kind: kind}
}

func setting(name, section string, kind Kind) Key {
	return Key{Name: name, Section: section, Kind: kind, Editable: true}
}

func (k Key) withChoices(choices ...string) Key {
	k.Choices = choices
	return k
}

func (k Key) replaces(old string) Key {
	k.Deprecated = append(k.Deprecated, old)
	return k
}

// table follows .env.example, then the keys only Compose names.
var table = []Key{
	generated("TURING_CLIENT_API_KEY"),
	generated("TURING_RUNTIME_TOKEN"),
	generated("TURING_APPROVAL_CONSUMER_TOKEN"),
	generated("TURING_MCP_FILES_CLEANUP_TOKEN"),
	generated("MCP_SYSTEM_TOKEN_GENERAL"),
	generated("MCP_FILES_TOKEN_GENERAL"),
	generated("TURING_APPROVAL_JWT_SECRET"),
	generated("TURING_EGRESS_SIGNING_SECRET"),
	generated("TURING_CURSOR_HMAC_SECRET"),
	generated("TURING_INTEGRATION_KEY"),

	wiring("ORCHESTRATOR_GRPC_ADDR", Text),
	setting("ORCHESTRATOR_PUBLIC_PORT", sectionPort, Number),
	wiring("ORCHESTRATOR_INTERNAL_PORT", Number),
	wiring("DATABASE_PATH", Text),
	wiring("MCP_CONFIG_ROOT", Text),
	wiring("FILES_SANDBOX_ROOT", Text),
	wiring("MEMORY_DISPLAY_ROOT", Text),
	wiring("HOST_UID", Number),
	wiring("HOST_GID", Number),
	wiring("MCP_SYSTEM_BASE_URL", URL),
	wiring("MCP_FILES_BASE_URL", URL),

	setting("TURING_LOCAL_ENGINE", sectionLocal, Choice).withChoices("ollama", "openai"),
	setting("TURING_LOCAL_ENGINE_URL", sectionLocal, URL).replaces("OLLAMA_BASE_URL"),
	setting("TURING_LOCAL_MODEL", sectionLocal, Text).replaces("OLLAMA_MODEL"),
	setting("TURING_LOCAL_ENGINE_LABEL", sectionLocal, Text),
	setting("TURING_LOCAL_ENGINE_API_KEY", sectionLocal, Secret),
	setting("OLLAMA_KEEP_ALIVE", sectionLocal, Duration),
	setting("TURING_LOCAL_CONTEXT_WINDOW_TOKENS", sectionLocal, Number).replaces("OLLAMA_CONTEXT_WINDOW_TOKENS"),
	setting("TURING_LOCAL_MAX_OUTPUT_TOKENS", sectionLocal, Number).replaces("OLLAMA_MAX_OUTPUT_TOKENS"),

	setting("OPENAI_BASE_URL", sectionRemote, URL),
	setting("OPENAI_API_KEY", sectionRemote, Secret),
	setting("OPENAI_MODEL", sectionRemote, Text),
	setting("OPENAI_CONTEXT_WINDOW_TOKENS", sectionRemote, Number),
	setting("OPENAI_MAX_OUTPUT_TOKENS", sectionRemote, Number),

	setting("TURING_AGENT_API_KEYS", sectionAgents, SecretMap),

	setting("TURING_JOB_TIMEOUT_MS", sectionQueue, Number),
	setting("TURING_JOB_REAPER_INTERVAL_MS", sectionQueue, Number),
	setting("TURING_QUEUE_NO_WORKER_TIMEOUT_MS", sectionQueue, Number),
	setting("TURING_QUEUE_MAX_WAIT_MS", sectionQueue, Number),
	setting("TURING_QUEUE_TIMEOUT_POLICY", sectionQueue, Choice).withChoices("fail", "cancel"),
	setting("TURING_AUTOMATION_TICK_MS", sectionQueue, Number),
	setting("TURING_JOB_MAX_ATTEMPTS", sectionQueue, Number),
	wiring("TURING_MAX_CONCURRENT_RUNS_GENERAL", Number),
	setting("TURING_MAX_TOOL_CALLS_PER_RUN", sectionQueue, Number),
	setting("TURING_AGENT_TEAM_ENABLED", sectionTeam, Boolean),
	setting("TURING_MAX_DELEGATIONS_PER_RUN", sectionTeam, Number),
	setting("TURING_DELEGATION_RESULT_MAX_BYTES", sectionTeam, Number),
	setting("TURING_MODEL_TIMEOUT_MS", sectionQueue, Number),
	setting("TURING_TOOL_TIMEOUT_MS", sectionQueue, Number),
	setting("TURING_APPROVAL_TIMEOUT_MS", sectionQueue, Number),
	setting("TURING_APPROVAL_WAIT_TIMEOUT_MS", sectionQueue, Number),
	setting("TURING_TOOL_TOTAL_TIMEOUT_MS", sectionQueue, Number),

	wiring("TURING_WORKER_ID", Text),
}

func (k Key) clone() Key {
	k.Choices = append([]string(nil), k.Choices...)
	k.Deprecated = append([]string(nil), k.Deprecated...)
	return k
}

// All returns every key, in table order. The result is the caller's to change.
func All() []Key {
	keys := make([]Key, len(table))
	for i, key := range table {
		keys[i] = key.clone()
	}
	return keys
}

// Lookup finds a key by its current name. A deprecated name is not a key.
func Lookup(name string) (Key, bool) {
	for _, key := range table {
		if key.Name == name {
			return key.clone(), true
		}
	}
	return Key{}, false
}

// Editable returns the keys `turing config` may change, in table order.
func Editable() []Key {
	var keys []Key
	for _, key := range table {
		if key.Editable {
			keys = append(keys, key.clone())
		}
	}
	return keys
}

// StripNames returns every key name and every deprecated name: the variables
// to remove from the environment before running Compose, which would
// otherwise prefer an exported one to .env.
func StripNames() []string {
	var names []string
	for _, key := range table {
		names = append(names, key.Name)
		names = append(names, key.Deprecated...)
	}
	return names
}

// IsSecret reports whether the key's value must never be printed.
func (k Key) IsSecret() bool {
	return k.Kind == Secret || k.Kind == SecretMap || k.Generated
}

// commentedAssignment matches an example such as `# KEY=value`. A value has no
// spaces, so a wrapped sentence that happens to begin `KEY=true neither` is
// still text.
var commentedAssignment = regexp.MustCompile(`^#\s*([A-Za-z_][A-Za-z0-9_]*)=\S*$`)

// Describe returns the comment directly above name in .env.example, with the
// leading `# ` removed, or "" when there is none.
//
// The comment is the run of `#` lines above the assignment. A commented
// example of the same key inside it is dropped. Another key's commented
// example ends the run, except that a key which is itself only an example
// skips the examples directly above it, to reach the text that introduces
// the block.
func Describe(example []byte, name string) string {
	lines := splitLines(example)
	at, isExample := -1, false
	for i, line := range lines {
		if strings.HasPrefix(line, name+"=") {
			at = i
			break
		}
	}
	if at < 0 {
		for i, line := range lines {
			if match := commentedAssignment.FindStringSubmatch(line); match != nil && match[1] == name {
				at, isExample = i, true
				break
			}
		}
	}
	if at < 0 {
		return ""
	}

	var above []string
	for i := at - 1; i >= 0 && strings.HasPrefix(lines[i], "#"); i-- {
		if match := commentedAssignment.FindStringSubmatch(lines[i]); match != nil {
			if match[1] == name || (isExample && len(above) == 0) {
				continue
			}
			break
		}
		above = append(above, strings.TrimPrefix(strings.TrimPrefix(lines[i], "#"), " "))
	}
	for i, j := 0, len(above)-1; i < j; i, j = i+1, j-1 {
		above[i], above[j] = above[j], above[i]
	}
	return strings.TrimSpace(strings.Join(above, "\n"))
}

func splitLines(data []byte) []string {
	lines := strings.Split(string(data), "\n")
	for i, line := range lines {
		lines[i] = strings.TrimSuffix(line, "\r")
	}
	return lines
}

// Shipped returns the value .env.example assigns to name, read the way Compose
// reads it, or "" when the key is absent or only shown as a commented example.
func Shipped(example []byte, name string) string {
	file, err := envfile.Parse(example)
	if err != nil {
		return ""
	}
	value, _ := file.Get(name)
	return value
}
