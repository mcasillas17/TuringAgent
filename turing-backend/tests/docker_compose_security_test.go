package tests

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

type composeDocument struct {
	Include  yaml.Node                 `yaml:"include"`
	Services map[string]composeService `yaml:"services"`
}

type composeService struct {
	User            string             `yaml:"user"`
	ReadOnly        bool               `yaml:"read_only"`
	CapDrop         []string           `yaml:"cap_drop"`
	CapAdd          []string           `yaml:"cap_add"`
	SecurityOpt     []string           `yaml:"security_opt"`
	Privileged      bool               `yaml:"privileged"`
	Volumes         []string           `yaml:"volumes"`
	Tmpfs           []string           `yaml:"tmpfs"`
	VolumesFrom     []string           `yaml:"volumes_from"`
	Environment     map[string]*string `yaml:"environment"`
	EnvironmentFile yaml.Node          `yaml:"env_file"`
	Extends         yaml.Node          `yaml:"extends"`
	Secrets         yaml.Node          `yaml:"secrets"`
}

func TestDockerComposeKeepsServiceSecretsLeastPrivilege(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "infra", "docker-compose.yml"))
	if err != nil {
		t.Fatal(err)
	}
	compose := string(data)

	orchestrator := composeServiceBlock(t, compose, "turing-orchestrator")
	requireNoEnvFile(t, "turing-orchestrator", orchestrator)
	requireContainsAll(t, "turing-orchestrator", orchestrator,
		"TURING_CLIENT_API_KEY:",
		"TURING_INTERNAL_TOKEN:",
		"MCP_SYSTEM_TOKEN_GENERAL:",
		"MCP_FILES_TOKEN_GENERAL:",
		"TURING_APPROVAL_JWT_SECRET:",
		"DATABASE_PATH:",
		"OLLAMA_BASE_URL:",
		"OPENAI_API_KEY:",
		"TURING_AGENT_API_KEYS:",
	)
	requireContainsNone(t, "turing-orchestrator", orchestrator,
		"ORCHESTRATOR_GRPC_ADDR:",
		"MCP_SYSTEM_BASE_URL:",
		"MCP_FILES_BASE_URL:",
		"FILES_SANDBOX_ROOT:",
	)

	agent := composeServiceBlock(t, compose, "turing-agent-runtime-general")
	requireNoEnvFile(t, "turing-agent-runtime-general", agent)
	requireContainsAll(t, "turing-agent-runtime-general", agent,
		"TURING_INTERNAL_TOKEN:",
		"ORCHESTRATOR_GRPC_ADDR:",
		"MCP_SYSTEM_BASE_URL:",
		"MCP_FILES_BASE_URL:",
		"MCP_SYSTEM_TOKEN_GENERAL:",
		"MCP_FILES_TOKEN_GENERAL:",
		"OLLAMA_BASE_URL:",
		"OPENAI_API_KEY:",
		"TURING_AGENT_API_KEYS:",
	)
	requireContainsNone(t, "turing-agent-runtime-general", agent,
		"TURING_CLIENT_API_KEY:",
		"TURING_APPROVAL_JWT_SECRET:",
	)

	system := composeServiceBlock(t, compose, "turing-mcp-system")
	requireNoEnvFile(t, "turing-mcp-system", system)
	requireContainsAll(t, "turing-mcp-system", system, "MCP_SYSTEM_TOKEN_GENERAL:")
	requireContainsNone(t, "turing-mcp-system", system,
		"TURING_CLIENT_API_KEY:",
		"TURING_INTERNAL_TOKEN:",
		"TURING_APPROVAL_JWT_SECRET:",
		"OPENAI_API_KEY:",
		// A tool server has no reason to hold a user's third-party API keys,
		// and it is the container most exposed to what a model asks for.
		"TURING_AGENT_API_KEYS:",
	)

	files := composeServiceBlock(t, compose, "turing-mcp-files")
	requireNoEnvFile(t, "turing-mcp-files", files)
	requireContainsAll(t, "turing-mcp-files", files,
		"build:",
		"context: ../..",
		"dockerfile: turing-backend/mcp-files/Dockerfile",
		"MCP_FILES_TOKEN_GENERAL:",
		"TURING_APPROVAL_JWT_SECRET:",
		"TURING_INTERNAL_TOKEN:",
		"ORCHESTRATOR_GRPC_ADDR:",
		"FILES_SANDBOX_ROOT: /sandbox",
	)
	requireContainsNone(t, "turing-mcp-files", files,
		"TURING_CLIENT_API_KEY:",
		"OPENAI_API_KEY:",
		"TURING_AGENT_API_KEYS:",
		"ORCHESTRATOR_INTERNAL_BASE_URL:",
		"${FILES_SANDBOX_ROOT",
	)

	allowedEnvironment := map[string][]string{
		"turing-orchestrator": {
			"TURING_CLIENT_API_KEY",
			"TURING_INTERNAL_TOKEN",
			"MCP_SYSTEM_TOKEN_GENERAL",
			"MCP_FILES_TOKEN_GENERAL",
			"TURING_APPROVAL_JWT_SECRET",
			"TURING_INTEGRATION_KEY",
			"ORCHESTRATOR_PUBLIC_PORT",
			"ORCHESTRATOR_INTERNAL_PORT",
			"DATABASE_PATH",
			"OLLAMA_BASE_URL",
			"OLLAMA_MODEL",
			"OPENAI_BASE_URL",
			"OPENAI_API_KEY",
			"OPENAI_MODEL",
			"TURING_AGENT_API_KEYS",
			"TURING_JOB_TIMEOUT_MS",
			"TURING_JOB_REAPER_INTERVAL_MS",
			"TURING_AUTOMATION_TICK_MS",
			"TURING_JOB_MAX_ATTEMPTS",
			"TURING_MAX_CONCURRENT_RUNS_GENERAL",
			"TURING_MAX_TOOL_CALLS_PER_RUN",
			"TURING_MODEL_TIMEOUT_MS",
			"TURING_TOOL_TIMEOUT_MS",
			"TURING_APPROVAL_TIMEOUT_MS",
			"LOG_LEVEL",
		},
		"turing-agent-runtime-general": {
			"TURING_INTERNAL_TOKEN",
			"ORCHESTRATOR_GRPC_ADDR",
			"TURING_WORKER_ID",
			"TURING_JOB_TIMEOUT_MS",
			"TURING_MAX_CONCURRENT_RUNS_GENERAL",
			"TURING_MAX_TOOL_CALLS_PER_RUN",
			"TURING_MODEL_TIMEOUT_MS",
			"TURING_TOOL_TIMEOUT_MS",
			"TURING_APPROVAL_TIMEOUT_MS",
			"TURING_APPROVAL_WAIT_TIMEOUT_MS",
			"TURING_TOOL_TOTAL_TIMEOUT_MS",
			"MCP_SYSTEM_BASE_URL",
			"MCP_FILES_BASE_URL",
			"MCP_SYSTEM_TOKEN_GENERAL",
			"MCP_FILES_TOKEN_GENERAL",
			"OLLAMA_BASE_URL",
			"OLLAMA_MODEL",
			"OLLAMA_KEEP_ALIVE",
			"OPENAI_BASE_URL",
			"OPENAI_API_KEY",
			"OPENAI_MODEL",
			"TURING_AGENT_API_KEYS",
			"LOG_LEVEL",
		},
		"turing-mcp-system": {
			"MCP_SYSTEM_TOKEN_GENERAL",
			"LOG_LEVEL",
			"LOG_PRETTY",
		},
		"turing-mcp-files": {
			"MCP_FILES_TOKEN_GENERAL",
			"TURING_APPROVAL_JWT_SECRET",
			"TURING_INTERNAL_TOKEN",
			"ORCHESTRATOR_GRPC_ADDR",
			"FILES_SANDBOX_ROOT",
			"LOG_LEVEL",
			"LOG_PRETTY",
		},
	}
	document := decodeComposeDocument(t, compose)
	for _, serviceName := range sortedServiceNames(document) {
		service := document.Services[serviceName]
		allowed, known := allowedEnvironment[serviceName]
		if !known {
			t.Errorf("%s has no explicit environment policy", serviceName)
			continue
		}
		if yamlNodePresent(service.EnvironmentFile) {
			t.Errorf("%s uses env_file instead of explicit environment entries", serviceName)
		}
		if yamlNodePresent(service.Secrets) {
			t.Errorf("%s uses unapproved Compose secrets", serviceName)
		}
		actual := make([]string, 0, len(service.Environment))
		for name, value := range service.Environment {
			actual = append(actual, name)
			if value == nil {
				t.Errorf("%s inherits %s from the host environment", serviceName, name)
			}
		}
		sort.Strings(actual)
		sort.Strings(allowed)
		if !equalStrings(actual, allowed) {
			t.Errorf("%s environment = %v, want %v", serviceName, actual, allowed)
		}
	}
}

