package mcpregistry

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"
)

// countingRoundTripper fails every request and counts how many it saw, so a
// test can assert a code path made zero HTTP calls rather than merely that
// it didn't observe a particular one.
type countingRoundTripper struct {
	calls int
}

func (c *countingRoundTripper) RoundTrip(*http.Request) (*http.Response, error) {
	c.calls++
	return nil, errors.New("unexpected HTTP call")
}

func TestRegisterMcpServerDoesNotContactTheEndpoint(t *testing.T) {
	service, _ := newRegistryTestService(t)
	transport := &countingRoundTripper{}
	service.httpClient = &http.Client{Transport: transport}

	descriptor, err := service.RegisterMcpServer(context.Background(), &turingv1.RegisterMcpServerRequest{
		Name: "vendor",
		Url:  "https://vendor.example/mcp",
		Tier: turingv1.McpServerTier_MCP_SERVER_TIER_REMOTE_URL,
	})
	if err != nil {
		t.Fatal(err)
	}
	if transport.calls != 0 {
		t.Fatalf("RegisterMcpServer made %d HTTP call(s), want 0", transport.calls)
	}
	if descriptor.GetEnabled() {
		t.Fatal("a freshly registered server must be disabled")
	}
	if descriptor.GetLiveness() != turingv1.McpServerLiveness_MCP_SERVER_LIVENESS_UNKNOWN {
		t.Fatalf("liveness = %v, want unknown", descriptor.GetLiveness())
	}
	if len(descriptor.GetTools()) != 0 {
		t.Fatalf("tools = %v, want none before any liveness contact", descriptor.GetTools())
	}
}

func TestRegisterMcpServerRequiresExplicitTier(t *testing.T) {
	service, _ := newRegistryTestService(t)
	for _, tier := range []turingv1.McpServerTier{
		turingv1.McpServerTier_MCP_SERVER_TIER_UNSPECIFIED,
		turingv1.McpServerTier_MCP_SERVER_TIER_BUNDLED,
	} {
		_, err := service.RegisterMcpServer(context.Background(), &turingv1.RegisterMcpServerRequest{
			Name: "vendor",
			Url:  "https://vendor.example/mcp",
			Tier: tier,
		})
		if status.Code(err) != codes.InvalidArgument {
			t.Fatalf("tier %v: code = %v, want InvalidArgument", tier, status.Code(err))
		}
	}
}

func TestRegisterMcpServerRequestedTierMustMatchURLClassification(t *testing.T) {
	service, _ := newRegistryTestService(t)
	_, err := service.RegisterMcpServer(context.Background(), &turingv1.RegisterMcpServerRequest{
		Name: "vendor",
		Url:  "https://vendor.example/mcp",
		Tier: turingv1.McpServerTier_MCP_SERVER_TIER_LOCAL_CONTAINER,
	})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("code = %v, want InvalidArgument for a tier/URL mismatch", status.Code(err))
	}
}

func TestRegisterMcpServerRefusesStdioShapedValue(t *testing.T) {
	service, _ := newRegistryTestService(t)
	_, err := service.RegisterMcpServer(context.Background(), &turingv1.RegisterMcpServerRequest{
		Name: "vendor",
		Url:  "npx vendor",
		Tier: turingv1.McpServerTier_MCP_SERVER_TIER_LOCAL_CONTAINER,
	})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("code = %v, want InvalidArgument for a stdio-shaped value", status.Code(err))
	}
}

func TestRegisterMcpServerRefusesInvalidName(t *testing.T) {
	service, _ := newRegistryTestService(t)
	_, err := service.RegisterMcpServer(context.Background(), &turingv1.RegisterMcpServerRequest{
		Name: "not a valid name!",
		Url:  "https://vendor.example/mcp",
		Tier: turingv1.McpServerTier_MCP_SERVER_TIER_REMOTE_URL,
	})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("code = %v, want InvalidArgument for an invalid name", status.Code(err))
	}
}

func TestRegisterMcpServerRefusesReservedBundledName(t *testing.T) {
	service, _ := newRegistryTestService(t)
	for _, name := range []string{"system", "files", "skills"} {
		_, err := service.RegisterMcpServer(context.Background(), &turingv1.RegisterMcpServerRequest{
			Name: name,
			Url:  "https://vendor.example/mcp",
			Tier: turingv1.McpServerTier_MCP_SERVER_TIER_REMOTE_URL,
		})
		if status.Code(err) != codes.FailedPrecondition {
			t.Fatalf("name %q: code = %v, want FailedPrecondition", name, status.Code(err))
		}
	}
}

