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
	Conn               *grpc.ClientConn
	InternalToken      string
	WorkerID           string
	MaxConcurrentRuns  int
	MaxToolCallsPerRun int
	ModelTimeout       time.Duration
	ToolTimeout        time.Duration
	ApprovalTimeout    time.Duration
	TotalToolTimeout   time.Duration
	OpenAIBaseURL      string
	OpenAIAPIKey       string
	MCPSystemBaseURL   string
	MCPFilesBaseURL    string
	MCPSystemToken     string
	MCPFilesToken      string
}

type WorkerExecutor interface {
	Execute(context.Context, *turingv1.AgentJob, func(*turingv1.RuntimeUpdate) error) error
}

func RunWorker(ctx context.Context, cfg WorkerConfig) error {
	client := orchestrator.New(cfg.Conn, cfg.InternalToken)
	providers := map[turingv1.ModelProvider]llm.Provider{
		turingv1.ModelProvider_MODEL_PROVIDER_OPENAI_COMPATIBLE: llm.NewOpenAICompatible(cfg.OpenAIBaseURL, cfg.OpenAIAPIKey, http.DefaultClient),
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
	}, runtimeClientAdapter{client: client}, executor)
	return runtimeWorker.Run(ctx)
}

func RunWorkerWithExecutor(ctx context.Context, cfg WorkerConfig, executor WorkerExecutor) error {
	client := orchestrator.New(cfg.Conn, cfg.InternalToken)
	runtimeWorker := worker.New(worker.Options{
		WorkerID:                 cfg.WorkerID,
		AgentID:                  turingv1.AgentId_AGENT_ID_GENERAL_ASSISTANT,
		MaxConcurrentRuns:        cfg.MaxConcurrentRuns,
		DisconnectCleanupTimeout: cfg.totalToolTimeout(),
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

type runtimeClientAdapter struct{ client *orchestrator.Client }

func (a runtimeClientAdapter) ConnectWorker(ctx context.Context) (worker.RuntimeStream, error) {
	return a.client.ConnectWorker(ctx)
}
