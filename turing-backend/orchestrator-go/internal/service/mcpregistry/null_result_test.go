package mcpregistry

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestNullMCPResultIsRejected(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			ID int64 `json:"id"`
		}
		_ = json.NewDecoder(r.Body).Decode(&request)
		w.Header().Set("content-type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"jsonrpc": "2.0", "id": request.ID, "result": nil,
		})
	}))
	t.Cleanup(server.Close)
	if _, err := newMCPClient(server.URL, "", server.Client()).callTool(
		context.Background(), "vendor.null", map[string]any{},
	); err == nil {
		t.Fatal("null MCP result was accepted")
	}
}
