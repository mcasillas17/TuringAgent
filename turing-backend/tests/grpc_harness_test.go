package tests

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"
	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	runtimetestkit "github.com/mcasillas17/TuringAgent/turing-backend/agent-runtime-go/testkit"
	orchestratortestkit "github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/testkit"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/types/known/structpb"
)

const (
	integrationClientKey     = "client-key"
	integrationInternalToken = "internal-token"
	integrationSystemToken   = "system-token"
	integrationFilesToken    = "files-token"
	integrationApprovalKey   = "approval-secret"
	integrationOpenAIKey     = "fake-key"
)

var integrationArtifacts string

type grpcHarness struct {
	repo             *orchestratortestkit.Repository
	fakeModel        *fakeModelServer
	systemMCP        *fakeMCPServer
	filesMCP         *fakeMCPServer
	chat             turingv1.ChatServiceClient
	sessions         turingv1.SessionServiceClient
	events           turingv1.EventServiceClient
	approvals        turingv1.ApprovalServiceClient
	runtimeApprovals turingv1.ApprovalServiceClient
	publicConn       *grpc.ClientConn
	internalConn     *grpc.ClientConn
	app              *orchestratortestkit.App
	databasePath     string
	publicLis        *bufconn.Listener
	internalLis      *bufconn.Listener
	workerCancel     context.CancelFunc
	workerDone       chan error
	closeOnce        sync.Once
}

type fakeModelServer struct {
	server               *httptest.Server
	started              chan struct{}
	cancelled            chan struct{}
	blockUntilCancel     bool
	modelDrivenTool      string
	emptyFinalResponse   bool
	startedOnce          sync.Once
	cancelledOnce        sync.Once
	handlerErrorOnce     sync.Once
	mu                   sync.Mutex
	chatCompletionBodies []map[string]any
	handlerErrors        chan error
}

type fakeMCPRequest struct {
	method string
	params map[string]any
	id     json.Number
}

type fakeMCPServer struct {
	server           *httptest.Server
	name             string
	token            string
	approvalTokens   chan string
	createStarted    chan struct{}
	createCancelled  chan struct{}
	mu               sync.Mutex
	advertiseTime    bool
	advertiseCreate  bool
	validateApproval bool
	blockCreate      bool
	approvalClient   turingv1.ApprovalServiceClient
	requests         []fakeMCPRequest
	handlerErrorOnce sync.Once
	handlerErrors    chan error
}

type harnessOption func(*harnessConfig)
type harnessConfig struct {
	blockModelUntilCancel bool
	approvalTTL           time.Duration
	startRuntimeWorker    bool
	advertiseSystemTime   bool
	advertiseFilesCreate  bool
}

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

func withApprovalTTL(ttl time.Duration) harnessOption {
	return func(cfg *harnessConfig) { cfg.approvalTTL = ttl }
}

func withoutRuntimeWorker() harnessOption {
	return func(cfg *harnessConfig) { cfg.startRuntimeWorker = false }
}

func withSystemTimeTool() harnessOption {
	return func(cfg *harnessConfig) { cfg.advertiseSystemTime = true }
}

func withFilesCreateTool() harnessOption {
	return func(cfg *harnessConfig) { cfg.advertiseFilesCreate = true }
}

func TestPublicGRPCSessionDeletionDeliversTerminalAndRejectsReads(t *testing.T) {
	h := newGRPCHarness(t, withoutRuntimeWorker())
	ctx, cancel := context.WithTimeout(h.clientContext(), 5*time.Second)
	defer cancel()
	created, err := h.sessions.CreateSession(ctx, &turingv1.CreateSessionRequest{Title: "Delete over gRPC"})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	stream, err := h.events.SubscribeSessionEvents(ctx, &turingv1.SubscribeSessionEventsRequest{
		SessionId: created.GetSessionId(),
	})
	if err != nil {
		t.Fatalf("SubscribeSessionEvents: %v", err)
	}
	if err := h.app.WaitForSessionEventSubscriber(ctx, created.GetSessionId()); err != nil {
		t.Fatal(err)
	}

	deleted, err := h.sessions.DeleteSession(ctx, &turingv1.DeleteSessionRequest{SessionId: created.GetSessionId()})
	if err != nil {
		t.Fatalf("DeleteSession: %v", err)
	}
	if deleted.GetDeletion().GetState() != turingv1.SessionDeletionState_SESSION_DELETION_STATE_COMPLETED {
		t.Fatalf("deletion receipt = %+v, want completed", deleted.GetDeletion())
	}
	terminal, err := stream.Recv()
	if err != nil {
		t.Fatalf("terminal event: %v", err)
	}
	if terminal.GetType() != turingv1.TuringEventType_TURING_EVENT_TYPE_SESSION_DELETED {
		t.Fatalf("terminal type = %v, want SESSION_DELETED", terminal.GetType())
	}
	if _, err := stream.Recv(); status.Code(err) != codes.NotFound {
		t.Fatalf("terminal stream close = %v, want NotFound", err)
	}
	if _, err := h.sessions.GetSession(ctx, &turingv1.GetSessionRequest{SessionId: created.GetSessionId()}); status.Code(err) != codes.NotFound {
		t.Fatalf("GetSession after deletion = %v, want NotFound", err)
	}
	if _, err := h.events.ListEvents(ctx, &turingv1.ListEventsRequest{SessionId: created.GetSessionId()}); status.Code(err) != codes.NotFound {
		t.Fatalf("ListEvents after deletion = %v, want NotFound", err)
	}
}

func newGRPCHarness(t *testing.T, opts ...harnessOption) *grpcHarness {
	t.Helper()
	cfg := harnessConfig{startRuntimeWorker: true}
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
	if cfg.advertiseSystemTime {
		systemMCP.enableTimeTool()
	}
	if cfg.advertiseFilesCreate {
		filesMCP.enableCreateToolWithApprovalValidation()
	}
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
		ApprovalTTLMS:            int(cfg.approvalTTL / time.Millisecond),
	})
	if err != nil {
		t.Fatal(err)
	}
	publicLis := bufconn.Listen(4 * 1024 * 1024)
	internalLis := bufconn.Listen(4 * 1024 * 1024)
	go serveBufconn(app.PublicServer, publicLis)
	go serveBufconn(app.InternalServer, internalLis)

	h := &grpcHarness{
		repo:         app.Repository,
		fakeModel:    fakeModel,
		systemMCP:    systemMCP,
		filesMCP:     filesMCP,
		app:          app,
		databasePath: dbPath,
		publicLis:    publicLis,
		internalLis:  internalLis,
	}
	t.Cleanup(h.close)

	h.publicConn = dialBufconn(t, publicLis)
	h.internalConn = dialBufconn(t, internalLis)
	h.chat = turingv1.NewChatServiceClient(h.publicConn)
	h.sessions = turingv1.NewSessionServiceClient(h.publicConn)
	h.events = turingv1.NewEventServiceClient(h.publicConn)
	h.approvals = turingv1.NewApprovalServiceClient(h.publicConn)
	h.runtimeApprovals = turingv1.NewApprovalServiceClient(h.internalConn)
	h.filesMCP.approvalClient = turingv1.NewApprovalServiceClient(h.internalConn)
	h.waitForHealth(t)
	if cfg.startRuntimeWorker {
		h.startRuntimeWorker()
		h.waitForRuntimeWorker(t)
	}
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
			Conn:               h.internalConn,
			InternalToken:      integrationInternalToken,
			WorkerID:           "worker-grpc-integration",
			MaxConcurrentRuns:  1,
			MaxToolCallsPerRun: 10,
			OpenAIBaseURL:      h.fakeModel.server.URL,
			OpenAIAPIKey:       integrationOpenAIKey,
			OpenAIModel:        "fake-model",
			MCPSystemBaseURL:   h.systemMCP.server.URL,
			MCPFilesBaseURL:    h.filesMCP.server.URL,
			MCPSystemToken:     integrationSystemToken,
			MCPFilesToken:      integrationFilesToken,
		})
		if err != nil && !errors.Is(err, context.Canceled) && status.Code(err) != codes.Canceled {
			h.workerDone <- err
			return
		}
		h.workerDone <- nil
	}()
}

