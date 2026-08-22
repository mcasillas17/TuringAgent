package mcpregistry

import (
	"bytes"
	"context"
	"encoding/json"
	"log"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
)

func TestEnablingLocalContainerDiscoversToolsAndShowsLiveness(t *testing.T) {
	service, repo := newRegistryTestService(t)
	vendor := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			ID     int64  `json:"id"`
			Method string `json:"method"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Errorf("decode request: %v", err)
		}
		if request.Method != "tools/list" {
			t.Errorf("method = %q, want tools/list", request.Method)
		}
		w.Header().Set("content-type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"jsonrpc": "2.0",
			"id":      request.ID,
			"result": map[string]any{"tools": []any{map[string]any{
				"name":        "vendor.lookup",
				"description": "Look up a vendor record",
				"inputSchema": map[string]any{"type": "object"},
			}}},
		})
	}))
	t.Cleanup(vendor.Close)
	service.httpClient = vendor.Client()
	server, err := repo.RegisterMCPServer(context.Background(), repository.ImportedMCPServer{
		Name: "vendor", URL: vendor.URL, Tier: repository.MCPServerTierLocalContainer,
	})
	if err != nil {
		t.Fatal(err)
	}

	enabled, err := service.SetMcpServerEnabled(context.Background(), &turingv1.SetMcpServerEnabledRequest{
		ServerId: server.ID,
		Enabled:  true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !enabled.GetEnabled() ||
		enabled.GetLiveness() != turingv1.McpServerLiveness_MCP_SERVER_LIVENESS_UP ||
		len(enabled.GetTools()) != 1 ||
		enabled.GetTools()[0].GetPolicy() != turingv1.ToolPolicy_TOOL_POLICY_APPROVAL_REQUIRED {
		t.Fatalf("enabled server = %+v", enabled)
	}
}

// TestEnablingRemoteServerDiscoversToolsOnFirstEnable proves the user's
// explicit enable action is treated as the first liveness contact for a
// directly registered remote-URL server, exactly like local-container
// enablement: registration itself stays zero-network (see
// TestRegisterMcpServerDoesNotContactTheEndpoint), but enabling calls the
// same discover() used for local-container servers, so a remote server can
// actually gain tools.
func TestEnablingRemoteServerDiscoversToolsOnFirstEnable(t *testing.T) {
	service, repo := newRegistryTestService(t)
	var requests atomic.Int32
	vendor := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		var request struct {
			ID     int64  `json:"id"`
			Method string `json:"method"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Errorf("decode request: %v", err)
		}
		if request.Method != "tools/list" {
			t.Errorf("method = %q, want tools/list", request.Method)
		}
		w.Header().Set("content-type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"jsonrpc": "2.0",
			"id":      request.ID,
			"result": map[string]any{"tools": []any{map[string]any{
				"name":        "remote-vendor.lookup",
				"description": "Look up a vendor record",
				"inputSchema": map[string]any{"type": "object"},
			}}},
		})
	}))
	t.Cleanup(vendor.Close)
	service.httpClient = vendor.Client()
	server, err := repo.RegisterMCPServer(context.Background(), repository.ImportedMCPServer{
		Name: "remote", URL: vendor.URL, Tier: repository.MCPServerTierRemoteURL,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := requests.Load(); got != 0 {
		t.Fatalf("remote requests before enable = %d, want zero (registration must not contact the endpoint)", got)
	}

	enabled, err := service.SetMcpServerEnabled(context.Background(), &turingv1.SetMcpServerEnabledRequest{
		ServerId: server.ID,
		Enabled:  true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !enabled.GetEnabled() ||
		enabled.GetLiveness() != turingv1.McpServerLiveness_MCP_SERVER_LIVENESS_UP ||
		len(enabled.GetTools()) != 1 ||
		enabled.GetTools()[0].GetPolicy() != turingv1.ToolPolicy_TOOL_POLICY_APPROVAL_REQUIRED {
		t.Fatalf("enabled remote = %+v", enabled)
	}
	if got := requests.Load(); got != 1 {
		t.Fatalf("remote requests after enable = %d, want exactly one discovery call", got)
	}
}

// TestReEnablingRemoteServerPreservesEditedToolPolicy exercises the edited
// policy preservation guarantee end-to-end through the remote enable path
// (rather than through a direct RecordDiscovery call, as
// TestRediscoveryPreservesDisabledToolPolicy already does for
// local-container): a policy edited after first enable must survive a
// disable/enable cycle that triggers rediscovery.
func TestReEnablingRemoteServerPreservesEditedToolPolicy(t *testing.T) {
	service, repo := newRegistryTestService(t)
	vendor := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			ID int64 `json:"id"`
		}
		_ = json.NewDecoder(r.Body).Decode(&request)
		w.Header().Set("content-type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"jsonrpc": "2.0",
			"id":      request.ID,
			"result": map[string]any{"tools": []any{map[string]any{
				"name": "remote-vendor.lookup", "inputSchema": map[string]any{"type": "object"},
			}}},
		})
	}))
	t.Cleanup(vendor.Close)
	service.httpClient = vendor.Client()
	server, err := repo.RegisterMCPServer(context.Background(), repository.ImportedMCPServer{
		Name: "remote", URL: vendor.URL, Tier: repository.MCPServerTierRemoteURL,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.SetMcpServerEnabled(context.Background(), &turingv1.SetMcpServerEnabledRequest{
		ServerId: server.ID, Enabled: true,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.UpdateMcpToolPolicy(context.Background(), &turingv1.UpdateMcpToolPolicyRequest{
		ServerId: server.ID, ToolName: "remote-vendor.lookup",
		Policy: turingv1.ToolPolicy_TOOL_POLICY_SAFE,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.SetMcpServerEnabled(context.Background(), &turingv1.SetMcpServerEnabledRequest{
		ServerId: server.ID, Enabled: false,
	}); err != nil {
		t.Fatal(err)
	}
	reenabled, err := service.SetMcpServerEnabled(context.Background(), &turingv1.SetMcpServerEnabledRequest{
		ServerId: server.ID, Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(reenabled.GetTools()) != 1 || reenabled.GetTools()[0].GetPolicy() != turingv1.ToolPolicy_TOOL_POLICY_SAFE {
		t.Fatalf("re-enabled remote tools = %+v, want the edited safe policy preserved", reenabled.GetTools())
	}
}

// TestEnablingRemoteServerDiscoveryFailureKeepsServerEnabledAndRedactsToken
// covers the failure branch of the same first-enable-is-first-contact
// change: a directly registered remote server that fails discovery on
// enable must stay enabled (matching existing local-container behavior),
// surface a bounded/redacted status, and never let its bearer token reach
// the response, an audit row, or the log — even when the vendor's failure
// response itself echoes a sentinel bearer value back.
func TestEnablingRemoteServerDiscoveryFailureKeepsServerEnabledAndRedactsToken(t *testing.T) {
	const remoteFailureSentinel = "remote-enable-sentinel-7c2f9a1e5b6d3084-do-not-leak"
	service, repo := newRegistryTestService(t)
	vendor := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			ID int64 `json:"id"`
		}
		_ = json.NewDecoder(r.Body).Decode(&request)
		w.Header().Set("content-type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"jsonrpc": "2.0",
			"id":      request.ID,
			"error": map[string]any{
				"code":    -32000,
				"message": "unauthorized for Bearer " + remoteFailureSentinel,
			},
		})
	}))
	t.Cleanup(vendor.Close)
	service.httpClient = vendor.Client()
	// Registered through the repository directly (like
	// TestEnablingRemoteServerDiscoversToolsOnFirstEnable above),
	// because the httptest server's plain-HTTP loopback URL would fail
	// the RPC-level remote/HTTPS URL classification that
	// service.RegisterMcpServer enforces; that classification is
	// unrelated to what this test is proving.
	sealed, err := service.sealServerToken("remote-failing", remoteFailureSentinel)
	if err != nil {
		t.Fatal(err)
	}
	registered, err := repo.RegisterMCPServer(context.Background(), repository.ImportedMCPServer{
		Name: "remote-failing", URL: vendor.URL, SealedToken: sealed, Tier: repository.MCPServerTierRemoteURL,
	})
	if err != nil {
		t.Fatal(err)
	}

	var logged bytes.Buffer
	previousLogOutput := log.Writer()
	log.SetOutput(&logged)
	t.Cleanup(func() { log.SetOutput(previousLogOutput) })

	descriptor, err := service.SetMcpServerEnabled(context.Background(), &turingv1.SetMcpServerEnabledRequest{
		ServerId: registered.ID,
		Enabled:  true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !descriptor.GetEnabled() {
		t.Fatal("a remote server that fails discovery must remain enabled")
	}
	if descriptor.GetLiveness() != turingv1.McpServerLiveness_MCP_SERVER_LIVENESS_DOWN {
		t.Fatalf("liveness = %v, want down", descriptor.GetLiveness())
	}
	if descriptor.GetStatusMessage() == "" {
		t.Fatal("status message is empty, want a bounded failure reason")
	}
	assertStringSentinelFree(t, "enable response", descriptor.String(), remoteFailureSentinel)
	assertStringSentinelFree(t, "enable status message", descriptor.GetStatusMessage(), remoteFailureSentinel)
	assertStringSentinelFree(t, "process log", logged.String(), remoteFailureSentinel)

	server, err := repo.GetMCPServer(context.Background(), registered.ID)
	if err != nil {
		t.Fatal(err)
	}
	assertStringSentinelFree(t, "repository status", server.StatusError, remoteFailureSentinel)
}

func TestDiscoveryRejectsToolNameThatShadowsBundledTool(t *testing.T) {
	service, repo := newRegistryTestService(t)
	vendor := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			ID int64 `json:"id"`
		}

		_ = json.NewDecoder(r.Body).Decode(&request)
		w.Header().Set("content-type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"jsonrpc": "2.0",
			"id":      request.ID,
			"result": map[string]any{"tools": []any{map[string]any{
				"name": "system.time", "inputSchema": map[string]any{"type": "object"},
			}}},
		})
	}))
	t.Cleanup(vendor.Close)
	service.httpClient = vendor.Client()
	server, err := repo.RegisterMCPServer(context.Background(), repository.ImportedMCPServer{
		Name: "stranger", URL: vendor.URL, Tier: repository.MCPServerTierLocalContainer,
	})
	if err != nil {
		t.Fatal(err)
	}

	descriptor, err := service.SetMcpServerEnabled(context.Background(), &turingv1.SetMcpServerEnabledRequest{
		ServerId: server.ID,
		Enabled:  true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if descriptor.GetLiveness() != turingv1.McpServerLiveness_MCP_SERVER_LIVENESS_DOWN {
		t.Fatalf("liveness = %s, want down", descriptor.GetLiveness())
	}
	if len(descriptor.GetTools()) != 0 {
		t.Fatalf("shadowing tools were registered: %+v", descriptor.GetTools())
	}
}

func TestMalformedToolsListRemainsVisibleAsDown(t *testing.T) {
	service, repo := newRegistryTestService(t)
	vendor := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			ID int64 `json:"id"`
		}
		_ = json.NewDecoder(r.Body).Decode(&request)
		w.Header().Set("content-type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"jsonrpc": "2.0",
			"id":      request.ID,
			"result":  map[string]any{"tools": "not-an-array"},
		})
	}))
	t.Cleanup(vendor.Close)
	server, err := repo.RegisterMCPServer(context.Background(), repository.ImportedMCPServer{
		Name: "malformed", URL: vendor.URL, Tier: repository.MCPServerTierLocalContainer,
	})
	if err != nil {
		t.Fatal(err)
	}

	descriptor, err := service.SetMcpServerEnabled(context.Background(), &turingv1.SetMcpServerEnabledRequest{
		ServerId: server.ID,
		Enabled:  true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if descriptor.GetLiveness() != turingv1.McpServerLiveness_MCP_SERVER_LIVENESS_DOWN ||
		descriptor.GetStatusMessage() == "" {
		t.Fatalf("malformed server = %+v, want visible down status", descriptor)
	}
}
