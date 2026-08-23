package mcpregistry

import (
	"context"
	"fmt"
	"strings"
	"testing"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// urlAtCanonicalLength builds a URL via buildPath (given a path suffix)
// and solves for the suffix length that makes classifyImportedURL's
// canonical result exactly targetLength bytes: it probes with a one-byte
// suffix to learn the fixed overhead (scheme/host/port and the leading
// path separator), all of which survive canonicalization unescaped, then
// computes the exact suffix length needed. This is what lets the boundary
// tests below assert the cap itself is not off-by-one, rather than merely
// "a very long URL is refused."
func urlAtCanonicalLength(t *testing.T, buildURL func(pathSuffix string) string, targetLength int) string {
	t.Helper()
	probe := buildURL("a")
	_, canonicalProbe, err := classifyImportedURL(probe)
	if err != nil {
		t.Fatalf("probe url %q was refused while computing overhead: %v", probe, err)
	}
	overhead := len(canonicalProbe) - 1
	suffixLen := targetLength - overhead
	if suffixLen < 1 {
		t.Fatalf("targetLength %d is too small for this URL shape (fixed overhead alone is %d bytes)", targetLength, overhead)
	}
	raw := buildURL(strings.Repeat("a", suffixLen))
	return raw
}

func remoteURLWithPathSuffix(suffix string) string {
	return "https://vendor.example/" + suffix
}

func localURLWithPathSuffix(suffix string) string {
	return "http://vendor:9000/" + suffix
}

// A canonical remote URL of exactly maxMCPServerURLBytes must still be
// accepted: the cap must not be off-by-one against a legitimate,
// maximally-long endpoint.
func TestClassifyImportedURLAcceptsRemoteURLAtExactCap(t *testing.T) {
	raw := urlAtCanonicalLength(t, remoteURLWithPathSuffix, maxMCPServerURLBytes)
	_, canonical, err := classifyImportedURL(raw)
	if err != nil {
		t.Fatalf("a canonical url of exactly maxMCPServerURLBytes must not be refused: %v", err)
	}
	if len(canonical) != maxMCPServerURLBytes {
		t.Fatalf("test setup is broken: canonical url is %d bytes, want exactly maxMCPServerURLBytes (%d)", len(canonical), maxMCPServerURLBytes)
	}
}

// A canonical remote URL one byte over maxMCPServerURLBytes must be
// refused, naming the limit in the reason.
func TestClassifyImportedURLRejectsRemoteURLOneByteOverCap(t *testing.T) {
	raw := urlAtCanonicalLength(t, remoteURLWithPathSuffix, maxMCPServerURLBytes+1)
	_, _, err := classifyImportedURL(raw)
	if err == nil {
		t.Fatal("want an error for a canonical url one byte over maxMCPServerURLBytes")
	}
	if !strings.Contains(err.Error(), fmt.Sprintf("%d", maxMCPServerURLBytes)) {
		t.Fatalf("err = %v, want it to name the maxMCPServerURLBytes limit", err)
	}
}

// The same exact-cap/cap+1 boundary must hold identically for a
// local-container URL, not just a remote one.
func TestClassifyImportedURLAcceptsLocalURLAtExactCap(t *testing.T) {
	raw := urlAtCanonicalLength(t, localURLWithPathSuffix, maxMCPServerURLBytes)
	_, canonical, err := classifyImportedURL(raw)
	if err != nil {
		t.Fatalf("a canonical url of exactly maxMCPServerURLBytes must not be refused: %v", err)
	}
	if len(canonical) != maxMCPServerURLBytes {
		t.Fatalf("test setup is broken: canonical url is %d bytes, want exactly maxMCPServerURLBytes (%d)", len(canonical), maxMCPServerURLBytes)
	}
}

func TestClassifyImportedURLRejectsLocalURLOneByteOverCap(t *testing.T) {
	raw := urlAtCanonicalLength(t, localURLWithPathSuffix, maxMCPServerURLBytes+1)
	_, _, err := classifyImportedURL(raw)
	if err == nil {
		t.Fatal("want an error for a canonical url one byte over maxMCPServerURLBytes")
	}
	if !strings.Contains(err.Error(), fmt.Sprintf("%d", maxMCPServerURLBytes)) {
		t.Fatalf("err = %v, want it to name the maxMCPServerURLBytes limit", err)
	}
}

// A short, ordinary endpoint (as every existing test already uses) must
// keep working — this is a coarse regression guard specific to this file,
// on top of the whole existing suite continuing to pass.
func TestClassifyImportedURLStillAcceptsOrdinaryShortEndpoints(t *testing.T) {
	if _, _, err := classifyImportedURL("https://vendor.example/mcp"); err != nil {
		t.Fatalf("an ordinary short remote url was refused: %v", err)
	}
	if _, _, err := classifyImportedURL("http://vendor:9000/mcp"); err != nil {
		t.Fatalf("an ordinary short local url was refused: %v", err)
	}
}

// The URL length cap must be enforced identically through the file-import
// path (ImportJSON) and the direct registration path (RegisterMcpServer),
// the same parity TestURLValidationParityBetweenFileImportAndRegister
// already proves for ForceQuery/port-range refusals.
func TestURLLengthCapParityBetweenFileImportAndRegister(t *testing.T) {
	for _, test := range []struct {
		name     string
		buildURL func(string) string
		tier     turingv1.McpServerTier
		repoTier repository.MCPServerTier
	}{
		{name: "remote", buildURL: remoteURLWithPathSuffix, tier: turingv1.McpServerTier_MCP_SERVER_TIER_REMOTE_URL, repoTier: repository.MCPServerTierRemoteURL},
		{name: "local", buildURL: localURLWithPathSuffix, tier: turingv1.McpServerTier_MCP_SERVER_TIER_LOCAL_CONTAINER, repoTier: repository.MCPServerTierLocalContainer},
	} {
		t.Run(test.name, func(t *testing.T) {
			oversized := urlAtCanonicalLength(t, test.buildURL, maxMCPServerURLBytes+1)

			service, repo := newRegistryTestService(t)
			ctx := context.Background()
			report, err := service.ImportJSON(ctx, []byte(`{
				"mcpServers": {
					"vendor": {"url": "`+oversized+`"}
				}
			}`))
			if err != nil {
				t.Fatal(err)
			}
			if _, refused := report.Unsupported["vendor"]; !refused {
				t.Fatalf("file import: Unsupported = %+v, want vendor refused for an oversized url", report.Unsupported)
			}
			if _, gerr := repo.GetMCPServerByName(ctx, "vendor"); gerr != repository.ErrMCPServerNotFound {
				t.Fatal("file import: an oversized url must not create a server row")
			}

			_, rpcErr := service.RegisterMcpServer(ctx, &turingv1.RegisterMcpServerRequest{
				Name: "vendor-register", Url: oversized, Tier: test.tier,
			})
			if status.Code(rpcErr) != codes.InvalidArgument {
				t.Fatalf("register: code = %v, want InvalidArgument for an oversized url", status.Code(rpcErr))
			}
		})
	}
}
