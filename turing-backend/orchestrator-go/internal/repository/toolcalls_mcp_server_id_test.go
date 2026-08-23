package repository

import (
	"context"
	"database/sql"
	"testing"

	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/db"
)

// mcpServerIDForToolCall reads the mcp_server_id column directly, since
// neither ToolCallRecord nor ToolCallBeforeResult exposes it back to a
// caller — nothing outside this package needs the value at insert time,
// only ApprovalRecord's own MCPServerID (joined from tool_calls, see
// approvalByID) does, once an approval exists for the call.
func mcpServerIDForToolCall(t *testing.T, database *db.DB, ctx context.Context, toolCallID string) sql.NullString {
	t.Helper()
	var got sql.NullString
	if err := database.QueryRowContext(ctx, `SELECT mcp_server_id FROM tool_calls WHERE id = ?`, toolCallID).Scan(&got); err != nil {
		t.Fatalf("query mcp_server_id for %s: %v", toolCallID, err)
	}
	return got
}

// TestRecordToolCallBeforePopulatesMCPServerIDFromCurrentRegistry proves
// finding #2's second requirement: every new tool_calls insert records
// the *current* mcp_server_id for its server_name, looked up at insert
// time — not merely the name — so a later approval created against this
// call (see ApprovalRecord.MCPServerID/approvalByID) can be bound to a
// specific server identity rather than only a name that could later be
// reused by a different server (see
// TestConsumeApprovalForThirdPartyRefusesAfterServerIsDeletedAndNameReregistered
// in the approvals package for the security property this makes
// possible).
func TestRecordToolCallBeforePopulatesMCPServerIDFromCurrentRegistry(t *testing.T) {
	database := openTestDB(t)
	repo := New(database)
	ctx := context.Background()

	vendor, err := repo.RegisterMCPServer(ctx, ImportedMCPServer{
		Name: "vendor", URL: "http://vendor:9000/mcp", Tier: MCPServerTierLocalContainer,
	})
	if err != nil {
		t.Fatal(err)
	}

	session, err := repo.CreateSession(ctx, "Tool call server binding")
	if err != nil {
		t.Fatal(err)
	}
	enqueued, err := repo.EnqueueUserMessage(ctx, EnqueueUserMessageInput{
		SessionID: session.SessionID, Content: "hi", AgentID: "general_assistant", ModelProvider: "ollama", Model: "llama3.2",
	})
	if err != nil {
		t.Fatal(err)
	}

	if err := repo.RecordToolCallBefore(ctx, ToolCallRecord{ToolCallID: "call_vendor", RunID: enqueued.RunID}, "general_assistant", "vendor", "vendor.write", `{"path":"a.txt"}`, "sha256:vendor"); err != nil {
		t.Fatal(err)
	}
	if err := repo.RecordToolCallBefore(ctx, ToolCallRecord{ToolCallID: "call_skills", RunID: enqueued.RunID}, "general_assistant", "skills", "skills.search", `{"q":"x"}`, "sha256:skills"); err != nil {
		t.Fatal(err)
	}
	if err := repo.RecordToolCallBefore(ctx, ToolCallRecord{ToolCallID: "call_integrations", RunID: enqueued.RunID}, "general_assistant", "integrations", "github.list_issues", `{}`, "sha256:integrations"); err != nil {
		t.Fatal(err)
	}

	if got := mcpServerIDForToolCall(t, database, ctx, "call_vendor"); !got.Valid || got.String != vendor.Server.ID {
		t.Fatalf("call_vendor mcp_server_id = %+v, want valid %q", got, vendor.Server.ID)
	}
	if got := mcpServerIDForToolCall(t, database, ctx, "call_skills"); got.Valid {
		t.Fatalf("call_skills mcp_server_id = %+v, want NULL: skills is a pseudo-server with no mcp_servers row", got)
	}
	if got := mcpServerIDForToolCall(t, database, ctx, "call_integrations"); got.Valid {
		t.Fatalf("call_integrations mcp_server_id = %+v, want NULL: integrations is a pseudo-server with no mcp_servers row", got)
	}
}

// TestRecordToolCallBeforeLeavesMCPServerIDNullForUnregisteredServerName
// covers a server_name that resolves to no current mcp_servers row at
// all (neither a registered third-party server nor a known
// pseudo-server) — the same fail-closed NULL a genuinely deleted
// server's future tool calls would get, proven directly rather than only
// implied by the pseudo-server cases above.
func TestRecordToolCallBeforeLeavesMCPServerIDNullForUnregisteredServerName(t *testing.T) {
	database := openTestDB(t)
	repo := New(database)
	ctx := context.Background()

	session, err := repo.CreateSession(ctx, "Unregistered server")
	if err != nil {
		t.Fatal(err)
	}
	enqueued, err := repo.EnqueueUserMessage(ctx, EnqueueUserMessageInput{
		SessionID: session.SessionID, Content: "hi", AgentID: "general_assistant", ModelProvider: "ollama", Model: "llama3.2",
	})
	if err != nil {
		t.Fatal(err)
	}

	if err := repo.RecordToolCallBefore(ctx, ToolCallRecord{ToolCallID: "call_unknown", RunID: enqueued.RunID}, "general_assistant", "never-registered", "never-registered.tool", `{}`, "sha256:unknown"); err != nil {
		t.Fatal(err)
	}
	if got := mcpServerIDForToolCall(t, database, ctx, "call_unknown"); got.Valid {
		t.Fatalf("call_unknown mcp_server_id = %+v, want NULL", got)
	}
}
