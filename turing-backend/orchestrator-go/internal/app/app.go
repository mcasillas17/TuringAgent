package app

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/auth"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/config"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/db"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/secretbox"
	agentsvc "github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/service/agents"
	approvalsvc "github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/service/approvals"
	auditsvc "github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/service/audit"
	automationsvc "github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/service/automations"
	chatsvc "github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/service/chat"
	eventsvc "github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/service/events"
	integrationsvc "github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/service/integrations"
	mcpregistrysvc "github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/service/mcpregistry"
	runtimesvc "github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/service/runtime"
	sessionsvc "github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/service/sessions"
	skillsvc "github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/service/skills"
	telemetrysvc "github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/service/telemetry"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/skillfiles"
	"google.golang.org/grpc"
)

const maxGRPCMessageSize = 4 * 1024 * 1024
const gracefulStopTimeout = 5 * time.Second
const defaultDeletionReconcileInterval = time.Minute

type App struct {
	PublicServer   *grpc.Server
	InternalServer *grpc.Server

	Repository         *repository.Repository
	EventBus           *eventsvc.Bus
	RuntimeService     *runtimesvc.Server
	IntegrationService *integrationsvc.Server
	SessionService     *sessionsvc.Server
	EventService       *eventsvc.Server
	ChatService        *chatsvc.Server
	ApprovalService    *approvalsvc.Server
	AuditService       *auditsvc.Server
	MCPRegistryService *mcpregistrysvc.Server
	HealthService      *HealthServer
	// InternalIdentityNames is the exact set of least-privilege identity
	// names wired into InternalServer's authorization interceptors — names
	// only, never the bearer tokens or the live, mutable allowlists those
	// identities carry. Exposed so tests can assert against the real
	// configuration — e.g. that every identity name here is a value the
	// audit_logs.actor_type CHECK constraint accepts — rather than a
	// hardcoded copy that can silently drift from it.
	InternalIdentityNames []string

	database     *db.DB
	stopOnce     sync.Once
	reaperCancel context.CancelFunc
	reaperDone   chan struct{}
	// The scheduler gets its own cancel/done pair rather than sharing the
	// reaper's: they stop independently, and a shutdown that waited on one
	// while the other was still firing runs would be a shutdown that queues
	// work on its way out.
	schedulerCancel         context.CancelFunc
	schedulerDone           chan struct{}
	deletionReconcileCancel context.CancelFunc
	deletionReconcileDone   chan struct{}
	authFailures            *auth.AsyncFailureRecorder
}

func boundedAppDiagnostic(message string, limit int) string {
	message = strings.ToValidUTF8(message, "\uFFFD")
	if len(message) <= limit {
		return message
	}
	message = message[:limit]
	for !utf8.ValidString(message) {
		message = message[:len(message)-1]
	}
	return message
}

