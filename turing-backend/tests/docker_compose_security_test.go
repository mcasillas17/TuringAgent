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
		"FILES_SANDBOX_ROOT:",
	)
	requireContainsNone(t, "turing-mcp-files", files,
		"TURING_CLIENT_API_KEY:",
		"OPENAI_API_KEY:",
		"ORCHESTRATOR_INTERNAL_BASE_URL:",
	)
}

func TestMCPFilesImageRunsAsFixedNonRootUser(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "mcp-files", "Dockerfile"))
	if err != nil {
		t.Fatal(err)
	}
	dockerfile := string(data)
	for _, snippet := range []string{
		"addgroup -g 1000 -S mcp-files",
		"adduser -u 1000 -S -G mcp-files mcp-files",
		"USER mcp-files:mcp-files",
	} {
		if !strings.Contains(dockerfile, snippet) {
			t.Fatalf("mcp-files Dockerfile missing %q", snippet)
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
			t.Fatalf("%s missing %q in explicit environment block", serviceName, snippet)
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
