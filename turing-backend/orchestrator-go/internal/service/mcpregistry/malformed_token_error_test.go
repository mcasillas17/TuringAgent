package mcpregistry

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestMalformedMCPEnvelopeCannotReflectRegisteredBearer(t *testing.T) {
	const token = "vendor-secret-envelope-reflection"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("content-type", "application/json")
		_, _ = fmt.Fprintf(w, `{"jsonrpc":"2.0","id":1,%q:true,"result":{}}`, token)
	}))
	t.Cleanup(server.Close)

	_, err := newMCPClient(server.URL, token, server.Client()).callTool(
		context.Background(), "vendor.fail", map[string]any{},
	)
	if err == nil || strings.Contains(err.Error(), token) {
		t.Fatalf("malformed MCP error leaked registered bearer: %v", err)
	}
}
