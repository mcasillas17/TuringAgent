package runtime

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/auth"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/db"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/runoutcome"
	approvalsvc "github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/service/approvals"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/service/events"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/types/known/structpb"
)

type harness struct {
	repo      *repository.Repository
	database  *db.DB
	bus       *events.Bus
	service   *Server
	approvals *approvalsvc.Server
	conn      *grpc.ClientConn
}

var runtimeTestDatabaseSequence atomic.Uint64

func newHarness(t *testing.T) *harness {
	return newHarnessWithDispatch(t, DispatchConfig{})
}

func newHarnessWithDispatch(t *testing.T, dispatch DispatchConfig) *harness {
	t.Helper()
	if dispatch.LegacyCapabilities == nil {
		dispatch.LegacyCapabilities = &LegacyCapabilityProfile{
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
			Tools:                       LegacyPolicyToolCapabilities(),
			ExternalAgentCredentialRefs: []string{"claude", "external"},
			SupportsExternalAgents:      true,
		}
	}
	database := openRuntimeTestDB(t)
	repo := repository.New(database)
	bus := events.NewBus(8)
	approvals := approvalsvc.New(repo, bus, "approval-secret")
	service := NewWithConfig(repo, bus, dispatch, approvals)
	lis := bufconn.Listen(1024 * 1024)
	grpcServer := grpc.NewServer(grpc.StreamInterceptor(auth.StreamInterceptor("internal-token")))
	turingv1.RegisterRuntimeServiceServer(grpcServer, service)
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
	t.Cleanup(func() {
		grpcServer.Stop()
		service.WaitForWorkerStreams()
		_ = conn.Close()
	})
	return &harness{repo: repo, database: database, bus: bus, service: service, approvals: approvals, conn: conn}
}

func testWorkerCapabilities(maxConcurrentRuns int) *registeredWorkerCapabilities {
	return &registeredWorkerCapabilities{
		models: []registeredModelCapability{{
			provider:         "ollama",
			model:            "llama3.2",
			maxContextTokens: 8192,
		}},
		agentIDs:          map[string]struct{}{"general_assistant": {}},
		tools:             map[string]struct{}{},
		maxConcurrentRuns: maxConcurrentRuns,
	}
}

func openRuntimeTestDB(t *testing.T) *db.DB {
	t.Helper()
	name := strings.NewReplacer("/", "_", " ", "_", ":", "_").Replace(t.Name()) + fmt.Sprintf("_%d", runtimeTestDatabaseSequence.Add(1))
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

func (h *harness) runtimeClient(t *testing.T) turingv1.RuntimeServiceClient {
	t.Helper()
	return turingv1.NewRuntimeServiceClient(h.conn)
}

func (h *harness) internalContext() context.Context {
	return metadata.NewOutgoingContext(context.Background(), metadata.Pairs("authorization", "Bearer internal-token"))
}

func (h *harness) createSessionAndRun(t *testing.T, content string) string {
	t.Helper()
	session, err := h.repo.CreateSession(context.Background(), "Runtime")
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if _, err := h.repo.EnqueueUserMessage(context.Background(), repository.EnqueueUserMessageInput{
		SessionID:     session.SessionID,
		Content:       content,
		AgentID:       "general_assistant",
		ModelProvider: "ollama",
		Model:         "llama3.2",
	}); err != nil {
		t.Fatalf("EnqueueUserMessage: %v", err)
	}
	return session.SessionID
}

func (h *harness) createRunningRun(t *testing.T, content string) string {
	t.Helper()
	return h.createRunningRunResult(t, content).RunID
}

func (h *harness) createRunningRunResult(t *testing.T, content string) repository.EnqueueUserMessageResult {
	t.Helper()
	session, err := h.repo.CreateSession(context.Background(), "Runtime")
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	enqueued, err := h.repo.EnqueueUserMessage(context.Background(), repository.EnqueueUserMessageInput{
		SessionID:     session.SessionID,
		Content:       content,
		AgentID:       "general_assistant",
		ModelProvider: "ollama",
		Model:         "llama3.2",
	})
	if err != nil {
		t.Fatalf("EnqueueUserMessage: %v", err)
	}
	if err := h.repo.MarkRunRunning(context.Background(), enqueued.RunID); err != nil {
		t.Fatalf("MarkRunRunning: %v", err)
	}
	return enqueued
}

func TestConnectWorkerPersistsReportedToolsWithOrchestratorPolicies(t *testing.T) {
	h := newHarness(t)
	client := h.runtimeClient(t)
	stream, err := client.ConnectWorker(h.internalContext())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = stream.CloseSend() }()
	schema, err := structpb.NewStruct(map[string]any{"type": "object", "required": []any{"path"}})
	if err != nil {
		t.Fatal(err)
	}
	if err := stream.Send(&turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_WorkerReady{WorkerReady: &turingv1.RuntimeWorkerReady{
		WorkerId:          "worker-discovery",
		AgentId:           turingv1.AgentId_AGENT_ID_GENERAL_ASSISTANT,
		MaxConcurrentRuns: 1,
		Tools: []*turingv1.DiscoveredTool{
			{ServerName: "system", ToolName: "system.time", Schema: &structpb.Struct{}},
			{ServerName: "files", ToolName: "files.create", Schema: schema},
			{ServerName: "custom", ToolName: "custom.unrecognized", Schema: &structpb.Struct{}},
		},
	}}}); err != nil {
		t.Fatal(err)
	}
	recvUntil(t, stream, func(cmd *turingv1.RuntimeCommand) bool { return cmd.GetWorkerAccepted() != nil })

	got, err := h.repo.ListEnabledTools(context.Background())
	if err != nil {
		t.Fatalf("ListEnabledTools: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("enabled tools = %+v, want 3", got)
	}
	wantPolicies := map[string]string{
		"custom/custom.unrecognized": "approval_required",
		"files/files.create":         "approval_required",
		"system/system.time":         "safe",
	}
	for _, tool := range got {
		key := tool.ServerName + "/" + tool.ToolName
		if tool.Policy != wantPolicies[key] {
			t.Fatalf("tool %s policy = %q, want %q", key, tool.Policy, wantPolicies[key])
		}
		if !json.Valid([]byte(tool.SchemaJSON)) {
			t.Fatalf("tool %s schema is invalid JSON: %q", key, tool.SchemaJSON)
		}
	}
}

func TestConnectWorkerWithoutDiscoveryCapabilityPreservesRegistry(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	if err := h.repo.UpsertTools(ctx, []repository.DiscoveredTool{{
		ServerName: "system", ToolName: "system.time", SchemaJSON: `{}`, Policy: "safe",
	}}); err != nil {
		t.Fatalf("seed registry: %v", err)
	}

	client := h.runtimeClient(t)
	stream, err := client.ConnectWorker(h.internalContext())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = stream.CloseSend() }()
	if err := stream.Send(&turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_WorkerReady{WorkerReady: &turingv1.RuntimeWorkerReady{
		WorkerId:          "legacy-worker",
		AgentId:           turingv1.AgentId_AGENT_ID_GENERAL_ASSISTANT,
		MaxConcurrentRuns: 1,
	}}}); err != nil {
		t.Fatal(err)
	}
	recvUntil(t, stream, func(cmd *turingv1.RuntimeCommand) bool { return cmd.GetWorkerAccepted() != nil })

	got, err := h.repo.ListEnabledTools(ctx)
	if err != nil {
		t.Fatalf("ListEnabledTools: %v", err)
	}
	gotNames := map[string]bool{}
	for _, tool := range got {
		gotNames[tool.ServerName+"/"+tool.ToolName] = true
	}
	if !gotNames["system/system.time"] || !gotNames["files/files.create"] {
		t.Fatalf("registry after legacy handshake = %+v, want compatibility tools", got)
	}
}

func TestWorkerReadyDistinguishesCompletedEmptyDiscoveryFromLegacy(t *testing.T) {
	t.Run("completed empty discovery", func(t *testing.T) {
		h := newHarness(t)
		stream, err := h.runtimeClient(t).ConnectWorker(h.internalContext())
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = stream.CloseSend() }()
		if err := stream.Send(&turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_WorkerReady{WorkerReady: &turingv1.RuntimeWorkerReady{
			WorkerId: "worker-empty-complete", AgentId: turingv1.AgentId_AGENT_ID_GENERAL_ASSISTANT,
			MaxConcurrentRuns: 1, ToolDiscoveryStatus: turingv1.ToolDiscoveryStatus_TOOL_DISCOVERY_STATUS_COMPLETE,
		}}}); err != nil {
			t.Fatal(err)
		}
		recvUntil(t, stream, func(cmd *turingv1.RuntimeCommand) bool { return cmd.GetWorkerAccepted() != nil })
		assertEnabledToolNames(t, h.repo, nil)
	})

	t.Run("failed discovery", func(t *testing.T) {
		h := newHarness(t)
		stream, err := h.runtimeClient(t).ConnectWorker(h.internalContext())
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = stream.CloseSend() }()
		if err := stream.Send(&turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_WorkerReady{WorkerReady: &turingv1.RuntimeWorkerReady{
			WorkerId: "worker-discovery-failed", AgentId: turingv1.AgentId_AGENT_ID_GENERAL_ASSISTANT,
			MaxConcurrentRuns: 1, ToolDiscoveryStatus: turingv1.ToolDiscoveryStatus_TOOL_DISCOVERY_STATUS_FAILED,
		}}}); err != nil {
			t.Fatal(err)
		}
		if _, err := stream.Recv(); status.Code(err) != codes.FailedPrecondition {
			t.Fatalf("failed discovery error = %v, want FailedPrecondition", err)
		}
		assertEnabledToolNames(t, h.repo, nil)
	})

	t.Run("legacy handshake", func(t *testing.T) {
		h := newHarness(t)
		stream, err := h.runtimeClient(t).ConnectWorker(h.internalContext())
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = stream.CloseSend() }()
		if err := stream.Send(&turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_WorkerReady{WorkerReady: &turingv1.RuntimeWorkerReady{
			WorkerId: "worker-empty-legacy", AgentId: turingv1.AgentId_AGENT_ID_GENERAL_ASSISTANT, MaxConcurrentRuns: 1,
		}}}); err != nil {
			t.Fatal(err)
		}
		recvUntil(t, stream, func(cmd *turingv1.RuntimeCommand) bool { return cmd.GetWorkerAccepted() != nil })
		tools, err := h.repo.ListEnabledTools(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		got := map[string]bool{}
		for _, tool := range tools {
			got[tool.ServerName+"/"+tool.ToolName] = true
		}
		if !got["system/system.time"] || !got["files/files.create"] {
			t.Fatalf("legacy registry = %+v, want compatibility capabilities", got)
		}
	})
}

func TestConnectWorkerCombinesLegacyCompatibilityToolsWithDiscoveredTools(t *testing.T) {
	h := newHarness(t)
	client := h.runtimeClient(t)
	ctx, cancel := context.WithTimeout(h.internalContext(), 3*time.Second)
	defer cancel()

	legacy, err := client.ConnectWorker(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = legacy.CloseSend() }()
	if err := legacy.Send(&turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_WorkerReady{WorkerReady: &turingv1.RuntimeWorkerReady{
		WorkerId: "worker-mixed-legacy", AgentId: turingv1.AgentId_AGENT_ID_GENERAL_ASSISTANT, MaxConcurrentRuns: 1,
	}}}); err != nil {
		t.Fatal(err)
	}
	recvUntil(t, legacy, func(cmd *turingv1.RuntimeCommand) bool { return cmd.GetWorkerAccepted() != nil })

	discovered, err := client.ConnectWorker(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = discovered.CloseSend() }()
	if err := discovered.Send(&turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_WorkerReady{WorkerReady: &turingv1.RuntimeWorkerReady{
		WorkerId: "worker-mixed-discovered", AgentId: turingv1.AgentId_AGENT_ID_GENERAL_ASSISTANT, MaxConcurrentRuns: 1,
		Tools: []*turingv1.DiscoveredTool{{ServerName: "custom", ToolName: "custom.inspect", Schema: &structpb.Struct{}}}, ToolDiscoveryStatus: turingv1.ToolDiscoveryStatus_TOOL_DISCOVERY_STATUS_COMPLETE,
	}}}); err != nil {
		t.Fatal(err)
	}
	recvUntil(t, discovered, func(cmd *turingv1.RuntimeCommand) bool { return cmd.GetWorkerAccepted() != nil })

	tools, err := h.repo.ListEnabledTools(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, tool := range tools {
		got[tool.ServerName+"/"+tool.ToolName] = true
	}
	if !got["system/system.time"] || !got["files/files.create"] || !got["custom/custom.inspect"] {
		t.Fatalf("mixed-version registry = %+v, want legacy and discovered capabilities", got)
	}
}

func TestConnectWorkerRejectsMalformedDiscoverySnapshotWithoutMutation(t *testing.T) {
	invalidSchema := &structpb.Struct{Fields: map[string]*structpb.Value{"bad": {}}}
	tests := map[string]*turingv1.DiscoveredTool{
		"blank server":   {ServerName: " ", ToolName: "custom.inspect", Schema: &structpb.Struct{}},
		"blank tool":     {ServerName: "custom", ToolName: "\t", Schema: &structpb.Struct{}},
		"invalid schema": {ServerName: "custom", ToolName: "custom.inspect", Schema: invalidSchema},
	}
	for name, invalidTool := range tests {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t)
			if err := h.repo.UpsertTools(context.Background(), []repository.DiscoveredTool{{
				ServerName: "system", ToolName: "system.time", SchemaJSON: `{}`, Policy: "safe",
			}}); err != nil {
				t.Fatal(err)
			}
			stream, err := h.runtimeClient(t).ConnectWorker(h.internalContext())
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = stream.CloseSend() }()
			if err := stream.Send(&turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_WorkerReady{WorkerReady: &turingv1.RuntimeWorkerReady{
				WorkerId: "worker-invalid-" + strings.ReplaceAll(name, " ", "-"), AgentId: turingv1.AgentId_AGENT_ID_GENERAL_ASSISTANT,
				MaxConcurrentRuns: 1, Tools: []*turingv1.DiscoveredTool{invalidTool}, ToolDiscoveryStatus: turingv1.ToolDiscoveryStatus_TOOL_DISCOVERY_STATUS_COMPLETE,
			}}}); err != nil {
				t.Fatal(err)
			}
			if _, err := stream.Recv(); status.Code(err) != codes.InvalidArgument {
				t.Fatalf("malformed snapshot error = %v, want InvalidArgument", err)
			}
			assertEnabledToolNames(t, h.repo, []string{"system/system.time"})
		})
	}
}

func TestConnectWorkerReconcilesUnionOfActiveWorkerTools(t *testing.T) {
	h := newHarness(t)
	client := h.runtimeClient(t)
	ctx, cancel := context.WithTimeout(h.internalContext(), 3*time.Second)
	defer cancel()
	objectSchema := &structpb.Struct{}

	workerA, err := client.ConnectWorker(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = workerA.CloseSend() }()
	if err := workerA.Send(&turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_WorkerReady{WorkerReady: &turingv1.RuntimeWorkerReady{
		WorkerId: "worker-union-a", AgentId: turingv1.AgentId_AGENT_ID_GENERAL_ASSISTANT, MaxConcurrentRuns: 1,
		Tools: []*turingv1.DiscoveredTool{{ServerName: "system", ToolName: "system.time", Schema: objectSchema}}, ToolDiscoveryStatus: turingv1.ToolDiscoveryStatus_TOOL_DISCOVERY_STATUS_COMPLETE,
	}}}); err != nil {
		t.Fatal(err)
	}
	recvUntil(t, workerA, func(cmd *turingv1.RuntimeCommand) bool { return cmd.GetWorkerAccepted() != nil })

	workerB, err := client.ConnectWorker(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := workerB.Send(&turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_WorkerReady{WorkerReady: &turingv1.RuntimeWorkerReady{
		WorkerId: "worker-union-b", AgentId: turingv1.AgentId_AGENT_ID_GENERAL_ASSISTANT, MaxConcurrentRuns: 1,
		Tools: []*turingv1.DiscoveredTool{{ServerName: "files", ToolName: "files.create", Schema: objectSchema}}, ToolDiscoveryStatus: turingv1.ToolDiscoveryStatus_TOOL_DISCOVERY_STATUS_COMPLETE,
	}}}); err != nil {
		t.Fatal(err)
	}
	recvUntil(t, workerB, func(cmd *turingv1.RuntimeCommand) bool { return cmd.GetWorkerAccepted() != nil })
	assertEnabledToolNames(t, h.repo, []string{"files/files.create", "system/system.time"})

	duplicate, err := client.ConnectWorker(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := duplicate.Send(&turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_WorkerReady{WorkerReady: &turingv1.RuntimeWorkerReady{
		WorkerId: "worker-union-a", AgentId: turingv1.AgentId_AGENT_ID_GENERAL_ASSISTANT, MaxConcurrentRuns: 1,
		Tools: []*turingv1.DiscoveredTool{{ServerName: "custom", ToolName: "custom.replace", Schema: objectSchema}}, ToolDiscoveryStatus: turingv1.ToolDiscoveryStatus_TOOL_DISCOVERY_STATUS_COMPLETE,
	}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := duplicate.Recv(); status.Code(err) != codes.AlreadyExists {
		t.Fatalf("duplicate worker error = %v, want AlreadyExists", err)
	}
	assertEnabledToolNames(t, h.repo, []string{"files/files.create", "system/system.time"})

	if err := workerB.CloseSend(); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	for {
		tools, err := h.repo.ListEnabledTools(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if len(tools) == 1 && tools[0].ServerName == "system" && tools[0].ToolName == "system.time" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("registry after worker B disconnect = %+v, want only system/system.time", tools)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestRemoveDiscoveredToolsDoesNotDeleteReplacementOwner(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	oldConnection := &worker{}
	replacement := &worker{}
	h.service.persistDiscoveredTools(ctx, "worker-reconnect", oldConnection, []repository.DiscoveredTool{{
		ServerName: "system", ToolName: "system.time", SchemaJSON: `{}`, Policy: "safe",
	}})
	h.service.persistDiscoveredTools(ctx, "worker-reconnect", replacement, []repository.DiscoveredTool{{
		ServerName: "files", ToolName: "files.create", SchemaJSON: `{}`, Policy: "approval_required",
	}})
	h.service.removeDiscoveredTools("worker-reconnect", oldConnection)
	assertEnabledToolNames(t, h.repo, []string{"files/files.create"})
}

func TestConnectWorkerDeniesToolDiscoveredOnlyByAnotherWorker(t *testing.T) {
	h := newHarness(t)
	enqueued := h.enqueueRun(t, "worker scoped tools")
	client := h.runtimeClient(t)
	ctx, cancel := context.WithTimeout(h.internalContext(), 3*time.Second)
	defer cancel()
	objectSchema := &structpb.Struct{}

	workerA, err := client.ConnectWorker(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = workerA.CloseSend() }()
	if err := workerA.Send(&turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_WorkerReady{WorkerReady: &turingv1.RuntimeWorkerReady{
		WorkerId: "worker-scoped-a", AgentId: turingv1.AgentId_AGENT_ID_GENERAL_ASSISTANT, MaxConcurrentRuns: 1,
		Tools: []*turingv1.DiscoveredTool{{ServerName: "custom", ToolName: "custom.inspect", Schema: objectSchema}}, ToolDiscoveryStatus: turingv1.ToolDiscoveryStatus_TOOL_DISCOVERY_STATUS_COMPLETE,
	}}}); err != nil {
		t.Fatal(err)
	}
	assigned := recvUntil(t, workerA, func(cmd *turingv1.RuntimeCommand) bool {
		return cmd.GetRunAssigned() != nil
	}).GetRunAssigned()
	if assigned.GetRunId() != enqueued.RunID {
		t.Fatalf("assigned run = %q, want %q", assigned.GetRunId(), enqueued.RunID)
	}

	workerB, err := client.ConnectWorker(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = workerB.CloseSend() }()
	if err := workerB.Send(&turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_WorkerReady{WorkerReady: &turingv1.RuntimeWorkerReady{
		WorkerId: "worker-scoped-b", AgentId: turingv1.AgentId_AGENT_ID_GENERAL_ASSISTANT, MaxConcurrentRuns: 1,
		Tools: []*turingv1.DiscoveredTool{{ServerName: "system", ToolName: "system.time", Schema: objectSchema}}, ToolDiscoveryStatus: turingv1.ToolDiscoveryStatus_TOOL_DISCOVERY_STATUS_COMPLETE,
	}}}); err != nil {
		t.Fatal(err)
	}
	recvUntil(t, workerB, func(cmd *turingv1.RuntimeCommand) bool { return cmd.GetWorkerAccepted() != nil })

	if err := workerA.Send(&turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_ToolBeacon{ToolBeacon: &turingv1.ToolCallBeacon{
		RunId: assigned.GetRunId(), TraceId: assigned.GetTraceId(), ToolCallId: "call-cross-worker",
		AgentId: turingv1.AgentId_AGENT_ID_GENERAL_ASSISTANT, ServerName: "system", ToolName: "system.time",
		Phase: turingv1.ToolCallPhase_TOOL_CALL_PHASE_BEFORE, Args: objectSchema,
	}}}); err != nil {
		t.Fatal(err)
	}
	decision := recvUntil(t, workerA, func(cmd *turingv1.RuntimeCommand) bool {
		return cmd.GetToolPolicyDecision() != nil
	}).GetToolPolicyDecision()
	if decision.GetDecision() != turingv1.ToolPolicyDecision_DECISION_DENY || decision.GetReason() != "unknown_tool" {
		t.Fatalf("cross-worker decision = %+v, want unknown_tool denial", decision)
	}
}

func TestToolBeaconDeniesToolRemovedFromDiscoverySnapshot(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	if err := h.repo.UpsertTools(ctx, []repository.DiscoveredTool{{
		ServerName: "system", ToolName: "system.time", SchemaJSON: `{}`, Policy: "safe",
	}}); err != nil {
		t.Fatalf("seed registry: %v", err)
	}
	if err := h.repo.UpsertTools(ctx, nil); err != nil {
		t.Fatalf("remove discovered tool: %v", err)
	}
	enqueued := h.createRunningRunResult(t, "removed tool")
	run, err := h.repo.GetRun(ctx, enqueued.RunID)
	if err != nil {
		t.Fatal(err)
	}
	decision, err := h.service.handleToolBefore(ctx, &turingv1.ToolCallBeacon{
		RunId: enqueued.RunID, TraceId: enqueued.TraceID, ToolCallId: "call-removed-tool",
		AgentId: turingv1.AgentId_AGENT_ID_GENERAL_ASSISTANT, ServerName: "system", ToolName: "system.time",
		Phase: turingv1.ToolCallPhase_TOOL_CALL_PHASE_BEFORE, Args: &structpb.Struct{},
	}, run, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if decision.GetDecision() != turingv1.ToolPolicyDecision_DECISION_DENY || decision.GetReason() != "unknown_tool" {
		t.Fatalf("decision = %+v, want unknown_tool denial", decision)
	}
}

func assertEnabledToolNames(t *testing.T, repo *repository.Repository, want []string) {
	t.Helper()
	tools, err := repo.ListEnabledTools(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	got := make([]string, 0, len(tools))
	for _, tool := range tools {
		got = append(got, tool.ServerName+"/"+tool.ToolName)
	}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("enabled tools = %v, want %v", got, want)
	}
}

func TestAssignsPendingJobToReadyWorker(t *testing.T) {
	h := newHarness(t)
	sessionID := h.createSessionAndRun(t, "hello")
	_ = sessionID
	client := h.runtimeClient(t)
	stream, err := client.ConnectWorker(h.internalContext())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = stream.CloseSend() }()
	if err := stream.Send(&turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_WorkerReady{WorkerReady: &turingv1.RuntimeWorkerReady{WorkerId: "worker-1", AgentId: turingv1.AgentId_AGENT_ID_GENERAL_ASSISTANT, MaxConcurrentRuns: 1}}}); err != nil {
		t.Fatal(err)
	}
	cmd := recvUntil(t, stream, func(cmd *turingv1.RuntimeCommand) bool {
		return cmd.GetRunAssigned() != nil
	})
	if cmd.GetRunAssigned() == nil {
		t.Fatalf("command = %T, want run_assigned", cmd.Command)
	}
}

func TestWorkerHeartbeatRenewsActiveAssignmentLease(t *testing.T) {
	h := newHarnessWithDispatch(t, DispatchConfig{MaxConcurrentRuns: 1, LeaseDuration: time.Second})
	enqueued := h.enqueueRun(t, "heartbeat")
	stream, err := h.runtimeClient(t).ConnectWorker(h.internalContext())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = stream.CloseSend() }()
	if err := stream.Send(&turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_WorkerReady{WorkerReady: &turingv1.RuntimeWorkerReady{
		WorkerId: "worker-heartbeat", AgentId: turingv1.AgentId_AGENT_ID_GENERAL_ASSISTANT, MaxConcurrentRuns: 1,
	}}}); err != nil {
		t.Fatal(err)
	}
	recvUntil(t, stream, func(command *turingv1.RuntimeCommand) bool {
		assigned := command.GetRunAssigned()
		return assigned != nil && assigned.RunId == enqueued.RunID
	})
	var before int64
	if err := h.database.QueryRowContext(context.Background(), `
		SELECT execution_lease_expires_at_ns FROM agent_runs WHERE id = ?
	`, enqueued.RunID).Scan(&before); err != nil {
		t.Fatal(err)
	}
	time.Sleep(2 * time.Millisecond)
	if err := stream.Send(&turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_Heartbeat{Heartbeat: &turingv1.RuntimeHeartbeat{
		WorkerId: "worker-heartbeat",
	}}}); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	for {
		var after int64
		if err := h.database.QueryRowContext(context.Background(), `
			SELECT execution_lease_expires_at_ns FROM agent_runs WHERE id = ?
		`, enqueued.RunID).Scan(&after); err != nil {
			t.Fatal(err)
		}
		if after > before {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("heartbeat did not extend lease beyond %d", before)
		}
		time.Sleep(time.Millisecond)
	}
}

