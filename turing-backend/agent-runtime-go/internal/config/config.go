package config

import (
	"errors"
	"fmt"
	"math"
	"net/url"
	"os"
	"strconv"
	"time"
)

const (
	defaultApprovalTTL              = 65 * time.Second
	approvalExpiryRoundingMargin    = time.Second
	approvalWaitPollTransportMargin = 5 * time.Second
	maxConcurrentRunsLimit          = 128
	maxHeartbeatInterval            = 30 * time.Second
)

type Config struct {
	OrchestratorGRPCAddr string
	InternalToken        string
	WorkerID             string
	OllamaBaseURL        string
	OllamaModel          string
	OpenAIBaseURL        string
	OpenAIAPIKey         string
	OpenAIModel          string
	MCPSystemBaseURL     string
	MCPFilesBaseURL      string
	MCPSystemToken       string
	MCPFilesToken        string
	MaxConcurrentRuns    int
	MaxToolCallsPerRun   int
	HeartbeatInterval    time.Duration
	ModelTimeout         time.Duration
	ToolTimeout          time.Duration
	ApprovalTimeout      time.Duration
	TotalToolTimeout     time.Duration
	LogLevel             string
}

func Load() (Config, error) {
	return LoadFromEnv(os.Getenv)
}

func LoadFromEnv(getenv func(string) string) (Config, error) {
	internalToken := getenv("TURING_INTERNAL_TOKEN")
	if internalToken == "" {
		return Config{}, errors.New("missing required env var TURING_INTERNAL_TOKEN")
	}
	maxConcurrentRuns, err := intValue(getenv, "TURING_MAX_CONCURRENT_RUNS_GENERAL", 1)
	if err != nil {
		return Config{}, err
	}
	if maxConcurrentRuns < 1 || maxConcurrentRuns > maxConcurrentRunsLimit {
		return Config{}, fmt.Errorf("TURING_MAX_CONCURRENT_RUNS_GENERAL must be between 1 and %d", maxConcurrentRunsLimit)
	}
	maxToolCalls, err := intValue(getenv, "TURING_MAX_TOOL_CALLS_PER_RUN", 10)
	if err != nil {
		return Config{}, err
	}
	if maxToolCalls <= 0 {
		return Config{}, errors.New("TURING_MAX_TOOL_CALLS_PER_RUN must be greater than 0")
	}
	jobTimeout, err := durationMillisecondsValue(getenv, "TURING_JOB_TIMEOUT_MS", 300000)
	if err != nil {
		return Config{}, err
	}
	heartbeatInterval := jobTimeout / 3
	if heartbeatInterval > maxHeartbeatInterval {
		heartbeatInterval = maxHeartbeatInterval
	}
	modelTimeout, err := durationMillisecondsValue(getenv, "TURING_MODEL_TIMEOUT_MS", 120000)
	if err != nil {
		return Config{}, err
	}
	toolTimeout, err := durationMillisecondsValue(getenv, "TURING_TOOL_TIMEOUT_MS", 30000)
	if err != nil {
		return Config{}, err
	}
	approvalTTL, err := durationMillisecondsValue(getenv, "TURING_APPROVAL_TIMEOUT_MS", defaultApprovalTTL.Milliseconds())
	if err != nil {
		return Config{}, err
	}
	approvalTimeout, err := approvalWaitTimeout(getenv, approvalTTL)
	if err != nil {
		return Config{}, err
	}
	totalToolTimeout, err := durationMillisecondsValue(getenv, "TURING_TOOL_TOTAL_TIMEOUT_MS", 180000)
	if err != nil {
		return Config{}, err
	}
	if totalToolTimeout <= approvalTimeout || toolTimeout > (totalToolTimeout-approvalTimeout)/3 {
		return Config{}, errors.New("TURING_TOOL_TOTAL_TIMEOUT_MS must cover approval and tool lifecycle stages")
	}
	ollamaBaseURL, err := endpointURLValue(getenv, "OLLAMA_BASE_URL", "http://host.docker.internal:11434")
	if err != nil {
		return Config{}, err
	}
	openAIBaseURL, err := endpointURLValue(getenv, "OPENAI_BASE_URL", "https://api.openai.com/v1")
	if err != nil {
		return Config{}, err
	}
	mcpSystemBaseURL, err := endpointURLValue(getenv, "MCP_SYSTEM_BASE_URL", "http://turing-mcp-system:7100/mcp")
	if err != nil {
		return Config{}, err
	}
	mcpFilesBaseURL, err := endpointURLValue(getenv, "MCP_FILES_BASE_URL", "http://turing-mcp-files:7110/mcp")
	if err != nil {
		return Config{}, err
	}
	return Config{
		OrchestratorGRPCAddr: grpcAddr(getenv),
		InternalToken:        internalToken,
		WorkerID:             defaultString(getenv("TURING_WORKER_ID"), "worker-general-go"),
		OllamaBaseURL:        ollamaBaseURL,
		OllamaModel:          defaultString(getenv("OLLAMA_MODEL"), "llama3.2"),
		OpenAIBaseURL:        openAIBaseURL,
		OpenAIAPIKey:         getenv("OPENAI_API_KEY"),
		OpenAIModel:          defaultString(getenv("OPENAI_MODEL"), "gpt-4o-mini"),
		MCPSystemBaseURL:     mcpSystemBaseURL,
		MCPFilesBaseURL:      mcpFilesBaseURL,
		MCPSystemToken:       getenv("MCP_SYSTEM_TOKEN_GENERAL"),
		MCPFilesToken:        getenv("MCP_FILES_TOKEN_GENERAL"),
		MaxConcurrentRuns:    maxConcurrentRuns,
		MaxToolCallsPerRun:   maxToolCalls,
		HeartbeatInterval:    heartbeatInterval,
		ModelTimeout:         modelTimeout,
		ToolTimeout:          toolTimeout,
		ApprovalTimeout:      approvalTimeout,
		TotalToolTimeout:     totalToolTimeout,
		LogLevel:             defaultString(getenv("LOG_LEVEL"), "info"),
	}, nil
}

