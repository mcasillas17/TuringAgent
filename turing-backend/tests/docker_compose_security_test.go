package tests

import (
	"fmt"
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
	Networks map[string]yaml.Node      `yaml:"networks"`
}

type composeService struct {
	Image             string             `yaml:"image"`
	Restart           string             `yaml:"restart"`
	Build             yaml.Node          `yaml:"build"`
	User              string             `yaml:"user"`
	ReadOnly          bool               `yaml:"read_only"`
	CapDrop           []string           `yaml:"cap_drop"`
	CapAdd            []string           `yaml:"cap_add"`
	SecurityOpt       []string           `yaml:"security_opt"`
	Privileged        bool               `yaml:"privileged"`
	Volumes           []string           `yaml:"volumes"`
	Tmpfs             []string           `yaml:"tmpfs"`
	VolumesFrom       []string           `yaml:"volumes_from"`
	Devices           []string           `yaml:"devices"`
	DeviceCgroupRules []string           `yaml:"device_cgroup_rules"`
	GPUs              yaml.Node          `yaml:"gpus"`
	Runtime           string             `yaml:"runtime"`
	Deploy            yaml.Node          `yaml:"deploy"`
	PID               string             `yaml:"pid"`
	IPC               string             `yaml:"ipc"`
	UsernsMode        string             `yaml:"userns_mode"`
	GroupAdd          []string           `yaml:"group_add"`
	Ports             []string           `yaml:"ports"`
	Expose            []string           `yaml:"expose"`
	Networks          []string           `yaml:"networks"`
	NetworkMode       string             `yaml:"network_mode"`
	Links             []string           `yaml:"links"`
	ExternalLinks     []string           `yaml:"external_links"`
	Environment       map[string]*string `yaml:"environment"`
	EnvironmentFile   yaml.Node          `yaml:"env_file"`
	Extends           yaml.Node          `yaml:"extends"`
	Secrets           yaml.Node          `yaml:"secrets"`
	DependsOn         yaml.Node          `yaml:"depends_on"`
	ExtraHosts        []string           `yaml:"extra_hosts"`
	Healthcheck       yaml.Node          `yaml:"healthcheck"`
}

