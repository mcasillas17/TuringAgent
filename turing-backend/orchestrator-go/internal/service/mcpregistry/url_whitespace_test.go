package mcpregistry

import (
	"context"
	"strings"
	"testing"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
)

// validateServerDefinition must trim surrounding whitespace from rawURL
// before classifyImportedURL ever sees it, so RegisterMcpServer/
// RotateMcpServerToken (a direct gRPC call) and ImportJSON (an mcp.json
// file import) agree with each other and with the Flutter client, which
// already trims the URL text field before submitting it
// (`_url.text.trim()` in workspace_pages.dart). Without the trim, a
// trailing space survives into url.Parse's Path and comes back out
// percent-encoded ("%20") in the stored/returned canonical URL, and a
// leading space makes url.Parse fail outright — so an untrimmed direct
// call would both disagree with an untrimmed file import in one case (the
// client already stripped its own leading/trailing space, so
// canonicalization only ever runs on trimmed input from that side) and
// silently store a canonical URL with a trailing "%20" nobody intended.
func TestValidateServerDefinitionTrimsTrailingWhitespaceRemoteURL(t *testing.T) {
	validated, err := validateServerDefinition("vendor", "https://vendor.example/mcp ", nil, "")
	if err != nil {
		t.Fatalf("trailing whitespace on a remote URL was refused: %v", err)
	}
	if validated.URL != "https://vendor.example/mcp" {
		t.Fatalf("URL = %q, want the canonical URL with no trailing whitespace and no %%20", validated.URL)
	}
	if validated.Tier != repository.MCPServerTierRemoteURL {
		t.Fatalf("Tier = %q, want remote-url", validated.Tier)
	}
	if strings.Contains(validated.URL, "%20") {
		t.Fatalf("URL = %q, must not contain a percent-encoded trailing space", validated.URL)
	}
}

func TestValidateServerDefinitionTrimsTrailingWhitespaceLocalContainer(t *testing.T) {
	validated, err := validateServerDefinition("vendor", "http://vendor:9000/mcp ", nil, "")
	if err != nil {
		t.Fatalf("trailing whitespace on a local container URL was refused: %v", err)
	}
	if validated.URL != "http://vendor:9000/mcp" {
		t.Fatalf("URL = %q, want the canonical URL with no trailing whitespace and no %%20", validated.URL)
	}
	if validated.Tier != repository.MCPServerTierLocalContainer {
		t.Fatalf("Tier = %q, want local-container", validated.Tier)
	}
	if strings.Contains(validated.URL, "%20") {
		t.Fatalf("URL = %q, must not contain a percent-encoded trailing space", validated.URL)
	}
}

// Leading whitespace makes url.Parse itself fail ("first path segment in
// URL cannot contain colon") for an otherwise well-formed https://...
// value. Trimming before classifyImportedURL must make this succeed
// exactly as if the caller had never included the leading space.
func TestValidateServerDefinitionTrimsLeadingWhitespace(t *testing.T) {
	validated, err := validateServerDefinition("vendor", "  https://vendor.example/mcp", nil, "")
	if err != nil {
		t.Fatalf("leading whitespace on a remote URL was refused: %v", err)
	}
	if validated.URL != "https://vendor.example/mcp" {
		t.Fatalf("URL = %q, want the canonical URL with no leading whitespace", validated.URL)
	}
	if validated.Tier != repository.MCPServerTierRemoteURL {
		t.Fatalf("Tier = %q, want remote-url", validated.Tier)
	}
}