func TestEveryBackendImageRunsAsExplicitNonRootUser(t *testing.T) {
	tests := map[string]struct {
		path     string
		user     string
		snippets []string
	}{
		"turing-orchestrator": {
			path: filepath.Join("..", "orchestrator-go", "Dockerfile"),
			user: "turing-orchestrator:turing-orchestrator",
			snippets: []string{
				"groupadd --gid 1000 turing-orchestrator",
				"useradd --uid 1000 --gid 1000",
			},
		},
		"turing-agent-runtime-general": {
			path: filepath.Join("..", "agent-runtime-go", "Dockerfile"),
			user: "turing-agent-runtime:turing-agent-runtime",
			snippets: []string{
				"groupadd --gid 1000 turing-agent-runtime",
				"useradd --uid 1000 --gid 1000",
			},
		},
		"turing-mcp-system": {
			path: filepath.Join("..", "mcp-system", "Dockerfile"),
			user: "mcp-system:mcp-system",
			snippets: []string{
				"addgroup -g 1000 -S mcp-system",
				"adduser -u 1000 -S -G mcp-system mcp-system",
			},
		},
		"turing-mcp-files": {
			path: filepath.Join("..", "mcp-files", "Dockerfile"),
			user: "mcp-files:mcp-files",
			snippets: []string{
				"addgroup -g 1000 -S mcp-files",
				"adduser -u 1000 -S -G mcp-files mcp-files",
			},
		},
	}
	composeData, err := os.ReadFile(filepath.Join("..", "infra", "docker-compose.yml"))
	if err != nil {
		t.Fatal(err)
	}
	document := decodeComposeDocument(t, string(composeData))
	for _, serviceName := range sortedServiceNames(document) {
		t.Run(serviceName, func(t *testing.T) {
			test, known := tests[serviceName]
			if !known {
				t.Fatalf("%s has no Dockerfile non-root policy", serviceName)
			}
			data, err := os.ReadFile(test.path)
			if err != nil {
				t.Fatal(err)
			}
			dockerfile := string(data)
			for _, snippet := range test.snippets {
				if !strings.Contains(dockerfile, snippet) {
					t.Errorf("%s Dockerfile missing %q", serviceName, snippet)
				}
			}
			if got := dockerfileFinalUser(dockerfile); got != test.user {
				t.Errorf("%s final runtime USER = %q, want %q", serviceName, got, test.user)
			}
		})
	}
}

