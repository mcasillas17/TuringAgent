package runtime

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/auth"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/db"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
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

func newHarness(t *testing.T) *harness {
	t.Helper()
	database := openRuntimeTestDB(t)
	repo := repository.New(database)
	bus := events.NewBus(8)
	approvals := approvalsvc.New(repo, bus, "approval-secret")
	service := New(repo, bus, approvals)
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
		_ = conn.Close()
	})
	return &harness{repo: repo, database: database, bus: bus, service: service, approvals: approvals, conn: conn}
}

func openRuntimeTestDB(t *testing.T) *db.DB {
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

func TestConnectWorkerRequeuesJobWhenAssignmentSendFails(t *testing.T) {
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
	if run.Status != "queued" {
		t.Fatalf("run status = %q, want queued after send failure", run.Status)
	}
	var jobStatus string
	var leaseOwner sql.NullString
	if err := h.database.QueryRowContext(context.Background(), `SELECT status, lease_owner FROM jobs WHERE id = ?`, enqueued.JobID).Scan(&jobStatus, &leaseOwner); err != nil {
		t.Fatal(err)
	}
	if jobStatus != "pending" || leaseOwner.Valid {
		t.Fatalf("job after send failure: status=%q lease_owner=%q", jobStatus, leaseOwner.String)
	}
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
	if _, err := h.repo.CancelRunWithEvent(context.Background(), enqueued.RunID, "client_cancelled", `{"reason":"client_cancelled"}`); err != nil {
		t.Fatal(err)
	}
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
		var payload map[string]string
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

func TestRunCompletedRejectsEmptyContent(t *testing.T) {
	h := newHarness(t)
	enqueued := h.createRunningRunResult(t, "empty completion")
	err := h.service.applyUpdate(context.Background(), &turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_RunCompleted{RunCompleted: &turingv1.RuntimeRunCompleted{
		RunId:              enqueued.RunID,
		AssistantMessageId: enqueued.AssistantMessageID,
	}}})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("empty completion error = %v, want InvalidArgument", err)
	}
	run, err := h.repo.GetRun(context.Background(), enqueued.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if run.Status != "running" {
		t.Fatalf("run status = %q, want running", run.Status)
	}
}

func TestRunCompletedMapsStateConflictToFailedPrecondition(t *testing.T) {
	h := newHarness(t)
	enqueued := h.createRunningRunResult(t, "already cancelled")
	if _, err := h.repo.CancelRunWithEvent(context.Background(), enqueued.RunID, "user_cancelled", `{"reason":"user_cancelled"}`); err != nil {
		t.Fatal(err)
	}

	err := h.service.applyUpdate(context.Background(), &turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_RunCompleted{RunCompleted: &turingv1.RuntimeRunCompleted{
		RunId:              enqueued.RunID,
		AssistantMessageId: enqueued.AssistantMessageID,
		Content:            "too late",
	}}})
	if status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("RunCompleted error = %v, want FailedPrecondition", err)
	}
}

