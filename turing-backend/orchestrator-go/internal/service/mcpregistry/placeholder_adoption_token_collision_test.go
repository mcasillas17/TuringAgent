package mcpregistry

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Post-merge round-4 finding #1: a legacy migration-0016 placeholder (a
// disabled, non-bundled row with url == "") can carry tool rows — present
// or withdrawn — whose name/schema_json survived from before the registry
// existed. Both adoption paths (direct RegisterMcpServer and file
// ImportJSON) must refuse a nonempty bearer token that appears verbatim in
// any one of those retained tools' own name or exact stored schema_json,
// exactly like RotateMcpServerToken already does for an existing server's
// retained tools (see token_metadata_collision_test.go and
// tokenAppearsInRetainedToolMetadata's own doc comment) — otherwise a
// chosen token could round-trip straight back out through the very
// register/import response (or a later List) whose descriptor still
// carries the colliding placeholder tool.

// seedPlaceholderWithTool creates a migration-0016-shaped placeholder row
// (disabled, non-bundled, url == "") named name and gives it exactly one
// tool row with the requested name/schema, then — if withdrawn is true —
// runs a second, empty ReplaceMCPServerTools so the tool survives with
// present = false (withdrawn) rather than present = true, exactly the way
// RecordDiscovery's own second-round-with-nothing-named case does for an
// already-registered server (see
// TestRotateMcpServerTokenRefusesNewTokenInWithdrawnToolNameAndSchema).
func seedPlaceholderWithTool(t *testing.T, repo *repository.Repository, name, toolName, schemaJSON string, withdrawn bool) repository.MCPRegisterResult {
	t.Helper()
	ctx := context.Background()
	placeholder, err := repo.RegisterMCPServer(ctx, repository.ImportedMCPServer{
		Name: name, URL: "", Tier: repository.MCPServerTierLocalContainer,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.ReplaceMCPServerTools(ctx, placeholder.Server.ID, []repository.MCPServerTool{
		{Name: toolName, Policy: "approval_required", SchemaJSON: schemaJSON},
	}); err != nil {
		t.Fatal(err)
	}
	if withdrawn {
		if err := repo.ReplaceMCPServerTools(ctx, placeholder.Server.ID, nil); err != nil {
			t.Fatal(err)
		}
	}
	return placeholder
}

func TestRegisterMcpServerRefusesTokenMatchingRetainedPlaceholderToolName(t *testing.T) {
	service, repo := newRegistryTestService(t)
	ctx := context.Background()
	placeholder := seedPlaceholderWithTool(t, repo, "vendor", "vendor.register-secret-tool", `{"type":"object"}`, false)

	_, err := service.RegisterMcpServer(ctx, &turingv1.RegisterMcpServerRequest{
		Name: "vendor", Url: "https://vendor.example/mcp",
		Tier:        turingv1.McpServerTier_MCP_SERVER_TIER_REMOTE_URL,
		BearerToken: "vendor.register-secret-tool",
	})
	if err == nil {
		t.Fatal("a token equal to a retained placeholder tool's own name must be refused")
	}
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("code = %v, want InvalidArgument", status.Code(err))
	}
	if got := status.Convert(err).Message(); got != errMCPTokenMatchesRetainedToolMetadata.Error() {
		t.Fatalf("message = %q, want the fixed reason %q", got, errMCPTokenMatchesRetainedToolMetadata.Error())
	}
	current, getErr := repo.GetMCPServer(ctx, placeholder.Server.ID)
	if getErr != nil {
		t.Fatal(getErr)
	}
	if current.URL != "" {
		t.Fatal("a refused registration must not adopt the placeholder's row (url must stay empty)")
	}
	if len(current.SealedToken) != 0 {
		t.Fatal("a refused registration must not persist any sealed token onto the placeholder")
	}
}

func TestRegisterMcpServerRefusesTokenMatchingRetainedPlaceholderToolSchema(t *testing.T) {
	service, repo := newRegistryTestService(t)
	ctx := context.Background()
	placeholder := seedPlaceholderWithTool(t, repo, "vendor", "vendor.write",
		`{"type":"object","description":"uses key register-schema-secret internally"}`, false)

	_, err := service.RegisterMcpServer(ctx, &turingv1.RegisterMcpServerRequest{
		Name: "vendor", Url: "https://vendor.example/mcp",
		Tier:        turingv1.McpServerTier_MCP_SERVER_TIER_REMOTE_URL,
		BearerToken: "register-schema-secret",
	})
	if err == nil {
		t.Fatal("a token contained in a retained placeholder tool's own schema must be refused")
	}
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("code = %v, want InvalidArgument", status.Code(err))
	}
	if got := status.Convert(err).Message(); got != errMCPTokenMatchesRetainedToolMetadata.Error() {
		t.Fatalf("message = %q, want the fixed reason %q", got, errMCPTokenMatchesRetainedToolMetadata.Error())
	}
	current, getErr := repo.GetMCPServer(ctx, placeholder.Server.ID)
	if getErr != nil {
		t.Fatal(getErr)
	}
	if current.URL != "" {
		t.Fatal("a refused registration must not adopt the placeholder's row (url must stay empty)")
	}
}