func (h *grpcHarness) waitForRuntimeWorker(t *testing.T) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	var lastErr error
	for time.Now().Before(deadline) {
		lastErr = h.app.ValidateRuntimeRoute(
			context.Background(), "general_assistant", "openai_compatible", "fake-model",
		)
		if lastErr == nil {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("runtime worker did not advertise capabilities: %v", lastErr)
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

func (h *grpcHarness) internalContext() context.Context {
	return metadata.AppendToOutgoingContext(context.Background(), "authorization", "Bearer "+integrationInternalToken)
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
		handlerErrors:    make(chan error, 1),
	}
	fake.server = httptest.NewServer(http.HandlerFunc(fake.handleChatCompletion))
	return fake
}

func (f *fakeModelServer) handleChatCompletion(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost || r.URL.Path != "/chat/completions" {
		f.reject(w, http.StatusNotFound, fmt.Errorf("OpenAI request = %s %s, want POST /chat/completions", r.Method, r.URL.Path))
		return
	}
	if got, want := r.Header.Get("authorization"), "Bearer "+integrationOpenAIKey; got != want {
		f.reject(w, http.StatusUnauthorized, fmt.Errorf("OpenAI authorization = %q, want %q", got, want))
		return
	}
	if got := r.Header.Get("content-type"); got != "application/json" {
		f.reject(w, http.StatusUnsupportedMediaType, fmt.Errorf("OpenAI content-type = %q, want application/json", got))
		return
	}
	defer r.Body.Close()
	var body map[string]any
	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(&body); err != nil {
		f.reject(w, http.StatusBadRequest, fmt.Errorf("decode OpenAI request: %w", err))
		return
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		f.reject(w, http.StatusBadRequest, fmt.Errorf("OpenAI request must contain one JSON object"))
		return
	}
	f.mu.Lock()
	modelDrivenTool := f.modelDrivenTool
	emptyFinalResponse := f.emptyFinalResponse
	requestNumber := len(f.chatCompletionBodies) + 1
	if err := validateOpenAIRequest(body, requestNumber, modelDrivenTool != ""); err != nil {
		f.mu.Unlock()
		f.reject(w, http.StatusBadRequest, err)
		return
	}
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
	if modelDrivenTool != "" {
		f.writeModelDrivenResponse(w, flusher, body, requestNumber, modelDrivenTool)
		return
	}
	if emptyFinalResponse {
		writeOpenAIChunk(w, "", "stop")
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
		if flusher != nil {
			flusher.Flush()
		}
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

func (f *fakeModelServer) reject(w http.ResponseWriter, statusCode int, err error) {
	f.handlerErrorOnce.Do(func() { f.handlerErrors <- err })
	http.Error(w, err.Error(), statusCode)
}

func validateOpenAIRequest(body map[string]any, requestNumber int, requireToolFlow bool) error {
	if got := body["model"]; got != "fake-model" {
		return fmt.Errorf("OpenAI model = %#v, want fake-model", got)
	}
	if got := body["stream"]; got != true {
		return fmt.Errorf("OpenAI stream = %#v, want true", got)
	}
	messages, ok := body["messages"].([]any)
	if !ok || len(messages) == 0 {
		return fmt.Errorf("OpenAI messages = %#v, want a non-empty array", body["messages"])
	}
	for index, raw := range messages {
		message, ok := raw.(map[string]any)
		if !ok {
			return fmt.Errorf("OpenAI message[%d] = %#v, want object", index, raw)
		}
		if role, ok := message["role"].(string); !ok || role == "" {
			return fmt.Errorf("OpenAI message[%d] role = %#v, want non-empty string", index, message["role"])
		}
	}

	rawTools, toolsPresent := body["tools"]
	tools, toolsAreArray := rawTools.([]any)
	if requireToolFlow && (!toolsPresent || !toolsAreArray || len(tools) != 3) {
		return fmt.Errorf("OpenAI tools = %#v, want two skill tools and one MCP tool", rawTools)
	}
	if toolsPresent && !toolsAreArray {
		return fmt.Errorf("OpenAI tools = %#v, want array", rawTools)
	}
	for index, raw := range tools {
		tool, ok := raw.(map[string]any)
		if !ok || tool["type"] != "function" {
			return fmt.Errorf("OpenAI tool[%d] = %#v, want function object", index, raw)
		}
		function, ok := tool["function"].(map[string]any)
		if !ok {
			return fmt.Errorf("OpenAI tool[%d] function = %#v, want object", index, tool["function"])
		}
		if name, ok := function["name"].(string); !ok || name == "" {
			return fmt.Errorf("OpenAI tool[%d] name = %#v, want non-empty string", index, function["name"])
		}
		if _, ok := function["description"].(string); !ok {
			return fmt.Errorf("OpenAI tool[%d] description = %#v, want string", index, function["description"])
		}
		if _, ok := function["parameters"].(map[string]any); !ok {
			return fmt.Errorf("OpenAI tool[%d] parameters = %#v, want object", index, function["parameters"])
		}
	}
	if !requireToolFlow {
		return nil
	}
	if !hasAdvertisedFunctionNamed(tools, "skills_list") || !hasAdvertisedFunctionNamed(tools, "skill_view") {
		return fmt.Errorf("OpenAI tools = %#v, want skills_list and skill_view", rawTools)
	}

	switch requestNumber {
	case 1:
		last, _ := messages[len(messages)-1].(map[string]any)
		if last["role"] != "user" {
			return fmt.Errorf("initial OpenAI final message role = %#v, want user", last["role"])
		}
	case 2:
		if len(messages) < 2 {
			return fmt.Errorf("follow-up OpenAI messages = %#v, want assistant and tool messages", messages)
		}
		assistant, _ := messages[len(messages)-2].(map[string]any)
		toolResult, _ := messages[len(messages)-1].(map[string]any)
		calls, callsOK := assistant["tool_calls"].([]any)
		content, contentPresent := assistant["content"]
		if assistant["role"] != "assistant" || !contentPresent || content != nil || !callsOK || len(calls) != 1 {
			return fmt.Errorf("follow-up OpenAI assistant message = %#v, want one tool call", assistant)
		}
		call, callOK := calls[0].(map[string]any)
		function, functionOK := call["function"].(map[string]any)
		_, idOK := call["id"].(string)
		_, argumentsOK := function["arguments"].(string)
		if !callOK || !functionOK || !idOK || call["type"] != "function" || !argumentsOK {
			return fmt.Errorf("follow-up OpenAI tool call = %#v, want serialized function call", calls[0])
		}
		_, resultIDOK := toolResult["tool_call_id"].(string)
		_, contentOK := toolResult["content"].(string)
		if toolResult["role"] != "tool" || !resultIDOK || !contentOK {
			return fmt.Errorf("follow-up OpenAI tool result = %#v, want linked tool content", toolResult)
		}
	default:
		return fmt.Errorf("OpenAI request count exceeded tool flow: got request %d", requestNumber)
	}
	return nil
}

func (f *fakeModelServer) writeModelDrivenResponse(w http.ResponseWriter, flusher http.Flusher, body map[string]any, requestNumber int, toolName string) {
	switch requestNumber {
	case 1:
		alias := advertisedFunctionAlias(body, toolName)
		if alias == "" {
			writeOpenAIChunk(w, "", "stop")
			return
		}
		firstArguments := `{"timezone":`
		secondArguments := `"UTC"}`
		if toolName == "files.create" {
			firstArguments = `{"content":"created by model",`
			secondArguments = `"path":"model-created.txt"}`
		}
		writeOpenAIToolCallChunk(w, alias, firstArguments, true)
		if flusher != nil {
			flusher.Flush()
		}
		writeOpenAIToolCallChunk(w, "", secondArguments, false)
		if flusher != nil {
			flusher.Flush()
		}
		writeOpenAIChunk(w, "", "tool_calls")
	case 2:
		finalText := "The fixed time is 2025-01-02T03:04:05Z."
		if toolName == "files.create" {
			finalText = "Created model-created.txt."
		}
		writeOpenAIChunk(w, finalText, "")
		if flusher != nil {
			flusher.Flush()
		}
		writeOpenAIChunk(w, "", "stop")
	default:
		writeOpenAIChunk(w, "", "stop")
	}
	_, _ = io.WriteString(w, "data: [DONE]\n\n")
	if flusher != nil {
		flusher.Flush()
	}
}

func advertisedFunctionAlias(body map[string]any, toolName string) string {
	tools, _ := body["tools"].([]any)
	wantDescription := map[string]string{
		"system.time":  "Get the current system time for a time zone.",
		"files.create": "Create a file.",
	}[toolName]
	for _, raw := range tools {
		tool, _ := raw.(map[string]any)
		function, _ := tool["function"].(map[string]any)
		if function["description"] == wantDescription {
			alias, _ := function["name"].(string)
			return alias
		}
	}
	return ""
}

func hasAdvertisedFunctionNamed(tools []any, name string) bool {
	for _, raw := range tools {
		tool, _ := raw.(map[string]any)
		function, _ := tool["function"].(map[string]any)
		if function["name"] == name {
			return true
		}
	}
	return false
}

func writeOpenAIToolCallChunk(w http.ResponseWriter, alias, arguments string, initial bool) {
	function := map[string]any{"arguments": arguments}
	call := map[string]any{"index": 0, "function": function}
	if initial {
		call["id"] = "provider_tool_call"
		call["type"] = "function"
		function["name"] = alias
	}
	data, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{
		"index": 0,
		"delta": map[string]any{"tool_calls": []any{call}},
	}}})
	_, _ = fmt.Fprintf(w, "data: %s\n\n", data)
}

func (f *fakeModelServer) enableModelDrivenToolCall() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.modelDrivenTool = "system.time"
}

func (f *fakeModelServer) enableModelDrivenFilesCreate() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.modelDrivenTool = "files.create"
}

func (f *fakeModelServer) disableModelDrivenToolCall() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.modelDrivenTool = ""
}

func (f *fakeModelServer) enableEmptyFinalResponse() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.emptyFinalResponse = true
}

func (f *fakeModelServer) resetBodies() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.chatCompletionBodies = nil
}

func (f *fakeModelServer) bodies() []map[string]any {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]map[string]any(nil), f.chatCompletionBodies...)
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
	fake := &fakeMCPServer{
		name:            name,
		token:           token,
		approvalTokens:  make(chan string, 4),
		createStarted:   make(chan struct{}),
		createCancelled: make(chan struct{}),
		handlerErrors:   make(chan error, 1),
	}
	fake.server = httptest.NewServer(http.HandlerFunc(fake.handle))
	return fake
}

