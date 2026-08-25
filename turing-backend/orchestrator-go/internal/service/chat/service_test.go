package chat

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/db"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/service/events"
	runtimesvc "github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/service/runtime"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/skillfiles"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/types/known/structpb"
)

type harness struct {
	repo       *repository.Repository
	database   *db.DB
	bus        *events.Bus
	runtime    *runtimesvc.Server
	service    *Server
	chatClient turingv1.ChatServiceClient
	conn       *grpc.ClientConn
	ctx        context.Context
	skillRoot  string
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	database := openChatTestDB(t)
	return newHarnessWithDatabase(t, database)
}

func newHarnessWithDatabase(t *testing.T, database *db.DB) *harness {
	t.Helper()
	repo := repository.New(database)
	skillRoot := t.TempDir()
	repo.SetSkillStore(skillfiles.New(skillRoot))
	bus := events.NewBus(8)
	runtimeServer := runtimesvc.NewWithConfig(repo, bus, runtimesvc.DispatchConfig{
		LegacyCapabilities: &runtimesvc.LegacyCapabilityProfile{
			Models: []*turingv1.ModelCapability{
				{
					Provider:         turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA,
					Model:            "llama3.2",
					MaxContextTokens: 8192,
				},
				{
					Provider:         turingv1.ModelProvider_MODEL_PROVIDER_OPENAI_COMPATIBLE,
					Model:            "gpt-4o-mini",
					MaxContextTokens: 8192,
				},
			},
			AgentIds:                    []turingv1.AgentId{turingv1.AgentId_AGENT_ID_GENERAL_ASSISTANT},
			ExternalAgentCredentialRefs: []string{"claude", "external"},
			SupportsExternalAgents:      true,
		},
	})
	chatServer := NewWithEgressConfig(repo, bus, runtimeServer, "llama3.2", "gpt-4o-mini", EgressConfig{
		OpenAIBaseURL: "https://api.openai.com/v1",
		SigningSecret: "test-egress-signing-secret",
	})
	lis := bufconn.Listen(1024 * 1024)
	grpcServer := grpc.NewServer()
	turingv1.RegisterChatServiceServer(grpcServer, chatServer)
	turingv1.RegisterRuntimeServiceServer(grpcServer, runtimeServer)
	go func() {
		_ = grpcServer.Serve(lis)
	}()
	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) {
			return lis.Dial()
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("dial bufconn: %v", err)
	}
	// NewClient starts the channel IDLE; DialContext connected eagerly in the
	// background. Connect() restores that so handshake latency does not land
	// inside a test's deadline.
	conn.Connect()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(func() {
		cancel()
		grpcServer.Stop()
		runtimeServer.WaitForWorkerStreams()
		_ = conn.Close()
	})
	return &harness{repo: repo, database: database, bus: bus, runtime: runtimeServer, service: chatServer, chatClient: turingv1.NewChatServiceClient(conn), conn: conn, ctx: ctx, skillRoot: skillRoot}
}

func openChatTestDB(t *testing.T) *db.DB {
	t.Helper()
	name := strings.NewReplacer("/", "_", " ", "_", ":", "_").Replace(t.Name())
	sqlDB, err := sql.Open("sqlite3", fmt.Sprintf("file:%s?mode=memory&cache=shared&_foreign_keys=on", name))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	sqlDB.SetMaxOpenConns(1)
	database := &db.DB{DB: sqlDB}
	t.Cleanup(func() { _ = database.Close() })
	if err := db.ApplyMigrations(context.Background(), database); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}
	return database
}

func (h *harness) clientContext() context.Context {
	return h.ctx
}

func (h *harness) createSession(t *testing.T) string {
	t.Helper()
	session, err := h.repo.CreateSession(context.Background(), "Chat")
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	return session.SessionID
}

func TestSendMessageDispatchContextSurvivesClientCancellation(t *testing.T) {
	database := openChatTestDB(t)
	repo := repository.New(database)
	bus := events.NewBus(8)
	dispatcher := &dispatchContextRecorder{}
	service := New(repo, bus, dispatcher, "llama3.2", "gpt-4o-mini")
	session, err := repo.CreateSession(context.Background(), "Cancelled dispatch")
	if err != nil {
		t.Fatal(err)
	}

	streamCtx, cancel := context.WithCancel(context.Background())
	stream := &cancellingChatStream{ctx: streamCtx, cancel: cancel}

	err = service.SendMessage(&turingv1.SendMessageRequest{
		SessionId:     session.SessionID,
		Content:       "keep global dispatch alive",
		ModelProvider: turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA,
		Model:         "llama3.2",
	}, stream)
	if status.Code(err) != codes.Canceled {
		t.Fatalf("SendMessage error = %v, want Canceled", err)
	}
	if dispatcher.contextErr != nil {
		t.Fatalf("DispatchPending received cancelled client context: %v", dispatcher.contextErr)
	}
}

func TestSendMessageDispatchesDurableRunAfterRoutingRefreshFailure(t *testing.T) {
	database := openChatTestDB(t)
	repo := repository.New(database)
	bus := events.NewBus(8)
	dispatcher := &dispatchContextRecorder{refreshErr: errors.New("refresh failed")}
	service := New(repo, bus, dispatcher, "llama3.2", "gpt-4o-mini")
	session, err := repo.CreateSession(context.Background(), "Refresh failure")
	if err != nil {
		t.Fatal(err)
	}
	streamCtx, cancel := context.WithCancel(context.Background())
	stream := &cancellingChatStream{ctx: streamCtx, cancel: cancel}

	err = service.SendMessage(&turingv1.SendMessageRequest{
		SessionId: session.SessionID, Content: "keep queued work",
		ModelProvider: turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA, Model: "llama3.2",
	}, stream)
	if status.Code(err) != codes.Canceled {
		t.Fatalf("SendMessage error = %v, want client cancellation after dispatch", err)
	}
	if dispatcher.dispatchCalls != 1 {
		t.Fatalf("DispatchPending calls = %d, want 1 after refresh failure", dispatcher.dispatchCalls)
	}
	if !slices.Equal(dispatcher.callOrder, []string{"dispatch", "refresh"}) {
		t.Fatalf("routing calls = %v, want dispatch before advisory refresh", dispatcher.callOrder)
	}
}