func TestRegisterMcpServerRefusesTokenMatchingWithdrawnPlaceholderToolNameAndSchema(t *testing.T) {
	for _, test := range []struct {
		name       string
		toolName   string
		schemaJSON string
		token      string
	}{
		{
			name:       "withdrawn tool name",
			toolName:   "vendor.register-withdrawn-secret",
			schemaJSON: `{"type":"object"}`,
			token:      "vendor.register-withdrawn-secret",
		},
		{
			name:       "withdrawn tool schema",
			toolName:   "vendor.write",
			schemaJSON: `{"type":"object","description":"register-withdrawn-schema-secret"}`,
			token:      "register-withdrawn-schema-secret",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			service, repo := newRegistryTestService(t)
			ctx := context.Background()
			placeholder := seedPlaceholderWithTool(t, repo, "vendor", test.toolName, test.schemaJSON, true)

			retained, err := repo.ListMCPServerTools(ctx, placeholder.Server.ID)
			if err != nil {
				t.Fatal(err)
			}
			if len(retained) != 1 || retained[0].Present {
				t.Fatalf("retained tools = %+v, want exactly one withdrawn (present=false) row", retained)
			}

			_, err = service.RegisterMcpServer(ctx, &turingv1.RegisterMcpServerRequest{
				Name: "vendor", Url: "https://vendor.example/mcp",
				Tier:        turingv1.McpServerTier_MCP_SERVER_TIER_REMOTE_URL,
				BearerToken: test.token,
			})
			if err == nil {
				t.Fatal("a token matching a withdrawn placeholder tool's retained name/schema must be refused")
			}
			if status.Code(err) != codes.InvalidArgument {
				t.Fatalf("code = %v, want InvalidArgument", status.Code(err))
			}
			if got := status.Convert(err).Message(); got != errMCPTokenMatchesRetainedToolMetadata.Error() {
				t.Fatalf("message = %q, want the fixed reason %q", got, errMCPTokenMatchesRetainedToolMetadata.Error())
			}
			current, getErr := repo.GetMCPServer(ctx, placeholder.Server.ID)
			if getErr != nil {
				t.Fatal(getErr)
			}
			if current.URL != "" {
				t.Fatal("a refused registration must not adopt the placeholder's row (url must stay empty)")
			}
		})
	}
}

// TestRegisterMcpServerAcceptsUnrelatedTokenDespiteRetainedPlaceholderTools
// is the non-regression case: a token with no relationship at all to the
// placeholder's retained tool must still adopt the placeholder
// successfully.
func TestRegisterMcpServerAcceptsUnrelatedTokenDespiteRetainedPlaceholderTools(t *testing.T) {
	service, repo := newRegistryTestService(t)
	ctx := context.Background()
	placeholder := seedPlaceholderWithTool(t, repo, "vendor", "vendor.lookup", `{"type":"object"}`, false)

	descriptor, err := service.RegisterMcpServer(ctx, &turingv1.RegisterMcpServerRequest{
		Name: "vendor", Url: "https://vendor.example/mcp",
		Tier:        turingv1.McpServerTier_MCP_SERVER_TIER_REMOTE_URL,
		BearerToken: "totally-unrelated-token",
	})
	if err != nil {
		t.Fatalf("an unrelated token must still adopt the placeholder: %v", err)
	}
	if descriptor.GetServerId() != placeholder.Server.ID {
		t.Fatalf("Id = %q, want the placeholder %q adopted in place", descriptor.GetServerId(), placeholder.Server.ID)
	}
	current, getErr := repo.GetMCPServer(ctx, placeholder.Server.ID)
	if getErr != nil {
		t.Fatal(getErr)
	}
	if current.URL != "https://vendor.example/mcp" {
		t.Fatalf("URL = %q, want the registered endpoint adopted", current.URL)
	}
}