func TestDispatchPendingPublishesRunStartedEvent(t *testing.T) {
	h := newHarness(t)
	sessionID := h.createSessionAndRun(t, "hello")
	ch, unsubscribe := h.bus.Subscribe(sessionID)
	defer unsubscribe()
	client := h.runtimeClient(t)
	stream, err := client.ConnectWorker(h.internalContext())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = stream.CloseSend() }()
	if err := stream.Send(&turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_WorkerReady{WorkerReady: &turingv1.RuntimeWorkerReady{WorkerId: "worker-started", AgentId: turingv1.AgentId_AGENT_ID_GENERAL_ASSISTANT, MaxConcurrentRuns: 1}}}); err != nil {
		t.Fatal(err)
	}
	assigned := recvUntil(t, stream, func(cmd *turingv1.RuntimeCommand) bool {
		return cmd.GetRunAssigned() != nil
	}).GetRunAssigned()

	deadline := time.After(2 * time.Second)
	for {
		select {
		case <-deadline:
			t.Fatal("timed out waiting for agent.run.started event")
		case event := <-ch:
			if event.Type != "agent.run.started" || event.RunID != assigned.RunId || event.TraceID != assigned.TraceId {
				continue
			}
			var payload map[string]any
			if err := json.Unmarshal([]byte(event.PayloadJSON), &payload); err != nil {
				t.Fatalf("decode started payload: %v", err)
			}
			if payload["runId"] != assigned.RunId || payload["jobId"] != assigned.JobId || payload["status"] != "running" || payload["agentId"] != "general_assistant" || payload["attempt"] != float64(assigned.Attempt) {
				t.Fatalf("bad started payload: %+v", payload)
			}
			return
		}
	}
}

func TestCancelRunSendsRuntimeCommand(t *testing.T) {
	h := newHarness(t)
	enqueued := h.enqueueRun(t, "cancel me")
	client := h.runtimeClient(t)
	stream, err := client.ConnectWorker(h.internalContext())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = stream.CloseSend() }()
	if err := stream.Send(&turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_WorkerReady{WorkerReady: &turingv1.RuntimeWorkerReady{WorkerId: "worker-1", AgentId: turingv1.AgentId_AGENT_ID_GENERAL_ASSISTANT, MaxConcurrentRuns: 1}}}); err != nil {
		t.Fatal(err)
	}
	assigned := recvUntil(t, stream, func(cmd *turingv1.RuntimeCommand) bool {
		assigned := cmd.GetRunAssigned()
		return assigned != nil && assigned.RunId == enqueued.RunID
	}).GetRunAssigned()
	h.service.CancelRun(context.Background(), assigned.RunId, "client_cancelled")
	recvUntil(t, stream, func(cmd *turingv1.RuntimeCommand) bool {
		cancel := cmd.GetRunCancelled()
		return cancel != nil && cancel.RunId == assigned.RunId
	})
}

func TestCancelRunOnlySendsToAssignedWorker(t *testing.T) {
	h := newHarness(t)
	first := h.enqueueRun(t, "cancel first")
	second := h.enqueueRun(t, "keep second")
	client := h.runtimeClient(t)
	ctx, cancel := context.WithTimeout(h.internalContext(), 2*time.Second)
	defer cancel()
	workerOne, err := client.ConnectWorker(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = workerOne.CloseSend() }()
	if err := workerOne.Send(&turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_WorkerReady{WorkerReady: &turingv1.RuntimeWorkerReady{WorkerId: "worker-cancel-owner", AgentId: turingv1.AgentId_AGENT_ID_GENERAL_ASSISTANT, MaxConcurrentRuns: 1}}}); err != nil {
		t.Fatal(err)
	}
	recvUntil(t, workerOne, func(cmd *turingv1.RuntimeCommand) bool {
		assigned := cmd.GetRunAssigned()
		return assigned != nil && assigned.RunId == first.RunID
	})
	workerTwo, err := client.ConnectWorker(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = workerTwo.CloseSend() }()
	if err := workerTwo.Send(&turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_WorkerReady{WorkerReady: &turingv1.RuntimeWorkerReady{WorkerId: "worker-cancel-bystander", AgentId: turingv1.AgentId_AGENT_ID_GENERAL_ASSISTANT, MaxConcurrentRuns: 1}}}); err != nil {
		t.Fatal(err)
	}
	recvUntil(t, workerTwo, func(cmd *turingv1.RuntimeCommand) bool {
		assigned := cmd.GetRunAssigned()
		return assigned != nil && assigned.RunId == second.RunID
	})

	h.service.CancelRun(context.Background(), first.RunID, "client_cancelled")
	recvUntil(t, workerOne, func(cmd *turingv1.RuntimeCommand) bool {
		cancel := cmd.GetRunCancelled()
		return cancel != nil && cancel.RunId == first.RunID
	})
	received := make(chan struct {
		cmd *turingv1.RuntimeCommand
		err error
	}, 1)
	go func() {
		cmd, err := workerTwo.Recv()
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
		if cancel := result.cmd.GetRunCancelled(); cancel != nil {
			t.Fatalf("bystander worker received cancellation: %+v", cancel)
		}
	case <-time.After(100 * time.Millisecond):
	}
}

func TestCancelRunDoesNotQueueForFutureWorker(t *testing.T) {
	h := newHarness(t)
	runID := h.createRunningRun(t, "already cancelled")
	h.service.CancelRun(context.Background(), runID, "client_cancelled")

	client := h.runtimeClient(t)
	ctx, cancel := context.WithCancel(h.internalContext())
	defer cancel()
	stream, err := client.ConnectWorker(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = stream.CloseSend() }()
	if err := stream.Send(&turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_WorkerReady{WorkerReady: &turingv1.RuntimeWorkerReady{WorkerId: "worker-after-cancel", AgentId: turingv1.AgentId_AGENT_ID_GENERAL_ASSISTANT, MaxConcurrentRuns: 1}}}); err != nil {
		t.Fatal(err)
	}
	accepted := recvUntil(t, stream, func(cmd *turingv1.RuntimeCommand) bool {
		return cmd.GetWorkerAccepted() != nil
	})
	if accepted.GetWorkerAccepted().WorkerId != "worker-after-cancel" {
		t.Fatalf("accepted worker = %+v", accepted.GetWorkerAccepted())
	}

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
	case result := <-received:
		if result.err != nil {
			t.Fatal(result.err)
		}
		if cancel := result.cmd.GetRunCancelled(); cancel != nil && cancel.RunId == runID {
			t.Fatalf("received queued cancellation for future worker: %+v", cancel)
		}
	case <-time.After(100 * time.Millisecond):
	}
}

func TestDuplicateWorkerIDIsRejected(t *testing.T) {
	h := newHarness(t)
	client := h.runtimeClient(t)
	first, err := client.ConnectWorker(h.internalContext())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = first.CloseSend() }()
	ready := &turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_WorkerReady{WorkerReady: &turingv1.RuntimeWorkerReady{WorkerId: "worker-1", AgentId: turingv1.AgentId_AGENT_ID_GENERAL_ASSISTANT, MaxConcurrentRuns: 1}}}
	if err := first.Send(ready); err != nil {
		t.Fatal(err)
	}
	recvUntil(t, first, func(cmd *turingv1.RuntimeCommand) bool {
		return cmd.GetWorkerAccepted() != nil
	})

	second, err := client.ConnectWorker(h.internalContext())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = second.CloseSend() }()
	if err := second.Send(ready); err != nil {
		t.Fatal(err)
	}
	_, err = second.Recv()
	if status.Code(err) != codes.AlreadyExists {
		t.Fatalf("duplicate worker error = %v, want AlreadyExists", err)
	}
}

func TestConnectWorkerFencesJobWhenAssignmentSendFails(t *testing.T) {
	h := newHarness(t)
	session, err := h.repo.CreateSession(context.Background(), "Runtime")
	if err != nil {
		t.Fatal(err)
	}
	enqueued, err := h.repo.EnqueueUserMessage(context.Background(), repository.EnqueueUserMessageInput{
		SessionID:     session.SessionID,
		Content:       "send failure",
		AgentID:       "general_assistant",
		ModelProvider: "ollama",
		Model:         "llama3.2",
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	err = h.service.ConnectWorker(&failingAssignmentStream{
		ctx: ctx,
		ready: &turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_WorkerReady{WorkerReady: &turingv1.RuntimeWorkerReady{
			WorkerId:          "worker-send-fails",
			AgentId:           turingv1.AgentId_AGENT_ID_GENERAL_ASSISTANT,
			MaxConcurrentRuns: 1,
		}}},
	})
	if err == nil {
		t.Fatal("ConnectWorker succeeded, want assignment send failure")
	}
	run, err := h.repo.GetRun(context.Background(), enqueued.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if run.Status != "recovering" || !run.ExecutionActive {
		t.Fatalf("run = %+v, want recovering fenced after ambiguous send failure", run)
	}
	var jobStatus string
	var leaseOwner sql.NullString
	if err := h.database.QueryRowContext(context.Background(), `SELECT status, lease_owner FROM jobs WHERE id = ?`, enqueued.JobID).Scan(&jobStatus, &leaseOwner); err != nil {
		t.Fatal(err)
	}
	if jobStatus != "in_progress" || !leaseOwner.Valid {
		t.Fatalf("job after send failure: status=%q lease_owner=%q", jobStatus, leaseOwner.String)
	}
}

func TestConnectWorkerTerminalizesApprovalWhenDecisionSendFails(t *testing.T) {
	h := newHarness(t)
	enqueued := h.enqueueRun(t, "approval decision send failure")
	published, unsubscribe := h.bus.Subscribe(enqueued.SessionID)
	defer unsubscribe()
	args, err := structpb.NewStruct(map[string]any{"path": "note.txt", "content": "hello"})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err = h.service.ConnectWorker(&failingApprovalDecisionStream{
		ctx:      ctx,
		cancel:   cancel,
		assigned: make(chan struct{}),
		ready: &turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_WorkerReady{WorkerReady: &turingv1.RuntimeWorkerReady{
			WorkerId:          "worker-decision-send-fails",
			AgentId:           turingv1.AgentId_AGENT_ID_GENERAL_ASSISTANT,
			MaxConcurrentRuns: 1,
		}}},
		beacon: &turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_ToolBeacon{ToolBeacon: &turingv1.ToolCallBeacon{
			RunId:      enqueued.RunID,
			TraceId:    enqueued.TraceID,
			ToolCallId: "call_delivery_failure",
			AgentId:    turingv1.AgentId_AGENT_ID_GENERAL_ASSISTANT,
			ServerName: "files",
			ToolName:   "files.update",
			Phase:      turingv1.ToolCallPhase_TOOL_CALL_PHASE_BEFORE,
			Args:       args,
		}}},
	})
	if err == nil || !strings.Contains(err.Error(), "tool policy decision send failed") {
		t.Fatalf("ConnectWorker error = %v, want tool policy decision send failure", err)
	}

	run, err := h.repo.GetRun(context.Background(), enqueued.RunID)
	if err != nil {
		t.Fatal(err)
	}
	var runCode sql.NullString
	if err := h.database.QueryRowContext(context.Background(), `SELECT error_code FROM agent_runs WHERE id = ?`, enqueued.RunID).Scan(&runCode); err != nil {
		t.Fatal(err)
	}
	if run.Status != "failed" || runCode.String != "approval_delivery_failed" {
		t.Fatalf("run status/code = %q/%q, want failed/approval_delivery_failed", run.Status, runCode.String)
	}
	approval, err := h.repo.GetApprovalByToolCall(context.Background(), enqueued.RunID, "call_delivery_failure")
	if err != nil {
		t.Fatal(err)
	}
	if approval.Status != "denied" {
		t.Fatalf("approval status = %q, want denied", approval.Status)
	}
	var jobStatus, jobCode, toolCallStatus string
	if err := h.database.QueryRowContext(context.Background(), `SELECT status, error_code FROM jobs WHERE id = ?`, enqueued.JobID).Scan(&jobStatus, &jobCode); err != nil {
		t.Fatal(err)
	}
	if err := h.database.QueryRowContext(context.Background(), `SELECT status FROM tool_calls WHERE id = ?`, "call_delivery_failure").Scan(&toolCallStatus); err != nil {
		t.Fatal(err)
	}
	if jobStatus != "failed" || jobCode != "approval_delivery_failed" || toolCallStatus != "failed" {
		t.Fatalf("terminal states job=%q/%q tool_call=%q, want failed/approval_delivery_failed/failed", jobStatus, jobCode, toolCallStatus)
	}
	var terminalEvents int
	if err := h.database.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM events WHERE run_id = ? AND type = 'agent.run.failed'`, enqueued.RunID).Scan(&terminalEvents); err != nil {
		t.Fatal(err)
	}
	if terminalEvents != 1 {
		t.Fatalf("agent.run.failed event count = %d, want 1", terminalEvents)
	}
	var publishedTypes []string
	for len(publishedTypes) < 3 {
		event := recvBusEvent(t, published, func(event events.Event) bool {
			return event.RunID == enqueued.RunID &&
				(event.Type == "approval.denied" || event.Type == "tool.call.failed" || event.Type == "agent.run.failed")
		})
		if event.TraceID != enqueued.TraceID {
			t.Fatalf("%s trace_id = %q, want %q", event.Type, event.TraceID, enqueued.TraceID)
		}
		publishedTypes = append(publishedTypes, event.Type)
	}
	if want := []string{"approval.denied", "tool.call.failed", "agent.run.failed"}; !reflect.DeepEqual(publishedTypes, want) {
		t.Fatalf("published delivery-failure events = %v, want %v", publishedTypes, want)
	}
}

func TestConnectWorkerFencesTerminalDecisionFailureUntilRecovery(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	session, err := h.repo.CreateSession(ctx, "Decision delivery recovery")
	if err != nil {
		t.Fatal(err)
	}
	first, err := h.repo.EnqueueUserMessage(ctx, repository.EnqueueUserMessageInput{
		SessionID: session.SessionID, Content: "first approval", AgentID: "general_assistant", ModelProvider: "ollama", Model: "llama3.2",
	})
	if err != nil {
		t.Fatal(err)
	}
	second, err := h.repo.EnqueueUserMessage(ctx, repository.EnqueueUserMessageInput{
		SessionID: session.SessionID, Content: "second after decision failure", AgentID: "general_assistant", ModelProvider: "ollama", Model: "llama3.2",
	})
	if err != nil {
		t.Fatal(err)
	}
	streamCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	err = h.service.ConnectWorker(&failingApprovalDecisionStream{
		ctx:      streamCtx,
		cancel:   cancel,
		assigned: make(chan struct{}),
		ready: &turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_WorkerReady{WorkerReady: &turingv1.RuntimeWorkerReady{
			WorkerId: "worker-terminal-decision-send-fails", AgentId: turingv1.AgentId_AGENT_ID_GENERAL_ASSISTANT, MaxConcurrentRuns: 1,
		}}},
		beacon: &turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_ToolBeacon{ToolBeacon: &turingv1.ToolCallBeacon{
			RunId: first.RunID, TraceId: first.TraceID, ToolCallId: "call_terminal_decision_failure",
			AgentId: turingv1.AgentId_AGENT_ID_GENERAL_ASSISTANT, ServerName: "files", ToolName: "files.update",
			Phase: turingv1.ToolCallPhase_TOOL_CALL_PHASE_BEFORE, Args: mustStruct(t, map[string]any{"path": "note.txt", "content": "hello"}),
		}}},
	})
	if err == nil || !strings.Contains(err.Error(), "tool policy decision send failed") {
		t.Fatalf("ConnectWorker error = %v, want tool policy decision send failure", err)
	}
	firstRun, err := h.repo.GetRun(ctx, first.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if firstRun.Status != "failed" || !firstRun.ExecutionActive || firstRun.ExecutionState != "uncertain" {
		t.Fatalf("terminal decision failure state = %+v, want failed active uncertain fence", firstRun)
	}

	client := h.runtimeClient(t)
	freshCtx, cancelFresh := context.WithTimeout(h.internalContext(), 2*time.Second)
	defer cancelFresh()
	fresh, err := client.ConnectWorker(freshCtx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = fresh.CloseSend() }()
	if err := fresh.Send(&turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_WorkerReady{WorkerReady: &turingv1.RuntimeWorkerReady{
		WorkerId: "worker-after-terminal-decision-failure", AgentId: turingv1.AgentId_AGENT_ID_GENERAL_ASSISTANT, MaxConcurrentRuns: 1,
	}}}); err != nil {
		t.Fatal(err)
	}
	recvUntil(t, fresh, func(cmd *turingv1.RuntimeCommand) bool {
		return cmd.GetWorkerAccepted() != nil
	})
	time.Sleep(20 * time.Millisecond)
	secondRun, err := h.repo.GetRun(ctx, second.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if secondRun.Status != "queued" {
		t.Fatalf("fresh worker claimed %q before stale execution recovery", secondRun.RunID)
	}
	expired := time.Now().Add(-time.Second)
	if _, err := h.database.ExecContext(ctx, `
		UPDATE agent_runs
		SET execution_lease_expires_at = ?, execution_lease_expires_at_ns = ?
		WHERE id = ?
	`, expired.Format("2006-01-02T15:04:05.000000000Z"), expired.UnixNano(), first.RunID); err != nil {
		t.Fatal(err)
	}
	if err := h.service.RecoverOrphanedAssignments(ctx); err != nil {
		t.Fatal(err)
	}
	assigned := recvUntil(t, fresh, func(cmd *turingv1.RuntimeCommand) bool {
		return cmd.GetRunAssigned() != nil
	}).GetRunAssigned()
	if assigned.RunId != second.RunID {
		t.Fatalf("fresh worker assigned run %q, want later same-session run %q", assigned.RunId, second.RunID)
	}
	if err := fresh.Send(&turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_RunCompleted{RunCompleted: &turingv1.RuntimeRunCompleted{
		RunId: second.RunID, AssistantMessageId: second.AssistantMessageID, Content: "done",
	}}}); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		run, getErr := h.repo.GetRun(ctx, second.RunID)
		if getErr == nil && run.Status == "completed" {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("fresh worker did not complete later same-session run")
}

func TestToolBeaconTerminalizesPostCommitApprovalCreationFailure(t *testing.T) {
	h := newHarness(t)
	enqueued := h.createRunningRunResult(t, "approval creation event failure")
	if _, err := h.database.ExecContext(context.Background(), `
		CREATE TRIGGER fail_approval_requested_event
		BEFORE INSERT ON events
		WHEN NEW.type = 'approval.requested'
		BEGIN
			SELECT RAISE(ABORT, 'append approval requested event failed');
		END
	`); err != nil {
		t.Fatal(err)
	}

	published, unsubscribe := h.bus.Subscribe(enqueued.SessionID)
	defer unsubscribe()
	args, err := structpb.NewStruct(map[string]any{"path": "note.txt", "content": "hello"})
	if err != nil {
		t.Fatal(err)
	}

	err = h.service.applyUpdate(context.Background(), &turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_ToolBeacon{ToolBeacon: &turingv1.ToolCallBeacon{
		RunId:      enqueued.RunID,
		TraceId:    enqueued.TraceID,
		ToolCallId: "call_post_commit_failure",
		AgentId:    turingv1.AgentId_AGENT_ID_GENERAL_ASSISTANT,
		ServerName: "files",
		ToolName:   "files.update",
		Phase:      turingv1.ToolCallPhase_TOOL_CALL_PHASE_BEFORE,
		Args:       args,
	}}})
	if err == nil || !strings.Contains(err.Error(), "append approval requested event failed") {
		t.Fatalf("tool beacon error = %v, want post-commit approval event failure", err)
	}

	run, err := h.repo.GetRun(context.Background(), enqueued.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.repo.GetApprovalByToolCall(context.Background(), enqueued.RunID, "call_post_commit_failure"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("approval after atomic creation failure = %v, want no approval row", err)
	}
	var runCode, jobCode sql.NullString
	var jobStatus, toolCallStatus string
	if err := h.database.QueryRowContext(context.Background(), `SELECT error_code FROM agent_runs WHERE id = ?`, enqueued.RunID).Scan(&runCode); err != nil {
		t.Fatal(err)
	}
	if err := h.database.QueryRowContext(context.Background(), `SELECT status, error_code FROM jobs WHERE id = ?`, enqueued.JobID).Scan(&jobStatus, &jobCode); err != nil {
		t.Fatal(err)
	}
	if err := h.database.QueryRowContext(context.Background(), `SELECT status FROM tool_calls WHERE id = ?`, "call_post_commit_failure").Scan(&toolCallStatus); err != nil {
		t.Fatal(err)
	}
	if run.Status != "failed" || runCode.String != "approval_delivery_failed" ||
		jobStatus != "failed" || jobCode.String != "approval_delivery_failed" || toolCallStatus != "failed" {
		t.Fatalf("terminal states run=%q/%q job=%q/%q tool_call=%q", run.Status, runCode.String, jobStatus, jobCode.String, toolCallStatus)
	}
	var terminalEvents int
	if err := h.database.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM events WHERE run_id = ? AND type = 'agent.run.failed'`, enqueued.RunID).Scan(&terminalEvents); err != nil {
		t.Fatal(err)
	}
	if terminalEvents != 1 {
		t.Fatalf("agent.run.failed event count = %d, want 1", terminalEvents)
	}
	_ = recvBusEvent(t, published, func(event events.Event) bool {
		return event.RunID == enqueued.RunID && event.Type == "agent.run.failed"
	})
}

