package mcpregistry

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/encoding/prototext"
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

// MCP_SERVER_TIER_BUNDLED is TuringAgent's own tier and is never something
// an operator registers into, so it is refused outright. An UNSPECIFIED
// tier is not refused: it is what a client built against the tier-less
// version of this RPC sends, and the URL's own classification stands (see
// TestRegisterMcpServerUnspecifiedTierIsDerivedFromTheURL).
func TestRegisterMcpServerRefusesBundledTier(t *testing.T) {
	service, _ := newRegistryTestService(t)
	_, err := service.RegisterMcpServer(context.Background(), &turingv1.RegisterMcpServerRequest{
		Name: "vendor",
		Url:  "https://vendor.example/mcp",
		Tier: turingv1.McpServerTier_MCP_SERVER_TIER_BUNDLED,
	})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("code = %v, want InvalidArgument for an explicitly bundled tier", status.Code(err))
	}
}

// A request that names no tier at all — every client built against the
// tier-less version of this RPC — must still register, taking the tier the
// hardened URL classifies to, for both classifications.
func TestRegisterMcpServerUnspecifiedTierIsDerivedFromTheURL(t *testing.T) {
	for _, testCase := range []struct {
		name string
		url  string
		want turingv1.McpServerTier
	}{
		{"remote", "https://vendor.example/mcp", turingv1.McpServerTier_MCP_SERVER_TIER_REMOTE_URL},
		{"local", "http://vendor-container:8080/mcp", turingv1.McpServerTier_MCP_SERVER_TIER_LOCAL_CONTAINER},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			service, _ := newRegistryTestService(t)
			descriptor, err := service.RegisterMcpServer(context.Background(), &turingv1.RegisterMcpServerRequest{
				Name: "vendor",
				Url:  testCase.url,
			})
			if err != nil {
				t.Fatal(err)
			}
			if descriptor.GetTier() != testCase.want {
				t.Fatalf("tier = %v, want %v derived from the URL", descriptor.GetTier(), testCase.want)
			}
		})
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

