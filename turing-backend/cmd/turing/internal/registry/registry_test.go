package registry

import (
	"os"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"
)

const (
	envExamplePath = "../../../../.env.example"
	composePath    = "../../../../infra/docker-compose.yml"
	initScriptPath = "../../../../scripts/init.sh"
)

func readFile(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func classified(name string) bool {
	if _, ok := Lookup(name); ok {
		return true
	}
	for _, key := range All() {
		for _, old := range key.Deprecated {
			if old == name {
				return true
			}
		}
	}
	return false
}

// exampleKeys returns every key .env.example assigns or shows commented out.
// A commented example has no space in its value; a wrapped sentence that
// begins `KEY=true neither` is prose.
func exampleKeys(t *testing.T) []string {
	t.Helper()
	pattern := regexp.MustCompile(`^(?:#\s*)?([A-Z][A-Z0-9_]*)=`)
	var keys []string
	for _, line := range strings.Split(string(readFile(t, envExamplePath)), "\n") {
		match := pattern.FindStringSubmatch(line)
		if match == nil || (strings.HasPrefix(line, "#") && strings.ContainsAny(line[strings.Index(line, "="):], " \t")) {
			continue
		}
		keys = append(keys, match[1])
	}
	if len(keys) == 0 {
		t.Fatal("found no key in .env.example; the scan is broken")
	}
	return keys
}

// interpolations returns every name a non-comment line of the compose file
// interpolates.
func interpolations(t *testing.T) map[string]bool {
	t.Helper()
	pattern := regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)`)
	names := map[string]bool{}
	for _, line := range strings.Split(string(readFile(t, composePath)), "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}
		for _, match := range pattern.FindAllStringSubmatch(line, -1) {
			names[match[1]] = true
		}
	}
	if len(names) == 0 {
		t.Fatal("found no interpolation in the compose file; the scan is broken")
	}
	return names
}

func TestEveryEnvExampleKeyIsClassified(t *testing.T) {
	var missing []string
	for _, name := range exampleKeys(t) {
		if !classified(name) {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		t.Fatalf(".env.example keys missing from the registry: %v", missing)
	}
}

func TestEveryComposeInterpolationIsClassified(t *testing.T) {
	var missing []string
	for name := range interpolations(t) {
		if !classified(name) {
			missing = append(missing, name)
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Fatalf("compose interpolations missing from the registry: %v", missing)
	}
}

func TestEveryRegistryKeyIsKnownToTheFiles(t *testing.T) {
	compose := interpolations(t)
	example := map[string]bool{}
	for _, name := range exampleKeys(t) {
		example[name] = true
	}
	for _, key := range All() {
		if !compose[key.Name] && !example[key.Name] {
			t.Errorf("%s is in neither .env.example nor the compose file", key.Name)
		}
		for _, old := range key.Deprecated {
			if !compose[old] {
				t.Errorf("deprecated %s of %s is not passed through by the compose file", old, key.Name)
			}
		}
	}
}

// serviceBlock returns the lines of one top-level service, comments removed.
func serviceBlock(t *testing.T, compose, service string) string {
	t.Helper()
	var block []string
	inside := false
	for _, line := range strings.Split(compose, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if indent := len(line) - len(strings.TrimLeft(line, " ")); indent <= 2 {
			inside = line == "  "+service+":"
			continue
		}
		if inside {
			block = append(block, line)
		}
	}
	if len(block) == 0 {
		t.Fatalf("no service %q in the compose file", service)
	}
	return strings.Join(block, "\n")
}

func mentions(block, name string) bool {
	return regexp.MustCompile(`\b` + name + `\b`).MatchString(block)
}

func TestEditableKeysReachOnlyTheOrchestratorAndRuntime(t *testing.T) {
	compose := string(readFile(t, composePath))
	reached := map[string]bool{}
	for _, service := range []string{"turing-orchestrator", "turing-agent-runtime-general"} {
		block := serviceBlock(t, compose, service)
		for _, key := range Editable() {
			if mentions(block, key.Name) {
				reached[key.Name] = true
			}
		}
	}
	for _, service := range []string{"turing-mcp-files", "turing-mcp-system"} {
		block := serviceBlock(t, compose, service)
		for _, key := range Editable() {
			if mentions(block, key.Name) {
				t.Errorf("editable key %s reaches %s", key.Name, service)
			}
		}
	}
	for _, key := range Editable() {
		if !reached[key.Name] {
			t.Errorf("editable key %s reaches neither the orchestrator nor the runtime", key.Name)
		}
	}
}

func TestConcurrencyIsNotEditable(t *testing.T) {
	for _, name := range []string{"TURING_MAX_CONCURRENT_RUNS_GENERAL", "TURING_WORKER_ID"} {
		key, ok := Lookup(name)
		if !ok {
			t.Fatalf("%s is not in the registry", name)
		}
		if key.Editable {
			t.Errorf("%s must not be editable", name)
		}
		for _, editable := range Editable() {
			if editable.Name == name {
				t.Errorf("Editable() lists %s", name)
			}
		}
	}
}

func TestGeneratedKeysMatchInitSh(t *testing.T) {
	ensured := map[string]bool{}
	pattern := regexp.MustCompile(`(?m)^ensure_var ([A-Z][A-Z0-9_]*) `)
	for _, match := range pattern.FindAllStringSubmatch(string(readFile(t, initScriptPath)), -1) {
		ensured[match[1]] = true
	}
	if len(ensured) == 0 {
		t.Fatal("found no ensure_var in init.sh; the scan is broken")
	}
	generated := map[string]bool{}
	for _, key := range All() {
		if !key.Generated {
			continue
		}
		generated[key.Name] = true
		if key.Editable {
			t.Errorf("generated key %s must not be editable", key.Name)
		}
		if !key.IsSecret() {
			t.Errorf("generated key %s must be treated as a secret", key.Name)
		}
	}
	if !reflect.DeepEqual(ensured, generated) {
		t.Fatalf("init.sh ensure_vars %v, registry generates %v", ensured, generated)
	}
}

func TestEveryEditableKeyHasADescription(t *testing.T) {
	example := readFile(t, envExamplePath)
	for _, key := range Editable() {
		if Describe(example, key.Name) == "" {
			t.Errorf("%s has no comment above it in .env.example", key.Name)
		}
	}
}

func TestShippedReadsTheExampleFile(t *testing.T) {
	example := readFile(t, envExamplePath)
	for name, want := range map[string]string{
		"ORCHESTRATOR_PUBLIC_PORT":    "3000",
		"TURING_LOCAL_ENGINE":         "ollama",
		"OLLAMA_KEEP_ALIVE":           "2m",
		"TURING_QUEUE_TIMEOUT_POLICY": "fail",
		"TURING_AGENT_TEAM_ENABLED":   "false",
		"OPENAI_API_KEY":              "",
		"TURING_LOCAL_ENGINE_LABEL":   "",
	} {
		if got := Shipped(example, name); got != want {
			t.Errorf("Shipped(%s) = %q, want %q", name, got, want)
		}
	}
}

const fixture = `# A banner.

# The port.
# Second line of the port.
PORT=3000
# Examples:
# OTHER=1
#   PORT=4000
# The mode, either kind.
# MODE=fast
#
# Fine print.
MODE='a b'
NOTE=bare
NOTE="last one"
# Lead-in:
# FIRST=a
# Own text.
# SECOND=b
# THIRD=c
PLAIN=1
# Wrapped, so a line
# OTHER=true can start with a key name.
WRAP=1
LAST=1`

func TestDescribeAndShipped(t *testing.T) {
	data := []byte(fixture)
	for _, tc := range []struct {
		name string
		want string
	}{
		{"PORT", "The port.\nSecond line of the port."},
		// The key's own example is dropped; another key's example ends the block.
		{"MODE", "The mode, either kind.\n\nFine print."},
		{"NOTE", ""},
		// A commented example skips the examples above it to reach the lead-in.
		{"FIRST", "Lead-in:"},
		{"SECOND", "Own text."},
		{"THIRD", "Own text."},
		// An assigned key does not borrow the lead-in of an example block.
		{"PLAIN", ""},
		// A wrapped sentence that begins with a key name is text, not an example.
		{"WRAP", "Wrapped, so a line\nOTHER=true can start with a key name."},
		{"LAST", ""},
		{"ABSENT", ""},
	} {
		if got := Describe(data, tc.name); got != tc.want {
			t.Errorf("Describe(%s) = %q, want %q", tc.name, got, tc.want)
		}
	}

	for _, tc := range []struct {
		name string
		want string
	}{
		{"PORT", "3000"},
		{"MODE", "a b"},
		{"NOTE", "last one"},
		{"FIRST", ""},
		{"OTHER", ""},
		{"ABSENT", ""},
	} {
		if got := Shipped(data, tc.name); got != tc.want {
			t.Errorf("Shipped(%s) = %q, want %q", tc.name, got, tc.want)
		}
	}
	if got := Shipped([]byte("A=\x00"), "A"); got != "" {
		t.Errorf("Shipped of an unreadable file = %q, want empty", got)
	}
}

// turing config shows this description beside the URL, so it has to say how
// the containers reach an engine on this machine: localhost inside a
// container is the container.
func TestTheEngineURLDescriptionNamesHostDockerInternal(t *testing.T) {
	got := strings.Join(strings.Fields(Describe(readFile(t, envExamplePath), "TURING_LOCAL_ENGINE_URL")), " ")
	for _, want := range []string{"use host.docker.internal", "inside a container, localhost and 127.0.0.1 are the container itself"} {
		if !strings.Contains(got, want) {
			t.Errorf("Describe(TURING_LOCAL_ENGINE_URL) = %q, want it to say %q", got, want)
		}
	}
}

func TestDescribeReadsTheWholeSharedQueueComment(t *testing.T) {
	got := Describe(readFile(t, envExamplePath), "TURING_QUEUE_NO_WORKER_TIMEOUT_MS")
	for _, want := range []string{"How long a queued message may wait", "even with the team turned off."} {
		if !strings.Contains(got, want) {
			t.Errorf("Describe lost %q from the shared queue comment:\n%s", want, got)
		}
	}
}

func TestDescribeHandlesWindowsLineEndings(t *testing.T) {
	got := Describe([]byte("# The port.\r\nPORT=3000\r\n"), "PORT")
	if got != "The port." {
		t.Errorf("Describe = %q, want %q", got, "The port.")
	}
}

func TestRegistryShape(t *testing.T) {
	names := map[string]bool{}
	deprecated := map[string]bool{}
	inSections := map[string]bool{}
	for _, section := range Sections {
		inSections[section] = true
	}
	usedSections := map[string]bool{}
	for _, key := range All() {
		if names[key.Name] {
			t.Errorf("%s is listed twice", key.Name)
		}
		names[key.Name] = true
		if key.Kind == "" {
			t.Errorf("%s has no kind", key.Name)
		}
		if key.Editable {
			if !inSections[key.Section] {
				t.Errorf("editable %s has section %q, not one of %v", key.Name, key.Section, Sections)
			}
			usedSections[key.Section] = true
		} else if key.Section != "" {
			t.Errorf("%s is not editable but has section %q", key.Name, key.Section)
		}
		if (key.Kind == Choice) != (len(key.Choices) > 0) {
			t.Errorf("%s: kind %q with choices %v", key.Name, key.Kind, key.Choices)
		}
		for _, old := range key.Deprecated {
			if deprecated[old] {
				t.Errorf("deprecated %s is listed twice", old)
			}
			deprecated[old] = true
		}
	}
	for old := range deprecated {
		if names[old] {
			t.Errorf("%s is both a key and a deprecated name", old)
		}
	}
	for _, section := range Sections {
		if !usedSections[section] {
			t.Errorf("section %q has no editable key", section)
		}
	}
}

func TestSectionsMatchTheSpecTable(t *testing.T) {
	wantOrder := []string{"App port", "Local engine", "Remote model", "External agents", "Agent team", "Queue, limits and timeouts"}
	if !reflect.DeepEqual(Sections, wantOrder) {
		t.Fatalf("Sections = %v, want %v", Sections, wantOrder)
	}
	want := map[string][]string{
		"App port":        {"ORCHESTRATOR_PUBLIC_PORT"},
		"Local engine":    {"TURING_LOCAL_ENGINE", "TURING_LOCAL_ENGINE_URL", "TURING_LOCAL_MODEL", "TURING_LOCAL_ENGINE_LABEL", "TURING_LOCAL_ENGINE_API_KEY", "OLLAMA_KEEP_ALIVE", "TURING_LOCAL_CONTEXT_WINDOW_TOKENS", "TURING_LOCAL_MAX_OUTPUT_TOKENS"},
		"Remote model":    {"OPENAI_BASE_URL", "OPENAI_API_KEY", "OPENAI_MODEL", "OPENAI_CONTEXT_WINDOW_TOKENS", "OPENAI_MAX_OUTPUT_TOKENS"},
		"External agents": {"TURING_AGENT_API_KEYS"},
		"Agent team":      {"TURING_AGENT_TEAM_ENABLED", "TURING_MAX_DELEGATIONS_PER_RUN", "TURING_DELEGATION_RESULT_MAX_BYTES"},
		"Queue, limits and timeouts": {
			"TURING_JOB_TIMEOUT_MS", "TURING_JOB_REAPER_INTERVAL_MS", "TURING_QUEUE_NO_WORKER_TIMEOUT_MS",
			"TURING_QUEUE_MAX_WAIT_MS", "TURING_QUEUE_TIMEOUT_POLICY", "TURING_AUTOMATION_TICK_MS",
			"TURING_JOB_MAX_ATTEMPTS", "TURING_MAX_TOOL_CALLS_PER_RUN", "TURING_MODEL_TIMEOUT_MS",
			"TURING_TOOL_TIMEOUT_MS", "TURING_APPROVAL_TIMEOUT_MS", "TURING_APPROVAL_WAIT_TIMEOUT_MS",
			"TURING_TOOL_TOTAL_TIMEOUT_MS",
		},
	}
	got := map[string][]string{}
	for _, key := range Editable() {
		got[key.Section] = append(got[key.Section], key.Name)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("editable keys by section:\n got %v\nwant %v", got, want)
	}
}

func TestKindsAndSecrets(t *testing.T) {
	for _, tc := range []struct {
		name   string
		kind   Kind
		secret bool
	}{
		{"ORCHESTRATOR_PUBLIC_PORT", Number, false},
		{"TURING_LOCAL_ENGINE", Choice, false},
		{"TURING_LOCAL_ENGINE_URL", URL, false},
		{"TURING_LOCAL_ENGINE_API_KEY", Secret, true},
		{"OLLAMA_KEEP_ALIVE", Duration, false},
		{"OPENAI_BASE_URL", URL, false},
		{"OPENAI_API_KEY", Secret, true},
		{"TURING_AGENT_API_KEYS", SecretMap, true},
		{"TURING_AGENT_TEAM_ENABLED", Boolean, false},
		{"TURING_QUEUE_TIMEOUT_POLICY", Choice, false},
		{"TURING_LOCAL_MODEL", Text, false},
		{"TURING_CLIENT_API_KEY", Secret, true},
		{"MEMORY_DISPLAY_ROOT", Text, false},
	} {
		key, ok := Lookup(tc.name)
		if !ok {
			t.Errorf("%s is not in the registry", tc.name)
			continue
		}
		if key.Kind != tc.kind {
			t.Errorf("%s kind = %q, want %q", tc.name, key.Kind, tc.kind)
		}
		if key.IsSecret() != tc.secret {
			t.Errorf("%s IsSecret = %v, want %v", tc.name, key.IsSecret(), tc.secret)
		}
	}
	for _, key := range All() {
		if (key.Kind == Secret || key.Kind == SecretMap) && !key.IsSecret() {
			t.Errorf("%s is a %s but not treated as a secret", key.Name, key.Kind)
		}
	}
}

func TestOnlyTheDocumentedKeysAreEditableSecrets(t *testing.T) {
	var got []string
	for _, key := range Editable() {
		if key.IsSecret() {
			got = append(got, key.Name)
		}
	}
	want := []string{"TURING_LOCAL_ENGINE_API_KEY", "OPENAI_API_KEY", "TURING_AGENT_API_KEYS"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("editable secrets = %v, want %v", got, want)
	}
}

func TestChoicesFollowTheServices(t *testing.T) {
	engine, _ := Lookup("TURING_LOCAL_ENGINE")
	if want := []string{"ollama", "openai"}; !reflect.DeepEqual(engine.Choices, want) {
		t.Errorf("TURING_LOCAL_ENGINE choices = %v, want %v", engine.Choices, want)
	}
	policy, _ := Lookup("TURING_QUEUE_TIMEOUT_POLICY")
	if want := []string{"fail", "cancel"}; !reflect.DeepEqual(policy.Choices, want) {
		t.Errorf("TURING_QUEUE_TIMEOUT_POLICY choices = %v, want %v", policy.Choices, want)
	}

	// The services' packages are internal to their own trees, so the registry
	// cannot import them; this keeps its copy from drifting.
	for _, tc := range []struct {
		path    string
		pattern string
		key     Key
	}{
		{"../../../../agent-runtime-go/internal/llm/local.go", `LocalEngine\w+\s*=\s*"(\w+)"`, engine},
		{"../../../../orchestrator-go/internal/runoutcome/outcome.go", `QueueTimeoutPolicy\w+\s+QueueTimeoutPolicy\s*=\s*"(\w+)"`, policy},
	} {
		var values []string
		for _, match := range regexp.MustCompile(tc.pattern).FindAllStringSubmatch(string(readFile(t, tc.path)), -1) {
			values = append(values, match[1])
		}
		if !reflect.DeepEqual(values, tc.key.Choices) {
			t.Errorf("%s: the service defines %v, the registry lists %v", tc.key.Name, values, tc.key.Choices)
		}
	}
}

func TestDeprecatedNames(t *testing.T) {
	for key, old := range map[string]string{
		"TURING_LOCAL_ENGINE_URL":            "OLLAMA_BASE_URL",
		"TURING_LOCAL_MODEL":                 "OLLAMA_MODEL",
		"TURING_LOCAL_CONTEXT_WINDOW_TOKENS": "OLLAMA_CONTEXT_WINDOW_TOKENS",
		"TURING_LOCAL_MAX_OUTPUT_TOKENS":     "OLLAMA_MAX_OUTPUT_TOKENS",
	} {
		k, ok := Lookup(key)
		if !ok || !reflect.DeepEqual(k.Deprecated, []string{old}) {
			t.Errorf("%s deprecated = %v, want [%s]", key, k.Deprecated, old)
		}
		if _, ok := Lookup(old); ok {
			t.Errorf("%s is deprecated and must not be a key of its own", old)
		}
	}
}

func TestStripNamesCoverEveryKeyAndDeprecatedName(t *testing.T) {
	strip := map[string]bool{}
	for _, name := range StripNames() {
		if strip[name] {
			t.Errorf("StripNames lists %s twice", name)
		}
		strip[name] = true
	}
	for _, key := range All() {
		if !strip[key.Name] {
			t.Errorf("StripNames omits %s", key.Name)
		}
		for _, old := range key.Deprecated {
			if !strip[old] {
				t.Errorf("StripNames omits deprecated %s", old)
			}
		}
	}
	for _, name := range []string{"OLLAMA_BASE_URL", "TURING_WORKER_ID", "TURING_CLIENT_API_KEY", "ORCHESTRATOR_PUBLIC_PORT"} {
		if !strip[name] {
			t.Errorf("StripNames omits %s", name)
		}
	}
}

func TestEditableKeepsTableOrderAndLookupFindsEveryKey(t *testing.T) {
	var editable []string
	for _, key := range All() {
		if got, ok := Lookup(key.Name); !ok || !reflect.DeepEqual(got, key) {
			t.Errorf("Lookup(%s) = %v, %v", key.Name, got, ok)
		}
		if key.Editable {
			editable = append(editable, key.Name)
		}
	}
	var listed []string
	for _, key := range Editable() {
		listed = append(listed, key.Name)
	}
	if !reflect.DeepEqual(listed, editable) {
		t.Errorf("Editable() = %v, want %v", listed, editable)
	}
	if _, ok := Lookup("NOT_A_KEY"); ok {
		t.Error("Lookup found a key that does not exist")
	}
	if _, ok := Lookup("OLLAMA_BASE_URL"); ok {
		t.Error("Lookup found a deprecated name as a key")
	}
}

func TestCallersCannotChangeTheRegistry(t *testing.T) {
	all := All()
	all[0].Name = "CHANGED"
	if got := All()[0].Name; got == "CHANGED" {
		t.Error("All() shares its slice with the registry")
	}
	engine, _ := Lookup("TURING_LOCAL_ENGINE")
	engine.Choices[0] = "changed"
	if again, _ := Lookup("TURING_LOCAL_ENGINE"); again.Choices[0] == "changed" {
		t.Error("Lookup shares its choices with the registry")
	}
	url, _ := Lookup("TURING_LOCAL_ENGINE_URL")
	url.Deprecated[0] = "CHANGED"
	if again, _ := Lookup("TURING_LOCAL_ENGINE_URL"); again.Deprecated[0] == "CHANGED" {
		t.Error("Lookup shares its deprecated names with the registry")
	}
}