func endpointURLValue(getenv func(string) string, name string, defaultValue string) (string, error) {
	value := defaultString(getenv(name), defaultValue)
	parsed, err := url.Parse(value)
	if err != nil || !parsed.IsAbs() || parsed.Hostname() == "" ||
		(parsed.Scheme != "http" && parsed.Scheme != "https") ||
		parsed.RawQuery != "" || parsed.ForceQuery || parsed.Fragment != "" {
		return "", fmt.Errorf("%s must be an absolute http or https URL with a non-empty host and no query or fragment", name)
	}
	return value, nil
}

func grpcAddr(getenv func(string) string) string {
	if value := getenv("ORCHESTRATOR_GRPC_ADDR"); value != "" {
		return value
	}
	if value := getenv("ORCHESTRATOR_INTERNAL_GRPC_ADDR"); value != "" {
		return value
	}
	return "turing-orchestrator:3001"
}

func intValue(getenv func(string) string, name string, defaultValue int) (int, error) {
	value := getenv(name)
	if value == "" {
		return defaultValue, nil
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return 0, fmt.Errorf("%s must be an integer", name)
	}
	return parsed, nil
}

func durationMillisecondsValue(getenv func(string) string, name string, defaultValue int64) (time.Duration, error) {
	value := getenv(name)
	if value == "" {
		value = strconv.FormatInt(defaultValue, 10)
	}
	milliseconds, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		var numberError *strconv.NumError
		if errors.As(err, &numberError) && errors.Is(numberError.Err, strconv.ErrRange) {
			return 0, fmt.Errorf("%s exceeds maximum duration of %d milliseconds", name, maxDurationMilliseconds())
		}
		return 0, fmt.Errorf("%s must be an integer", name)
	}
	if milliseconds <= 0 {
		return 0, fmt.Errorf("%s must be greater than 0", name)
	}
	maxMilliseconds := maxDurationMilliseconds()
	if milliseconds > maxMilliseconds {
		return 0, fmt.Errorf("%s exceeds maximum duration of %d milliseconds", name, maxMilliseconds)
	}
	return time.Duration(milliseconds) * time.Millisecond, nil
}

func approvalWaitTimeout(getenv func(string) string, approvalTTL time.Duration) (time.Duration, error) {
	margin := approvalExpiryRoundingMargin + approvalWaitPollTransportMargin
	if approvalTTL > time.Duration(math.MaxInt64)-margin {
		return 0, fmt.Errorf("TURING_APPROVAL_TIMEOUT_MS must leave room for approval wait margin")
	}
	minimum := approvalTTL + margin
	timeout, err := durationMillisecondsValue(getenv, "TURING_APPROVAL_WAIT_TIMEOUT_MS", minimum.Milliseconds())
	if err != nil {
		return 0, err
	}
	if timeout < minimum {
		return 0, fmt.Errorf("TURING_APPROVAL_WAIT_TIMEOUT_MS must cover effective approval expiry plus poll and transport margin")
	}
	return timeout, nil
}

func maxDurationMilliseconds() int64 {
	return int64(math.MaxInt64 / int64(time.Millisecond))
}

func defaultString(value string, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}
