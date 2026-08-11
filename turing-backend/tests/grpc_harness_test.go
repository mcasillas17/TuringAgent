package tests

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	runtimetestkit "github.com/mcasillas17/TuringAgent/turing-backend/agent-runtime-go/testkit"
	orchestratortestkit "github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/testkit"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
)

const (
	integrationClientKey     = "client-key"
	integrationInternalToken = "internal-token"
	integrationSystemToken   = "system-token"
	integrationFilesToken    = "files-token"
	integrationApprovalKey   = "approval-secret"
)

var integrationArtifacts string

type grpcHarness struct {
	repo         *orchestratortestkit.Repository
	fakeModel    *fakeModelServer
	systemMCP    *fakeMCPServer
	filesMCP     *fakeMCPServer
	chat         turingv1.ChatServiceClient
	sessions     turingv1.SessionServiceClient
	events       turingv1.EventServiceClient
	approvals    turingv1.ApprovalServiceClient
	publicConn   *grpc.ClientConn
	internalConn *grpc.ClientConn
	app          *orchestratortestkit.App
	publicLis    *bufconn.Listener
	internalLis  *bufconn.Listener
	workerCancel context.CancelFunc
	workerDone   chan error
	closeOnce    sync.Once
}

type fakeModelServer struct {
	server               *httptest.Server
	started              chan struct{}
	cancelled            chan struct{}
	blockUntilCancel     bool
	startedOnce          sync.Once
	cancelledOnce        sync.Once
	mu                   sync.Mutex
	chatCompletionBodies []map[string]any
}

type fakeMCPServer struct {
	server         *httptest.Server
	name           string
	token          string
	approvalTokens chan string
}

type harnessOption func(*harnessConfig)
type harnessConfig struct{ blockModelUntilCancel bool }

func TestMain(m *testing.M) {
	code := m.Run()
	if integrationArtifacts != "" {
		_ = os.RemoveAll(integrationArtifacts)
	}
	os.Exit(code)
}

func withBlockingModel() harnessOption {
	return func(cfg *harnessConfig) { cfg.blockModelUntilCancel = true }
}

func newGRPCHarness(t *testing.T, opts ...harnessOption) *grpcHarness {
	t.Helper()
	cfg := harnessConfig{}
	for _, opt := range opts {
		opt(&cfg)
	}
	backendRoot, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	dbPath := filepath.Join(artifactDir(t, backendRoot), fmt.Sprintf("%s-%d.db", sanitizeName(t.Name()), time.Now().UnixNano()))

	fakeModel := newFakeModelServer(cfg.blockModelUntilCancel)
	systemMCP := newFakeMCPServer("system", integrationSystemToken)
	filesMCP := newFakeMCPServer("files", integrationFilesToken)
	app, err := orchestratortestkit.NewApp(orchestratortestkit.Config{
		ClientAPIKey:             integrationClientKey,
		InternalToken:            integrationInternalToken,
		MCPSystemTokenGeneral:    integrationSystemToken,
		MCPFilesTokenGeneral:     integrationFilesToken,
		ApprovalJWTSecret:        integrationApprovalKey,
		DatabasePath:             dbPath,
		OllamaModel:              "fake-ollama",
		OpenAIModel:              "fake-model",
		MaxConcurrentRunsGeneral: 1,
		MaxToolCallsPerRun:       10,
	})
	if err != nil {
		t.Fatal(err)
	}
	publicLis := bufconn.Listen(4 * 1024 * 1024)
	internalLis := bufconn.Listen(4 * 1024 * 1024)
	go serveBufconn(app.PublicServer, publicLis)
	go serveBufconn(app.InternalServer, internalLis)

	h := &grpcHarness{
		repo:        app.Repository,
		fakeModel:   fakeModel,
		systemMCP:   systemMCP,
		filesMCP:    filesMCP,
		app:         app,
		publicLis:   publicLis,
		internalLis: internalLis,
	}
	t.Cleanup(h.close)

	h.publicConn = dialBufconn(t, publicLis)
	h.internalConn = dialBufconn(t, internalLis)
	h.chat = turingv1.NewChatServiceClient(h.publicConn)
	h.sessions = turingv1.NewSessionServiceClient(h.publicConn)
	h.events = turingv1.NewEventServiceClient(h.publicConn)
	h.approvals = turingv1.NewApprovalServiceClient(h.publicConn)
	h.waitForHealth(t)
	h.startRuntimeWorker()
	return h
}

