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
	approvalsvc "github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/service/approvals"
	auditsvc "github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/service/audit"
	chatsvc "github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/service/chat"
	eventsvc "github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/service/events"
	runtimesvc "github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/service/runtime"
	sessionsvc "github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/service/sessions"
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
	authFailures *auth.AsyncFailureRecorder
}

func New(cfg config.Config) (*App, error) {
	database, err := db.Open(cfg.DatabasePath)
	if err != nil {
		return nil, err
	}
	if err := db.ApplyMigrations(context.Background(), database); err != nil {
		_ = database.Close()
		return nil, err
	}
	schemaVersion, err := db.LatestSchemaVersion()
	if err != nil {
		_ = database.Close()
		return nil, errors.New("initialize schema version")
	}

	repo := repository.New(database)
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
	runtimeService := runtimesvc.NewWithConfig(repo, eventBus, runtimesvc.DispatchConfig{
		MaxConcurrentRuns: maxConcurrentRuns,
		LeaseDuration:     time.Duration(cfg.JobTimeoutMS) * time.Millisecond,
		MaxAttempts:       cfg.JobMaxAttempts,
	}, approvalService)
	sessionService := sessionsvc.New(repo, cfg)
	eventService := eventsvc.NewServer(repo, eventBus)
	chatService := chatsvc.New(repo, eventBus, runtimeService, cfg.OllamaModel, cfg.OpenAIModel)
	auditService := auditsvc.New(repo)
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
