package mcpregistry

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"github.com/mcasillas17/TuringAgent/turing-backend/mcpwire"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
)

// These tests point the orchestrator's registry client at a server built by the
// official Model Context Protocol Go SDK, pinned in go.mod at v1.7.0. That
// server is an independent implementation: it enforces the Accept header,
// answers over an event stream, assigns and requires a session id, and refuses
// operation-phase calls before `initialize`. Nothing here reaches the network
// or needs a third-party credential.

type stockLookupInput struct {
	Query string `json:"query"`
}

func newStockRegistryPeer(t *testing.T) *httptest.Server {
	t.Helper()
	server := sdk.NewServer(&sdk.Implementation{Name: "stock-vendor", Version: "2.0.0"}, nil)
	sdk.AddTool(server, &sdk.Tool{
		Name:        "vendor.lookup",
		Description: "Look up a vendor record.",
		// A peer is free to claim anything here. Discovery keeps the name and
		// the input schema and drops the rest, so this hint reaches no policy.
		Annotations: &sdk.ToolAnnotations{ReadOnlyHint: true},
	}, func(_ context.Context, _ *sdk.CallToolRequest, in stockLookupInput) (*sdk.CallToolResult, any, error) {
		return &sdk.CallToolResult{Content: []sdk.Content{&sdk.TextContent{Text: "found:" + in.Query}}}, nil, nil
	})
	httpServer := httptest.NewServer(stockMCPHandler(server))
	t.Cleanup(httpServer.Close)
	return httpServer
}

func stockMCPHandler(server *sdk.Server) http.Handler {
	return sdk.NewStreamableHTTPHandler(func(*http.Request) *sdk.Server { return server }, nil)
}

func TestRegistryClientInitializesAndDiscoversAgainstAStockServer(t *testing.T) {
	stock := newStockRegistryPeer(t)

	tools, err := newMCPClient(stock.URL, "", stock.Client()).listTools(context.Background())
	if err != nil {
		t.Fatalf("listTools against a stock MCP server: %v", err)
	}
	if len(tools) != 1 || tools[0]["name"] != "vendor.lookup" {
		t.Fatalf("tools = %#v", tools)
	}
}

func TestRegistryClientCallsAStockServerTool(t *testing.T) {
	stock := newStockRegistryPeer(t)

	result, err := newMCPClient(stock.URL, "", stock.Client()).
		callTool(context.Background(), "vendor.lookup", map[string]any{"query": "acme"})
	if err != nil {
		t.Fatalf("callTool against a stock MCP server: %v", err)
	}
	content, _ := result["content"].([]any)
	if len(content) != 1 {
		t.Fatalf("result = %#v", result)
	}
	block, _ := content[0].(map[string]any)
	if block["text"] != "found:acme" {
		t.Fatalf("content = %#v", block)
	}
}

func TestEnablingAStockServerDiscoversItsToolsWithADefaultApprovalPolicy(t *testing.T) {
	// The whole enable path, not just the client: discovery over the real
	// protocol, then the registry's own default policy. A tool a stock server
	// annotates as read-only still arrives requiring approval.
	stock := newStockRegistryPeer(t)
	service, repo := newRegistryTestService(t)
	service.httpClient = stock.Client()
	registered, err := repo.RegisterMCPServer(context.Background(), repository.ImportedMCPServer{
		Name: "stock", URL: stock.URL, Tier: repository.MCPServerTierLocalContainer,
	})
	if err != nil {
		t.Fatal(err)
	}

	enabled, err := service.SetMcpServerEnabled(context.Background(), &turingv1.SetMcpServerEnabledRequest{
		ServerId: registered.Server.ID, Enabled: true,
	})
	if err != nil {
		t.Fatalf("SetMcpServerEnabled: %v", err)
	}
	if enabled.GetLiveness() != turingv1.McpServerLiveness_MCP_SERVER_LIVENESS_UP {
		t.Fatalf("liveness = %v, want up", enabled.GetLiveness())
	}
	if len(enabled.GetTools()) != 1 || enabled.GetTools()[0].GetToolName() != "vendor.lookup" {
		t.Fatalf("tools = %+v", enabled.GetTools())
	}
	if enabled.GetTools()[0].GetPolicy() != turingv1.ToolPolicy_TOOL_POLICY_APPROVAL_REQUIRED {
		t.Fatalf("policy = %v; a peer's readOnlyHint must not lower the default policy", enabled.GetTools()[0].GetPolicy())
	}
}

