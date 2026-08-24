package mcpregistry

import (
	"context"
	"testing"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// classifyImportedURL is the one shared URL validator both ImportJSON and
// RegisterMcpServer/RotateMcpServerToken (via validateServerDefinition)
// call, so a case it refuses here is refused identically through both
// entry points. url.Parse alone does not refuse a bare trailing "?" (a
// query with nothing after it): RawQuery stays empty in that case (there
// is nothing to be empty or not), but ForceQuery is true, so a check that
// only looked at RawQuery != "" would let it through unnoticed.
func TestClassifyImportedURLRejectsForceQueryTrailingQuestionMark(t *testing.T) {
	for _, test := range []struct {
		name string
		url  string
	}{
		{name: "remote https", url: "https://vendor.example/mcp?"},
		{name: "local http", url: "http://vendor:9000/mcp?"},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, _, err := classifyImportedURL(test.url); err == nil {
				t.Fatalf("url %q with a trailing bare '?' (ForceQuery) was accepted, want refused", test.url)
			}
		})
	}
}

// A port of 0 is syntactically a valid decimal string (so url.Parse's own
// Port() accepts it), but it is not a usable TCP destination port. Both
// tiers must refuse it explicitly.
func TestClassifyImportedURLRejectsPortZero(t *testing.T) {
	for _, test := range []struct {
		name string
		url  string
	}{
		{name: "remote https", url: "https://vendor.example:0/mcp"},
		{name: "local http", url: "http://vendor:0/mcp"},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, _, err := classifyImportedURL(test.url); err == nil {
				t.Fatalf("url %q with port 0 was accepted, want refused", test.url)
			}
		})
	}
}

// A port beyond the valid 16-bit TCP range (1-65535) must be refused
// explicitly: url.Parse's own Port() only guarantees a decimal-digit
// string, never that it fits the actual valid range.
func TestClassifyImportedURLRejectsPortOverflow(t *testing.T) {
	for _, test := range []struct {
		name string
		url  string
	}{
		{name: "remote https", url: "https://vendor.example:99999/mcp"},
		{name: "local http", url: "http://vendor:99999/mcp"},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, _, err := classifyImportedURL(test.url); err == nil {
				t.Fatalf("url %q with an overflowing port was accepted, want refused", test.url)
			}
		})
	}
}

// A valid, in-bounds explicit port must still classify normally for both
// tiers — the new port check must not be off-by-one against a legitimate
// port number.
func TestClassifyImportedURLAcceptsValidExplicitPorts(t *testing.T) {
	if _, canonical, err := classifyImportedURL("https://vendor.example:8443/mcp"); err != nil {
		t.Fatalf("valid remote port was refused: %v", err)
	} else if canonical != "https://vendor.example:8443/mcp" {
		t.Fatalf("canonical = %q", canonical)
	}
	if _, canonical, err := classifyImportedURL("http://vendor:9000/mcp"); err != nil {
		t.Fatalf("valid local port was refused: %v", err)
	} else if canonical != "http://vendor:9000/mcp" {
		t.Fatalf("canonical = %q", canonical)
	}
}

// An ordinary remote URL with no explicit port at all (defaulting to 443)
// must remain accepted: the port check only applies when a port is
// actually present.
func TestClassifyImportedURLAcceptsRemoteURLWithNoExplicitPort(t *testing.T) {
	if _, _, err := classifyImportedURL("https://vendor.example/mcp"); err != nil {
		t.Fatalf("remote URL with no explicit port was refused: %v", err)
	}
}

// The ForceQuery and port-range refusals must be reachable — identically —
// through both the file-import path (ImportJSON) and the direct
// registration path (RegisterMcpServer), proving the parity the shared
// classifyImportedURL/validateServerDefinition implementation is meant to
// guarantee.
func TestURLValidationParityBetweenFileImportAndRegister(t *testing.T) {
	for _, test := range []struct {
		name string
		url  string
		tier turingv1.McpServerTier
	}{
		{name: "remote ForceQuery", url: "https://vendor.example/mcp?", tier: turingv1.McpServerTier_MCP_SERVER_TIER_REMOTE_URL},
		{name: "local ForceQuery", url: "http://vendor:9000/mcp?", tier: turingv1.McpServerTier_MCP_SERVER_TIER_LOCAL_CONTAINER},
		{name: "remote port zero", url: "https://vendor.example:0/mcp", tier: turingv1.McpServerTier_MCP_SERVER_TIER_REMOTE_URL},
		{name: "local port zero", url: "http://vendor:0/mcp", tier: turingv1.McpServerTier_MCP_SERVER_TIER_LOCAL_CONTAINER},
		{name: "remote port overflow", url: "https://vendor.example:99999/mcp", tier: turingv1.McpServerTier_MCP_SERVER_TIER_REMOTE_URL},
		{name: "local port overflow", url: "http://vendor:99999/mcp", tier: turingv1.McpServerTier_MCP_SERVER_TIER_LOCAL_CONTAINER},
	} {
		t.Run(test.name, func(t *testing.T) {
			service, repo := newRegistryTestService(t)
			ctx := context.Background()

			report, err := service.ImportJSON(ctx, []byte(`{
				"mcpServers": {
					"vendor": {"url": "`+test.url+`"}
				}
			}`))
			if err != nil {
				t.Fatal(err)
			}
			if _, refused := report.Unsupported["vendor"]; !refused {
				t.Fatalf("file import: Unsupported = %+v, want vendor refused for url %q", report.Unsupported, test.url)
			}
			if _, err := repo.GetMCPServerByName(ctx, "vendor"); err == nil {
				t.Fatal("file import: a refused URL must not create a server row")
			}

			_, rpcErr := service.RegisterMcpServer(ctx, &turingv1.RegisterMcpServerRequest{
				Name: "vendor-register", Url: test.url, Tier: test.tier,
			})
			if status.Code(rpcErr) != codes.InvalidArgument {
				t.Fatalf("register: code = %v, want InvalidArgument for url %q", status.Code(rpcErr), test.url)
			}
		})
	}
}
