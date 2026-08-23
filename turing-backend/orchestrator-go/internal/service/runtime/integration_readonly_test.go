package runtime

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
	"google.golang.org/protobuf/types/known/structpb"
)

func createRuntimeGitHubConnection(t *testing.T, h *harness) repository.Connection {
	t.Helper()
	connection, err := h.repo.CreateConnection(context.Background(), repository.NewConnection{
		ConnectionID: "conn_runtime_github", Provider: "github", DisplayName: "Work GitHub",
		CredentialCiphertext: []byte{1}, CredentialHint: "••••token", GrantedScopes: []string{"Write issue comments."},
	})
	if err != nil {
		t.Fatal(err)
	}
	return connection
}

func TestRedeliveredIntegrationReadKeepsReadOnlyDecision(t *testing.T) {
	h := newHarness(t)
	if err := h.repo.UpsertTools(context.Background(), []repository.DiscoveredTool{{
		ServerName: "integrations", ToolName: "github.list_issues", SchemaJSON: `{}`, Policy: "approval_required",
	}}); err != nil {
		t.Fatal(err)
	}
	enqueued := h.createRunningRunResult(t, "list issues")
	args, err := structpb.NewStruct(map[string]any{"connection_id": "conn_read", "owner": "owner", "repo": "repo"})
	if err != nil {
		t.Fatal(err)
	}
	beacon := &turingv1.ToolCallBeacon{
		RunId: enqueued.RunID, TraceId: enqueued.TraceID, ToolCallId: "call_integration_read",
		AgentId: turingv1.AgentId_AGENT_ID_GENERAL_ASSISTANT, ToolName: "github.list_issues",
		Phase: turingv1.ToolCallPhase_TOOL_CALL_PHASE_BEFORE, Args: args,
	}
	first, err := h.service.handleToolBeacon(context.Background(), beacon)
	if err != nil {
		t.Fatal(err)
	}
	replayed, err := h.service.handleToolBeacon(context.Background(), beacon)
	if err != nil {
		t.Fatal(err)
	}
	for name, decision := range map[string]*turingv1.ToolPolicyDecision{"fresh": first, "redelivered": replayed} {
		if decision.GetDecision() != turingv1.ToolPolicyDecision_DECISION_APPROVAL_REQUIRED || !decision.GetReadOnly() {
			t.Fatalf("%s decision=%+v, want approval-required read", name, decision)
		}
	}
	if replayed.GetApprovalId() != first.GetApprovalId() {
		t.Fatalf("redelivered approval=%q, want %q", replayed.GetApprovalId(), first.GetApprovalId())
	}
}

func TestIntegrationWriteApprovalUsesFullRenderOrRefusesBeforeCreatingApproval(t *testing.T) {
	h := newHarness(t)
	connection := createRuntimeGitHubConnection(t, h)
	if err := h.repo.UpsertTools(context.Background(), []repository.DiscoveredTool{{
		ServerName: "integrations", ToolName: "github.create_comment", SchemaJSON: `{}`, Policy: "approval_required",
	}}); err != nil {
		t.Fatal(err)
	}
	baseArgs := map[string]any{
		"connection_id": connection.ConnectionID, "owner": "octo", "repo": "project",
		"issue_number": float64(7), "body": "",
	}
	baseRender, err := h.repo.IntegrationApprovalRender(context.Background(), "github.create_comment", baseArgs)
	if err != nil {
		t.Fatal(err)
	}
	bodyBytes := repository.MaxIntegrationApprovalRenderBytes - len([]byte(baseRender))
	for _, test := range []struct {
		name    string
		body    string
		allowed bool
	}{
		{name: "exact", body: strings.Repeat("x", bodyBytes), allowed: true},
		{name: "one over", body: strings.Repeat("x", bodyBytes+1)},
	} {
		t.Run(test.name, func(t *testing.T) {
			enqueued := h.createRunningRunResult(t, test.name)
			argsMap := map[string]any{
				"connection_id": connection.ConnectionID, "owner": "octo", "repo": "project",
				"issue_number": float64(7), "body": test.body,
			}
			args, err := structpb.NewStruct(argsMap)
			if err != nil {
				t.Fatal(err)
			}
			decision, err := h.service.handleToolBeacon(context.Background(), &turingv1.ToolCallBeacon{
				RunId: enqueued.RunID, TraceId: enqueued.TraceID, ToolCallId: "call_comment_" + strings.ReplaceAll(test.name, " ", "_"),
				AgentId: turingv1.AgentId_AGENT_ID_GENERAL_ASSISTANT, ToolName: "github.create_comment",
				Phase: turingv1.ToolCallPhase_TOOL_CALL_PHASE_BEFORE, Args: args,
			})
			if err != nil {
				t.Fatal(err)
			}
			if !test.allowed {
				if decision.GetDecision() != turingv1.ToolPolicyDecision_DECISION_DENY || decision.GetReason() != "integration_approval_render_too_large" {
					t.Fatalf("decision=%+v, want render-too-large denial", decision)
				}
				var count int
				if err := h.database.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM approvals WHERE run_id = ?`, enqueued.RunID).Scan(&count); err != nil || count != 0 {
					t.Fatalf("approval rows=%d err=%v, want none", count, err)
				}
				return
			}
			if decision.GetDecision() != turingv1.ToolPolicyDecision_DECISION_APPROVAL_REQUIRED {
				t.Fatalf("decision=%+v, want approval", decision)
			}
			wantRender, err := h.repo.IntegrationApprovalRender(context.Background(), "github.create_comment", argsMap)
			if err != nil || len([]byte(wantRender)) != repository.MaxIntegrationApprovalRenderBytes {
				t.Fatalf("render bytes=%d err=%v", len([]byte(wantRender)), err)
			}
			secondDecision, err := h.service.handleToolBeacon(context.Background(), &turingv1.ToolCallBeacon{
				RunId: enqueued.RunID, TraceId: enqueued.TraceID, ToolCallId: "call_comment_second",
				AgentId: turingv1.AgentId_AGENT_ID_GENERAL_ASSISTANT, ToolName: "github.create_comment",
				Phase: turingv1.ToolCallPhase_TOOL_CALL_PHASE_BEFORE, Args: args,
			})
			if err != nil {
				t.Fatal(err)
			}
			if secondDecision.GetDecision() != turingv1.ToolPolicyDecision_DECISION_APPROVAL_REQUIRED {
				t.Fatalf("second decision=%+v, want approval", secondDecision)
			}
			events, _, err := h.repo.ReplayEvents(context.Background(), enqueued.SessionID, 0, 20)
			if err != nil {
				t.Fatal(err)
			}
			requests := 0
			for _, event := range events {
				if event.Type != "approval.requested" {
					continue
				}
				requests++
				var payload map[string]any
				if err := json.Unmarshal([]byte(event.PayloadJSON), &payload); err != nil {
					t.Fatal(err)
				}
				gotRender, _ := payload["fullArguments"].(string)
				if gotRender != wantRender || !strings.Contains(gotRender, test.body) {
					t.Fatalf("event render differs from checked full render")
				}
			}
			if requests != 2 {
				t.Fatalf("approval request events=%d, want 2", requests)
			}
		})
	}
}
