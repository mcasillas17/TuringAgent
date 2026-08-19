package app

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

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
	runtimesvc "github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/service/runtime"
	sessionsvc "github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/service/sessions"
	skillsvc "github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/service/skills"
	telemetrysvc "github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/service/telemetry"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/skillfiles"
	"google.golang.org/grpc"
)

const maxGRPCMessageSize = 4 * 1024 * 1024
const gracefulStopTimeout = 5 * time.Second

type App struct {
	PublicServer   *grpc.Server
	InternalServer *grpc.Server

	Repository      *repository.Repository
	EventBus        *eventsvc.Bus
	RuntimeService  *runtimesvc.Server
	SessionService  *sessionsvc.Server
	EventService    *eventsvc.Server
	ChatService     *chatsvc.Server
	ApprovalService *approvalsvc.Server
	AuditService    *auditsvc.Server
	HealthService   *HealthServer

	database     *db.DB
	stopOnce     sync.Once
	reaperCancel context.CancelFunc
	reaperDone   chan struct{}
	// The scheduler gets its own cancel/done pair rather than sharing the
	// reaper's: they stop independently, and a shutdown that waited on one
	// while the other was still firing runs would be a shutdown that queues
	// work on its way out.
	schedulerCancel context.CancelFunc
	schedulerDone   chan struct{}
	authFailures    *auth.AsyncFailureRecorder
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
	if cfg.OpenAIAPIKey != "" {
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
			Models:                 legacyModels,
			AgentIds:               []turingv1.AgentId{turingv1.AgentId_AGENT_ID_GENERAL_ASSISTANT},
			Tools:                  runtimesvc.LegacyPolicyToolCapabilities(),
			SupportsExternalAgents: len(cfg.AgentCredentialNames) > 0,
		},
	}, approvalService)
	sessionService := sessionsvc.New(repo, cfg, runtimeService)
	skillService := skillsvc.New(repo)
	agentService := agentsvc.New(repo, cfg.AgentCredentialNames)
	automationService := automationsvc.New(repo)
	eventService := eventsvc.NewServer(repo, eventBus)
	chatService := chatsvc.New(repo, eventBus, runtimeService, cfg.OllamaModel, cfg.OpenAIModel)
	auditService := auditsvc.New(repo)
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
	internalAuth := auth.InterceptorOptions{ActorType: "runtime", FailureRecorder: authFailures.Record}
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
		grpc.UnaryInterceptor(auth.UnaryInterceptor(cfg.InternalToken, internalAuth)),
		grpc.StreamInterceptor(auth.StreamInterceptor(cfg.InternalToken, internalAuth)),
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
	// Public only: nothing internal reads a connection today, and the sealed
	// credential is not served to anyone at all.
	turingv1.RegisterIntegrationServiceServer(publicServer, integrationService)
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
	turingv1.RegisterSessionServiceServer(internalServer, sessionService)
	turingv1.RegisterApprovalServiceServer(internalServer, approvalsvc.NewInternalServer(approvalService))
	turingv1.RegisterRuntimeServiceServer(internalServer, runtimeService)

	application := &App{
		PublicServer:    publicServer,
		InternalServer:  internalServer,
		Repository:      repo,
		EventBus:        eventBus,
		RuntimeService:  runtimeService,
		SessionService:  sessionService,
		EventService:    eventService,
		ChatService:     chatService,
		ApprovalService: approvalService,
		AuditService:    auditService,
		HealthService:   healthService,
		database:        database,
		authFailures:    authFailures,
		reaperDone:      make(chan struct{}),
		schedulerDone:   make(chan struct{}),
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
