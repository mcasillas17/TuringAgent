package config

import (
	"fmt"
	"os"
	"strconv"
)

const (
	maxConcurrentRunsLimit = 128
	maxApprovalTTLMS       = 24 * 60 * 60 * 1000
)

type Config struct {
	ClientAPIKey             string
	InternalToken            string
	MCPSystemTokenGeneral    string
	MCPFilesTokenGeneral     string
	ApprovalJWTSecret        string
	PublicPort               int
	InternalPort             int
	DatabasePath             string
	OllamaBaseURL            string
	OllamaModel              string
	OpenAIBaseURL            string
	OpenAIAPIKey             string
	OpenAIModel              string
	JobTimeoutMS             int
	JobReaperIntervalMS      int
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
	publicPort, err := intValue("ORCHESTRATOR_PUBLIC_PORT", 3000)
	if err != nil {
		return Config{}, err
	}
	internalPort, err := intValue("ORCHESTRATOR_INTERNAL_PORT", 3001)
	if err != nil {
		return Config{}, err
	}
	jobTimeout, err := intValue("TURING_JOB_TIMEOUT_MS", 300000)
	if err != nil {
		return Config{}, err
	}
	reaperInterval, err := intValue("TURING_JOB_REAPER_INTERVAL_MS", 60000)
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
	maxTools, err := intValue("TURING_MAX_TOOL_CALLS_PER_RUN", 10)
	if err != nil {
		return Config{}, err
	}
	modelTimeout, err := intValue("TURING_MODEL_TIMEOUT_MS", 120000)
	if err != nil {
		return Config{}, err
	}
	toolTimeout, err := intValue("TURING_TOOL_TIMEOUT_MS", 30000)
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

	return Config{
		ClientAPIKey:             clientKey,
		InternalToken:            internalToken,
		MCPSystemTokenGeneral:    systemToken,
		MCPFilesTokenGeneral:     filesToken,
		ApprovalJWTSecret:        approvalSecret,
		PublicPort:               publicPort,
		InternalPort:             internalPort,
		DatabasePath:             stringValue("DATABASE_PATH", "/app/data/turing.db"),
		OllamaBaseURL:            stringValue("OLLAMA_BASE_URL", "http://host.docker.internal:11434"),
		OllamaModel:              stringValue("OLLAMA_MODEL", "llama3.2"),
		OpenAIBaseURL:            stringValue("OPENAI_BASE_URL", "https://api.openai.com/v1"),
		OpenAIAPIKey:             env["OPENAI_API_KEY"],
		OpenAIModel:              stringValue("OPENAI_MODEL", "gpt-4o-mini"),
		JobTimeoutMS:             jobTimeout,
		JobReaperIntervalMS:      reaperInterval,
		JobMaxAttempts:           maxAttempts,
		MaxConcurrentRunsGeneral: maxRuns,
		MaxToolCallsPerRun:       maxTools,
		ModelTimeoutMS:           modelTimeout,
		ToolTimeoutMS:            toolTimeout,
		ApprovalTTLMS:            approvalTTL,
		LogLevel:                 stringValue("LOG_LEVEL", "info"),
	}, nil
}