func TestEveryComposeServiceUsesLeastPrivilegeRuntime(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "infra", "docker-compose.yml"))
	if err != nil {
		t.Fatal(err)
	}
	document := decodeComposeDocument(t, string(data))
	for _, violation := range composeInheritanceViolations(document) {
		t.Error(violation)
	}
	allowedVolumeMounts := map[string][]string{
		"turing-orchestrator":          {"../data:/app/data"},
		"turing-agent-runtime-general": nil,
		"turing-mcp-system":            nil,
		"turing-mcp-files":             {"../sandbox:/sandbox"},
	}
	expectedUsers := map[string]string{
		"turing-orchestrator":          "${HOST_UID:?Use scripts/compose.sh to launch}:${HOST_GID:?Use scripts/compose.sh to launch}",
		"turing-agent-runtime-general": "turing-agent-runtime:turing-agent-runtime",
		"turing-mcp-system":            "mcp-system:mcp-system",
		"turing-mcp-files":             "${HOST_UID:?Use scripts/compose.sh to launch}:${HOST_GID:?Use scripts/compose.sh to launch}",
	}

	serviceNames := sortedServiceNames(document)
	if len(serviceNames) == 0 {
		t.Fatal("docker-compose.yml has no backend services")
	}
	for _, serviceName := range serviceNames {
		service := document.Services[serviceName]
		allowedMounts, known := allowedVolumeMounts[serviceName]
		if !known {
			t.Errorf("%s has no explicit container security policy", serviceName)
			continue
		}
		if !service.ReadOnly {
			t.Errorf("%s read_only = false, want true", serviceName)
		}
		if !equalStrings(service.CapDrop, []string{"ALL"}) {
			t.Errorf("%s cap_drop = %v, want [ALL]", serviceName, service.CapDrop)
		}
		if len(service.CapAdd) != 0 {
			t.Errorf("%s cap_add = %v, want none", serviceName, service.CapAdd)
		}
		if !equalStrings(service.SecurityOpt, []string{"no-new-privileges:true"}) {
			t.Errorf("%s security_opt = %v, want [no-new-privileges:true]", serviceName, service.SecurityOpt)
		}
		if service.Privileged {
			t.Errorf("%s enables privileged mode", serviceName)
		}

		user := service.User
		if want := expectedUsers[serviceName]; user != want {
			t.Errorf("%s Compose user = %q, want %q", serviceName, user, want)
		} else if composeUserIsRoot(user) {
			t.Errorf("%s uses root Compose user %q", serviceName, user)
		}

		if !equalStrings(service.Volumes, allowedMounts) {
			t.Errorf("%s volume mounts = %v, want %v", serviceName, service.Volumes, allowedMounts)
		}
		if len(service.Tmpfs) != 0 {
			t.Errorf("%s has unapproved writable tmpfs mounts: %v", serviceName, service.Tmpfs)
		}
		if len(service.VolumesFrom) != 0 {
			t.Errorf("%s inherits unapproved volumes from %v", serviceName, service.VolumesFrom)
		}
	}
}

