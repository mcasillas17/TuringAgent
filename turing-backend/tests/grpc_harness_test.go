package tests

import (
	"context"
	"crypto/sha256"
	"encoding/base32"
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
	modelDrivenToolCall  bool
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
	mu               sync.Mutex
	advertiseTime    bool
	requests         []fakeMCPRequest
	handlerErrorOnce sync.Once
	handlerErrors    chan error
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
			Conn:               h.internalConn,
			InternalToken:      integrationInternalToken,
			WorkerID:           "worker-grpc-integration",
			MaxConcurrentRuns:  1,
			MaxToolCallsPerRun: 10,
			OpenAIBaseURL:      h.fakeModel.server.URL,
			OpenAIAPIKey:       integrationOpenAIKey,
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
	modelDrivenToolCall := f.modelDrivenToolCall
	requestNumber := len(f.chatCompletionBodies) + 1
	if err := validateOpenAIRequest(body, requestNumber, modelDrivenToolCall); err != nil {
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
	if modelDrivenToolCall {
		f.writeModelDrivenResponse(w, flusher, body, requestNumber)
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
	if requireToolFlow && (!toolsPresent || !toolsAreArray || len(tools) != 1) {
		return fmt.Errorf("OpenAI tools = %#v, want one function tool", rawTools)
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
		if assistant["role"] != "assistant" || !callsOK || len(calls) != 1 {
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

func (f *fakeModelServer) writeModelDrivenResponse(w http.ResponseWriter, flusher http.Flusher, body map[string]any, requestNumber int) {
	switch requestNumber {
	case 1:
		alias := advertisedFunctionAlias(body)
		if alias == "" {
			writeOpenAIChunk(w, "", "stop")
			return
		}
		writeOpenAIToolCallChunk(w, alias, `{"timezone":`, true)
		if flusher != nil {
			flusher.Flush()
		}
		writeOpenAIToolCallChunk(w, "", `"UTC"}`, false)
		if flusher != nil {
			flusher.Flush()
		}
		writeOpenAIChunk(w, "", "tool_calls")
	case 2:
		writeOpenAIChunk(w, "The fixed time is 2025-01-02T03:04:05Z.", "")
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

func advertisedFunctionAlias(body map[string]any) string {
	tools, _ := body["tools"].([]any)
	if len(tools) == 0 {
		return ""
	}
	tool, _ := tools[0].(map[string]any)
	function, _ := tool["function"].(map[string]any)
	alias, _ := function["name"].(string)
	return alias
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
	f.modelDrivenToolCall = true
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
		name:           name,
		token:          token,
		approvalTokens: make(chan string, 4),
		handlerErrors:  make(chan error, 1),
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
	if completed := messageCompletedContent(events); completed != "Hello" {
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
		"files/files.list":      turingv1.ToolPolicy_TOOL_POLICY_SAFE,
		"files/files.read":      turingv1.ToolPolicy_TOOL_POLICY_SAFE,
		"files/files.search":    turingv1.ToolPolicy_TOOL_POLICY_SAFE,
		"files/files.update":    turingv1.ToolPolicy_TOOL_POLICY_APPROVAL_REQUIRED,
		"system/system.echo":    turingv1.ToolPolicy_TOOL_POLICY_SAFE,
		"system/system.health":  turingv1.ToolPolicy_TOOL_POLICY_SAFE,
		"system/system.info":    turingv1.ToolPolicy_TOOL_POLICY_SAFE,
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

func TestModelDrivenToolCallCompletesRun(t *testing.T) {
	const (
		userText  = "What time is it in UTC?"
		finalText = "The fixed time is 2025-01-02T03:04:05Z."
	)
	harness := newGRPCHarness(t)
	defer harness.close()
	harness.systemMCP.enableTimeTool()
	harness.fakeModel.enableModelDrivenToolCall()

	sessionID := harness.createSession(t, "model-driven tool call")
	streamEvents := harness.sendMessageToCompletion(t, sessionID, userText)
	assertNoFakeHandlerErrors(t, harness.fakeModel, harness.systemMCP, harness.filesMCP)
	runID := completedRunID(t, streamEvents)
	expectedToolCallID := deterministicToolCallID(runID, 0, 0)
	listContext, cancelList := context.WithTimeout(harness.clientContext(), 5*time.Second)
	defer cancelList()
	listed, err := harness.events.ListEvents(listContext, &turingv1.ListEventsRequest{
		SessionId: sessionID,
		Limit:     100,
	})
	if err != nil {
		t.Fatalf("ListEvents: %v", err)
	}

	var started, completed []*turingv1.TuringEvent
	for _, event := range listed.Events {
		switch event.Type {
		case turingv1.TuringEventType_TURING_EVENT_TYPE_TOOL_CALL_STARTED:
			started = append(started, event)
		case turingv1.TuringEventType_TURING_EVENT_TYPE_TOOL_CALL_COMPLETED:
			completed = append(completed, event)
		case turingv1.TuringEventType_TURING_EVENT_TYPE_TOOL_CALL_FAILED,
			turingv1.TuringEventType_TURING_EVENT_TYPE_TOOL_CALL_DENIED,
			turingv1.TuringEventType_TURING_EVENT_TYPE_APPROVAL_DENIED:
			t.Fatalf("unexpected terminal tool event: %s", event.Type)
		}
	}
	if len(started) != 1 || len(completed) != 1 {
		t.Fatalf("tool lifecycle counts: started=%d completed=%d, want 1 each", len(started), len(completed))
	}
	if started[0].Sequence >= completed[0].Sequence {
		t.Fatalf("tool lifecycle sequences: started=%d completed=%d, want STARTED before COMPLETED", started[0].Sequence, completed[0].Sequence)
	}
	if started[0].RunId != runID || completed[0].RunId != runID {
		t.Fatalf("tool lifecycle run IDs: started=%q completed=%q, want %q", started[0].RunId, completed[0].RunId, runID)
	}
	if got := stringField(started[0].Payload, "toolCallId"); got != expectedToolCallID {
		t.Fatalf("started toolCallId = %q, want %q", got, expectedToolCallID)
	}
	if got := stringField(completed[0].Payload, "toolCallId"); got != expectedToolCallID {
		t.Fatalf("completed toolCallId = %q, want %q", got, expectedToolCallID)
	}
	if startedName, completedName := stringField(started[0].Payload, "toolName"), stringField(completed[0].Payload, "toolName"); startedName != "system.time" || completedName != startedName {
		t.Fatalf("tool lifecycle names: started=%q completed=%q, want system.time for both", startedName, completedName)
	}
	if got := messageCompletedContent(streamEvents); got != finalText {
		t.Fatalf("stream message.completed content = %q, want %q", got, finalText)
	}
	if got := messageCompletedPayload(listed.Events); got != finalText {
		t.Fatalf("persisted message.completed content = %q, want %q", got, finalText)
	}
	if got := runCompletedPersistedContent(t, harness, sessionID, streamEvents); got != finalText {
		t.Fatalf("run completed content = %q, want %q", got, finalText)
	}

	modelBodies := harness.fakeModel.bodies()
	if len(modelBodies) != 2 {
		t.Fatalf("OpenAI request count = %d, want 2", len(modelBodies))
	}
	alias := assertInitialOpenAIRequest(t, modelBodies[0], userText)
	assertFollowupOpenAIRequest(t, modelBodies[1], userText, alias, expectedToolCallID)
	assertModelDrivenMCPRequests(t, harness.systemMCP.recordedRequests(), harness.filesMCP.recordedRequests())
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
	runID := ""
	for _, event := range events {
		if completed := event.GetRunCompleted(); completed != nil {
			if runID != "" {
				t.Fatalf("stream included multiple run.completed events: %q and %q", runID, completed.RunId)
			}
			runID = completed.RunId
		}
	}
	if runID == "" {
		t.Fatal("stream did not include run.completed with a run ID")
	}
	return runID
}

func deterministicToolCallID(runID string, round, index int) string {
	input := fmt.Sprintf("%d:%s:%d:%d", len(runID), runID, round, index)
	sum := sha256.Sum256([]byte(input))
	return "call_" + base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(sum[:])
}

func assertInitialOpenAIRequest(t *testing.T, body map[string]any, userText string) string {
	t.Helper()
	tools, _ := body["tools"].([]any)
	if len(tools) != 1 {
		t.Fatalf("initial OpenAI tools = %#v, want one tool", body["tools"])
	}
	tool, _ := tools[0].(map[string]any)
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
	wantMessages := []any{map[string]any{"role": "user", "content": userText}}
	if !reflect.DeepEqual(messages, wantMessages) {
		t.Fatalf("initial OpenAI messages = %#v, want exactly %#v", messages, wantMessages)
	}
	return alias
}

func assertFollowupOpenAIRequest(t *testing.T, body map[string]any, userText, alias, toolCallID string) {
	t.Helper()
	messages, _ := body["messages"].([]any)
	if len(messages) != 3 {
		t.Fatalf("follow-up OpenAI messages = %#v, want exactly user, assistant, and tool", body["messages"])
	}
	wantUser := map[string]any{"role": "user", "content": userText}
	if !reflect.DeepEqual(messages[0], wantUser) {
		t.Fatalf("follow-up message[0] = %#v, want %#v", messages[0], wantUser)
	}

	assistant, _ := messages[1].(map[string]any)
	if assistant["role"] != "assistant" {
		t.Fatalf("follow-up message[1] role = %#v, want assistant", assistant["role"])
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

	tool, _ := messages[2].(map[string]any)
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