type composeRuntimePolicy struct {
	user       string
	volumes    []string
	tmpfs      []string
	ports      []string
	expose     []string
	networks   []string
	extraHosts []string
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
		"SKILLS_ROOT:",
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
			"SKILLS_ROOT",
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
	composePath := filepath.Join("..", "infra", "docker-compose.yml")
	tests := map[string]struct {
		context  string
		path     string
		user     string
		snippets []string
	}{
		"turing-orchestrator": {
			context: filepath.Join("..", ".."),
			path:    filepath.Join("..", "orchestrator-go", "Dockerfile"),
			user:    "turing-orchestrator:turing-orchestrator",
			snippets: []string{
				"groupadd --gid 1000 turing-orchestrator",
				"useradd --uid 1000 --gid 1000",
			},
		},
		"turing-agent-runtime-general": {
			context: filepath.Join("..", ".."),
			path:    filepath.Join("..", "agent-runtime-go", "Dockerfile"),
			user:    "turing-agent-runtime:turing-agent-runtime",
			snippets: []string{
				"groupadd --gid 1000 turing-agent-runtime",
				"useradd --uid 1000 --gid 1000",
			},
		},
		"turing-mcp-system": {
			context: filepath.Join("..", "mcp-system"),
			path:    filepath.Join("..", "mcp-system", "Dockerfile"),
			user:    "mcp-system:mcp-system",
			snippets: []string{
				"addgroup -g 1000 -S mcp-system",
				"adduser -u 1000 -S -G mcp-system mcp-system",
			},
		},
		"turing-mcp-files": {
			context: filepath.Join("..", ".."),
			path:    filepath.Join("..", "mcp-files", "Dockerfile"),
			user:    "mcp-files:mcp-files",
			snippets: []string{
				"addgroup -g 1000 -S mcp-files",
				"adduser -u 1000 -S -G mcp-files mcp-files",
			},
		},
	}
	composeData, err := os.ReadFile(composePath)
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
			contextPath, dockerfilePath, err := composeBuildPaths(composePath, document.Services[serviceName].Build)
			if err != nil {
				t.Fatalf("%s build: %v", serviceName, err)
			}
			expectedContext, err := filepath.Abs(test.context)
			if err != nil {
				t.Fatal(err)
			}
			if contextPath != expectedContext {
				t.Errorf("%s build context = %q, want %q", serviceName, contextPath, expectedContext)
			}
			expectedPath, err := filepath.Abs(test.path)
			if err != nil {
				t.Fatal(err)
			}
			if dockerfilePath != expectedPath {
				t.Errorf("%s builds %q, want %q", serviceName, dockerfilePath, expectedPath)
			}
			data, err := os.ReadFile(dockerfilePath)
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
			if dockerfileDeclaresVolume(dockerfile) {
				t.Errorf("%s Dockerfile declares VOLUME and bypasses the Compose writable-storage allowlist", serviceName)
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
	for _, violation := range composeNetworkDefinitionViolations(document, []string{"net-files", "net-system"}) {
		t.Error(violation)
	}
	policies := map[string]composeRuntimePolicy{
		"turing-orchestrator": {
			user:     "${HOST_UID:?Use scripts/compose.sh to launch}:${HOST_GID:?Use scripts/compose.sh to launch}",
			volumes:  []string{"../data:/app/data", "../skills:/skills"},
			tmpfs:    []string{"/dev/shm:ro,nosuid,nodev,noexec,size=64k"},
			ports:    []string{"127.0.0.1:${ORCHESTRATOR_PUBLIC_PORT:-3000}:3000"},
			expose:   []string{"3001"},
			networks: []string{"net-system", "net-files"},
		},
		"turing-agent-runtime-general": {
			user:       "turing-agent-runtime:turing-agent-runtime",
			tmpfs:      []string{"/dev/shm:ro,nosuid,nodev,noexec,size=64k"},
			networks:   []string{"net-system", "net-files"},
			extraHosts: []string{"host.docker.internal:host-gateway"},
		},
		"turing-mcp-system": {
			user:     "mcp-system:mcp-system",
			tmpfs:    []string{"/dev/shm:ro,nosuid,nodev,noexec,size=64k"},
			expose:   []string{"7100"},
			networks: []string{"net-system"},
		},
		"turing-mcp-files": {
			user:     "${HOST_UID:?Use scripts/compose.sh to launch}:${HOST_GID:?Use scripts/compose.sh to launch}",
			volumes:  []string{"../sandbox:/sandbox"},
			tmpfs:    []string{"/dev/shm:ro,nosuid,nodev,noexec,size=64k"},
			expose:   []string{"7110"},
			networks: []string{"net-files"},
		},
	}

	serviceNames := sortedServiceNames(document)
	if len(serviceNames) == 0 {
		t.Fatal("docker-compose.yml has no backend services")
	}
	for _, serviceName := range serviceNames {
		service := document.Services[serviceName]
		policy, known := policies[serviceName]
		if !known {
			t.Errorf("%s has no explicit container security policy", serviceName)
			continue
		}
		for _, violation := range composeRuntimePolicyViolations(serviceName, service, policy) {
			t.Error(violation)
		}
	}
}

func TestDecodeComposeDocumentResolvesSecurityRelevantYAMLForms(t *testing.T) {
	compose := `
services:
  base: &hardening
    read_only: true
    cap_drop: [ALL]
    security_opt: ["no-new-privileges:true"]
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

func TestComposeRuntimePolicyRejectsPrivilegeAndExposureBypasses(t *testing.T) {
	policy := composeRuntimePolicy{
		user:     "app:app",
		expose:   []string{"7100"},
		networks: []string{"internal"},
	}
	tests := map[string]struct {
		compose string
		want    []string
	}{
		"missing identity and extra access": {
			compose: `
services:
  guarded:
    read_only: true
    cap_drop: [ALL]
    security_opt: ["no-new-privileges:true"]
    devices: ["/dev/kvm:/dev/kvm"]
    device_cgroup_rules: ["c 195:* rmw"]
    gpus: all
    runtime: nvidia
    deploy:
      resources:
        reservations:
          devices:
            - driver: nvidia
              capabilities: [gpu]
    pid: host
    ipc: host
    userns_mode: host
    group_add: ["0"]
    extra_hosts: ["host.docker.internal:host-gateway"]
    ports: ["7100:7100"]
    network_mode: host
`,
			want: []string{
				"missing Compose user",
				"devices",
				"device cgroup rules",
				"GPU access",
				"container runtime",
				"deploy resources",
				"pid namespace",
				"IPC namespace",
				"user namespace",
				"supplementary groups",
				"host aliases",
				"ports",
				"expose",
				"networks",
				"network_mode",
			},
		},
		"root identity": {
			compose: `
services:
  guarded:
    user: "0:0"
    read_only: true
    cap_drop: [ALL]
    security_opt: ["no-new-privileges:true"]
    expose: ["7100"]
    networks: [internal]
`,
			want: []string{"uses root Compose user"},
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			document := decodeComposeDocument(t, test.compose)
			violations := strings.Join(composeRuntimePolicyViolations("guarded", document.Services["guarded"], policy), "\n")
			for _, want := range test.want {
				if !strings.Contains(violations, want) {
					t.Errorf("violations %q do not contain %q", violations, want)
				}
			}
		})
	}
}

func TestDecodeComposeDocumentRejectsUnknownServiceKeys(t *testing.T) {
	_, err := parseComposeDocument(`
services:
  guarded:
    image: alpine
    unreviewed_privilege_path: enabled
`)
	if err == nil {
		t.Fatal("parseComposeDocument accepted an unknown service key")
	}
}

func TestComposeNetworkDefinitionsRemainProjectScoped(t *testing.T) {
	document := decodeComposeDocument(t, `
services:
  guarded:
    image: alpine
    networks: [internal]
networks:
  internal:
    external: true
`)
	violations := strings.Join(composeNetworkDefinitionViolations(document, []string{"internal"}), "\n")
	if !strings.Contains(violations, "must use an empty project-scoped definition") {
		t.Fatalf("network violations = %q, want an external-network rejection", violations)
	}
}

func TestComposeDockerfilePathSupportsCanonicalBuildForms(t *testing.T) {
	composePath := filepath.Join(string(filepath.Separator), "repo", "turing-backend", "infra", "docker-compose.yml")
	document := decodeComposeDocument(t, `
services:
  scalar:
    build: ../mcp-system
  mapping:
    build:
      context: ../..
      dockerfile: turing-backend/orchestrator-go/Dockerfile
  targeted:
    build:
      context: ../..
      dockerfile: turing-backend/orchestrator-go/Dockerfile
      target: build
`)
	tests := map[string]string{
		"scalar":  filepath.Join(string(filepath.Separator), "repo", "turing-backend", "mcp-system", "Dockerfile"),
		"mapping": filepath.Join(string(filepath.Separator), "repo", "turing-backend", "orchestrator-go", "Dockerfile"),
	}
	for serviceName, want := range tests {
		got, err := composeDockerfilePath(composePath, document.Services[serviceName].Build)
		if err != nil {
			t.Fatalf("%s build: %v", serviceName, err)
		}
		if got != want {
			t.Errorf("%s Dockerfile = %q, want %q", serviceName, got, want)
		}
	}
	if _, err := composeDockerfilePath(composePath, document.Services["targeted"].Build); err == nil {
		t.Fatal("composeDockerfilePath accepted a non-default target stage")
	}
}

func TestComposeBuildPathsExposeBroadenedContext(t *testing.T) {
	composePath := filepath.Join(string(filepath.Separator), "repo", "turing-backend", "infra", "docker-compose.yml")
	document := decodeComposeDocument(t, `
services:
  canonical:
    build:
      context: ../..
      dockerfile: turing-backend/orchestrator-go/Dockerfile
  broadened:
    build:
      context: ../../..
      dockerfile: repo/turing-backend/orchestrator-go/Dockerfile
`)
	canonicalContext, canonicalDockerfile, err := composeBuildPaths(composePath, document.Services["canonical"].Build)
	if err != nil {
		t.Fatal(err)
	}
	broadenedContext, broadenedDockerfile, err := composeBuildPaths(composePath, document.Services["broadened"].Build)
	if err != nil {
		t.Fatal(err)
	}
	if canonicalDockerfile != broadenedDockerfile {
		t.Fatalf("fixture Dockerfiles differ: %q != %q", canonicalDockerfile, broadenedDockerfile)
	}
	if canonicalContext == broadenedContext {
		t.Fatalf("broadened context = canonical context %q", canonicalContext)
	}
}

func TestDockerfileFinalUserUsesLastRuntimeStageInstruction(t *testing.T) {
	for name, dockerfile := range map[string]string{
		"space separated": "FROM alpine AS build\nUSER builder\nFROM alpine\nUSER app\nUSER root\n",
		"tab separated":   "FROM alpine AS build\nUSER builder\nFROM alpine\nUSER app\nUSER\troot\n",
	} {
		t.Run(name, func(t *testing.T) {
			if got := dockerfileFinalUser(dockerfile); got != "root" {
				t.Fatalf("dockerfileFinalUser() = %q, want root", got)
			}
		})
	}
	if !dockerfileDeclaresVolume("FROM alpine\nVOLUME /tmp\nUSER app\n") {
		t.Fatal("dockerfileDeclaresVolume missed a writable image volume")
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
		"**/skills",
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
	document, err := parseComposeDocument(compose)
	if err != nil {
		t.Fatalf("decode docker-compose.yml: %v", err)
	}
	if len(document.Services) == 0 {
		t.Fatal("docker-compose.yml has no backend services")
	}
	return document
}

func parseComposeDocument(compose string) (composeDocument, error) {
	var document composeDocument
	decoder := yaml.NewDecoder(strings.NewReader(compose))
	decoder.KnownFields(true)
	if err := decoder.Decode(&document); err != nil {
		return composeDocument{}, err
	}
	return document, nil
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

func composeNetworkDefinitionViolations(document composeDocument, expected []string) []string {
	actual := make([]string, 0, len(document.Networks))
	for name := range document.Networks {
		actual = append(actual, name)
	}
	sort.Strings(actual)
	want := append([]string(nil), expected...)
	sort.Strings(want)

	var violations []string
	if !equalStrings(actual, want) {
		violations = append(violations, fmt.Sprintf("Compose networks = %v, want %v", actual, want))
	}
	for _, name := range actual {
		definition := document.Networks[name]
		empty := !yamlNodePresent(definition) ||
			(definition.Kind == yaml.ScalarNode && definition.Tag == "!!null") ||
			(definition.Kind == yaml.MappingNode && len(definition.Content) == 0)
		if !empty {
			violations = append(violations, name+" must use an empty project-scoped definition")
		}
	}
	return violations
}

func composeRuntimePolicyViolations(serviceName string, service composeService, policy composeRuntimePolicy) []string {
	var violations []string
	if !service.ReadOnly {
		violations = append(violations, serviceName+" read_only = false, want true")
	}
	if !equalStrings(service.CapDrop, []string{"ALL"}) {
		violations = append(violations, fmt.Sprintf("%s cap_drop = %v, want [ALL]", serviceName, service.CapDrop))
	}
	if len(service.CapAdd) != 0 {
		violations = append(violations, fmt.Sprintf("%s cap_add = %v, want none", serviceName, service.CapAdd))
	}
	if !equalStrings(service.SecurityOpt, []string{"no-new-privileges:true"}) {
		violations = append(violations, fmt.Sprintf("%s security_opt = %v, want [no-new-privileges:true]", serviceName, service.SecurityOpt))
	}
	if service.Privileged {
		violations = append(violations, serviceName+" enables privileged mode")
	}

	switch {
	case strings.TrimSpace(service.User) == "":
		violations = append(violations, serviceName+" is missing Compose user")
	case composeUserIsRoot(service.User):
		violations = append(violations, fmt.Sprintf("%s uses root Compose user %q", serviceName, service.User))
	case service.User != policy.user:
		violations = append(violations, fmt.Sprintf("%s Compose user = %q, want %q", serviceName, service.User, policy.user))
	}

	if !equalStrings(service.Volumes, policy.volumes) {
		violations = append(violations, fmt.Sprintf("%s volume mounts = %v, want %v", serviceName, service.Volumes, policy.volumes))
	}
	if !equalStrings(service.Tmpfs, policy.tmpfs) {
		violations = append(violations, fmt.Sprintf("%s tmpfs mounts = %v, want %v", serviceName, service.Tmpfs, policy.tmpfs))
	}
	if len(service.VolumesFrom) != 0 {
		violations = append(violations, fmt.Sprintf("%s inherits unapproved volumes from %v", serviceName, service.VolumesFrom))
	}
	if len(service.Devices) != 0 {
		violations = append(violations, fmt.Sprintf("%s exposes unapproved devices: %v", serviceName, service.Devices))
	}
	if len(service.DeviceCgroupRules) != 0 {
		violations = append(violations, fmt.Sprintf("%s adds unapproved device cgroup rules: %v", serviceName, service.DeviceCgroupRules))
	}
	if yamlNodePresent(service.GPUs) {
		violations = append(violations, serviceName+" requests unapproved GPU access")
	}
	if service.Runtime != "" {
		violations = append(violations, fmt.Sprintf("%s selects unapproved container runtime %q", serviceName, service.Runtime))
	}
	if yamlNodePresent(service.Deploy) {
		violations = append(violations, serviceName+" uses unapproved deploy resources")
	}
	if service.PID != "" {
		violations = append(violations, fmt.Sprintf("%s shares unapproved pid namespace %q", serviceName, service.PID))
	}
	if service.IPC != "" {
		violations = append(violations, fmt.Sprintf("%s shares unapproved IPC namespace %q", serviceName, service.IPC))
	}
	if service.UsernsMode != "" {
		violations = append(violations, fmt.Sprintf("%s selects unapproved user namespace mode %q", serviceName, service.UsernsMode))
	}
	if len(service.GroupAdd) != 0 {
		violations = append(violations, fmt.Sprintf("%s adds unapproved supplementary groups: %v", serviceName, service.GroupAdd))
	}

	if !equalStrings(service.Ports, policy.ports) {
		violations = append(violations, fmt.Sprintf("%s ports = %v, want %v", serviceName, service.Ports, policy.ports))
	}
	if !equalStrings(service.Expose, policy.expose) {
		violations = append(violations, fmt.Sprintf("%s expose = %v, want %v", serviceName, service.Expose, policy.expose))
	}
	if !equalStrings(service.Networks, policy.networks) {
		violations = append(violations, fmt.Sprintf("%s networks = %v, want %v", serviceName, service.Networks, policy.networks))
	}
	if service.NetworkMode != "" {
		violations = append(violations, fmt.Sprintf("%s uses unapproved network_mode %q", serviceName, service.NetworkMode))
	}
	if len(service.Links) != 0 {
		violations = append(violations, fmt.Sprintf("%s uses unapproved links: %v", serviceName, service.Links))
	}
	if len(service.ExternalLinks) != 0 {
		violations = append(violations, fmt.Sprintf("%s uses unapproved external_links: %v", serviceName, service.ExternalLinks))
	}
	if !equalStrings(service.ExtraHosts, policy.extraHosts) {
		violations = append(violations, fmt.Sprintf("%s host aliases = %v, want %v", serviceName, service.ExtraHosts, policy.extraHosts))
	}
	return violations
}

func composeUserIsRoot(user string) bool {
	user = strings.Trim(strings.TrimSpace(user), `"'`)
	name, _, _ := strings.Cut(user, ":")
	return name == "root" || name == "0"
}

func composeDockerfilePath(composePath string, build yaml.Node) (string, error) {
	_, dockerfilePath, err := composeBuildPaths(composePath, build)
	return dockerfilePath, err
}

func composeBuildPaths(composePath string, build yaml.Node) (string, string, error) {
	if !yamlNodePresent(build) {
		return "", "", fmt.Errorf("missing build configuration")
	}

	context := ""
	dockerfile := ""
	switch build.Kind {
	case yaml.ScalarNode:
		if build.Tag == "!!null" {
			return "", "", fmt.Errorf("build configuration is null")
		}
		context = build.Value
	case yaml.MappingNode:
		for index := 0; index < len(build.Content); index += 2 {
			key := build.Content[index].Value
			value := build.Content[index+1]
			switch key {
			case "context", "dockerfile":
				if value.Kind != yaml.ScalarNode || value.Tag == "!!null" {
					return "", "", fmt.Errorf("build %s must be a string", key)
				}
				if key == "context" {
					context = value.Value
				} else {
					dockerfile = value.Value
				}
			case "target":
				return "", "", fmt.Errorf("build target is not allowed")
			case "dockerfile_inline":
				return "", "", fmt.Errorf("inline Dockerfiles are not allowed")
			case "args":
				if err := validateComposeBuildArgs(*value); err != nil {
					return "", "", err
				}
			default:
				return "", "", fmt.Errorf("unapproved build key %q", key)
			}
		}
	default:
		return "", "", fmt.Errorf("build configuration must be a path or mapping")
	}
	if context == "" {
		return "", "", fmt.Errorf("build context is empty")
	}
	if dockerfile == "" {
		dockerfile = "Dockerfile"
	}
	if filepath.IsAbs(context) || filepath.IsAbs(dockerfile) {
		return "", "", fmt.Errorf("build context and Dockerfile must be repository-relative")
	}
	contextPath, err := filepath.Abs(filepath.Join(filepath.Dir(composePath), context))
	if err != nil {
		return "", "", err
	}
	return contextPath, filepath.Join(contextPath, dockerfile), nil
}

func validateComposeBuildArgs(args yaml.Node) error {
	if args.Kind != yaml.MappingNode {
		return fmt.Errorf("build args must use mapping syntax")
	}
	for index := 0; index < len(args.Content); index += 2 {
		name := args.Content[index].Value
		value := args.Content[index+1]
		if name != "GOFLAGS" || value.Kind != yaml.ScalarNode || value.Value != "-tags=sqlite_fts5" {
			return fmt.Errorf("unapproved build argument %q", name)
		}
	}
	return nil
}

func dockerfileFinalUser(dockerfile string) string {
	user := ""
	for _, line := range strings.Split(dockerfile, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		switch strings.ToUpper(fields[0]) {
		case "FROM":
			user = ""
		case "USER":
			user = strings.Join(fields[1:], " ")
		}
	}
	return user
}

func dockerfileDeclaresVolume(dockerfile string) bool {
	for _, line := range strings.Split(dockerfile, "\n") {
		fields := strings.Fields(line)
		if len(fields) > 0 && !strings.HasPrefix(fields[0], "#") && strings.EqualFold(fields[0], "VOLUME") {
			return true
		}
	}
	return false
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