func TestRegisterMcpServerRefusesBundledCollisionFromRepository(t *testing.T) {
	service, repo := newRegistryTestService(t)
	// A bundled row that does not go through the reserved-name list (the
	// repository is the second line of defense against a bundled-name
	// collision).
	if err := seedBundledMCPServer(t, repo, "bundled-vendor"); err != nil {
		t.Fatal(err)
	}
	_, err := service.RegisterMcpServer(context.Background(), &turingv1.RegisterMcpServerRequest{
		Name: "bundled-vendor",
		Url:  "https://vendor.example/mcp",
		Tier: turingv1.McpServerTier_MCP_SERVER_TIER_REMOTE_URL,
	})
	if status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("code = %v, want FailedPrecondition for a bundled name collision", status.Code(err))
	}
}

func TestRegisterMcpServerMissingKeyWithTokenFailsPrecondition(t *testing.T) {
	service, _ := newRegistryTestService(t)
	service.sealer = nil
	_, err := service.RegisterMcpServer(context.Background(), &turingv1.RegisterMcpServerRequest{
		Name:        "vendor",
		Url:         "https://vendor.example/mcp",
		Tier:        turingv1.McpServerTier_MCP_SERVER_TIER_REMOTE_URL,
		BearerToken: "vendor-secret-token",
	})
	if status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("code = %v, want FailedPrecondition when a token is given without a key", status.Code(err))
	}
	if !strings.Contains(err.Error(), "TURING_INTEGRATION_KEY") {
		t.Fatalf("error = %v, want it to name the missing key", err)
	}
}

func TestRegisterMcpServerNoTokenNeedsNoSealer(t *testing.T) {
	service, _ := newRegistryTestService(t)
	service.sealer = nil
	descriptor, err := service.RegisterMcpServer(context.Background(), &turingv1.RegisterMcpServerRequest{
		Name: "vendor",
		Url:  "https://vendor.example/mcp",
		Tier: turingv1.McpServerTier_MCP_SERVER_TIER_REMOTE_URL,
	})
	if err != nil {
		t.Fatalf("RegisterMcpServer without a token should not require a sealer: %v", err)
	}
	if descriptor == nil {
		t.Fatal("descriptor is nil")
	}
}

func TestRegisterMcpServerClearsMatchingTombstone(t *testing.T) {
	service, repo := newRegistryTestService(t)
	server, err := repo.RegisterMCPServer(context.Background(), repository.ImportedMCPServer{
		Name: "vendor", URL: "https://vendor.example/mcp", Tier: repository.MCPServerTierRemoteURL,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.DeleteMCPServer(context.Background(), server.ID); err != nil {
		t.Fatal(err)
	}
	tombstoned, err := repo.MCPServerTombstoned(context.Background(), "vendor")
	if err != nil {
		t.Fatal(err)
	}
	if !tombstoned {
		t.Fatal("expected the deleted server's name to be tombstoned")
	}

	if _, err := service.RegisterMcpServer(context.Background(), &turingv1.RegisterMcpServerRequest{
		Name: "vendor",
		Url:  "https://vendor.example/mcp",
		Tier: turingv1.McpServerTier_MCP_SERVER_TIER_REMOTE_URL,
	}); err != nil {
		t.Fatalf("RegisterMcpServer failed to re-register a tombstoned name: %v", err)
	}

	tombstoned, err = repo.MCPServerTombstoned(context.Background(), "vendor")
	if err != nil {
		t.Fatal(err)
	}
	if tombstoned {
		t.Fatal("explicit registration must clear the matching tombstone")
	}
}

func TestRegisterMcpServerExistingNameIsAlreadyExists(t *testing.T) {
	service, repo := newRegistryTestService(t)
	if _, err := repo.RegisterMCPServer(context.Background(), repository.ImportedMCPServer{
		Name: "vendor", URL: "https://vendor.example/mcp", Tier: repository.MCPServerTierRemoteURL,
	}); err != nil {
		t.Fatal(err)
	}

	_, err := service.RegisterMcpServer(context.Background(), &turingv1.RegisterMcpServerRequest{
		Name: "vendor",
		Url:  "https://vendor-two.example/mcp",
		Tier: turingv1.McpServerTier_MCP_SERVER_TIER_REMOTE_URL,
	})
	if status.Code(err) != codes.AlreadyExists {
		t.Fatalf("code = %v, want AlreadyExists for a name already registered", status.Code(err))
	}
}

func TestRegisterMcpServerResponseNeverIncludesTokenOrCiphertext(t *testing.T) {
	service, _ := newRegistryTestService(t)
	const token = "vendor-secret-should-never-be-returned"
	descriptor, err := service.RegisterMcpServer(context.Background(), &turingv1.RegisterMcpServerRequest{
		Name:        "vendor",
		Url:         "https://vendor.example/mcp",
		Tier:        turingv1.McpServerTier_MCP_SERVER_TIER_REMOTE_URL,
		BearerToken: token,
	})
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := protojson.Marshal(descriptor)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), token) {
		t.Fatalf("descriptor carries the bearer token: %s", encoded)
	}
}

