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

	sensitiveEnvironment := map[string]bool{
		"TURING_CLIENT_API_KEY":      true,
		"TURING_INTERNAL_TOKEN":      true,
		"MCP_SYSTEM_TOKEN_GENERAL":   true,
		"MCP_FILES_TOKEN_GENERAL":    true,
		"TURING_APPROVAL_JWT_SECRET": true,
		"TURING_INTEGRATION_KEY":     true,
		"OPENAI_API_KEY":             true,
		"TURING_AGENT_API_KEYS":      true,
	}
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
		for _, environmentName := range composeEnvironmentNames(t, service) {
			if sensitiveEnvironment[environmentName] && !allowedSecrets[serviceName][environmentName] {
				t.Errorf("%s receives unapproved secret %s", serviceName, environmentName)
			}
		}
	}
}

func TestEveryBackendImageRunsAsExplicitNonRootUser(t *testing.T) {
	tests := map[string]struct {
		path     string
		snippets []string
	}{
		"turing-orchestrator": {
			path: filepath.Join("..", "orchestrator-go", "Dockerfile"),
			snippets: []string{
				"groupadd --gid 1000 turing-orchestrator",
				"useradd --uid 1000 --gid 1000",
				"USER turing-orchestrator:turing-orchestrator",
			},
		},
		"turing-agent-runtime-general": {
			path: filepath.Join("..", "agent-runtime-go", "Dockerfile"),
			snippets: []string{
				"groupadd --gid 1000 turing-agent-runtime",
				"useradd --uid 1000 --gid 1000",
				"USER turing-agent-runtime:turing-agent-runtime",
			},
		},
		"turing-mcp-system": {
			path: filepath.Join("..", "mcp-system", "Dockerfile"),
			snippets: []string{
				"addgroup -g 1000 -S mcp-system",
				"adduser -u 1000 -S -G mcp-system mcp-system",
				"USER mcp-system:mcp-system",
			},
		},
		"turing-mcp-files": {
			path: filepath.Join("..", "mcp-files", "Dockerfile"),
			snippets: []string{
				"addgroup -g 1000 -S mcp-files",
				"adduser -u 1000 -S -G mcp-files mcp-files",
				"USER mcp-files:mcp-files",
			},
		},
	}
	for serviceName, test := range tests {
		t.Run(serviceName, func(t *testing.T) {
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
		})
	}
}

func TestEveryComposeServiceUsesLeastPrivilegeRuntime(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "infra", "docker-compose.yml"))
	if err != nil {
		t.Fatal(err)
	}
	compose := string(data)
	allowedWritableMounts := map[string][]string{
		"turing-orchestrator":          {"../data:/app/data"},
		"turing-agent-runtime-general": nil,
		"turing-mcp-system":            nil,
		"turing-mcp-files":             {"../sandbox:/sandbox"},
	}

	serviceNames := composeServiceNames(t, compose)
	if len(serviceNames) == 0 {
		t.Fatal("docker-compose.yml has no backend services")
	}
	for _, serviceName := range serviceNames {
		block := composeServiceBlock(t, compose, serviceName)
		requireContainsAll(t, serviceName, block,
			"read_only: true",
			"cap_drop:",
			"- ALL",
			"security_opt:",
			"- no-new-privileges:true",
		)
		if got := composeList(block, "cap_drop"); !equalStrings(got, []string{"ALL"}) {
			t.Errorf("%s cap_drop = %v, want [ALL]", serviceName, got)
		}
		requireContainsNone(t, serviceName, block, "cap_add:", "privileged: true")

		user := composeScalar(t, block, "user")
		if user == "" {
			t.Errorf("%s has no explicit Compose user", serviceName)
		} else if composeUserIsRoot(user) {
			t.Errorf("%s uses root Compose user %q", serviceName, user)
		}

		if got, want := composeList(block, "volumes"), allowedWritableMounts[serviceName]; !equalStrings(got, want) {
			t.Errorf("%s writable mounts = %v, want %v", serviceName, got, want)
		}
		if got := composeList(block, "tmpfs"); len(got) != 0 {
			t.Errorf("%s has unapproved writable tmpfs mounts: %v", serviceName, got)
		}
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

func TestMCPFilesComposeRequiresValidatedHostIdentity(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "infra", "docker-compose.yml"))
	if err != nil {
		t.Fatal(err)
	}
	files := composeServiceBlock(t, string(data), "turing-mcp-files")
	requireContainsAll(t, "turing-mcp-files", files,
		"${HOST_UID:?",
		"${HOST_GID:?",
	)
	if strings.Contains(files, "HOST_UID:-") || strings.Contains(files, "HOST_GID:-") {
		t.Fatal("turing-mcp-files permits a fallback identity instead of requiring validated host IDs")
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
	if len(lines) == 0 || lines[0] != "services:" {
		t.Fatal("docker-compose.yml must start with a services block")
	}
	var names []string
	for _, line := range lines[1:] {
		if line != "" && !strings.HasPrefix(line, " ") {
			break
		}
		if strings.HasPrefix(line, "  ") && !strings.HasPrefix(line, "    ") && strings.HasSuffix(line, ":") {
			names = append(names, strings.TrimSuffix(strings.TrimSpace(line), ":"))
		}
	}
	return names
}

func composeEnvironmentNames(t *testing.T, block string) []string {
	t.Helper()
	var names []string
	inEnvironment := false
	for _, line := range strings.Split(block, "\n") {
		if line == "    environment:" {
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
			name, _, found := strings.Cut(trimmed, ":")
			if !found {
				t.Fatalf("invalid environment entry %q", line)
			}
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
	for _, line := range strings.Split(block, "\n") {
		if strings.HasPrefix(line, prefix) {
			return strings.TrimSpace(strings.TrimPrefix(line, prefix))
		}
	}
	return ""
}

func composeUserIsRoot(user string) bool {
	user = strings.Trim(strings.TrimSpace(user), `"'`)
	name, _, _ := strings.Cut(user, ":")
	return name == "root" || name == "0"
}

func composeList(block string, key string) []string {
	header := "    " + key + ":"
	var entries []string
	inList := false
	for _, line := range strings.Split(block, "\n") {
		if line == header {
			inList = true
			continue
		}
		if !inList {
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
