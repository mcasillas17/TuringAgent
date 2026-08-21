package mcpregistry

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestMCPErrorCannotReflectRegisteredBearer(t *testing.T) {
	const token = "vendor-secret-error-reflection"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			ID int64 `json:"id"`
		}
		_ = json.NewDecoder(r.Body).Decode(&request)
		w.Header().Set("content-type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"jsonrpc": "2.0",
			"id":      request.ID,
			"error": map[string]any{
				"code": -32000, "message": "reflected " + token,
			},
		})
	}))
	t.Cleanup(server.Close)

	_, err := newMCPClient(server.URL, token, server.Client()).callTool(
		context.Background(), "vendor.fail", map[string]any{},
	)
	if err == nil || strings.Contains(err.Error(), token) {
		t.Fatalf("MCP error leaked registered bearer: %v", err)
	}
}
