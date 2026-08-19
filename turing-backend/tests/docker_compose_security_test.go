package tests

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

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

	allowedSecrets := map[string]map[string]bool{
		"turing-orchestrator": {
			"TURING_CLIENT_API_KEY":      true,
			"TURING_INTERNAL_TOKEN":      true,
			"MCP_SYSTEM_TOKEN_GENERAL":   true,
			"MCP_FILES_TOKEN_GENERAL":    true,
			"TURING_APPROVAL_JWT_SECRET": true,
			"TURING_INTEGRATION_KEY":     true,
			"OPENAI_API_KEY":             true,
			"TURING_AGENT_API_KEYS":      true,
		},
		"turing-agent-runtime-general": {
			"TURING_INTERNAL_TOKEN":    true,
			"MCP_SYSTEM_TOKEN_GENERAL": true,
			"MCP_FILES_TOKEN_GENERAL":  true,
			"OPENAI_API_KEY":           true,
			"TURING_AGENT_API_KEYS":    true,
		},
		"turing-mcp-system": {
			"MCP_SYSTEM_TOKEN_GENERAL": true,
		},
		"turing-mcp-files": {
			"TURING_INTERNAL_TOKEN":      true,
			"MCP_FILES_TOKEN_GENERAL":    true,
			"TURING_APPROVAL_JWT_SECRET": true,
		},
	}
	for _, serviceName := range composeServiceNames(t, compose) {
		service := composeServiceBlock(t, compose, serviceName)
		requireNoEnvFile(t, serviceName, service)
		allowed, known := allowedSecrets[serviceName]
		if !known {
			t.Errorf("%s has no explicit secret policy", serviceName)
			continue
		}
		for _, environmentName := range composeEnvironmentNames(t, service) {
			if environmentNameLooksSensitive(environmentName) && !allowed[environmentName] {
				t.Errorf("%s receives unapproved secret %s", serviceName, environmentName)
			}
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
	for _, serviceName := range composeServiceNames(t, string(composeData)) {
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
	compose := string(data)
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

	serviceNames := composeServiceNames(t, compose)
	if len(serviceNames) == 0 {
		t.Fatal("docker-compose.yml has no backend services")
	}
	for _, serviceName := range serviceNames {
		block := composeServiceBlock(t, compose, serviceName)
		allowedMounts, known := allowedVolumeMounts[serviceName]
		if !known {
			t.Errorf("%s has no explicit container security policy", serviceName)
			continue
		}
		if got := composeScalar(t, block, "read_only"); got != "true" {
			t.Errorf("%s read_only = %q, want true", serviceName, got)
		}
		if got := composeList(t, block, "cap_drop"); !equalStrings(got, []string{"ALL"}) {
			t.Errorf("%s cap_drop = %v, want [ALL]", serviceName, got)
		}
		if got := composeList(t, block, "security_opt"); !equalStrings(got, []string{"no-new-privileges:true"}) {
			t.Errorf("%s security_opt = %v, want [no-new-privileges:true]", serviceName, got)
		}
		for _, forbiddenKey := range []string{"cap_add", "privileged"} {
			if composeKeyPresent(block, forbiddenKey) {
				t.Errorf("%s sets forbidden Compose key %s", serviceName, forbiddenKey)
			}
		}

		user := strings.Trim(composeScalar(t, block, "user"), `"'`)
		if want := expectedUsers[serviceName]; user != want {
			t.Errorf("%s Compose user = %q, want %q", serviceName, user, want)
		} else if composeUserIsRoot(user) {
			t.Errorf("%s uses root Compose user %q", serviceName, user)
		}

		if got, want := composeList(t, block, "volumes"), allowedMounts; !equalStrings(got, want) {
			t.Errorf("%s volume mounts = %v, want %v", serviceName, got, want)
		}
		if got := composeList(t, block, "tmpfs"); len(got) != 0 {
			t.Errorf("%s has unapproved writable tmpfs mounts: %v", serviceName, got)
		}
	}
}

func TestComposeServiceNamesKeepServicesAfterTopLevelComments(t *testing.T) {
	compose := "services:\n  first:\n    image: alpine\n# grouping comment\n  second:\n    image: alpine\nnetworks:\n  internal:\n"
	if got := composeServiceNames(t, compose); !equalStrings(got, []string{"first", "second"}) {
		t.Fatalf("composeServiceNames() = %v, want [first second]", got)
	}
}

func TestComposeListDoesNotHideInlineWritableStorage(t *testing.T) {
	for _, test := range []struct {
		name  string
		block string
		key   string
	}{
		{name: "scalar tmpfs", block: "    tmpfs: /tmp", key: "tmpfs"},
		{name: "flow volumes", block: `    volumes: ["/etc:/host-etc"]`, key: "volumes"},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := composeList(t, test.block, test.key); len(got) == 0 {
				t.Fatalf("composeList(%q) silently accepted unsupported inline syntax", test.key)
			}
		})
	}
}