func serveBufconn(server *grpc.Server, lis *bufconn.Listener) {
	if err := server.Serve(lis); err != nil && !errors.Is(err, grpc.ErrServerStopped) && !strings.Contains(err.Error(), "closed") {
		panic(err)
	}
}

func (h *grpcHarness) startRuntimeWorker() {
	ctx, cancel := context.WithCancel(context.Background())
	h.workerCancel = cancel
	h.workerDone = make(chan error, 1)
	go func() {
		err := runtimetestkit.RunWorker(ctx, runtimetestkit.WorkerConfig{
			Conn:              h.internalConn,
			InternalToken:     integrationInternalToken,
			WorkerID:          "worker-grpc-integration",
			MaxConcurrentRuns: 1,
			OpenAIBaseURL:     h.fakeModel.server.URL,
			OpenAIAPIKey:      "fake-key",
			MCPSystemBaseURL:  h.systemMCP.server.URL,
			MCPFilesBaseURL:   h.filesMCP.server.URL,
			MCPSystemToken:    integrationSystemToken,
			MCPFilesToken:     integrationFilesToken,
		})
		if err != nil && !errors.Is(err, context.Canceled) && status.Code(err) != codes.Canceled {
			h.workerDone <- err
			return
		}
		h.workerDone <- nil
	}()
}

func artifactDir(t *testing.T, backendRoot string) string {
	t.Helper()
	if integrationArtifacts == "" {
		integrationArtifacts = filepath.Join(backendRoot, "data", "go-grpc-tests", fmt.Sprintf("run-%d", os.Getpid()))
	}
	if err := os.MkdirAll(integrationArtifacts, 0o755); err != nil {
		t.Fatal(err)
	}
	return integrationArtifacts
}

func (h *grpcHarness) waitForHealth(t *testing.T) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	var lastErr error
	for time.Now().Before(deadline) {
		ctx, cancel := context.WithTimeout(h.clientContext(), 500*time.Millisecond)
		_, err := turingv1.NewHealthServiceClient(h.publicConn).Check(ctx, &turingv1.HealthCheckRequest{})
		cancel()
		if err == nil {
			return
		}
		lastErr = err
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("orchestrator health check failed: %v", lastErr)
}

func dialBufconn(t *testing.T, lis *bufconn.Listener) *grpc.ClientConn {
	t.Helper()
	conn, err := grpc.NewClient("passthrough:///bufnet", grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) {
		return lis.Dial()
	}), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	return conn
}

func (h *grpcHarness) clientContext() context.Context {
	return metadata.AppendToOutgoingContext(context.Background(), "authorization", "Bearer "+integrationClientKey)
}

func (h *grpcHarness) close() {
	h.closeOnce.Do(func() {
		if h.workerCancel != nil {
			h.workerCancel()
		}
		if h.publicConn != nil {
			_ = h.publicConn.Close()
			h.publicConn = nil
		}
		if h.internalConn != nil {
			_ = h.internalConn.Close()
			h.internalConn = nil
		}
		if h.workerDone != nil {
			select {
			case <-h.workerDone:
			case <-time.After(2 * time.Second):
			}
		}
		if h.app != nil {
			h.app.Stop()
			h.app = nil
		}
		if h.publicLis != nil {
			_ = h.publicLis.Close()
			h.publicLis = nil
		}
		if h.internalLis != nil {
			_ = h.internalLis.Close()
			h.internalLis = nil
		}
		if h.fakeModel != nil {
			h.fakeModel.server.Close()
			h.fakeModel = nil
		}
		if h.systemMCP != nil {
			h.systemMCP.server.Close()
			h.systemMCP = nil
		}
		if h.filesMCP != nil {
			h.filesMCP.server.Close()
			h.filesMCP = nil
		}
	})
}

func newFakeModelServer(blockUntilCancel bool) *fakeModelServer {
	fake := &fakeModelServer{
		started:          make(chan struct{}),
		cancelled:        make(chan struct{}),
		blockUntilCancel: blockUntilCancel,
	}
	fake.server = httptest.NewServer(http.HandlerFunc(fake.handleChatCompletion))
	return fake
}

func (f *fakeModelServer) handleChatCompletion(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost || r.URL.Path != "/chat/completions" {
		http.NotFound(w, r)
		return
	}
	defer r.Body.Close()
	var body map[string]any
	_ = json.NewDecoder(r.Body).Decode(&body)
	f.mu.Lock()
	f.chatCompletionBodies = append(f.chatCompletionBodies, body)
	f.mu.Unlock()

	w.Header().Set("content-type", "text/event-stream")
	flusher, _ := w.(http.Flusher)
	if flusher != nil {
		flusher.Flush()
	}
	f.startedOnce.Do(func() { close(f.started) })
	if f.blockUntilCancel {
		<-r.Context().Done()
		f.cancelledOnce.Do(func() { close(f.cancelled) })
		return
	}
	for _, token := range []string{"Hel", "lo"} {
		writeOpenAIChunk(w, token, "")
		if flusher != nil {
			flusher.Flush()
		}
	}
	writeOpenAIChunk(w, "", "stop")
	if flusher != nil {
		flusher.Flush()
	}
}

