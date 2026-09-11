package mcp

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// These tests point the runtime's own MCP client at a server built by the
// official Model Context Protocol Go SDK, pinned in go.mod at v1.7.0. That
// server is an independent implementation of the specification — it enforces
// the Accept header, answers over an event stream, assigns and requires a
// session id, and refuses operation-phase calls before `initialize` — so
// passing it is evidence about the protocol rather than about Turing's own
// assumptions. Nothing here reaches the network or needs a credential.

type stockEchoInput struct {
	Text string `json:"text"`
}

// newStockMCPServer returns a stock SDK server exposing one safe tool.
func newStockMCPServer(t *testing.T) *httptest.Server {
	t.Helper()
	server := sdk.NewServer(&sdk.Implementation{Name: "stock-test-server", Version: "1.0.0"}, nil)
	sdk.AddTool(server, &sdk.Tool{
		Name:        "stock.echo",
		Description: "Echo the supplied text.",
		Annotations: &sdk.ToolAnnotations{ReadOnlyHint: true},
	}, func(_ context.Context, _ *sdk.CallToolRequest, in stockEchoInput) (*sdk.CallToolResult, any, error) {
		return &sdk.CallToolResult{Content: []sdk.Content{&sdk.TextContent{Text: "echo:" + in.Text}}}, nil, nil
	})
	httpServer := httptest.NewServer(sdk.NewStreamableHTTPHandler(
		func(*http.Request) *sdk.Server { return server }, nil))
	t.Cleanup(httpServer.Close)
	return httpServer
}

func TestRuntimeClientInitializesDiscoversAndCallsAStockServer(t *testing.T) {
	stock := newStockMCPServer(t)
	client := NewClient(stock.URL, "", stock.Client())

	tools, err := client.ListTools(context.Background())
	if err != nil {
		t.Fatalf("ListTools against a stock MCP server: %v", err)
	}
	if len(tools) != 1 || tools[0]["name"] != "stock.echo" {
		t.Fatalf("tools = %#v, want the stock server's one tool", tools)
	}

	result, err := client.CallTool(context.Background(), "stock.echo", map[string]any{"text": "hi"})
	if err != nil {
		t.Fatalf("CallTool against a stock MCP server: %v", err)
	}
	content, _ := result["content"].([]any)
	if len(content) != 1 {
		t.Fatalf("result = %#v", result)
	}
	block, _ := content[0].(map[string]any)
	if block["text"] != "echo:hi" {
		t.Fatalf("content = %#v", block)
	}

	if err := client.Ping(context.Background()); err != nil {
		t.Fatalf("Ping against a stock MCP server: %v", err)
	}
}

func TestRuntimeClientSurvivesAStockServerRestart(t *testing.T) {
	// A restarted peer forgets the session it assigned. The transport answers
	// the old session with 404, and discovery has to re-initialize against the
	// replacement rather than fail permanently.
	//
	// The restart is modelled by swapping the handler behind one address, which
	// is what a client sees when a container is replaced: same endpoint, new
	// process, unknown session.
	var current atomic.Pointer[http.Handler]
	install := func() {
		server := sdk.NewServer(&sdk.Implementation{Name: "stock-test-server", Version: "1.0.0"}, nil)
		sdk.AddTool(server, &sdk.Tool{Name: "stock.echo", Description: "Echo the supplied text."},
			func(_ context.Context, _ *sdk.CallToolRequest, in stockEchoInput) (*sdk.CallToolResult, any, error) {
				return &sdk.CallToolResult{Content: []sdk.Content{&sdk.TextContent{Text: in.Text}}}, nil, nil
			})
		handler := http.Handler(sdk.NewStreamableHTTPHandler(
			func(*http.Request) *sdk.Server { return server }, nil))
		current.Store(&handler)
	}
	install()
	stock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		(*current.Load()).ServeHTTP(w, r)
	}))
	t.Cleanup(stock.Close)

	// Keep-alives are off so the restart is purely a *session* loss. Whether a
	// pooled TCP connection survives a peer restart is the transport's own
	// concern, and an EOF there is already classified retryable for the
	// worker loop; what this test is about is the MCP session the peer forgot.
	httpClient := stock.Client()
	httpClient.Transport.(*http.Transport).DisableKeepAlives = true

	client := NewClient(stock.URL, "", httpClient)
	if _, err := client.ListTools(context.Background()); err != nil {
		t.Fatalf("first ListTools: %v", err)
	}

	install() // the peer restarts; every session it had assigned is gone

	if _, err := client.ListTools(context.Background()); err != nil {
		t.Fatalf("ListTools after the peer restarted: %v", err)
	}
}

func TestRuntimeClientReportsAStockServersProtocolErrorForAnUnknownTool(t *testing.T) {
	// A protocol error (unknown tool) is not a tool execution error, and must
	// not be reported as a successful result the model then reads.
	stock := newStockMCPServer(t)
	client := NewClient(stock.URL, "", stock.Client())

	result, err := client.CallTool(context.Background(), "stock.missing", nil)
	if err == nil {
		t.Fatalf("CallTool result = %#v, want a protocol error", result)
	}
	var rpcErr JSONRPCError
	if !errors.As(err, &rpcErr) {
		t.Fatalf("error = %T %v, want a JSON-RPC protocol error", err, err)
	}
	if !strings.Contains(strings.ToLower(err.Error()), "tool") {
		t.Fatalf("error = %v, want it to name the unknown tool", err)
	}
}