func New(cfg config.Config) (*App, error) {
	skillsRoot := cfg.SkillsRoot
	if skillsRoot == "" {
		skillsRoot = "/skills"
	}
	database, err := db.Open(cfg.DatabasePath)
	if err != nil {
		return nil, err
	}
	if err := db.ApplyMigrationsWithSkillsRoot(context.Background(), database, skillsRoot); err != nil {
		_ = database.Close()
		return nil, err
	}
	schemaVersion, err := db.LatestSchemaVersion()
	if err != nil {
		_ = database.Close()
		return nil, errors.New("initialize schema version")
	}

	repo := repository.New(database)
	repo.SetSkillStore(skillfiles.New(skillsRoot))
	if err := repo.ReconcileSkills(context.Background()); err != nil {
		_ = database.Close()
		return nil, fmt.Errorf("reconcile skills: %w", err)
	}
	// Conversations created before naming moved to the backend are all called
	// "New chat". Naming only happens as a message is enqueued, so without a
	// pass at startup a conversation the user never writes to again would keep
	// that placeholder forever.
	if _, err := repo.BackfillSessionTitles(context.Background()); err != nil {
		_ = database.Close()
		return nil, fmt.Errorf("backfill session titles: %w", err)
	}
	eventBus := eventsvc.NewBus(128)
	recoveredEvents, err := repo.RecoverAllActiveAssignmentsWithLimit(context.Background(), cfg.JobMaxAttempts)
	if err != nil {
		_ = database.Close()
		return nil, err
	}
	approvalService := approvalsvc.New(repo, eventBus, cfg.ApprovalJWTSecret, time.Duration(cfg.ApprovalTTLMS)*time.Millisecond)
	maxConcurrentRuns := cfg.MaxConcurrentRunsGeneral
	if maxConcurrentRuns <= 0 {
		maxConcurrentRuns = 1
	}
	legacyModels := []*turingv1.ModelCapability{{
		Provider:         turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA,
		Model:            cfg.OllamaModel,
		MaxContextTokens: int32(cfg.OllamaContextWindowTokens),
	}}
	if cfg.OpenAIEnabled {
		legacyModels = append(legacyModels, &turingv1.ModelCapability{
			Provider:         turingv1.ModelProvider_MODEL_PROVIDER_OPENAI_COMPATIBLE,
			Model:            cfg.OpenAIModel,
			MaxContextTokens: int32(cfg.OpenAIContextWindowTokens),
		})
	}
	runtimeService := runtimesvc.NewWithConfig(repo, eventBus, runtimesvc.DispatchConfig{
		MaxConcurrentRuns: maxConcurrentRuns,
		LeaseDuration:     time.Duration(cfg.JobTimeoutMS) * time.Millisecond,
		MaxAttempts:       cfg.JobMaxAttempts,
		LegacyCapabilities: &runtimesvc.LegacyCapabilityProfile{
			Models:                      legacyModels,
			AgentIds:                    []turingv1.AgentId{turingv1.AgentId_AGENT_ID_GENERAL_ASSISTANT},
			Tools:                       runtimesvc.LegacyPolicyToolCapabilities(),
			ExternalAgentCredentialRefs: append([]string(nil), cfg.AgentCredentialNames...),
			SupportsExternalAgents:      len(cfg.AgentCredentialNames) > 0,
		},
	}, approvalService)
	sessionService := sessionsvc.New(repo, cfg, runtimeService, eventBus)
	sessionService.SetArtifactCleaner(sessionsvc.NewMCPArtifactCleaner(
		cfg.MCPFilesBaseURL,
		cfg.MCPFilesCleanupToken,
		nil,
	))
	if err := sessionService.ResumePendingDeletions(context.Background()); err != nil {
		_ = database.Close()
		return nil, fmt.Errorf("resume pending session deletions: %w", err)
	}
	skillService := skillsvc.New(repo)
	agentService := agentsvc.New(repo, cfg.AgentCredentialNames)
	automationService := automationsvc.New(repo)
	eventService := eventsvc.NewServer(repo, eventBus)
	chatService := chatsvc.NewWithEgressConfig(repo, eventBus, runtimeService, cfg.OllamaModel, cfg.OpenAIModel, chatsvc.EgressConfig{
		OpenAIBaseURL: cfg.OpenAIBaseURL,
		SigningSecret: cfg.EgressSigningSecret,
	})
	// Passing the server-side approval signing secret means the cursor MAC key
	// is derived deterministically (auditsvc.New domain-separates it), so a
	// cursor minted before a restart is still accepted after one — as long as
	// this install's approval secret has not changed. The secret is server-side
	// and shared with approval-verifying components, never public clients; only
	// the orchestrator derives this cursor subkey. A public bearer holder cannot
	// forge a cursor MAC, and rotating that bearer alone leaves cursors valid.
	// auditsvc.New never returns or logs the derived key.
	auditService := auditsvc.New(repo, cfg.ApprovalJWTSecret)
	telemetryService := telemetrysvc.New(repo)
	// A missing key is not fatal: everything else still works, and the
	// integrations service says so on its catalogue and refuses to connect an
	// account, rather than storing a credential in the clear. A malformed key
	// never reaches here — config rejects it.
	var integrationSealer *secretbox.Sealer
	if cfg.IntegrationKey != "" {
		integrationSealer, err = secretbox.FromHexKey(cfg.IntegrationKey)
		if err != nil {
			_ = database.Close()
			return nil, fmt.Errorf("initialize integration key: %w", err)
		}
	}
	integrationService := integrationsvc.New(repo, integrationSealer, auditService)
	integrationService.SetApprovalEnforcer(approvalService)
	integrationService.SetRegistryChangeNotifier(runtimeService)
	mcpRegistryService := mcpregistrysvc.New(repo, integrationSealer, nil)
	mcpRegistryService.SetApprovalEnforcer(approvalService)
	mcpRegistryService.SetRegistryChangeNotifier(runtimeService)
	if cfg.MCPConfigRoot != "" {
		mcpJSON, readErr := os.ReadFile(filepath.Join(cfg.MCPConfigRoot, "mcp.json"))
		if readErr == nil {
			if _, err := mcpRegistryService.ImportJSON(context.Background(), mcpJSON); err != nil {
				message := boundedAppDiagnostic(err.Error(), 512)
				if recordErr := repo.ReplaceMCPImportIssues(
					context.Background(),
					map[string]string{"_document": message},
				); recordErr != nil {
					_ = database.Close()
					return nil, fmt.Errorf("record mcp.json import failure: %w", recordErr)
				}

				log.Printf("mcp.json import failed: %v", err)
			}
		} else if errors.Is(readErr, os.ErrNotExist) {
			if err := repo.ReplaceMCPImportIssues(context.Background(), map[string]string{}); err != nil {
				_ = database.Close()
				return nil, fmt.Errorf("clear mcp.json import issues: %w", err)
			}
		} else if !errors.Is(readErr, os.ErrNotExist) {
			_ = database.Close()
			return nil, fmt.Errorf("read mcp.json: %w", readErr)
		}
	}
	healthService := &HealthServer{schemaVersion: schemaVersion}
	persistAuthFailure := func(ctx context.Context, failure auth.Failure) error {
		return auditService.Record(ctx, failure.RequestID, failure.ActorType, failure.Peer, "auth.failed", failure.Method, map[string]any{
			"method":    failure.Method,
			"requestId": failure.RequestID,
			"userAgent": failure.UserAgent,
			"peer":      failure.Peer,
		})
	}
	authFailures := auth.NewAsyncFailureRecorder(persistAuthFailure)
	publicAuth := auth.InterceptorOptions{ActorType: "client", FailureRecorder: authFailures.Record}
	internalAuth := auth.InterceptorOptions{FailureRecorder: authFailures.Record}
	// Two least-privilege internal identities share the internal gRPC port:
	// the runtime claims jobs and reads session history for context and
	// recall; the approval consumer (bundled mcp-files only) may call ConsumeApproval,
	// FinalizeSandboxArtifact, and CheckSessionCapability. Neither token
	// grants the other's methods, so a compromised mcp-files cannot claim a
	// job or read conversation history, and a compromised runtime cannot be
	// swapped in as the approval consumer for a different tool server.
	internalIdentities, err := auth.NewInternalIdentities([]auth.ServiceIdentity{
		auth.NewServiceIdentity("runtime", cfg.RuntimeToken,
			turingv1.RuntimeService_ConnectWorker_FullMethodName,
			turingv1.SessionService_ListMessages_FullMethodName,
			turingv1.SessionService_SearchMessages_FullMethodName,
			turingv1.ApprovalService_GetApprovalForRuntime_FullMethodName,
			turingv1.ApprovalService_ConsumeApproval_FullMethodName,
			turingv1.McpRegistryService_ListMcpServers_FullMethodName,
			turingv1.McpRegistryService_CallRegisteredMcpTool_FullMethodName,
			turingv1.IntegrationService_ListIntegrationTools_FullMethodName,
			turingv1.IntegrationService_CallIntegrationTool_FullMethodName,
		),
		auth.NewServiceIdentity("approval-consumer", cfg.ApprovalConsumerToken,
			turingv1.ApprovalService_ConsumeApproval_FullMethodName,
			turingv1.ApprovalService_FinalizeSandboxArtifact_FullMethodName,
			turingv1.ApprovalService_CheckSessionCapability_FullMethodName,
		),
	})
	if err != nil {
		_ = database.Close()
		return nil, err
	}
	internalIdentityNames := make([]string, len(internalIdentities))
	for i, identity := range internalIdentities {
		internalIdentityNames[i] = identity.Name
	}
	for _, event := range recoveredEvents {
		eventBus.Publish(eventsvc.Event{
			EventID: event.EventID, SessionID: event.SessionID, RunID: event.RunID.String,
			TraceID: event.TraceID, Sequence: event.Sequence, Type: event.Type, CreatedAt: event.CreatedAt, PayloadJSON: event.PayloadJSON,
		})
	}

	publicServer := grpc.NewServer(
		grpc.UnaryInterceptor(auth.UnaryInterceptor(cfg.ClientAPIKey, publicAuth)),
		grpc.StreamInterceptor(auth.StreamInterceptor(cfg.ClientAPIKey, publicAuth)),
		grpc.MaxRecvMsgSize(maxGRPCMessageSize),
		grpc.MaxSendMsgSize(maxGRPCMessageSize),
	)
	internalServer := grpc.NewServer(
		grpc.UnaryInterceptor(auth.UnaryIdentityInterceptor(internalIdentities, internalAuth)),
		grpc.StreamInterceptor(auth.StreamIdentityInterceptor(internalIdentities, internalAuth)),
		grpc.MaxRecvMsgSize(maxGRPCMessageSize),
		grpc.MaxSendMsgSize(maxGRPCMessageSize),
	)

	turingv1.RegisterHealthServiceServer(publicServer, healthService)
	turingv1.RegisterSessionServiceServer(publicServer, sessionService)
	// Public only: the runtime never reads skills over gRPC, it receives the
	// snapshot on the job it claims.
	turingv1.RegisterSkillServiceServer(publicServer, skillService)
	// Public only, for the same reason: the runtime is handed the destination
	// on the job it claims and never asks for it.
	turingv1.RegisterExternalAgentServiceServer(publicServer, agentService)
	// The public facet manages connections; the internal facet can only list
	// and dispatch tools. Neither facet ever returns a sealed credential.
	turingv1.RegisterIntegrationServiceServer(publicServer, integrationsvc.NewPublicServer(integrationService))
	turingv1.RegisterMcpRegistryServiceServer(publicServer, mcpregistrysvc.NewPublicServer(mcpRegistryService))
	// Public only: nothing outside the orchestrator schedules a run, and the
	// runtime has no reason to read the automation library.
	turingv1.RegisterAutomationServiceServer(publicServer, automationService)
	// Public only, and read-only. The runtime has nothing to ask it, and there
	// is no internal registration because nothing off this process should be
	// able to read a usage report either.
	turingv1.RegisterTelemetryServiceServer(publicServer, telemetryService)
	turingv1.RegisterEventServiceServer(publicServer, eventService)
	turingv1.RegisterChatServiceServer(publicServer, chatService)
	turingv1.RegisterApprovalServiceServer(publicServer, approvalsvc.NewPublicServer(approvalService))
	// Public only: this is the user reading what was recorded about their own
	// install. The runtime has nothing to ask it and holding the internal
	// token should never be enough to read every client's audit trail.
	turingv1.RegisterAuditServiceServer(publicServer, auditService)
	turingv1.RegisterSessionServiceServer(internalServer, sessionService)
	turingv1.RegisterApprovalServiceServer(internalServer, approvalsvc.NewInternalServer(approvalService))
	turingv1.RegisterRuntimeServiceServer(internalServer, runtimeService)
	turingv1.RegisterMcpRegistryServiceServer(internalServer, mcpregistrysvc.NewInternalServer(mcpRegistryService))
	turingv1.RegisterIntegrationServiceServer(internalServer, integrationsvc.NewInternalServer(integrationService))

	application := &App{
		PublicServer:          publicServer,
		InternalServer:        internalServer,
		Repository:            repo,
		EventBus:              eventBus,
		RuntimeService:        runtimeService,
		IntegrationService:    integrationService,
		SessionService:        sessionService,
		EventService:          eventService,
		ChatService:           chatService,
		ApprovalService:       approvalService,
		AuditService:          auditService,
		MCPRegistryService:    mcpRegistryService,
		HealthService:         healthService,
		InternalIdentityNames: internalIdentityNames,
		database:              database,
		authFailures:          authFailures,
		reaperDone:            make(chan struct{}),
		schedulerDone:         make(chan struct{}),
		deletionReconcileDone: make(chan struct{}),
	}
	reaperCtx, reaperCancel := context.WithCancel(context.Background())
	application.reaperCancel = reaperCancel
	if cfg.JobReaperIntervalMS > 0 {
		go func() {
			defer close(application.reaperDone)
			runtimeService.RunRecoveryLoop(reaperCtx, time.Duration(cfg.JobReaperIntervalMS)*time.Millisecond)
		}()
	} else {
		reaperCancel()
		close(application.reaperDone)
	}
	scheduler := automationsvc.NewScheduler(repo, eventBus, runtimeService, repository.AutomationRunDefaults{
		AgentID:         "general_assistant",
		ModelProvider:   "ollama",
		Model:           cfg.OllamaModel,
		ValidateRouting: runtimeService.ValidateRouting,
	})
	schedulerCtx, schedulerCancel := context.WithCancel(context.Background())
	application.schedulerCancel = schedulerCancel
	if cfg.AutomationTickMS > 0 {
		go func() {
			defer close(application.schedulerDone)
			scheduler.Run(schedulerCtx, time.Duration(cfg.AutomationTickMS)*time.Millisecond)
		}()
	} else {
		schedulerCancel()
		close(application.schedulerDone)
	}
	deletionReconcileCtx, deletionReconcileCancel := context.WithCancel(context.Background())
	application.deletionReconcileCancel = deletionReconcileCancel
	deletionReconcileInterval := defaultDeletionReconcileInterval
	if cfg.JobReaperIntervalMS > 0 {
		deletionReconcileInterval = time.Duration(cfg.JobReaperIntervalMS) * time.Millisecond
	}
	go func() {
		defer close(application.deletionReconcileDone)
		ticker := time.NewTicker(deletionReconcileInterval)
		defer ticker.Stop()
		for {
			select {
			case <-deletionReconcileCtx.Done():
				return
			case <-ticker.C:
				if err := sessionService.ResumePendingDeletions(deletionReconcileCtx); err != nil {
					// A durable receipt remains retryable; this loop must not
					// die because one external cleanup endpoint is offline.
					fmt.Printf("resume pending session deletions: %v\n", err)
				}
			}
		}
	}()
	return application, nil
}