func TestDecodeComposeDocumentResolvesSecurityRelevantYAMLForms(t *testing.T) {
	compose := `
x-hardening: &hardening
  read_only: true
  cap_drop: [ALL]
  security_opt: ["no-new-privileges:true"]
services:
  guarded: # inline comments remain part of the service map
    <<: *hardening
    user: "1000:1000"
    "cap_add": [SYS_ADMIN]
    "volumes": ["/etc:/host-etc"]
    volumes_from: [another]
    environment:
      # comments do not truncate the environment policy
      "TURING_AGENT_API_KEYS": ${TURING_AGENT_API_KEYS:-}
    "env_file": [".env"]
    secrets: [api_key]
`
	document := decodeComposeDocument(t, compose)
	service := document.Services["guarded"]
	if !service.ReadOnly ||
		!equalStrings(service.CapDrop, []string{"ALL"}) ||
		!equalStrings(service.SecurityOpt, []string{"no-new-privileges:true"}) ||
		!equalStrings(service.CapAdd, []string{"SYS_ADMIN"}) ||
		!equalStrings(service.Volumes, []string{"/etc:/host-etc"}) ||
		!equalStrings(service.VolumesFrom, []string{"another"}) {
		t.Fatalf("decoded security policy = %+v", service)
	}
	if _, ok := service.Environment["TURING_AGENT_API_KEYS"]; !ok {
		t.Fatal("quoted environment key was not decoded")
	}
	if !yamlNodePresent(service.EnvironmentFile) || !yamlNodePresent(service.Secrets) {
		t.Fatal("env_file or secrets escaped structured decoding")
	}
}

func TestComposeInheritanceCannotBypassSecurityPolicy(t *testing.T) {
	tests := map[string]struct {
		compose string
		want    string
	}{
		"top-level include": {
			compose: `
include: [inherited-compose.yml]
services:
  guarded:
    image: alpine
`,
			want: "top-level include",
		},
		"service extends": {
			compose: `
services:
  guarded:
    extends:
      file: inherited-compose.yml
      service: inherited
    image: alpine
`,
			want: "guarded uses extends",
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			document := decodeComposeDocument(t, test.compose)
			violations := composeInheritanceViolations(document)
			if !strings.Contains(strings.Join(violations, "\n"), test.want) {
				t.Fatalf("composeInheritanceViolations() = %v, want a violation containing %q", violations, test.want)
			}
		})
	}
}

