package config

import (
	"errors"
	"fmt"
	"math"
	"os"
	"strconv"
	"time"
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
	ModelTimeout         time.Duration
	ToolTimeout          time.Duration
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
	maxToolCalls, err := intValue(getenv, "TURING_MAX_TOOL_CALLS_PER_RUN", 10)
	if err != nil {
		return Config{}, err
	}
	if maxToolCalls <= 0 {
		return Config{}, errors.New("TURING_MAX_TOOL_CALLS_PER_RUN must be greater than 0")
	}
	modelTimeout, err := durationMillisecondsValue(getenv, "TURING_MODEL_TIMEOUT_MS", 120000)
	if err != nil {
		return Config{}, err
	}
	toolTimeout, err := durationMillisecondsValue(getenv, "TURING_TOOL_TIMEOUT_MS", 30000)
	if err != nil {
		return Config{}, err
	}
	return Config{
		OrchestratorGRPCAddr: grpcAddr(getenv),
		InternalToken:        internalToken,
		WorkerID:             defaultString(getenv("TURING_WORKER_ID"), "worker-general-go"),
		OllamaBaseURL:        defaultString(getenv("OLLAMA_BASE_URL"), "http://host.docker.internal:11434"),
		OllamaModel:          defaultString(getenv("OLLAMA_MODEL"), "llama3.2"),
		OpenAIBaseURL:        defaultString(getenv("OPENAI_BASE_URL"), "https://api.openai.com/v1"),
		OpenAIAPIKey:         getenv("OPENAI_API_KEY"),
		OpenAIModel:          defaultString(getenv("OPENAI_MODEL"), "gpt-4o-mini"),
		MCPSystemBaseURL:     defaultString(getenv("MCP_SYSTEM_BASE_URL"), "http://turing-mcp-system:7100/mcp"),
		MCPFilesBaseURL:      defaultString(getenv("MCP_FILES_BASE_URL"), "http://turing-mcp-files:7110/mcp"),
		MCPSystemToken:       getenv("MCP_SYSTEM_TOKEN_GENERAL"),
		MCPFilesToken:        getenv("MCP_FILES_TOKEN_GENERAL"),
		MaxConcurrentRuns:    maxConcurrentRuns,
		MaxToolCallsPerRun:   maxToolCalls,
		ModelTimeout:         modelTimeout,
		ToolTimeout:          toolTimeout,
		LogLevel:             defaultString(getenv("LOG_LEVEL"), "info"),
	}, nil
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

func maxDurationMilliseconds() int64 {
	return int64(math.MaxInt64 / int64(time.Millisecond))
}

func defaultString(value string, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}
