package testkit

import (
	"context"
	"time"

	"github.com/mcasillas17/TuringAgent/turing-backend/approvalpreview"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/app"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/config"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/ids"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/safejson"
	approvalsvc "github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/service/approvals"
	"google.golang.org/grpc"
)

type Config struct {
	ClientAPIKey             string
	RuntimeToken             string
	ApprovalConsumerToken    string
	ApprovalJWTSecret        string
	EgressSigningSecret      string
	DatabasePath             string
	OllamaModel              string
	OpenAIBaseURL            string
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

func (r *Repository) RegisterLocalMCPServer(ctx context.Context, name string) error {
	registration, err := r.inner.RegisterMCPServer(ctx, repository.ImportedMCPServer{
		Name: name, URL: "http://" + name + ":9000/mcp", Tier: repository.MCPServerTierLocalContainer,
	})
	if err != nil {
		return err
	}
	server := registration.Server
	if err := r.inner.SetMCPServerEnabled(ctx, server.ID, true); err != nil {
		return err
	}
	return r.inner.ReplaceMCPServerTools(ctx, server.ID, []repository.MCPServerTool{{
		Name: name + ".inspect", Policy: "approval_required", SchemaJSON: `{"type":"object"}`,
	}})
}

func NewApp(cfg Config) (*App, error) {
	inner, err := app.New(config.Config{
		ClientAPIKey:             cfg.ClientAPIKey,
		RuntimeToken:             cfg.RuntimeToken,
		ApprovalConsumerToken:    cfg.ApprovalConsumerToken,
		ApprovalJWTSecret:        cfg.ApprovalJWTSecret,
		EgressSigningSecret:      cfg.EgressSigningSecret,
		DatabasePath:             cfg.DatabasePath,
		OllamaModel:              cfg.OllamaModel,
		OpenAIBaseURL:            cfg.OpenAIBaseURL,
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

func (a *App) SetPreviewEndpoint(endpoint string) {
	a.inner.ApprovalService.SetPreviewEndpoint(endpoint)
}

type FileApproval struct {
	ApprovalID      string
	ProvenanceToken string
	SessionID       string
	RunID           string
	ArgsHash        string
}

// CreateFileApproval records the same immutable call/args used by the runtime,
// enabling cross-module protected-file tests without exporting storage internals.
func (a *App) CreateFileApproval(ctx context.Context, tool string, args map[string]any) (FileApproval, error) {
	session, err := a.inner.Repository.CreateSession(ctx, "file preview integration")
	if err != nil {
		return FileApproval{}, err
	}
	run, err := a.inner.Repository.EnqueueUserMessage(ctx, repository.EnqueueUserMessageInput{SessionID: session.SessionID, Content: "review a file", AgentID: "general_assistant", ModelProvider: "ollama", Model: "test"})
	if err != nil {
		return FileApproval{}, err
	}
	if err := a.inner.Repository.MarkRunRunning(ctx, run.RunID); err != nil {
		return FileApproval{}, err
	}
	raw, err := safejson.MarshalCanonical(args)
	if err != nil {
		return FileApproval{}, err
	}
	hash := approvalpreview.Hash(string(raw))
	callID := ids.New("call")
	if err := a.inner.Repository.RecordToolCallBefore(ctx, repository.ToolCallRecord{ToolCallID: callID, RunID: run.RunID}, "general_assistant", "files", tool, string(raw), hash); err != nil {
		return FileApproval{}, err
	}
	id, err := a.inner.ApprovalService.CreateApprovalForTool(ctx, run.RunID, callID, "general_assistant", tool, args)
	if err != nil {
		return FileApproval{}, err
	}
	logical, _ := args["path"].(string)
	token, err := a.inner.ApprovalService.IssueToolProvenance(ctx, approvalsvc.ProvenanceRequest{SessionID: session.SessionID, RunID: run.RunID, AgentID: "general_assistant", ToolName: tool, ArgsHash: hash, LogicalPath: logical})
	return FileApproval{ApprovalID: id, ProvenanceToken: token, SessionID: session.SessionID, RunID: run.RunID, ArgsHash: hash}, err
}

func (a *App) WaitForSessionEventSubscriber(ctx context.Context, sessionID string) error {
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	for {
		if a != nil && a.inner != nil && a.inner.EventBus.SessionSubscriberCount(sessionID) > 0 {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
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