func writeOpenAIChunk(w http.ResponseWriter, token string, finishReason string) {
	choice := map[string]any{"delta": map[string]any{}}
	if token != "" {
		choice["delta"] = map[string]any{"content": token}
	}
	if finishReason != "" {
		choice["finish_reason"] = finishReason
	}
	data, _ := json.Marshal(map[string]any{"choices": []any{choice}})
	_, _ = fmt.Fprintf(w, "data: %s\n\n", data)
}

func newFakeMCPServer(name string, token string) *fakeMCPServer {
	fake := &fakeMCPServer{name: name, token: token, approvalTokens: make(chan string, 4)}
	fake.server = httptest.NewServer(http.HandlerFunc(fake.handle))
	return fake
}

func (f *fakeMCPServer) handle(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.NotFound(w, r)
		return
	}
	if got, want := r.Header.Get("authorization"), "Bearer "+f.token; got != want {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	defer r.Body.Close()
	var req struct {
		JSONRPC string         `json:"jsonrpc"`
		ID      any            `json:"id"`
		Method  string         `json:"method"`
		Params  map[string]any `json:"params"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONRPCError(w, nil, "bad request")
		return
	}
	if req.Method == "tools/list" {
		writeJSONRPCResult(w, req.ID, map[string]any{"tools": []any{}})
		return
	}
	if req.Method != "tools/call" {
		writeJSONRPCError(w, req.ID, "unknown method")
		return
	}
	toolName, _ := req.Params["name"].(string)
	args, _ := req.Params["arguments"].(map[string]any)
	meta, _ := req.Params["_meta"].(map[string]any)
	switch toolName {
	case "system.time":
		writeJSONRPCResult(w, req.ID, map[string]any{"time": "2025-01-02T03:04:05Z"})
	case "files.create":
		approvalToken, _ := meta["approvalToken"].(string)
		if approvalToken == "" {
			writeJSONRPCError(w, req.ID, "approval token required")
			return
		}
		select {
		case f.approvalTokens <- approvalToken:
		default:
		}
		path, _ := args["path"].(string)
		writeJSONRPCResult(w, req.ID, map[string]any{"path": path, "created": true, "content": "created through approval flow"})
	default:
		writeJSONRPCError(w, req.ID, "unknown tool")
	}
}

func writeJSONRPCResult(w http.ResponseWriter, id any, result map[string]any) {
	w.Header().Set("content-type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": id, "result": result})
}

func writeJSONRPCError(w http.ResponseWriter, id any, message string) {
	w.Header().Set("content-type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": id, "error": map[string]any{"code": -32000, "message": message}})
}

func sanitizeName(name string) string {
	name = strings.ToLower(name)
	var builder strings.Builder
	for _, r := range name {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			builder.WriteRune(r)
			continue
		}
		builder.WriteByte('-')
	}
	out := strings.Trim(builder.String(), "-")
	if out == "" {
		return "test"
	}
	return out
}

func TestSendMessageStreamsTokensToCompletion(t *testing.T) {
	harness := newGRPCHarness(t)
	defer harness.close()

	sessionID := harness.createSession(t, "token streaming")
	events := harness.sendMessageToCompletion(t, sessionID, "hello")

	assertTokenDeltas(t, events, []string{"Hel", "lo"})
	if completed := messageCompletedContent(events); completed != "Hello" {
		t.Fatalf("message completed content = %q, want Hello", completed)
	}
	if !hasRunCompleted(events) {
		t.Fatal("stream did not include run_completed")
	}
}

func TestApprovalRequiredToolFlow(t *testing.T) {
	harness := newGRPCHarness(t)
	defer harness.close()

	sessionID := harness.createSession(t, "approval flow")
	ctx, cancel := context.WithTimeout(harness.clientContext(), 15*time.Second)
	defer cancel()
	stream, err := harness.chat.SendMessage(ctx, &turingv1.SendMessageRequest{
		SessionId:     sessionID,
		Content:       "/tool files.create",
		ContentType:   "text",
		AgentId:       turingv1.AgentId_AGENT_ID_GENERAL_ASSISTANT,
		ModelProvider: turingv1.ModelProvider_MODEL_PROVIDER_OPENAI_COMPATIBLE,
		Model:         "fake-model",
	})
	if err != nil {
		t.Fatal(err)
	}

	var got []*turingv1.ChatStreamEvent
	approvalID := ""
	approved := false
	for {
		event, err := stream.Recv()
		if err != nil {
			t.Fatal(err)
		}
		got = append(got, event)
		if persisted := event.GetPersistedEvent(); persisted != nil && persisted.Type == turingv1.TuringEventType_TURING_EVENT_TYPE_APPROVAL_REQUESTED {
			approvalID = stringField(persisted.Payload, "approvalId")
			if approvalID == "" {
				t.Fatal("approval.requested missing approvalId")
			}
			if _, err := harness.approvals.ApproveApproval(harness.clientContext(), &turingv1.ApproveApprovalRequest{ApprovalId: approvalID}); err != nil {
				t.Fatal(err)
			}
			approved = true
		}
		if event.GetRunCompleted() != nil {
			break
		}
	}
	if !approved {
		t.Fatal("approval was not requested")
	}
	select {
	case token := <-harness.filesMCP.approvalTokens:
		if token == "" {
			t.Fatal("files MCP received empty approval token")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("files MCP did not receive approval token")
	}
	assertPersistedTypes(t, got,
		turingv1.TuringEventType_TURING_EVENT_TYPE_APPROVAL_REQUESTED,
		turingv1.TuringEventType_TURING_EVENT_TYPE_APPROVAL_APPROVED,
		turingv1.TuringEventType_TURING_EVENT_TYPE_TOOL_CALL_COMPLETED,
	)
	if completed := messageCompletedContent(got); completed == "" {
		t.Fatal("tool flow did not complete assistant message")
	}
}

func TestSubscribeSessionEventsReplaysAfterSequence(t *testing.T) {
	harness := newGRPCHarness(t)
	defer harness.close()

	sessionID := harness.createSession(t, "event replay")
	_ = harness.sendMessageToCompletion(t, sessionID, "hello")

	listed, err := harness.events.ListEvents(harness.clientContext(), &turingv1.ListEventsRequest{SessionId: sessionID, Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	if len(listed.Events) < 4 {
		t.Fatalf("listed %d events, want at least 4", len(listed.Events))
	}
	after := listed.Events[1].Sequence
	expected := eventsAfter(listed.Events, after)

	ctx, cancel := context.WithTimeout(harness.clientContext(), 3*time.Second)
	defer cancel()
	stream, err := harness.events.SubscribeSessionEvents(ctx, &turingv1.SubscribeSessionEventsRequest{SessionId: sessionID, AfterSequence: after})
	if err != nil {
		t.Fatal(err)
	}
	replayed := make([]*turingv1.TuringEvent, 0, len(expected))
	for range expected {
		event, err := stream.Recv()
		if err != nil {
			t.Fatal(err)
		}
		replayed = append(replayed, event)
	}
	assertSameEventSequenceAndTypes(t, replayed, expected)
}

func (h *grpcHarness) createSession(t *testing.T, title string) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(h.clientContext(), 5*time.Second)
	defer cancel()
	resp, err := h.sessions.CreateSession(ctx, &turingv1.CreateSessionRequest{Title: title})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if resp.SessionId == "" {
		t.Fatal("CreateSession returned empty session_id")
	}
	return resp.SessionId
}

func (h *grpcHarness) sendMessageToCompletion(t *testing.T, sessionID string, content string) []*turingv1.ChatStreamEvent {
	t.Helper()
	ctx, cancel := context.WithTimeout(h.clientContext(), 15*time.Second)
	defer cancel()
	stream, err := h.chat.SendMessage(ctx, &turingv1.SendMessageRequest{
		SessionId:     sessionID,
		Content:       content,
		ContentType:   "text",
		AgentId:       turingv1.AgentId_AGENT_ID_GENERAL_ASSISTANT,
		ModelProvider: turingv1.ModelProvider_MODEL_PROVIDER_OPENAI_COMPATIBLE,
		Model:         "fake-model",
	})
	if err != nil {
		t.Fatal(err)
	}
	var got []*turingv1.ChatStreamEvent
	for {
		event, err := stream.Recv()
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			t.Fatalf("SendMessage Recv: %v", err)
		}
		got = append(got, event)
		if event.GetRunCompleted() != nil || event.GetRunFailed() != nil || event.GetRunCancelled() != nil {
			break
		}
	}
	return got
}
