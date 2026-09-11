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
// discovers and calls. Passing it is evidence about the protocol rather than
// about Turing's own assumptions. Nothing here reaches the network.

// bearerRoundTripper adds the one thing a stock client needs that the protocol
// does not describe: these endpoints sit behind a shared bundled bearer.
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
	server := httptest.NewServer(testFilesHandler(t))
	t.Cleanup(server.Close)
	session := newStockClientSession(t, server, "files-token")

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
		if tool.Name == "files.read" && !tool.Annotations.ReadOnlyHint {
			t.Error("files.read did not reach a stock client as read-only")
		}
	}
	for _, want := range []string{"files.list", "files.search", "files.read", "files.create", "files.update"} {
		if !names[want] {
			t.Errorf("tool %q was not discovered by a stock client", want)
		}
	}

	// A safe tool still needs Turing's own provenance capability, which is not
	// part of the protocol: without it the call is refused, and that refusal
	// reaches the stock client as a protocol error rather than as a result.
	if _, err := session.CallTool(context.Background(), &sdk.CallToolParams{
		Name: "files.list", Arguments: map[string]any{},
	}); err == nil {
		t.Fatal("a safe tool call without a provenance capability should be refused")
	}
}

func TestAStockClientReadsTheResultOfACompletedToolCall(t *testing.T) {
	// The refusal above proves the gate holds; this proves the other half of
	// the acceptance criterion — that when a call does complete, the answer is
	// shaped so a stock client can actually read it.
	server := httptest.NewServer(testFilesHandler(t))
	t.Cleanup(server.Close)
	session := newStockClientSession(t, server, "files-token")

	result, err := session.CallTool(context.Background(), &sdk.CallToolParams{
		Name:      "files.list",
		Arguments: map[string]any{},
		Meta:      sdk.Meta{"provenanceToken": testProvenanceToken(t, "files.list", map[string]any{}, "")},
	})
	if err != nil {
		t.Fatalf("a stock client could not complete files.list: %v", err)
	}
	if result.IsError {
		t.Fatalf("files.list reported a tool error: %+v", result)
	}
	if len(result.Content) == 0 && result.StructuredContent == nil {
		t.Fatal("a stock client received no content and no structuredContent; the tool result is not a CallToolResult")
	}
}

func TestAStockClientCanPingThisServer(t *testing.T) {
	server := httptest.NewServer(testFilesHandler(t))
	t.Cleanup(server.Close)
	session := newStockClientSession(t, server, "files-token")

	if err := session.Ping(context.Background(), nil); err != nil {
		t.Fatalf("a stock client could not ping this server: %v", err)
	}
}

func TestAStockClientCannotMutateWithoutAnApproval(t *testing.T) {
	// Conformance must not have opened a way around the approval boundary: a
	// stock client speaking the protocol perfectly still cannot write.
	server := httptest.NewServer(testFilesHandler(t))
	t.Cleanup(server.Close)
	session := newStockClientSession(t, server, "files-token")

	_, err := session.CallTool(context.Background(), &sdk.CallToolParams{
		Name:      "files.create",
		Arguments: map[string]any{"path": "note.txt", "content": "written by a stock client"},
	})
	if err == nil {
		t.Fatal("a stock client wrote a file with no approval token, provenance or preview")
	}
	if strings.Contains(err.Error(), "written by a stock client") {
		t.Fatalf("the refusal echoed the attempted content: %v", err)
	}
}

func TestAStockClientIsRefusedWithoutTheBundledCredential(t *testing.T) {
	server := httptest.NewServer(testFilesHandler(t))
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
