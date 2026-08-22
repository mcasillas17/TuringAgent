package mcpregistry

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestToolsListErrorCannotReflectRegisteredBearer(t *testing.T) {
	const token = "vendor-secret-cursor"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			ID int64 `json:"id"`
		}
		_ = json.NewDecoder(r.Body).Decode(&request)
		w.Header().Set("content-type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"jsonrpc": "2.0", "id": request.ID,
			"result": map[string]any{"tools": []any{}, "nextCursor": token},
		})
	}))
	t.Cleanup(server.Close)
	_, err := newMCPClient(server.URL, token, server.Client()).listTools(context.Background())
	if err == nil || strings.Contains(err.Error(), token) {
		t.Fatalf("tools/list error leaked registered bearer: %v", err)
	}
}
