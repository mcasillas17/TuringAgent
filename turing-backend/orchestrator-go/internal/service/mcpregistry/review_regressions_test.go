package mcpregistry

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
)

func TestImportCanonicalizesRemoteDefaultPortAndTrailingSlash(t *testing.T) {
	service, repo := newRegistryTestService(t)
	if _, err := service.ImportJSON(context.Background(), []byte(`{
		"mcpServers": {
			"vendor": {"url": "https://vendor.example:443/mcp/"}
		}

	}`)); err != nil {
		t.Fatal(err)
	}
	servers, err := repo.ListMCPServers(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	vendor := findRepositoryServer(t, servers, "vendor")
	if vendor.URL != "https://vendor.example/mcp" {
		t.Fatalf("stored URL = %q, want canonical endpoint", vendor.URL)
	}
}

func TestImportPreservesBracketsForPublicIPv6Endpoint(t *testing.T) {
	_, canonical, err := classifyImportedURL("https://[2606:4700:4700::1111]/mcp")
	if err != nil {
		t.Fatal(err)
	}
	if canonical != "https://[2606:4700:4700::1111]/mcp" {
		t.Fatalf("canonical IPv6 URL = %q", canonical)
	}
}

func TestThirdPartyDiscoveryRejectsReservedBundledNamespaces(t *testing.T) {
	service, repo := newRegistryTestService(t)
	for _, test := range []struct {
		serverName string
		toolName   string
	}{
		{serverName: "vendor-files", toolName: "files.delete"},
		{serverName: "vendor-system", toolName: "system.future"},
		{serverName: "vendor-skills", toolName: "skill_view"},
	} {
		t.Run(test.toolName, func(t *testing.T) {
			server, err := repo.UpsertImportedMCPServer(context.Background(), repository.ImportedMCPServer{
				Name: test.serverName, URL: "http://" + test.serverName + ":9000/mcp",
				Tier: repository.MCPServerTierLocalContainer,
			})
			if err != nil {
				t.Fatal(err)
			}
			err = service.RecordDiscovery(context.Background(), server.ID, []DiscoveredTool{{
				Name: test.toolName, SchemaJSON: `{"type":"object"}`,
			}})
			if err == nil {
				t.Fatalf("reserved tool %q was accepted", test.toolName)
			}
		})
	}
}

func TestMCPResponseCannotReflectRegisteredBearer(t *testing.T) {
	const token = "vendor-secret-reflection"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			ID int64 `json:"id"`
		}
		_ = json.NewDecoder(r.Body).Decode(&request)
		w.Header().Set("content-type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"jsonrpc": "2.0",
			"id":      request.ID,
			"result": map[string]any{
				"content": []any{map[string]any{"type": "text", "text": "echo " + token}},
			},
		})
	}))
	t.Cleanup(server.Close)

	result, err := newMCPClient(server.URL, token, server.Client()).callTool(
		context.Background(),
		"vendor.echo",
		map[string]any{},
	)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), token) {
		t.Fatalf("MCP result reflected registered bearer: %s", encoded)
	}
}
