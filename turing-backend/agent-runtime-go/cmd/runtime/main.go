package main

import (
	"context"
	"log"
	"net/http"
	"os/signal"
	"syscall"
	"time"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"github.com/mcasillas17/TuringAgent/turing-backend/agent-runtime-go/internal/agent"
	"github.com/mcasillas17/TuringAgent/turing-backend/agent-runtime-go/internal/config"
	"github.com/mcasillas17/TuringAgent/turing-backend/agent-runtime-go/internal/llm"
	"github.com/mcasillas17/TuringAgent/turing-backend/agent-runtime-go/internal/mcp"
	"github.com/mcasillas17/TuringAgent/turing-backend/agent-runtime-go/internal/orchestrator"
	"github.com/mcasillas17/TuringAgent/turing-backend/agent-runtime-go/internal/tools"
	"github.com/mcasillas17/TuringAgent/turing-backend/agent-runtime-go/internal/worker"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	client, err := orchestrator.Dial(ctx, cfg.OrchestratorGRPCAddr, cfg.InternalToken)
	if err != nil {
		return err
	}
	defer func() { _ = client.Close() }()
	providers := map[turingv1.ModelProvider]llm.Provider{
		turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA: llm.NewOllama(cfg.OllamaBaseURL, http.DefaultClient),
	}
	if cfg.OpenAIAPIKey != "" {
		providers[turingv1.ModelProvider_MODEL_PROVIDER_OPENAI_COMPATIBLE] = llm.NewOpenAICompatible(cfg.OpenAIBaseURL, cfg.OpenAIAPIKey, http.DefaultClient)
	}
	toolRunner := &tools.Runner{WaitApproval: func(ctx context.Context, approvalID string) (string, error) {
		return client.WaitForApprovalToken(ctx, approvalID, time.Second, cfg.ApprovalTimeout)
	}}
	toolset := &agent.GeneralAssistantTools{
		SystemMCP:          mcp.NewClient(cfg.MCPSystemBaseURL, cfg.MCPSystemToken, http.DefaultClient),
		FilesMCP:           mcp.NewClient(cfg.MCPFilesBaseURL, cfg.MCPFilesToken, http.DefaultClient),
		Runner:             toolRunner,
		MaxToolCallsPerRun: cfg.MaxToolCallsPerRun,
		ModelTimeout:       cfg.ModelTimeout,
		ToolTimeout:        cfg.ToolTimeout,
		TotalToolTimeout:   cfg.TotalToolTimeout,
	}
	executor := agent.NewGeneralAssistant(providers, client, toolset)
	runtimeWorker := worker.New(worker.Options{WorkerID: cfg.WorkerID, AgentID: turingv1.AgentId_AGENT_ID_GENERAL_ASSISTANT, MaxConcurrentRuns: cfg.MaxConcurrentRuns}, runtimeClientAdapter{client: client}, executor)
	return runtimeWorker.Run(ctx)
}

type runtimeClientAdapter struct{ client *orchestrator.Client }

func (a runtimeClientAdapter) ConnectWorker(ctx context.Context) (worker.RuntimeStream, error) {
	return a.client.ConnectWorker(ctx)
}