func TestSendMessageUsesLiveRoutableDefaultWhenModelIsOmitted(t *testing.T) {
	h := newHarness(t)
	capabilities := defaultChatWorkerCapabilities(false)
	capabilities.Models = []*turingv1.ModelCapability{{
		Provider:         turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA,
		Model:            "live-ollama",
		MaxContextTokens: 8192,
	}}
	worker := connectChatTestWorker(t, h, capabilities)

	stream, err := h.chatClient.SendMessage(h.clientContext(), &turingv1.SendMessageRequest{
		SessionId:     h.createSession(t),
		Content:       "use the live fallback",
		ModelProvider: turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := stream.Recv(); err != nil {
		t.Fatalf("receive queued event: %v", err)
	}
	assigned := recvRuntimeCommand(t, worker, func(command *turingv1.RuntimeCommand) bool {
		return command.GetRunAssigned() != nil
	}).GetRunAssigned()
	if assigned.GetModel() != "live-ollama" {
		t.Fatalf("assigned model = %q, want live-ollama", assigned.GetModel())
	}
}

type dispatchContextRecorder struct {
	contextErr           error
	err                  error
	refreshErr           error
	routableDefaultModel string
	dispatchCalls        int
	callOrder            []string
}

type blockingDispatcher struct {
	started chan struct{}
	release <-chan struct{}
}

func (d *blockingDispatcher) DispatchPending(context.Context) error {
	close(d.started)
	<-d.release
	return nil
}

func (*blockingDispatcher) CancelRun(context.Context, string, string) {}

func (*blockingDispatcher) ValidateRouting(context.Context, repository.RoutingRequirements) error {
	return nil
}

func (*blockingDispatcher) RefreshPendingRoutingState(context.Context, string) error {
	return nil
}

func (*blockingDispatcher) RoutableDefaultModel(_ string, configured string) string {
	return configured
}

func (d *dispatchContextRecorder) DispatchPending(ctx context.Context) error {
	d.contextErr = ctx.Err()
	d.dispatchCalls++
	d.callOrder = append(d.callOrder, "dispatch")
	return d.err
}

func (d *dispatchContextRecorder) CancelRun(context.Context, string, string) {}

func (d *dispatchContextRecorder) ValidateRouting(context.Context, repository.RoutingRequirements) error {
	return nil
}

func (d *dispatchContextRecorder) RefreshPendingRoutingState(context.Context, string) error {
	d.callOrder = append(d.callOrder, "refresh")
	return d.refreshErr
}

func (d *dispatchContextRecorder) RoutableDefaultModel(_ string, configured string) string {
	if d.routableDefaultModel != "" {
		return d.routableDefaultModel
	}
	return configured
}

type cancellingChatStream struct {
	grpc.ServerStream
	ctx    context.Context
	cancel context.CancelFunc
}

func (s *cancellingChatStream) Context() context.Context {
	return s.ctx
}

func (s *cancellingChatStream) Send(event *turingv1.ChatStreamEvent) error {
	if event.GetRunQueued() != nil {
		s.cancel()
	}
	return nil
}

type failingQueuedChatStream struct {
	grpc.ServerStream
	ctx    context.Context
	cancel context.CancelFunc
}

type queuedChatStream struct {
	grpc.ServerStream
	ctx context.Context
}

type notifyingQueuedChatStream struct {
	grpc.ServerStream
	ctx    context.Context
	queued chan struct{}
}

func (s *queuedChatStream) Context() context.Context {
	return s.ctx
}

func (s *queuedChatStream) Send(*turingv1.ChatStreamEvent) error {
	return nil
}

func (s *notifyingQueuedChatStream) Context() context.Context {
	return s.ctx
}

func (s *notifyingQueuedChatStream) Send(event *turingv1.ChatStreamEvent) error {
	if event.GetRunQueued() != nil {
		s.queued <- struct{}{}
	}
	return nil
}

func (s *failingQueuedChatStream) Context() context.Context {
	return s.ctx
}

func (s *failingQueuedChatStream) Send(event *turingv1.ChatStreamEvent) error {
	if event.GetRunQueued() != nil {
		s.cancel()
		return status.Error(codes.Unavailable, "queued acknowledgement lost")
	}
	return nil
}

func TestSendMessageWithIdempotencyKeyDispatchesAfterQueuedSendFails(t *testing.T) {
	database := openChatTestDB(t)
	repo := repository.New(database)
	bus := events.NewBus(8)
	dispatcher := &dispatchContextRecorder{}
	service := New(repo, bus, dispatcher, "llama3.2", "gpt-4o-mini")
	session, err := repo.CreateSession(context.Background(), "Lost acknowledgement")
	if err != nil {
		t.Fatal(err)
	}
	streamCtx, cancel := context.WithCancel(context.Background())
	stream := &failingQueuedChatStream{ctx: streamCtx, cancel: cancel}

	err = service.SendMessage(&turingv1.SendMessageRequest{
		SessionId:      session.SessionID,
		Content:        "continue after acknowledgement loss",
		ModelProvider:  turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA,
		Model:          "llama3.2",
		IdempotencyKey: "lost-queued-acknowledgement",
	}, stream)
	if status.Code(err) != codes.Unavailable {
		t.Fatalf("SendMessage error = %v, want Unavailable", err)
	}
	if dispatcher.dispatchCalls != 1 || dispatcher.contextErr != nil {
		t.Fatalf("DispatchPending calls=%d context error=%v, want one uncancelled dispatch", dispatcher.dispatchCalls, dispatcher.contextErr)
	}
	var runID, runStatus string
	if err := database.QueryRowContext(context.Background(), `SELECT id, status FROM agent_runs WHERE session_id = ?`, session.SessionID).Scan(&runID, &runStatus); err != nil {
		t.Fatal(err)
	}
	if runStatus != "queued" {
		t.Fatalf("run %q status = %q, want queued after acknowledgement loss", runID, runStatus)
	}
}

func TestSendMessageWithIdempotencyKeyKeepsQueuedRunOnDispatchFailure(t *testing.T) {
	database := openChatTestDB(t)
	repo := repository.New(database)
	bus := events.NewBus(8)
	dispatcher := &dispatchContextRecorder{err: errors.New("dispatch unavailable")}
	service := New(repo, bus, dispatcher, "llama3.2", "gpt-4o-mini")
	session, err := repo.CreateSession(context.Background(), "Dispatch failure")
	if err != nil {
		t.Fatal(err)
	}
	stream := &queuedChatStream{ctx: context.Background()}

	err = service.SendMessage(&turingv1.SendMessageRequest{
		SessionId:      session.SessionID,
		Content:        "keep queued when dispatch fails",
		ModelProvider:  turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA,
		Model:          "llama3.2",
		IdempotencyKey: "dispatch-failure",
	}, stream)
	if status.Code(err) != codes.Internal {
		t.Fatalf("SendMessage error = %v, want Internal", err)
	}
	if dispatcher.dispatchCalls != 1 {
		t.Fatalf("DispatchPending calls = %d, want 1", dispatcher.dispatchCalls)
	}
	var runID, runStatus string
	if err := database.QueryRowContext(context.Background(), `SELECT id, status FROM agent_runs WHERE session_id = ?`, session.SessionID).Scan(&runID, &runStatus); err != nil {
		t.Fatal(err)
	}
	if runStatus != "queued" {
		t.Fatalf("run %q status = %q, want queued after dispatch failure", runID, runStatus)
	}
}

func TestSendMessageReplayDispatchesPendingRunAfterPriorFailure(t *testing.T) {
	database := openChatTestDB(t)
	repo := repository.New(database)
	bus := events.NewBus(8)
	dispatcher := &dispatchContextRecorder{err: errors.New("dispatch unavailable")}
	service := New(repo, bus, dispatcher, "llama3.2", "gpt-4o-mini")
	session, err := repo.CreateSession(context.Background(), "Replay dispatch")
	if err != nil {
		t.Fatal(err)
	}
	request := &turingv1.SendMessageRequest{
		SessionId:      session.SessionID,
		Content:        "replay dispatches pending run",
		ModelProvider:  turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA,
		Model:          "llama3.2",
		IdempotencyKey: "replay-dispatch",
	}
	if err := service.SendMessage(request, &queuedChatStream{ctx: context.Background()}); status.Code(err) != codes.Internal {
		t.Fatalf("initial SendMessage error = %v, want Internal", err)
	}
	dispatcher.err = nil
	replayCtx, cancel := context.WithCancel(context.Background())
	replay := &cancellingChatStream{ctx: replayCtx, cancel: cancel}
	if err := service.SendMessage(request, replay); status.Code(err) != codes.Canceled {
		t.Fatalf("replay SendMessage error = %v, want Canceled", err)
	}
	if dispatcher.dispatchCalls != 2 {
		t.Fatalf("DispatchPending calls = %d, want retry on replay", dispatcher.dispatchCalls)
	}
}

func TestSendMessageReplayDispatchesPendingRunWhenQueuedAckIsLost(t *testing.T) {
	database := openChatTestDB(t)
	repo := repository.New(database)
	bus := events.NewBus(8)
	dispatcher := &dispatchContextRecorder{err: errors.New("dispatch unavailable")}
	service := New(repo, bus, dispatcher, "llama3.2", "gpt-4o-mini")
	session, err := repo.CreateSession(context.Background(), "Replay acknowledgement loss")
	if err != nil {
		t.Fatal(err)
	}
	request := &turingv1.SendMessageRequest{
		SessionId:      session.SessionID,
		Content:        "replay dispatches after acknowledgement loss",
		ModelProvider:  turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA,
		Model:          "llama3.2",
		IdempotencyKey: "replay-queued-acknowledgement-loss",
	}
	if err := service.SendMessage(request, &queuedChatStream{ctx: context.Background()}); status.Code(err) != codes.Internal {
		t.Fatalf("initial SendMessage error = %v, want Internal", err)
	}
	dispatcher.err = nil
	replayCtx, cancel := context.WithCancel(context.Background())
	replay := &failingQueuedChatStream{ctx: replayCtx, cancel: cancel}
	if err := service.SendMessage(request, replay); status.Code(err) != codes.Unavailable {
		t.Fatalf("replay SendMessage error = %v, want Unavailable", err)
	}
	if dispatcher.dispatchCalls != 2 {
		t.Fatalf("DispatchPending calls = %d, want retry after lost acknowledgement", dispatcher.dispatchCalls)
	}
}

func TestSendMessageCancellingReplayDoesNotCancelOriginalRun(t *testing.T) {
	database := openChatTestDB(t)
	repo := repository.New(database)
	bus := events.NewBus(8)
	service := New(repo, bus, nil, "llama3.2", "gpt-4o-mini")
	session, err := repo.CreateSession(context.Background(), "Replay cancellation")
	if err != nil {
		t.Fatal(err)
	}
	original, err := repo.EnqueueUserMessage(context.Background(), repository.EnqueueUserMessageInput{
		SessionID:      session.SessionID,
		Content:        "do not cancel the original",
		ContentType:    "text",
		AgentID:        "general_assistant",
		ModelProvider:  "ollama",
		Model:          "llama3.2",
		IdempotencyKey: "replay-cancellation",
	})
	if err != nil {
		t.Fatal(err)
	}
	streamCtx, cancel := context.WithCancel(context.Background())
	stream := &cancellingChatStream{ctx: streamCtx, cancel: cancel}

	err = service.SendMessage(&turingv1.SendMessageRequest{
		SessionId:      session.SessionID,
		Content:        "do not cancel the original",
		ModelProvider:  turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA,
		Model:          "llama3.2",
		IdempotencyKey: "replay-cancellation",
	}, stream)
	if status.Code(err) != codes.Canceled {
		t.Fatalf("SendMessage error = %v, want Canceled", err)
	}
	run, err := repo.GetRun(context.Background(), original.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if run.Status != "queued" {
		t.Fatalf("original run status = %q, want queued after replay cancellation", run.Status)
	}
}

func TestSendMessageStreamsQueuedEvent(t *testing.T) {
	h := newHarness(t)
	worker := connectChatTestWorker(t, h, defaultChatWorkerCapabilities(false))
	defer func() { _ = worker.CloseSend() }()
	sessionID := h.createSession(t)
	stream, err := h.chatClient.SendMessage(h.clientContext(), &turingv1.SendMessageRequest{
		SessionId:     sessionID,
		Content:       "hello",
		ModelProvider: turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA,
		Model:         "llama3.2",
	})
	if err != nil {
		t.Fatal(err)
	}
	event, err := stream.Recv()
	if err != nil {
		t.Fatal(err)
	}
	if event.GetRunQueued() == nil {
		t.Fatalf("first event = %T, want run_queued", event.Event)
	}
}

func TestSendMessageIdempotentReplayReturnsOriginalRunAndResumesEventStream(t *testing.T) {
	h := newHarness(t)
	worker := connectChatTestWorker(t, h, defaultChatWorkerCapabilities(false))
	defer func() { _ = worker.CloseSend() }()
	sessionID := h.createSession(t)
	key := "send-replay-key"
	first, err := h.chatClient.SendMessage(h.clientContext(), &turingv1.SendMessageRequest{
		SessionId:      sessionID,
		Content:        "replay this operation",
		IdempotencyKey: key,
	})
	if err != nil {
		t.Fatal(err)
	}
	firstQueued, err := first.Recv()
	if err != nil {
		t.Fatal(err)
	}

	replay, err := h.chatClient.SendMessage(h.clientContext(), &turingv1.SendMessageRequest{
		SessionId:      sessionID,
		Content:        "replay this operation",
		ContentType:    "text",
		AgentId:        turingv1.AgentId_AGENT_ID_GENERAL_ASSISTANT,
		ModelProvider:  turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA,
		Model:          "llama3.2",
		IdempotencyKey: key,
	})
	if err != nil {
		t.Fatal(err)
	}
	replayedQueued, err := replay.Recv()
	if err != nil {
		t.Fatal(err)
	}
	if replayedQueued.GetRunQueued().GetRunId() != firstQueued.GetRunQueued().GetRunId() ||
		replayedQueued.GetRunQueued().GetJobId() != firstQueued.GetRunQueued().GetJobId() ||
		replayedQueued.GetRunQueued().GetTraceId() != firstQueued.GetRunQueued().GetTraceId() {
		t.Fatalf("replayed queued event = %+v, want original %+v", replayedQueued.GetRunQueued(), firstQueued.GetRunQueued())
	}
	assertSingleEnqueuedOperation(t, h.database, sessionID)

	completed, err := h.repo.AppendEvent(context.Background(), repository.AppendEventInput{
		SessionID:   sessionID,
		RunID:       firstQueued.GetRunQueued().GetRunId(),
		TraceID:     firstQueued.GetRunQueued().GetTraceId(),
		Type:        "agent.run.completed",
		PayloadJSON: `{"assistantMessageId":"msg_done"}`,
	})
	if err != nil {
		t.Fatal(err)
	}
	var replayedTerminal *turingv1.ChatStreamEvent
	for replayedTerminal == nil {
		event, err := replay.Recv()
		if err != nil {
			t.Fatal(err)
		}
		if event.GetRunCompleted() != nil {
			replayedTerminal = event
		}
	}
	if replayedTerminal.GetRunCompleted().GetRunId() != firstQueued.GetRunQueued().GetRunId() ||
		replayedTerminal.Sequence != completed.Sequence {
		t.Fatalf("replayed terminal event = %+v, want run %q at sequence %d", replayedTerminal, firstQueued.GetRunQueued().GetRunId(), completed.Sequence)
	}
}

func TestSendMessageConcurrentIdempotentRequestsQueueOneOperation(t *testing.T) {
	h := newHarness(t)
	worker := connectChatTestWorker(t, h, defaultChatWorkerCapabilities(false))
	defer func() { _ = worker.CloseSend() }()
	sessionID := h.createSession(t)
	start := make(chan struct{})
	queued := make(chan *turingv1.RunQueued, 2)
	errs := make(chan error, 2)
	var workers sync.WaitGroup
	for range 2 {
		workers.Add(1)
		go func() {
			defer workers.Done()
			<-start
			stream, err := h.chatClient.SendMessage(h.clientContext(), &turingv1.SendMessageRequest{
				SessionId:      sessionID,
				Content:        "enqueue only once",
				ModelProvider:  turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA,
				Model:          "llama3.2",
				IdempotencyKey: "concurrent-send-key",
			})
			if err != nil {
				errs <- err
				return
			}
			event, err := stream.Recv()
			if err != nil {
				errs <- err
				return
			}
			queued <- event.GetRunQueued()
		}()
	}
	close(start)
	workers.Wait()
	close(errs)
	close(queued)
	for err := range errs {
		t.Fatal(err)
	}
	var results []*turingv1.RunQueued
	for result := range queued {
		results = append(results, result)
	}
	if len(results) != 2 {
		t.Fatalf("queued responses = %d, want 2", len(results))
	}
	if results[0].GetRunId() != results[1].GetRunId() || results[0].GetJobId() != results[1].GetJobId() || results[0].GetTraceId() != results[1].GetTraceId() {
		t.Fatalf("concurrent queued responses = %+v and %+v, want identical IDs", results[0], results[1])
	}
	assertSingleEnqueuedOperation(t, h.database, sessionID)
}

func TestSendMessageRejectsConflictingIdempotencyKeyReuse(t *testing.T) {
	h := newHarness(t)
	worker := connectChatTestWorker(t, h, defaultChatWorkerCapabilities(false))
	defer func() { _ = worker.CloseSend() }()
	sessionID := h.createSession(t)
	key := "conflicting-send-key"
	first, err := h.chatClient.SendMessage(h.clientContext(), &turingv1.SendMessageRequest{
		SessionId:      sessionID,
		Content:        "original content",
		ModelProvider:  turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA,
		Model:          "llama3.2",
		IdempotencyKey: key,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := first.Recv(); err != nil {
		t.Fatal(err)
	}

	conflict, err := h.chatClient.SendMessage(h.clientContext(), &turingv1.SendMessageRequest{
		SessionId:      sessionID,
		Content:        "different content",
		ModelProvider:  turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA,
		Model:          "llama3.2",
		IdempotencyKey: key,
	})
	if err == nil {
		_, err = conflict.Recv()
	}
	if status.Code(err) != codes.AlreadyExists {
		t.Fatalf("conflicting idempotency key error = %v, want AlreadyExists", err)
	}
	assertSingleEnqueuedOperation(t, h.database, sessionID)
}

func TestSendMessageRejectsIdempotencyKeyReuseWithDifferentRoutingRequirements(t *testing.T) {
	h := newHarness(t)
	worker := connectChatTestWorker(t, h, defaultChatWorkerCapabilities(false))
	defer func() { _ = worker.CloseSend() }()
	sessionID := h.createSession(t)
	const key = "conflicting-routing-requirements"
	first, err := h.chatClient.SendMessage(h.clientContext(), &turingv1.SendMessageRequest{
		SessionId:             sessionID,
		Content:               "route this once",
		ModelProvider:         turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA,
		Model:                 "llama3.2",
		IdempotencyKey:        key,
		RequiredContextTokens: 1024,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := first.Recv(); err != nil {
		t.Fatal(err)
	}

	conflict, err := h.chatClient.SendMessage(h.clientContext(), &turingv1.SendMessageRequest{
		SessionId:             sessionID,
		Content:               "route this once",
		ModelProvider:         turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA,
		Model:                 "llama3.2",
		IdempotencyKey:        key,
		RequiredContextTokens: 2048,
	})
	if err == nil {
		_, err = conflict.Recv()
	}
	if status.Code(err) != codes.AlreadyExists {
		t.Fatalf("conflicting idempotency key error = %v, want AlreadyExists", err)
	}
	assertSingleEnqueuedOperation(t, h.database, sessionID)
}

func TestSendMessageReplayIgnoresLiveDefaultModelChanges(t *testing.T) {
	database := openChatTestDB(t)
	repo := repository.New(database)
	bus := events.NewBus(8)
	dispatcher := &dispatchContextRecorder{routableDefaultModel: "live-model-a"}
	service := New(repo, bus, dispatcher, "configured-model", "gpt-4o-mini")
	session, err := repo.CreateSession(context.Background(), "Live default changed")
	if err != nil {
		t.Fatal(err)
	}
	request := &turingv1.SendMessageRequest{
		SessionId:      session.SessionID,
		Content:        "retry the same omitted model",
		IdempotencyKey: "stable-default-model",
	}
	firstContext, cancelFirst := context.WithCancel(context.Background())
	if err := service.SendMessage(request, &cancellingChatStream{ctx: firstContext, cancel: cancelFirst}); status.Code(err) != codes.Canceled {
		t.Fatalf("first SendMessage error = %v, want Canceled", err)
	}

	dispatcher.routableDefaultModel = "live-model-b"
	replayContext, cancelReplay := context.WithCancel(context.Background())
	if err := service.SendMessage(request, &cancellingChatStream{ctx: replayContext, cancel: cancelReplay}); status.Code(err) != codes.Canceled {
		t.Fatalf("replayed SendMessage error = %v, want Canceled", err)
	}
	assertSingleEnqueuedOperation(t, database, session.SessionID)
	var persistedModel string
	if err := database.QueryRowContext(context.Background(), `SELECT model_name FROM agent_runs WHERE session_id = ?`, session.SessionID).Scan(&persistedModel); err != nil {
		t.Fatal(err)
	}
	if persistedModel != "live-model-a" {
		t.Fatalf("persisted model = %q, want original live-model-a", persistedModel)
	}
}

func TestSendMessageIdempotencySurvivesDatabaseRestart(t *testing.T) {
	databasePath := filepath.Join(t.TempDir(), "turing.db")
	const key = "restart-send-key"
	var sessionID string
	var original *turingv1.RunQueued
	t.Run("first process", func(t *testing.T) {
		database, err := db.Open(databasePath)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = database.Close() })
		if err := db.ApplyMigrations(context.Background(), database); err != nil {
			t.Fatal(err)
		}
		h := newHarnessWithDatabase(t, database)
		worker := connectChatTestWorker(t, h, defaultChatWorkerCapabilities(false))
		defer func() { _ = worker.CloseSend() }()
		sessionID = h.createSession(t)
		stream, err := h.chatClient.SendMessage(h.clientContext(), &turingv1.SendMessageRequest{
			SessionId:      sessionID,
			Content:        "survive restart",
			ModelProvider:  turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA,
			Model:          "llama3.2",
			IdempotencyKey: key,
		})
		if err != nil {
			t.Fatal(err)
		}
		event, err := stream.Recv()
		if err != nil {
			t.Fatal(err)
		}
		original = event.GetRunQueued()
	})
	t.Run("replay process", func(t *testing.T) {
		database, err := db.Open(databasePath)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = database.Close() })
		if err := db.ApplyMigrations(context.Background(), database); err != nil {
			t.Fatal(err)
		}
		h := newHarnessWithDatabase(t, database)
		worker := connectChatTestWorker(t, h, defaultChatWorkerCapabilities(false))
		defer func() { _ = worker.CloseSend() }()
		stream, err := h.chatClient.SendMessage(h.clientContext(), &turingv1.SendMessageRequest{
			SessionId:      sessionID,
			Content:        "survive restart",
			ModelProvider:  turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA,
			Model:          "llama3.2",
			IdempotencyKey: key,
		})
		if err != nil {
			t.Fatal(err)
		}
		event, err := stream.Recv()
		if err != nil {
			t.Fatal(err)
		}
		replayed := event.GetRunQueued()
		if replayed.GetRunId() != original.GetRunId() || replayed.GetJobId() != original.GetJobId() || replayed.GetTraceId() != original.GetTraceId() {
			t.Fatalf("replayed queued event = %+v, want %+v", replayed, original)
		}
		assertSingleEnqueuedOperation(t, database, sessionID)
	})
}

func assertSingleEnqueuedOperation(t *testing.T, database *db.DB, sessionID string) {
	t.Helper()
	var userMessages, runs, jobs int
	if err := database.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM messages WHERE session_id = ? AND role = 'user'`, sessionID).Scan(&userMessages); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM agent_runs WHERE session_id = ?`, sessionID).Scan(&runs); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM jobs WHERE run_id IN (SELECT id FROM agent_runs WHERE session_id = ?)`, sessionID).Scan(&jobs); err != nil {
		t.Fatal(err)
	}
	if userMessages != 1 || runs != 1 || jobs != 1 {
		t.Fatalf("enqueued rows: user messages=%d, runs=%d, jobs=%d; want exactly one each", userMessages, runs, jobs)
	}
}

func TestSendMessageRejectsUnavailableRoutingBeforePersistence(t *testing.T) {
	tests := []struct {
		name    string
		request func(sessionID string) *turingv1.SendMessageRequest
		kind    turingv1.RoutingRequirementKind
		code    codes.Code
	}{
		{
			name: "provider",
			request: func(sessionID string) *turingv1.SendMessageRequest {
				return &turingv1.SendMessageRequest{
					SessionId: sessionID, Content: "hello",
					ModelProvider: turingv1.ModelProvider_MODEL_PROVIDER_OPENAI_COMPATIBLE, Model: "gpt-4o-mini",
				}
			},
			kind: turingv1.RoutingRequirementKind_ROUTING_REQUIREMENT_KIND_PROVIDER,
		},
		{
			name: "model",
			request: func(sessionID string) *turingv1.SendMessageRequest {
				return &turingv1.SendMessageRequest{
					SessionId: sessionID, Content: "hello",
					ModelProvider: turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA, Model: "other-model",
				}
			},
			kind: turingv1.RoutingRequirementKind_ROUTING_REQUIREMENT_KIND_MODEL,
		},
		{
			name: "tool",
			request: func(sessionID string) *turingv1.SendMessageRequest {
				return &turingv1.SendMessageRequest{
					SessionId: sessionID, Content: "hello",
					ModelProvider: turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA, Model: "llama3.2",
					RequestedTools: []string{"files/files.read"},
				}
			},
			kind: turingv1.RoutingRequirementKind_ROUTING_REQUIREMENT_KIND_TOOL,
		},
		{
			name: "agent",
			request: func(sessionID string) *turingv1.SendMessageRequest {
				return &turingv1.SendMessageRequest{
					SessionId: sessionID, Content: "hello",
					AgentId: turingv1.AgentId(99), ModelProvider: turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA, Model: "llama3.2",
				}
			},
			kind: turingv1.RoutingRequirementKind_ROUTING_REQUIREMENT_KIND_AGENT,
			code: codes.InvalidArgument,
		},
		{
			name: "context",
			request: func(sessionID string) *turingv1.SendMessageRequest {
				return &turingv1.SendMessageRequest{
					SessionId: sessionID, Content: "hello",
					ModelProvider: turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA, Model: "llama3.2",
					RequiredContextTokens: 8193,
				}
			},
			kind: turingv1.RoutingRequirementKind_ROUTING_REQUIREMENT_KIND_CONTEXT,
		},
		{
			name: "capacity",
			request: func(sessionID string) *turingv1.SendMessageRequest {
				return &turingv1.SendMessageRequest{
					SessionId: sessionID, Content: "hello",
					ModelProvider: turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA, Model: "llama3.2",
					MinimumWorkerMaxConcurrentRuns: 3,
				}
			},
			kind: turingv1.RoutingRequirementKind_ROUTING_REQUIREMENT_KIND_CAPACITY,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			h := newHarness(t)
			worker := connectChatTestWorker(t, h, &turingv1.WorkerCapabilities{
				Models: []*turingv1.ModelCapability{{
					Provider:         turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA,
					Model:            "llama3.2",
					MaxContextTokens: 8192,
				}},
				AgentIds:          []turingv1.AgentId{turingv1.AgentId_AGENT_ID_GENERAL_ASSISTANT},
				Tools:             []*turingv1.DiscoveredTool{{ServerName: "system", ToolName: "system.time", Schema: &structpb.Struct{}}},
				MaxConcurrentRuns: 2,
			})
			defer func() { _ = worker.CloseSend() }()
			sessionID := h.createSession(t)

			err := sendMessageError(h, test.request(sessionID))
			wantCode := test.code
			if wantCode == codes.OK {
				wantCode = codes.FailedPrecondition
			}
			if status.Code(err) != wantCode {
				t.Fatalf("SendMessage error = %v, want %v", err, wantCode)
			}
			if wantCode == codes.FailedPrecondition {
				detail := chatRoutingUnavailableDetail(t, err)
				if detail.GetKind() != test.kind || detail.GetRequested() == "" {
					t.Fatalf("routing detail = %+v, want kind %v with requested value", detail, test.kind)
				}
			}
			for _, table := range []string{"messages", "agent_runs", "jobs"} {
				var count int
				if err := h.database.QueryRowContext(context.Background(), "SELECT COUNT(*) FROM "+table).Scan(&count); err != nil {
					t.Fatalf("count %s: %v", table, err)
				}
				if count != 0 {
					t.Fatalf("%s rows = %d, want no persistence before routing validation", table, count)
				}
			}
		})
	}
}

func connectChatTestWorker(t *testing.T, h *harness, capabilities *turingv1.WorkerCapabilities) turingv1.RuntimeService_ConnectWorkerClient {
	t.Helper()
	stream, err := turingv1.NewRuntimeServiceClient(h.conn).ConnectWorker(h.clientContext())
	if err != nil {
		t.Fatal(err)
	}
	if err := stream.Send(&turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_WorkerReady{
		WorkerReady: &turingv1.RuntimeWorkerReady{
			WorkerId: "worker-chat-capabilities", RegistrationId: "registration-chat-capabilities", Capabilities: capabilities,
		},
	}}); err != nil {
		t.Fatal(err)
	}
	accepted := recvRuntimeCommand(t, stream, func(command *turingv1.RuntimeCommand) bool {
		return command.GetWorkerAccepted() != nil
	}).GetWorkerAccepted()
	if accepted.GetRegistrationId() != "registration-chat-capabilities" {
		t.Fatalf("accepted registration = %q", accepted.GetRegistrationId())
	}
	return stream
}

func defaultChatWorkerCapabilities(supportsExternalAgents bool) *turingv1.WorkerCapabilities {
	capabilities := &turingv1.WorkerCapabilities{
		Models: []*turingv1.ModelCapability{
			{
				Provider:         turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA,
				Model:            "llama3.2",
				MaxContextTokens: 8192,
			},
			{
				Provider:         turingv1.ModelProvider_MODEL_PROVIDER_OPENAI_COMPATIBLE,
				Model:            "gpt-4o-mini",
				MaxContextTokens: 8192,
			},
		},
		AgentIds:                    []turingv1.AgentId{turingv1.AgentId_AGENT_ID_GENERAL_ASSISTANT},
		Tools:                       []*turingv1.DiscoveredTool{{ServerName: "system", ToolName: "system.time", Schema: &structpb.Struct{}}},
		MaxConcurrentRuns:           2,
		SupportsExternalAgents:      supportsExternalAgents,
		RemoteEgressDecisionVersion: int32(repository.RunEgressDecisionVersion),
	}
	if supportsExternalAgents {
		capabilities.ExternalAgentCredentialRefs = []string{"claude", "external"}
	}
	return capabilities
}

func sendMessageError(h *harness, request *turingv1.SendMessageRequest) error {
	stream, err := h.chatClient.SendMessage(h.clientContext(), request)
	if err != nil {
		return err
	}
	_, err = stream.Recv()
	return err
}

func chatRoutingUnavailableDetail(t *testing.T, err error) *turingv1.RoutingUnavailableDetail {
	t.Helper()
	for _, detail := range status.Convert(err).Details() {
		if unavailable, ok := detail.(*turingv1.RoutingUnavailableDetail); ok {
			return unavailable
		}
	}
	t.Fatalf("error %v has no RoutingUnavailableDetail", err)
	return nil
}

func TestSendMessagePublishesSessionUpdatedBeforeQueued(t *testing.T) {
	h := newHarness(t)
	worker := connectChatTestWorker(t, h, defaultChatWorkerCapabilities(false))
	defer func() { _ = worker.CloseSend() }()
	session, err := h.repo.CreateSession(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	published, unsubscribe := h.bus.Subscribe(session.SessionID)
	defer unsubscribe()

	stream, err := h.chatClient.SendMessage(h.clientContext(), &turingv1.SendMessageRequest{
		SessionId:     session.SessionID,
		Content:       "What is in the sandbox?",
		ModelProvider: turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA,
		Model:         "llama3.2",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := stream.Recv(); err != nil {
		t.Fatal(err)
	}

	first := <-published
	if first.Type != "session.updated" {
		t.Fatalf("first published event = %q, want session.updated", first.Type)
	}
	if first.RunID != "" {
		t.Fatalf("session update run_id = %q, want empty", first.RunID)
	}
	var payload map[string]string
	if err := json.Unmarshal([]byte(first.PayloadJSON), &payload); err != nil {
		t.Fatalf("decode session update: %v", err)
	}
	if payload["title"] != "What is in the sandbox?" {
		t.Fatalf("session update payload = %+v", payload)
	}

	second := <-published
	if second.Type != "agent.run.queued" {
		t.Fatalf("second published event = %q, want agent.run.queued", second.Type)
	}
}

func TestSendMessageRejectsUnknownModelProvider(t *testing.T) {
	h := newHarness(t)
	sessionID := h.createSession(t)
	stream, err := h.chatClient.SendMessage(h.clientContext(), &turingv1.SendMessageRequest{
		SessionId:     sessionID,
		Content:       "hello",
		ModelProvider: turingv1.ModelProvider(999),
	})
	if err == nil {
		_, err = stream.Recv()
	}
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("SendMessage error = %v, want InvalidArgument", err)
	}
}

func TestSendMessageRejectsUnsupportedAgent(t *testing.T) {
	h := newHarness(t)
	worker := connectChatTestWorker(t, h, defaultChatWorkerCapabilities(false))
	defer func() { _ = worker.CloseSend() }()
	sessionID := h.createSession(t)
	stream, err := h.chatClient.SendMessage(h.clientContext(), &turingv1.SendMessageRequest{
		SessionId:     sessionID,
		Content:       "hello",
		AgentId:       turingv1.AgentId(999),
		ModelProvider: turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA,
	})
	if err == nil {
		_, err = stream.Recv()
	}
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("SendMessage error = %v, want InvalidArgument", err)
	}
}

func TestSendMessageRejectsUnsupportedContentType(t *testing.T) {
	h := newHarness(t)
	sessionID := h.createSession(t)
	stream, err := h.chatClient.SendMessage(h.clientContext(), &turingv1.SendMessageRequest{
		SessionId:     sessionID,
		Content:       "hello",
		ContentType:   "application/json",
		ModelProvider: turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA,
	})
	if err == nil {
		_, err = stream.Recv()
	}
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("SendMessage error = %v, want InvalidArgument", err)
	}
}

func TestSendMessageAssignsJobToAlreadyConnectedWorker(t *testing.T) {
	h := newHarness(t)
	sessionID := h.createSession(t)
	ctx, cancel := context.WithTimeout(h.clientContext(), 2*time.Second)
	defer cancel()
	runtimeClient := turingv1.NewRuntimeServiceClient(h.conn)
	workerStream, err := runtimeClient.ConnectWorker(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = workerStream.CloseSend() }()
	if err := workerStream.Send(&turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_WorkerReady{WorkerReady: &turingv1.RuntimeWorkerReady{WorkerId: "worker-before-chat", AgentId: turingv1.AgentId_AGENT_ID_GENERAL_ASSISTANT, MaxConcurrentRuns: 1}}}); err != nil {
		t.Fatal(err)
	}
	recvRuntimeCommand(t, workerStream, func(cmd *turingv1.RuntimeCommand) bool {
		return cmd.GetWorkerAccepted() != nil
	})

	chatStream, err := h.chatClient.SendMessage(ctx, &turingv1.SendMessageRequest{
		SessionId:     sessionID,
		Content:       "hello connected worker",
		ModelProvider: turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA,
		Model:         "llama3.2",
	})
	if err != nil {
		t.Fatal(err)
	}
	queued, err := chatStream.Recv()
	if err != nil {
		t.Fatal(err)
	}
	runID := queued.GetRunQueued().RunId
	assigned := recvRuntimeCommand(t, workerStream, func(cmd *turingv1.RuntimeCommand) bool {
		assigned := cmd.GetRunAssigned()
		return assigned != nil && assigned.RunId == runID
	}).GetRunAssigned()
	if assigned.UserText != "hello connected worker" {
		t.Fatalf("assigned job = %+v", assigned)
	}
}

func TestSendMessageStreamsRunStartedWhenWorkerClaimsJob(t *testing.T) {
	h := newHarness(t)
	sessionID := h.createSession(t)
	ctx, cancel := context.WithTimeout(h.clientContext(), 2*time.Second)
	defer cancel()
	runtimeClient := turingv1.NewRuntimeServiceClient(h.conn)
	workerStream, err := runtimeClient.ConnectWorker(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = workerStream.CloseSend() }()
	if err := workerStream.Send(&turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_WorkerReady{WorkerReady: &turingv1.RuntimeWorkerReady{WorkerId: "worker-started-chat", AgentId: turingv1.AgentId_AGENT_ID_GENERAL_ASSISTANT, MaxConcurrentRuns: 1}}}); err != nil {
		t.Fatal(err)
	}
	recvRuntimeCommand(t, workerStream, func(cmd *turingv1.RuntimeCommand) bool {
		return cmd.GetWorkerAccepted() != nil
	})

	chatStream, err := h.chatClient.SendMessage(ctx, &turingv1.SendMessageRequest{
		SessionId:     sessionID,
		Content:       "hello started",
		ModelProvider: turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA,
		Model:         "llama3.2",
	})
	if err != nil {
		t.Fatal(err)
	}
	queued, err := chatStream.Recv()
	if err != nil {
		t.Fatal(err)
	}
	runID := queued.GetRunQueued().RunId
	assigned := recvRuntimeCommand(t, workerStream, func(cmd *turingv1.RuntimeCommand) bool {
		assigned := cmd.GetRunAssigned()
		return assigned != nil && assigned.RunId == runID
	}).GetRunAssigned()
	started, err := chatStream.Recv()
	if err != nil {
		t.Fatal(err)
	}
	if started.GetRunStarted().GetRunId() != runID || started.GetRunStarted().GetJobId() != assigned.JobId || started.GetRunStarted().GetAttempt() != assigned.Attempt {
		t.Fatalf("run_started = %+v, assigned = %+v", started.GetRunStarted(), assigned)
	}
}

func TestSendMessageCancelsRunWhenDispatchFails(t *testing.T) {
	h := newHarness(t)
	sessionID := h.createSession(t)
	ctx, cancel := context.WithTimeout(h.clientContext(), 2*time.Second)
	defer cancel()
	runtimeClient := turingv1.NewRuntimeServiceClient(h.conn)
	workerStream, err := runtimeClient.ConnectWorker(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = workerStream.CloseSend() }()
	if err := workerStream.Send(&turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_WorkerReady{WorkerReady: &turingv1.RuntimeWorkerReady{WorkerId: "worker-dispatch-fails", AgentId: turingv1.AgentId_AGENT_ID_GENERAL_ASSISTANT, MaxConcurrentRuns: 1}}}); err != nil {
		t.Fatal(err)
	}
	recvRuntimeCommand(t, workerStream, func(cmd *turingv1.RuntimeCommand) bool {
		return cmd.GetWorkerAccepted() != nil
	})
	if _, err := h.database.ExecContext(ctx, `
		CREATE TRIGGER fail_job_claim
		BEFORE UPDATE OF status ON jobs
		WHEN NEW.status = 'in_progress'
		BEGIN
			SELECT RAISE(ABORT, 'claim failed');
		END;
	`); err != nil {
		t.Fatal(err)
	}
	chatStream, err := h.chatClient.SendMessage(ctx, &turingv1.SendMessageRequest{
		SessionId:     sessionID,
		Content:       "dispatch failure",
		ModelProvider: turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA,
		Model:         "llama3.2",
	})
	if err != nil {
		t.Fatal(err)
	}
	queued, err := chatStream.Recv()
	if err != nil {
		t.Fatal(err)
	}
	receivedCancelled := false
	for {
		event, recvErr := chatStream.Recv()
		if recvErr != nil {
			if !receivedCancelled && status.Code(recvErr) != codes.Internal {
				t.Fatalf("Recv after dispatch failure = %v, want Internal or run_cancelled event", recvErr)
			}
			if receivedCancelled && !errors.Is(recvErr, io.EOF) {
				t.Fatalf("Recv after run_cancelled = %v, want EOF", recvErr)
			}
			break
		}
		if event.GetRunStarted() != nil {
			t.Fatalf("run_started event = %+v, want dispatch failure", event.GetRunStarted())
		}
		if event.GetRunCancelled() != nil {
			receivedCancelled = true
		}
	}
	run, err := h.repo.GetRun(context.Background(), queued.GetRunQueued().RunId)
	if err != nil {
		t.Fatal(err)
	}
	if run.Status != "cancelled" {
		t.Fatalf("run status = %q, want cancelled", run.Status)
	}
}

func TestSendMessageMapsEnqueueDatabaseErrorToInternal(t *testing.T) {
	h := newHarness(t)
	sessionID := h.createSession(t)
	if err := h.database.Close(); err != nil {
		t.Fatal(err)
	}
	stream, err := h.chatClient.SendMessage(h.clientContext(), &turingv1.SendMessageRequest{
		SessionId:     sessionID,
		Content:       "db closed",
		ModelProvider: turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA,
		Model:         "llama3.2",
	})
	if err == nil {
		_, err = stream.Recv()
	}
	if status.Code(err) != codes.Internal {
		t.Fatalf("SendMessage error = %v, want Internal", err)
	}
}

func TestSendMessageMissingSessionReturnsNotFound(t *testing.T) {
	h := newHarness(t)
	stream, err := h.chatClient.SendMessage(h.clientContext(), &turingv1.SendMessageRequest{
		SessionId:     "sess_missing",
		Content:       "missing",
		ModelProvider: turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA,
		Model:         "llama3.2",
	})
	if err == nil {
		_, err = stream.Recv()
	}
	if status.Code(err) != codes.NotFound {
		t.Fatalf("SendMessage error = %v, want NotFound", err)
	}
}

func TestSendMessageDeletingSessionReturnsFailedPrecondition(t *testing.T) {
	h := newHarness(t)
	sessionID := h.createSession(t)
	if _, err := h.repo.BeginSessionDeletion(context.Background(), sessionID); err != nil {
		t.Fatal(err)
	}

	err := sendMessageError(h, &turingv1.SendMessageRequest{
		SessionId:     sessionID,
		Content:       "do not resurrect",
		ModelProvider: turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA,
		Model:         "llama3.2",
	})
	if status.Code(err) != codes.FailedPrecondition ||
		status.Convert(err).Message() != "session deletion is in progress" {
		t.Fatalf("SendMessage error = %v, want deletion FailedPrecondition", err)
	}
}

func TestSendMessageStreamClosesCleanlyWhenSessionIsDeleted(t *testing.T) {
	database := openChatTestDB(t)
	repo := repository.New(database)
	bus := events.NewBus(8)
	dispatchStarted := make(chan struct{})
	dispatchRelease := make(chan struct{})
	dispatcher := &blockingDispatcher{
		started: dispatchStarted,
		release: dispatchRelease,
	}
	service := New(repo, bus, dispatcher, "llama3.2", "gpt-4o-mini")
	session, err := repo.CreateSession(context.Background(), "Delete while streaming")
	if err != nil {
		t.Fatal(err)
	}
	stream := &notifyingQueuedChatStream{
		ctx:    context.Background(),
		queued: make(chan struct{}, 1),
	}
	done := make(chan error, 1)
	go func() {
		done <- service.SendMessage(&turingv1.SendMessageRequest{
			SessionId:     session.SessionID,
			Content:       "delete this stream",
			ModelProvider: turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA,
			Model:         "llama3.2",
		}, stream)
	}()
	<-stream.queued
	<-dispatchStarted

	receipt, err := repo.BeginSessionDeletion(context.Background(), session.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	receipt, err = repo.AdvanceSessionDeletion(context.Background(), session.SessionID, nil)
	if err != nil {
		t.Fatal(err)
	}
	if receipt.State != "completed" {
		t.Fatalf("deletion state = %q, want completed", receipt.State)
	}
	bus.TerminateSession(events.Event{
		SessionID: session.SessionID,
		Sequence:  receipt.TerminalSequence,
		Type:      "session.deleted",
	})
	close(dispatchRelease)

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("SendMessage after session deletion = %v, want clean close", err)
		}
	case <-time.After(time.Second):
		t.Fatal("SendMessage did not close after session deletion")
	}
}

func TestSendMessageCancellationCancelsRun(t *testing.T) {
	h := newHarness(t)
	worker := connectChatTestWorker(t, h, defaultChatWorkerCapabilities(false))
	defer func() { _ = worker.CloseSend() }()
	sessionID := h.createSession(t)
	ctx, cancel := context.WithCancel(h.clientContext())
	stream, err := h.chatClient.SendMessage(ctx, &turingv1.SendMessageRequest{
		SessionId:     sessionID,
		Content:       "cancel this",
		ModelProvider: turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA,
		Model:         "llama3.2",
	})
	if err != nil {
		t.Fatal(err)
	}
	first, err := stream.Recv()
	if err != nil {
		t.Fatal(err)
	}
	runID := first.GetRunQueued().RunId
	cancel()
	// A run-started event may already be buffered when cancellation wins on
	// the server. Drain in-flight events until the cancelled stream closes.
	for err == nil {
		_, err = stream.Recv()
	}
	if status.Code(err) != codes.Canceled && err != io.EOF {
		t.Fatalf("Recv after cancel = %v", err)
	}
	cancelled := false
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		run, err := h.repo.GetRun(context.Background(), runID)
		if err == nil && run.Status == "cancelled" {
			cancelled = true
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !cancelled {
		t.Fatal("run was not cancelled")
	}
	replayed, _, err := h.repo.ReplayEvents(context.Background(), sessionID, 0, 20)
	if err != nil {
		t.Fatal(err)
	}
	for _, event := range replayed {
		if event.Type == "agent.run.cancelled" && event.RunID.Valid && event.RunID.String == runID {
			// map[string]any: the cancellation payload now carries the
			// nested canonical run state beside its scalar fields.
			var payload map[string]any
			if err := json.Unmarshal([]byte(event.PayloadJSON), &payload); err != nil {
				t.Fatal(err)
			}
			if payload["runId"] != runID || payload["reason"] != "abandoned" {
				t.Fatalf("cancel payload = %+v", payload)
			}
			return
		}
	}
	t.Fatalf("agent.run.cancelled event not replayed: %+v", replayed)
}

func TestSendMessageStillCancelsRuntimeWhenPersistenceFails(t *testing.T) {
	h := newHarness(t)
	sessionID := h.createSession(t)
	chatCtx, cancelChat := context.WithCancel(h.clientContext())
	workerCtx, cancelWorker := context.WithTimeout(h.clientContext(), 2*time.Second)
	defer cancelWorker()
	runtimeClient := turingv1.NewRuntimeServiceClient(h.conn)
	workerStream, err := runtimeClient.ConnectWorker(workerCtx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = workerStream.CloseSend() }()
	if err := workerStream.Send(&turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_WorkerReady{WorkerReady: &turingv1.RuntimeWorkerReady{WorkerId: "worker-cancel-persist-fails", AgentId: turingv1.AgentId_AGENT_ID_GENERAL_ASSISTANT, MaxConcurrentRuns: 1}}}); err != nil {
		t.Fatal(err)
	}
	recvRuntimeCommand(t, workerStream, func(cmd *turingv1.RuntimeCommand) bool {
		return cmd.GetWorkerAccepted() != nil
	})
	chatStream, err := h.chatClient.SendMessage(chatCtx, &turingv1.SendMessageRequest{
		SessionId:     sessionID,
		Content:       "cancel persistence fails",
		ModelProvider: turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA,
		Model:         "llama3.2",
	})
	if err != nil {
		t.Fatal(err)
	}
	queued, err := chatStream.Recv()
	if err != nil {
		t.Fatal(err)
	}
	runID := queued.GetRunQueued().RunId
	recvRuntimeCommand(t, workerStream, func(cmd *turingv1.RuntimeCommand) bool {
		assigned := cmd.GetRunAssigned()
		return assigned != nil && assigned.RunId == runID
	})
	recvChatRunStarted(t, chatStream, runID)
	if _, err := h.database.ExecContext(context.Background(), `
		CREATE TRIGGER fail_chat_cancel_event
		BEFORE INSERT ON events
		WHEN NEW.type = 'agent.run.cancelled'
		BEGIN
			SELECT RAISE(ABORT, 'cancel event insert failed');
		END;
	`); err != nil {
		t.Fatal(err)
	}
	cancelChat()
	_, err = chatStream.Recv()
	if status.Code(err) != codes.Canceled && err != io.EOF {
		t.Fatalf("Recv after cancel = %v", err)
	}

	received := make(chan struct {
		cmd *turingv1.RuntimeCommand
		err error
	}, 1)
	go func() {
		cmd, err := workerStream.Recv()
		received <- struct {
			cmd *turingv1.RuntimeCommand
			err error
		}{cmd: cmd, err: err}
	}()
	select {
	case result := <-received:
		if result.err != nil {
			t.Fatal(result.err)
		}
		cancel := result.cmd.GetRunCancelled()
		if cancel == nil || cancel.RunId != runID || cancel.Reason != "client_cancelled" {
			t.Fatalf("runtime command = %+v, want cancellation for %q", result.cmd, runID)
		}
	case <-time.After(time.Second):
		t.Fatal("runtime cancellation was skipped after persistence failure")
	}
}

func TestSendMessageStreamsRuntimeEvents(t *testing.T) {
	h := newHarness(t)
	sessionID := h.createSession(t)
	ctx, cancel := context.WithTimeout(h.clientContext(), 2*time.Second)
	defer cancel()
	workerStream := connectChatTestWorker(t, h, defaultChatWorkerCapabilities(false))
	defer func() { _ = workerStream.CloseSend() }()
	chatStream, err := h.chatClient.SendMessage(ctx, &turingv1.SendMessageRequest{
		SessionId:     sessionID,
		Content:       "stream this",
		ModelProvider: turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA,
		Model:         "llama3.2",
	})
	if err != nil {
		t.Fatal(err)
	}
	queued, err := chatStream.Recv()
	if err != nil {
		t.Fatal(err)
	}
	runID := queued.GetRunQueued().RunId
	traceID := queued.GetRunQueued().TraceId

	recvRuntimeCommand(t, workerStream, func(cmd *turingv1.RuntimeCommand) bool {
		assigned := cmd.GetRunAssigned()
		return assigned != nil && assigned.RunId == runID
	})
	recvChatRunStarted(t, chatStream, runID)
	payload, err := structpb.NewStruct(map[string]any{"delta": "hi"})
	if err != nil {
		t.Fatal(err)
	}
	if err := workerStream.Send(&turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_Event{Event: &turingv1.TuringEvent{
		SessionId: sessionID,
		RunId:     runID,
		TraceId:   traceID,
		Type:      turingv1.TuringEventType_TURING_EVENT_TYPE_MESSAGE_DELTA,
		Payload:   payload,
	}}}); err != nil {
		t.Fatal(err)
	}
	delta, err := chatStream.Recv()
	if err != nil {
		t.Fatal(err)
	}
	if delta.GetTokenDelta().GetDelta() != "hi" {
		t.Fatalf("delta event = %+v", delta)
	}
}

func TestSendMessageStreamsRuntimeRunCompleted(t *testing.T) {
	h := newHarness(t)
	sessionID := h.createSession(t)
	ctx, cancel := context.WithTimeout(h.clientContext(), 2*time.Second)
	defer cancel()
	workerStream := connectChatTestWorker(t, h, defaultChatWorkerCapabilities(false))
	defer func() { _ = workerStream.CloseSend() }()
	chatStream, err := h.chatClient.SendMessage(ctx, &turingv1.SendMessageRequest{
		SessionId:     sessionID,
		Content:       "complete this",
		ModelProvider: turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA,
		Model:         "llama3.2",
	})
	if err != nil {
		t.Fatal(err)
	}
	queued, err := chatStream.Recv()
	if err != nil {
		t.Fatal(err)
	}
	runID := queued.GetRunQueued().RunId

	assigned := recvRuntimeCommand(t, workerStream, func(cmd *turingv1.RuntimeCommand) bool {
		assigned := cmd.GetRunAssigned()
		return assigned != nil && assigned.RunId == runID
	}).GetRunAssigned()
	recvChatRunStarted(t, chatStream, runID)
	if err := workerStream.Send(&turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_RunCompleted{RunCompleted: &turingv1.RuntimeRunCompleted{
		RunId:              runID,
		AssistantMessageId: assigned.AssistantMessageId,
		Content:            "done",
	}}}); err != nil {
		t.Fatal(err)
	}

	messageCompleted, err := chatStream.Recv()
	if err != nil {
		t.Fatal(err)
	}
	if messageCompleted.GetMessageCompleted().GetContent() != "done" || messageCompleted.GetMessageCompleted().GetMessageId() != assigned.AssistantMessageId {
		t.Fatalf("message_completed = %+v", messageCompleted)
	}
	completed, err := chatStream.Recv()
	if err != nil {
		t.Fatal(err)
	}
	if completed.GetRunCompleted().GetRunId() != runID || completed.GetRunCompleted().GetAssistantMessageId() != assigned.AssistantMessageId {
		t.Fatalf("run_completed = %+v", completed)
	}
}

func TestSendMessageWaitsForRunCompletedAfterTokenDelta(t *testing.T) {
	h := newHarness(t)
	sessionID := h.createSession(t)
	ctx, cancel := context.WithTimeout(h.clientContext(), 2*time.Second)
	defer cancel()
	workerStream := connectChatTestWorker(t, h, defaultChatWorkerCapabilities(false))
	defer func() { _ = workerStream.CloseSend() }()
	chatStream, err := h.chatClient.SendMessage(ctx, &turingv1.SendMessageRequest{
		SessionId:     sessionID,
		Content:       "complete after message",
		ModelProvider: turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA,
		Model:         "llama3.2",
	})
	if err != nil {
		t.Fatal(err)
	}
	queued, err := chatStream.Recv()
	if err != nil {
		t.Fatal(err)
	}
	runID := queued.GetRunQueued().RunId
	traceID := queued.GetRunQueued().TraceId

	assigned := recvRuntimeCommand(t, workerStream, func(cmd *turingv1.RuntimeCommand) bool {
		assigned := cmd.GetRunAssigned()
		return assigned != nil && assigned.RunId == runID
	}).GetRunAssigned()
	recvChatRunStarted(t, chatStream, runID)
	messagePayload, err := structpb.NewStruct(map[string]any{"messageId": assigned.AssistantMessageId, "delta": "partial"})
	if err != nil {
		t.Fatal(err)
	}
	if err := workerStream.Send(&turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_Event{Event: &turingv1.TuringEvent{
		SessionId: sessionID,
		RunId:     runID,
		TraceId:   traceID,
		Type:      turingv1.TuringEventType_TURING_EVENT_TYPE_MESSAGE_DELTA,
		Payload:   messagePayload,
	}}}); err != nil {
		t.Fatal(err)
	}
	tokenDelta, err := chatStream.Recv()
	if err != nil {
		t.Fatal(err)
	}
	if tokenDelta.GetTokenDelta().GetDelta() != "partial" {
		t.Fatalf("token_delta = %+v", tokenDelta)
	}
	if err := workerStream.Send(&turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_RunCompleted{RunCompleted: &turingv1.RuntimeRunCompleted{
		RunId:              runID,
		AssistantMessageId: assigned.AssistantMessageId,
		Content:            "done",
	}}}); err != nil {
		t.Fatal(err)
	}
	messageCompleted, err := chatStream.Recv()
	if err != nil {
		t.Fatal(err)
	}
	if messageCompleted.GetMessageCompleted().GetContent() != "done" {
		t.Fatalf("message_completed = %+v", messageCompleted)
	}
	runCompleted, err := chatStream.Recv()
	if err != nil {
		t.Fatal(err)
	}
	if runCompleted.GetRunCompleted().GetRunId() != runID {
		t.Fatalf("run_completed = %+v", runCompleted)
	}
}

func TestSendMessageReplaysPersistedTerminalEventWithoutBusWake(t *testing.T) {
	h := newHarness(t)
	worker := connectChatTestWorker(t, h, defaultChatWorkerCapabilities(false))
	defer func() { _ = worker.CloseSend() }()
	sessionID := h.createSession(t)
	ctx, cancel := context.WithTimeout(h.clientContext(), 2*time.Second)
	defer cancel()
	chatStream, err := h.chatClient.SendMessage(ctx, &turingv1.SendMessageRequest{
		SessionId:     sessionID,
		Content:       "persisted terminal",
		ModelProvider: turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA,
		Model:         "llama3.2",
	})
	if err != nil {
		t.Fatal(err)
	}
	queued, err := chatStream.Recv()
	if err != nil {
		t.Fatal(err)
	}
	recvChatRunStarted(t, chatStream, queued.GetRunQueued().GetRunId())
	appended, err := h.repo.AppendEvent(ctx, repository.AppendEventInput{
		SessionID:   sessionID,
		RunID:       queued.GetRunQueued().RunId,
		TraceID:     queued.GetRunQueued().TraceId,
		Type:        "agent.run.completed",
		PayloadJSON: `{"assistantMessageId":"msg_done"}`,
	})
	if err != nil {
		t.Fatal(err)
	}

	completed, err := chatStream.Recv()
	if err != nil {
		t.Fatal(err)
	}
	if completed.Sequence != appended.Sequence || completed.GetRunCompleted().GetRunId() != queued.GetRunQueued().RunId {
		t.Fatalf("run_completed = %+v, appended = %+v", completed, appended)
	}
}

func TestCancelRunPublishesDependentLifecycleEventsInOrder(t *testing.T) {
	h := newHarness(t)
	session, err := h.repo.CreateSession(context.Background(), "Cancel pending approval")
	if err != nil {
		t.Fatal(err)
	}
	enqueued, err := h.repo.EnqueueUserMessage(context.Background(), repository.EnqueueUserMessageInput{
		SessionID: session.SessionID, Content: "cancel me", AgentID: "general_assistant",
		ModelProvider: "ollama", Model: "llama3.2",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := h.repo.MarkRunRunning(context.Background(), enqueued.RunID); err != nil {
		t.Fatal(err)
	}
	if err := h.repo.RecordToolCallBefore(context.Background(), repository.ToolCallRecord{
		ToolCallID: "call_cancelled", RunID: enqueued.RunID, Status: "approval_required",
	}, "general_assistant", "files", "files.update", `{"path":"note.txt"}`, "sha256:test"); err != nil {
		t.Fatal(err)
	}
	if _, err := h.repo.CreateApproval(context.Background(), enqueued.RunID, "call_cancelled", "general_assistant", "files.update", `{"path":"note.txt"}`, "sha256:test", "2099-01-01T00:00:00Z"); err != nil {
		t.Fatal(err)
	}
	ch, unsubscribe := h.bus.Subscribe(enqueued.SessionID)
	defer unsubscribe()

	h.service.cancelRun(enqueued.RunID)

	var got []string
	for len(got) < 3 {
		select {
		case event := <-ch:
			if event.RunID == enqueued.RunID &&
				(event.Type == "approval.expired" || event.Type == "tool.call.failed" || event.Type == "agent.run.cancelled") {
				got = append(got, event.Type)
			}
		case <-time.After(2 * time.Second):
			t.Fatalf("published cancellation lifecycle = %v, want dependent and terminal events", got)
		}
	}
	want := []string{"approval.expired", "tool.call.failed", "agent.run.cancelled"}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("published cancellation lifecycle = %v, want %v", got, want)
		}
	}
}

func recvRuntimeCommand(t *testing.T, stream turingv1.RuntimeService_ConnectWorkerClient, match func(*turingv1.RuntimeCommand) bool) *turingv1.RuntimeCommand {
	t.Helper()
	deadline := time.After(2 * time.Second)
	for {
		received := make(chan struct {
			cmd *turingv1.RuntimeCommand
			err error
		}, 1)
		go func() {
			cmd, err := stream.Recv()
			received <- struct {
				cmd *turingv1.RuntimeCommand
				err error
			}{cmd: cmd, err: err}
		}()
		select {
		case <-deadline:
			t.Fatal("timed out waiting for runtime command")
		case result := <-received:
			if result.err != nil {
				t.Fatalf("Recv runtime command: %v", result.err)
			}
			if match(result.cmd) {
				return result.cmd
			}
		}
	}
}

func recvChatRunStarted(t *testing.T, stream turingv1.ChatService_SendMessageClient, runID string) *turingv1.RunStarted {
	t.Helper()
	event, err := stream.Recv()
	if err != nil {
		t.Fatal(err)
	}
	started := event.GetRunStarted()
	if started == nil || started.RunId != runID {
		t.Fatalf("chat event = %+v, want run_started for %s", event, runID)
	}
	return started
}

func TestMapChatEventConvertsKnownEvents(t *testing.T) {
	tests := []struct {
		name     string
		event    events.Event
		assertFn func(*testing.T, *turingv1.ChatStreamEvent)
	}{
		{
			name:  "message delta",
			event: events.Event{SessionID: "sess_1", RunID: "run_1", TraceID: "trace_1", Sequence: 2, Type: "message.delta", PayloadJSON: `{"delta":"hi"}`},
			assertFn: func(t *testing.T, got *turingv1.ChatStreamEvent) {
				t.Helper()
				if got.GetTokenDelta().GetDelta() != "hi" {
					t.Fatalf("delta = %q", got.GetTokenDelta().GetDelta())
				}
			},
		},
		{
			name:  "message completed",
			event: events.Event{SessionID: "sess_1", RunID: "run_1", TraceID: "trace_1", Sequence: 3, Type: "message.completed", PayloadJSON: `{"content":"done"}`},
			assertFn: func(t *testing.T, got *turingv1.ChatStreamEvent) {
				t.Helper()
				if got.GetMessageCompleted().GetContent() != "done" {
					t.Fatalf("content = %q", got.GetMessageCompleted().GetContent())
				}
			},
		},
		{
			name:  "run started",
			event: events.Event{SessionID: "sess_1", RunID: "run_1", TraceID: "trace_1", Sequence: 4, Type: "agent.run.started", PayloadJSON: `{"jobId":"job_1","attempt":2}`},
			assertFn: func(t *testing.T, got *turingv1.ChatStreamEvent) {
				t.Helper()
				started := got.GetRunStarted()
				if started.GetRunId() != "run_1" || started.GetJobId() != "job_1" || started.GetAttempt() != 2 {
					t.Fatalf("run_started = %+v", started)
				}
			},
		},
		{
			name:  "run completed",
			event: events.Event{SessionID: "sess_1", RunID: "run_1", TraceID: "trace_1", Sequence: 5, Type: "agent.run.completed", PayloadJSON: `{}`},
			assertFn: func(t *testing.T, got *turingv1.ChatStreamEvent) {
				t.Helper()
				if got.GetRunCompleted().GetRunId() != "run_1" {
					t.Fatalf("run_completed = %+v", got.GetRunCompleted())
				}
			},
		},
		{
			// The legacy code, message and retryable fields are answered with
			// fixed generic values whatever the row holds: a new client reads
			// RunState, and an older one must not be handed a provider's own
			// sentence just because a stored payload still has one.
			name:  "run failed",
			event: events.Event{SessionID: "sess_1", RunID: "run_1", TraceID: "trace_1", Sequence: 6, Type: "agent.run.failed", PayloadJSON: `{"code":"model_error","message":"boom","retryable":true}`},
			assertFn: func(t *testing.T, got *turingv1.ChatStreamEvent) {
				t.Helper()
				failed := got.GetRunFailed()
				retryableField := failed.ProtoReflect().Descriptor().Fields().ByNumber(4)
				if retryableField == nil || retryableField.Name() != "retryable" {
					t.Fatalf("run_failed retryable field descriptor = %v, want field 4 named retryable", retryableField)
				}
				if failed.GetCode() != legacyFailureCode || failed.GetMessage() != "" || failed.ProtoReflect().Get(retryableField).Bool() {
					t.Fatalf("run_failed = %+v, want the fixed generic legacy values", failed)
				}
			},
		},
		{
			name:  "run cancelled",
			event: events.Event{SessionID: "sess_1", RunID: "run_1", TraceID: "trace_1", Sequence: 7, Type: "agent.run.cancelled", PayloadJSON: `{"reason":"client_cancelled"}`},
			assertFn: func(t *testing.T, got *turingv1.ChatStreamEvent) {
				t.Helper()
				if got.GetRunCancelled().GetReason() != legacyCancellationReason {
					t.Fatalf("run_cancelled = %+v, want the fixed generic legacy reason", got.GetRunCancelled())
				}
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := mapChatEvent(tt.event)
			if got.SessionId != tt.event.SessionID || got.RunId != tt.event.RunID || got.TraceId != tt.event.TraceID || got.Sequence != tt.event.Sequence {
				t.Fatalf("metadata = %+v, want event metadata %+v", got, tt.event)
			}
			tt.assertFn(t, got)
		})
	}
}

func TestMapChatEventFallsBackToPersistedEvent(t *testing.T) {
	got := mapChatEvent(events.Event{
		EventID:     "evt_1",
		SessionID:   "sess_1",
		RunID:       "run_1",
		TraceID:     "trace_1",
		Sequence:    7,
		Type:        "system",
		CreatedAt:   "2026-05-15T00:00:00Z",
		PayloadJSON: `{"ready":true}`,
	})
	persisted := got.GetPersistedEvent()
	if persisted == nil {
		t.Fatalf("event = %T, want persisted_event", got.Event)
	}
	if persisted.EventId != "evt_1" || persisted.Type != turingv1.TuringEventType_TURING_EVENT_TYPE_SYSTEM || persisted.CreatedAt == nil {
		t.Fatalf("persisted event = %+v", persisted)
	}
	if !persisted.Payload.GetFields()["ready"].GetBoolValue() {
		t.Fatalf("payload = %+v", persisted.Payload)
	}
}

func TestMapChatEventMapsSessionUpdatedFallback(t *testing.T) {
	got := mapChatEvent(events.Event{
		EventID:     "evt_session_updated",
		SessionID:   "sess_1",
		TraceID:     "trace_1",
		Sequence:    1,
		Type:        "session.updated",
		CreatedAt:   "2026-08-18T20:00:00Z",
		PayloadJSON: `{"title":"First turn","updatedAt":"2026-08-18T20:00:00Z"}`,
	})
	persisted := got.GetPersistedEvent()
	if persisted == nil {
		t.Fatalf("event = %T, want persisted_event", got.Event)
	}
	if persisted.Type != turingv1.TuringEventType_TURING_EVENT_TYPE_SESSION_UPDATED {
		t.Fatalf("event type = %v, want SESSION_UPDATED", persisted.Type)
	}
}

// A payload this build cannot read used to be answered with the JSON parser's
// own sentence, which is built from the bytes it failed on — so an unreadable
// row published exactly what the read boundary exists to withhold. The event
// keeps the type it actually is and loses only the values nobody could read.
func TestMapChatEventDropsUnreadablePayloadWithoutParserText(t *testing.T) {
	got := mapChatEvent(events.Event{SessionID: "sess_1", RunID: "run_1", TraceID: "trace_1", Sequence: 8, Type: "message.delta", PayloadJSON: `{`})
	delta := got.GetTokenDelta()
	if delta == nil {
		t.Fatalf("event = %T, want token_delta", got.Event)
	}
	if delta.GetDelta() != "" || delta.GetMessageId() != "" {
		t.Fatalf("token_delta = %+v, want no values read from an unreadable payload", delta)
	}
	if failed := got.GetRunFailed(); failed != nil {
		t.Fatalf("unreadable payload reported a failure: %+v", failed)
	}
}

// The one cancellation signal this product has covers both a deliberate stop
// and an unkeyed transport loss, and the client offers no cancel affordance. So
// a disconnect is abandonment, and it must say so in the closed vocabulary
// rather than storing the transport's own word for it.
func TestChatDisconnectNormalizesToAbandonedWithoutRawReason(t *testing.T) {
	h := newHarness(t)
	session, err := h.repo.CreateSession(context.Background(), "Disconnect")
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	enqueued, err := h.repo.EnqueueUserMessage(context.Background(), repository.EnqueueUserMessageInput{
		SessionID:     session.SessionID,
		Content:       "abandon me",
		AgentID:       "general_assistant",
		ModelProvider: "ollama",
		Model:         "llama3.2",
	})
	if err != nil {
		t.Fatalf("EnqueueUserMessage: %v", err)
	}

	h.service.cancelRun(enqueued.RunID)

	state, err := h.repo.GetRunState(context.Background(), enqueued.RunID)
	if err != nil {
		t.Fatalf("GetRunState: %v", err)
	}
	if state.Lifecycle != "cancelled" {
		t.Fatalf("lifecycle = %q, want cancelled", state.Lifecycle)
	}
	if state.OutcomeReason != "abandoned" {
		t.Fatalf("outcome reason = %q, want abandoned", state.OutcomeReason)
	}

	var cancellationReason, errorMessage sql.NullString
	if err := h.database.QueryRowContext(context.Background(),
		`SELECT cancellation_reason, error_message FROM agent_runs WHERE id = ?`, enqueued.RunID,
	).Scan(&cancellationReason, &errorMessage); err != nil {
		t.Fatalf("read run diagnostics: %v", err)
	}
	if errorMessage.Valid {
		t.Fatalf("cancellation persisted an error message %q", errorMessage.String)
	}
	if cancellationReason.Valid && cancellationReason.String != "client_cancelled" {
		t.Fatalf("cancellation_reason = %q, want the allowlisted client_cancelled", cancellationReason.String)
	}

	var payload string
	if err := h.database.QueryRowContext(context.Background(),
		`SELECT payload_json FROM events WHERE run_id = ? AND type = 'agent.run.cancelled'`, enqueued.RunID,
	).Scan(&payload); err != nil {
		t.Fatalf("read cancellation event: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal([]byte(payload), &decoded); err != nil {
		t.Fatalf("decode cancellation payload: %v", err)
	}
	if decoded["reason"] != "abandoned" {
		t.Fatalf("cancellation payload reason = %#v, want abandoned", decoded["reason"])
	}
	if _, exists := decoded["message"]; exists {
		t.Fatalf("cancellation payload carries a message: %#v", decoded)
	}
}
