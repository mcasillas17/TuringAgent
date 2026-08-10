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
	OpenAIBaseURL      string
	OpenAIAPIKey       string
	MCPSystemBaseURL   string
	MCPFilesBaseURL    string
	MCPSystemToken     string
	MCPFilesToken      string
}

func RunWorker(ctx context.Context, cfg WorkerConfig) error {
	client := orchestrator.New(cfg.Conn, cfg.InternalToken)
	providers := map[turingv1.ModelProvider]llm.Provider{
		turingv1.ModelProvider_MODEL_PROVIDER_OPENAI_COMPATIBLE: llm.NewOpenAICompatible(cfg.OpenAIBaseURL, cfg.OpenAIAPIKey, http.DefaultClient),
	}
	toolRunner := &tools.Runner{WaitApproval: func(ctx context.Context, approvalID string) (string, error) {
		return client.WaitForApprovalToken(ctx, approvalID, 10*time.Millisecond, 5*time.Second)
	}}
	toolset := &agent.GeneralAssistantTools{
		SystemMCP:          mcp.NewClient(cfg.MCPSystemBaseURL, cfg.MCPSystemToken, http.DefaultClient),
		FilesMCP:           mcp.NewClient(cfg.MCPFilesBaseURL, cfg.MCPFilesToken, http.DefaultClient),
		Runner:             toolRunner,
		MaxToolCallsPerRun: cfg.MaxToolCallsPerRun,
	}
	executor := agent.NewGeneralAssistant(providers, client, toolset)
	runtimeWorker := worker.New(worker.Options{WorkerID: cfg.WorkerID, AgentID: turingv1.AgentId_AGENT_ID_GENERAL_ASSISTANT, MaxConcurrentRuns: cfg.MaxConcurrentRuns}, runtimeClientAdapter{client: client}, executor)
	return runtimeWorker.Run(ctx)
}

type runtimeClientAdapter struct{ client *orchestrator.Client }

func (a runtimeClientAdapter) ConnectWorker(ctx context.Context) (worker.RuntimeStream, error) {
	return a.client.ConnectWorker(ctx)
}
