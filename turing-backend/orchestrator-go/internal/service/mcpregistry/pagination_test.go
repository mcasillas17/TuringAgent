package mcpregistry

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRegisteredDiscoveryFollowsToolsListPagination(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			ID     int64 `json:"id"`
			Params struct {
				Cursor string `json:"cursor"`
			} `json:"params"`
		}
		_ = json.NewDecoder(r.Body).Decode(&request)
		result := map[string]any{"tools": []any{map[string]any{"name": "first"}}}
		if request.Params.Cursor == "" {
			result["nextCursor"] = "page-2"
		} else {
			result["tools"] = []any{map[string]any{"name": "second"}}
		}
		w.Header().Set("content-type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"jsonrpc": "2.0", "id": request.ID, "result": result,
		})
	}))
	t.Cleanup(server.Close)

	tools, err := newMCPClient(server.URL, "", server.Client()).listTools(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(tools) != 2 || tools[0]["name"] != "first" || tools[1]["name"] != "second" {
		t.Fatalf("paginated tools = %+v", tools)
	}
}