// TestRegisterMcpServerFreshRegistrationUnaffectedByRetainedToolCheck proves
// the new placeholder pre-check never runs (and never touches the
// repository an extra time) for a genuinely new name — there is no
// existing row at all, so nothing can be "retained".
func TestRegisterMcpServerFreshRegistrationUnaffectedByRetainedToolCheck(t *testing.T) {
	service, repo := newRegistryTestService(t)
	ctx := context.Background()

	descriptor, err := service.RegisterMcpServer(ctx, &turingv1.RegisterMcpServerRequest{
		Name: "brand-new", Url: "https://brand-new.example/mcp",
		Tier:        turingv1.McpServerTier_MCP_SERVER_TIER_REMOTE_URL,
		BearerToken: "any-token-at-all",
	})
	if err != nil {
		t.Fatalf("a fresh registration must succeed: %v", err)
	}
	if descriptor.GetName() != "brand-new" {
		t.Fatalf("Name = %q, want brand-new", descriptor.GetName())
	}
	if _, getErr := repo.GetMCPServerByName(ctx, "brand-new"); getErr != nil {
		t.Fatal(getErr)
	}
}

func TestImportJSONRefusesTokenMatchingRetainedPlaceholderToolName(t *testing.T) {
	service, repo := newRegistryTestService(t)
	ctx := context.Background()
	placeholder := seedPlaceholderWithTool(t, repo, "vendor", "vendor.import-secret-tool", `{"type":"object"}`, false)

	report, err := service.ImportJSON(ctx, []byte(`{
		"mcpServers": {
			"vendor": {
				"url": "https://vendor.example/mcp",
				"headers": {"Authorization": "Bearer vendor.import-secret-tool"}
			}
		}
	}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Imported) != 0 {
		t.Fatalf("Imported = %v, want none: a colliding token must refuse the whole entry", report.Imported)
	}
	if len(report.Skipped) != 0 {
		t.Fatalf("Skipped = %v, want none: this is an Unsupported refusal, not a create-only skip", report.Skipped)
	}
	reason, ok := report.Unsupported["vendor"]
	if !ok {
		t.Fatalf("Unsupported = %+v, want an entry for vendor", report.Unsupported)
	}
	// Unlike RegisterMcpServer's identical collision (see
	// TestRegisterMcpServerRefusesTokenMatchingRetainedPlaceholderToolName
	// above), a file import's refusal must never say why: it uses the
	// one fixed, generic mcpToolDefinitionRefusedMessage every other
	// malformed/refused mcp.json entry already uses, not the explicit
	// errMCPTokenMatchesRetainedToolMetadata reason, so an mcp.json entry
	// can never distinguish this collision from any other refusal.
	if reason != mcpToolDefinitionRefusedMessage {
		t.Fatalf("reason = %q, want the fixed generic reason %q", reason, mcpToolDefinitionRefusedMessage)
	}
	if strings.Contains(strings.ToLower(reason), "token") || strings.Contains(strings.ToLower(reason), "metadata") {
		t.Fatalf("reason = %q, must not name token/metadata", reason)
	}
	current, getErr := repo.GetMCPServer(ctx, placeholder.Server.ID)
	if getErr != nil {
		t.Fatal(getErr)
	}
	if current.URL != "" {
		t.Fatal("a refused import must not adopt the placeholder's row (url must stay empty)")
	}
	if len(current.SealedToken) != 0 {
		t.Fatal("a refused import must not persist any sealed token onto the placeholder")
	}
}

func TestImportJSONRefusesTokenMatchingRetainedPlaceholderToolSchema(t *testing.T) {
	service, repo := newRegistryTestService(t)
	ctx := context.Background()
	placeholder := seedPlaceholderWithTool(t, repo, "vendor", "vendor.write",
		`{"type":"object","description":"uses key import-schema-secret internally"}`, false)

	report, err := service.ImportJSON(ctx, []byte(`{
		"mcpServers": {
			"vendor": {
				"url": "https://vendor.example/mcp",
				"headers": {"Authorization": "Bearer import-schema-secret"}
			}
		}
	}`))
	if err != nil {
		t.Fatal(err)
	}
	reason, ok := report.Unsupported["vendor"]
	if !ok {
		t.Fatalf("Unsupported = %+v, want an entry for vendor", report.Unsupported)
	}
	if reason != mcpToolDefinitionRefusedMessage {
		t.Fatalf("reason = %q, want the fixed generic reason %q", reason, mcpToolDefinitionRefusedMessage)
	}
	if strings.Contains(strings.ToLower(reason), "token") || strings.Contains(strings.ToLower(reason), "metadata") {
		t.Fatalf("reason = %q, must not name token/metadata", reason)
	}
	current, getErr := repo.GetMCPServer(ctx, placeholder.Server.ID)
	if getErr != nil {
		t.Fatal(getErr)
	}
	if current.URL != "" {
		t.Fatal("a refused import must not adopt the placeholder's row (url must stay empty)")
	}
}

func TestImportJSONRefusesTokenMatchingWithdrawnPlaceholderToolNameAndSchema(t *testing.T) {
	for _, test := range []struct {
		name       string
		toolName   string
		schemaJSON string
		token      string
	}{
		{
			name:       "withdrawn tool name",
			toolName:   "vendor.import-withdrawn-secret",
			schemaJSON: `{"type":"object"}`,
			token:      "vendor.import-withdrawn-secret",
		},
		{
			name:       "withdrawn tool schema",
			toolName:   "vendor.write",
			schemaJSON: `{"type":"object","description":"import-withdrawn-schema-secret"}`,
			token:      "import-withdrawn-schema-secret",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			service, repo := newRegistryTestService(t)
			ctx := context.Background()
			placeholder := seedPlaceholderWithTool(t, repo, "vendor", test.toolName, test.schemaJSON, true)

			bearerHeader, err := json.Marshal("Bearer " + test.token)
			if err != nil {
				t.Fatal(err)
			}
			report, err := service.ImportJSON(ctx, []byte(`{
				"mcpServers": {
					"vendor": {
						"url": "https://vendor.example/mcp",
						"headers": {"Authorization": `+string(bearerHeader)+`}
					}
				}
			}`))
			if err != nil {
				t.Fatal(err)
			}
			reason, ok := report.Unsupported["vendor"]
			if !ok {
				t.Fatalf("Unsupported = %+v, want an entry for vendor", report.Unsupported)
			}
			if reason != mcpToolDefinitionRefusedMessage {
				t.Fatalf("reason = %q, want the fixed generic reason %q", reason, mcpToolDefinitionRefusedMessage)
			}
			if strings.Contains(strings.ToLower(reason), "token") || strings.Contains(strings.ToLower(reason), "metadata") {
				t.Fatalf("reason = %q, must not name token/metadata", reason)
			}
			current, getErr := repo.GetMCPServer(ctx, placeholder.Server.ID)
			if getErr != nil {
				t.Fatal(getErr)
			}
			if current.URL != "" {
				t.Fatal("a refused import must not adopt the placeholder's row (url must stay empty)")
			}
		})
	}
}

// TestImportJSONAcceptsCorrectedUnrelatedTokenAndAdoptsPlaceholder is the
// non-regression case: once the operator corrects the mcp.json entry to
// use a token unrelated to the placeholder's retained tool, the import
// must succeed and adopt/withdraw/reconcile exactly as it did before this
// check existed.
func TestImportJSONAcceptsCorrectedUnrelatedTokenAndAdoptsPlaceholder(t *testing.T) {
	service, repo := newRegistryTestService(t)
	ctx := context.Background()
	placeholder := seedPlaceholderWithTool(t, repo, "vendor", "vendor.lookup", `{"type":"object"}`, false)

	report, err := service.ImportJSON(ctx, []byte(`{
		"mcpServers": {
			"vendor": {
				"url": "https://vendor.example/mcp",
				"headers": {"Authorization": "Bearer totally-unrelated-token"},
				"tools": [{"name": "vendor.lookup", "inputSchema": {"type": "object"}}]
			}
		}
	}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Imported) != 1 || report.Imported[0] != "vendor" {
		t.Fatalf("Imported = %v, want [vendor]", report.Imported)
	}
	if _, present := report.Unsupported["vendor"]; present {
		t.Fatalf("Unsupported = %+v, want vendor absent", report.Unsupported)
	}
	current, getErr := repo.GetMCPServer(ctx, placeholder.Server.ID)
	if getErr != nil {
		t.Fatal(getErr)
	}
	if current.URL != "https://vendor.example/mcp" {
		t.Fatalf("URL = %q, want the imported endpoint adopted", current.URL)
	}
	if len(current.SealedToken) == 0 {
		t.Fatal("SealedToken is empty, want the corrected token sealed and stored")
	}
	tools, err := repo.ListMCPServerTools(ctx, placeholder.Server.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(tools) != 1 || !tools[0].Present {
		t.Fatalf("tools = %+v, want the reconfirmed tool present", tools)
	}
}

// TestRegisterMcpServerRetainedToolLookupFailureReturnsInternal proves the
// placeholder pre-check's own repository read failure — distinct from the
// token collision it normally detects — is mapped to the same generic
// Internal status every other unexpected repository failure in this RPC
// already uses, rather than leaking a raw driver/SQL error.
// GetMCPServerByName's own lookup (against mcp_servers) still succeeds
// here; only the second read, ListMCPServerTools (against the tools
// table, dropped below), fails, so this specifically exercises
// placeholderAdoptionTokenCollision's own error path, not the disposition
// lookup above it.
func TestRegisterMcpServerRetainedToolLookupFailureReturnsInternal(t *testing.T) {
	service, repo, database := newRegistryTestServiceWithRealAudit(t)
	ctx := context.Background()
	placeholder := seedPlaceholderWithTool(t, repo, "vendor", "vendor.lookup", `{"type":"object"}`, false)

	if _, err := database.ExecContext(ctx, `DROP TABLE tools`); err != nil {
		t.Fatal(err)
	}

	_, err := service.RegisterMcpServer(ctx, &turingv1.RegisterMcpServerRequest{
		Name: "vendor", Url: "https://vendor.example/mcp",
		Tier:        turingv1.McpServerTier_MCP_SERVER_TIER_REMOTE_URL,
		BearerToken: "any-token-at-all",
	})
	if err == nil {
		t.Fatal("want an error once the retained-tools read fails")
	}
	if status.Code(err) != codes.Internal {
		t.Fatalf("code = %v, want Internal", status.Code(err))
	}
	if got := status.Convert(err).Message(); got != "read MCP server failed" {
		t.Fatalf("message = %q, want the fixed generic reason, not a raw driver error", got)
	}
	_ = placeholder
}

// TestImportJSONRetainedToolLookupFailureReturnsErrorWithoutMutation proves
// the same repository-read-failure path for ImportJSON's placeholder
// branch: a failure reading retained tools must not adopt the placeholder,
// and must surface as an error from ImportJSON itself (which
// ReimportMcpJson, its own only caller, maps to a generic Internal status)
// rather than being silently swallowed into an ordinary Unsupported entry.
func TestImportJSONRetainedToolLookupFailureReturnsErrorWithoutMutation(t *testing.T) {
	service, repo, database := newRegistryTestServiceWithRealAudit(t)
	ctx := context.Background()
	placeholder := seedPlaceholderWithTool(t, repo, "vendor", "vendor.lookup", `{"type":"object"}`, false)

	if _, err := database.ExecContext(ctx, `DROP TABLE tools`); err != nil {
		t.Fatal(err)
	}

	_, err := service.ImportJSON(ctx, []byte(`{
		"mcpServers": {
			"vendor": {
				"url": "https://vendor.example/mcp",
				"headers": {"Authorization": "Bearer any-token-at-all"}
			}
		}
	}`))
	if err == nil {
		t.Fatal("want an error once the retained-tools read fails")
	}
	current, getErr := repo.GetMCPServer(ctx, placeholder.Server.ID)
	if getErr != nil {
		t.Fatal(getErr)
	}
	if current.URL != "" {
		t.Fatal("a retained-tools lookup failure must not adopt the placeholder's row")
	}
}

// TestRegisterMcpServerRefusalRecordsNoAuditOrNotification proves the
// refusal happens strictly before any notify/audit call, mirroring
// RotateMcpServerToken's own placement of its retained-tool check before
// sealing/persisting.
func TestRegisterMcpServerRefusalRecordsNoAuditOrNotification(t *testing.T) {
	service, repo, database := newRegistryTestServiceWithRealAudit(t)
	ctx := context.Background()
	seedPlaceholderWithTool(t, repo, "vendor", "vendor.register-secret-tool", `{"type":"object"}`, false)

	_, err := service.RegisterMcpServer(ctx, &turingv1.RegisterMcpServerRequest{
		Name: "vendor", Url: "https://vendor.example/mcp",
		Tier:        turingv1.McpServerTier_MCP_SERVER_TIER_REMOTE_URL,
		BearerToken: "vendor.register-secret-tool",
	})
	if err == nil {
		t.Fatal("want the registration refused")
	}
	if payloads := auditPayloadsForAction(t, database, "mcp.server.registered"); len(payloads) != 0 {
		t.Fatalf("audit payloads = %+v, want none for a refused registration", payloads)
	}
}