func TestDockerfileFinalUserUsesLastRuntimeStageInstruction(t *testing.T) {
	dockerfile := "FROM alpine AS build\nUSER builder\nFROM alpine\nUSER app\nUSER root\n"
	if got := dockerfileFinalUser(dockerfile); got != "root" {
		t.Fatalf("dockerfileFinalUser() = %q, want root", got)
	}
}

func TestMCPComposeServicesUseRuntimeHardening(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "infra", "docker-compose.yml"))
	if err != nil {
		t.Fatal(err)
	}
	compose := string(data)

	for _, serviceName := range []string{"turing-mcp-system", "turing-mcp-files"} {
		block := composeServiceBlock(t, compose, serviceName)
		requireContainsAll(t, serviceName, block,
			"read_only: true",
			"cap_drop:",
			"- ALL",
			"security_opt:",
			"- no-new-privileges:true",
		)
	}

	files := composeServiceBlock(t, compose, "turing-mcp-files")
	requireContainsAll(t, "turing-mcp-files", files, "- ../sandbox:/sandbox")
	if strings.Contains(files, "../sandbox:/sandbox:ro") {
		t.Fatal("turing-mcp-files sandbox mount is read-only")
	}
}

func TestBindMountComposeServicesRequireValidatedHostIdentity(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "infra", "docker-compose.yml"))
	if err != nil {
		t.Fatal(err)
	}
	for _, serviceName := range []string{"turing-orchestrator", "turing-mcp-files"} {
		service := composeServiceBlock(t, string(data), serviceName)
		requireContainsAll(t, serviceName, service,
			"${HOST_UID:?",
			"${HOST_GID:?",
		)
		if strings.Contains(service, "HOST_UID:-") || strings.Contains(service, "HOST_GID:-") {
			t.Errorf("%s permits a fallback identity instead of requiring validated host IDs", serviceName)
		}
	}
}

func TestRepositoryDockerignoreExcludesSensitiveAndGeneratedContent(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", ".dockerignore"))
	if err != nil {
		t.Fatal(err)
	}
	lines := make(map[string]bool)
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line != "" && !strings.HasPrefix(line, "#") {
			lines[line] = true
		}
	}
	for _, pattern := range []string{
		".git",
		".worktrees",
		".copilot",
		".env",
		".env.*",
		"**/.env",
		"**/.env.*",
		"**/.runtime",
		"**/data",
		"**/sandbox",
		"**/node_modules",
		"**/.dart_tool",
		"**/build",
		"**/dist",
		"**/coverage",
		"**/*.log",
		"**/*.pem",
		"**/*.key",
		"**/*.cert",
	} {
		if !lines[pattern] {
			t.Errorf(".dockerignore missing %q", pattern)
		}
	}
	for _, requiredInput := range []string{"go.mod", "go.sum", "gen", "turing-backend"} {
		if lines[requiredInput] {
			t.Errorf(".dockerignore excludes required Dockerfile input %q", requiredInput)
		}
	}
}

func composeServiceBlock(t *testing.T, compose string, serviceName string) string {
	t.Helper()
	allLines := strings.Split(compose, "\n")
	start := -1
	for index, line := range allLines {
		if name, ok := composeServiceHeader(line); ok && name == serviceName {
			start = index
			break
		}
	}
	if start == -1 {
		t.Fatalf("service %q not found", serviceName)
	}
	var block []string
	for _, line := range allLines[start+1:] {
		if _, ok := composeServiceHeader(line); ok {
			break
		}
		block = append(block, line)
	}
	return strings.Join(block, "\n")
}

func decodeComposeDocument(t *testing.T, compose string) composeDocument {
	t.Helper()
	var document composeDocument
	if err := yaml.Unmarshal([]byte(compose), &document); err != nil {
		t.Fatalf("decode docker-compose.yml: %v", err)
	}
	if len(document.Services) == 0 {
		t.Fatal("docker-compose.yml has no backend services")
	}
	return document
}

