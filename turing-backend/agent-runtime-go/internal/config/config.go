package config

import (
	"errors"
	"fmt"
	"math"
	"net/url"
	"os"
	"strconv"
	"time"

	"github.com/mcasillas17/TuringAgent/turing-backend/agent-runtime-go/internal/llm"
)

const (
	defaultApprovalTTL              = 65 * time.Second
	approvalExpiryRoundingMargin    = time.Second
	approvalWaitPollTransportMargin = 5 * time.Second
	maxConcurrentRunsLimit          = 128
	maxHeartbeatInterval            = 30 * time.Second
)

// defaultOllamaModel keeps this side in step with the orchestrator, which is
// where the effective default actually lives (sessions advertises it to
// clients). Nothing in the runtime reads cfg.OllamaModel today; the constant
// exists so the two cannot drift silently.
const defaultOllamaModel = "qwen2.5:7b"

// defaultOllamaKeepAlive must stay ABOVE the approval wait
// (TURING_APPROVAL_WAIT_TIMEOUT_MS, 71s by default). A shorter value evicts the
// model in the middle of a single run while the user is deciding whether to
// approve a tool call, throwing away the KV cache for the whole conversation
// prefix and charging a multi-second reload on the resumed answer. Still far
// tighter than Ollama's own 5 minutes, so an idle machine gets its memory back.
const defaultOllamaKeepAlive = "2m"

type Config struct {
	OrchestratorGRPCAddr string
	InternalToken        string
	WorkerID             string
	OllamaBaseURL        string
	OllamaModel          string
	OllamaKeepAlive      string
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
	keepAlive := defaultString(getenv("OLLAMA_KEEP_ALIVE"), defaultOllamaKeepAlive)
	// Validated here so a bad value is a startup failure with a readable message
	// rather than an opaque "Ollama returned 400" on every chat request.
	if _, err := llm.EncodeOllamaKeepAlive(keepAlive); err != nil {
		return Config{}, fmt.Errorf("OLLAMA_KEEP_ALIVE: %w", err)
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
		OllamaModel:          defaultString(getenv("OLLAMA_MODEL"), defaultOllamaModel),
		OllamaKeepAlive:      keepAlive,
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
