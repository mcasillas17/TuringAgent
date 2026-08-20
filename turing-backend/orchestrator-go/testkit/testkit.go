package testkit

import (
	"context"

	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/app"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/config"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
	"google.golang.org/grpc"
)

type Config struct {
	ClientAPIKey             string
	RuntimeToken             string
	ApprovalConsumerToken    string
	ApprovalJWTSecret        string
	DatabasePath             string
	OllamaModel              string
	OpenAIModel              string
	OpenAIEnabled            bool
	FilesMCPEnabled          bool
	MaxConcurrentRunsGeneral int
	MaxToolCallsPerRun       int
	ApprovalTTLMS            int
}

type App struct {
	PublicServer   *grpc.Server
	InternalServer *grpc.Server
	Repository     *Repository
	inner          *app.App
}

type Repository struct{ inner *repository.Repository }

type Run struct {
	Status          string
	ExecutionActive bool
}

func NewApp(cfg Config) (*App, error) {
	inner, err := app.New(config.Config{
		ClientAPIKey:             cfg.ClientAPIKey,
		RuntimeToken:             cfg.RuntimeToken,
		ApprovalConsumerToken:    cfg.ApprovalConsumerToken,
		ApprovalJWTSecret:        cfg.ApprovalJWTSecret,
		DatabasePath:             cfg.DatabasePath,
		OllamaModel:              cfg.OllamaModel,
		OpenAIModel:              cfg.OpenAIModel,
		OpenAIEnabled:            cfg.OpenAIEnabled,
		FilesMCPEnabled:          cfg.FilesMCPEnabled,
		MaxConcurrentRunsGeneral: cfg.MaxConcurrentRunsGeneral,
		MaxToolCallsPerRun:       cfg.MaxToolCallsPerRun,
		ApprovalTTLMS:            cfg.ApprovalTTLMS,
	})
	if err != nil {
		return nil, err
	}
	return &App{PublicServer: inner.PublicServer, InternalServer: inner.InternalServer, Repository: &Repository{inner: inner.Repository}, inner: inner}, nil
}

func (a *App) Stop() {
	if a != nil && a.inner != nil {
		a.inner.Stop()
	}
}

func (a *App) ValidateRuntimeRoute(ctx context.Context, agentID, provider, model string) error {
	return a.inner.RuntimeService.ValidateRouting(ctx, repository.RoutingRequirements{
		AgentID:       agentID,
		ModelProvider: provider,
		Model:         model,
	})
}

func (r *Repository) GetRun(ctx context.Context, runID string) (Run, error) {
	run, err := r.inner.GetRun(ctx, runID)
	if err != nil {
		return Run{}, err
	}
	return Run{Status: run.Status, ExecutionActive: run.ExecutionActive}, nil
}