func TestApprovalCreationFailureKeepsCapacityUntilWorkerExitAck(t *testing.T) {
	h := newHarnessWithDispatch(t, DispatchConfig{MaxConcurrentRuns: 1})
	ctx := context.Background()
	session, err := h.repo.CreateSession(ctx, "Approval creation failure")
	if err != nil {
		t.Fatal(err)
	}
	first, err := h.repo.EnqueueUserMessage(ctx, repository.EnqueueUserMessageInput{
		SessionID: session.SessionID, Content: "first approval", AgentID: "general_assistant", ModelProvider: "ollama", Model: "llama3.2",
	})
	if err != nil {
		t.Fatal(err)
	}
	second, err := h.repo.EnqueueUserMessage(ctx, repository.EnqueueUserMessageInput{
		SessionID: session.SessionID, Content: "later same-session work", AgentID: "general_assistant", ModelProvider: "ollama", Model: "llama3.2",
	})
	if err != nil {
		t.Fatal(err)
	}
	global := h.enqueueRun(t, "later global work")
	client := h.runtimeClient(t)
	firstWorker, err := client.ConnectWorker(h.internalContext())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = firstWorker.CloseSend() }()
	if err := firstWorker.Send(workerReady("worker-approval-failure")); err != nil {
		t.Fatal(err)
	}
	assigned := recvUntil(t, firstWorker, func(command *turingv1.RuntimeCommand) bool {
		return command.GetRunAssigned() != nil && command.GetRunAssigned().GetRunId() == first.RunID
	}).GetRunAssigned()
	secondWorker, err := client.ConnectWorker(h.internalContext())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = secondWorker.CloseSend() }()
	if err := secondWorker.Send(workerReady("worker-capacity-waiter")); err != nil {
		t.Fatal(err)
	}
	recvUntil(t, secondWorker, func(command *turingv1.RuntimeCommand) bool {
		return command.GetWorkerAccepted() != nil
	})
	if _, err := h.database.ExecContext(ctx, `
		CREATE TRIGGER fail_approval_requested_capacity
		BEFORE INSERT ON events
		WHEN NEW.type = 'approval.requested'
		BEGIN
			SELECT RAISE(ABORT, 'approval request event unavailable');
		END
	`); err != nil {
		t.Fatal(err)
	}
	if err := firstWorker.Send(&turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_ToolBeacon{ToolBeacon: &turingv1.ToolCallBeacon{
		RunId:      assigned.GetRunId(),
		TraceId:    assigned.GetTraceId(),
		ToolCallId: "call_capacity_terminal",
		AgentId:    turingv1.AgentId_AGENT_ID_GENERAL_ASSISTANT,
		ServerName: "files",
		ToolName:   "files.update",
		Phase:      turingv1.ToolCallPhase_TOOL_CALL_PHASE_BEFORE,
		Args:       mustStruct(t, map[string]any{"path": "note.txt", "content": "hello"}),
	}}}); err != nil {
		t.Fatal(err)
	}
	decision := recvUntil(t, firstWorker, func(command *turingv1.RuntimeCommand) bool {
		return command.GetToolPolicyDecision() != nil && command.GetToolPolicyDecision().GetToolCallId() == "call_capacity_terminal"
	}).GetToolPolicyDecision()
	if !decision.GetTerminalRun() || decision.GetDecision() != turingv1.ToolPolicyDecision_DECISION_DENY {
		t.Fatalf("approval failure decision = %+v, want terminal deny", decision)
	}
	run, err := h.repo.GetRun(ctx, first.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if run.Status != "failed" || !run.ExecutionActive || run.ExecutionAttemptID == "" {
		t.Fatalf("terminalized run = %+v, want failed active owned attempt", run)
	}
	time.Sleep(50 * time.Millisecond)
	for _, queued := range []repository.EnqueueUserMessageResult{second, global} {
		candidate, getErr := h.repo.GetRun(ctx, queued.RunID)
		if getErr != nil {
			t.Fatal(getErr)
		}
		if candidate.Status != "queued" {
			t.Fatalf("run %q status = %q, want queued until old worker exits", queued.RunID, candidate.Status)
		}
	}
	if err := firstWorker.Send(&turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_RunCancelledAck{
		RunCancelledAck: &turingv1.RuntimeCancelledAck{RunId: first.RunID},
	}}); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		candidate, getErr := h.repo.GetRun(ctx, second.RunID)
		if getErr == nil && candidate.Status == "running" {
			globalRun, globalErr := h.repo.GetRun(ctx, global.RunID)
			if globalErr != nil {
				t.Fatal(globalErr)
			}
			if globalRun.Status != "queued" {
				t.Fatalf("global run status = %q, want queued behind recovered capacity", globalRun.Status)
			}
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("same-session run was not assigned after worker exit acknowledgement")
}

func TestToolBeaconTerminalizesPostCommitApprovalCreationFailureAfterCancellation(t *testing.T) {
	h := newHarness(t)
	enqueued := h.createRunningRunResult(t, "cancelled post-commit approval failure")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	service := New(h.repo, h.bus, cancellingApprovalCreator{repo: h.repo, cancel: cancel})
	published, unsubscribe := h.bus.Subscribe(enqueued.SessionID)
	defer unsubscribe()

	err := service.applyUpdate(ctx, &turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_ToolBeacon{ToolBeacon: &turingv1.ToolCallBeacon{
		RunId:      enqueued.RunID,
		TraceId:    enqueued.TraceID,
		ToolCallId: "call_cancelled_post_commit_failure",
		AgentId:    turingv1.AgentId_AGENT_ID_GENERAL_ASSISTANT,
		ServerName: "files",
		ToolName:   "files.update",
		Phase:      turingv1.ToolCallPhase_TOOL_CALL_PHASE_BEFORE,
		Args:       mustStruct(t, map[string]any{"path": "note.txt", "content": "hello"}),
	}}})
	if err == nil || !strings.Contains(err.Error(), "post-commit approval creation failed") {
		t.Fatalf("tool beacon error = %v, want post-commit approval creation failure", err)
	}

	approval, err := h.repo.GetApprovalByToolCall(context.Background(), enqueued.RunID, "call_cancelled_post_commit_failure")
	if err != nil {
		t.Fatal(err)
	}
	var runCode, jobCode sql.NullString
	var runStatus, jobStatus, toolCallStatus string
	if err := h.database.QueryRowContext(context.Background(), `SELECT status, error_code FROM agent_runs WHERE id = ?`, enqueued.RunID).Scan(&runStatus, &runCode); err != nil {
		t.Fatal(err)
	}
	if err := h.database.QueryRowContext(context.Background(), `SELECT status, error_code FROM jobs WHERE id = ?`, enqueued.JobID).Scan(&jobStatus, &jobCode); err != nil {
		t.Fatal(err)
	}
	if err := h.database.QueryRowContext(context.Background(), `SELECT status FROM tool_calls WHERE id = ?`, "call_cancelled_post_commit_failure").Scan(&toolCallStatus); err != nil {
		t.Fatal(err)
	}
	if runStatus != "failed" || runCode.String != "approval_delivery_failed" || approval.Status != "denied" ||
		jobStatus != "failed" || jobCode.String != "approval_delivery_failed" || toolCallStatus != "failed" {
		t.Fatalf("terminal states run=%q/%q approval=%q job=%q/%q tool_call=%q", runStatus, runCode.String, approval.Status, jobStatus, jobCode.String, toolCallStatus)
	}
	_ = recvBusEvent(t, published, func(event events.Event) bool {
		return event.RunID == enqueued.RunID && event.Type == "agent.run.failed"
	})
}

type failingAssignmentStream struct {
	grpc.ServerStream
	ctx       context.Context
	ready     *turingv1.RuntimeUpdate
	readySent bool
}

func (s *failingAssignmentStream) Send(cmd *turingv1.RuntimeCommand) error {
	if cmd.GetWorkerAccepted() != nil {
		return nil
	}
	if cmd.GetRunAssigned() != nil {
		return errors.New("assignment send failed")
	}
	return nil
}

func (s *failingAssignmentStream) Recv() (*turingv1.RuntimeUpdate, error) {
	if !s.readySent {
		s.readySent = true
		return s.ready, nil
	}
	<-s.ctx.Done()
	return nil, s.ctx.Err()
}

func (s *failingAssignmentStream) Context() context.Context { return s.ctx }

// failingApprovalDecisionStream is a worker whose tool-policy decision cannot
// be delivered.
//
// Its beacon deliberately waits for the assignment to be sent. A real worker
// cannot beacon for a run it has not been handed, and without that wait the
// beacon races the assignment for the worker's update lock: whichever wins
// decides whether the run is still 'running' when BeginAssignmentSend guards
// it, so the assignment is fenced or delivered depending on scheduling alone.
// The wait models the protocol's own causality rather than the fast path CI
// happened to take.
type failingApprovalDecisionStream struct {
	grpc.ServerStream
	ctx          context.Context
	cancel       context.CancelFunc
	ready        *turingv1.RuntimeUpdate
	beacon       *turingv1.RuntimeUpdate
	assigned     chan struct{}
	assignedOnce sync.Once
	readySent    bool
	beaconSent   bool
}

func (s *failingApprovalDecisionStream) Send(cmd *turingv1.RuntimeCommand) error {
	if cmd.GetRunAssigned() != nil {
		s.assignedOnce.Do(func() { close(s.assigned) })
		return nil
	}
	if cmd.GetToolPolicyDecision() != nil {
		if s.cancel != nil {
			s.cancel()
		}
		return errors.New("tool policy decision send failed")
	}
	return nil
}

func (s *failingApprovalDecisionStream) Recv() (*turingv1.RuntimeUpdate, error) {
	if !s.readySent {
		s.readySent = true
		return s.ready, nil
	}
	if !s.beaconSent {
		s.beaconSent = true
		select {
		case <-s.assigned:
		case <-s.ctx.Done():
			return nil, s.ctx.Err()
		}
		return s.beacon, nil
	}
	<-s.ctx.Done()
	return nil, s.ctx.Err()
}

func (s *failingApprovalDecisionStream) Context() context.Context { return s.ctx }

type cancellingApprovalCreator struct {
	repo   *repository.Repository
	cancel context.CancelFunc
}

func (c cancellingApprovalCreator) CreateApprovalForTool(ctx context.Context, runID string, toolCallID string, agentID string, toolName string, _ map[string]any) (string, error) {
	approval, err := c.repo.CreateApproval(ctx, runID, toolCallID, agentID, toolName, `{}`, "sha256:cancelled", "2099-01-01T00:00:00Z")
	if err != nil {
		return "", err
	}
	c.cancel()
	return approval.ApprovalID, errors.New("post-commit approval creation failed")
}

func TestDispatchPendingRespectsWorkerMaxConcurrentRuns(t *testing.T) {
	h := newHarness(t)
	first := h.enqueueRun(t, "first")
	client := h.runtimeClient(t)
	ctx, cancel := context.WithTimeout(h.internalContext(), 2*time.Second)
	defer cancel()
	stream, err := client.ConnectWorker(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = stream.CloseSend() }()
	if err := stream.Send(&turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_WorkerReady{WorkerReady: &turingv1.RuntimeWorkerReady{WorkerId: "worker-capacity", AgentId: turingv1.AgentId_AGENT_ID_GENERAL_ASSISTANT, MaxConcurrentRuns: 1}}}); err != nil {
		t.Fatal(err)
	}
	recvUntil(t, stream, func(cmd *turingv1.RuntimeCommand) bool {
		assigned := cmd.GetRunAssigned()
		return assigned != nil && assigned.RunId == first.RunID
	})
	second := h.enqueueRun(t, "second")
	if err := h.service.DispatchPending(context.Background()); err != nil {
		t.Fatal(err)
	}
	run, err := h.repo.GetRun(context.Background(), second.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if run.Status != "queued" {
		t.Fatalf("second run status = %q, want queued while worker is at capacity", run.Status)
	}

	if err := stream.Send(&turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_RunCompleted{RunCompleted: &turingv1.RuntimeRunCompleted{
		RunId:              first.RunID,
		AssistantMessageId: first.AssistantMessageID,
		Content:            "done",
	}}}); err != nil {
		t.Fatal(err)
	}
	recvUntil(t, stream, func(cmd *turingv1.RuntimeCommand) bool {
		assigned := cmd.GetRunAssigned()
		return assigned != nil && assigned.RunId == second.RunID
	})
}

func TestConnectWorkerHonorsMaxConcurrentAboveDefaultBuffer(t *testing.T) {
	h := newHarness(t)
	const runCount = 9
	enqueued := make(map[string]repository.EnqueueUserMessageResult, runCount)
	for i := 0; i < runCount; i++ {
		run := h.enqueueRun(t, fmt.Sprintf("run %d", i))
		enqueued[run.RunID] = run
	}
	client := h.runtimeClient(t)
	ctx, cancel := context.WithTimeout(h.internalContext(), 2*time.Second)
	defer cancel()
	stream, err := client.ConnectWorker(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = stream.CloseSend() }()
	if err := stream.Send(&turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_WorkerReady{WorkerReady: &turingv1.RuntimeWorkerReady{WorkerId: "worker-large-capacity", AgentId: turingv1.AgentId_AGENT_ID_GENERAL_ASSISTANT, MaxConcurrentRuns: runCount}}}); err != nil {
		t.Fatal(err)
	}
	assigned := map[string]bool{}
	for len(assigned) < runCount {
		cmd := recvUntil(t, stream, func(cmd *turingv1.RuntimeCommand) bool {
			return cmd.GetRunAssigned() != nil
		})
		runID := cmd.GetRunAssigned().RunId
		if _, ok := enqueued[runID]; !ok {
			t.Fatalf("unexpected run assigned: %+v", cmd.GetRunAssigned())
		}
		assigned[runID] = true
	}
}

func TestRuntimeRejectsGenericTerminalEvents(t *testing.T) {
	h := newHarness(t)
	enqueued := h.createRunningRunResult(t, "generic terminal")
	err := h.service.applyUpdate(context.Background(), &turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_Event{Event: &turingv1.TuringEvent{
		SessionId: enqueued.SessionID,
		RunId:     enqueued.RunID,
		TraceId:   enqueued.TraceID,
		Type:      turingv1.TuringEventType_TURING_EVENT_TYPE_AGENT_RUN_COMPLETED,
	}}})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("generic terminal event error = %v, want InvalidArgument", err)
	}
	run, err := h.repo.GetRun(context.Background(), enqueued.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if run.Status != "running" {
		t.Fatalf("run status = %q, want running", run.Status)
	}
}

func TestRuntimeEventUsesPersistedRunSessionAndTrace(t *testing.T) {
	h := newHarness(t)
	enqueued := h.createRunningRunResult(t, "event session")
	otherSession, err := h.repo.CreateSession(context.Background(), "Other")
	if err != nil {
		t.Fatal(err)
	}
	payload, err := structpb.NewStruct(map[string]any{"delta": "hi"})
	if err != nil {
		t.Fatal(err)
	}
	if err := h.service.applyUpdate(context.Background(), &turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_Event{Event: &turingv1.TuringEvent{
		SessionId: otherSession.SessionID,
		RunId:     enqueued.RunID,
		TraceId:   "trace_spoofed",
		Type:      turingv1.TuringEventType_TURING_EVENT_TYPE_MESSAGE_DELTA,
		Payload:   payload,
	}}}); err != nil {
		t.Fatal(err)
	}
	replayed, _, err := h.repo.ReplayEvents(context.Background(), enqueued.SessionID, enqueued.QueuedEvent.Sequence, 10)
	if err != nil {
		t.Fatal(err)
	}
	for _, event := range replayed {
		if event.Type == "message.delta" && event.RunID.Valid && event.RunID.String == enqueued.RunID {
			if event.SessionID != enqueued.SessionID || event.TraceID != enqueued.TraceID {
				t.Fatalf("event used spoofed metadata: %+v", event)
			}
			return
		}
	}
	t.Fatalf("message.delta not replayed for run session: %+v", replayed)
}

func TestRuntimePersistsContextBudgetNotice(t *testing.T) {
	h := newHarness(t)
	enqueued := h.createRunningRunResult(t, "context budget notice")
	payload, err := structpb.NewStruct(map[string]any{
		"note":                   "Context window limit: omitted recalled material from this model request.",
		"reason":                 "context_budget",
		"recallOmitted":          true,
		"historyMessagesOmitted": 0,
		"toolDefinitionsOmitted": 0,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := h.service.applyUpdate(context.Background(), &turingv1.RuntimeUpdate{
		Update: &turingv1.RuntimeUpdate_Event{Event: &turingv1.TuringEvent{
			RunId:   enqueued.RunID,
			Type:    turingv1.TuringEventType_TURING_EVENT_TYPE_AGENT_RUN_STEP,
			Payload: payload,
		}},
	}); err != nil {
		t.Fatal(err)
	}

	replayed, _, err := h.repo.ReplayEvents(
		context.Background(),
		enqueued.SessionID,
		enqueued.QueuedEvent.Sequence,
		10,
	)
	if err != nil {
		t.Fatal(err)
	}
	for _, event := range replayed {
		if event.Type != "agent.run.step" || !event.RunID.Valid || event.RunID.String != enqueued.RunID {
			continue
		}
		var got map[string]any
		if err := json.Unmarshal([]byte(event.PayloadJSON), &got); err != nil {
			t.Fatal(err)
		}
		if got["reason"] != "context_budget" || got["recallOmitted"] != true ||
			got["note"] != "Context window limit: omitted recalled material from this model request." {
			t.Fatalf("persisted context notice payload = %#v", got)
		}
		return
	}
	t.Fatalf("context budget notice not replayed: %+v", replayed)
}

// normalizeRuntimeEvent must not write through to the worker's event. It clones
// rather than dereferencing (a generated message embeds a mutex, so copying by
// value is unsafe), and this pins the observable half of that: the caller's
// message keeps whatever it sent, and the returned one carries the run's
// authoritative session/trace.
func TestNormalizeRuntimeEventDoesNotMutateCallerEvent(t *testing.T) {
	h := newHarness(t)
	enqueued := h.createRunningRunResult(t, "normalize session")
	otherSession, err := h.repo.CreateSession(context.Background(), "Other")
	if err != nil {
		t.Fatal(err)
	}
	payload, err := structpb.NewStruct(map[string]any{"delta": "hi"})
	if err != nil {
		t.Fatal(err)
	}
	event := &turingv1.TuringEvent{
		SessionId: otherSession.SessionID,
		RunId:     enqueued.RunID,
		TraceId:   "trace_spoofed",
		Type:      turingv1.TuringEventType_TURING_EVENT_TYPE_MESSAGE_DELTA,
		Payload:   payload,
	}

	out, err := h.service.normalizeRuntimeEvent(context.Background(), event)
	if err != nil {
		t.Fatal(err)
	}
	if out == event {
		t.Fatal("normalizeRuntimeEvent returned the caller's event; it must return a copy")
	}
	// Deep, not shallow: a `*event` dereference would share this pointer (and
	// would also be copying the message's embedded mutex).
	if out.Payload == event.Payload {
		t.Fatal("returned event shares the caller's payload; the copy must be deep")
	}
	if out.SessionId != enqueued.SessionID || out.TraceId != enqueued.TraceID {
		t.Fatalf("returned event did not adopt the run's session/trace: %+v", out)
	}
	if event.SessionId != otherSession.SessionID || event.TraceId != "trace_spoofed" {
		t.Fatalf("caller's event was mutated: %+v", event)
	}
}

func TestRuntimeRejectsEventsWithoutRunID(t *testing.T) {
	h := newHarness(t)
	session, err := h.repo.CreateSession(context.Background(), "Runtime")
	if err != nil {
		t.Fatal(err)
	}
	err = h.service.applyUpdate(context.Background(), &turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_Event{Event: &turingv1.TuringEvent{
		SessionId: session.SessionID,
		TraceId:   "trace_session",
		Type:      turingv1.TuringEventType_TURING_EVENT_TYPE_SYSTEM,
	}}})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("empty-run event error = %v, want InvalidArgument", err)
	}
}

func TestRuntimeRejectsUnspecifiedEventType(t *testing.T) {
	h := newHarness(t)
	enqueued := h.createRunningRunResult(t, "unspecified event")
	err := h.service.applyUpdate(context.Background(), &turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_Event{Event: &turingv1.TuringEvent{
		RunId: enqueued.RunID,
		Type:  turingv1.TuringEventType_TURING_EVENT_TYPE_UNSPECIFIED,
	}}})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("unspecified event error = %v, want InvalidArgument", err)
	}
}

func TestRuntimeRejectsUnknownEventType(t *testing.T) {
	h := newHarness(t)
	enqueued := h.createRunningRunResult(t, "unknown event")
	err := h.service.applyUpdate(context.Background(), &turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_Event{Event: &turingv1.TuringEvent{
		RunId: enqueued.RunID,
		Type:  turingv1.TuringEventType(999),
	}}})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("unknown event error = %v, want InvalidArgument", err)
	}
}

func TestWorkerCannotCompleteAnotherWorkersRun(t *testing.T) {
	h := newHarness(t)
	first := h.enqueueRun(t, "first")
	second := h.enqueueRun(t, "second")
	client := h.runtimeClient(t)
	ctx, cancel := context.WithTimeout(h.internalContext(), 2*time.Second)
	defer cancel()
	workerOne, err := client.ConnectWorker(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = workerOne.CloseSend() }()
	if err := workerOne.Send(&turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_WorkerReady{WorkerReady: &turingv1.RuntimeWorkerReady{WorkerId: "worker-owner-1", AgentId: turingv1.AgentId_AGENT_ID_GENERAL_ASSISTANT, MaxConcurrentRuns: 1}}}); err != nil {
		t.Fatal(err)
	}
	recvUntil(t, workerOne, func(cmd *turingv1.RuntimeCommand) bool {
		assigned := cmd.GetRunAssigned()
		return assigned != nil && assigned.RunId == first.RunID
	})
	workerTwo, err := client.ConnectWorker(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = workerTwo.CloseSend() }()
	if err := workerTwo.Send(&turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_WorkerReady{WorkerReady: &turingv1.RuntimeWorkerReady{WorkerId: "worker-owner-2", AgentId: turingv1.AgentId_AGENT_ID_GENERAL_ASSISTANT, MaxConcurrentRuns: 1}}}); err != nil {
		t.Fatal(err)
	}
	recvUntil(t, workerTwo, func(cmd *turingv1.RuntimeCommand) bool {
		assigned := cmd.GetRunAssigned()
		return assigned != nil && assigned.RunId == second.RunID
	})
	if err := workerTwo.Send(&turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_RunCompleted{RunCompleted: &turingv1.RuntimeRunCompleted{
		RunId:              first.RunID,
		AssistantMessageId: first.AssistantMessageID,
		Content:            "wrong worker",
	}}}); err != nil {
		t.Fatal(err)
	}
	time.Sleep(50 * time.Millisecond)
	run, err := h.repo.GetRun(context.Background(), first.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if run.Status != "running" {
		t.Fatalf("first run status = %q, want running after wrong-worker completion", run.Status)
	}
}

func TestRunCancelledAckRequiresPersistedCancellation(t *testing.T) {
	h := newHarness(t)
	enqueued := h.createRunningRunResult(t, "bad ack")
	err := h.service.applyUpdate(context.Background(), &turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_RunCancelledAck{RunCancelledAck: &turingv1.RuntimeCancelledAck{RunId: enqueued.RunID}}})
	if status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("cancel ack error = %v, want FailedPrecondition", err)
	}
	run, err := h.repo.GetRun(context.Background(), enqueued.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if run.Status != "running" {
		t.Fatalf("run status = %q, want running", run.Status)
	}
}

func TestRuntimeRejectsEventsAfterCancelledRun(t *testing.T) {
	h := newHarness(t)
	enqueued := h.createRunningRunResult(t, "cancelled event")
	cancelRunFixture(t, h, enqueued.RunID)
	payload, err := structpb.NewStruct(map[string]any{"delta": "late"})
	if err != nil {
		t.Fatal(err)
	}
	err = h.service.applyUpdate(context.Background(), &turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_Event{Event: &turingv1.TuringEvent{
		RunId:   enqueued.RunID,
		Type:    turingv1.TuringEventType_TURING_EVENT_TYPE_MESSAGE_DELTA,
		Payload: payload,
	}}})
	if status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("post-cancel event error = %v, want FailedPrecondition", err)
	}
	replayed, _, err := h.repo.ReplayEvents(context.Background(), enqueued.SessionID, enqueued.QueuedEvent.Sequence, 10)
	if err != nil {
		t.Fatal(err)
	}
	for _, event := range replayed {
		if event.Type == "message.delta" && event.RunID.Valid && event.RunID.String == enqueued.RunID {
			t.Fatalf("late event was persisted after cancellation: %+v", event)
		}
	}
}

func TestRuntimeRejectsGenericMessageCompletedEvent(t *testing.T) {
	tests := []struct {
		name            string
		payload         map[string]any
		useRunMessageID bool
	}{
		{name: "valid payload", payload: map[string]any{"content": "done"}, useRunMessageID: true},
		{name: "wrong message id", payload: map[string]any{"messageId": "msg_wrong", "content": "done"}},
		{name: "empty message id", payload: map[string]any{"messageId": "", "content": "done"}},
		{name: "empty content", payload: map[string]any{"content": ""}, useRunMessageID: true},
		{name: "missing content", payload: map[string]any{}, useRunMessageID: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newHarness(t)
			enqueued := h.createRunningRunResult(t, "bad message completed")
			if tt.useRunMessageID {
				tt.payload["messageId"] = enqueued.AssistantMessageID
			}
			payload, err := structpb.NewStruct(tt.payload)
			if err != nil {
				t.Fatal(err)
			}
			err = h.service.applyUpdate(context.Background(), &turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_Event{Event: &turingv1.TuringEvent{
				RunId:   enqueued.RunID,
				Type:    turingv1.TuringEventType_TURING_EVENT_TYPE_MESSAGE_COMPLETED,
				Payload: payload,
			}}})
			if status.Code(err) != codes.InvalidArgument {
				t.Fatalf("message.completed error = %v, want InvalidArgument", err)
			}
		})
	}
}