func TestComposeEnvironmentNamesNormalizeQuotedKeys(t *testing.T) {
	block := "    environment:\n      \"TURING_AGENT_API_KEYS\": ${TURING_AGENT_API_KEYS:-}\n"
	if got := composeEnvironmentNames(t, block); !equalStrings(got, []string{"TURING_AGENT_API_KEYS"}) {
		t.Fatalf("composeEnvironmentNames() = %v, want [TURING_AGENT_API_KEYS]", got)
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
	header := "  " + serviceName + ":"
	allLines := strings.Split(compose, "\n")
	start := -1
	for index, line := range allLines {
		if line == header {
			start = index
			break
		}
	}
	if start == -1 {
		t.Fatalf("service %q not found", serviceName)
	}
	var block []string
	for _, line := range allLines[start+1:] {
		if strings.HasPrefix(line, "  ") && !strings.HasPrefix(line, "    ") && strings.HasSuffix(strings.TrimSpace(line), ":") {
			break
		}
		block = append(block, line)
	}
	return strings.Join(block, "\n")
}

func composeServiceNames(t *testing.T, compose string) []string {
	t.Helper()
	lines := strings.Split(compose, "\n")
	start := -1
	for index, line := range lines {
		if line == "services:" {
			start = index
			break
		}
	}
	if start == -1 {
		t.Fatal("docker-compose.yml has no services block")
	}
	var names []string
	seen := make(map[string]bool)
	for _, line := range lines[start+1:] {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if !strings.HasPrefix(line, " ") {
			break
		}
		if strings.HasPrefix(line, "  ") && !strings.HasPrefix(line, "    ") && strings.HasSuffix(line, ":") {
			name := strings.TrimSuffix(strings.TrimSpace(line), ":")
			if seen[name] {
				t.Fatalf("docker-compose.yml repeats service %q", name)
			}
			seen[name] = true
			names = append(names, name)
		}
	}
	return names
}

func composeEnvironmentNames(t *testing.T, block string) []string {
	t.Helper()
	var names []string
	inEnvironment := false
	for _, line := range strings.Split(block, "\n") {
		if strings.HasPrefix(line, "    environment:") && line != "    environment:" {
			t.Fatalf("environment must use block mapping syntax, got %q", line)
		}
		if line == "    environment:" {
			if inEnvironment {
				t.Fatal("service repeats environment block")
			}
			inEnvironment = true
			continue
		}
		if !inEnvironment {
			continue
		}
		if strings.HasPrefix(line, "      ") {
			trimmed := strings.TrimSpace(line)
			if trimmed == "" || strings.HasPrefix(trimmed, "#") {
				continue
			}
			if strings.HasPrefix(trimmed, "- ") {
				t.Fatalf("environment must use mapping syntax, got %q", line)
			}
			name, _, found := strings.Cut(trimmed, ":")
			if !found {
				t.Fatalf("invalid environment entry %q", line)
			}
			name = strings.Trim(strings.TrimSpace(name), `"'`)
			names = append(names, name)
			continue
		}
		if strings.TrimSpace(line) != "" {
			break
		}
	}
	return names
}

func composeScalar(t *testing.T, block string, key string) string {
	t.Helper()
	prefix := "    " + key + ":"
	value := ""
	found := false
	for _, line := range strings.Split(block, "\n") {
		if strings.HasPrefix(line, prefix) {
			if found {
				t.Fatalf("service repeats %s", key)
			}
			found = true
			value = strings.TrimSpace(strings.TrimPrefix(line, prefix))
		}
	}
	return value
}

func composeUserIsRoot(user string) bool {
	user = strings.Trim(strings.TrimSpace(user), `"'`)
	name, _, _ := strings.Cut(user, ":")
	return name == "root" || name == "0"
}

func composeKeyPresent(block string, key string) bool {
	prefix := "    " + key + ":"
	for _, line := range strings.Split(block, "\n") {
		if strings.HasPrefix(line, prefix) {
			return true
		}
	}
	return false
}

func composeList(t *testing.T, block string, key string) []string {
	t.Helper()
	header := "    " + key + ":"
	var entries []string
	inList := false
	found := false
	for _, line := range strings.Split(block, "\n") {
		if strings.HasPrefix(line, header) && line != header {
			if found {
				t.Fatalf("service repeats %s", key)
			}
			return []string{strings.TrimSpace(strings.TrimPrefix(line, header))}
		}
		if line == header {
			if found {
				t.Fatalf("service repeats %s", key)
			}
			found = true
			inList = true
			continue
		}
		if !inList {
			continue
		}
		if strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}
		if strings.HasPrefix(line, "      - ") {
			entries = append(entries, strings.TrimSpace(strings.TrimPrefix(line, "      - ")))
			continue
		}
		if strings.TrimSpace(line) != "" {
			break
		}
	}
	return entries
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

func environmentNameLooksSensitive(name string) bool {
	upper := strings.ToUpper(name)
	for _, marker := range []string{"KEY", "TOKEN", "SECRET", "PASSWORD", "CREDENTIAL"} {
		if strings.Contains(upper, marker) {
			return true
		}
	}
	return false
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