func TestAStockServerThatRefusesTheCredentialFailsDiscoveryClearly(t *testing.T) {
	// A stock server behind an authenticating proxy that rejects the bearer:
	// discovery must fail rather than proceed on an unnegotiated connection,
	// and the failure must not carry the credential.
	const token = "vendor-conformance-sentinel-6b21f9"
	stock := newStockRegistryPeer(t)
	guarded := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer expected" {
			http.Error(w, "unauthorized for "+token, http.StatusUnauthorized)
			return
		}
		stock.Config.Handler.ServeHTTP(w, r)
	}))
	t.Cleanup(guarded.Close)

	_, err := newMCPClient(guarded.URL, token, guarded.Client()).listTools(context.Background())
	if err == nil {
		t.Fatal("want discovery to fail against a server that refuses the credential")
	}
	if !strings.Contains(err.Error(), "401") {
		t.Fatalf("error = %v, want the refusal surfaced", err)
	}
	if strings.Contains(err.Error(), token) {
		t.Fatalf("error leaked the bearer token: %q", err)
	}
}

func TestEnablingAStockServerReleasesTheSessionItWasAssigned(t *testing.T) {
	// Session termination proven against the real SDK server rather than a
	// hand-rolled handler, and through a production entry point rather than by
	// calling releaseSession directly. Both matter: the stock server is what
	// actually assigns and requires a session id, and `defer peer.releaseSession()`
	// in discover() is the wiring that would otherwise be unguarded — a test
	// that calls the method by hand stays green if that line is deleted.
	deleted := make(chan string, 4)
	server := sdk.NewServer(&sdk.Implementation{Name: "stock-vendor", Version: "2.0.0"}, nil)
	sdk.AddTool(server, &sdk.Tool{Name: "vendor.lookup", Description: "Look up a vendor record."},
		func(_ context.Context, _ *sdk.CallToolRequest, in stockLookupInput) (*sdk.CallToolResult, any, error) {
			return &sdk.CallToolResult{Content: []sdk.Content{&sdk.TextContent{Text: "found:" + in.Query}}}, nil, nil
		})
	handler := stockMCPHandler(server)
	stock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			if got := r.Header.Get(mcpwire.ProtocolVersionHeader); got != mcpwire.ProtocolVersion {
				t.Errorf("delete carried protocol version %q, want %q", got, mcpwire.ProtocolVersion)
			}
			select {
			case deleted <- r.Header.Get(mcpwire.SessionHeader):
			default:
			}
		}
		handler.ServeHTTP(w, r)
	}))
	defer stock.Close()

	service, repo := newRegistryTestService(t)
	service.httpClient = stock.Client()
	registered, err := repo.RegisterMCPServer(context.Background(), repository.ImportedMCPServer{
		Name: "stock", URL: stock.URL, Tier: repository.MCPServerTierLocalContainer,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.SetMcpServerEnabled(context.Background(), &turingv1.SetMcpServerEnabledRequest{
		ServerId: registered.Server.ID, Enabled: true,
	}); err != nil {
		t.Fatalf("SetMcpServerEnabled: %v", err)
	}

	// The release is deliberately detached from the dispatch, so wait for it.
	select {
	case session := <-deleted:
		if session == "" {
			t.Fatal("the delete carried no session id, so it released nothing")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the stock server never received a session delete; discovery abandoned the session it opened")
	}
}