func TestRunCompletedUsesPersistedAssistantMessageID(t *testing.T) {
	h := newHarness(t)
	enqueued := h.createRunningRunResult(t, "complete without message id")
	err := h.service.applyUpdate(context.Background(), &turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_RunCompleted{RunCompleted: &turingv1.RuntimeRunCompleted{
		RunId:   enqueued.RunID,
		Content: "done",
	}}})
	if err != nil {
		t.Fatal(err)
	}
	var content string
	if err := h.database.QueryRowContext(context.Background(), `SELECT content FROM messages WHERE id = ?`, enqueued.AssistantMessageID).Scan(&content); err != nil {
		t.Fatal(err)
	}
	if content != "done" {
		t.Fatalf("assistant content = %q, want done", content)
	}
	replayed, _, err := h.repo.ReplayEvents(context.Background(), enqueued.SessionID, enqueued.QueuedEvent.Sequence, 10)
	if err != nil {
		t.Fatal(err)
	}
	for _, event := range replayed {
		if event.Type != "agent.run.completed" {
			continue
		}
		// map[string]any, not map[string]string: the payload now carries the
		// nested canonical run state beside its scalar fields.
		var payload map[string]any
		if err := json.Unmarshal([]byte(event.PayloadJSON), &payload); err != nil {
			t.Fatal(err)
		}
		if payload["assistantMessageId"] != enqueued.AssistantMessageID {
			t.Fatalf("completion payload = %+v", payload)
		}
		return
	}
	t.Fatal("agent.run.completed event not replayed")
}

func TestRunCompletedRejectsMismatchedAssistantMessageID(t *testing.T) {
	h := newHarness(t)
	enqueued := h.createRunningRunResult(t, "wrong assistant")
	err := h.service.applyUpdate(context.Background(), &turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_RunCompleted{RunCompleted: &turingv1.RuntimeRunCompleted{
		RunId:              enqueued.RunID,
		AssistantMessageId: "msg_wrong",
		Content:            "wrong",
	}}})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("mismatched assistant message error = %v, want InvalidArgument", err)
	}
	run, err := h.repo.GetRun(context.Background(), enqueued.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if run.Status != "running" {
		t.Fatalf("run status = %q, want running", run.Status)
	}
}

// An explicit completion carrying no content is a success, not a malformed
// report. It commits completed with the outcome that says there was nothing to
// show, and the empty content is persisted exactly as reported.
func TestRunCompletedAcceptsExplicitEmptyContent(t *testing.T) {
	h := newHarness(t)
	enqueued := h.createRunningRunResult(t, "empty completion")
	if err := h.service.applyUpdate(context.Background(), &turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_RunCompleted{RunCompleted: &turingv1.RuntimeRunCompleted{
		RunId:              enqueued.RunID,
		AssistantMessageId: enqueued.AssistantMessageID,
	}}}); err != nil {
		t.Fatalf("explicit empty completion was refused: %v", err)
	}
	state := h.runState(t, enqueued.RunID)
	if state.Lifecycle != "completed" || state.OutcomeReason != "completed_no_content" {
		t.Fatalf("run = %s/%s, want completed/completed_no_content", state.Lifecycle, state.OutcomeReason)
	}
	if state.HasDisplayableContent {
		t.Fatal("an empty completion was marked displayable")
	}
	run, err := h.repo.GetRun(context.Background(), enqueued.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if run.AssistantContent != "" {
		t.Fatalf("assistant content = %q, want the empty answer", run.AssistantContent)
	}
	if got := countRunEvents(t, h, enqueued.RunID, "message.completed"); got != 0 {
		t.Fatalf("message.completed events = %d, want none for an empty answer", got)
	}
}

func TestRunCompletedMapsStateConflictToFailedPrecondition(t *testing.T) {
	h := newHarness(t)
	enqueued := h.createRunningRunResult(t, "already cancelled")
	cancelRunFixture(t, h, enqueued.RunID)

	err := h.service.applyUpdate(context.Background(), &turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_RunCompleted{RunCompleted: &turingv1.RuntimeRunCompleted{
		RunId:              enqueued.RunID,
		AssistantMessageId: enqueued.AssistantMessageID,
		Content:            "too late",
	}}})
	if status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("RunCompleted error = %v, want FailedPrecondition", err)
	}
}

// TestRunCompletedOnARequeuedRunMapsToFailedPrecondition covers the other
// refusal shape. A run that was requeued while its old worker was still
// finishing is nonterminal, not immutable, but reporting completion for it is
// still a precondition failure the worker can act on — and a worker that gets
// an unknown internal error instead retries a report that can never succeed.
func TestRunCompletedOnARequeuedRunMapsToFailedPrecondition(t *testing.T) {
	h := newHarness(t)
	enqueued := h.enqueueRun(t, "still queued")

	err := h.service.applyUpdate(context.Background(), &turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_RunCompleted{RunCompleted: &turingv1.RuntimeRunCompleted{
		RunId:              enqueued.RunID,
		AssistantMessageId: enqueued.AssistantMessageID,
		Content:            "never ran",
	}}})
	if status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("RunCompleted on a queued run = %v (%v), want FailedPrecondition", err, status.Code(err))
	}
}

func TestRunFailedMapsStateConflictToFailedPrecondition(t *testing.T) {
	h := newHarness(t)
	enqueued := h.createRunningRunResult(t, "already cancelled")
	cancelRunFixture(t, h, enqueued.RunID)

	err := h.service.applyUpdate(context.Background(), &turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_RunFailed{RunFailed: &turingv1.RuntimeRunFailed{
		RunId:   enqueued.RunID,
		Code:    "model_error",
		Message: "too late",
	}}})
	if status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("RunFailed error = %v, want FailedPrecondition", err)
	}
}

func TestToolBeaconRequiresRunID(t *testing.T) {
	h := newHarness(t)
	err := h.service.applyUpdate(context.Background(), &turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_ToolBeacon{ToolBeacon: &turingv1.ToolCallBeacon{
		ToolCallId: "call_missing_run",
		AgentId:    turingv1.AgentId_AGENT_ID_GENERAL_ASSISTANT,
		ToolName:   "system.time",
	}}})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("tool beacon error = %v, want InvalidArgument", err)
	}
}

func TestToolBeaconRejectsInvalidFields(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*turingv1.ToolCallBeacon)
	}{
		{
			name: "empty tool call id",
			mutate: func(beacon *turingv1.ToolCallBeacon) {
				beacon.ToolCallId = ""
			},
		},
		{
			name: "unspecified phase",
			mutate: func(beacon *turingv1.ToolCallBeacon) {
				beacon.Phase = turingv1.ToolCallPhase_TOOL_CALL_PHASE_UNSPECIFIED
			},
		},
		{
			name: "unsupported agent",
			mutate: func(beacon *turingv1.ToolCallBeacon) {
				beacon.AgentId = turingv1.AgentId(999)
			},
		},
		{
			name: "missing tool name",
			mutate: func(beacon *turingv1.ToolCallBeacon) {
				beacon.ToolName = ""
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newHarness(t)
			enqueued := h.createRunningRunResult(t, "invalid beacon")
			beacon := &turingv1.ToolCallBeacon{
				RunId:      enqueued.RunID,
				TraceId:    enqueued.TraceID,
				ToolCallId: "call_valid",
				AgentId:    turingv1.AgentId_AGENT_ID_GENERAL_ASSISTANT,
				ToolName:   "system.time",
				Phase:      turingv1.ToolCallPhase_TOOL_CALL_PHASE_BEFORE,
			}
			tt.mutate(beacon)
			err := h.service.applyUpdate(context.Background(), &turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_ToolBeacon{ToolBeacon: beacon}})
			if status.Code(err) != codes.InvalidArgument {
				t.Fatalf("tool beacon error = %v, want InvalidArgument", err)
			}
		})
	}
}

func TestToolBeaconRejectsTerminalRun(t *testing.T) {
	h := newHarness(t)
	enqueued := h.createRunningRunResult(t, "terminal beacon")
	cancelRunFixture(t, h, enqueued.RunID)
	err := h.service.applyUpdate(context.Background(), &turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_ToolBeacon{ToolBeacon: &turingv1.ToolCallBeacon{
		RunId:      enqueued.RunID,
		TraceId:    enqueued.TraceID,
		ToolCallId: "call_terminal",
		AgentId:    turingv1.AgentId_AGENT_ID_GENERAL_ASSISTANT,
		ToolName:   "system.time",
		Phase:      turingv1.ToolCallPhase_TOOL_CALL_PHASE_BEFORE,
	}}})
	if status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("terminal tool beacon error = %v, want FailedPrecondition", err)
	}
}

func TestToolBeaconRejectsTraceMismatch(t *testing.T) {
	h := newHarness(t)
	enqueued := h.createRunningRunResult(t, "trace mismatch")
	err := h.service.applyUpdate(context.Background(), &turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_ToolBeacon{ToolBeacon: &turingv1.ToolCallBeacon{
		RunId:      enqueued.RunID,
		TraceId:    "trace_wrong",
		ToolCallId: "call_trace_mismatch",
		AgentId:    turingv1.AgentId_AGENT_ID_GENERAL_ASSISTANT,
		ToolName:   "system.time",
		Phase:      turingv1.ToolCallPhase_TOOL_CALL_PHASE_BEFORE,
	}}})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("trace mismatch error = %v, want InvalidArgument", err)
	}
}

func TestToolBeaconSendsPolicyDecisionCommand(t *testing.T) {
	h := newHarness(t)
	enqueued := h.enqueueRun(t, "tool decision")
	client := h.runtimeClient(t)
	ctx, cancel := context.WithTimeout(h.internalContext(), 2*time.Second)
	defer cancel()
	stream, err := client.ConnectWorker(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = stream.CloseSend() }()
	if err := stream.Send(&turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_WorkerReady{WorkerReady: &turingv1.RuntimeWorkerReady{WorkerId: "worker-tool-decision", AgentId: turingv1.AgentId_AGENT_ID_GENERAL_ASSISTANT, MaxConcurrentRuns: 1}}}); err != nil {
		t.Fatal(err)
	}
	recvUntil(t, stream, func(cmd *turingv1.RuntimeCommand) bool {
		assigned := cmd.GetRunAssigned()
		return assigned != nil && assigned.RunId == enqueued.RunID
	})
	if err := stream.Send(&turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_ToolBeacon{ToolBeacon: &turingv1.ToolCallBeacon{
		RunId:      enqueued.RunID,
		TraceId:    enqueued.TraceID,
		ToolCallId: "call_allow",
		AgentId:    turingv1.AgentId_AGENT_ID_GENERAL_ASSISTANT,
		ToolName:   "system.time",
		Phase:      turingv1.ToolCallPhase_TOOL_CALL_PHASE_BEFORE,
	}}}); err != nil {
		t.Fatal(err)
	}
	decision := recvUntil(t, stream, func(cmd *turingv1.RuntimeCommand) bool {
		decision := cmd.GetToolPolicyDecision()
		return decision != nil && decision.ToolCallId == "call_allow"
	}).GetToolPolicyDecision()
	if decision.Decision != turingv1.ToolPolicyDecision_DECISION_ALLOW {
		t.Fatalf("tool policy decision = %+v", decision)
	}
}

func TestToolBeaconRequiresApprovalForFilesTool(t *testing.T) {
	h := newHarness(t)
	enqueued := h.enqueueRun(t, "files approval")
	client := h.runtimeClient(t)
	ctx, cancel := context.WithTimeout(h.internalContext(), 2*time.Second)
	defer cancel()
	stream, err := client.ConnectWorker(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = stream.CloseSend() }()
	if err := stream.Send(&turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_WorkerReady{WorkerReady: &turingv1.RuntimeWorkerReady{WorkerId: "worker-files-approval", AgentId: turingv1.AgentId_AGENT_ID_GENERAL_ASSISTANT, MaxConcurrentRuns: 1}}}); err != nil {
		t.Fatal(err)
	}
	recvUntil(t, stream, func(cmd *turingv1.RuntimeCommand) bool {
		assigned := cmd.GetRunAssigned()
		return assigned != nil && assigned.RunId == enqueued.RunID
	})
	args, err := structpb.NewStruct(map[string]any{"path": "note.txt", "content": "hello"})
	if err != nil {
		t.Fatal(err)
	}
	if err := stream.Send(&turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_ToolBeacon{ToolBeacon: &turingv1.ToolCallBeacon{
		RunId:           enqueued.RunID,
		TraceId:         enqueued.TraceID,
		ToolCallId:      "call_files_update",
		ModelToolCallId: "provider_call_1",
		AgentId:         turingv1.AgentId_AGENT_ID_GENERAL_ASSISTANT,
		ServerName:      "files",
		ToolName:        "files.update",
		Phase:           turingv1.ToolCallPhase_TOOL_CALL_PHASE_BEFORE,
		Args:            args,
	}}}); err != nil {
		t.Fatal(err)
	}
	decision := recvUntil(t, stream, func(cmd *turingv1.RuntimeCommand) bool {
		decision := cmd.GetToolPolicyDecision()
		return decision != nil && decision.ToolCallId == "call_files_update"
	}).GetToolPolicyDecision()
	if decision.Decision != turingv1.ToolPolicyDecision_DECISION_APPROVAL_REQUIRED || decision.ApprovalId == "" {
		t.Fatalf("tool policy decision = %+v", decision)
	}
	if err := stream.Send(&turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_ToolBeacon{ToolBeacon: &turingv1.ToolCallBeacon{
		RunId: enqueued.RunID, TraceId: enqueued.TraceID, ToolCallId: "call_files_update",
		ModelToolCallId: "provider_call_1",
		AgentId:         turingv1.AgentId_AGENT_ID_GENERAL_ASSISTANT, ServerName: "files",
		ToolName: "files.update", Phase: turingv1.ToolCallPhase_TOOL_CALL_PHASE_BEFORE, Args: args,
	}}}); err != nil {
		t.Fatal(err)
	}
	repeated := recvUntil(t, stream, func(cmd *turingv1.RuntimeCommand) bool {
		decision := cmd.GetToolPolicyDecision()
		return decision != nil && decision.ToolCallId == "call_files_update"
	}).GetToolPolicyDecision()
	if repeated.ApprovalId != decision.ApprovalId {
		t.Fatalf("repeated approval ID = %q, want %q", repeated.ApprovalId, decision.ApprovalId)
	}
	var toolCallStatus, approvalID string
	if err := h.database.QueryRowContext(context.Background(), `SELECT status, approval_id FROM tool_calls WHERE id = ?`, "call_files_update").Scan(&toolCallStatus, &approvalID); err != nil {
		t.Fatal(err)
	}
	if toolCallStatus != "approval_required" || approvalID != decision.ApprovalId {
		t.Fatalf("tool call status=%q approval_id=%q decision=%+v", toolCallStatus, approvalID, decision)
	}
	run, err := h.repo.GetRun(context.Background(), enqueued.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if run.Status != "waiting_approval" {
		t.Fatalf("run status = %q, want waiting_approval", run.Status)
	}
	replayed, _, err := h.repo.ReplayEvents(context.Background(), enqueued.SessionID, 0, 20)
	if err != nil {
		t.Fatal(err)
	}
	var lifecycle []string
	var startedPayload map[string]any
	for _, event := range replayed {
		if !event.RunID.Valid || event.RunID.String != enqueued.RunID {
			continue
		}
		if event.Type == "tool.call.started" || event.Type == "approval.requested" {
			lifecycle = append(lifecycle, event.Type)
		}
		if event.Type == "tool.call.started" {
			if err := json.Unmarshal([]byte(event.PayloadJSON), &startedPayload); err != nil {
				t.Fatal(err)
			}
		}
	}
	if want := []string{"tool.call.started", "approval.requested"}; !reflect.DeepEqual(lifecycle, want) {
		t.Fatalf("approval start lifecycle = %v, want %v", lifecycle, want)
	}
	if want := map[string]any{
		"toolCallId": "call_files_update",
		"serverName": "files",
		"toolName":   "files.update",
	}; !reflect.DeepEqual(startedPayload, want) {
		t.Fatalf("tool.call.started payload = %#v", startedPayload)
	}
	var auditAction string
	if err := h.database.QueryRowContext(context.Background(), `SELECT action FROM audit_logs WHERE target = 'call_files_update'`).Scan(&auditAction); err != nil && !errors.Is(err, sql.ErrNoRows) {
		t.Fatal(err)
	}
	if auditAction != "" {
		t.Fatalf("approval-required before beacon wrote tool audit action %q before approval", auditAction)
	}
}

func TestApprovingApprovalNotifiesAssignedWorkerWithToken(t *testing.T) {
	h := newHarness(t)
	enqueued := h.enqueueRun(t, "files approval")
	client := h.runtimeClient(t)
	ctx, cancel := context.WithTimeout(h.internalContext(), 2*time.Second)
	defer cancel()
	stream, err := client.ConnectWorker(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = stream.CloseSend() }()
	if err := stream.Send(&turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_WorkerReady{WorkerReady: &turingv1.RuntimeWorkerReady{WorkerId: "worker-approval-notify", AgentId: turingv1.AgentId_AGENT_ID_GENERAL_ASSISTANT, MaxConcurrentRuns: 1}}}); err != nil {
		t.Fatal(err)
	}
	recvUntil(t, stream, func(cmd *turingv1.RuntimeCommand) bool {
		assigned := cmd.GetRunAssigned()
		return assigned != nil && assigned.RunId == enqueued.RunID
	})
	args, err := structpb.NewStruct(map[string]any{"path": "note.txt", "content": "hello"})
	if err != nil {
		t.Fatal(err)
	}
	if err := stream.Send(&turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_ToolBeacon{ToolBeacon: &turingv1.ToolCallBeacon{
		RunId:      enqueued.RunID,
		TraceId:    enqueued.TraceID,
		ToolCallId: "call_approval_notify",
		AgentId:    turingv1.AgentId_AGENT_ID_GENERAL_ASSISTANT,
		ServerName: "files",
		ToolName:   "files.update",
		Phase:      turingv1.ToolCallPhase_TOOL_CALL_PHASE_BEFORE,
		Args:       args,
	}}}); err != nil {
		t.Fatal(err)
	}
	decision := recvUntil(t, stream, func(cmd *turingv1.RuntimeCommand) bool {
		decision := cmd.GetToolPolicyDecision()
		return decision != nil && decision.ToolCallId == "call_approval_notify"
	}).GetToolPolicyDecision()
	if _, err := h.approvals.ApproveApproval(context.Background(), &turingv1.ApproveApprovalRequest{ApprovalId: decision.ApprovalId}); err != nil {
		t.Fatal(err)
	}
	update := recvUntil(t, stream, func(cmd *turingv1.RuntimeCommand) bool {
		update := cmd.GetApprovalUpdated()
		return update != nil && update.ApprovalId == decision.ApprovalId
	}).GetApprovalUpdated()
	if update.Status != "approved" || !strings.Contains(update.ApprovalToken, ".") {
		t.Fatalf("approval_updated = %+v", update)
	}
}

func TestDenyingApprovalWaitsForWorkerExitBeforeReleasingCapacity(t *testing.T) {
	h := newHarness(t)
	first := h.enqueueRun(t, "first approval")
	second := h.enqueueRun(t, "second after denial")
	client := h.runtimeClient(t)
	ctx, cancel := context.WithTimeout(h.internalContext(), 2*time.Second)
	defer cancel()
	stream, err := client.ConnectWorker(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = stream.CloseSend() }()
	if err := stream.Send(&turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_WorkerReady{WorkerReady: &turingv1.RuntimeWorkerReady{WorkerId: "worker-deny-release", AgentId: turingv1.AgentId_AGENT_ID_GENERAL_ASSISTANT, MaxConcurrentRuns: 1}}}); err != nil {
		t.Fatal(err)
	}
	recvUntil(t, stream, func(cmd *turingv1.RuntimeCommand) bool {
		assigned := cmd.GetRunAssigned()
		return assigned != nil && assigned.RunId == first.RunID
	})
	args, err := structpb.NewStruct(map[string]any{"path": "note.txt", "content": "hello"})
	if err != nil {
		t.Fatal(err)
	}
	if err := stream.Send(&turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_ToolBeacon{ToolBeacon: &turingv1.ToolCallBeacon{
		RunId:      first.RunID,
		TraceId:    first.TraceID,
		ToolCallId: "call_deny_release",
		AgentId:    turingv1.AgentId_AGENT_ID_GENERAL_ASSISTANT,
		ServerName: "files",
		ToolName:   "files.update",
		Phase:      turingv1.ToolCallPhase_TOOL_CALL_PHASE_BEFORE,
		Args:       args,
	}}}); err != nil {
		t.Fatal(err)
	}
	decision := recvUntil(t, stream, func(cmd *turingv1.RuntimeCommand) bool {
		decision := cmd.GetToolPolicyDecision()
		return decision != nil && decision.ToolCallId == "call_deny_release"
	}).GetToolPolicyDecision()
	if _, err := h.approvals.DenyApproval(context.Background(), &turingv1.DenyApprovalRequest{ApprovalId: decision.ApprovalId, Reason: "no"}); err != nil {
		t.Fatal(err)
	}
	recvUntil(t, stream, func(cmd *turingv1.RuntimeCommand) bool {
		update := cmd.GetApprovalUpdated()
		return update != nil && update.ApprovalId == decision.ApprovalId && update.Status == "denied"
	})
	secondRun, err := h.repo.GetRun(context.Background(), second.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if secondRun.Status != "queued" {
		t.Fatalf("second run status = %q, want queued until denied run exits", secondRun.Status)
	}
	if err := stream.Send(&turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_RunCancelledAck{RunCancelledAck: &turingv1.RuntimeCancelledAck{RunId: first.RunID}}}); err != nil {
		t.Fatal(err)
	}
	recvUntil(t, stream, func(cmd *turingv1.RuntimeCommand) bool {
		assigned := cmd.GetRunAssigned()
		return assigned != nil && assigned.RunId == second.RunID
	})
	var jobStatus string
	if err := h.database.QueryRowContext(context.Background(), `SELECT status FROM jobs WHERE id = ?`, first.JobID).Scan(&jobStatus); err != nil {
		t.Fatal(err)
	}
	if jobStatus != "failed" {
		t.Fatalf("denied job status = %q, want failed", jobStatus)
	}
}

