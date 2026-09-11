package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// These tests drive this server with a client built by the official Model
// Context Protocol Go SDK, pinned in go.mod at v1.7.0. The stock client is a
// dual-era implementation: it probes for the modern stateless revision first,
// receives this server's refusal, falls back to the `initialize` handshake at
// 2025-11-25, opens (and is refused) the optional GET stream, and only then
// discovers and calls.
//
// The dependency is deliberate and test-only. This module otherwise depends on
// nothing outside the standard library, and that is worth keeping — but the
// acceptance criterion for CON-001 is that a *stock* client works against each
// bundled server, and a fixture written from Turing's own assumptions cannot
// establish that. The dependency never enters the server binary: it appears in
// no non-test file here, which `go list -deps ./cmd/server` shows. Nothing in
// these tests reaches the network.

// bearerRoundTripper adds the one thing a stock client needs that the protocol
// does not describe: this endpoint sits behind a shared bundled bearer.
type bearerRoundTripper struct {
	token string
	inner http.RoundTripper
}

func (b *bearerRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	cloned := request.Clone(request.Context())
	cloned.Header.Set("Authorization", "Bearer "+b.token)
	inner := b.inner
	if inner == nil {
		inner = http.DefaultTransport
	}
	return inner.RoundTrip(cloned)
}

func newStockClientSession(t *testing.T, server *httptest.Server, token string) *sdk.ClientSession {
	t.Helper()
	httpClient := server.Client()
	httpClient.Transport = &bearerRoundTripper{token: token, inner: httpClient.Transport}
	client := sdk.NewClient(&sdk.Implementation{Name: "stock-conformance-client", Version: "1.0.0"}, nil)
	session, err := client.Connect(context.Background(), &sdk.StreamableClientTransport{
		Endpoint:   server.URL + "/mcp",
		HTTPClient: httpClient,
	}, nil)
	if err != nil {
		t.Fatalf("a stock MCP client could not initialize against this server: %v", err)
	}
	t.Cleanup(func() { _ = session.Close() })
	return session
}

func TestAStockClientInitializesDiscoversAndCallsASafeTool(t *testing.T) {
	server := httptest.NewServer(newHandler("system-token"))
	t.Cleanup(server.Close)
	session := newStockClientSession(t, server, "system-token")

	if got := session.InitializeResult().ProtocolVersion; got != supportedProtocolVersion {
		t.Fatalf("negotiated version = %q, want %q", got, supportedProtocolVersion)
	}
	if got := session.InitializeResult().ServerInfo.Name; got != serverImplementation {
		t.Fatalf("serverInfo.name = %q", got)
	}

	tools, err := session.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("a stock client could not list tools: %v", err)
	}
	names := make(map[string]bool, len(tools.Tools))
	for _, tool := range tools.Tools {
		names[tool.Name] = true
		if !tool.Annotations.ReadOnlyHint {
			t.Errorf("%s did not reach a stock client as read-only", tool.Name)
		}
	}
	for _, want := range []string{"system.health", "system.time", "system.echo", "system.info"} {
		if !names[want] {
			t.Errorf("tool %q was not discovered by a stock client", want)
		}
	}

	result, err := session.CallTool(context.Background(), &sdk.CallToolParams{
		Name: "system.echo", Arguments: map[string]any{"text": "conformance"},
	})
	if err != nil {
		t.Fatalf("a stock client could not call system.echo: %v", err)
	}
	if result.IsError {
		t.Fatalf("system.echo reported a tool error: %+v", result)
	}
	// The call succeeding is not the acceptance criterion — the client has to
	// be able to READ the answer. A bare JSON-RPC result carrying the tool's
	// own keys decodes into an empty CallToolResult, so a stock client would
	// see nothing at all.
	if len(result.Content) == 0 && result.StructuredContent == nil {
		t.Fatal("a stock client received no content and no structuredContent; the tool result is not a CallToolResult")
	}
	var echoed string
	for _, block := range result.Content {
		if text, ok := block.(*sdk.TextContent); ok {
			echoed += text.Text
		}
	}
	if !strings.Contains(echoed, "conformance") {
		t.Fatalf("the echoed text never reached the stock client: content=%q structured=%v", echoed, result.StructuredContent)
	}
}

func TestAStockClientCanPingThisServer(t *testing.T) {
	server := httptest.NewServer(newHandler("system-token"))
	t.Cleanup(server.Close)
	session := newStockClientSession(t, server, "system-token")

	if err := session.Ping(context.Background(), nil); err != nil {
		t.Fatalf("a stock client could not ping this server: %v", err)
	}
}

func TestAStockClientSeesAProtocolErrorForAnUnknownTool(t *testing.T) {
	// An unknown tool is a protocol error, not a tool execution result the
	// model should try to recover from.
	server := httptest.NewServer(newHandler("system-token"))
	t.Cleanup(server.Close)
	session := newStockClientSession(t, server, "system-token")

	result, err := session.CallTool(context.Background(), &sdk.CallToolParams{Name: "system.nope"})
	if err == nil {
		t.Fatalf("CallTool result = %+v, want a protocol error", result)
	}
}

func TestAStockClientIsRefusedWithoutTheBundledCredential(t *testing.T) {
	server := httptest.NewServer(newHandler("system-token"))
	t.Cleanup(server.Close)
	client := sdk.NewClient(&sdk.Implementation{Name: "stock-conformance-client", Version: "1.0.0"}, nil)

	_, err := client.Connect(context.Background(), &sdk.StreamableClientTransport{
		Endpoint:   server.URL + "/mcp",
		HTTPClient: server.Client(),
	}, nil)
	if err == nil {
		t.Fatal("a stock client initialized without the bundled bearer")
	}
}
