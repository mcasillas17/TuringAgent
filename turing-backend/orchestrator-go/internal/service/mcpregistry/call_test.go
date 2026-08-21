package mcpregistry

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/db"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/secretbox"
	approvalsvc "github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/service/approvals"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/service/events"
)

func TestMutatingCallToANonCooperatingServerIsRefusedWithoutApproval(t *testing.T) {
	h := newRegistryCallHarness(t)
	args := map[string]any{"path": "x"}
	runID := h.runningToolCall(t, "call_unapproved", args)

	_, err := h.registry.CallTool(context.Background(), CallInput{
		ServerID: h.serverID,
		RunID:    runID,
		ToolName: "vendor.write",
		Args:     args,
	})

	if err == nil {
		t.Fatal("an approval_required tool must not dispatch without an approval")
	}
	if got := h.reached.Load(); got != 0 {
		t.Fatalf("vendor requests = %d, want zero", got)
	}
}

func TestTheOrchestratorConsumesTheApprovalItselfExactlyOnce(t *testing.T) {
	h := newRegistryCallHarness(t)
	args := map[string]any{"path": "x"}
	runID := h.runningToolCall(t, "call_approved", args)
	approvalID, err := h.approvals.CreateApprovalForTool(
		context.Background(),
		runID,
		"call_approved",
		"general_assistant",
		"vendor.write",
		args,
	)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := h.approvals.ApproveApproval(context.Background(), &turingv1.ApproveApprovalRequest{
		ApprovalId: approvalID,
	}); err != nil {
		t.Fatal(err)
	}

	input := CallInput{
		ServerID:   h.serverID,
		RunID:      runID,
		ApprovalID: approvalID,
		ToolName:   "vendor.write",
		Args:       args,
	}
	if _, err := h.registry.CallTool(context.Background(), input); err != nil {
		t.Fatalf("first CallTool: %v", err)
	}
	if _, err := h.registry.CallTool(context.Background(), input); err == nil {
		t.Fatal("the same approval must not authorise a second call")
	}
	if got := h.reached.Load(); got != 1 {
		t.Fatalf("vendor requests = %d, want exactly one", got)
	}
	approval, err := h.repo.GetApproval(context.Background(), approvalID)
	if err != nil {
		t.Fatal(err)
	}
	if approval.Status != "consumed" {
		t.Fatalf("approval status = %q, want consumed", approval.Status)
	}
}