func TestDenyingApprovalKeepsLaterSameSessionRunBlockedUntilExecutionExitAck(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	session, err := h.repo.CreateSession(ctx, "Causal approval exit")
	if err != nil {
		t.Fatal(err)
	}
	first, err := h.repo.EnqueueUserMessage(ctx, repository.EnqueueUserMessageInput{
		SessionID: session.SessionID, Content: "first approval", AgentID: "general_assistant", ModelProvider: "ollama", Model: "llama3.2",
	})
	if err != nil {
		t.Fatal(err)
	}
	second, err := h.repo.EnqueueUserMessage(ctx, repository.EnqueueUserMessageInput{
		SessionID: session.SessionID, Content: "second after denial", AgentID: "general_assistant", ModelProvider: "ollama", Model: "llama3.2",
	})
	if err != nil {
		t.Fatal(err)
	}
	client := h.runtimeClient(t)
	firstCtx, cancelFirst := context.WithTimeout(h.internalContext(), 2*time.Second)
	defer cancelFirst()
	firstWorker, err := client.ConnectWorker(firstCtx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = firstWorker.CloseSend() }()
	if err := firstWorker.Send(&turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_WorkerReady{WorkerReady: &turingv1.RuntimeWorkerReady{
		WorkerId: "worker-blocked-executor", AgentId: turingv1.AgentId_AGENT_ID_GENERAL_ASSISTANT, MaxConcurrentRuns: 1,
	}}}); err != nil {
		t.Fatal(err)
	}
	assignedFirst := recvUntil(t, firstWorker, func(cmd *turingv1.RuntimeCommand) bool {
		assigned := cmd.GetRunAssigned()
		return assigned != nil && assigned.RunId == first.RunID
	}).GetRunAssigned()

	secondCtx, cancelSecond := context.WithTimeout(h.internalContext(), 2*time.Second)
	defer cancelSecond()
	secondWorker, err := client.ConnectWorker(secondCtx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = secondWorker.CloseSend() }()
	if err := secondWorker.Send(&turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_WorkerReady{WorkerReady: &turingv1.RuntimeWorkerReady{
		WorkerId: "worker-available-capacity", AgentId: turingv1.AgentId_AGENT_ID_GENERAL_ASSISTANT, MaxConcurrentRuns: 1,
	}}}); err != nil {
		t.Fatal(err)
	}
	recvUntil(t, secondWorker, func(cmd *turingv1.RuntimeCommand) bool {
		return cmd.GetWorkerAccepted() != nil
	})

	args, err := structpb.NewStruct(map[string]any{"path": "note.txt", "content": "hello"})
	if err != nil {
		t.Fatal(err)
	}
	if err := firstWorker.Send(&turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_ToolBeacon{ToolBeacon: &turingv1.ToolCallBeacon{
		RunId:      assignedFirst.RunId,
		TraceId:    assignedFirst.TraceId,
		ToolCallId: "call_causal_denial",
		AgentId:    turingv1.AgentId_AGENT_ID_GENERAL_ASSISTANT,
		ServerName: "files",
		ToolName:   "files.update",
		Phase:      turingv1.ToolCallPhase_TOOL_CALL_PHASE_BEFORE,
		Args:       args,
	}}}); err != nil {
		t.Fatal(err)
	}
	decision := recvUntil(t, firstWorker, func(cmd *turingv1.RuntimeCommand) bool {
		toolDecision := cmd.GetToolPolicyDecision()
		return toolDecision != nil && toolDecision.ToolCallId == "call_causal_denial"
	}).GetToolPolicyDecision()
	if _, err := h.approvals.DenyApproval(ctx, &turingv1.DenyApprovalRequest{ApprovalId: decision.ApprovalId, Reason: "no"}); err != nil {
		t.Fatal(err)
	}
	recvUntil(t, firstWorker, func(cmd *turingv1.RuntimeCommand) bool {
		update := cmd.GetApprovalUpdated()
		return update != nil && update.ApprovalId == decision.ApprovalId && update.Status == "denied"
	})

	if err := h.service.DispatchPending(ctx); err != nil {
		t.Fatal(err)
	}
	secondRun, err := h.repo.GetRun(ctx, second.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if secondRun.Status != "queued" {
		t.Fatalf("later same-session run status = %q, want queued until blocked execution exits", secondRun.Status)
	}

	if err := firstWorker.Send(&turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_RunCancelledAck{RunCancelledAck: &turingv1.RuntimeCancelledAck{RunId: first.RunID}}}); err != nil {
		t.Fatal(err)
	}
	assignedSecond := recvUntilEither(t, firstWorker, secondWorker, func(cmd *turingv1.RuntimeCommand) bool {
		assigned := cmd.GetRunAssigned()
		return assigned != nil && assigned.RunId == second.RunID
	}).GetRunAssigned()
	if assignedSecond.RunId != second.RunID {
		t.Fatalf("assigned run = %q, want %q", assignedSecond.RunId, second.RunID)
	}
}

func TestTerminalApprovalCleanupAfterKeepsWorkerStreamUsable(t *testing.T) {
	h := newHarness(t)
	first := h.enqueueRun(t, "first approval")
	second := h.enqueueRun(t, "later work")
	client := h.runtimeClient(t)
	ctx, cancel := context.WithTimeout(h.internalContext(), 2*time.Second)
	defer cancel()
	stream, err := client.ConnectWorker(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = stream.CloseSend() }()
	if err := stream.Send(&turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_WorkerReady{WorkerReady: &turingv1.RuntimeWorkerReady{
		WorkerId: "worker-terminal-cleanup", AgentId: turingv1.AgentId_AGENT_ID_GENERAL_ASSISTANT, MaxConcurrentRuns: 1,
	}}}); err != nil {
		t.Fatal(err)
	}
	assigned := recvUntil(t, stream, func(cmd *turingv1.RuntimeCommand) bool {
		return cmd.GetRunAssigned() != nil && cmd.GetRunAssigned().RunId == first.RunID
	}).GetRunAssigned()
	before := &turingv1.ToolCallBeacon{
		RunId: assigned.RunId, TraceId: assigned.TraceId, ToolCallId: "call_terminal_cleanup_stream",
		AgentId: turingv1.AgentId_AGENT_ID_GENERAL_ASSISTANT, ServerName: "files", ToolName: "files.update",
		Phase: turingv1.ToolCallPhase_TOOL_CALL_PHASE_BEFORE, Args: mustStruct(t, map[string]any{"path": "note.txt", "content": "hello"}),
	}
	if err := stream.Send(&turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_ToolBeacon{ToolBeacon: before}}); err != nil {
		t.Fatal(err)
	}
	decision := recvUntil(t, stream, func(cmd *turingv1.RuntimeCommand) bool {
		return cmd.GetToolPolicyDecision() != nil && cmd.GetToolPolicyDecision().ToolCallId == before.ToolCallId
	}).GetToolPolicyDecision()
	if _, err := h.approvals.DenyApproval(context.Background(), &turingv1.DenyApprovalRequest{ApprovalId: decision.ApprovalId, Reason: "no"}); err != nil {
		t.Fatal(err)
	}
	recvUntil(t, stream, func(cmd *turingv1.RuntimeCommand) bool {
		update := cmd.GetApprovalUpdated()
		return update != nil && update.ApprovalId == decision.ApprovalId && update.Status == "denied"
	})
	cleanup := &turingv1.ToolCallBeacon{
		RunId: before.RunId, TraceId: before.TraceId, ToolCallId: before.ToolCallId,
		AgentId: before.AgentId, ServerName: before.ServerName, ToolName: before.ToolName,
		Phase: turingv1.ToolCallPhase_TOOL_CALL_PHASE_AFTER, Status: turingv1.ToolCallStatus_TOOL_CALL_STATUS_FAILED,
		Args:  before.Args,
		Error: &turingv1.ToolCallError{Code: "approval_wait_failed", Message: "context canceled"},
	}
	if err := stream.Send(&turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_ToolBeacon{ToolBeacon: cleanup}}); err != nil {
		t.Fatal(err)
	}
	afterDecision := recvUntil(t, stream, func(cmd *turingv1.RuntimeCommand) bool {
		return cmd.GetToolPolicyDecision() != nil && cmd.GetToolPolicyDecision().ToolCallId == cleanup.ToolCallId
	}).GetToolPolicyDecision()
	if afterDecision.Decision != turingv1.ToolPolicyDecision_DECISION_ALLOW {
		t.Fatalf("cleanup decision = %+v, want allow", afterDecision)
	}
	if err := stream.Send(&turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_RunCancelledAck{RunCancelledAck: &turingv1.RuntimeCancelledAck{RunId: first.RunID}}}); err != nil {
		t.Fatal(err)
	}
	next := recvUntil(t, stream, func(cmd *turingv1.RuntimeCommand) bool {
		return cmd.GetRunAssigned() != nil && cmd.GetRunAssigned().RunId == second.RunID
	}).GetRunAssigned()
	if next.RunId != second.RunID {
		t.Fatalf("next assignment = %+v, want later run", next)
	}
}

func TestExpiredApprovalWaitsForWorkerExitBeforeReleasingCapacity(t *testing.T) {
	h := newHarness(t)
	first := h.enqueueRun(t, "first approval")
	second := h.enqueueRun(t, "second after expiry")
	client := h.runtimeClient(t)
	ctx, cancel := context.WithTimeout(h.internalContext(), 2*time.Second)
	defer cancel()
	stream, err := client.ConnectWorker(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = stream.CloseSend() }()
	if err := stream.Send(&turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_WorkerReady{WorkerReady: &turingv1.RuntimeWorkerReady{WorkerId: "worker-expire-release", AgentId: turingv1.AgentId_AGENT_ID_GENERAL_ASSISTANT, MaxConcurrentRuns: 1}}}); err != nil {
		t.Fatal(err)
	}
	recvUntil(t, stream, func(cmd *turingv1.RuntimeCommand) bool {
		assigned := cmd.GetRunAssigned()
		return assigned != nil && assigned.RunId == first.RunID
	})
	args, err := structpb.NewStruct(map[string]any{"path": "note.txt", "content": "hello"})
	if err != nil {
		t.Fatal(err)
	}
	if err := stream.Send(&turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_ToolBeacon{ToolBeacon: &turingv1.ToolCallBeacon{
		RunId:      first.RunID,
		TraceId:    first.TraceID,
		ToolCallId: "call_expire_release",
		AgentId:    turingv1.AgentId_AGENT_ID_GENERAL_ASSISTANT,
		ServerName: "files",
		ToolName:   "files.update",
		Phase:      turingv1.ToolCallPhase_TOOL_CALL_PHASE_BEFORE,
		Args:       args,
	}}}); err != nil {
		t.Fatal(err)
	}
	decision := recvUntil(t, stream, func(cmd *turingv1.RuntimeCommand) bool {
		decision := cmd.GetToolPolicyDecision()
		return decision != nil && decision.ToolCallId == "call_expire_release"
	}).GetToolPolicyDecision()
	if _, err := h.database.ExecContext(context.Background(), `UPDATE approvals SET expires_at = ? WHERE id = ?`, time.Now().Add(-time.Minute).Format(time.RFC3339Nano), decision.ApprovalId); err != nil {
		t.Fatal(err)
	}
	if _, err := h.approvals.ApproveApproval(context.Background(), &turingv1.ApproveApprovalRequest{ApprovalId: decision.ApprovalId}); status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("ApproveApproval expired error = %v, want FailedPrecondition", err)
	}
	recvUntil(t, stream, func(cmd *turingv1.RuntimeCommand) bool {
		update := cmd.GetApprovalUpdated()
		return update != nil && update.ApprovalId == decision.ApprovalId && update.Status == "expired"
	})
	secondRun, err := h.repo.GetRun(context.Background(), second.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if secondRun.Status != "queued" {
		t.Fatalf("second run status = %q, want queued until expired run exits", secondRun.Status)
	}
	if err := stream.Send(&turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_RunCancelledAck{RunCancelledAck: &turingv1.RuntimeCancelledAck{RunId: first.RunID}}}); err != nil {
		t.Fatal(err)
	}
	recvUntil(t, stream, func(cmd *turingv1.RuntimeCommand) bool {
		assigned := cmd.GetRunAssigned()
		return assigned != nil && assigned.RunId == second.RunID
	})
	var jobStatus string
	if err := h.database.QueryRowContext(context.Background(), `SELECT status FROM jobs WHERE id = ?`, first.JobID).Scan(&jobStatus); err != nil {
		t.Fatal(err)
	}
	if jobStatus != "failed" {
		t.Fatalf("expired job status = %q, want failed", jobStatus)
	}
}

func TestToolBeaconDeniesApprovalRequiredToolWithoutArgs(t *testing.T) {
	h := newHarness(t)
	enqueued := h.enqueueRun(t, "missing approval args")
	client := h.runtimeClient(t)
	ctx, cancel := context.WithTimeout(h.internalContext(), 2*time.Second)
	defer cancel()
	stream, err := client.ConnectWorker(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = stream.CloseSend() }()
	if err := stream.Send(&turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_WorkerReady{WorkerReady: &turingv1.RuntimeWorkerReady{WorkerId: "worker-missing-args", AgentId: turingv1.AgentId_AGENT_ID_GENERAL_ASSISTANT, MaxConcurrentRuns: 1}}}); err != nil {
		t.Fatal(err)
	}
	recvUntil(t, stream, func(cmd *turingv1.RuntimeCommand) bool {
		assigned := cmd.GetRunAssigned()
		return assigned != nil && assigned.RunId == enqueued.RunID
	})
	if err := stream.Send(&turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_ToolBeacon{ToolBeacon: &turingv1.ToolCallBeacon{
		RunId:      enqueued.RunID,
		TraceId:    enqueued.TraceID,
		ToolCallId: "call_missing_args",
		AgentId:    turingv1.AgentId_AGENT_ID_GENERAL_ASSISTANT,
		ServerName: "files",
		ToolName:   "files.update",
		Phase:      turingv1.ToolCallPhase_TOOL_CALL_PHASE_BEFORE,
	}}}); err != nil {
		t.Fatal(err)
	}
	decision := recvUntil(t, stream, func(cmd *turingv1.RuntimeCommand) bool {
		decision := cmd.GetToolPolicyDecision()
		return decision != nil && decision.ToolCallId == "call_missing_args"
	}).GetToolPolicyDecision()
	if decision.Decision != turingv1.ToolPolicyDecision_DECISION_DENY || decision.Reason != "approval_args_missing" {
		t.Fatalf("tool policy decision = %+v", decision)
	}
	var approvalCount int
	if err := h.database.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM approvals WHERE tool_call_id = 'call_missing_args'`).Scan(&approvalCount); err != nil {
		t.Fatal(err)
	}
	if approvalCount != 0 {
		t.Fatalf("approval count = %d, want 0", approvalCount)
	}
	var toolCallStatus string
	if err := h.database.QueryRowContext(context.Background(), `SELECT status FROM tool_calls WHERE id = 'call_missing_args'`).Scan(&toolCallStatus); err != nil {
		t.Fatal(err)
	}
	if toolCallStatus != "denied" {
		t.Fatalf("tool call status = %q, want denied", toolCallStatus)
	}
}

func TestToolBeaconDeniesUnknownToolWithDurableEvent(t *testing.T) {
	h := newHarness(t)
	enqueued := h.enqueueRun(t, "unknown tool")
	client := h.runtimeClient(t)
	ctx, cancel := context.WithTimeout(h.internalContext(), 2*time.Second)
	defer cancel()
	stream, err := client.ConnectWorker(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = stream.CloseSend() }()
	if err := stream.Send(&turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_WorkerReady{WorkerReady: &turingv1.RuntimeWorkerReady{WorkerId: "worker-unknown-tool", AgentId: turingv1.AgentId_AGENT_ID_GENERAL_ASSISTANT, MaxConcurrentRuns: 1}}}); err != nil {
		t.Fatal(err)
	}
	recvUntil(t, stream, func(cmd *turingv1.RuntimeCommand) bool {
		assigned := cmd.GetRunAssigned()
		return assigned != nil && assigned.RunId == enqueued.RunID
	})
	if err := stream.Send(&turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_ToolBeacon{ToolBeacon: &turingv1.ToolCallBeacon{
		RunId:      enqueued.RunID,
		TraceId:    enqueued.TraceID,
		ToolCallId: "call_unknown",
		AgentId:    turingv1.AgentId_AGENT_ID_GENERAL_ASSISTANT,
		ServerName: "system",
		ToolName:   "system.shell",
		Phase:      turingv1.ToolCallPhase_TOOL_CALL_PHASE_BEFORE,
	}}}); err != nil {
		t.Fatal(err)
	}
	decision := recvUntil(t, stream, func(cmd *turingv1.RuntimeCommand) bool {
		decision := cmd.GetToolPolicyDecision()
		return decision != nil && decision.ToolCallId == "call_unknown"
	}).GetToolPolicyDecision()
	if decision.Decision != turingv1.ToolPolicyDecision_DECISION_DENY || decision.Reason != "unknown_tool" {
		t.Fatalf("tool policy decision = %+v", decision)
	}
	replayed, _, err := h.repo.ReplayEvents(context.Background(), enqueued.SessionID, 0, 20)
	if err != nil {
		t.Fatal(err)
	}
	var denied repository.Event
	for _, event := range replayed {
		if event.Type == "tool.call.denied" {
			denied = event
		}
	}
	if denied.EventID == "" {
		t.Fatal("tool.call.denied event was not persisted")
	}
	var payload map[string]string
	if err := json.Unmarshal([]byte(denied.PayloadJSON), &payload); err != nil {
		t.Fatal(err)
	}
	if want := map[string]string{
		"toolCallId": "call_unknown",
		"serverName": "system",
		"toolName":   "system.shell",
		"category":   string(runoutcome.ReasonPolicyDenied),
	}; !reflect.DeepEqual(payload, want) {
		t.Fatalf("tool.call.denied payload = %+v", payload)
	}
}

func TestToolBeaconConflictMapsToAlreadyExists(t *testing.T) {
	h := newHarness(t)
	first := h.createRunningRunResult(t, "first tool call")
	second := h.createRunningRunResult(t, "second tool call")
	args, err := structpb.NewStruct(map[string]any{"value": "hello"})
	if err != nil {
		t.Fatal(err)
	}
	if err := h.service.applyUpdate(context.Background(), &turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_ToolBeacon{ToolBeacon: &turingv1.ToolCallBeacon{
		RunId:      first.RunID,
		TraceId:    first.TraceID,
		ToolCallId: "call_conflict",
		AgentId:    turingv1.AgentId_AGENT_ID_GENERAL_ASSISTANT,
		ServerName: "system",
		ToolName:   "system.echo",
		Phase:      turingv1.ToolCallPhase_TOOL_CALL_PHASE_BEFORE,
		Args:       args,
	}}}); err != nil {
		t.Fatal(err)
	}
	err = h.service.applyUpdate(context.Background(), &turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_ToolBeacon{ToolBeacon: &turingv1.ToolCallBeacon{
		RunId:      second.RunID,
		TraceId:    second.TraceID,
		ToolCallId: "call_conflict",
		AgentId:    turingv1.AgentId_AGENT_ID_GENERAL_ASSISTANT,
		ServerName: "system",
		ToolName:   "system.echo",
		Phase:      turingv1.ToolCallPhase_TOOL_CALL_PHASE_BEFORE,
		Args:       args,
	}}})
	if status.Code(err) != codes.AlreadyExists {
		t.Fatalf("tool call conflict error = %v, want AlreadyExists", err)
	}
}

func TestToolBeaconDeniedBeforeExactDuplicateSuppressesSideEffects(t *testing.T) {
	h := newHarness(t)
	enqueued := h.createRunningRunResult(t, "duplicate denied before")
	beacon := &turingv1.ToolCallBeacon{
		RunId: enqueued.RunID, TraceId: enqueued.TraceID, ToolCallId: "call_duplicate_denied",
		AgentId: turingv1.AgentId_AGENT_ID_GENERAL_ASSISTANT, ServerName: "system",
		ToolName: "system.unknown", Phase: turingv1.ToolCallPhase_TOOL_CALL_PHASE_BEFORE,
	}
	for attempt := 0; attempt < 2; attempt++ {
		decision, err := h.service.handleToolBeacon(context.Background(), beacon)
		if err != nil {
			t.Fatalf("duplicate denied BEFORE attempt %d: %v", attempt+1, err)
		}
		if decision.GetDecision() != turingv1.ToolPolicyDecision_DECISION_DENY || decision.GetReason() != "unknown_tool" {
			t.Fatalf("duplicate denied decision = %+v", decision)
		}
	}
	var eventCount, auditCount int
	if err := h.database.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM events WHERE run_id = ? AND type = 'tool.call.denied'`, enqueued.RunID).Scan(&eventCount); err != nil {
		t.Fatal(err)
	}
	if err := h.database.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM audit_logs WHERE target = ? AND action = 'tool.call.before'`, beacon.ToolCallId).Scan(&auditCount); err != nil {
		t.Fatal(err)
	}
	if eventCount != 1 || auditCount != 1 {
		t.Fatalf("duplicate denied BEFORE event/audit counts = %d/%d, want 1/1", eventCount, auditCount)
	}
}

func TestToolBeaconSafeBeforeExactDuplicateReturnsSameAllowDecision(t *testing.T) {
	h := newHarness(t)
	enqueued := h.createRunningRunResult(t, "duplicate safe before")
	args, err := structpb.NewStruct(map[string]any{"value": "hello"})
	if err != nil {
		t.Fatal(err)
	}
	beacon := &turingv1.ToolCallBeacon{
		RunId: enqueued.RunID, TraceId: enqueued.TraceID, ToolCallId: "call_duplicate_safe",
		AgentId: turingv1.AgentId_AGENT_ID_GENERAL_ASSISTANT, ServerName: "system",
		ToolName: "system.echo", Phase: turingv1.ToolCallPhase_TOOL_CALL_PHASE_BEFORE, Args: args,
	}
	first, err := h.service.handleToolBeacon(context.Background(), beacon)
	if err != nil || first.GetDecision() != turingv1.ToolPolicyDecision_DECISION_ALLOW {
		t.Fatalf("first safe BEFORE = %+v/%v, want allow", first, err)
	}
	duplicate, err := h.service.handleToolBeacon(context.Background(), beacon)
	if err != nil {
		t.Fatalf("duplicate safe BEFORE returned error: %v", err)
	}
	if duplicate.GetDecision() != turingv1.ToolPolicyDecision_DECISION_ALLOW {
		t.Fatalf("duplicate safe BEFORE = %+v, want same allow decision", duplicate)
	}
}

func TestToolBeaconAfterMissingCallMapsToNotFound(t *testing.T) {
	h := newHarness(t)
	run := h.createRunningRunResult(t, "missing tool call")
	err := h.service.applyUpdate(context.Background(), &turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_ToolBeacon{ToolBeacon: &turingv1.ToolCallBeacon{
		RunId:         run.RunID,
		TraceId:       run.TraceID,
		ToolCallId:    "call_missing",
		AgentId:       turingv1.AgentId_AGENT_ID_GENERAL_ASSISTANT,
		ServerName:    "system",
		ToolName:      "system.echo",
		Phase:         turingv1.ToolCallPhase_TOOL_CALL_PHASE_AFTER,
		Status:        turingv1.ToolCallStatus_TOOL_CALL_STATUS_COMPLETED,
		ResultSummary: "done",
	}}})
	if status.Code(err) != codes.NotFound {
		t.Fatalf("missing tool call error = %v, want NotFound", err)
	}
}

func TestToolBeaconAfterRecordsCompletionEvent(t *testing.T) {
	h := newHarness(t)
	enqueued := h.createRunningRunResult(t, "tool after")
	beforeArgs, err := structpb.NewStruct(map[string]any{"value": "hello"})
	if err != nil {
		t.Fatal(err)
	}
	if err := h.service.applyUpdate(context.Background(), &turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_ToolBeacon{ToolBeacon: &turingv1.ToolCallBeacon{
		RunId:      enqueued.RunID,
		TraceId:    enqueued.TraceID,
		ToolCallId: "call_echo",
		AgentId:    turingv1.AgentId_AGENT_ID_GENERAL_ASSISTANT,
		ServerName: "system",
		ToolName:   "system.echo",
		Phase:      turingv1.ToolCallPhase_TOOL_CALL_PHASE_BEFORE,
		Args:       beforeArgs,
	}}}); err != nil {
		t.Fatal(err)
	}
	if err := h.service.applyUpdate(context.Background(), &turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_ToolBeacon{ToolBeacon: &turingv1.ToolCallBeacon{
		RunId:         enqueued.RunID,
		TraceId:       enqueued.TraceID,
		ToolCallId:    "call_echo",
		AgentId:       turingv1.AgentId_AGENT_ID_GENERAL_ASSISTANT,
		ServerName:    "system",
		ToolName:      "system.echo",
		Phase:         turingv1.ToolCallPhase_TOOL_CALL_PHASE_AFTER,
		Status:        turingv1.ToolCallStatus_TOOL_CALL_STATUS_COMPLETED,
		Args:          beforeArgs,
		ResultSummary: "echoed hello",
		DurationMs:    12,
	}}}); err != nil {
		t.Fatal(err)
	}
	var toolCallStatus, resultSummary string
	if err := h.database.QueryRowContext(context.Background(), `SELECT status, result_summary FROM tool_calls WHERE id = ?`, "call_echo").Scan(&toolCallStatus, &resultSummary); err != nil {
		t.Fatal(err)
	}
	if toolCallStatus != "completed" || resultSummary != "echoed hello" {
		t.Fatalf("tool call status=%q result_summary=%q", toolCallStatus, resultSummary)
	}
	var auditActions []string
	rows, err := h.database.QueryContext(context.Background(), `SELECT action FROM audit_logs WHERE target = ? ORDER BY created_at`, "call_echo")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var action string
		if err := rows.Scan(&action); err != nil {
			t.Fatal(err)
		}
		auditActions = append(auditActions, action)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if len(auditActions) != 2 || auditActions[0] != "tool.call.before" || auditActions[1] != "tool.call.after" {
		t.Fatalf("audit actions = %+v", auditActions)
	}
	replayed, _, err := h.repo.ReplayEvents(context.Background(), enqueued.SessionID, 0, 20)
	if err != nil {
		t.Fatal(err)
	}
	var completed repository.Event
	for _, event := range replayed {
		if event.Type == "tool.call.completed" {
			completed = event
		}
	}
	if completed.EventID == "" {
		t.Fatal("tool.call.completed event was not persisted")
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(completed.PayloadJSON), &payload); err != nil {
		t.Fatal(err)
	}
	if want := map[string]any{
		"toolCallId": "call_echo",
		"serverName": "system",
		"toolName":   "system.echo",
	}; !reflect.DeepEqual(payload, want) {
		t.Fatalf("tool.call.completed payload = %+v", payload)
	}
}

func TestToolBeaconAfterRetriesTerminalEventAppendFailure(t *testing.T) {
	h := newHarness(t)
	enqueued := h.createRunningRunResult(t, "retry terminal event")
	before := &turingv1.ToolCallBeacon{
		RunId:      enqueued.RunID,
		TraceId:    enqueued.TraceID,
		ToolCallId: "call_retry_terminal_event",
		AgentId:    turingv1.AgentId_AGENT_ID_GENERAL_ASSISTANT,
		ServerName: "system",
		ToolName:   "system.echo",
		Phase:      turingv1.ToolCallPhase_TOOL_CALL_PHASE_BEFORE,
	}
	if err := h.service.applyUpdate(context.Background(), &turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_ToolBeacon{ToolBeacon: before}}); err != nil {
		t.Fatal(err)
	}
	after := &turingv1.ToolCallBeacon{
		RunId:         enqueued.RunID,
		TraceId:       enqueued.TraceID,
		ToolCallId:    before.ToolCallId,
		AgentId:       turingv1.AgentId_AGENT_ID_GENERAL_ASSISTANT,
		ServerName:    "system",
		ToolName:      "system.echo",
		Phase:         turingv1.ToolCallPhase_TOOL_CALL_PHASE_AFTER,
		Status:        turingv1.ToolCallStatus_TOOL_CALL_STATUS_COMPLETED,
		ResultSummary: "echoed hello",
		DurationMs:    12,
	}
	if _, err := h.database.ExecContext(context.Background(), `
		CREATE TRIGGER fail_terminal_tool_event
		BEFORE INSERT ON events
		WHEN NEW.type = 'tool.call.completed'
		BEGIN
			SELECT RAISE(ABORT, 'append terminal tool event failed');
		END
	`); err != nil {
		t.Fatal(err)
	}
	update := &turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_ToolBeacon{ToolBeacon: after}}
	err := h.service.applyUpdate(context.Background(), update)
	if err == nil || !strings.Contains(err.Error(), "append terminal tool event failed") {
		t.Fatalf("first terminal beacon error = %v, want append failure", err)
	}
	if _, err := h.database.ExecContext(context.Background(), `DROP TRIGGER fail_terminal_tool_event`); err != nil {
		t.Fatal(err)
	}
	if err := h.service.applyUpdate(context.Background(), update); err != nil {
		t.Fatalf("identical terminal retry: %v", err)
	}
	var events int
	if err := h.database.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM events WHERE run_id = ? AND type = 'tool.call.completed'`, enqueued.RunID).Scan(&events); err != nil {
		t.Fatal(err)
	}
	if events != 1 {
		t.Fatalf("terminal tool events = %d, want 1", events)
	}
}