func TestRegisterMcpServerRefusesReservedName(t *testing.T) {
	service, _ := newRegistryTestService(t)
	// Reserved names are refused case-insensitively: "Files"/"SYSTEM"/
	// "sKiLlS"/"Integrations" all name the same first-party namespaces as
	// their lowercase forms, and mcpServerNamePattern itself accepts mixed
	// case, so without a case-insensitive check these would otherwise
	// register successfully and shadow a bundled server — or the
	// "integrations" pseudo-server that owns the github.* tools — under a
	// differently-cased name.
	for _, name := range []string{
		"system", "files", "skills", "integrations",
		"Files", "SYSTEM", "sKiLlS", "Integrations",
	} {
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
	if _, err := repo.DeleteMCPServer(context.Background(), server.Server.ID); err != nil {
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

// A mobile operator cannot edit backend files, so file reimport's own
// placeholder-adoption escape hatch (legacy_placeholder_import_test.go) is
// unreachable to them. Explicitly registering a migration-0016 placeholder
// name through this public RPC, with a valid endpoint, must adopt it in
// place instead of returning AlreadyExists and stranding them.
func TestRegisterMcpServerAdoptsLegacyPlaceholderInsteadOfAlreadyExists(t *testing.T) {
	service, repo := newRegistryTestService(t)
	ctx := context.Background()

	placeholder, err := repo.RegisterMCPServer(ctx, repository.ImportedMCPServer{
		Name: "vendor", URL: "", Tier: repository.MCPServerTierLocalContainer,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.ReplaceMCPServerTools(ctx, placeholder.Server.ID, []repository.MCPServerTool{
		{Name: "vendor.lookup", Policy: "approval_required", SchemaJSON: `{"type":"object"}`},
	}); err != nil {
		t.Fatal(err)
	}

	descriptor, err := service.RegisterMcpServer(ctx, &turingv1.RegisterMcpServerRequest{
		Name: "vendor", Url: "https://vendor.example/mcp", Tier: turingv1.McpServerTier_MCP_SERVER_TIER_REMOTE_URL,
	})
	if err != nil {
		t.Fatalf("RegisterMcpServer must adopt the placeholder rather than error: %v", err)
	}
	if descriptor.GetServerId() != placeholder.Server.ID {
		t.Fatalf("ServerId = %q, want the placeholder %q adopted in place", descriptor.GetServerId(), placeholder.Server.ID)
	}
	if descriptor.GetUrl() != "https://vendor.example/mcp" {
		t.Fatalf("Url = %q, want the registered endpoint populated", descriptor.GetUrl())
	}
	if descriptor.GetEnabled() {
		t.Fatal("adopting a placeholder must force the server disabled")
	}
	if descriptor.GetLiveness() != turingv1.McpServerLiveness_MCP_SERVER_LIVENESS_UNKNOWN {
		t.Fatalf("liveness = %v, want unknown after adoption", descriptor.GetLiveness())
	}

	tools, err := repo.ListMCPServerTools(ctx, placeholder.Server.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(tools) != 1 || tools[0].Present || tools[0].Enabled {
		t.Fatalf("tools = %+v, want the carried tool withdrawn (present=0, enabled=0)", tools)
	}
}

// A real, already-registered server (non-empty URL) must still be refused
// as AlreadyExists through the public RPC — adoption applies only to a
// url-empty placeholder.
func TestRegisterMcpServerRealExistingRowStillAlreadyExistsThroughRPC(t *testing.T) {
	service, repo := newRegistryTestService(t)
	if _, err := repo.RegisterMCPServer(context.Background(), repository.ImportedMCPServer{
		Name: "vendor", URL: "https://vendor.example/mcp", Tier: repository.MCPServerTierRemoteURL,
	}); err != nil {
		t.Fatal(err)
	}

	_, err := service.RegisterMcpServer(context.Background(), &turingv1.RegisterMcpServerRequest{
		Name: "vendor", Url: "https://vendor-two.example/mcp", Tier: turingv1.McpServerTier_MCP_SERVER_TIER_REMOTE_URL,
	})
	if status.Code(err) != codes.AlreadyExists {
		t.Fatalf("code = %v, want AlreadyExists for a real existing row", status.Code(err))
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

func TestRegisterMcpServerArrivesDisabledWithDerivedTierAndSealedToken(t *testing.T) {
	service, repo := newRegistryTestService(t)
	notifier := &countingRegistryNotifier{}
	service.SetRegistryChangeNotifier(notifier)

	descriptor, err := service.RegisterMcpServer(context.Background(), &turingv1.RegisterMcpServerRequest{
		Name: "vendor", Url: "https://vendor.example/mcp", BearerToken: "register-secret",
	})
	if err != nil {
		t.Fatal(err)
	}
	if descriptor.GetEnabled() {
		t.Fatal("a registered server must arrive disabled")
	}
	if descriptor.GetTier() != turingv1.McpServerTier_MCP_SERVER_TIER_REMOTE_URL {
		t.Fatalf("tier = %v, want remote URL derived from the URL", descriptor.GetTier())
	}
	// The descriptor has no token field by construction; assert against the
	// serialized message so a future field cannot smuggle it back.
	if rendered := prototext.Format(descriptor); strings.Contains(rendered, "register-secret") {
		t.Fatalf("response echoes the bearer token: %s", rendered)
	}
	servers, err := repo.ListMCPServers(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	vendor := findRepositoryServer(t, servers, "vendor")
	if len(vendor.SealedToken) == 0 || bytes.Contains(vendor.SealedToken, []byte("register-secret")) {
		t.Fatal("the bearer token was not stored sealed")
	}
	if notifier.calls.Load() != 1 {
		t.Fatalf("registry notifications = %d, want 1", notifier.calls.Load())
	}
}

// Ported from the pre-merge origin/main registration surface, with the
// reserved/bundled cases expecting this branch's FailedPrecondition
// (see mapMCPValidationError) rather than InvalidArgument: a reserved
// name names a precondition about who owns it, not a malformed request.
func TestRegisterMcpServerRefusesReservedAndInvalidInput(t *testing.T) {
	service, _ := newRegistryTestService(t)
	for name, testCase := range map[string]struct {
		request *turingv1.RegisterMcpServerRequest
		want    codes.Code
	}{
		"reserved name":   {&turingv1.RegisterMcpServerRequest{Name: "integrations", Url: "https://vendor.example/mcp"}, codes.FailedPrecondition},
		"bundled name":    {&turingv1.RegisterMcpServerRequest{Name: "files", Url: "https://vendor.example/mcp"}, codes.FailedPrecondition},
		"invalid name":    {&turingv1.RegisterMcpServerRequest{Name: "../escape", Url: "https://vendor.example/mcp"}, codes.InvalidArgument},
		"userinfo url":    {&turingv1.RegisterMcpServerRequest{Name: "vendor", Url: "https://user:pass@vendor.example/mcp"}, codes.InvalidArgument},
		"relative url":    {&turingv1.RegisterMcpServerRequest{Name: "vendor", Url: "/mcp"}, codes.InvalidArgument},
		"stdio-ish blank": {&turingv1.RegisterMcpServerRequest{Name: "vendor", Url: ""}, codes.InvalidArgument},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := service.RegisterMcpServer(context.Background(), testCase.request); status.Code(err) != testCase.want {
				t.Fatalf("err = %v, want %v", err, testCase.want)
			}
		})
	}
}

func TestExplicitRegisterClearsTombstoneButReimportDoesNot(t *testing.T) {
	service, repo := newRegistryTestService(t)
	document := []byte(`{"mcpServers": {"vendor": {"url": "https://vendor.example/mcp"}}}`)
	if _, err := service.ImportJSON(context.Background(), document); err != nil {
		t.Fatal(err)
	}
	servers, err := repo.ListMCPServers(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	vendor := findRepositoryServer(t, servers, "vendor")
	if _, err := repo.DeleteMCPServer(context.Background(), vendor.ID); err != nil {
		t.Fatal(err)
	}

	// File re-import must not resurrect the deletion.
	report, err := service.ImportJSON(context.Background(), document)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(report.Unsupported["vendor"], "suppressed") {
		t.Fatalf("re-import after delete = %v, want suppression", report.Unsupported)
	}

	// The user asking for the name by hand is the consent the tombstone was
	// waiting for.
	if _, err := service.RegisterMcpServer(context.Background(), &turingv1.RegisterMcpServerRequest{
		Name: "vendor", Url: "https://vendor.example/mcp",
	}); err != nil {
		t.Fatal(err)
	}
	servers, err = repo.ListMCPServers(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if findRepositoryServer(t, servers, "vendor").Enabled {
		t.Fatal("re-registered server must arrive disabled")
	}
}

func TestReimportPreservesUserDecisions(t *testing.T) {
	service, repo := newRegistryTestService(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "mcp.json")
	document := []byte(`{"mcpServers": {"vendor": {
		"url": "https://vendor.example/mcp",
		"tools": [{"name": "vendor.lookup", "inputSchema": {"type": "object"}}]
	}}}`)
	if err := os.WriteFile(path, document, 0o600); err != nil {
		t.Fatal(err)
	}
	service.SetMCPConfigRoot(filepath.Dir(path))

	first, err := service.ReimportMcpJson(context.Background(), &turingv1.ReimportMcpJsonRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if len(first.GetImported()) != 1 || first.GetImported()[0] != "vendor" {
		t.Fatalf("imported = %v, want [vendor]", first.GetImported())
	}

	servers, err := repo.ListMCPServers(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	vendor := findRepositoryServer(t, servers, "vendor")
	if err := repo.SetMCPServerEnabled(context.Background(), vendor.ID, true); err != nil {
		t.Fatal(err)
	}
	if err := repo.SetMCPToolPolicy(context.Background(), vendor.ID, "vendor.lookup", "safe"); err != nil {
		t.Fatal(err)
	}

	if _, err := service.ReimportMcpJson(context.Background(), &turingv1.ReimportMcpJsonRequest{}); err != nil {
		t.Fatal(err)
	}
	servers, err = repo.ListMCPServers(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !findRepositoryServer(t, servers, "vendor").Enabled {
		t.Fatal("re-import with an unchanged URL flipped the user's enablement")
	}
	tools, err := repo.ListMCPServerTools(context.Background(), vendor.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(tools) != 1 || tools[0].Policy != "safe" {
		t.Fatalf("tools after re-import = %+v, want the edited policy preserved", tools)
	}
}

func TestReimportReportsUnsupportedEntries(t *testing.T) {
	service, _ := newRegistryTestService(t)
	path := filepath.Join(t.TempDir(), "mcp.json")
	if err := os.WriteFile(path, []byte(`{"mcpServers": {
		"runner": {"command": "npx", "args": ["some-server"]},
		"vendor": {"url": "https://vendor.example/mcp"}
	}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	service.SetMCPConfigRoot(filepath.Dir(path))
	report, err := service.ReimportMcpJson(context.Background(), &turingv1.ReimportMcpJsonRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if len(report.GetImported()) != 1 || report.GetImported()[0] != "vendor" {
		t.Fatalf("imported = %v, want [vendor]", report.GetImported())
	}
	if len(report.GetUnsupported()) != 1 || report.GetUnsupported()[0].GetName() != "runner" ||
		!strings.Contains(report.GetUnsupported()[0].GetReason(), "stdio") {
		t.Fatalf("unsupported = %v, want the stdio refusal", report.GetUnsupported())
	}
}

func TestRotateMcpServerTokenReplacesClearsAndNeverEchoes(t *testing.T) {
	service, repo := newRegistryTestService(t)
	registered, err := service.RegisterMcpServer(context.Background(), &turingv1.RegisterMcpServerRequest{
		Name: "vendor", Url: "https://vendor.example/mcp", BearerToken: "old-secret",
	})
	if err != nil {
		t.Fatal(err)
	}
	sealedBefore := currentSealedToken(t, repo, "vendor")

	descriptor, err := service.RotateMcpServerToken(context.Background(), &turingv1.RotateMcpServerTokenRequest{
		ServerId: registered.GetServerId(), BearerToken: "new-secret",
	})
	if err != nil {
		t.Fatal(err)
	}
	if rendered := prototext.Format(descriptor); strings.Contains(rendered, "new-secret") || strings.Contains(rendered, "old-secret") {
		t.Fatalf("rotation response echoes a token: %s", rendered)
	}
	sealedAfter := currentSealedToken(t, repo, "vendor")
	if bytes.Equal(sealedBefore, sealedAfter) || len(sealedAfter) == 0 {
		t.Fatal("rotation did not replace the sealed token")
	}
	if bytes.Contains(sealedAfter, []byte("new-secret")) {
		t.Fatal("rotated token stored in plaintext")
	}

	if _, err := service.RotateMcpServerToken(context.Background(), &turingv1.RotateMcpServerTokenRequest{
		ServerId: registered.GetServerId(),
	}); err != nil {
		t.Fatal(err)
	}
	if sealed := currentSealedToken(t, repo, "vendor"); len(sealed) != 0 {
		t.Fatal("an empty rotation must clear the stored token")
	}
}

func TestRotateMcpServerTokenRefusesBundledAndUnknown(t *testing.T) {
	service, repo := newRegistryTestService(t)
	servers, err := repo.ListMCPServers(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	bundled := findRepositoryServer(t, servers, "files")
	if _, err := service.RotateMcpServerToken(context.Background(), &turingv1.RotateMcpServerTokenRequest{
		ServerId: bundled.ID, BearerToken: "nope",
	}); status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("bundled rotation err = %v, want FailedPrecondition", err)
	}
	if _, err := service.RotateMcpServerToken(context.Background(), &turingv1.RotateMcpServerTokenRequest{
		ServerId: "mcp_missing", BearerToken: "nope",
	}); status.Code(err) != codes.NotFound {
		t.Fatalf("unknown rotation err = %v, want NotFound", err)
	}
}

func currentSealedToken(t *testing.T, repo *repository.Repository, name string) []byte {
	t.Helper()
	servers, err := repo.ListMCPServers(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return findRepositoryServer(t, servers, name).SealedToken
}

func TestRegisterMcpServerRefusesAnExistingName(t *testing.T) {
	service, repo := newRegistryTestService(t)
	request := &turingv1.RegisterMcpServerRequest{
		Name: "vendor", Url: "https://vendor.example/mcp", BearerToken: "first-token",
	}
	if _, err := service.RegisterMcpServer(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	// Re-adding must not silently re-point the URL or wipe the stored token.
	again := &turingv1.RegisterMcpServerRequest{Name: "vendor", Url: "https://other.example/mcp"}
	if _, err := service.RegisterMcpServer(context.Background(), again); status.Code(err) != codes.AlreadyExists {
		t.Fatalf("err = %v, want AlreadyExists", err)
	}
	servers, err := repo.ListMCPServers(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	vendor := findRepositoryServer(t, servers, "vendor")
	if vendor.URL != "https://vendor.example/mcp" || len(vendor.SealedToken) == 0 {
		t.Fatalf("existing server mutated by refused register: url=%q sealedLen=%d", vendor.URL, len(vendor.SealedToken))
	}
}

// Register and rotate share mcp.json import's own token hygiene: a pasted
// line break is refused, and a whitespace-only token is a mistake rather
// than a request to clear the stored one (only a genuinely empty token
// clears it).
func TestRegisterAndRotateApplyImportTokenHygiene(t *testing.T) {
	service, _ := newRegistryTestService(t)
	if _, err := service.RegisterMcpServer(context.Background(), &turingv1.RegisterMcpServerRequest{
		Name: "vendor", Url: "https://vendor.example/mcp", BearerToken: "tok\nen",
	}); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("register with line break err = %v, want InvalidArgument", err)
	}
	registered, err := service.RegisterMcpServer(context.Background(), &turingv1.RegisterMcpServerRequest{
		Name: "vendor", Url: "https://vendor.example/mcp",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.RotateMcpServerToken(context.Background(), &turingv1.RotateMcpServerTokenRequest{
		ServerId: registered.GetServerId(), BearerToken: "   ",
	}); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("whitespace-only rotation err = %v, want InvalidArgument (empty means clear, blanks mean mistake)", err)
	}
	if _, err := service.RotateMcpServerToken(context.Background(), &turingv1.RotateMcpServerTokenRequest{
		ServerId: "", BearerToken: "x",
	}); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("empty server_id err = %v, want InvalidArgument", err)
	}
}

func TestRotatedTokenOpensUnderTheServerNameBinding(t *testing.T) {
	service, repo := newRegistryTestService(t)
	registered, err := service.RegisterMcpServer(context.Background(), &turingv1.RegisterMcpServerRequest{
		Name: "vendor", Url: "https://vendor.example/mcp", BearerToken: "old-secret",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.SetMCPServerStatus(context.Background(), registered.GetServerId(), "down", "401 unauthorized"); err != nil {
		t.Fatal(err)
	}
	if _, err := service.RotateMcpServerToken(context.Background(), &turingv1.RotateMcpServerTokenRequest{
		ServerId: registered.GetServerId(), BearerToken: "  new-secret  ",
	}); err != nil {
		t.Fatal(err)
	}
	// The rotated token must be openable under the same associated data the
	// dispatch path uses — the server NAME — or every later call would fail
	// with an unreadable-token error. Trimming is part of the contract too.
	opened, err := service.sealer.Open(currentSealedToken(t, repo, "vendor"), []byte("vendor"))
	if err != nil || string(opened) != "new-secret" {
		t.Fatalf("opened=%q err=%v, want the trimmed rotated secret under the name binding", opened, err)
	}
	servers, err := repo.ListMCPServers(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got := findRepositoryServer(t, servers, "vendor").Status; got != "unknown" {
		t.Fatalf("status after rotation = %q, want the stale 401 reset to unknown", got)
	}
}

// The RPC refuses legibly when no config root was ever wired, and — unlike
// the pre-merge origin/main behaviour, which treated an absent file as its
// own FailedPrecondition — a configured root whose mcp.json simply does not
// exist yet is a successful, empty re-import that clears any stale issues
// (see ReimportConfiguredJSON): a user who deleted the file is telling the
// registry there is nothing to import, not making a mistake.
func TestReimportWithoutMountedFileRefusesLegibly(t *testing.T) {
	service, repo := newRegistryTestService(t)
	if _, err := service.ReimportMcpJson(context.Background(), &turingv1.ReimportMcpJsonRequest{}); status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("err = %v, want FailedPrecondition before any config root is wired", err)
	}
	if err := repo.ReplaceMCPImportIssues(context.Background(), map[string]string{
		"stale": "a previous run's issue",
	}); err != nil {
		t.Fatal(err)
	}
	service.SetMCPConfigRoot(t.TempDir())
	response, err := service.ReimportMcpJson(context.Background(), &turingv1.ReimportMcpJsonRequest{})
	if err != nil {
		t.Fatalf("an absent mcp.json must re-import to an empty report, not an error: %v", err)
	}
	if len(response.GetImported()) != 0 || len(response.GetSkipped()) != 0 || len(response.GetUnsupported()) != 0 {
		t.Fatalf("response = %+v, want an empty report for an absent mcp.json", response)
	}
	issues, err := repo.ListMCPImportIssues(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(issues) != 0 {
		t.Fatalf("issues = %+v, want the stale issue cleared", issues)
	}
}

// A malformed document replaces the previous run's issues rather than
// leaving them standing. Unlike the pre-merge origin/main behaviour, which
// mapped this to FailedPrecondition, the whole-document refusal is reported
// through the response's own Unsupported list as the bounded "_document"
// entry (see recordDocumentRefusal), so a client renders it exactly the way
// it renders a per-entry refusal.
func TestReimportMalformedDocumentReplacesStaleIssues(t *testing.T) {
	service, repo := newRegistryTestService(t)
	root := t.TempDir()
	path := filepath.Join(root, "mcp.json")
	if err := os.WriteFile(path, []byte(`{"mcpServers": {"runner": {"command": "npx"}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	service.SetMCPConfigRoot(root)
	if _, err := service.ReimportMcpJson(context.Background(), &turingv1.ReimportMcpJsonRequest{}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{not json`), 0o600); err != nil {
		t.Fatal(err)
	}
	response, err := service.ReimportMcpJson(context.Background(), &turingv1.ReimportMcpJsonRequest{})
	if err != nil {
		t.Fatalf("a malformed document must be reported, not returned as an error: %v", err)
	}
	if len(response.GetUnsupported()) != 1 || response.GetUnsupported()[0].GetName() != "_document" {
		t.Fatalf("unsupported = %+v, want only the _document refusal", response.GetUnsupported())
	}
	issues, err := repo.ListMCPImportIssues(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(issues) != 1 || issues[0].Name != "_document" {
		t.Fatalf("issues after malformed re-import = %+v, want only the _document decode issue replacing the stale runner row", issues)
	}
}

// A bearer that is present on the wire but normalizes to nothing is a
// mistake, not a request to register without one — asserted directly on the
// register path, since TestRegisterAndRotateApplyImportTokenHygiene's own
// register case trips normalizeBearerToken's control-character branch rather
// than requireNonBlankBearerToken's blank branch.
func TestRegisterMcpServerRefusesWhitespaceOnlyBearerToken(t *testing.T) {
	service, repo := newRegistryTestService(t)
	for name, token := range map[string]string{
		"spaces": "   ",
		"tab":    "\t",
	} {
		t.Run(name, func(t *testing.T) {
			_, err := service.RegisterMcpServer(context.Background(), &turingv1.RegisterMcpServerRequest{
				Name:        "vendor",
				Url:         "https://vendor.example/mcp",
				Tier:        turingv1.McpServerTier_MCP_SERVER_TIER_REMOTE_URL,
				BearerToken: token,
			})
			if status.Code(err) != codes.InvalidArgument {
				t.Fatalf("code = %v, want InvalidArgument for a whitespace-only bearer", status.Code(err))
			}
			// Nothing was registered: a refused blank token must not leave a
			// tokenless server behind under the requested name.
			if _, err := repo.GetMCPServerByName(context.Background(), "vendor"); !errors.Is(err, repository.ErrMCPServerNotFound) {
				t.Fatalf("GetMCPServerByName err = %v, want ErrMCPServerNotFound", err)
			}
		})
	}
}
