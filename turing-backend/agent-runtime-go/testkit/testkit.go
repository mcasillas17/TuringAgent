package testkit

import (
	"context"
	"net/http"
	"time"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"github.com/mcasillas17/TuringAgent/turing-backend/agent-runtime-go/internal/agent"
	"github.com/mcasillas17/TuringAgent/turing-backend/agent-runtime-go/internal/llm"
	"github.com/mcasillas17/TuringAgent/turing-backend/agent-runtime-go/internal/mcp"
	"github.com/mcasillas17/TuringAgent/turing-backend/agent-runtime-go/internal/orchestrator"
	"github.com/mcasillas17/TuringAgent/turing-backend/agent-runtime-go/internal/tools"
	"github.com/mcasillas17/TuringAgent/turing-backend/agent-runtime-go/internal/worker"
	"google.golang.org/grpc"
)

type WorkerConfig struct {
	Conn                *grpc.ClientConn
	RuntimeToken        string
	WorkerID            string
	MaxConcurrentRuns   int
	MaxToolCallsPerRun  int
	ModelTimeout        time.Duration
	ToolTimeout         time.Duration
	ApprovalTimeout     time.Duration
	TotalToolTimeout    time.Duration
	OpenAIBaseURL       string
	OpenAIAPIKey        string
	OpenAIModel         string
	ContextWindowTokens int
	MaxOutputTokens     int
	MCPSystemBaseURL    string
	MCPFilesBaseURL     string
	MCPSystemToken      string
	MCPFilesToken       string
	DiscoveredTools     []*turingv1.DiscoveredTool
}

type WorkerExecutor interface {
	Execute(context.Context, *turingv1.AgentJob, func(*turingv1.RuntimeUpdate) error) error
}

func RunWorker(ctx context.Context, cfg WorkerConfig) error {
	client := orchestrator.New(cfg.Conn, cfg.RuntimeToken)
	openAIProvider, err := llm.NewOpenAICompatibleWithLimits(
		cfg.OpenAIBaseURL,
		cfg.OpenAIAPIKey,
		http.DefaultClient,
		cfg.contextWindowTokens(),
		cfg.maxOutputTokens(),
	)
	if err != nil {
		return err
	}
	providers := map[turingv1.ModelProvider]llm.Provider{
		turingv1.ModelProvider_MODEL_PROVIDER_OPENAI_COMPATIBLE: openAIProvider,
	}

	toolRunner := &tools.Runner{WaitApproval: func(ctx context.Context, approvalID string) (string, error) {
		return client.WaitForApprovalToken(ctx, approvalID, 10*time.Millisecond, cfg.approvalTimeout())
	}}
	toolset := &agent.GeneralAssistantTools{
		SystemMCP:          mcp.NewClient(cfg.MCPSystemBaseURL, cfg.MCPSystemToken, http.DefaultClient),
		FilesMCP:           mcp.NewClient(cfg.MCPFilesBaseURL, cfg.MCPFilesToken, http.DefaultClient),
		Runner:             toolRunner,
		MaxToolCallsPerRun: cfg.MaxToolCallsPerRun,
		ModelTimeout:       cfg.modelTimeout(),
		ToolTimeout:        cfg.toolTimeout(),
		TotalToolTimeout:   cfg.totalToolTimeout(),
	}
	executor := agent.NewGeneralAssistant(providers, client, toolset)
	runtimeWorker := worker.New(worker.Options{
		WorkerID:                 cfg.WorkerID,
		AgentID:                  turingv1.AgentId_AGENT_ID_GENERAL_ASSISTANT,
		MaxConcurrentRuns:        cfg.MaxConcurrentRuns,
		DisconnectCleanupTimeout: cfg.totalToolTimeout(),
		Models:                   cfg.models(),
		DiscoverTools:            executor.AdvertisedTools,
	}, runtimeClientAdapter{client: client}, executor)
	return runtimeWorker.Run(ctx)
}

func (cfg WorkerConfig) contextWindowTokens() int {
	if cfg.ContextWindowTokens > 0 {
		return cfg.ContextWindowTokens
	}
	return llm.DefaultContextWindowTokens
}

func (cfg WorkerConfig) maxOutputTokens() int {
	if cfg.MaxOutputTokens > 0 {
		return cfg.MaxOutputTokens
	}
	return llm.DefaultMaxOutputTokens
}

func RunWorkerWithExecutor(ctx context.Context, cfg WorkerConfig, executor WorkerExecutor) error {
	client := orchestrator.New(cfg.Conn, cfg.RuntimeToken)
	var discoverTools func(context.Context) ([]*turingv1.DiscoveredTool, error)
	if cfg.DiscoveredTools != nil {
		discoverTools = func(context.Context) ([]*turingv1.DiscoveredTool, error) {
			return cfg.DiscoveredTools, nil
		}
	}
	runtimeWorker := worker.New(worker.Options{
		WorkerID:                 cfg.WorkerID,
		AgentID:                  turingv1.AgentId_AGENT_ID_GENERAL_ASSISTANT,
		MaxConcurrentRuns:        cfg.MaxConcurrentRuns,
		DisconnectCleanupTimeout: cfg.totalToolTimeout(),
		Models:                   cfg.models(),
		DiscoverTools:            discoverTools,
	}, runtimeClientAdapter{client: client}, executor)
	return runtimeWorker.Run(ctx)
}

func (cfg WorkerConfig) modelTimeout() time.Duration {
	if cfg.ModelTimeout > 0 {
		return cfg.ModelTimeout
	}
	return 120 * time.Second
}

func (cfg WorkerConfig) toolTimeout() time.Duration {
	if cfg.ToolTimeout > 0 {
		return cfg.ToolTimeout
	}
	return 30 * time.Second
}

func (cfg WorkerConfig) approvalTimeout() time.Duration {
	if cfg.ApprovalTimeout > 0 {
		return cfg.ApprovalTimeout
	}
	return 5 * time.Second
}

func (cfg WorkerConfig) totalToolTimeout() time.Duration {
	if cfg.TotalToolTimeout > 0 {
		return cfg.TotalToolTimeout
	}
	return 30 * time.Second
}

func (cfg WorkerConfig) models() []*turingv1.ModelCapability {
	model := cfg.OpenAIModel
	if model == "" {
		model = "gpt-4o-mini"
	}
	return []*turingv1.ModelCapability{{
		Provider:         turingv1.ModelProvider_MODEL_PROVIDER_OPENAI_COMPATIBLE,
		Model:            model,
		MaxContextTokens: int32(cfg.contextWindowTokens()),
	}}
}

type runtimeClientAdapter struct{ client *orchestrator.Client }

func (a runtimeClientAdapter) ConnectWorker(ctx context.Context) (worker.RuntimeStream, error) {
	return a.client.ConnectWorker(ctx)
}