func TestRunFailedMapsStateConflictToFailedPrecondition(t *testing.T) {
	h := newHarness(t)
	enqueued := h.createRunningRunResult(t, "already cancelled")
	if _, err := h.repo.CancelRunWithEvent(context.Background(), enqueued.RunID, "user_cancelled", `{"reason":"user_cancelled"}`); err != nil {
		t.Fatal(err)
	}

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
	if _, err := h.repo.CancelRunWithEvent(context.Background(), enqueued.RunID, "client_cancelled", `{"reason":"client_cancelled"}`); err != nil {
		t.Fatal(err)
	}
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
		RunId:      enqueued.RunID,
		TraceId:    enqueued.TraceID,
		ToolCallId: "call_files_update",
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
		return decision != nil && decision.ToolCallId == "call_files_update"
	}).GetToolPolicyDecision()
	if decision.Decision != turingv1.ToolPolicyDecision_DECISION_APPROVAL_REQUIRED || decision.ApprovalId == "" {
		t.Fatalf("tool policy decision = %+v", decision)
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
	var sawRequested bool
	for _, event := range replayed {
		if event.Type == "approval.requested" && event.RunID.Valid && event.RunID.String == enqueued.RunID {
			sawRequested = true
		}
		if event.Type == "tool.call.started" && event.RunID.Valid && event.RunID.String == enqueued.RunID {
			t.Fatal("approval-required tool emitted tool.call.started before approval")
		}
	}
	if !sawRequested {
		t.Fatal("approval.requested event was not persisted")
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

func TestDenyingApprovalReleasesWorkerAndFailsJob(t *testing.T) {
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

func TestExpiredApprovalReleasesWorkerAndFailsJob(t *testing.T) {
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
	if payload["toolCallId"] != "call_unknown" || payload["reason"] != "unknown_tool" {
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
	if payload["toolCallId"] != "call_echo" || payload["resultSummary"] != "echoed hello" || payload["durationMs"] != float64(12) {
		t.Fatalf("tool.call.completed payload = %+v", payload)
	}
}

func TestNotifyApprovalUpdatedSendsTokenToAssignedWorker(t *testing.T) {
	h := newHarness(t)
	commands := make(chan *turingv1.RuntimeCommand, 1)
	h.service.mu.Lock()
	h.service.workers["worker-approval-update"] = &worker{commands: commands, maxConcurrent: 1, assignments: map[string]string{"run_approval": "job_approval"}}
	h.service.mu.Unlock()

	if err := h.service.NotifyApprovalUpdated(context.Background(), "run_approval", "appr_1", "approved", "header.payload.signature"); err != nil {
		t.Fatal(err)
	}

	select {
	case cmd := <-commands:
		update := cmd.GetApprovalUpdated()
		if update.GetApprovalId() != "appr_1" || update.GetStatus() != "approved" || update.GetApprovalToken() != "header.payload.signature" {
			t.Fatalf("approval_updated = %+v", update)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for approval_updated command")
	}
}

func TestCancelRunWaitsForCommandBufferSpace(t *testing.T) {
	h := newHarness(t)
	commands := make(chan *turingv1.RuntimeCommand, 1)
	commands <- &turingv1.RuntimeCommand{Command: &turingv1.RuntimeCommand_WorkerAccepted{WorkerAccepted: &turingv1.RuntimeWorkerAccepted{WorkerId: "worker-buffered"}}}
	h.service.mu.Lock()
	h.service.workers["worker-buffered"] = &worker{commands: commands, maxConcurrent: 1, assignments: map[string]string{"run_buffered": "job_buffered"}}
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
		cancel := cmd.GetRunCancelled()
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

func TestWorkerDisconnectRequeuesAssignedJob(t *testing.T) {
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
		if err == nil && run.Status == "queued" {
			var jobStatus string
			if err := h.database.QueryRowContext(context.Background(), `SELECT status FROM jobs WHERE id = ?`, enqueued.JobID).Scan(&jobStatus); err != nil {
				t.Fatal(err)
			}
			if jobStatus != "pending" {
				t.Fatalf("job status = %q, want pending", jobStatus)
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("assigned job was not requeued after worker disconnect")
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
	usage, ok := payload["usage"].(map[string]any)
	if payload["runId"] != enqueued.RunID || payload["assistantMessageId"] != enqueued.AssistantMessageID || !ok || usage["prompt_tokens"] != float64(3) || usage["completion_tokens"] != float64(4) {
		t.Fatalf("payload = %+v", payload)
	}
}

func TestRunFailedPublishesTerminalEvent(t *testing.T) {
	h := newHarness(t)
	enqueued := h.createRunningRunResult(t, "fail me")
	ch, unsubscribe := h.bus.Subscribe(enqueued.SessionID)
	defer unsubscribe()

	err := h.service.applyUpdate(context.Background(), &turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_RunFailed{RunFailed: &turingv1.RuntimeRunFailed{
		RunId:     enqueued.RunID,
		Code:      "model_error",
		Message:   "model failed",
		Retryable: true,
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
	if payload.RunID != enqueued.RunID || payload.Code != "model_error" || payload.Message != "model failed" || !payload.Retryable {
		t.Fatalf("payload = %+v", payload)
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
