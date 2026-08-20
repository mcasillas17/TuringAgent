package config

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/secretbox"
)

const (
	maxConcurrentRunsLimit = 128
	maxApprovalTTLMS       = 24 * 60 * 60 * 1000
	maxContextWindowTokens = 16 * 1024 * 1024
)

type Config struct {
	ClientAPIKey          string
	InternalToken         string
	MCPSystemTokenGeneral string
	MCPFilesTokenGeneral  string
	MCPFilesBaseURL       string
	ApprovalJWTSecret     string
	// IntegrationKey seals third-party credentials before they are stored.
	// Optional: when it is empty, connecting an account is refused with a
	// reason rather than the credential being stored in the clear.
	IntegrationKey            string
	PublicPort                int
	InternalPort              int
	DatabasePath              string
	SkillsRoot                string
	OllamaBaseURL             string
	OllamaModel               string
	OllamaContextWindowTokens int
	OpenAIBaseURL             string
	OpenAIAPIKey              string
	OpenAIModel               string
	OpenAIContextWindowTokens int
	// AgentCredentialNames is the set of credential names an external agent may
	// refer to — names only. The keys themselves are decoded, counted and
	// dropped: the orchestrator never calls a third-party API, so holding the
	// secret would buy nothing and risk it reaching a log or a response.
	AgentCredentialNames     []string
	JobTimeoutMS             int
	JobReaperIntervalMS      int
	AutomationTickMS         int
	JobMaxAttempts           int
	MaxConcurrentRunsGeneral int
	MaxToolCallsPerRun       int
	ModelTimeoutMS           int
	ToolTimeoutMS            int
	ApprovalTTLMS            int
	LogLevel                 string
}

func Load() (Config, error) {
	env := map[string]string{}
	for _, item := range os.Environ() {
		for i := 0; i < len(item); i++ {
			if item[i] == '=' {
				env[item[:i]] = item[i+1:]
				break
			}
		}
	}
	return LoadFromMap(env)
}