func TestRemoteCallCannotDispatchWithoutTheRunEgressDecision(t *testing.T) {
	h := newRegistryCallHarness(t)
	if _, err := h.database.ExecContext(context.Background(), `
		UPDATE mcp_servers SET tier = 'remote_url' WHERE id = ?
	`, h.serverID); err != nil {
		t.Fatal(err)
	}

	args := map[string]any{"path": "x"}
	runID := h.runningToolCall(t, "call_remote_without_egress", args)
	approvalID, err := h.approvals.CreateApprovalForTool(
		context.Background(),
		runID,
		"call_remote_without_egress",
		"general_assistant",
		"vendor.write",
		args,
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.approvals.ApproveApproval(context.Background(), &turingv1.ApproveApprovalRequest{
		ApprovalId: approvalID,
	}); err != nil {
		t.Fatal(err)
	}

	_, err = h.registry.CallTool(context.Background(), CallInput{
		ServerID: h.serverID, RunID: runID, ApprovalID: approvalID,
		ToolName: "vendor.write", Args: args,
	})
	if err == nil {
		t.Fatal("remote MCP call dispatched without a run-owned egress decision")
	}
	if got := h.reached.Load(); got != 0 {
		t.Fatalf("vendor requests = %d, want zero", got)
	}
	approval, err := h.repo.GetApproval(context.Background(), approvalID)
	if err != nil {
		t.Fatal(err)
	}
	if approval.Status != "approved" {
		t.Fatalf("approval status = %q, want approved because egress was refused before consume", approval.Status)
	}
}

func TestServerDisappearingBetweenDiscoveryAndDispatchIsRecordedDown(t *testing.T) {
	h := newRegistryCallHarness(t)
	runID := h.runningToolCall(t, "call_disappeared", map[string]any{"path": "x"})
	if err := h.repo.SetMCPToolPolicy(context.Background(), h.serverID, "vendor.write", "safe"); err != nil {
		t.Fatal(err)
	}
	h.vendor.Close()

	if _, err := h.registry.CallTool(context.Background(), CallInput{
		ServerID: h.serverID,
		RunID:    runID,
		ToolName: "vendor.write",
		Args:     map[string]any{"path": "x"},
	}); err == nil {
		t.Fatal("call to disappeared server succeeded")
	}
	server, err := h.repo.GetMCPServer(context.Background(), h.serverID)
	if err != nil {
		t.Fatal(err)
	}
	if server.Status != "down" {
		t.Fatalf("server status = %q, want down", server.Status)
	}
}

type registryCallHarness struct {
	registry      *Server
	approvals     *approvalsvc.Server
	repo          *repository.Repository
	database      *db.DB
	vendor        *httptest.Server
	serverID      string
	reached       atomic.Int32
	authorization atomic.Value
	deleteOnCall  atomic.Bool
}

func newRegistryCallHarness(t *testing.T) *registryCallHarness {
	t.Helper()
	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if err := db.ApplyMigrations(context.Background(), database); err != nil {
		t.Fatal(err)
	}
	repo := repository.New(database)
	sealer, err := secretbox.New(bytes.Repeat([]byte{0x42}, secretbox.KeySize))
	if err != nil {
		t.Fatal(err)
	}
	h := &registryCallHarness{repo: repo, database: database}
	vendor := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.reached.Add(1)
		h.authorization.Store(r.Header.Get("authorization"))
		var request struct {
			ID int64 `json:"id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Errorf("decode vendor request: %v", err)
		}
		if h.deleteOnCall.Load() {
			if err := h.repo.DeleteMCPServer(context.Background(), h.serverID); err != nil {
				t.Errorf("delete server during call: %v", err)
			}
		}
		w.Header().Set("content-type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"jsonrpc": "2.0",
			"id":      request.ID,
			"result":  map[string]any{"content": []any{}},
		})
	}))
	t.Cleanup(vendor.Close)
	h.vendor = vendor
	sealed, err := sealer.Seal([]byte("vendor-token"), []byte("vendor"))
	if err != nil {
		t.Fatal(err)
	}
	server, err := repo.UpsertImportedMCPServer(context.Background(), repository.ImportedMCPServer{
		Name:        "vendor",
		URL:         vendor.URL,
		SealedToken: sealed,
		Tier:        repository.MCPServerTierLocalContainer,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.SetMCPServerEnabled(context.Background(), server.ID, true); err != nil {
		t.Fatal(err)
	}
	h.serverID = server.ID
	bus := events.NewBus(8)
	h.approvals = approvalsvc.New(repo, bus, "approval-secret")
	h.registry = New(repo, sealer, vendor.Client())
	h.registry.SetApprovalEnforcer(h.approvals)
	if err := h.registry.RecordDiscovery(context.Background(), server.ID, []DiscoveredTool{{
		Name:       "vendor.write",
		SchemaJSON: `{"type":"object"}`,
	}}); err != nil {
		t.Fatal(err)
	}
	return h
}

func (h *registryCallHarness) runningToolCall(t *testing.T, toolCallID string, args map[string]any) string {
	t.Helper()
	session, err := h.repo.CreateSession(context.Background(), "registry approval")
	if err != nil {
		t.Fatal(err)
	}
	enqueued, err := h.repo.EnqueueUserMessage(context.Background(), repository.EnqueueUserMessageInput{
		SessionID: session.SessionID, Content: "write", AgentID: "general_assistant",
		ModelProvider: "ollama", Model: "test-model",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := h.repo.MarkRunRunning(context.Background(), enqueued.RunID); err != nil {
		t.Fatal(err)
	}
	if _, err := h.database.ExecContext(
		context.Background(),
		`UPDATE agent_runs SET execution_active = 1 WHERE id = ?`,
		enqueued.RunID,
	); err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(args)
	if err != nil {
		t.Fatal(err)
	}
	if err := h.repo.RecordToolCallBefore(
		context.Background(),
		repository.ToolCallRecord{ToolCallID: toolCallID, RunID: enqueued.RunID},
		"general_assistant",
		"vendor",
		"vendor.write",
		string(encoded),
		"sha256:test-placeholder",
	); err != nil {
		t.Fatal(err)
	}
	return enqueued.RunID
}