func TestToolBeaconAfterRejectsCompletedPendingApproval(t *testing.T) {
	h := newHarness(t)
	enqueued := h.createRunningRunResult(t, "pending approval completion")
	args, err := structpb.NewStruct(map[string]any{"path": "note.txt", "content": "hello"})
	if err != nil {
		t.Fatal(err)
	}
	before := &turingv1.ToolCallBeacon{
		RunId:      enqueued.RunID,
		TraceId:    enqueued.TraceID,
		ToolCallId: "call_pending_approval",
		AgentId:    turingv1.AgentId_AGENT_ID_GENERAL_ASSISTANT,
		ServerName: "files",
		ToolName:   "files.update",
		Phase:      turingv1.ToolCallPhase_TOOL_CALL_PHASE_BEFORE,
		Args:       args,
	}
	if err := h.service.applyUpdate(context.Background(), &turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_ToolBeacon{ToolBeacon: before}}); err != nil {
		t.Fatal(err)
	}
	err = h.service.applyUpdate(context.Background(), &turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_ToolBeacon{ToolBeacon: &turingv1.ToolCallBeacon{
		RunId:         enqueued.RunID,
		TraceId:       enqueued.TraceID,
		ToolCallId:    before.ToolCallId,
		AgentId:       turingv1.AgentId_AGENT_ID_GENERAL_ASSISTANT,
		ServerName:    "files",
		ToolName:      "files.update",
		Phase:         turingv1.ToolCallPhase_TOOL_CALL_PHASE_AFTER,
		Status:        turingv1.ToolCallStatus_TOOL_CALL_STATUS_COMPLETED,
		Args:          args,
		ResultSummary: "updated",
	}}})
	if status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("pending approval completion error = %v, want FailedPrecondition", err)
	}
	var toolCallStatus string
	if err := h.database.QueryRowContext(context.Background(), `SELECT status FROM tool_calls WHERE id = ?`, before.ToolCallId).Scan(&toolCallStatus); err != nil {
		t.Fatal(err)
	}
	if toolCallStatus != "approval_required" {
		t.Fatalf("tool call status = %q, want approval_required", toolCallStatus)
	}
}

func TestToolBeaconAfterRequiresImmutableIdentityAndSingleTerminalTransition(t *testing.T) {
	newCompletedBeacon := func(enqueued repository.EnqueueUserMessageResult) *turingv1.ToolCallBeacon {
		return &turingv1.ToolCallBeacon{
			RunId:           enqueued.RunID,
			TraceId:         enqueued.TraceID,
			ToolCallId:      "call_identity",
			ModelToolCallId: "provider_call_1",
			AgentId:         turingv1.AgentId_AGENT_ID_GENERAL_ASSISTANT,
			ServerName:      "system",
			ToolName:        "system.time",
			Phase:           turingv1.ToolCallPhase_TOOL_CALL_PHASE_AFTER,
			Status:          turingv1.ToolCallStatus_TOOL_CALL_STATUS_COMPLETED,
			ResultSummary:   "noon",
			DurationMs:      12,
		}
	}
	recordBefore := func(t *testing.T, h *harness, enqueued repository.EnqueueUserMessageResult) {
		t.Helper()
		if err := h.service.applyUpdate(context.Background(), &turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_ToolBeacon{ToolBeacon: &turingv1.ToolCallBeacon{
			RunId:           enqueued.RunID,
			TraceId:         enqueued.TraceID,
			ToolCallId:      "call_identity",
			ModelToolCallId: "provider_call_1",
			AgentId:         turingv1.AgentId_AGENT_ID_GENERAL_ASSISTANT,
			ServerName:      "system",
			ToolName:        "system.time",
			Phase:           turingv1.ToolCallPhase_TOOL_CALL_PHASE_BEFORE,
		}}}); err != nil {
			t.Fatal(err)
		}
	}

	t.Run("rejects mismatched identity", func(t *testing.T) {
		h := newHarness(t)
		enqueued := h.createRunningRunResult(t, "identity")
		recordBefore(t, h, enqueued)
		after := newCompletedBeacon(enqueued)
		after.ModelToolCallId = "provider_call_other"
		err := h.service.applyUpdate(context.Background(), &turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_ToolBeacon{ToolBeacon: after}})
		if status.Code(err) != codes.FailedPrecondition {
			t.Fatalf("mismatched after identity error = %v, want FailedPrecondition", err)
		}
	})

	t.Run("rejects unexpected model identity", func(t *testing.T) {
		h := newHarness(t)
		enqueued := h.createRunningRunResult(t, "identity")
		if err := h.service.applyUpdate(context.Background(), &turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_ToolBeacon{ToolBeacon: &turingv1.ToolCallBeacon{
			RunId:      enqueued.RunID,
			TraceId:    enqueued.TraceID,
			ToolCallId: "call_identity",
			AgentId:    turingv1.AgentId_AGENT_ID_GENERAL_ASSISTANT,
			ServerName: "system",
			ToolName:   "system.time",
			Phase:      turingv1.ToolCallPhase_TOOL_CALL_PHASE_BEFORE,
		}}}); err != nil {
			t.Fatal(err)
		}
		after := newCompletedBeacon(enqueued)
		err := h.service.applyUpdate(context.Background(), &turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_ToolBeacon{ToolBeacon: after}})
		if status.Code(err) != codes.FailedPrecondition {
			t.Fatalf("unexpected model identity error = %v, want FailedPrecondition", err)
		}
	})

	t.Run("retries identical terminal outcome without duplicate event", func(t *testing.T) {
		h := newHarness(t)
		enqueued := h.createRunningRunResult(t, "idempotent")
		recordBefore(t, h, enqueued)
		after := newCompletedBeacon(enqueued)
		update := &turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_ToolBeacon{ToolBeacon: after}}
		if err := h.service.applyUpdate(context.Background(), update); err != nil {
			t.Fatal(err)
		}
		if err := h.service.applyUpdate(context.Background(), update); err != nil {
			t.Fatalf("identical after retry: %v", err)
		}
		var events int
		if err := h.database.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM events WHERE run_id = ? AND type = 'tool.call.completed'`, enqueued.RunID).Scan(&events); err != nil {
			t.Fatal(err)
		}
		if events != 1 {
			t.Fatalf("terminal tool events = %d, want 1", events)
		}
	})

	t.Run("retries identical terminal outcome after run completes", func(t *testing.T) {
		h := newHarness(t)
		enqueued := h.createRunningRunResult(t, "completed idempotent")
		recordBefore(t, h, enqueued)
		after := newCompletedBeacon(enqueued)
		update := &turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_ToolBeacon{ToolBeacon: after}}
		if err := h.service.applyUpdate(context.Background(), update); err != nil {
			t.Fatal(err)
		}
		completeRunFixture(t, h, enqueued.RunID, "", "")
		if err := h.service.applyUpdate(context.Background(), update); err != nil {
			t.Fatalf("identical after retry after completion: %v", err)
		}
		after.ResultSummary = "different"
		err := h.service.applyUpdate(context.Background(), &turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_ToolBeacon{ToolBeacon: after}})
		if status.Code(err) != codes.FailedPrecondition {
			t.Fatalf("conflicting terminal retry after completion error = %v, want FailedPrecondition", err)
		}
		var events int
		if err := h.database.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM events WHERE run_id = ? AND type = 'tool.call.completed'`, enqueued.RunID).Scan(&events); err != nil {
			t.Fatal(err)
		}
		if events != 1 {
			t.Fatalf("terminal tool events = %d, want 1", events)
		}
	})

	t.Run("rejects conflicting terminal retry", func(t *testing.T) {
		h := newHarness(t)
		enqueued := h.createRunningRunResult(t, "conflict")
		recordBefore(t, h, enqueued)
		after := newCompletedBeacon(enqueued)
		if err := h.service.applyUpdate(context.Background(), &turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_ToolBeacon{ToolBeacon: after}}); err != nil {
			t.Fatal(err)
		}
		after.ResultSummary = "different"
		err := h.service.applyUpdate(context.Background(), &turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_ToolBeacon{ToolBeacon: after}})
		if status.Code(err) != codes.FailedPrecondition {
			t.Fatalf("conflicting terminal retry error = %v, want FailedPrecondition", err)
		}
	})

	t.Run("rejects after from denied call", func(t *testing.T) {
		h := newHarness(t)
		enqueued := h.createRunningRunResult(t, "illegal transition")
		if err := h.service.applyUpdate(context.Background(), &turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_ToolBeacon{ToolBeacon: &turingv1.ToolCallBeacon{
			RunId:      enqueued.RunID,
			TraceId:    enqueued.TraceID,
			ToolCallId: "call_denied_transition",
			AgentId:    turingv1.AgentId_AGENT_ID_GENERAL_ASSISTANT,
			ServerName: "system",
			ToolName:   "system.shell",
			Phase:      turingv1.ToolCallPhase_TOOL_CALL_PHASE_BEFORE,
		}}}); err != nil {
			t.Fatal(err)
		}
		err := h.service.applyUpdate(context.Background(), &turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_ToolBeacon{ToolBeacon: &turingv1.ToolCallBeacon{
			RunId:      enqueued.RunID,
			TraceId:    enqueued.TraceID,
			ToolCallId: "call_denied_transition",
			AgentId:    turingv1.AgentId_AGENT_ID_GENERAL_ASSISTANT,
			ServerName: "system",
			ToolName:   "system.shell",
			Phase:      turingv1.ToolCallPhase_TOOL_CALL_PHASE_AFTER,
			Status:     turingv1.ToolCallStatus_TOOL_CALL_STATUS_COMPLETED,
		}}})
		if status.Code(err) != codes.FailedPrecondition {
			t.Fatalf("after denied call error = %v, want FailedPrecondition", err)
		}
	})
}

func TestToolBeaconAfterAcceptsFailedCleanupForTerminalApproval(t *testing.T) {
	for _, test := range []struct {
		name     string
		terminal func(*harness, string) error
	}{
		{
			name: "denied",
			terminal: func(h *harness, approvalID string) error {
				_, err := h.approvals.DenyApproval(context.Background(), &turingv1.DenyApprovalRequest{ApprovalId: approvalID, Reason: "no"})
				return err
			},
		},
		{
			name: "expired",
			terminal: func(h *harness, approvalID string) error {
				if _, err := h.database.ExecContext(context.Background(), `UPDATE approvals SET expires_at = ? WHERE id = ?`, time.Now().Add(-time.Second).Format(time.RFC3339Nano), approvalID); err != nil {
					return err
				}
				_, err := h.approvals.ApproveApproval(context.Background(), &turingv1.ApproveApprovalRequest{ApprovalId: approvalID})
				if status.Code(err) != codes.FailedPrecondition {
					return fmt.Errorf("ApproveApproval error = %v, want FailedPrecondition", err)
				}
				return nil
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			h := newHarness(t)
			enqueued := h.createRunningRunResult(t, "terminal approval cleanup")
			before := &turingv1.ToolCallBeacon{
				RunId: enqueued.RunID, TraceId: enqueued.TraceID, ToolCallId: "call_terminal_approval_cleanup",
				AgentId: turingv1.AgentId_AGENT_ID_GENERAL_ASSISTANT, ServerName: "files", ToolName: "files.update",
				Phase: turingv1.ToolCallPhase_TOOL_CALL_PHASE_BEFORE, Args: mustStruct(t, map[string]any{"path": "note.txt", "content": "hello"}),
			}
			decision, err := h.service.handleToolBeacon(context.Background(), before)
			if err != nil {
				t.Fatal(err)
			}
			if decision.GetApprovalId() == "" {
				t.Fatalf("before decision = %+v, want approval", decision)
			}
			if err := test.terminal(h, decision.GetApprovalId()); err != nil {
				t.Fatal(err)
			}
			cleanup := &turingv1.ToolCallBeacon{
				RunId: before.RunId, TraceId: before.TraceId, ToolCallId: before.ToolCallId,
				AgentId: before.AgentId, ServerName: before.ServerName, ToolName: before.ToolName,
				Phase: turingv1.ToolCallPhase_TOOL_CALL_PHASE_AFTER, Status: turingv1.ToolCallStatus_TOOL_CALL_STATUS_FAILED,
				Args:  before.Args,
				Error: &turingv1.ToolCallError{Code: "approval_wait_failed", Message: "context canceled"},
			}
			if err := h.service.applyUpdate(context.Background(), &turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_ToolBeacon{ToolBeacon: cleanup}}); err != nil {
				t.Fatalf("failed cleanup AFTER = %v", err)
			}
			cleanup.Status = turingv1.ToolCallStatus_TOOL_CALL_STATUS_COMPLETED
			if err := h.service.applyUpdate(context.Background(), &turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_ToolBeacon{ToolBeacon: cleanup}}); status.Code(err) != codes.FailedPrecondition {
				t.Fatalf("conflicting terminal cleanup AFTER = %v, want FailedPrecondition", err)
			}
		})
	}
}

func TestNotifyApprovalUpdatedSendsTokenToAssignedWorker(t *testing.T) {
	h := newHarness(t)
	commands := make(chan workerCommand, 1)
	h.service.mu.Lock()
	h.service.workers["worker-approval-update"] = &worker{commands: commands, maxConcurrent: 1, assignments: map[string]assignment{"run_approval": {runID: "run_approval", jobID: "job_approval"}}}
	h.service.mu.Unlock()

	if err := h.service.NotifyApprovalUpdated(context.Background(), "run_approval", "appr_1", "approved", "header.payload.signature"); err != nil {
		t.Fatal(err)
	}

	select {
	case cmd := <-commands:
		update := cmd.command.GetApprovalUpdated()
		if update.GetApprovalId() != "appr_1" || update.GetStatus() != "approved" || update.GetApprovalToken() != "header.payload.signature" {
			t.Fatalf("approval_updated = %+v", update)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for approval_updated command")
	}
}

func TestWorkerCloseWaitsForInFlightUpdate(t *testing.T) {
	connectedWorker := &worker{commands: make(chan workerCommand), assignments: map[string]assignment{"run_1": {runID: "run_1", jobID: "job_1"}}}
	release, err := connectedWorker.beginUpdate(&turingv1.RuntimeUpdate{
		Update: &turingv1.RuntimeUpdate_RunCancelledAck{RunCancelledAck: &turingv1.RuntimeCancelledAck{RunId: "run_1"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	closed := make(chan []assignment, 1)
	go func() {
		closed <- connectedWorker.close()
	}()
	select {
	case assignments := <-closed:
		t.Fatalf("worker close completed while update was in flight: %+v", assignments)
	case <-time.After(20 * time.Millisecond):
	}
	release()
	select {
	case assignments := <-closed:
		if len(assignments) != 1 || assignments[0] != (assignment{jobID: "job_1", runID: "run_1"}) {
			t.Fatalf("closed assignments = %+v, want run_1", assignments)
		}
	case <-time.After(time.Second):
		t.Fatal("worker close did not finish after update completed")
	}
}

func TestCancelRunWaitsForCommandBufferSpace(t *testing.T) {
	h := newHarness(t)
	commands := make(chan workerCommand, 1)
	commands <- workerCommand{command: &turingv1.RuntimeCommand{Command: &turingv1.RuntimeCommand_WorkerAccepted{WorkerAccepted: &turingv1.RuntimeWorkerAccepted{WorkerId: "worker-buffered"}}}}
	h.service.mu.Lock()
	h.service.workers["worker-buffered"] = &worker{commands: commands, maxConcurrent: 1, assignments: map[string]assignment{"run_buffered": {runID: "run_buffered", jobID: "job_buffered"}}}
	h.service.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	done := make(chan struct{})
	go func() {
		h.service.CancelRun(ctx, "run_buffered", "client_cancelled")
		close(done)
	}()
	time.Sleep(20 * time.Millisecond)
	<-commands
	select {
	case <-done:
		t.Fatal("CancelRun returned before buffer space was available")
	default:
	}
	select {
	case cmd := <-commands:
		cancel := cmd.command.GetRunCancelled()
		if cancel == nil || cancel.RunId != "run_buffered" {
			t.Fatalf("command = %+v, want run_cancelled", cmd)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for cancellation command")
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("CancelRun did not return after cancellation delivery")
	}
}

func TestWorkerDisconnectFencesDeliveredJobUntilRecovery(t *testing.T) {
	h := newHarness(t)
	enqueued := h.enqueueRun(t, "disconnect")
	client := h.runtimeClient(t)
	stream, err := client.ConnectWorker(h.internalContext())
	if err != nil {
		t.Fatal(err)
	}
	if err := stream.Send(&turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_WorkerReady{WorkerReady: &turingv1.RuntimeWorkerReady{WorkerId: "worker-disconnect", AgentId: turingv1.AgentId_AGENT_ID_GENERAL_ASSISTANT, MaxConcurrentRuns: 1}}}); err != nil {
		t.Fatal(err)
	}
	recvUntil(t, stream, func(cmd *turingv1.RuntimeCommand) bool {
		assigned := cmd.GetRunAssigned()
		return assigned != nil && assigned.RunId == enqueued.RunID
	})
	if err := stream.CloseSend(); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		run, err := h.repo.GetRun(context.Background(), enqueued.RunID)
		if err == nil && run.Status == "recovering" && run.ExecutionActive && run.ExecutionState == "uncertain" {
			var jobStatus string
			if err := h.database.QueryRowContext(context.Background(), `SELECT status FROM jobs WHERE id = ?`, enqueued.JobID).Scan(&jobStatus); err != nil {
				t.Fatal(err)
			}
			if jobStatus != "in_progress" {
				t.Fatalf("job status = %q, want in_progress while execution is uncertain", jobStatus)
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("assigned job was not fenced after worker disconnect")
}

func TestWorkerDisconnectTerminalizesPendingApprovalAndFencesExecution(t *testing.T) {
	h := newHarness(t)
	enqueued := h.enqueueRun(t, "disconnect pending approval")
	client := h.runtimeClient(t)
	stream, err := client.ConnectWorker(h.internalContext())
	if err != nil {
		t.Fatal(err)
	}
	if err := stream.Send(&turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_WorkerReady{WorkerReady: &turingv1.RuntimeWorkerReady{
		WorkerId: "worker-disconnect-pending-approval", AgentId: turingv1.AgentId_AGENT_ID_GENERAL_ASSISTANT, MaxConcurrentRuns: 1,
	}}}); err != nil {
		t.Fatal(err)
	}
	recvUntil(t, stream, func(cmd *turingv1.RuntimeCommand) bool {
		return cmd.GetRunAssigned() != nil && cmd.GetRunAssigned().RunId == enqueued.RunID
	})
	if err := stream.Send(&turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_ToolBeacon{ToolBeacon: &turingv1.ToolCallBeacon{
		RunId: enqueued.RunID, TraceId: enqueued.TraceID, ToolCallId: "call_disconnect_pending_approval",
		AgentId: turingv1.AgentId_AGENT_ID_GENERAL_ASSISTANT, ServerName: "files", ToolName: "files.update",
		Phase: turingv1.ToolCallPhase_TOOL_CALL_PHASE_BEFORE, Args: mustStruct(t, map[string]any{"path": "note.txt", "content": "hello"}),
	}}}); err != nil {
		t.Fatal(err)
	}
	decision := recvUntil(t, stream, func(cmd *turingv1.RuntimeCommand) bool {
		return cmd.GetToolPolicyDecision() != nil && cmd.GetToolPolicyDecision().ToolCallId == "call_disconnect_pending_approval"
	}).GetToolPolicyDecision()
	if decision.GetApprovalId() == "" {
		t.Fatalf("approval decision = %+v, want approval ID", decision)
	}
	if err := stream.CloseSend(); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		var statusValue string
		var active int
		var executionState string
		var acknowledgedAt sql.NullString
		if err := h.database.QueryRowContext(context.Background(), `SELECT status, execution_active, execution_state, execution_exit_acknowledged_at FROM agent_runs WHERE id = ?`, enqueued.RunID).Scan(&statusValue, &active, &executionState, &acknowledgedAt); err == nil &&
			statusValue == "failed" && active == 1 && executionState == "uncertain" && !acknowledgedAt.Valid {
			break
		}
		time.Sleep(time.Millisecond)
	}
	var statusValue string
	var active int
	var executionState string
	if err := h.database.QueryRowContext(context.Background(), `SELECT status, execution_active, execution_state FROM agent_runs WHERE id = ?`, enqueued.RunID).Scan(&statusValue, &active, &executionState); err != nil {
		t.Fatal(err)
	}
	if statusValue != "failed" || active != 1 || executionState != "uncertain" {
		t.Fatalf("pending approval disconnect state status=%q active=%d execution_state=%q, want failed/1/uncertain", statusValue, active, executionState)
	}
	expired := time.Now().Add(-time.Second)
	if _, err := h.database.ExecContext(context.Background(), `
		UPDATE agent_runs
		SET execution_lease_expires_at = ?, execution_lease_expires_at_ns = ?
		WHERE id = ?
	`, expired.Format("2006-01-02T15:04:05.000000000Z"), expired.UnixNano(), enqueued.RunID); err != nil {
		t.Fatal(err)
	}
	if err := h.service.RecoverOrphanedAssignments(context.Background()); err != nil {
		t.Fatal(err)
	}
	deadline = time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		run, err := h.repo.GetRun(context.Background(), enqueued.RunID)
		if err == nil && !run.ExecutionActive {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("pending approval execution fence was not released by stale recovery")
}

func (h *harness) enqueueRun(t *testing.T, content string) repository.EnqueueUserMessageResult {
	t.Helper()
	session, err := h.repo.CreateSession(context.Background(), "Runtime")
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	enqueued, err := h.repo.EnqueueUserMessage(context.Background(), repository.EnqueueUserMessageInput{
		SessionID:     session.SessionID,
		Content:       content,
		AgentID:       "general_assistant",
		ModelProvider: "ollama",
		Model:         "llama3.2",
	})
	if err != nil {
		t.Fatalf("EnqueueUserMessage: %v", err)
	}
	return enqueued
}

func TestRunCompletedPublishesTerminalEvent(t *testing.T) {
	h := newHarness(t)
	enqueued := h.createRunningRunResult(t, "complete me")
	ch, unsubscribe := h.bus.Subscribe(enqueued.SessionID)
	defer unsubscribe()

	err := h.service.applyUpdate(context.Background(), &turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_RunCompleted{RunCompleted: &turingv1.RuntimeRunCompleted{
		RunId:              enqueued.RunID,
		AssistantMessageId: enqueued.AssistantMessageID,
		Content:            "done",
		Usage:              mustStruct(t, map[string]any{"prompt_tokens": float64(3), "completion_tokens": float64(4)}),
	}}})
	if err != nil {
		t.Fatal(err)
	}

	event := recvBusEvent(t, ch, func(event events.Event) bool {
		return event.Type == "agent.run.completed" && event.RunID == enqueued.RunID
	})
	if event.TraceID != enqueued.TraceID {
		t.Fatalf("terminal event trace_id = %q, want %q", event.TraceID, enqueued.TraceID)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(event.PayloadJSON), &payload); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	if payload["runId"] != enqueued.RunID || payload["assistantMessageId"] != enqueued.AssistantMessageID {
		t.Fatalf("payload = %+v", payload)
	}
	// The provider's free-form usage object is not republished here. It is
	// provider-controlled content, and a completion event carries the run's
	// identity and canonical state; the counts the product actually uses are
	// the typed ones stored on the run and read through telemetry.
	if _, exists := payload["usage"]; exists {
		t.Fatalf("completion payload republished provider usage: %+v", payload)
	}
}

func TestRunFailedPublishesTerminalEvent(t *testing.T) {
	h := newHarness(t)
	enqueued := h.createRunningRunResult(t, "fail me")
	ch, unsubscribe := h.bus.Subscribe(enqueued.SessionID)
	defer unsubscribe()

	err := h.service.applyUpdate(context.Background(), &turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_RunFailed{RunFailed: &turingv1.RuntimeRunFailed{
		RunId:         enqueued.RunID,
		Code:          "model_error",
		Message:       "model failed",
		FailureOrigin: turingv1.FailureOrigin_FAILURE_ORIGIN_EXTERNAL_PROVIDER,
	}}})
	if err != nil {
		t.Fatal(err)
	}

	event := recvBusEvent(t, ch, func(event events.Event) bool {
		return event.Type == "agent.run.failed" && event.RunID == enqueued.RunID
	})
	var payload struct {
		RunID     string `json:"runId"`
		Code      string `json:"code"`
		Message   string `json:"message"`
		Retryable bool   `json:"retryable"`
	}
	if err := json.Unmarshal([]byte(event.PayloadJSON), &payload); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	if payload.RunID != enqueued.RunID || payload.Code != "model_error" || payload.Retryable {
		t.Fatalf("payload = %+v", payload)
	}
	if payload.Message != "" {
		t.Fatalf("failure payload republished the worker's message %q", payload.Message)
	}
}

func TestRunFailedPublishesDependentLifecycleEventsInOrder(t *testing.T) {
	h := newHarness(t)
	enqueued := h.createRunningRunResult(t, "fail pending approval")
	if err := h.repo.RecordToolCallBefore(context.Background(), repository.ToolCallRecord{
		ToolCallID: "call_run_failed", RunID: enqueued.RunID, ModelToolCallID: "model_run_failed",
		Status: "approval_required",
	}, "general_assistant", "files", "files.update", `{"path":"note.txt"}`, "sha256:test"); err != nil {
		t.Fatal(err)
	}
	if _, err := h.repo.CreateApproval(context.Background(), enqueued.RunID, "call_run_failed", "general_assistant", "files.update", `{"path":"note.txt"}`, "sha256:test", "2099-01-01T00:00:00Z"); err != nil {
		t.Fatal(err)
	}
	ch, unsubscribe := h.bus.Subscribe(enqueued.SessionID)
	defer unsubscribe()

	err := h.service.applyUpdate(context.Background(), &turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_RunFailed{RunFailed: &turingv1.RuntimeRunFailed{
		RunId: enqueued.RunID, Code: "model_error", Message: "model failed",
	}}})
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for len(got) < 3 {
		event := recvBusEvent(t, ch, func(event events.Event) bool {
			return event.RunID == enqueued.RunID &&
				(event.Type == "approval.expired" || event.Type == "tool.call.failed" || event.Type == "agent.run.failed")
		})
		got = append(got, event.Type)
	}
	if want := []string{"approval.expired", "tool.call.failed", "agent.run.failed"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("published failure lifecycle = %v, want %v", got, want)
	}
}

func mustStruct(t *testing.T, values map[string]any) *structpb.Struct {
	t.Helper()
	out, err := structpb.NewStruct(values)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func recvBusEvent(t *testing.T, ch <-chan events.Event, match func(events.Event) bool) events.Event {
	t.Helper()
	deadline := time.After(2 * time.Second)
	for {
		select {
		case <-deadline:
			t.Fatal("timed out waiting for bus event")
		case event := <-ch:
			if match(event) {
				return event
			}
		}
	}
}

func recvUntil(t *testing.T, stream turingv1.RuntimeService_ConnectWorkerClient, match func(*turingv1.RuntimeCommand) bool) *turingv1.RuntimeCommand {
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
				t.Fatal(result.err)
			}
			if match(result.cmd) {
				return result.cmd
			}
		}
	}
}