func (f *fakeMCPServer) handle(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		f.reject(w, http.StatusMethodNotAllowed, fmt.Errorf("%s MCP method = %s, want POST", f.name, r.Method))
		return
	}
	if got, want := r.Header.Get("authorization"), "Bearer "+f.token; got != want {
		f.reject(w, http.StatusUnauthorized, fmt.Errorf("%s MCP authorization = %q, want %q", f.name, got, want))
		return
	}
	if got := r.Header.Get("content-type"); got != "application/json" {
		f.reject(w, http.StatusUnsupportedMediaType, fmt.Errorf("%s MCP content-type = %q, want application/json", f.name, got))
		return
	}
	defer r.Body.Close()
	var req struct {
		JSONRPC string         `json:"jsonrpc"`
		ID      any            `json:"id"`
		Method  string         `json:"method"`
		Params  map[string]any `json:"params"`
	}
	decoder := json.NewDecoder(r.Body)
	decoder.UseNumber()
	if err := decoder.Decode(&req); err != nil {
		f.reject(w, http.StatusBadRequest, fmt.Errorf("decode %s MCP request: %w", f.name, err))
		return
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		f.reject(w, http.StatusBadRequest, fmt.Errorf("%s MCP request must contain one JSON object", f.name))
		return
	}
	if req.JSONRPC != "2.0" {
		f.reject(w, http.StatusBadRequest, fmt.Errorf("%s MCP jsonrpc = %q, want 2.0", f.name, req.JSONRPC))
		return
	}
	requestID, ok := req.ID.(json.Number)
	if !ok {
		f.reject(w, http.StatusBadRequest, fmt.Errorf("%s MCP request ID = %#v, want integer", f.name, req.ID))
		return
	}
	requestIDValue, err := requestID.Int64()
	if err != nil || requestIDValue < 1 {
		f.reject(w, http.StatusBadRequest, fmt.Errorf("%s MCP request ID = %q, want positive integer", f.name, requestID))
		return
	}
	if err := validateMCPParams(req.Method, req.Params); err != nil {
		f.reject(w, http.StatusBadRequest, fmt.Errorf("%s MCP: %w", f.name, err))
		return
	}
	f.mu.Lock()
	f.requests = append(f.requests, fakeMCPRequest{method: req.Method, params: req.Params, id: requestID})
	advertiseTime := f.advertiseTime
	advertiseCreate := f.advertiseCreate
	validateApproval := f.validateApproval
	blockCreate := f.blockCreate
	f.mu.Unlock()
	if req.Method == "tools/list" {
		tools := []any{}
		if advertiseTime && f.name == "system" {
			tools = append(tools, map[string]any{
				"name":        "system.time",
				"description": "Get the current system time for a time zone.",
				"inputSchema": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"timezone": map[string]any{"type": "string"},
					},
				},
			})
		}
		if advertiseCreate && f.name == "files" {
			tools = append(tools, map[string]any{
				"name":        "files.create",
				"description": "Create a file.",
				"inputSchema": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"path":    map[string]any{"type": "string"},
						"content": map[string]any{"type": "string"},
					},
				},
			})
		}
		writeJSONRPCResult(w, requestID, map[string]any{"tools": tools})
		return
	}
	toolName, _ := req.Params["name"].(string)
	args, _ := req.Params["arguments"].(map[string]any)
	meta, _ := req.Params["_meta"].(map[string]any)
	switch toolName {
	case "system.time":
		writeJSONRPCResult(w, requestID, map[string]any{
			"iso":      "2025-01-02T03:04:05Z",
			"unixMs":   int64(1735787045000),
			"timezone": "UTC",
		})
	case "files.create":
		approvalToken, _ := meta["approvalToken"].(string)
		if approvalToken == "" {
			writeJSONRPCError(w, requestID, "approval token required")
			return
		}
		provenanceToken, _ := meta["provenanceToken"].(string)
		if validateApproval {
			if len(meta) != 2 || provenanceToken == "" {
				f.reject(w, http.StatusBadRequest, fmt.Errorf("files MCP _meta = %#v, want an approval token and a provenance capability", meta))
				return
			}
			if blockCreate {
				select {
				case <-f.createStarted:
				default:
					close(f.createStarted)
				}
				<-r.Context().Done()
				select {
				case <-f.createCancelled:
				default:
					close(f.createCancelled)
				}
				return
			}
			approvalID, err := validateIntegrationApprovalToken(approvalToken, args)
			if err != nil {
				f.reject(w, http.StatusBadRequest, fmt.Errorf("files MCP approval token: %w", err))
				return
			}
			provenance, err := integrationProvenanceClaims(provenanceToken, args)
			if err != nil {
				f.reject(w, http.StatusBadRequest, fmt.Errorf("files MCP provenance capability: %w", err))
				return
			}
			ctx := metadata.AppendToOutgoingContext(r.Context(), "authorization", "Bearer "+integrationInternalToken)
			consumed, err := f.approvalClient.ConsumeApproval(ctx, &turingv1.ConsumeApprovalRequest{
				ApprovalId:      approvalID,
				ProvenanceToken: provenanceToken,
				PhysicalPath:    provenance.ownedPath(),
			})
			if err != nil || consumed.GetStatus() != turingv1.ApprovalStatus_APPROVAL_STATUS_CONSUMED {
				f.reject(w, http.StatusBadRequest, fmt.Errorf("files MCP consume approval %q: response=%v error=%v", approvalID, consumed, err))
				return
			}
			reservation := consumed.GetReservation()
			if reservation.GetArtifactId() == "" || reservation.GetPhysicalPath() != provenance.ownedPath() {
				f.reject(w, http.StatusBadRequest, fmt.Errorf("files MCP artifact reservation = %v, want the run-scoped path %q", reservation, provenance.ownedPath()))
				return
			}
			if _, err := f.approvalClient.FinalizeSandboxArtifact(ctx, &turingv1.FinalizeSandboxArtifactRequest{
				ArtifactId:      reservation.GetArtifactId(),
				ProvenanceToken: provenanceToken,
				Committed:       true,
			}); err != nil {
				f.reject(w, http.StatusBadRequest, fmt.Errorf("files MCP finalize artifact %q: %w", reservation.GetArtifactId(), err))
				return
			}
		}
		select {
		case f.approvalTokens <- approvalToken:
		default:
		}
		path, _ := args["path"].(string)
		writeJSONRPCResult(w, requestID, map[string]any{"path": path, "created": true, "content": "created through approval flow"})
	default:
		f.reject(w, http.StatusBadRequest, fmt.Errorf("%s MCP unexpected tool %q", f.name, toolName))
	}
}

func (f *fakeMCPServer) reject(w http.ResponseWriter, statusCode int, err error) {
	f.handlerErrorOnce.Do(func() { f.handlerErrors <- err })
	http.Error(w, err.Error(), statusCode)
}

func validateMCPParams(method string, params map[string]any) error {
	if params == nil {
		return fmt.Errorf("%s params = nil, want object", method)
	}
	switch method {
	case "tools/list":
		if len(params) != 0 {
			return fmt.Errorf("tools/list params = %#v, want empty object", params)
		}
	case "tools/call":
		name, nameOK := params["name"].(string)
		if !nameOK || name == "" {
			return fmt.Errorf("tools/call name = %#v, want non-empty string", params["name"])
		}
		if _, ok := params["arguments"].(map[string]any); !ok {
			return fmt.Errorf("tools/call arguments = %#v, want object", params["arguments"])
		}
		if meta, present := params["_meta"]; present {
			if _, ok := meta.(map[string]any); !ok {
				return fmt.Errorf("tools/call _meta = %#v, want object", meta)
			}
		}
		for key := range params {
			if key != "name" && key != "arguments" && key != "_meta" {
				return fmt.Errorf("tools/call unexpected param %q", key)
			}
		}
	default:
		return fmt.Errorf("method = %q, want tools/list or tools/call", method)
	}
	return nil
}

func (f *fakeMCPServer) enableTimeTool() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.advertiseTime = true
}

func (f *fakeMCPServer) enableCreateToolWithApprovalValidation() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.advertiseCreate = true
	f.validateApproval = true
}

func (f *fakeMCPServer) blockCreateCallUntilCancelled() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.blockCreate = true
}

// integrationProvenance is what the fake mcp-files reads out of the capability
// the orchestrator issued: the session and run that own the write, and the path
// it is scoped to. The real server derives the same physical path from it.
type integrationProvenance struct {
	sessionID   string
	runID       string
	logicalPath string
}

func (p integrationProvenance) ownedPath() string {
	return "sessions/" + p.sessionID + "/runs/" + p.runID + "/files/" + p.logicalPath
}

func integrationProvenanceClaims(token string, args map[string]any) (integrationProvenance, error) {
	claims, err := verifyIntegrationJWT(token)
	if err != nil {
		return integrationProvenance{}, err
	}
	canonicalArgs, err := json.Marshal(args)
	if err != nil {
		return integrationProvenance{}, err
	}
	argsHash := sha256.Sum256(canonicalArgs)
	if claims["kind"] != "provenance" ||
		claims["aud"] != "mcp-files" ||
		claims["sub"] != "general_assistant" ||
		claims["tool"] != "files.create" ||
		claims["args_hash"] != "sha256:"+hex.EncodeToString(argsHash[:]) {
		return integrationProvenance{}, fmt.Errorf("unexpected provenance claims: %#v", claims)
	}
	sessionID, _ := claims["sid"].(string)
	runID, _ := claims["rid"].(string)
	logicalPath, _ := claims["path"].(string)
	if sessionID == "" || runID == "" || logicalPath == "" {
		return integrationProvenance{}, fmt.Errorf("provenance capability lacks a session, run or path scope: %#v", claims)
	}
	return integrationProvenance{sessionID: sessionID, runID: runID, logicalPath: logicalPath}, nil
}

func verifyIntegrationJWT(token string) (map[string]any, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return nil, errors.New("invalid JWT shape")
	}
	headerJSON, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return nil, err
	}
	var header map[string]any
	if err := json.Unmarshal(headerJSON, &header); err != nil || header["alg"] != "HS256" {
		return nil, fmt.Errorf("invalid JWT header: %#v: %v", header, err)
	}
	mac := hmac.New(sha256.New, []byte(integrationApprovalKey))
	_, _ = mac.Write([]byte(parts[0] + "." + parts[1]))
	signature, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil || !hmac.Equal(signature, mac.Sum(nil)) {
		return nil, errors.New("invalid JWT signature")
	}
	payloadJSON, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, err
	}
	var claims map[string]any
	if err := json.Unmarshal(payloadJSON, &claims); err != nil {
		return nil, err
	}
	return claims, nil
}

func validateIntegrationApprovalToken(token string, args map[string]any) (string, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return "", errors.New("invalid JWT shape")
	}
	headerJSON, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return "", err
	}
	var header map[string]any
	if err := json.Unmarshal(headerJSON, &header); err != nil || header["alg"] != "HS256" {
		return "", fmt.Errorf("invalid JWT header: %#v: %v", header, err)
	}
	mac := hmac.New(sha256.New, []byte(integrationApprovalKey))
	_, _ = mac.Write([]byte(parts[0] + "." + parts[1]))
	signature, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil || !hmac.Equal(signature, mac.Sum(nil)) {
		return "", errors.New("invalid JWT signature")
	}
	payloadJSON, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return "", err
	}
	var claims map[string]any
	if err := json.Unmarshal(payloadJSON, &claims); err != nil {
		return "", err
	}
	canonicalArgs, err := json.Marshal(args)
	if err != nil {
		return "", err
	}
	argsHash := sha256.Sum256(canonicalArgs)
	wantArgsHash := "sha256:" + hex.EncodeToString(argsHash[:])
	if claims["iss"] != "turing.orchestrator" ||
		claims["sub"] != "general_assistant" ||
		claims["aud"] != "mcp-files" ||
		claims["tool"] != "files.create" ||
		claims["args_hash"] != wantArgsHash {
		return "", fmt.Errorf("unexpected JWT claims: %#v", claims)
	}
	approvalID, _ := claims["jti"].(string)
	if approvalID == "" {
		return "", errors.New("JWT jti is empty")
	}
	return approvalID, nil
}