func (a *App) Stop() {
	if a == nil {
		return
	}
	a.stopOnce.Do(func() {
		if a.reaperCancel != nil {
			a.reaperCancel()
		}
		if a.reaperDone != nil {
			<-a.reaperDone
		}
		if a.schedulerCancel != nil {
			a.schedulerCancel()
		}
		if a.schedulerDone != nil {
			<-a.schedulerDone
		}
		if a.deletionReconcileCancel != nil {
			a.deletionReconcileCancel()
		}
		if a.deletionReconcileDone != nil {
			<-a.deletionReconcileDone
		}
		var wg sync.WaitGroup
		if a.PublicServer != nil {
			wg.Add(1)
			go func() {
				defer wg.Done()
				stopGRPCServer(a.PublicServer)
			}()
		}
		if a.InternalServer != nil {
			wg.Add(1)
			go func() {
				defer wg.Done()
				stopGRPCServer(a.InternalServer)
			}()
		}
		wg.Wait()
		if a.RuntimeService != nil {
			a.RuntimeService.WaitForWorkerStreams()
		}
		if a.authFailures != nil {
			closeCtx, cancel := context.WithTimeout(context.Background(), gracefulStopTimeout)
			_ = a.authFailures.Close(closeCtx)
			cancel()
		}
		if a.database != nil {
			_ = a.database.Close()
		}
	})
}

func stopGRPCServer(server *grpc.Server) {
	done := make(chan struct{})
	go func() {
		server.GracefulStop()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(gracefulStopTimeout):
		server.Stop()
		<-done
	}
}

type HealthServer struct {
	turingv1.UnimplementedHealthServiceServer

	schemaVersion string
}

func (s *HealthServer) Check(context.Context, *turingv1.HealthCheckRequest) (*turingv1.HealthCheckResponse, error) {
	return &turingv1.HealthCheckResponse{Ok: true}, nil
}

func (s *HealthServer) Version(context.Context, *turingv1.VersionRequest) (*turingv1.VersionResponse, error) {
	return &turingv1.VersionResponse{Version: "1.0.0-go", SchemaVersion: s.schemaVersion}, nil
}