func recvUntilEither(t *testing.T, first turingv1.RuntimeService_ConnectWorkerClient, second turingv1.RuntimeService_ConnectWorkerClient, match func(*turingv1.RuntimeCommand) bool) *turingv1.RuntimeCommand {
	t.Helper()
	type receivedCommand struct {
		cmd *turingv1.RuntimeCommand
		err error
	}
	received := make(chan receivedCommand, 2)
	for _, stream := range []turingv1.RuntimeService_ConnectWorkerClient{first, second} {
		stream := stream
		go func() {
			cmd, err := stream.Recv()
			received <- receivedCommand{cmd: cmd, err: err}
		}()
	}
	select {
	case result := <-received:
		if result.err != nil {
			t.Fatal(result.err)
		}
		if match(result.cmd) {
			return result.cmd
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for runtime command")
	}
	select {
	case result := <-received:
		if result.err != nil {
			t.Fatal(result.err)
		}
		if match(result.cmd) {
			return result.cmd
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for runtime command")
	}
	t.Fatal("received no matching runtime command")
	return nil
}

// ---------------------------------------------------------------------------
// Assignment, version, and ownership-proof identity.
//
// Every lifecycle change a worker can cause has to name the state it was
// computed against, or a fenced predecessor can keep writing the run's story
// from a state the orchestrator has already moved past.
// ---------------------------------------------------------------------------

// connectAssignedWorker connects one worker and returns the assignment it was
// handed, so the version tests below start from a real dispatch rather than a
// hand-built row.
func (h *harness) connectAssignedWorker(
	t *testing.T,
	workerID string,
	runID string,
) (turingv1.RuntimeService_ConnectWorkerClient, *turingv1.AgentJob) {
	t.Helper()
	stream, err := h.runtimeClient(t).ConnectWorker(h.internalContext())
	if err != nil {
		t.Fatalf("ConnectWorker: %v", err)
	}
	t.Cleanup(func() { _ = stream.CloseSend() })
	if err := stream.Send(&turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_WorkerReady{
		WorkerReady: &turingv1.RuntimeWorkerReady{
			WorkerId: workerID, AgentId: turingv1.AgentId_AGENT_ID_GENERAL_ASSISTANT, MaxConcurrentRuns: 1,
		},
	}}); err != nil {
		t.Fatalf("send worker_ready: %v", err)
	}
	assigned := recvUntil(t, stream, func(cmd *turingv1.RuntimeCommand) bool {
		job := cmd.GetRunAssigned()
		return job != nil && (runID == "" || job.GetRunId() == runID)
	}).GetRunAssigned()
	return stream, assigned
}

func (h *harness) runState(t *testing.T, runID string) repository.RunState {
	t.Helper()
	state, err := h.repo.GetRunState(context.Background(), runID)
	if err != nil {
		t.Fatalf("GetRunState: %v", err)
	}
	return state
}

// fenceOwnership drives the run into recovering the way a lost worker does, so
// the proof tests below start from real fenced truth rather than a status the
// test wrote by hand.
func (h *harness) fenceOwnership(t *testing.T, runID string, workerID string, attemptID string) repository.RunState {
	t.Helper()
	before := h.runState(t, runID)
	result, err := h.repo.FenceRunOwnership(context.Background(), repository.FenceRunOwnershipInput{
		RunID:                runID,
		ExpectedStateVersion: before.StateVersion,
		WorkerID:             workerID,
		AssignmentAttemptID:  attemptID,
	})
	if err != nil {
		t.Fatalf("FenceRunOwnership: %v", err)
	}
	if result.State.Lifecycle != "recovering" {
		t.Fatalf("fenced lifecycle = %q, want recovering", result.State.Lifecycle)
	}
	return result.State
}

func TestAssignedJobCarriesExpectedStateVersionAndDurableAttemptID(t *testing.T) {
	h := newHarness(t)
	enqueued := h.enqueueRun(t, "version me")
	_, assigned := h.connectAssignedWorker(t, "worker-assignment-identity", enqueued.RunID)

	run, err := h.repo.GetRun(context.Background(), enqueued.RunID)
	if err != nil {
		t.Fatalf("GetRun: %v", err)
	}
	state := h.runState(t, enqueued.RunID)
	if state.StateVersion < 1 {
		t.Fatalf("run state version = %d, want at least 1", state.StateVersion)
	}
	if assigned.GetExpectedStateVersion() != state.StateVersion {
		t.Fatalf("assignment expected_state_version = %d, want %d",
			assigned.GetExpectedStateVersion(), state.StateVersion)
	}
	if run.ExecutionAttemptID == "" {
		t.Fatal("run has no durable execution attempt ID")
	}
	if assigned.GetAssignmentAttemptId() != run.ExecutionAttemptID {
		t.Fatalf("assignment attempt = %q, want the durable %q",
			assigned.GetAssignmentAttemptId(), run.ExecutionAttemptID)
	}
}

func TestTerminalReportWithWrongExpectedVersionIsFenced(t *testing.T) {
	h := newHarness(t)
	enqueued := h.enqueueRun(t, "stale terminal")
	stream, assigned := h.connectAssignedWorker(t, "worker-stale-terminal", enqueued.RunID)
	before := h.runState(t, enqueued.RunID)

	if err := stream.Send(&turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_RunCompleted{
		RunCompleted: &turingv1.RuntimeRunCompleted{
			RunId:                enqueued.RunID,
			AssistantMessageId:   assigned.GetAssistantMessageId(),
			Content:              "answered against a state that moved",
			ExpectedStateVersion: before.StateVersion + 5,
		},
	}}); err != nil {
		t.Fatalf("send run_completed: %v", err)
	}
	expectStreamRejection(t, stream, "stale terminal report")

	// Refusing the report tears the stream down, and the ordinary
	// reconciliation fence then moves the run to recovering — the run's
	// ownership really is in doubt. What must not happen is the report landing:
	// no terminal outcome, and no completion event.
	after := h.runState(t, enqueued.RunID)
	if isTerminalRunStatus(after.Lifecycle) {
		t.Fatalf("stale terminal report terminalized the run as %s", after.Lifecycle)
	}
	if got := countRunEvents(t, h, enqueued.RunID, "agent.run.completed"); got != 0 {
		t.Fatalf("agent.run.completed events = %d, want none", got)
	}
	run, err := h.repo.GetRun(context.Background(), enqueued.RunID)
	if err != nil {
		t.Fatalf("GetRun: %v", err)
	}
	if run.AssistantContent != "" {
		t.Fatalf("stale terminal report persisted content %q", run.AssistantContent)
	}
	if after.StateVersion < before.StateVersion {
		t.Fatalf("run version went backwards to %d from %d", after.StateVersion, before.StateVersion)
	}
}

func TestGenericRuntimeEventCannotResumeRecoveringWithoutVersionReply(t *testing.T) {
	h := newHarness(t)
	enqueued := h.enqueueRun(t, "generic narration")
	stream, assigned := h.connectAssignedWorker(t, "worker-generic-event", enqueued.RunID)
	fenced := h.fenceOwnership(t, enqueued.RunID, "worker-generic-event", assigned.GetAssignmentAttemptId())

	if err := stream.Send(&turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_Event{
		Event: &turingv1.TuringEvent{
			RunId: enqueued.RunID,
			Type:  turingv1.TuringEventType_TURING_EVENT_TYPE_AGENT_RUN_STEP,
		},
	}}); err != nil {
		t.Fatalf("send runtime event: %v", err)
	}
	expectStreamRejection(t, stream, "generic runtime event under recovering")

	after := h.runState(t, enqueued.RunID)
	if after.Lifecycle != "recovering" || after.StateVersion != fenced.StateVersion {
		t.Fatalf("run = %s at version %d, want recovering at %d",
			after.Lifecycle, after.StateVersion, fenced.StateVersion)
	}
}

func TestSameAttemptAssignmentRefreshResumesRecoveringAndUpdatesWorkerVersion(t *testing.T) {
	h := newHarness(t)
	enqueued := h.enqueueRun(t, "prove ownership")
	stream, assigned := h.connectAssignedWorker(t, "worker-reconnect-proof", enqueued.RunID)
	fenced := h.fenceOwnership(t, enqueued.RunID, "worker-reconnect-proof", assigned.GetAssignmentAttemptId())

	if err := stream.Send(&turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_Heartbeat{
		Heartbeat: &turingv1.RuntimeHeartbeat{WorkerId: "worker-reconnect-proof"},
	}}); err != nil {
		t.Fatalf("send heartbeat: %v", err)
	}
	refresh := recvUntil(t, stream, func(cmd *turingv1.RuntimeCommand) bool {
		job := cmd.GetRunAssigned()
		return job != nil && job.GetRunId() == enqueued.RunID
	}).GetRunAssigned()

	after := h.runState(t, enqueued.RunID)
	if after.Lifecycle != "running" {
		t.Fatalf("run lifecycle = %q, want running after the ownership proof", after.Lifecycle)
	}
	if after.StateVersion != fenced.StateVersion+1 {
		t.Fatalf("resumed version = %d, want %d", after.StateVersion, fenced.StateVersion+1)
	}
	if refresh.GetAssignmentAttemptId() != assigned.GetAssignmentAttemptId() {
		t.Fatalf("refresh attempt = %q, want the unchanged %q",
			refresh.GetAssignmentAttemptId(), assigned.GetAssignmentAttemptId())
	}
	if refresh.GetExpectedStateVersion() != after.StateVersion {
		t.Fatalf("refresh expected_state_version = %d, want the committed %d",
			refresh.GetExpectedStateVersion(), after.StateVersion)
	}
}

func TestToolBeaconProofReturnsResultingVersionBeforeContinuation(t *testing.T) {
	h := newHarness(t)
	enqueued := h.enqueueRun(t, "beacon proof")
	stream, assigned := h.connectAssignedWorker(t, "worker-beacon-proof", enqueued.RunID)
	run, err := h.repo.GetRun(context.Background(), enqueued.RunID)
	if err != nil {
		t.Fatalf("GetRun: %v", err)
	}
	fenced := h.fenceOwnership(t, enqueued.RunID, "worker-beacon-proof", assigned.GetAssignmentAttemptId())

	if err := stream.Send(&turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_ToolBeacon{
		ToolBeacon: &turingv1.ToolCallBeacon{
			Phase:      turingv1.ToolCallPhase_TOOL_CALL_PHASE_BEFORE,
			ToolCallId: "call_beacon_proof",
			AgentId:    turingv1.AgentId_AGENT_ID_GENERAL_ASSISTANT,
			ServerName: "system",
			ToolName:   "system.time",
			RunId:      enqueued.RunID,
			TraceId:    run.TraceID,
		},
	}}); err != nil {
		t.Fatalf("send tool beacon: %v", err)
	}
	decision := recvUntil(t, stream, func(cmd *turingv1.RuntimeCommand) bool {
		return cmd.GetToolPolicyDecision() != nil
	}).GetToolPolicyDecision()

	after := h.runState(t, enqueued.RunID)
	if after.Lifecycle != "running" {
		t.Fatalf("run lifecycle = %q, want running after the beacon proof", after.Lifecycle)
	}
	if after.StateVersion != fenced.StateVersion+1 {
		t.Fatalf("resumed version = %d, want %d", after.StateVersion, fenced.StateVersion+1)
	}
	if decision.GetRunStateVersion() != after.StateVersion {
		t.Fatalf("decision run_state_version = %d, want the committed %d",
			decision.GetRunStateVersion(), after.StateVersion)
	}
	if decision.GetDecision() != turingv1.ToolPolicyDecision_DECISION_ALLOW {
		t.Fatalf("decision = %s, want ALLOW for a safe tool", decision.GetDecision())
	}
}

func TestUnownedUpdateCannotBypassRecoveringFence(t *testing.T) {
	h := newHarness(t)
	enqueued := h.enqueueRun(t, "unowned proof")
	_, assigned := h.connectAssignedWorker(t, "worker-unowned-owner", enqueued.RunID)
	run, err := h.repo.GetRun(context.Background(), enqueued.RunID)
	if err != nil {
		t.Fatalf("GetRun: %v", err)
	}
	fenced := h.fenceOwnership(t, enqueued.RunID, "worker-unowned-owner", assigned.GetAssignmentAttemptId())

	// A second worker holds no assignment for this run, so nothing it says can
	// prove which attempt owns it — which is the whole doubt recovering names.
	bystander, err := h.runtimeClient(t).ConnectWorker(h.internalContext())
	if err != nil {
		t.Fatalf("ConnectWorker: %v", err)
	}
	defer func() { _ = bystander.CloseSend() }()
	if err := bystander.Send(&turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_WorkerReady{
		WorkerReady: &turingv1.RuntimeWorkerReady{
			WorkerId: "worker-unowned-bystander", AgentId: turingv1.AgentId_AGENT_ID_GENERAL_ASSISTANT, MaxConcurrentRuns: 1,
		},
	}}); err != nil {
		t.Fatalf("send worker_ready: %v", err)
	}
	recvUntil(t, bystander, func(cmd *turingv1.RuntimeCommand) bool { return cmd.GetWorkerAccepted() != nil })

	if err := bystander.Send(&turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_ToolBeacon{
		ToolBeacon: &turingv1.ToolCallBeacon{
			Phase:      turingv1.ToolCallPhase_TOOL_CALL_PHASE_BEFORE,
			ToolCallId: "call_unowned_proof",
			AgentId:    turingv1.AgentId_AGENT_ID_GENERAL_ASSISTANT,
			ServerName: "system",
			ToolName:   "system.time",
			RunId:      enqueued.RunID,
			TraceId:    run.TraceID,
		},
	}}); err != nil {
		t.Fatalf("send tool beacon: %v", err)
	}
	decision := recvUntil(t, bystander, func(cmd *turingv1.RuntimeCommand) bool {
		return cmd.GetToolPolicyDecision() != nil
	}).GetToolPolicyDecision()
	if decision.GetDecision() == turingv1.ToolPolicyDecision_DECISION_ALLOW {
		t.Fatalf("unowned beacon was allowed: %+v", decision)
	}
	if decision.GetRunStateVersion() != 0 {
		t.Fatalf("unowned beacon was answered with version %d, want none",
			decision.GetRunStateVersion())
	}

	after := h.runState(t, enqueued.RunID)
	if after.Lifecycle != "recovering" || after.StateVersion != fenced.StateVersion {
		t.Fatalf("run = %s at version %d, want recovering at %d",
			after.Lifecycle, after.StateVersion, fenced.StateVersion)
	}
}

func TestTerminalAfterFenceAndVersionedProofCommitsExactlyOnce(t *testing.T) {
	h := newHarness(t)
	enqueued := h.enqueueRun(t, "terminal after proof")
	stream, assigned := h.connectAssignedWorker(t, "worker-terminal-proof", enqueued.RunID)
	run, err := h.repo.GetRun(context.Background(), enqueued.RunID)
	if err != nil {
		t.Fatalf("GetRun: %v", err)
	}
	h.fenceOwnership(t, enqueued.RunID, "worker-terminal-proof", assigned.GetAssignmentAttemptId())

	if err := stream.Send(&turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_ToolBeacon{
		ToolBeacon: &turingv1.ToolCallBeacon{
			Phase:      turingv1.ToolCallPhase_TOOL_CALL_PHASE_BEFORE,
			ToolCallId: "call_terminal_proof",
			AgentId:    turingv1.AgentId_AGENT_ID_GENERAL_ASSISTANT,
			ServerName: "system",
			ToolName:   "system.time",
			RunId:      enqueued.RunID,
			TraceId:    run.TraceID,
		},
	}}); err != nil {
		t.Fatalf("send tool beacon: %v", err)
	}
	decision := recvUntil(t, stream, func(cmd *turingv1.RuntimeCommand) bool {
		return cmd.GetToolPolicyDecision() != nil
	}).GetToolPolicyDecision()
	proven := decision.GetRunStateVersion()
	if proven == 0 {
		t.Fatal("beacon proof returned no resulting version")
	}

	terminal := &turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_RunCompleted{
		RunCompleted: &turingv1.RuntimeRunCompleted{
			RunId:                enqueued.RunID,
			AssistantMessageId:   assigned.GetAssistantMessageId(),
			Content:              "answered after proving ownership",
			ExpectedStateVersion: proven,
		},
	}}
	if err := stream.Send(terminal); err != nil {
		t.Fatalf("send run_completed: %v", err)
	}
	waitForRunStatus(t, h, enqueued.RunID, "completed")
	committed := h.runState(t, enqueued.RunID)
	if committed.StateVersion != proven+1 {
		t.Fatalf("terminal version = %d, want %d", committed.StateVersion, proven+1)
	}

	// The same report again is the worker's retry, not a second outcome.
	if err := stream.Send(terminal); err != nil {
		t.Fatalf("resend run_completed: %v", err)
	}
	if err := stream.Send(&turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_Heartbeat{
		Heartbeat: &turingv1.RuntimeHeartbeat{WorkerId: "worker-terminal-proof"},
	}}); err != nil {
		t.Fatalf("send heartbeat: %v", err)
	}
	replayed := h.runState(t, enqueued.RunID)
	if replayed.StateVersion != committed.StateVersion {
		t.Fatalf("duplicate terminal moved the version to %d, want %d",
			replayed.StateVersion, committed.StateVersion)
	}
	if got := countRunEvents(t, h, enqueued.RunID, "agent.run.completed"); got != 1 {
		t.Fatalf("agent.run.completed events = %d, want exactly 1", got)
	}
}

func TestIsActiveRunStatusRejectsUnprovenRecoveringOwnership(t *testing.T) {
	for _, test := range []struct {
		runStatus string
		want      bool
	}{
		{runStatus: "running", want: true},
		{runStatus: "waiting_approval", want: true},
		{runStatus: "recovering", want: false},
		{runStatus: "queued", want: false},
		{runStatus: "completed", want: false},
		{runStatus: "failed", want: false},
		{runStatus: "cancelled", want: false},
		{runStatus: "", want: false},
	} {
		t.Run(test.runStatus, func(t *testing.T) {
			if got := isActiveRunStatus(test.runStatus); got != test.want {
				t.Fatalf("isActiveRunStatus(%q) = %v, want %v", test.runStatus, got, test.want)
			}
		})
	}
}