func (f *fakeMCPServer) recordedRequests() []fakeMCPRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]fakeMCPRequest(nil), f.requests...)
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
	if completed := messageCompletedContent(t, events); completed != "Hello" {
		t.Fatalf("message completed content = %q, want Hello", completed)
	}
	if !hasRunCompleted(events) {
		t.Fatal("stream did not include run_completed")
	}
}

func TestDiscoveredToolsAppearInListTools(t *testing.T) {
	harness := newGRPCHarness(t)
	defer harness.close()

	internalCtx, cancelInternal := context.WithTimeout(
		metadata.AppendToOutgoingContext(context.Background(), "authorization", "Bearer "+integrationInternalToken),
		5*time.Second,
	)
	defer cancelInternal()
	stream, err := turingv1.NewRuntimeServiceClient(harness.internalConn).ConnectWorker(internalCtx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = stream.CloseSend() }()
	objectSchema, err := structpb.NewStruct(map[string]any{"type": "object"})
	if err != nil {
		t.Fatal(err)
	}
	if err := stream.Send(&turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_WorkerReady{WorkerReady: &turingv1.RuntimeWorkerReady{
		WorkerId:          "worker-discovery-e2e",
		AgentId:           turingv1.AgentId_AGENT_ID_GENERAL_ASSISTANT,
		MaxConcurrentRuns: 1,
		Tools: []*turingv1.DiscoveredTool{
			{ServerName: "system", ToolName: "system.time", Schema: objectSchema},
			{ServerName: "files", ToolName: "files.create", Schema: objectSchema},
			{ServerName: "custom", ToolName: "custom.inspect", Schema: objectSchema},
		},
	}}}); err != nil {
		t.Fatal(err)
	}
	accepted, err := stream.Recv()
	if err != nil {
		t.Fatal(err)
	}
	if accepted.GetWorkerAccepted() == nil {
		t.Fatalf("first runtime command = %+v, want worker_accepted", accepted)
	}

	listed, err := harness.sessions.ListTools(harness.clientContext(), &turingv1.ListToolsRequest{})
	if err != nil {
		t.Fatal(err)
	}
	got := make(map[string]turingv1.ToolPolicy, len(listed.Tools))
	for _, tool := range listed.Tools {
		got[tool.ServerName+"/"+tool.ToolName] = tool.Policy
	}
	want := map[string]turingv1.ToolPolicy{
		"custom/custom.inspect": turingv1.ToolPolicy_TOOL_POLICY_APPROVAL_REQUIRED,
		"files/files.create":    turingv1.ToolPolicy_TOOL_POLICY_APPROVAL_REQUIRED,
		"skills/skill_view":     turingv1.ToolPolicy_TOOL_POLICY_APPROVAL_REQUIRED,
		"skills/skills_list":    turingv1.ToolPolicy_TOOL_POLICY_APPROVAL_REQUIRED,
		"system/system.time":    turingv1.ToolPolicy_TOOL_POLICY_SAFE,
	}
	if len(got) != len(want) {
		t.Fatalf("ListTools = %+v, want exactly %+v", got, want)
	}
	for name, policy := range want {
		if got[name] != policy {
			t.Fatalf("ListTools[%q] = %v, want %v", name, got[name], policy)
		}
	}
}

func TestQueuedTurnsUseCausalModelHistory(t *testing.T) {
	harness := newGRPCHarness(t)
	defer harness.close()

	sessionID := harness.createSession(t, "causal model history")
	queue := func(content string) (turingv1.ChatService_SendMessageClient, context.CancelFunc) {
		ctx, cancel := context.WithTimeout(harness.clientContext(), 15*time.Second)
		stream, err := harness.chat.SendMessage(ctx, &turingv1.SendMessageRequest{
			SessionId:     sessionID,
			Content:       content,
			ContentType:   "text",
			AgentId:       turingv1.AgentId_AGENT_ID_GENERAL_ASSISTANT,
			ModelProvider: turingv1.ModelProvider_MODEL_PROVIDER_OPENAI_COMPATIBLE,
			Model:         "fake-model",
		})
		if err != nil {
			cancel()
			t.Fatal(err)
		}
		event, err := stream.Recv()
		if err != nil {
			cancel()
			t.Fatal(err)
		}
		if queued := event.GetRunQueued(); queued == nil || queued.GetRunId() == "" {
			cancel()
			t.Fatalf("first queued event = %+v, want run_queued", event)
		}
		return stream, cancel
	}
	firstStream, cancelFirst := queue("turn one")
	defer cancelFirst()
	secondStream, cancelSecond := queue("turn two")
	defer cancelSecond()

	readTerminal := func(stream turingv1.ChatService_SendMessageClient) {
		for {
			event, err := stream.Recv()
			if err != nil {
				t.Fatal(err)
			}
			if event.GetRunCompleted() != nil {
				return
			}
			if event.GetRunFailed() != nil || event.GetRunCancelled() != nil {
				t.Fatalf("queued turn terminal event = %+v, want completion", event)
			}
		}
	}
	readTerminal(firstStream)
	readTerminal(secondStream)

	bodies := harness.fakeModel.bodies()
	if len(bodies) != 2 {
		t.Fatalf("OpenAI request count = %d, want 2", len(bodies))
	}
	assertMessages := func(body map[string]any, want []map[string]any) {
		raw, ok := body["messages"].([]any)
		if !ok {
			t.Fatalf("OpenAI messages = %#v, want array", body["messages"])
		}
		got := make([]map[string]any, 0, len(raw))
		for _, entry := range raw {
			message, ok := entry.(map[string]any)
			if !ok {
				t.Fatalf("OpenAI message = %#v, want object", entry)
			}
			got = append(got, map[string]any{"role": message["role"], "content": message["content"]})
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("OpenAI messages = %#v, want %#v", got, want)
		}
	}
	assertMessages(bodies[0], []map[string]any{
		{"role": "user", "content": "turn one"},
	})
	assertMessages(bodies[1], []map[string]any{
		{"role": "user", "content": "turn one"},
		{"role": "assistant", "content": "Hello"},
		{"role": "user", "content": "turn two"},
	})
}