func TestRegisterMcpServerNotifiesRegistryChange(t *testing.T) {
	service, _ := newRegistryTestService(t)
	notifier := &countingRegistryChangeNotifier{}
	service.SetRegistryChangeNotifier(notifier)

	if _, err := service.RegisterMcpServer(context.Background(), &turingv1.RegisterMcpServerRequest{
		Name: "vendor",
		Url:  "https://vendor.example/mcp",
		Tier: turingv1.McpServerTier_MCP_SERVER_TIER_REMOTE_URL,
	}); err != nil {
		t.Fatal(err)
	}
	if notifier.calls != 1 {
		t.Fatalf("notify calls = %d, want 1 after a successful registration", notifier.calls)
	}
}

func TestRegisterMcpServerDoesNotNotifyOnFailure(t *testing.T) {
	service, _ := newRegistryTestService(t)
	notifier := &countingRegistryChangeNotifier{}
	service.SetRegistryChangeNotifier(notifier)

	if _, err := service.RegisterMcpServer(context.Background(), &turingv1.RegisterMcpServerRequest{
		Name: "not a valid name!",
		Url:  "https://vendor.example/mcp",
		Tier: turingv1.McpServerTier_MCP_SERVER_TIER_REMOTE_URL,
	}); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("code = %v, want InvalidArgument", status.Code(err))
	}
	if notifier.calls != 0 {
		t.Fatalf("notify calls = %d, want 0 after a failed registration", notifier.calls)
	}
}

// RegisterMcpServer has no pre-existing row to corrupt the way the rotate
// equivalent test does (the server doesn't exist until the repository
// mutation itself creates it), so this uses the same mechanism as
// TestAuditContextIsDetachedFromClientCancellationAfterCommit — a registry
// change notifier that cancels the caller's own context — but, unlike that
// test, asserts on the RPC's outcome: with the fix, notify+audit already
// happened before the notifier cancels ctx, so the *later* serverDescriptor
// call observes a cancelled context and deterministically fails, and the
// RPC must still report that the mutation, notification, and audit all
// happened. Reverting the reorder (descriptor built before notify/audit)
// would make serverDescriptor run first, while ctx is still live, so it
// would succeed and the RPC would return no error at all — the opposite of
// what this test requires — which is what makes this discriminating.
func TestRegisterMcpServerNotifiesAndAuditsBeforeADescriptorFailure(t *testing.T) {
	service, repo := newRegistryTestService(t)
	ctx, cancel := context.WithCancel(context.Background())
	service.SetRegistryChangeNotifier(&cancelingRegistryChangeNotifier{cancel: cancel})
	recorder := &recordingAuditRecorder{}
	service.SetAuditRecorder(recorder)

	_, err := service.RegisterMcpServer(ctx, &turingv1.RegisterMcpServerRequest{
		Name: "vendor", Url: "https://vendor.example/mcp", Tier: turingv1.McpServerTier_MCP_SERVER_TIER_REMOTE_URL,
	})
	if status.Code(err) != codes.Internal {
		t.Fatalf("code = %v, want Internal from serverDescriptor observing the notifier's own context cancellation", status.Code(err))
	}
	if ctx.Err() == nil {
		t.Fatal("test setup failed: the notifier never cancelled the original request context")
	}

	stored, err := repo.GetMCPServerByName(context.Background(), "vendor")
	if err != nil {
		t.Fatalf("the repository mutation must have committed despite the later descriptor failure: %v", err)
	}
	if stored.Name != "vendor" {
		t.Fatalf("stored server = %+v, want name=vendor", stored)
	}
	if len(recorder.records) != 1 {
		t.Fatalf("records = %+v, want one audit row despite the later descriptor failure", recorder.records)
	}
	if recorder.records[0].action != "mcp.server.registered" {
		t.Fatalf("action = %q, want mcp.server.registered", recorder.records[0].action)
	}
}

// seedBundledMCPServer inserts a bundled-tier row directly, bypassing the
// reserved-name list, so tests can exercise the repository's own
// bundled-collision refusal independently of the service-level reserved
// name check.
func seedBundledMCPServer(t *testing.T, repo *repository.Repository, name string) error {
	t.Helper()
	_, err := repo.ImportMCPServer(context.Background(), repository.ImportedMCPServer{
		Name: name, URL: "https://bundled.example/mcp", Tier: repository.MCPServerTierBundled,
	})
	return err
}