func sortedServiceNames(document composeDocument) []string {
	names := make([]string, 0, len(document.Services))
	for name := range document.Services {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func yamlNodePresent(node yaml.Node) bool {
	return node.Kind != 0
}

func composeInheritanceViolations(document composeDocument) []string {
	var violations []string
	if yamlNodePresent(document.Include) {
		violations = append(violations, "docker-compose.yml uses top-level include; keep every backend service in the guarded Compose file")
	}
	for _, serviceName := range sortedServiceNames(document) {
		if yamlNodePresent(document.Services[serviceName].Extends) {
			violations = append(violations, serviceName+" uses extends; define its complete policy in the guarded service block")
		}
	}
	return violations
}

func composeUserIsRoot(user string) bool {
	user = strings.Trim(strings.TrimSpace(user), `"'`)
	name, _, _ := strings.Cut(user, ":")
	return name == "root" || name == "0"
}

func dockerfileFinalUser(dockerfile string) string {
	user := ""
	for _, line := range strings.Split(dockerfile, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		instruction, value, found := strings.Cut(line, " ")
		if !found {
			continue
		}
		switch strings.ToUpper(instruction) {
		case "FROM":
			user = ""
		case "USER":
			user = strings.TrimSpace(value)
		}
	}
	return user
}

func composeServiceHeader(line string) (string, bool) {
	line = yamlLineWithoutComment(line)
	if !strings.HasPrefix(line, "  ") || strings.HasPrefix(line, "    ") {
		return "", false
	}
	trimmed := strings.TrimSpace(line)
	if !strings.HasSuffix(trimmed, ":") {
		return "", false
	}
	name := strings.Trim(strings.TrimSuffix(trimmed, ":"), `"'`)
	return name, name != ""
}

func yamlLineWithoutComment(line string) string {
	var singleQuoted, doubleQuoted, escaped bool
	for index, character := range line {
		switch {
		case escaped:
			escaped = false
		case character == '\\' && doubleQuoted:
			escaped = true
		case character == '\'' && !doubleQuoted:
			singleQuoted = !singleQuoted
		case character == '"' && !singleQuoted:
			doubleQuoted = !doubleQuoted
		case character == '#' && !singleQuoted && !doubleQuoted &&
			(index == 0 || line[index-1] == ' ' || line[index-1] == '\t'):
			return strings.TrimRight(line[:index], " \t")
		}
	}
	return strings.TrimRight(line, " \t")
}

func equalStrings(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for index := range got {
		if got[index] != want[index] {
			return false
		}
	}
	return true
}

func requireNoEnvFile(t *testing.T, serviceName string, block string) {
	t.Helper()
	if strings.Contains(block, "env_file:") {
		t.Fatalf("%s uses env_file and receives the whole .env; use explicit environment entries", serviceName)
	}
}

func requireContainsAll(t *testing.T, serviceName string, block string, snippets ...string) {
	t.Helper()
	for _, snippet := range snippets {
		if !strings.Contains(block, snippet) {
			t.Fatalf("%s missing required policy %q", serviceName, snippet)
		}
	}
}

func requireContainsNone(t *testing.T, serviceName string, block string, snippets ...string) {
	t.Helper()
	for _, snippet := range snippets {
		if strings.Contains(block, snippet) {
			t.Fatalf("%s exposes unnecessary secret/config %q", serviceName, snippet)
		}
	}
}

// The in-process reconnect loop covers stream failures; the restart policy
// covers what it cannot — a panic, an OOM kill, or a fatal config error at
// startup. Without it a service that exits stays down until someone notices,
// which is the failure this was added to end. Asserted here so it cannot be
// removed silently.
func TestComposeServicesRestartUnlessStopped(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "infra", "docker-compose.yml"))
	if err != nil {
		t.Fatal(err)
	}
	compose := string(data)
	for _, serviceName := range []string{
		"turing-orchestrator",
		"turing-agent-runtime-general",
		"turing-mcp-system",
		"turing-mcp-files",
	} {
		block := composeServiceBlock(t, compose, serviceName)
		requireContainsAll(t, serviceName, block, "restart: unless-stopped")
		// "always" would fight an operator who deliberately stopped a service.
		if strings.Contains(block, "restart: always") {
			t.Fatalf("%s uses restart: always; unless-stopped is required", serviceName)
		}
	}
}