func TestApprovalPersistenceFailureFencesRealWorkerUntilExecutorExit(t *testing.T) {
	harness := newGRPCHarness(t, withoutRuntimeWorker())
	defer harness.close()
	database, err := sql.Open("sqlite3", harness.databasePath)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if _, err := database.Exec(`
		CREATE TRIGGER fail_real_worker_approval_requested
		BEFORE INSERT ON events
		WHEN NEW.type = 'approval.requested'
		BEGIN
			SELECT RAISE(ABORT, 'approval requested persistence failed');
		END
	`); err != nil {
		t.Fatal(err)
	}

	queue := func(sessionID, content string) (string, context.CancelFunc) {
		ctx, cancel := context.WithCancel(harness.clientContext())
		stream, streamErr := harness.chat.SendMessage(ctx, &turingv1.SendMessageRequest{
			SessionId:     sessionID,
			Content:       content,
			ContentType:   "text",
			AgentId:       turingv1.AgentId_AGENT_ID_GENERAL_ASSISTANT,
			ModelProvider: turingv1.ModelProvider_MODEL_PROVIDER_OPENAI_COMPATIBLE,
			Model:         "fake-model",
		})
		if streamErr != nil {
			cancel()
			t.Fatal(streamErr)
		}
		event, streamErr := stream.Recv()
		if streamErr != nil {
			cancel()
			t.Fatal(streamErr)
		}
		if queued := event.GetRunQueued(); queued != nil && queued.GetRunId() != "" {
			return queued.GetRunId(), cancel
		}
		cancel()
		t.Fatalf("queued event = %+v, want run_queued", event)
		return "", nil
	}
	firstExecutor := &terminalDecisionBlockingExecutor{
		started:      make(chan struct{}),
		decisionSeen: make(chan error, 1),
		release:      make(chan struct{}),
		exited:       make(chan struct{}),
		afterStarted: make(chan string, 2),
	}
	startWorker := func(workerID string, executor runtimetestkit.WorkerExecutor, discoveredTools ...*turingv1.DiscoveredTool) (context.CancelFunc, <-chan error, *grpc.ClientConn) {
		ctx, cancel := context.WithCancel(context.Background())
		conn := dialBufconn(t, harness.internalLis)
		done := make(chan error, 1)
		go func() {
			done <- runtimetestkit.RunWorkerWithExecutor(ctx, runtimetestkit.WorkerConfig{
				Conn:               conn,
				InternalToken:      integrationInternalToken,
				WorkerID:           workerID,
				MaxConcurrentRuns:  1,
				TotalToolTimeout:   time.Second,
				MaxToolCallsPerRun: 1,
				OpenAIModel:        "fake-model",
				DiscoveredTools:    discoveredTools,
			}, executor)
		}()
		harness.waitForRuntimeWorker(t)
		t.Cleanup(func() {
			cancel()
			_ = conn.Close()
			select {
			case <-done:
			case <-time.After(2 * time.Second):
				t.Errorf("worker %q did not stop", workerID)
			}
		})
		return cancel, done, conn
	}
	toolSchema, err := structpb.NewStruct(map[string]any{"type": "object"})
	if err != nil {
		t.Fatal(err)
	}
	_, _, _ = startWorker("worker-approval-failure-real", firstExecutor, &turingv1.DiscoveredTool{
		ServerName: "files", ToolName: "files.update", Schema: toolSchema,
	})
	sessionID := harness.createSession(t, "real worker approval failure")
	firstRunID, cancelFirst := queue(sessionID, "first approval")
	defer cancelFirst()
	secondRunID, cancelSecond := queue(sessionID, "same-session follow-up")
	defer cancelSecond()
	globalSessionID := harness.createSession(t, "global capacity follow-up")
	globalRunID, cancelGlobal := queue(globalSessionID, "global follow-up")
	defer cancelGlobal()
	select {
	case <-firstExecutor.started:
	case <-time.After(2 * time.Second):
		t.Fatal("first real worker executor did not start")
	}
	if firstExecutor.firstRunID != firstRunID {
		t.Fatalf("first executor run = %q, want %q", firstExecutor.firstRunID, firstRunID)
	}
	select {
	case decisionErr := <-firstExecutor.decisionSeen:
		if decisionErr != nil {
			t.Fatal(decisionErr)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("first real worker did not receive terminal approval decision")
	}

	_, _, _ = startWorker("worker-approval-failure-waiter", passiveExecutor{started: firstExecutor.afterStarted})
	time.Sleep(100 * time.Millisecond)
	for _, runID := range []string{secondRunID, globalRunID} {
		run, getErr := harness.repo.GetRun(context.Background(), runID)
		if getErr != nil {
			t.Fatal(getErr)
		}
		if run.Status != "queued" {
			t.Fatalf("run %q status = %q, want queued before first executor exits", runID, run.Status)
		}
	}
	select {
	case runID := <-firstExecutor.afterStarted:
		t.Fatalf("later run %q started before first executor exited", runID)
	default:
	}

	close(firstExecutor.release)
	select {
	case <-firstExecutor.exited:
	case <-time.After(2 * time.Second):
		t.Fatal("first real worker executor did not exit")
	}
	select {
	case runID := <-firstExecutor.afterStarted:
		if runID != secondRunID {
			t.Fatalf("first released assignment = %q, want same-session run %q", runID, secondRunID)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("capacity did not release after first executor exit")
	}
	global, err := harness.repo.GetRun(context.Background(), globalRunID)
	if err != nil {
		t.Fatal(err)
	}
	if global.Status != "queued" {
		t.Fatalf("global run status = %q, want queued behind the released same-session run", global.Status)
	}
}

type terminalDecisionBlockingExecutor struct {
	firstRunID   string
	poster       func(context.Context, *turingv1.ToolCallBeacon) (*turingv1.ToolPolicyDecision, error)
	started      chan struct{}
	decisionSeen chan error
	release      chan struct{}
	exited       chan struct{}
	afterStarted chan string
}

func (e *terminalDecisionBlockingExecutor) SetToolBeaconPoster(post func(context.Context, *turingv1.ToolCallBeacon) (*turingv1.ToolPolicyDecision, error)) {
	e.poster = post
}

func (e *terminalDecisionBlockingExecutor) Execute(ctx context.Context, job *turingv1.AgentJob, _ func(*turingv1.RuntimeUpdate) error) error {
	if e.firstRunID == "" {
		e.firstRunID = job.GetRunId()
	}
	if job.GetRunId() != e.firstRunID {
		select {
		case e.afterStarted <- job.GetRunId():
		case <-ctx.Done():
			return ctx.Err()
		}
		<-ctx.Done()
		return ctx.Err()
	}
	if e.poster == nil {
		return errors.New("tool beacon poster was not configured")
	}
	args, err := structpb.NewStruct(map[string]any{"path": "note.txt", "content": "hello"})
	if err != nil {
		return err
	}
	close(e.started)
	decision, err := e.poster(ctx, &turingv1.ToolCallBeacon{
		RunId:      job.GetRunId(),
		TraceId:    job.GetTraceId(),
		ToolCallId: "call_real_worker_terminal",
		AgentId:    turingv1.AgentId_AGENT_ID_GENERAL_ASSISTANT,
		ServerName: "files",
		ToolName:   "files.update",
		Phase:      turingv1.ToolCallPhase_TOOL_CALL_PHASE_BEFORE,
		Args:       args,
	})
	if err == nil && (decision == nil || !decision.GetTerminalRun()) {
		err = errors.New("approval persistence failure did not return a terminal decision")
	}
	e.decisionSeen <- err
	if err != nil {
		return err
	}
	select {
	case <-e.release:
		close(e.exited)
		return terminalWorkerExitError{}
	case <-ctx.Done():
		return ctx.Err()
	}
}

type terminalWorkerExitError struct{}

func (terminalWorkerExitError) Error() string     { return "terminalized by orchestrator" }
func (terminalWorkerExitError) RunTerminal() bool { return true }

type passiveExecutor struct {
	started chan<- string
}

func (e passiveExecutor) Execute(ctx context.Context, job *turingv1.AgentJob, _ func(*turingv1.RuntimeUpdate) error) error {
	select {
	case e.started <- job.GetRunId():
	case <-ctx.Done():
		return ctx.Err()
	}
	<-ctx.Done()
	return ctx.Err()
}

func TestEmptyFinalModelResponsePersistsFallback(t *testing.T) {
	const fallback = "The model returned an empty response."
	harness := newGRPCHarness(t)
	defer harness.close()
	harness.fakeModel.enableEmptyFinalResponse()

	sessionID := harness.createSession(t, "empty final response")
	events := harness.sendMessageToCompletion(t, sessionID, "answer without content")

	assertNoFakeHandlerErrors(t, harness.fakeModel, harness.systemMCP, harness.filesMCP)
	assertTokenDeltas(t, events, []string{fallback})
	if got := messageCompletedContent(t, events); got != fallback {
		t.Fatalf("message.completed content = %q, want %q", got, fallback)
	}
	if got := runCompletedPersistedContent(t, harness, sessionID, events); got != fallback {
		t.Fatalf("persisted completion content = %q, want %q", got, fallback)
	}
}

func TestModelDrivenToolCallCompletesRun(t *testing.T) {
	const (
		priorUserText      = "Remember this previous turn."
		priorAssistantText = "Hello"
		userText           = "What time is it in UTC?"
		finalText          = "The fixed time is 2025-01-02T03:04:05Z."
	)
	harness := newGRPCHarness(t, withSystemTimeTool())
	defer harness.close()

	sessionID := harness.createSession(t, "model-driven tool call")
	priorEvents := harness.sendMessageToCompletion(t, sessionID, priorUserText)
	if got := messageCompletedContent(t, priorEvents); got != priorAssistantText {
		t.Fatalf("prior message.completed content = %q, want %q", got, priorAssistantText)
	}
	harness.fakeModel.resetBodies()
	harness.fakeModel.enableModelDrivenToolCall()

	streamEvents := harness.sendMessageToCompletion(t, sessionID, userText)
	assertNoFakeHandlerErrors(t, harness.fakeModel, harness.systemMCP, harness.filesMCP)
	runID := completedRunID(t, streamEvents)
	toolCallID := assertStreamedToolLifecycle(t, streamEvents, "system.time", runID, finalText)
	listContext, cancelList := context.WithTimeout(harness.clientContext(), 5*time.Second)
	defer cancelList()
	listed, err := harness.events.ListEvents(listContext, &turingv1.ListEventsRequest{
		SessionId: sessionID,
		Limit:     100,
	})
	if err != nil {
		t.Fatalf("ListEvents: %v", err)
	}

	var started, completed, messageCompleted []*turingv1.TuringEvent
	for _, event := range listed.Events {
		if event.RunId != runID {
			continue
		}
		switch event.Type {
		case turingv1.TuringEventType_TURING_EVENT_TYPE_TOOL_CALL_STARTED:
			started = append(started, event)
		case turingv1.TuringEventType_TURING_EVENT_TYPE_TOOL_CALL_COMPLETED:
			completed = append(completed, event)
		case turingv1.TuringEventType_TURING_EVENT_TYPE_MESSAGE_COMPLETED:
			messageCompleted = append(messageCompleted, event)
		case turingv1.TuringEventType_TURING_EVENT_TYPE_AGENT_RUN_FAILED,
			turingv1.TuringEventType_TURING_EVENT_TYPE_TOOL_CALL_FAILED,
			turingv1.TuringEventType_TURING_EVENT_TYPE_TOOL_CALL_DENIED,
			turingv1.TuringEventType_TURING_EVENT_TYPE_APPROVAL_DENIED:
			t.Fatalf("unexpected failed or denied event: %s", event.Type)
		}
	}
	if len(started) != 1 || len(completed) != 1 {
		t.Fatalf("tool lifecycle counts: started=%d completed=%d, want 1 each", len(started), len(completed))
	}
	if len(messageCompleted) != 1 {
		t.Fatalf("persisted message.completed count = %d, want 1", len(messageCompleted))
	}
	if started[0].Sequence >= completed[0].Sequence {
		t.Fatalf("tool lifecycle sequences: started=%d completed=%d, want STARTED before COMPLETED", started[0].Sequence, completed[0].Sequence)
	}
	if completed[0].Sequence >= messageCompleted[0].Sequence {
		t.Fatalf("completion sequences: tool=%d message=%d, want tool before message", completed[0].Sequence, messageCompleted[0].Sequence)
	}
	if started[0].RunId != runID || completed[0].RunId != runID {
		t.Fatalf("tool lifecycle run IDs: started=%q completed=%q, want %q", started[0].RunId, completed[0].RunId, runID)
	}
	if got := stringField(started[0].Payload, "toolCallId"); got != toolCallID {
		t.Fatalf("started toolCallId = %q, want streamed opaque ID %q", got, toolCallID)
	}
	if got := stringField(completed[0].Payload, "toolCallId"); got != toolCallID {
		t.Fatalf("completed toolCallId = %q, want streamed opaque ID %q", got, toolCallID)
	}
	if startedName, completedName := stringField(started[0].Payload, "toolName"), stringField(completed[0].Payload, "toolName"); startedName != "system.time" || completedName != startedName {
		t.Fatalf("tool lifecycle names: started=%q completed=%q, want system.time for both", startedName, completedName)
	}
	if got := messageCompletedContent(t, streamEvents); got != finalText {
		t.Fatalf("stream message.completed content = %q, want %q", got, finalText)
	}
	if got := stringField(messageCompleted[0].Payload, "content"); got != finalText {
		t.Fatalf("persisted message.completed content = %q, want %q", got, finalText)
	}
	if got := runCompletedPersistedContent(t, harness, sessionID, streamEvents); got != finalText {
		t.Fatalf("run completed content = %q, want %q", got, finalText)
	}

	modelBodies := harness.fakeModel.bodies()
	if len(modelBodies) != 2 {
		t.Fatalf("OpenAI request count = %d, want 2", len(modelBodies))
	}
	alias := assertInitialOpenAIRequest(t, modelBodies[0], priorUserText, priorAssistantText, userText)
	modelLinkageID := modelToolCallID(t, modelBodies[1])
	if modelLinkageID == toolCallID {
		t.Fatalf("model linkage ID %q unexpectedly reused beacon lifecycle ID", modelLinkageID)
	}
	assertFollowupOpenAIRequest(t, modelBodies[1], priorUserText, priorAssistantText, userText, alias, modelLinkageID)
	assertModelDrivenMCPRequests(t, harness.systemMCP.recordedRequests(), harness.filesMCP.recordedRequests())
}

func TestModelDrivenFilesCreateCompletesApprovalFlow(t *testing.T) {
	const (
		userText  = "Create model-created.txt."
		finalText = "Created model-created.txt."
	)
	wantArgs := map[string]any{"path": "model-created.txt", "content": "created by model"}
	harness := newGRPCHarness(t, withFilesCreateTool())
	defer harness.close()
	harness.fakeModel.enableModelDrivenFilesCreate()

	sessionID := harness.createSession(t, "model-driven files approval")
	ctx, cancel := context.WithTimeout(harness.clientContext(), 15*time.Second)
	defer cancel()
	stream, err := harness.chat.SendMessage(ctx, &turingv1.SendMessageRequest{
		SessionId:     sessionID,
		Content:       userText,
		ContentType:   "text",
		AgentId:       turingv1.AgentId_AGENT_ID_GENERAL_ASSISTANT,
		ModelProvider: turingv1.ModelProvider_MODEL_PROVIDER_OPENAI_COMPATIBLE,
		Model:         "fake-model",
	})
	if err != nil {
		t.Fatal(err)
	}

	var events []*turingv1.ChatStreamEvent
	approvalID := ""
	for {
		event, err := stream.Recv()
		if err != nil {
			t.Fatal(err)
		}
		events = append(events, event)
		persisted := event.GetPersistedEvent()
		if persisted != nil && persisted.Type == turingv1.TuringEventType_TURING_EVENT_TYPE_APPROVAL_REQUESTED {
			approvalID = stringField(persisted.Payload, "approvalId")
			if approvalID == "" {
				t.Fatal("approval.requested missing approvalId")
			}
			approved, err := harness.approvals.ApproveApproval(
				harness.clientContext(),
				&turingv1.ApproveApprovalRequest{ApprovalId: approvalID},
			)
			if err != nil || approved.GetStatus() != turingv1.ApprovalStatus_APPROVAL_STATUS_APPROVED {
				t.Fatalf("ApproveApproval = %+v, %v", approved, err)
			}
		}
		if event.GetRunCompleted() != nil {
			break
		}
	}

	assertNoFakeHandlerErrors(t, harness.fakeModel, harness.systemMCP, harness.filesMCP)
	if approvalID == "" {
		t.Fatal("approval was not requested")
	}
	runID := completedRunID(t, events)
	beaconID := assertStreamedApprovalToolLifecycle(t, events, "files.create", runID, approvalID, finalText)
	assertPersistedTypes(t, events,
		turingv1.TuringEventType_TURING_EVENT_TYPE_TOOL_CALL_STARTED,
		turingv1.TuringEventType_TURING_EVENT_TYPE_APPROVAL_REQUESTED,
		turingv1.TuringEventType_TURING_EVENT_TYPE_APPROVAL_APPROVED,
		turingv1.TuringEventType_TURING_EVENT_TYPE_APPROVAL_CONSUMED,
		turingv1.TuringEventType_TURING_EVENT_TYPE_TOOL_CALL_COMPLETED,
	)
	if got := messageCompletedContent(t, events); got != finalText {
		t.Fatalf("message.completed content = %q, want %q", got, finalText)
	}
	if got := runCompletedPersistedContent(t, harness, sessionID, events); got != finalText {
		t.Fatalf("persisted completion content = %q, want %q", got, finalText)
	}

	modelBodies := harness.fakeModel.bodies()
	if len(modelBodies) != 2 {
		t.Fatalf("OpenAI request count = %d, want 2", len(modelBodies))
	}
	alias := assertInitialFilesOpenAIRequest(t, modelBodies[0], userText)
	modelID := modelToolCallID(t, modelBodies[1])
	if modelID == beaconID {
		t.Fatalf("model linkage ID %q unexpectedly reused beacon ID", modelID)
	}
	assertFollowupFilesOpenAIRequest(t, modelBodies[1], userText, alias, modelID, wantArgs)

	systemRequests := harness.systemMCP.recordedRequests()
	filesRequests := harness.filesMCP.recordedRequests()
	if len(systemRequests) != 1 || systemRequests[0].method != "tools/list" {
		t.Fatalf("system MCP requests = %#v, want only tools/list", systemRequests)
	}
	if len(filesRequests) != 2 || filesRequests[0].method != "tools/list" || filesRequests[1].method != "tools/call" {
		t.Fatalf("files MCP requests = %#v, want tools/list then tools/call", filesRequests)
	}
	if filesRequests[1].params["name"] != "files.create" || !reflect.DeepEqual(filesRequests[1].params["arguments"], wantArgs) {
		t.Fatalf("files MCP call params = %#v", filesRequests[1].params)
	}
	meta, _ := filesRequests[1].params["_meta"].(map[string]any)
	token, _ := meta["approvalToken"].(string)
	if token == "" {
		t.Fatal("files MCP call has empty approval token")
	}
	select {
	case received := <-harness.filesMCP.approvalTokens:
		if received != token {
			t.Fatalf("files MCP token = %q, recorded meta token = %q", received, token)
		}
	default:
		t.Fatal("files MCP did not record approval token")
	}
}
func TestApprovalRequiredToolFlow(t *testing.T) {
	harness := newGRPCHarness(t, withFilesCreateTool())
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
			assertNoFakeHandlerErrors(t, harness.fakeModel, harness.systemMCP, harness.filesMCP)
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
	if completed := messageCompletedContent(t, got); completed == "" {
		t.Fatal("tool flow did not complete assistant message")
	}
}

func TestTerminalApprovalKeepsRuntimeWorkerLiveAndUnblocksSameSession(t *testing.T) {
	for _, test := range []struct {
		name  string
		apply func(*grpcHarness, string)
	}{
		{
			name: "denied",
			apply: func(h *grpcHarness, approvalID string) {
				response, err := h.approvals.DenyApproval(h.clientContext(), &turingv1.DenyApprovalRequest{ApprovalId: approvalID, Reason: "no"})
				if err != nil || response.GetStatus() != turingv1.ApprovalStatus_APPROVAL_STATUS_DENIED {
					t.Fatalf("DenyApproval = %+v, %v", response, err)
				}
			},
		},
		{
			name: "expired",
			apply: func(h *grpcHarness, approvalID string) {
				deadline := time.Now().Add(3 * time.Second)
				for time.Now().Before(deadline) {
					state, err := h.runtimeApprovals.GetApprovalForRuntime(h.internalContext(), &turingv1.GetApprovalForRuntimeRequest{ApprovalId: approvalID})
					if err == nil && state.GetStatus() == turingv1.ApprovalStatus_APPROVAL_STATUS_EXPIRED {
						return
					}
					time.Sleep(10 * time.Millisecond)
				}
				t.Fatalf("approval %q did not expire", approvalID)
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			harness := newGRPCHarness(t, withApprovalTTL(time.Millisecond), withFilesCreateTool())
			defer harness.close()
			sessionID := harness.createSession(t, "terminal approval keeps worker")
			ctx, cancel := context.WithTimeout(harness.clientContext(), 10*time.Second)
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
			runID := ""
			for {
				event, err := stream.Recv()
				if err != nil {
					t.Fatalf("terminal approval stream: %v", err)
				}
				if persisted := event.GetPersistedEvent(); persisted != nil && persisted.Type == turingv1.TuringEventType_TURING_EVENT_TYPE_APPROVAL_REQUESTED {
					runID = event.GetRunId()
					approvalID := stringField(persisted.Payload, "approvalId")
					if runID == "" || approvalID == "" {
						t.Fatalf("approval.requested event = %+v, want run and approval IDs", event)
					}
					test.apply(harness, approvalID)
				}
				if failed := event.GetRunFailed(); failed != nil {
					if failed.GetRunId() != runID {
						t.Fatalf("failed run = %q, want %q", failed.GetRunId(), runID)
					}
					break
				}
			}
			run := waitForInactiveTerminalRun(t, harness, runID)
			if run.Status != "failed" {
				t.Fatalf("terminal approval run = %+v, want failed", run)
			}
			select {
			case workerErr := <-harness.workerDone:
				t.Fatalf("runtime worker exited after terminal approval: %v", workerErr)
			default:
			}
			later := harness.sendMessageToCompletion(t, sessionID, "later same-session message")
			started := false
			for _, event := range later {
				started = started || event.GetRunStarted() != nil
			}
			if !started || !hasRunCompleted(later) {
				t.Fatalf("later same-session events did not start and complete: %+v", later)
			}
		})
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

func waitForInactiveTerminalRun(t *testing.T, harness *grpcHarness, runID string) orchestratortestkit.Run {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		run, err := harness.repo.GetRun(context.Background(), runID)
		if err == nil && !run.ExecutionActive {
			return run
		}
		time.Sleep(time.Millisecond)
	}
	run, err := harness.repo.GetRun(context.Background(), runID)
	if err != nil {
		t.Fatal(err)
	}
	t.Fatalf("run %q remained execution_active: %+v", runID, run)
	return orchestratortestkit.Run{}
}

func runCompletedPersistedContent(t *testing.T, harness *grpcHarness, sessionID string, events []*turingv1.ChatStreamEvent) string {
	t.Helper()
	assistantMessageID := ""
	for _, event := range events {
		if completed := event.GetRunCompleted(); completed != nil {
			assistantMessageID = completed.AssistantMessageId
			break
		}
	}
	if assistantMessageID == "" {
		t.Fatal("stream did not include run completed with an assistant message ID")
	}
	ctx, cancel := context.WithTimeout(harness.clientContext(), 5*time.Second)
	defer cancel()
	response, err := harness.sessions.ListMessages(ctx, &turingv1.ListMessagesRequest{SessionId: sessionID})
	if err != nil {
		t.Fatalf("ListMessages: %v", err)
	}
	for _, message := range response.Messages {
		if message.MessageId == assistantMessageID {
			return message.Content
		}
	}
	t.Fatalf("run completed assistant message %q was not persisted", assistantMessageID)
	return ""
}

func completedRunID(t *testing.T, events []*turingv1.ChatStreamEvent) string {
	t.Helper()
	var completedRuns []*turingv1.RunCompleted
	for _, event := range events {
		if completed := event.GetRunCompleted(); completed != nil {
			completedRuns = append(completedRuns, completed)
		}
	}
	if len(completedRuns) != 1 {
		t.Fatalf("stream run.completed count = %d, want 1", len(completedRuns))
	}
	if completedRuns[0].RunId == "" {
		t.Fatal("stream run.completed had an empty run ID")
	}
	return completedRuns[0].RunId
}

func assertStreamedToolLifecycle(t *testing.T, events []*turingv1.ChatStreamEvent, toolName, runID, finalText string) string {
	t.Helper()
	var started, completed []*turingv1.TuringEvent
	startedIndex, completedIndex := -1, -1
	finalDeltaIndex, messageCompletedIndex, runCompletedIndex := -1, -1, -1
	finalDeltaCount, messageCompletedCount, runCompletedCount := 0, 0, 0
	for index, streamEvent := range events {
		if delta := streamEvent.GetTokenDelta(); delta != nil {
			if delta.Delta != finalText {
				t.Fatalf("streamed message delta = %q, want %q", delta.Delta, finalText)
			}

			finalDeltaCount++
			finalDeltaIndex = index
		}
		if message := streamEvent.GetMessageCompleted(); message != nil {
			if message.Content != finalText {
				t.Fatalf("streamed message.completed content = %q, want %q", message.Content, finalText)
			}
			messageCompletedCount++
			messageCompletedIndex = index
		}
		if streamEvent.GetRunCompleted() != nil {
			runCompletedCount++
			runCompletedIndex = index
		}
		if streamEvent.GetRunFailed() != nil || streamEvent.GetRunCancelled() != nil {
			t.Fatalf("unexpected streamed run terminal event: %#v", streamEvent.Event)
		}
		event := streamEvent.GetPersistedEvent()
		if event == nil {
			continue
		}
		switch event.Type {
		case turingv1.TuringEventType_TURING_EVENT_TYPE_TOOL_CALL_STARTED:
			started = append(started, event)
			startedIndex = index
		case turingv1.TuringEventType_TURING_EVENT_TYPE_TOOL_CALL_COMPLETED:
			completed = append(completed, event)
			completedIndex = index
		case turingv1.TuringEventType_TURING_EVENT_TYPE_TOOL_CALL_FAILED,
			turingv1.TuringEventType_TURING_EVENT_TYPE_TOOL_CALL_DENIED,
			turingv1.TuringEventType_TURING_EVENT_TYPE_APPROVAL_DENIED:
			t.Fatalf("unexpected streamed terminal tool event: %s", event.Type)
		}
	}
	if len(started) != 1 || len(completed) != 1 {
		t.Fatalf("streamed tool lifecycle counts: started=%d completed=%d, want 1 each", len(started), len(completed))
	}
	if startedIndex >= completedIndex {
		t.Fatalf("streamed tool lifecycle order: started index=%d completed index=%d, want STARTED before COMPLETED", startedIndex, completedIndex)
	}
	if finalDeltaCount != 1 || messageCompletedCount != 1 || runCompletedCount != 1 {
		t.Fatalf("streamed completion counts: final delta=%d message.completed=%d run.completed=%d, want 1 each", finalDeltaCount, messageCompletedCount, runCompletedCount)
	}
	if completedIndex >= finalDeltaIndex || finalDeltaIndex >= messageCompletedIndex || messageCompletedIndex >= runCompletedIndex {
		t.Fatalf("streamed completion order: tool=%d final delta=%d message=%d run=%d", completedIndex, finalDeltaIndex, messageCompletedIndex, runCompletedIndex)
	}
	toolCallID := stringField(started[0].Payload, "toolCallId")
	if toolCallID == "" {
		t.Fatal("streamed started toolCallId is empty")
	}
	assertToolLifecycleEvent(t, "streamed started", started[0], toolCallID, toolName, runID)
	assertToolLifecycleEvent(t, "streamed completed", completed[0], toolCallID, toolName, runID)
	return toolCallID
}

func assertStreamedApprovalToolLifecycle(t *testing.T, events []*turingv1.ChatStreamEvent, toolName, runID, approvalID, finalText string) string {
	t.Helper()
	var started, requested, approved, consumed, completed []*turingv1.TuringEvent
	startedIndex, requestedIndex, approvedIndex, consumedIndex, completedIndex := -1, -1, -1, -1, -1
	messageCompletedIndex, runCompletedIndex := -1, -1
	for index, streamEvent := range events {
		if failed := streamEvent.GetRunFailed(); failed != nil {
			t.Fatalf("unexpected run failure: %+v", failed)
		}
		if message := streamEvent.GetMessageCompleted(); message != nil {
			if message.Content != finalText {
				t.Fatalf("streamed message.completed content = %q, want %q", message.Content, finalText)
			}
			messageCompletedIndex = index
		}
		event := streamEvent.GetPersistedEvent()
		if streamEvent.GetRunCompleted() != nil {
			runCompletedIndex = index
		}
		if event == nil || event.RunId != runID {
			continue
		}
		switch event.Type {
		case turingv1.TuringEventType_TURING_EVENT_TYPE_TOOL_CALL_STARTED:
			started = append(started, event)
			startedIndex = index
		case turingv1.TuringEventType_TURING_EVENT_TYPE_APPROVAL_REQUESTED:
			requested = append(requested, event)
			requestedIndex = index
		case turingv1.TuringEventType_TURING_EVENT_TYPE_APPROVAL_APPROVED:
			approved = append(approved, event)
			approvedIndex = index
		case turingv1.TuringEventType_TURING_EVENT_TYPE_APPROVAL_CONSUMED:
			consumed = append(consumed, event)
			consumedIndex = index
		case turingv1.TuringEventType_TURING_EVENT_TYPE_TOOL_CALL_COMPLETED:
			completed = append(completed, event)
			completedIndex = index
		case turingv1.TuringEventType_TURING_EVENT_TYPE_AGENT_RUN_FAILED,
			turingv1.TuringEventType_TURING_EVENT_TYPE_TOOL_CALL_FAILED,
			turingv1.TuringEventType_TURING_EVENT_TYPE_TOOL_CALL_DENIED,
			turingv1.TuringEventType_TURING_EVENT_TYPE_APPROVAL_DENIED:
			t.Fatalf("unexpected failed or denied event: %s", event.Type)
		}
	}
	if len(started) != 1 || len(requested) != 1 || len(approved) != 1 || len(consumed) != 1 || len(completed) != 1 {
		t.Fatalf("approval lifecycle counts: started=%d requested=%d approved=%d consumed=%d completed=%d, want 1 each",
			len(started), len(requested), len(approved), len(consumed), len(completed))
	}
	if startedIndex >= requestedIndex || requestedIndex >= approvedIndex || approvedIndex >= consumedIndex ||
		consumedIndex >= completedIndex || completedIndex >= messageCompletedIndex || messageCompletedIndex >= runCompletedIndex {
		t.Fatalf("approval lifecycle order: started=%d requested=%d approved=%d consumed=%d completed=%d message=%d run=%d",
			startedIndex, requestedIndex, approvedIndex, consumedIndex, completedIndex, messageCompletedIndex, runCompletedIndex)
	}
	for label, event := range map[string]*turingv1.TuringEvent{
		"requested": requested[0],
		"approved":  approved[0],
		"consumed":  consumed[0],
	} {
		if got := stringField(event.Payload, "approvalId"); got != approvalID {
			t.Fatalf("%s approvalId = %q, want %q", label, got, approvalID)
		}
	}
	toolCallID := stringField(started[0].Payload, "toolCallId")
	if toolCallID == "" {
		t.Fatal("tool.call.started toolCallId is empty")
	}
	assertToolLifecycleEvent(t, "streamed started", started[0], toolCallID, toolName, runID)
	assertToolLifecycleEvent(t, "streamed completed", completed[0], toolCallID, toolName, runID)
	if got := messageCompletedContent(t, events); got != finalText {
		t.Fatalf("message.completed content = %q, want %q", got, finalText)
	}
	return toolCallID
}

func assertToolLifecycleEvent(t *testing.T, label string, event *turingv1.TuringEvent, toolCallID string, toolName string, runID string) {
	t.Helper()
	if event.RunId != runID {
		t.Fatalf("%s run ID = %q, want %q", label, event.RunId, runID)
	}
	if got := stringField(event.Payload, "toolCallId"); got != toolCallID {
		t.Fatalf("%s toolCallId = %q, want %q", label, got, toolCallID)
	}
	if got := stringField(event.Payload, "toolName"); got != toolName {
		t.Fatalf("%s toolName = %q, want %q", label, got, toolName)
	}
}

func assertInitialOpenAIRequest(t *testing.T, body map[string]any, priorUserText, priorAssistantText, userText string) string {
	t.Helper()
	tools, _ := body["tools"].([]any)
	if len(tools) != 3 {
		t.Fatalf("initial OpenAI tools = %#v, want two skill tools and system.time", body["tools"])
	}
	assertSkillToolAdvertisements(t, tools)
	tool := advertisedToolByDescription(t, tools, "Get the current system time for a time zone.")
	if tool["type"] != "function" {
		t.Fatalf("advertised OpenAI tool type = %#v, want function", tool["type"])
	}
	function, _ := tool["function"].(map[string]any)
	alias, _ := function["name"].(string)
	if alias == "" || strings.Contains(alias, ".") || !isOpenAIFunctionAlias(alias) {
		t.Fatalf("advertised OpenAI function alias = %q, want a valid non-dotted alias", alias)
	}
	if got := function["description"]; got != "Get the current system time for a time zone." {
		t.Fatalf("advertised description = %#v", got)
	}
	wantSchema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"timezone": map[string]any{"type": "string"},
		},
	}
	if got := function["parameters"]; !reflect.DeepEqual(got, wantSchema) {
		t.Fatalf("advertised schema = %#v, want %#v", got, wantSchema)
	}
	messages, _ := body["messages"].([]any)
	wantMessages := []any{
		map[string]any{"role": "user", "content": priorUserText},
		map[string]any{"role": "assistant", "content": priorAssistantText},
		map[string]any{"role": "user", "content": userText},
	}
	if !reflect.DeepEqual(messages, wantMessages) {
		t.Fatalf("initial OpenAI messages = %#v, want exactly %#v", messages, wantMessages)
	}
	return alias
}

func assertInitialFilesOpenAIRequest(t *testing.T, body map[string]any, userText string) string {
	t.Helper()
	tools, _ := body["tools"].([]any)
	if len(tools) != 3 {
		t.Fatalf("initial OpenAI tools = %#v, want two skill tools and files.create", body["tools"])
	}
	assertSkillToolAdvertisements(t, tools)
	tool := advertisedToolByDescription(t, tools, "Create a file.")
	function, _ := tool["function"].(map[string]any)
	alias, _ := function["name"].(string)
	if tool["type"] != "function" || alias == "" || strings.Contains(alias, ".") || !isOpenAIFunctionAlias(alias) {
		t.Fatalf("advertised files.create tool = %#v", tool)
	}
	if function["description"] != "Create a file." {
		t.Fatalf("files.create description = %#v", function["description"])
	}
	messages, _ := body["messages"].([]any)
	wantMessages := []any{map[string]any{"role": "user", "content": userText}}
	if !reflect.DeepEqual(messages, wantMessages) {
		t.Fatalf("initial messages = %#v, want %#v", messages, wantMessages)
	}
	return alias
}

func assertSkillToolAdvertisements(t *testing.T, tools []any) {
	t.Helper()
	if !hasAdvertisedFunctionNamed(tools, "skills_list") || !hasAdvertisedFunctionNamed(tools, "skill_view") {
		t.Fatalf("advertised tools = %#v, want skills_list and skill_view", tools)
	}
}

func advertisedToolByDescription(t *testing.T, tools []any, description string) map[string]any {
	t.Helper()
	for _, raw := range tools {
		tool, _ := raw.(map[string]any)
		function, _ := tool["function"].(map[string]any)
		if function["description"] == description {
			return tool
		}
	}
	t.Fatalf("advertised tools = %#v, want description %q", tools, description)
	return nil
}

func assertFollowupOpenAIRequest(t *testing.T, body map[string]any, priorUserText, priorAssistantText, userText, alias, toolCallID string) {
	t.Helper()
	messages, _ := body["messages"].([]any)
	if len(messages) != 5 {
		t.Fatalf("follow-up OpenAI messages = %#v, want exactly prior user, prior assistant, current user, tool call, and tool result", body["messages"])
	}
	wantHistory := []any{
		map[string]any{"role": "user", "content": priorUserText},
		map[string]any{"role": "assistant", "content": priorAssistantText},
		map[string]any{"role": "user", "content": userText},
	}
	if !reflect.DeepEqual(messages[:3], wantHistory) {
		t.Fatalf("follow-up history = %#v, want %#v", messages[:3], wantHistory)
	}

	assistant, _ := messages[3].(map[string]any)
	if assistant["role"] != "assistant" {
		t.Fatalf("follow-up message[3] role = %#v, want assistant", assistant["role"])
	}
	if content, present := assistant["content"]; !present || content != nil {
		t.Fatalf("follow-up assistant content = %#v (present=%t), want canonical null", content, present)
	}
	calls, _ := assistant["tool_calls"].([]any)
	if len(calls) != 1 {
		t.Fatalf("assistant tool_calls = %#v, want one", assistant["tool_calls"])
	}
	call, _ := calls[0].(map[string]any)
	if got := call["id"]; got != toolCallID {
		t.Fatalf("assistant tool call ID = %#v, want %q", got, toolCallID)
	}
	if got := call["type"]; got != "function" {
		t.Fatalf("assistant tool call type = %#v, want function", got)
	}
	function, _ := call["function"].(map[string]any)
	if got := function["name"]; got != alias {
		t.Fatalf("assistant wire function name = %#v, want alias %q", got, alias)
	}
	var arguments map[string]any
	argumentsJSON, _ := function["arguments"].(string)
	if err := json.Unmarshal([]byte(argumentsJSON), &arguments); err != nil {
		t.Fatalf("assistant wire arguments %q: %v", argumentsJSON, err)
	}
	if !reflect.DeepEqual(arguments, map[string]any{"timezone": "UTC"}) {
		t.Fatalf("assistant wire arguments = %#v", arguments)
	}

	tool, _ := messages[4].(map[string]any)
	if tool["role"] != "tool" || tool["tool_call_id"] != toolCallID {
		t.Fatalf("tool result linkage = %#v, want tool_call_id %q", tool, toolCallID)
	}
	var result map[string]any
	resultJSON, _ := tool["content"].(string)
	if err := json.Unmarshal([]byte(resultJSON), &result); err != nil {
		t.Fatalf("tool result content %q: %v", resultJSON, err)
	}
	wantResult := map[string]any{
		"iso":      "2025-01-02T03:04:05Z",
		"unixMs":   float64(1735787045000),
		"timezone": "UTC",
	}
	if !reflect.DeepEqual(result, wantResult) {
		t.Fatalf("tool result content = %#v", result)
	}
}

func assertFollowupFilesOpenAIRequest(t *testing.T, body map[string]any, userText, alias, modelID string, wantArgs map[string]any) {
	t.Helper()
	messages, _ := body["messages"].([]any)
	if len(messages) != 3 || !reflect.DeepEqual(messages[0], map[string]any{"role": "user", "content": userText}) {
		t.Fatalf("follow-up messages = %#v, want user, assistant call, tool result", body["messages"])
	}
	assistant, _ := messages[1].(map[string]any)
	if content, present := assistant["content"]; assistant["role"] != "assistant" || !present || content != nil {
		t.Fatalf("assistant tool-call message = %#v, want role and null content", assistant)
	}
	calls, _ := assistant["tool_calls"].([]any)
	if len(calls) != 1 {
		t.Fatalf("assistant tool_calls = %#v", assistant["tool_calls"])
	}
	call, _ := calls[0].(map[string]any)
	function, _ := call["function"].(map[string]any)
	if call["id"] != modelID || call["type"] != "function" || function["name"] != alias {
		t.Fatalf("assistant tool call = %#v, want model ID %q and alias %q", call, modelID, alias)
	}
	var args map[string]any
	argumentsJSON, _ := function["arguments"].(string)
	if err := json.Unmarshal([]byte(argumentsJSON), &args); err != nil || !reflect.DeepEqual(args, wantArgs) {
		t.Fatalf("assistant arguments = %q => %#v, error=%v", argumentsJSON, args, err)
	}
	toolResult, _ := messages[2].(map[string]any)
	if toolResult["role"] != "tool" || toolResult["tool_call_id"] != modelID {
		t.Fatalf("tool result linkage = %#v, want model ID %q", toolResult, modelID)
	}
	var result map[string]any
	resultJSON, _ := toolResult["content"].(string)
	if err := json.Unmarshal([]byte(resultJSON), &result); err != nil {
		t.Fatalf("tool result JSON = %q: %v", resultJSON, err)
	}
	wantResult := map[string]any{"path": "model-created.txt", "created": true, "content": "created through approval flow"}
	if !reflect.DeepEqual(result, wantResult) {
		t.Fatalf("tool result = %#v, want %#v", result, wantResult)
	}
}

func modelToolCallID(t *testing.T, body map[string]any) string {
	t.Helper()
	messages, _ := body["messages"].([]any)
	if len(messages) < 2 {
		t.Fatalf("OpenAI messages = %#v, want assistant and tool result", body["messages"])
	}
	assistant, _ := messages[len(messages)-2].(map[string]any)
	calls, _ := assistant["tool_calls"].([]any)
	if len(calls) != 1 {
		t.Fatalf("assistant tool_calls = %#v, want one", assistant["tool_calls"])
	}
	call, _ := calls[0].(map[string]any)
	id, _ := call["id"].(string)
	if id == "" {
		t.Fatalf("assistant model linkage ID = %#v, want nonempty string", call["id"])
	}
	toolResult, _ := messages[len(messages)-1].(map[string]any)
	if toolResult["tool_call_id"] != id {
		t.Fatalf("tool result linkage = %#v, want model ID %q", toolResult, id)
	}
	return id
}
func isOpenAIFunctionAlias(name string) bool {
	if len(name) == 0 || len(name) > 64 {
		return false
	}
	for _, character := range name {
		if character >= 'a' && character <= 'z' ||
			character >= 'A' && character <= 'Z' ||
			character >= '0' && character <= '9' ||
			character == '_' || character == '-' {
			continue
		}
		return false
	}
	return true
}

func assertModelDrivenMCPRequests(t *testing.T, system, files []fakeMCPRequest) {
	t.Helper()
	if len(system) != 2 || system[0].method != "tools/list" || system[1].method != "tools/call" {
		t.Fatalf("system MCP requests = %#v, want tools/list then tools/call", system)
	}
	if len(files) != 1 || files[0].method != "tools/list" {
		t.Fatalf("files MCP requests = %#v, want only tools/list", files)
	}
	if len(system[0].params) != 0 || len(files[0].params) != 0 {
		t.Fatalf("MCP tools/list params: system=%#v files=%#v, want empty objects", system[0].params, files[0].params)
	}
	if system[0].id == "" || system[1].id == "" || files[0].id == "" {
		t.Fatalf("MCP request IDs must be present integers: system=%#v files=%#v", system, files)
	}
	if got := system[1].params["name"]; got != "system.time" {
		t.Fatalf("MCP tools/call name = %#v, want system.time", got)
	}
	if got := system[1].params["arguments"]; !reflect.DeepEqual(got, map[string]any{"timezone": "UTC"}) {
		t.Fatalf("MCP tools/call arguments = %#v", got)
	}
}

func assertNoFakeHandlerErrors(t *testing.T, model *fakeModelServer, servers ...*fakeMCPServer) {
	t.Helper()
	select {
	case err := <-model.handlerErrors:
		t.Fatalf("fake OpenAI rejected request: %v", err)
	default:
	}
	for _, server := range servers {
		select {
		case err := <-server.handlerErrors:
			t.Fatalf("fake %s MCP rejected request: %v", server.name, err)
		default:
		}
	}
}