// Leading and trailing whitespace together, on both tiers, must both
// trim cleanly and classify to the tier the trimmed URL actually names.
func TestValidateServerDefinitionTrimsLeadingAndTrailingWhitespaceBothTiers(t *testing.T) {
	for _, test := range []struct {
		name string
		url  string
		tier repository.MCPServerTier
		want string
	}{
		{name: "remote", url: "  https://vendor.example/mcp  ", tier: repository.MCPServerTierRemoteURL, want: "https://vendor.example/mcp"},
		{name: "local", url: "\thttp://vendor:9000/mcp\n", tier: repository.MCPServerTierLocalContainer, want: "http://vendor:9000/mcp"},
	} {
		t.Run(test.name, func(t *testing.T) {
			validated, err := validateServerDefinition("vendor", test.url, nil, "")
			if err != nil {
				t.Fatalf("whitespace-padded url %q was refused: %v", test.url, err)
			}
			if validated.URL != test.want {
				t.Fatalf("URL = %q, want %q", validated.URL, test.want)
			}
			if validated.Tier != test.tier {
				t.Fatalf("Tier = %q, want %q", validated.Tier, test.tier)
			}
		})
	}
}

// A requested tier is still checked against the trimmed URL's own
// classification — trimming must happen before, not instead of, the
// tier-match check, so a caller that explicitly names the tier it expects
// still gets the agreement check it asked for.
func TestValidateServerDefinitionTrimsWhitespaceThenStillEnforcesRequestedTierMatch(t *testing.T) {
	remoteTier := repository.MCPServerTierRemoteURL
	if _, err := validateServerDefinition("vendor", "  https://vendor.example/mcp  ", &remoteTier, ""); err != nil {
		t.Fatalf("trimmed URL matching the requested tier was refused: %v", err)
	}

	localTier := repository.MCPServerTierLocalContainer
	if _, err := validateServerDefinition("vendor", "  https://vendor.example/mcp  ", &localTier, ""); err == nil {
		t.Fatal("a trimmed remote URL registered against a mismatched requested local-container tier must still be refused")
	}
}

// The trim must be reachable identically through both entry points that
// funnel through validateServerDefinition: a direct RegisterMcpServer
// call and an mcp.json ImportJSON entry. Both must accept the padded URL,
// classify the same tier, and store/return the exact same trimmed
// canonical URL with no percent-encoded whitespace.
func TestURLWhitespaceTrimParityBetweenFileImportAndRegister(t *testing.T) {
	for _, test := range []struct {
		name string
		url  string
		tier turingv1.McpServerTier
		want string
	}{
		{name: "remote trailing space", url: "https://vendor.example/mcp ", tier: turingv1.McpServerTier_MCP_SERVER_TIER_REMOTE_URL, want: "https://vendor.example/mcp"},
		{name: "local trailing space", url: "http://vendor:9000/mcp ", tier: turingv1.McpServerTier_MCP_SERVER_TIER_LOCAL_CONTAINER, want: "http://vendor:9000/mcp"},
		{name: "remote leading space", url: "  https://vendor.example/mcp", tier: turingv1.McpServerTier_MCP_SERVER_TIER_REMOTE_URL, want: "https://vendor.example/mcp"},
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
			if len(report.Imported) != 1 || report.Imported[0] != "vendor" {
				t.Fatalf("file import: Imported = %+v, want vendor imported for whitespace-padded url %q", report.Imported, test.url)
			}
			imported, err := repo.GetMCPServerByName(ctx, "vendor")
			if err != nil {
				t.Fatal(err)
			}
			if imported.URL != test.want {
				t.Fatalf("file import: URL = %q, want %q", imported.URL, test.want)
			}
			if strings.Contains(imported.URL, "%20") {
				t.Fatalf("file import: URL = %q, must not contain a percent-encoded space", imported.URL)
			}

			descriptor, rpcErr := service.RegisterMcpServer(ctx, &turingv1.RegisterMcpServerRequest{
				Name: "vendor-register", Url: test.url, Tier: test.tier,
			})
			if rpcErr != nil {
				t.Fatalf("register: whitespace-padded url %q was refused: %v", test.url, rpcErr)
			}
			if descriptor.GetUrl() != test.want {
				t.Fatalf("register: URL = %q, want %q", descriptor.GetUrl(), test.want)
			}
			if strings.Contains(descriptor.GetUrl(), "%20") {
				t.Fatalf("register: URL = %q, must not contain a percent-encoded space", descriptor.GetUrl())
			}
		})
	}
}