// expectStreamRejection waits for the orchestrator to refuse an update by
// failing the stream. A rejection that never arrives is the failure this
// asserts, so the wait is bounded rather than blocking on Recv forever.
func expectStreamRejection(t *testing.T, stream turingv1.RuntimeService_ConnectWorkerClient, what string) {
	t.Helper()
	received := make(chan error, 1)
	go func() {
		_, err := stream.Recv()
		received <- err
	}()
	select {
	case err := <-received:
		if err == nil {
			t.Fatalf("%s was accepted", what)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("%s was not rejected", what)
	}
}

func waitForRunStatus(t *testing.T, h *harness, runID string, want string) {
	t.Helper()
	deadline := time.After(2 * time.Second)
	for {
		state := h.runState(t, runID)
		if state.Lifecycle == want {
			return
		}
		select {
		case <-deadline:
			t.Fatalf("run %s is %q, want %q", runID, state.Lifecycle, want)
		case <-time.After(5 * time.Millisecond):
		}
	}
}

func countRunEvents(t *testing.T, h *harness, runID string, eventType string) int {
	t.Helper()
	var count int
	if err := h.database.QueryRowContext(context.Background(),
		`SELECT COUNT(*) FROM events WHERE run_id = ? AND type = ?`, runID, eventType,
	).Scan(&count); err != nil {
		t.Fatalf("count %s events: %v", eventType, err)
	}
	return count
}

// ---------------------------------------------------------------------------
// Typed failure ingestion.
//
// The orchestrator is the last place a worker's or a provider's words could
// become durable public truth, so every failure it writes is normalized from a
// typed origin and an allowlisted code before it reaches storage.
// ---------------------------------------------------------------------------

func (h *harness) runDiagnostics(t *testing.T, runID string) (string, sql.NullString) {
	t.Helper()
	var code string
	var message sql.NullString
	if err := h.database.QueryRowContext(context.Background(),
		`SELECT COALESCE(error_code, ''), error_message FROM agent_runs WHERE id = ?`, runID,
	).Scan(&code, &message); err != nil {
		t.Fatalf("read run diagnostics: %v", err)
	}
	return code, message
}

func (h *harness) terminalPayload(t *testing.T, runID string, eventType string) map[string]any {
	t.Helper()
	var payload string
	if err := h.database.QueryRowContext(context.Background(),
		`SELECT payload_json FROM events WHERE run_id = ? AND type = ? ORDER BY sequence DESC LIMIT 1`,
		runID, eventType,
	).Scan(&payload); err != nil {
		t.Fatalf("read %s payload: %v", eventType, err)
	}
	var decoded map[string]any
	if err := json.Unmarshal([]byte(payload), &decoded); err != nil {
		t.Fatalf("decode %s payload: %v", eventType, err)
	}
	return decoded
}

func TestOrchestratorTypedFailureIngestionNormalizesEveryRuntimeOrigin(t *testing.T) {
	tests := []struct {
		name          string
		origin        turingv1.FailureOrigin
		code          string
		retry         turingv1.AutomaticRetryClass
		wantLifecycle string
		wantCode      string
		wantReason    string
	}{
		{
			name: "context_assembly", origin: turingv1.FailureOrigin_FAILURE_ORIGIN_CONTEXT_ASSEMBLY,
			code: "message_fetch_failed", wantCode: "message_fetch_failed", wantReason: "internal_failure",
		},
		{
			name: "context_assembly_budget", origin: turingv1.FailureOrigin_FAILURE_ORIGIN_CONTEXT_ASSEMBLY,
			code: "context_budget_exceeded", wantCode: "context_budget_exceeded", wantReason: "context_limit",
		},
		{
			name: "external_provider", origin: turingv1.FailureOrigin_FAILURE_ORIGIN_EXTERNAL_PROVIDER,
			code: "external_agent_unavailable", wantCode: "external_agent_unavailable", wantReason: "provider_failure",
		},
		{
			name: "provider_configuration", origin: turingv1.FailureOrigin_FAILURE_ORIGIN_PROVIDER_CONFIGURATION,
			code: "model_provider_unavailable", wantCode: "model_provider_unavailable", wantReason: "provider_failure",
		},
		{
			name: "provider_protocol_auth", origin: turingv1.FailureOrigin_FAILURE_ORIGIN_PROVIDER_PROTOCOL,
			code: "model_auth_failed", wantCode: "model_auth_failed", wantReason: "provider_failure",
		},
		{
			name: "provider_protocol_quota", origin: turingv1.FailureOrigin_FAILURE_ORIGIN_PROVIDER_PROTOCOL,
			code: "model_quota_exceeded", wantCode: "model_quota_exceeded", wantReason: "provider_failure",
		},
		{
			name: "provider_protocol_status", origin: turingv1.FailureOrigin_FAILURE_ORIGIN_PROVIDER_PROTOCOL,
			code: "model_request_failed", wantCode: "model_request_failed", wantReason: "provider_failure",
		},
		{
			name: "provider_protocol_malformed_chunk", origin: turingv1.FailureOrigin_FAILURE_ORIGIN_PROVIDER_PROTOCOL,
			code: "model_bad_chunk", wantCode: "model_bad_chunk", wantReason: "provider_failure",
		},
		{
			name: "provider_transport", origin: turingv1.FailureOrigin_FAILURE_ORIGIN_PROVIDER_TRANSPORT,
			code: "model_stream_failed", wantCode: "model_stream_failed", wantReason: "provider_failure",
		},
		{
			name: "provider_output_guard", origin: turingv1.FailureOrigin_FAILURE_ORIGIN_PROVIDER_OUTPUT_GUARD,
			code: "model_output_limit_exceeded", wantCode: "model_output_limit_exceeded", wantReason: "provider_failure",
		},
		{
			name: "tool_infrastructure", origin: turingv1.FailureOrigin_FAILURE_ORIGIN_TOOL_INFRASTRUCTURE,
			code: "tool_discovery_failed", wantCode: "tool_discovery_failed", wantReason: "tool_failure",
		},
		{
			name: "tool_execution", origin: turingv1.FailureOrigin_FAILURE_ORIGIN_TOOL_EXECUTION,
			code: "tool_call_failed", wantCode: "tool_call_failed", wantReason: "tool_failure",
		},
		{
			name: "tool_guard", origin: turingv1.FailureOrigin_FAILURE_ORIGIN_TOOL_GUARD,
			code: "tool_call_limit_exceeded", wantCode: "tool_call_limit_exceeded", wantReason: "tool_failure",
		},
		{
			name: "tool_policy", origin: turingv1.FailureOrigin_FAILURE_ORIGIN_TOOL_POLICY,
			code: "tool_policy_decision_failed", wantCode: "tool_policy_decision_failed", wantReason: "policy_denied",
		},
		{
			name: "approval_transport", origin: turingv1.FailureOrigin_FAILURE_ORIGIN_APPROVAL_TRANSPORT,
			code: "approval_delivery_failed", wantCode: "approval_delivery_failed", wantReason: "approval_delivery_failed",
		},
		{
			name: "approval_expiry", origin: turingv1.FailureOrigin_FAILURE_ORIGIN_APPROVAL_EXPIRY,
			code: "approval_expired", wantCode: "approval_expired", wantReason: "expired",
		},
		{
			name: "automation_policy", origin: turingv1.FailureOrigin_FAILURE_ORIGIN_AUTOMATION_POLICY,
			code: "automation_tool_not_allowlisted", wantCode: "automation_tool_not_allowlisted", wantReason: "policy_denied",
		},
		{
			name: "worker_runtime", origin: turingv1.FailureOrigin_FAILURE_ORIGIN_WORKER_RUNTIME,
			code: "runtime_error", wantCode: "runtime_error", wantReason: "internal_failure",
		},
		{
			name: "dispatch", origin: turingv1.FailureOrigin_FAILURE_ORIGIN_DISPATCH,
			code: "retries_exhausted", wantCode: "retries_exhausted", wantReason: "retries_exhausted",
		},
		{
			name: "recovery", origin: turingv1.FailureOrigin_FAILURE_ORIGIN_RECOVERY,
			code: "side_effect_uncertain", wantCode: "side_effect_uncertain", wantReason: "side_effect_uncertain",
		},
		{
			// No code is allowlisted under the orchestrator's own origin today,
			// so its family fallback is what must hold: the origin survives and
			// the reported spelling does not.
			name: "orchestrator_internal", origin: turingv1.FailureOrigin_FAILURE_ORIGIN_ORCHESTRATOR_INTERNAL,
			code: "runtime_error", wantCode: "unknown", wantReason: "internal_failure",
		},
		{
			// An abandoned outcome belongs on a cancellation: it reports that
			// the client went away, not that the run failed.
			name: "client_lifecycle", origin: turingv1.FailureOrigin_FAILURE_ORIGIN_CLIENT_LIFECYCLE,
			code: "client_cancelled", wantLifecycle: "cancelled", wantCode: "", wantReason: "abandoned",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			h := newHarness(t)
			enqueued := h.createRunningRunResult(t, test.name)
			state, err := h.repo.GetRunState(context.Background(), enqueued.RunID)
			if err != nil {
				t.Fatalf("GetRunState: %v", err)
			}

			if _, err := h.service.handleRunFailed(context.Background(), &turingv1.RuntimeRunFailed{
				RunId:                enqueued.RunID,
				Code:                 test.code,
				Message:              "provider prose that must not survive",
				FailureOrigin:        test.origin,
				AutomaticRetryClass:  test.retry,
				ExpectedStateVersion: state.StateVersion,
			}); err != nil {
				t.Fatalf("handleRunFailed: %v", err)
			}

			after := h.runState(t, enqueued.RunID)
			wantLifecycle := test.wantLifecycle
			if wantLifecycle == "" {
				wantLifecycle = "failed"
			}
			if after.Lifecycle != wantLifecycle {
				t.Fatalf("lifecycle = %q, want %q", after.Lifecycle, wantLifecycle)
			}
			if after.OutcomeReason != test.wantReason {
				t.Fatalf("outcome reason = %q, want %q", after.OutcomeReason, test.wantReason)
			}
			code, message := h.runDiagnostics(t, enqueued.RunID)
			if code != test.wantCode {
				t.Fatalf("error_code = %q, want %q", code, test.wantCode)
			}
			if message.Valid {
				t.Fatalf("error_message persisted %q, want NULL", message.String)
			}
		})
	}
}

func TestRuntimeFailureMessageIsNeverPersisted(t *testing.T) {
	h := newHarness(t)
	enqueued := h.createRunningRunResult(t, "no message")
	state, err := h.repo.GetRunState(context.Background(), enqueued.RunID)
	if err != nil {
		t.Fatalf("GetRunState: %v", err)
	}

	if _, err := h.service.handleRunFailed(context.Background(), &turingv1.RuntimeRunFailed{
		RunId:                enqueued.RunID,
		Code:                 "model_stream_failed",
		Message:              "SECRET-PROVIDER-DIAGNOSTIC",
		FailureOrigin:        turingv1.FailureOrigin_FAILURE_ORIGIN_PROVIDER_TRANSPORT,
		ExpectedStateVersion: state.StateVersion,
	}); err != nil {
		t.Fatalf("handleRunFailed: %v", err)
	}

	_, message := h.runDiagnostics(t, enqueued.RunID)
	if message.Valid {
		t.Fatalf("error_message persisted %q, want NULL", message.String)
	}
	var jobMessage sql.NullString
	if err := h.database.QueryRowContext(context.Background(),
		`SELECT error_message FROM jobs WHERE run_id = ?`, enqueued.RunID,
	).Scan(&jobMessage); err != nil {
		t.Fatalf("read job diagnostics: %v", err)
	}
	if jobMessage.Valid {
		t.Fatalf("job error_message persisted %q, want NULL", jobMessage.String)
	}
	payload := h.terminalPayload(t, enqueued.RunID, "agent.run.failed")
	if _, exists := payload["message"]; exists {
		t.Fatalf("failure payload carries a message: %#v", payload)
	}
	if payload["code"] != "model_stream_failed" {
		t.Fatalf("failure payload code = %#v, want the normalized code", payload["code"])
	}
	if payload["retryable"] != false {
		t.Fatalf("failure payload retryable = %#v, want false", payload["retryable"])
	}
	var found int
	if err := h.database.QueryRowContext(context.Background(),
		`SELECT COUNT(*) FROM events WHERE run_id = ? AND payload_json LIKE '%SECRET-PROVIDER-DIAGNOSTIC%'`,
		enqueued.RunID,
	).Scan(&found); err != nil {
		t.Fatalf("scan events: %v", err)
	}
	if found != 0 {
		t.Fatalf("%d events carry the provider diagnostic", found)
	}
}

func TestUnknownOriginAndRetryClassFailClosed(t *testing.T) {
	tests := []struct {
		name       string
		origin     turingv1.FailureOrigin
		code       string
		retry      turingv1.AutomaticRetryClass
		wantCode   string
		wantReason string
	}{
		{
			name: "absent_origin", origin: turingv1.FailureOrigin_FAILURE_ORIGIN_UNSPECIFIED,
			code: "model_stream_failed", wantCode: "unknown", wantReason: "internal_failure",
		},
		{
			name: "unknown_origin", origin: turingv1.FailureOrigin_FAILURE_ORIGIN_UNKNOWN,
			code: "model_stream_failed", wantCode: "unknown", wantReason: "internal_failure",
		},
		{
			name: "unrecognized_numeric_origin", origin: turingv1.FailureOrigin(4242),
			code: "model_stream_failed", wantCode: "unknown", wantReason: "internal_failure",
		},
		{
			name: "unknown_code_keeps_provider_family", origin: turingv1.FailureOrigin_FAILURE_ORIGIN_PROVIDER_PROTOCOL,
			code: "model_invented_by_a_newer_worker", wantCode: "unknown", wantReason: "provider_failure",
		},
		{
			name: "unknown_code_keeps_tool_family", origin: turingv1.FailureOrigin_FAILURE_ORIGIN_TOOL_EXECUTION,
			code: "tool_invented_by_a_newer_worker", wantCode: "unknown", wantReason: "tool_failure",
		},
		{
			// An unrecognized pair may not buy itself a retry, whatever class
			// the reporter asked for.
			name: "unknown_pair_cannot_request_a_retry", origin: turingv1.FailureOrigin_FAILURE_ORIGIN_UNKNOWN,
			code: "worker_busy", retry: turingv1.AutomaticRetryClass_AUTOMATIC_RETRY_CLASS_SAME_RUN_TRANSIENT,
			wantCode: "unknown", wantReason: "internal_failure",
		},
		{
			name: "unrecognized_retry_numeric_fails_closed", origin: turingv1.FailureOrigin_FAILURE_ORIGIN_WORKER_RUNTIME,
			code: "runtime_error", retry: turingv1.AutomaticRetryClass(777),
			wantCode: "runtime_error", wantReason: "internal_failure",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			h := newHarness(t)
			enqueued := h.createRunningRunResult(t, test.name)
			state, err := h.repo.GetRunState(context.Background(), enqueued.RunID)
			if err != nil {
				t.Fatalf("GetRunState: %v", err)
			}

			if _, err := h.service.handleRunFailed(context.Background(), &turingv1.RuntimeRunFailed{
				RunId:                enqueued.RunID,
				Code:                 test.code,
				Message:              "unrecognized",
				FailureOrigin:        test.origin,
				AutomaticRetryClass:  test.retry,
				Retryable:            true,
				ExpectedStateVersion: state.StateVersion,
			}); err != nil {
				t.Fatalf("handleRunFailed: %v", err)
			}

			after := h.runState(t, enqueued.RunID)
			if !isTerminalRunStatus(after.Lifecycle) {
				t.Fatalf("lifecycle = %q, want a terminal outcome rather than a retry", after.Lifecycle)
			}
			if after.OutcomeReason != test.wantReason {
				t.Fatalf("outcome reason = %q, want %q", after.OutcomeReason, test.wantReason)
			}
			code, message := h.runDiagnostics(t, enqueued.RunID)
			if code != test.wantCode {
				t.Fatalf("error_code = %q, want %q", code, test.wantCode)
			}
			if message.Valid {
				t.Fatalf("error_message persisted %q, want NULL", message.String)
			}
		})
	}
}

func TestApprovalAndToolCallersSupplyTypedOriginBeforePersistence(t *testing.T) {
	t.Run("automation_policy_block_is_policy_denied", func(t *testing.T) {
		h := newHarness(t)
		enqueued := h.createRunningRunResult(t, "automation block")

		if _, err := h.service.failUnattendedRun(context.Background(), &turingv1.ToolCallBeacon{
			RunId: enqueued.RunID, ToolCallId: "call_automation", ToolName: "files.create",
		}, runoutcome.NormalizeFailure(
			runoutcome.OriginAutomationPolicy, AutomationNotAllowlistedCode, runoutcome.RetryClassNever,
		)); err != nil {
			t.Fatalf("failUnattendedRun: %v", err)
		}

		after := h.runState(t, enqueued.RunID)
		if after.OutcomeReason != "policy_denied" {
			t.Fatalf("outcome reason = %q, want policy_denied", after.OutcomeReason)
		}
		code, message := h.runDiagnostics(t, enqueued.RunID)
		if code != AutomationNotAllowlistedCode {
			t.Fatalf("error_code = %q, want %q", code, AutomationNotAllowlistedCode)
		}
		if message.Valid {
			t.Fatalf("automation block persisted an error message %q", message.String)
		}
	})

	t.Run("approval_transport_failure_is_approval_delivery_failed", func(t *testing.T) {
		h := newHarness(t)
		enqueued := h.createRunningRunResult(t, "approval transport")

		if err := h.service.terminalizePostCommitApprovalFailure(
			context.Background(), enqueued.RunID, "call_missing_approval",
		); err != nil {
			t.Fatalf("terminalizePostCommitApprovalFailure: %v", err)
		}

		after := h.runState(t, enqueued.RunID)
		if after.OutcomeReason != "approval_delivery_failed" {
			t.Fatalf("outcome reason = %q, want approval_delivery_failed", after.OutcomeReason)
		}
		code, message := h.runDiagnostics(t, enqueued.RunID)
		if code != "approval_delivery_failed" {
			t.Fatalf("error_code = %q, want approval_delivery_failed", code)
		}
		if message.Valid {
			t.Fatalf("approval transport failure persisted an error message %q", message.String)
		}
		payload := h.terminalPayload(t, enqueued.RunID, "agent.run.failed")
		if _, exists := payload["message"]; exists {
			t.Fatalf("approval failure payload carries a message: %#v", payload)
		}
	})
}

// ---------------------------------------------------------------------------
// Terminal fixtures.
//
// A production caller names the version its report was computed against. A test
// that is setting up an already-terminal run has no such report, so it reads
// the run's current version here rather than restating it at every fixture.
// ---------------------------------------------------------------------------

func completeRunFixture(t *testing.T, h *harness, runID string, assistantMessageID string, content string) []repository.Event {
	t.Helper()
	result, err := h.repo.CompleteRunCanonical(context.Background(), repository.CompleteRunInput{
		RunID:                runID,
		AssistantMessageID:   assistantMessageID,
		Content:              content,
		ExpectedStateVersion: h.runState(t, runID).StateVersion,
	})
	if err != nil {
		t.Fatalf("CompleteRunCanonical: %v", err)
	}
	return result.Events
}

func failRunFixture(t *testing.T, h *harness, runID string, failure runoutcome.Failure) []repository.Event {
	t.Helper()
	result, err := h.repo.FailRunCanonical(context.Background(), repository.FailRunInput{
		RunID:                runID,
		ExpectedStateVersion: h.runState(t, runID).StateVersion,
		Failure:              failure,
		PreserveExecution:    true,
	})
	if err != nil {
		t.Fatalf("FailRunCanonical: %v", err)
	}
	return result.Events
}

func cancelRunFixture(t *testing.T, h *harness, runID string) []repository.Event {
	t.Helper()
	result, err := h.repo.CancelRunCanonical(context.Background(), repository.CancelRunInput{
		RunID:                runID,
		ExpectedStateVersion: h.runState(t, runID).StateVersion,
		Cancellation:         runoutcome.AbandonedCancellation(),
	})
	if err != nil {
		t.Fatalf("CancelRunCanonical: %v", err)
	}
	return result.Events
}

// toolExecutionFailure is the normalized failure these fixtures terminalize
// with. It goes through the real constructor, so a fixture cannot express an
// outcome production code could not.
func toolExecutionFailure() runoutcome.Failure {
	return runoutcome.NormalizeFailure(runoutcome.OriginToolExecution, "tool_call_failed", runoutcome.RetryClassNever)
}

// A disconnect is not a terminal report. The run's ownership becomes uncertain,
// which is what recovering means, and it terminalizes only when the recovery
// path decides it — never as a completion nobody reported.
func TestDisconnectWithoutTerminalReportMovesThroughRecovery(t *testing.T) {
	h := newHarnessWithDispatch(t, DispatchConfig{LeaseDuration: time.Hour, MaxAttempts: 3})
	enqueued := h.enqueueRun(t, "disconnect me")
	ctx, cancel := context.WithCancel(h.internalContext())
	stream, err := h.runtimeClient(t).ConnectWorker(ctx)
	if err != nil {
		t.Fatalf("ConnectWorker: %v", err)
	}
	if err := stream.Send(&turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_WorkerReady{
		WorkerReady: &turingv1.RuntimeWorkerReady{
			WorkerId: "worker-disconnect", AgentId: turingv1.AgentId_AGENT_ID_GENERAL_ASSISTANT, MaxConcurrentRuns: 1,
		},
	}}); err != nil {
		t.Fatalf("send worker_ready: %v", err)
	}
	recvUntil(t, stream, func(cmd *turingv1.RuntimeCommand) bool {
		job := cmd.GetRunAssigned()
		return job != nil && job.GetRunId() == enqueued.RunID
	})

	cancel()
	_ = stream.CloseSend()
	h.service.WaitForWorkerStreams()

	after := h.runState(t, enqueued.RunID)
	if after.Lifecycle == "completed" {
		t.Fatal("a disconnect completed the run")
	}
	if after.Lifecycle != "recovering" && after.Lifecycle != "queued" {
		t.Fatalf("lifecycle = %q, want recovering or the requeue it leads to", after.Lifecycle)
	}
	if got := countRunEvents(t, h, enqueued.RunID, "agent.run.completed"); got != 0 {
		t.Fatalf("agent.run.completed events = %d, want none", got)
	}
}

// Content that was already durable when a run failed stays exactly as it was.
// Live deltas are event rows and do not update the canonical message, so this
// asserts what actually survives rather than promising that partial output does.
func TestDurablePartialContentSurvivesFailureButLiveDeltaIsNotPromised(t *testing.T) {
	h := newHarness(t)
	enqueued := h.createRunningRunResult(t, "partial content")
	if _, err := h.database.ExecContext(context.Background(),
		`UPDATE messages SET content = ? WHERE id = ?`, "durable partial answer", enqueued.AssistantMessageID,
	); err != nil {
		t.Fatalf("seed durable partial content: %v", err)
	}

	if err := h.service.applyUpdate(context.Background(), &turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_Event{
		Event: &turingv1.TuringEvent{
			RunId: enqueued.RunID,
			Type:  turingv1.TuringEventType_TURING_EVENT_TYPE_MESSAGE_DELTA,
			Payload: mustStruct(t, map[string]any{
				"messageId": enqueued.AssistantMessageID, "delta": " and a live delta nobody promised",
			}),
		},
	}}); err != nil {
		t.Fatalf("apply live delta: %v", err)
	}

	state := h.runState(t, enqueued.RunID)
	if _, err := h.service.handleRunFailed(context.Background(), &turingv1.RuntimeRunFailed{
		RunId:                enqueued.RunID,
		Code:                 "model_stream_failed",
		FailureOrigin:        turingv1.FailureOrigin_FAILURE_ORIGIN_PROVIDER_TRANSPORT,
		ExpectedStateVersion: state.StateVersion,
	}); err != nil {
		t.Fatalf("handleRunFailed: %v", err)
	}

	run, err := h.repo.GetRun(context.Background(), enqueued.RunID)
	if err != nil {
		t.Fatalf("GetRun: %v", err)
	}
	if run.AssistantContent != "durable partial answer" {
		t.Fatalf("assistant content = %q, want the durable bytes byte-for-byte", run.AssistantContent)
	}
	after := h.runState(t, enqueued.RunID)
	if after.Lifecycle != "failed" || after.OutcomeReason != "provider_failure" {
		t.Fatalf("run = %s/%s, want failed/provider_failure", after.Lifecycle, after.OutcomeReason)
	}
	if !after.HasDisplayableContent {
		t.Fatal("preserved content was not marked displayable")
	}
	if after.ContentSHA256 != runoutcome.ContentSHA256("durable partial answer") {
		t.Fatalf("content hash = %q, want the hash of the preserved bytes", after.ContentSHA256)
	}
}