func LoadFromMap(env map[string]string) (Config, error) {
	required := func(name string) (string, error) {
		if env[name] == "" {
			return "", fmt.Errorf("missing required env var %s", name)
		}
		return env[name], nil
	}
	intValue := func(name string, fallback int) (int, error) {
		raw := env[name]
		if raw == "" {
			return fallback, nil
		}
		n, err := strconv.Atoi(raw)
		if err != nil || n < 0 {
			return 0, fmt.Errorf("invalid integer env var %s", name)
		}
		return n, nil
	}
	positiveIntValue := func(name string, fallback int) (int, error) {
		n, err := intValue(name, fallback)
		if err != nil {
			return 0, err
		}
		if n <= 0 {
			return 0, fmt.Errorf("%s must be greater than 0", name)
		}
		return n, nil
	}
	durationMillisecondsValue := func(name string, fallback int) (int, error) {
		n, err := positiveIntValue(name, fallback)
		if err != nil {
			return 0, err
		}
		maxMilliseconds := int64(math.MaxInt64 / int64(time.Millisecond))
		if int64(n) > maxMilliseconds {
			return 0, fmt.Errorf("%s exceeds maximum duration of %d milliseconds", name, maxMilliseconds)
		}
		return n, nil
	}
	stringValue := func(name, fallback string) string {
		if env[name] != "" {
			return env[name]
		}
		return fallback
	}

	clientKey, err := required("TURING_CLIENT_API_KEY")
	if err != nil {
		return Config{}, err
	}
	internalToken, err := required("TURING_INTERNAL_TOKEN")
	if err != nil {
		return Config{}, err
	}
	systemToken, err := required("MCP_SYSTEM_TOKEN_GENERAL")
	if err != nil {
		return Config{}, err
	}
	filesToken, err := required("MCP_FILES_TOKEN_GENERAL")
	if err != nil {
		return Config{}, err
	}
	approvalSecret, err := required("TURING_APPROVAL_JWT_SECRET")
	if err != nil {
		return Config{}, err
	}
	// Validated at startup rather than at the first connect attempt: a key
	// that is present but malformed is a misconfiguration, and finding out
	// about it while pasting a token is finding out too late.
	integrationKey := env["TURING_INTEGRATION_KEY"]
	if integrationKey != "" {
		if _, err := secretbox.ParseKey(integrationKey); err != nil {
			return Config{}, fmt.Errorf("invalid TURING_INTEGRATION_KEY: %w", err)
		}
	}
	publicPort, err := intValue("ORCHESTRATOR_PUBLIC_PORT", 3000)
	if err != nil {
		return Config{}, err
	}
	internalPort, err := intValue("ORCHESTRATOR_INTERNAL_PORT", 3001)
	if err != nil {
		return Config{}, err
	}
	jobTimeout, err := durationMillisecondsValue("TURING_JOB_TIMEOUT_MS", 300000)
	if err != nil {
		return Config{}, err
	}
	reaperInterval, err := intValue("TURING_JOB_REAPER_INTERVAL_MS", 60000)
	if err != nil {
		return Config{}, err
	}
	// 0 turns the scheduler off entirely, which is the same shape as the job
	// reaper's interval and the only honest way to say "do not run unattended
	// work on this machine".
	automationTick, err := intValue("TURING_AUTOMATION_TICK_MS", 30000)
	if err != nil {
		return Config{}, err
	}
	maxAttempts, err := intValue("TURING_JOB_MAX_ATTEMPTS", 3)
	if err != nil {
		return Config{}, err
	}
	maxRuns, err := intValue("TURING_MAX_CONCURRENT_RUNS_GENERAL", 1)
	if err != nil {
		return Config{}, err
	}
	if maxRuns < 1 || maxRuns > maxConcurrentRunsLimit {
		return Config{}, fmt.Errorf("TURING_MAX_CONCURRENT_RUNS_GENERAL must be between 1 and %d", maxConcurrentRunsLimit)
	}
	ollamaContextWindowTokens, err := positiveIntValue("OLLAMA_CONTEXT_WINDOW_TOKENS", 32768)
	if err != nil {
		return Config{}, err
	}
	if ollamaContextWindowTokens > maxContextWindowTokens {
		return Config{}, fmt.Errorf("OLLAMA_CONTEXT_WINDOW_TOKENS must be between 1 and %d", maxContextWindowTokens)
	}
	openAIContextWindowTokens, err := positiveIntValue("OPENAI_CONTEXT_WINDOW_TOKENS", 32768)
	if err != nil {
		return Config{}, err
	}
	if openAIContextWindowTokens > maxContextWindowTokens {
		return Config{}, fmt.Errorf("OPENAI_CONTEXT_WINDOW_TOKENS must be between 1 and %d", maxContextWindowTokens)
	}
	maxTools, err := positiveIntValue("TURING_MAX_TOOL_CALLS_PER_RUN", 10)
	if err != nil {
		return Config{}, err
	}
	modelTimeout, err := durationMillisecondsValue("TURING_MODEL_TIMEOUT_MS", 120000)
	if err != nil {
		return Config{}, err
	}
	toolTimeout, err := durationMillisecondsValue("TURING_TOOL_TIMEOUT_MS", 30000)
	if err != nil {
		return Config{}, err
	}
	approvalTTL, err := intValue("TURING_APPROVAL_TIMEOUT_MS", 65000)
	if err != nil {
		return Config{}, err
	}
	if approvalTTL <= 0 || approvalTTL > maxApprovalTTLMS {
		return Config{}, fmt.Errorf("invalid integer env var TURING_APPROVAL_TIMEOUT_MS")
	}
	agentAPIKeys, err := ParseAgentAPIKeys(env[AgentAPIKeysVar])
	if err != nil {
		return Config{}, err
	}

	skillsRoot := stringValue("SKILLS_ROOT", "/skills")
	if !filepath.IsAbs(skillsRoot) || filepath.Clean(skillsRoot) != skillsRoot {
		return Config{}, fmt.Errorf("SKILLS_ROOT must be a clean absolute path")
	}
	return Config{
		ClientAPIKey:              clientKey,
		InternalToken:             internalToken,
		MCPSystemTokenGeneral:     systemToken,
		MCPFilesTokenGeneral:      filesToken,
		MCPFilesBaseURL:           stringValue("MCP_FILES_BASE_URL", "http://turing-mcp-files:7110/mcp"),
		ApprovalJWTSecret:         approvalSecret,
		IntegrationKey:            integrationKey,
		PublicPort:                publicPort,
		InternalPort:              internalPort,
		DatabasePath:              stringValue("DATABASE_PATH", "/app/data/turing.db"),
		SkillsRoot:                skillsRoot,
		OllamaBaseURL:             stringValue("OLLAMA_BASE_URL", "http://host.docker.internal:11434"),
		OllamaModel:               stringValue("OLLAMA_MODEL", "qwen2.5:7b"),
		OllamaContextWindowTokens: ollamaContextWindowTokens,
		OpenAIBaseURL:             stringValue("OPENAI_BASE_URL", "https://api.openai.com/v1"),
		OpenAIAPIKey:              env["OPENAI_API_KEY"],
		OpenAIModel:               stringValue("OPENAI_MODEL", "gpt-4o-mini"),
		OpenAIContextWindowTokens: openAIContextWindowTokens,
		AgentCredentialNames:      AgentCredentialNames(agentAPIKeys),
		JobTimeoutMS:              jobTimeout,
		JobReaperIntervalMS:       reaperInterval,
		AutomationTickMS:          automationTick,
		JobMaxAttempts:            maxAttempts,
		MaxConcurrentRunsGeneral:  maxRuns,
		MaxToolCallsPerRun:        maxTools,
		ModelTimeoutMS:            modelTimeout,
		ToolTimeoutMS:             toolTimeout,
		ApprovalTTLMS:             approvalTTL,
		LogLevel:                  stringValue("LOG_LEVEL", "info"),
	}, nil
}
