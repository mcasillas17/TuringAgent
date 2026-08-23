package mcpregistry

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestListToolsReturnsRawUnredactedToolMetadata pins the exact contract
// discover's own token-metadata scan (service.go) depends on: unlike
// request (used by callTool), listTools must hand back each tool exactly
// as the peer sent it, with none of request's own marker-substitution
// redaction applied. If a future change routed listTools through
// request instead of requestRaw again, a bearer echo would already read
// "vendor.[redacted]" by the time discover() ever saw it — passing
// discover's own raw-metadata scan trivially (the literal token would
// already be gone) and silently reintroducing the "redact and still
// persist" behavior this fix replaces, without any single assertion at
// the discover()/SetMcpServerEnabled level necessarily pinning *why* it
// started failing. This test catches that regression directly, at the
// client layer, independent of discover's own behavior.
func TestListToolsReturnsRawUnredactedToolMetadata(t *testing.T) {
	const token = "mcp-listtools-raw-sentinel-1a7c9e3f6b2d5084"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			ID int64 `json:"id"`
		}
		_ = json.NewDecoder(r.Body).Decode(&request)
		w.Header().Set("content-type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"jsonrpc": "2.0", "id": request.ID,
			"result": map[string]any{"tools": []any{map[string]any{
				"name": "vendor." + token, "inputSchema": map[string]any{"type": "object"},
			}}},
		})
	}))
	t.Cleanup(server.Close)

	tools, err := newMCPClient(server.URL, token, server.Client()).listTools(context.Background())
	if err != nil {
		t.Fatalf("listTools: %v", err)
	}
	if len(tools) != 1 {
		t.Fatalf("tools = %+v, want exactly one", tools)
	}
	name, _ := tools[0]["name"].(string)
	if !strings.Contains(name, token) {
		t.Fatalf("tool name = %q, want the raw, unredacted token still present: listTools must not pre-redact — that is discover's own job, over the raw value, before persistence", name)
	}
}

func TestThirdPartyServerNeverReceivesTheApprovalConsumerIdentity(t *testing.T) {
	const (
		vendorToken           = "vendor-token"
		approvalConsumerToken = "approval-consumer-must-stay-in-orchestrator"
	)
	h := newRegistryCallHarness(t)
	runID := h.runningToolCall(t, "call_identity", map[string]any{"path": "x"})
	if err := h.repo.SetMCPToolPolicy(context.Background(), h.serverID, "vendor.write", "safe"); err != nil {
		t.Fatal(err)
	}
	if _, err := h.registry.CallTool(context.Background(), CallInput{
		ServerID: h.serverID,
		RunID:    runID,
		ToolName: "vendor.write",
		Args:     map[string]any{"path": "x"},
	}); err != nil {
		t.Fatalf("CallTool: %v", err)
	}

	authorization, _ := h.authorization.Load().(string)
	if authorization == "Bearer "+approvalConsumerToken {
		t.Fatal("the approval-consumer identity must never leave the orchestrator")
	}
	if authorization != "Bearer "+vendorToken {
		t.Fatalf("authorization = %q, want only the registered server bearer", authorization)
	}
}
