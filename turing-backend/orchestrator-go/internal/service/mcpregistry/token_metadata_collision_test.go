package mcpregistry

import (
	"bytes"
	"context"
	"encoding/json"
	"log"
	"testing"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/db"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/secretbox"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/service/audit"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// TestRegisterMcpServerRefusesTokenEqualToServerName proves the shared
// register/import validation (validateServerDefinition) refuses a bearer
// token that appears verbatim in the server's own name: a name is public
// metadata (returned in every list/response, recorded in every audit row,
// visible in the Flutter MCPs page), so a token equal to it can never
// actually be secret regardless of how carefully it is sealed.
func TestRegisterMcpServerRefusesTokenEqualToServerName(t *testing.T) {
	service, repo := newRegistryTestService(t)
	ctx := context.Background()

	_, err := service.RegisterMcpServer(ctx, &turingv1.RegisterMcpServerRequest{
		Name: "my-secret-token", Url: "https://vendor.example/mcp",
		Tier:        turingv1.McpServerTier_MCP_SERVER_TIER_REMOTE_URL,
		BearerToken: "my-secret-token",
	})
	if err == nil {
		t.Fatal("a token equal to the server's own name must be refused")
	}
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("code = %v, want InvalidArgument", status.Code(err))
	}
	// Asserted exactly, not just the status code: InvalidArgument alone
	// would not distinguish this refusal from an unrelated URL/name
	// validation failure that also maps to InvalidArgument (see
	// mapMCPValidationError) — a future change to classifyImportedURL
	// (e.g. one that started rejecting quote/backslash characters in a
	// URL) could otherwise silently mask this check never having run at
	// all while this test kept passing.
	if got := status.Convert(err).Message(); got != errMCPTokenMatchesPublicMetadata.Error() {
		t.Fatalf("message = %q, want the fixed reason %q", got, errMCPTokenMatchesPublicMetadata.Error())
	}
	if _, lookupErr := repo.GetMCPServerByName(ctx, "my-secret-token"); lookupErr != repository.ErrMCPServerNotFound {
		t.Fatalf("err = %v, want ErrMCPServerNotFound: a refused registration must create no row", lookupErr)
	}
}

// TestRegisterMcpServerRefusesTokenInURLPath proves the same check catches
// a token appearing verbatim in the server's canonical URL, not just its
// name — a canonical URL is exactly as public (returned in the register
// response and the register audit payload) as the name is.
func TestRegisterMcpServerRefusesTokenInURLPath(t *testing.T) {
	service, repo := newRegistryTestService(t)
	ctx := context.Background()

	_, err := service.RegisterMcpServer(ctx, &turingv1.RegisterMcpServerRequest{
		Name: "vendor", Url: "https://vendor.example/mcp/my-path-token",
		Tier:        turingv1.McpServerTier_MCP_SERVER_TIER_REMOTE_URL,
		BearerToken: "my-path-token",
	})
	if err == nil {
		t.Fatal("a token appearing verbatim in the URL path must be refused")
	}
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("code = %v, want InvalidArgument", status.Code(err))
	}
	if got := status.Convert(err).Message(); got != errMCPTokenMatchesPublicMetadata.Error() {
		t.Fatalf("message = %q, want the fixed reason %q", got, errMCPTokenMatchesPublicMetadata.Error())
	}
	if _, lookupErr := repo.GetMCPServerByName(ctx, "vendor"); lookupErr != repository.ErrMCPServerNotFound {
		t.Fatalf("err = %v, want ErrMCPServerNotFound: a refused registration must create no row", lookupErr)
	}
}

// TestRegisterMcpServerRefusesTokenRequiringDecodedURLPathComparison
// proves the check compares against the URL path's *decoded*
// representation, not only the raw canonical (percent-encoded) string: a
// token containing a quote or backslash is re-encoded by url.URL.String()
// (`"` becomes `%22`, `\` becomes `%5C`), so a substring search against
// only the raw canonical string would never find it there even though the
// URL, once decoded, still names it verbatim.
func TestRegisterMcpServerRefusesTokenRequiringDecodedURLPathComparison(t *testing.T) {
	service, repo := newRegistryTestService(t)
	ctx := context.Background()

	const token = `sec"ret\token`
	_, err := service.RegisterMcpServer(ctx, &turingv1.RegisterMcpServerRequest{
		Name: "vendor", Url: `https://vendor.example/mcp/` + token,
		Tier:        turingv1.McpServerTier_MCP_SERVER_TIER_REMOTE_URL,
		BearerToken: token,
	})
	if err == nil {
		t.Fatal("a token requiring decoded-path comparison (quote/backslash) must still be refused")
	}
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("code = %v, want InvalidArgument", status.Code(err))
	}
	if got := status.Convert(err).Message(); got != errMCPTokenMatchesPublicMetadata.Error() {
		t.Fatalf("message = %q, want the fixed reason %q", got, errMCPTokenMatchesPublicMetadata.Error())
	}
	if _, lookupErr := repo.GetMCPServerByName(ctx, "vendor"); lookupErr != repository.ErrMCPServerNotFound {
		t.Fatalf("err = %v, want ErrMCPServerNotFound: a refused registration must create no row", lookupErr)
	}
}

// TestRegisterMcpServerAcceptsUnrelatedToken is the non-regression case:
// a token with no relationship at all to the server's name or URL must
// still register successfully.
func TestRegisterMcpServerAcceptsUnrelatedToken(t *testing.T) {
	service, _ := newRegistryTestService(t)
	ctx := context.Background()

	descriptor, err := service.RegisterMcpServer(ctx, &turingv1.RegisterMcpServerRequest{
		Name: "vendor", Url: "https://vendor.example/mcp",
		Tier:        turingv1.McpServerTier_MCP_SERVER_TIER_REMOTE_URL,
		BearerToken: "completely-unrelated-bearer-value",
	})
	if err != nil {
		t.Fatalf("an unrelated token must be accepted: %v", err)
	}
	if descriptor.GetName() != "vendor" {
		t.Fatalf("descriptor = %+v, want the server actually registered", descriptor)
	}
}

// TestImportJSONRefusesTokenEqualToServerName proves the same check
// applies to the file-import path too, since ImportJSON calls the same
// validateServerDefinition RegisterMcpServer does.
func TestImportJSONRefusesTokenEqualToServerName(t *testing.T) {
	service, repo := newRegistryTestService(t)
	ctx := context.Background()

	report, err := service.ImportJSON(ctx, []byte(`{
		"mcpServers": {
			"vendor": {
				"url": "https://other-host.example/mcp",
				"headers": {"Authorization": "Bearer vendor"}
			}
		}
	}`))
	if err != nil {
		t.Fatal(err)
	}
	reason, refused := report.Unsupported["vendor"]
	if !refused {
		t.Fatalf("Unsupported = %+v, want vendor refused for a token equal to its own name", report.Unsupported)
	}
	if reason != errMCPTokenMatchesPublicMetadata.Error() {
		t.Fatalf("reason = %q, want the fixed generic reason %q", reason, errMCPTokenMatchesPublicMetadata.Error())
	}
	if _, lookupErr := repo.GetMCPServerByName(ctx, "vendor"); lookupErr != repository.ErrMCPServerNotFound {
		t.Fatalf("err = %v, want ErrMCPServerNotFound", lookupErr)
	}
}

// TestRotateMcpServerTokenRefusesNewTokenEqualToCurrentServerNameOrURL
// proves rotation runs its own, symmetric check: a *new* token compared
// against the *existing* server row's own name/url (there is no new
// name/url being set during a rotation, only a new token), refused the
// same generic InvalidArgument way.
func TestRotateMcpServerTokenRefusesNewTokenEqualToCurrentServerNameOrURL(t *testing.T) {
	service, repo := newRegistryTestService(t)
	ctx := context.Background()
	server, err := repo.RegisterMCPServer(ctx, repository.ImportedMCPServer{
		Name: "vendor", URL: "https://other-host.example/mcp", Tier: repository.MCPServerTierRemoteURL,
	})
	if err != nil {
		t.Fatal(err)
	}

	for _, test := range []struct {
		name  string
		token string
	}{
		{name: "equal to server name", token: "vendor"},
		{name: "contained in server url", token: "other-host"},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := service.RotateMcpServerToken(ctx, &turingv1.RotateMcpServerTokenRequest{
				ServerId: server.Server.ID, BearerToken: test.token,
			})
			if err == nil {
				t.Fatal("a new token matching the current server's own name/url must be refused")
			}
			if status.Code(err) != codes.InvalidArgument {
				t.Fatalf("code = %v, want InvalidArgument", status.Code(err))
			}
			// The rotation must not have taken effect: the server still
			// carries no token.
			current, getErr := repo.GetMCPServer(ctx, server.Server.ID)
			if getErr != nil {
				t.Fatal(getErr)
			}
			if len(current.SealedToken) != 0 {
				t.Fatal("a refused rotation must not replace the server's sealed token")
			}
		})
	}
}

// TestRotateMcpServerTokenRefusesNewTokenEqualToPresentToolName proves
// finding #2's core requirement: before accepting a new token, rotation
// also loads every tool retained for the server and refuses a token that
// appears verbatim in a *tool's* own name, not just the server's own
// name/url (TestRotateMcpServerTokenRefusesNewTokenEqualToCurrentServerNameOrURL
// above). A tool descriptor's name is exactly as public as the server's
// own name/url: it is returned in every list/register/rotate response and
// recorded in every audit row for that server, so a token recoverable from
// it can never actually be secret.
func TestRotateMcpServerTokenRefusesNewTokenEqualToPresentToolName(t *testing.T) {
	service, repo := newRegistryTestService(t)
	ctx := context.Background()
	server, err := repo.RegisterMCPServer(ctx, repository.ImportedMCPServer{
		Name: "vendor", URL: "https://vendor.example/mcp", Tier: repository.MCPServerTierRemoteURL,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := service.RecordDiscovery(ctx, server.Server.ID, []DiscoveredTool{
		{Name: "vendor.rotate-secret-tool", SchemaJSON: `{"type":"object"}`},
	}); err != nil {
		t.Fatal(err)
	}

	_, err = service.RotateMcpServerToken(ctx, &turingv1.RotateMcpServerTokenRequest{
		ServerId: server.Server.ID, BearerToken: "vendor.rotate-secret-tool",
	})
	if err == nil {
		t.Fatal("a new token equal to a present tool's own name must be refused")
	}
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("code = %v, want InvalidArgument", status.Code(err))
	}
	if got := status.Convert(err).Message(); got != errMCPTokenMatchesRetainedToolMetadata.Error() {
		t.Fatalf("message = %q, want the fixed reason %q", got, errMCPTokenMatchesRetainedToolMetadata.Error())
	}
	current, getErr := repo.GetMCPServer(ctx, server.Server.ID)
	if getErr != nil {
		t.Fatal(getErr)
	}
	if len(current.SealedToken) != 0 {
		t.Fatal("a refused rotation must not replace the server's sealed token")
	}
}

// TestRotateMcpServerTokenRefusesNewTokenContainedInPresentToolSchema
// proves the same check reaches a tool's stored schema_json, not only its
// name: an ordinary string value nested inside the schema.
func TestRotateMcpServerTokenRefusesNewTokenContainedInPresentToolSchema(t *testing.T) {
	service, repo := newRegistryTestService(t)
	ctx := context.Background()
	server, err := repo.RegisterMCPServer(ctx, repository.ImportedMCPServer{
		Name: "vendor", URL: "https://vendor.example/mcp", Tier: repository.MCPServerTierRemoteURL,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := service.RecordDiscovery(ctx, server.Server.ID, []DiscoveredTool{
		{Name: "vendor.write", SchemaJSON: `{"type":"object","description":"uses key rotate-schema-secret internally"}`},
	}); err != nil {
		t.Fatal(err)
	}

	_, err = service.RotateMcpServerToken(ctx, &turingv1.RotateMcpServerTokenRequest{
		ServerId: server.Server.ID, BearerToken: "rotate-schema-secret",
	})
	if err == nil {
		t.Fatal("a new token contained in a present tool's own schema must be refused")
	}
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("code = %v, want InvalidArgument", status.Code(err))
	}
	if got := status.Convert(err).Message(); got != errMCPTokenMatchesRetainedToolMetadata.Error() {
		t.Fatalf("message = %q, want the fixed reason %q", got, errMCPTokenMatchesRetainedToolMetadata.Error())
	}
	current, getErr := repo.GetMCPServer(ctx, server.Server.ID)
	if getErr != nil {
		t.Fatal(getErr)
	}
	if len(current.SealedToken) != 0 {
		t.Fatal("a refused rotation must not replace the server's sealed token")
	}
}

// TestRotateMcpServerTokenRefusesNewTokenInWithdrawnToolNameAndSchema
// proves the check covers *withdrawn* (present = 0) tools too, not only
// currently-present ones: re-discovering without a tool marks it withdrawn
// but retains its row (name and schema_json unchanged), and
// ListMcpServers/serverDescriptor still returns that withdrawn tool's own
// name/schema in every server descriptor (see buildServerDescriptor /
// ListMCPServerTools, which never filters by present) — exactly as public
// as a present tool's metadata, so a token hiding in either must be
// refused the same way.
func TestRotateMcpServerTokenRefusesNewTokenInWithdrawnToolNameAndSchema(t *testing.T) {
	for _, test := range []struct {
		name  string
		tools []DiscoveredTool
		token string
	}{
		{
			name:  "withdrawn tool name",
			tools: []DiscoveredTool{{Name: "vendor.withdrawn-secret-tool", SchemaJSON: `{"type":"object"}`}},
			token: "vendor.withdrawn-secret-tool",
		},
		{
			name:  "withdrawn tool schema",
			tools: []DiscoveredTool{{Name: "vendor.write", SchemaJSON: `{"type":"object","description":"withdrawn-schema-secret"}`}},
			token: "withdrawn-schema-secret",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			service, repo := newRegistryTestService(t)
			ctx := context.Background()
			server, err := repo.RegisterMCPServer(ctx, repository.ImportedMCPServer{
				Name: "vendor", URL: "https://vendor.example/mcp", Tier: repository.MCPServerTierRemoteURL,
			})
			if err != nil {
				t.Fatal(err)
			}
			if err := service.RecordDiscovery(ctx, server.Server.ID, test.tools); err != nil {
				t.Fatal(err)
			}
			// A second discovery naming no tools at all withdraws every
			// tool from the first round (present -> 0) without deleting
			// its row: name and schema_json survive unchanged.
			if err := service.RecordDiscovery(ctx, server.Server.ID, nil); err != nil {
				t.Fatal(err)
			}
			retained, err := repo.ListMCPServerTools(ctx, server.Server.ID)
			if err != nil {
				t.Fatal(err)
			}
			if len(retained) != 1 || retained[0].Present {
				t.Fatalf("retained tools = %+v, want exactly one withdrawn (present=false) row", retained)
			}

			_, err = service.RotateMcpServerToken(ctx, &turingv1.RotateMcpServerTokenRequest{
				ServerId: server.Server.ID, BearerToken: test.token,
			})
			if err == nil {
				t.Fatal("a new token matching a withdrawn tool's retained name/schema must be refused")
			}
			if status.Code(err) != codes.InvalidArgument {
				t.Fatalf("code = %v, want InvalidArgument", status.Code(err))
			}
			if got := status.Convert(err).Message(); got != errMCPTokenMatchesRetainedToolMetadata.Error() {
				t.Fatalf("message = %q, want the fixed reason %q", got, errMCPTokenMatchesRetainedToolMetadata.Error())
			}
			current, getErr := repo.GetMCPServer(ctx, server.Server.ID)
			if getErr != nil {
				t.Fatal(getErr)
			}
			if len(current.SealedToken) != 0 {
				t.Fatal("a refused rotation must not replace the server's sealed token")
			}
		})
	}
}

// TestRotateMcpServerTokenRefusesNewTokenMatchingNumericSchemaLiteral
// proves the check catches a token equal to the literal serialized text of
// a JSON number — a value redactMCPSecret's own scalar handling has to
// treat specially because it never decodes to a Go string — which only a
// raw-text scan of the *stored* schema_json can catch: a token like "42"
// never appears as any decoded string/map-key value inside
// {"type":"object","minimum":42} (42 decodes to a float64, never scanned
// as a string by a decoded-value walk), yet it is right there in the
// stored schema's own literal, already-serialized text.
func TestRotateMcpServerTokenRefusesNewTokenMatchingNumericSchemaLiteral(t *testing.T) {
	service, repo := newRegistryTestService(t)
	ctx := context.Background()
	server, err := repo.RegisterMCPServer(ctx, repository.ImportedMCPServer{
		Name: "vendor", URL: "https://vendor.example/mcp", Tier: repository.MCPServerTierRemoteURL,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := service.RecordDiscovery(ctx, server.Server.ID, []DiscoveredTool{
		{Name: "vendor.write", SchemaJSON: `{"type":"object","minimum":42}`},
	}); err != nil {
		t.Fatal(err)
	}

	_, err = service.RotateMcpServerToken(ctx, &turingv1.RotateMcpServerTokenRequest{
		ServerId: server.Server.ID, BearerToken: "42",
	})
	if err == nil {
		t.Fatal("a new token matching a numeric schema literal must be refused")
	}
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("code = %v, want InvalidArgument", status.Code(err))
	}
	if got := status.Convert(err).Message(); got != errMCPTokenMatchesRetainedToolMetadata.Error() {
		t.Fatalf("message = %q, want the fixed reason %q", got, errMCPTokenMatchesRetainedToolMetadata.Error())
	}
}

// TestRotateMcpServerTokenRefusesNewTokenMatchingCanonicalizedSchemaNumericLiteral
// proves tokenAppearsInRetainedToolMetadata's third scan: re-marshaling
// the decoded schema_json back into canonical JSON and scanning those
// bytes too, not just the stored raw text and the decoded-string walk.
// A schema_json literal stored in scientific notation (json.Marshal's own
// canonical re-serialization of the *decoded* float64 renders "1e2" as
// "100", "1e-2" as "0.01", "1.5e2" as "150" — Go's shortest-round-trip
// float formatting, not the original literal text) never appears
// character-for-character in the stored text, and it never decodes to a
// Go string (it is a JSON number, invisible to the map/slice/string walk
// mcpRawMetadataContainsToken performs). Only comparing against the
// canonical re-marshal catches it. This is exactly the gap the previous
// version of tokenAppearsInRetainedToolMetadata's own doc comment
// acknowledged and dismissed ("the secrecy invariant... still holds
// regardless") — this test proves that dismissal is no longer the
// behavior: the collision is now refused, not waved through.
func TestRotateMcpServerTokenRefusesNewTokenMatchingCanonicalizedSchemaNumericLiteral(t *testing.T) {
	for _, test := range []struct {
		name       string
		schemaJSON string
		token      string
	}{
		{name: "positive exponent collapses to plain integer", schemaJSON: `{"type":"object","minimum":1e2}`, token: "100"},
		{name: "negative exponent collapses to decimal", schemaJSON: `{"type":"object","minimum":1e-2}`, token: "0.01"},
		{name: "decimal mantissa with exponent collapses", schemaJSON: `{"type":"object","minimum":1.5e2}`, token: "150"},
	} {
		t.Run(test.name, func(t *testing.T) {
			service, repo, database := newRegistryTestServiceWithRealAudit(t)
			notifier := &countingRegistryChangeNotifier{}
			service.SetRegistryChangeNotifier(notifier)
			ctx := context.Background()
			server, err := repo.RegisterMCPServer(ctx, repository.ImportedMCPServer{
				Name: "vendor", URL: "https://vendor.example/mcp", Tier: repository.MCPServerTierRemoteURL,
			})
			if err != nil {
				t.Fatal(err)
			}
			if err := service.RecordDiscovery(ctx, server.Server.ID, []DiscoveredTool{
				{Name: "vendor.write", SchemaJSON: test.schemaJSON},
			}); err != nil {
				t.Fatal(err)
			}

			_, err = service.RotateMcpServerToken(ctx, &turingv1.RotateMcpServerTokenRequest{
				ServerId: server.Server.ID, BearerToken: test.token,
			})
			if err == nil {
				t.Fatal("a new token matching only the schema's canonical re-serialization must be refused")
			}
			if status.Code(err) != codes.InvalidArgument {
				t.Fatalf("code = %v, want InvalidArgument", status.Code(err))
			}
			if got := status.Convert(err).Message(); got != errMCPTokenMatchesRetainedToolMetadata.Error() {
				t.Fatalf("message = %q, want the fixed reason %q", got, errMCPTokenMatchesRetainedToolMetadata.Error())
			}
			current, getErr := repo.GetMCPServer(ctx, server.Server.ID)
			if getErr != nil {
				t.Fatal(getErr)
			}
			if len(current.SealedToken) != 0 {
				t.Fatal("a refused rotation must not replace the server's sealed token")
			}
			if notifier.calls != 0 {
				t.Fatalf("notify calls = %d, want 0: a refused rotation must not notify", notifier.calls)
			}
			if payloads := auditPayloadsForAction(t, database, "mcp.server.token_rotated"); len(payloads) != 0 {
				t.Fatalf("audit payloads = %+v, want none for a refused rotation", payloads)
			}
		})
	}
}

// TestRotateMcpServerTokenAcceptsUnrelatedTokenDespiteCanonicalizedSchemaNumericLiteral
// is the non-regression case for the new canonical scan: a token wholly
// unrelated to a retained tool's schema must still rotate successfully
// even though that schema contains a scientific-notation numeric literal
// whose canonical re-serialization the new third scan now compares
// against.
func TestRotateMcpServerTokenAcceptsUnrelatedTokenDespiteCanonicalizedSchemaNumericLiteral(t *testing.T) {
	service, repo := newRegistryTestService(t)
	ctx := context.Background()
	server, err := repo.RegisterMCPServer(ctx, repository.ImportedMCPServer{
		Name: "vendor", URL: "https://vendor.example/mcp", Tier: repository.MCPServerTierRemoteURL,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := service.RecordDiscovery(ctx, server.Server.ID, []DiscoveredTool{
		{Name: "vendor.write", SchemaJSON: `{"type":"object","minimum":1e2}`},
	}); err != nil {
		t.Fatal(err)
	}

	descriptor, err := service.RotateMcpServerToken(ctx, &turingv1.RotateMcpServerTokenRequest{
		ServerId: server.Server.ID, BearerToken: "completely-unrelated-bearer-value",
	})
	if err != nil {
		t.Fatalf("an unrelated token must still rotate successfully: %v", err)
	}
	if descriptor.GetName() != "vendor" {
		t.Fatalf("descriptor = %+v, want the server actually rotated", descriptor)
	}
	current, err := repo.GetMCPServer(ctx, server.Server.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(current.SealedToken) == 0 {
		t.Fatal("an accepted rotation must have replaced the server's sealed token")
	}
}

// TestRotateMcpServerTokenRefusesNewTokenRequiringDecodedToolSchemaComparison
// proves the opposite half of the same two-scan requirement: a token
// containing a quote or backslash, stored escaped inside schema_json's own
// text (`"` becomes `\"`, `\` becomes `\\`), can never be found there by a
// plain, raw-text substring search — only by re-decoding the stored JSON
// back into its original runtime value first (mirroring
// TestRegisterMcpServerRefusesTokenRequiringDecodedURLPathComparison's own
// reasoning, applied to a tool's schema instead of a URL path).
func TestRotateMcpServerTokenRefusesNewTokenRequiringDecodedToolSchemaComparison(t *testing.T) {
	service, repo := newRegistryTestService(t)
	ctx := context.Background()
	server, err := repo.RegisterMCPServer(ctx, repository.ImportedMCPServer{
		Name: "vendor", URL: "https://vendor.example/mcp", Tier: repository.MCPServerTierRemoteURL,
	})
	if err != nil {
		t.Fatal(err)
	}
	const token = `sec"ret\schema`
	encodedDescription, err := json.Marshal(token)
	if err != nil {
		t.Fatal(err)
	}
	schemaJSON := `{"type":"object","description":` + string(encodedDescription) + `}`
	if err := service.RecordDiscovery(ctx, server.Server.ID, []DiscoveredTool{
		{Name: "vendor.write", SchemaJSON: schemaJSON},
	}); err != nil {
		t.Fatal(err)
	}

	_, err = service.RotateMcpServerToken(ctx, &turingv1.RotateMcpServerTokenRequest{
		ServerId: server.Server.ID, BearerToken: token,
	})
	if err == nil {
		t.Fatal("a new token requiring decoded-schema comparison (quote/backslash) must still be refused")
	}
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("code = %v, want InvalidArgument", status.Code(err))
	}
	if got := status.Convert(err).Message(); got != errMCPTokenMatchesRetainedToolMetadata.Error() {
		t.Fatalf("message = %q, want the fixed reason %q", got, errMCPTokenMatchesRetainedToolMetadata.Error())
	}
}

// TestRotateMcpServerTokenAcceptsUnrelatedTokenDespiteRetainedTools is the
// non-regression case: a token with no relationship at all to any retained
// tool's name or schema must still rotate successfully even though the
// server carries both a present and a withdrawn tool.
func TestRotateMcpServerTokenAcceptsUnrelatedTokenDespiteRetainedTools(t *testing.T) {
	service, repo := newRegistryTestService(t)
	ctx := context.Background()
	server, err := repo.RegisterMCPServer(ctx, repository.ImportedMCPServer{
		Name: "vendor", URL: "https://vendor.example/mcp", Tier: repository.MCPServerTierRemoteURL,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := service.RecordDiscovery(ctx, server.Server.ID, []DiscoveredTool{
		{Name: "vendor.will-withdraw", SchemaJSON: `{"type":"object","minimum":42}`},
	}); err != nil {
		t.Fatal(err)
	}
	if err := service.RecordDiscovery(ctx, server.Server.ID, []DiscoveredTool{
		{Name: "vendor.write", SchemaJSON: `{"type":"object","properties":{"path":{"type":"string"}}}`},
	}); err != nil {
		t.Fatal(err)
	}

	descriptor, err := service.RotateMcpServerToken(ctx, &turingv1.RotateMcpServerTokenRequest{
		ServerId: server.Server.ID, BearerToken: "completely-unrelated-bearer-value",
	})
	if err != nil {
		t.Fatalf("an unrelated token must still rotate successfully: %v", err)
	}
	if descriptor.GetName() != "vendor" {
		t.Fatalf("descriptor = %+v, want the server actually rotated", descriptor)
	}
	current, err := repo.GetMCPServer(ctx, server.Server.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(current.SealedToken) == 0 {
		t.Fatal("an accepted rotation must have replaced the server's sealed token")
	}
}

// TestRotateMcpServerTokenRetainedToolMetadataCollisionSentinelSweep proves
// a refused retained-tool-metadata-collision rotation leaves no trace
// anywhere a response, log, audit row, event, or repository row could
// carry it — mirroring
// TestRegisterMcpServerTokenMetadataCollisionSentinelSweep below and
// TestRotateMcpServerTokenNotifiesAndAuditsBeforeADescriptorFailure
// (rotate_test.go) for the shape of a real, audited rotation. The refusal
// happens before any token mutation, so the sealed token is unchanged, no
// audit row exists, no registry-change notification fired, and the
// sentinel never appears in the process log either.
func TestRotateMcpServerTokenRetainedToolMetadataCollisionSentinelSweep(t *testing.T) {
	const sentinel = "mcp-rotate-retained-tool-sentinel-7f3ad9c1-do-not-leak"
	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if err := db.ApplyMigrations(context.Background(), database); err != nil {
		t.Fatal(err)
	}
	repo := repository.New(database)
	sealer, err := secretbox.New(bytes.Repeat([]byte{0x41}, secretbox.KeySize))
	if err != nil {
		t.Fatal(err)
	}
	service := New(repo, sealer, nil)
	auditService := audit.New(repo)
	service.SetAuditRecorder(auditService)
	notifier := &countingRegistryChangeNotifier{}
	service.SetRegistryChangeNotifier(notifier)
	ctx := context.Background()

	server, err := repo.RegisterMCPServer(ctx, repository.ImportedMCPServer{
		Name: "vendor", URL: "https://vendor.example/mcp", Tier: repository.MCPServerTierRemoteURL,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := service.RecordDiscovery(ctx, server.Server.ID, []DiscoveredTool{
		{Name: sentinel, SchemaJSON: `{"type":"object"}`},
	}); err != nil {
		t.Fatal(err)
	}

	var logged bytes.Buffer
	previousLogOutput := log.Writer()
	log.SetOutput(&logged)
	t.Cleanup(func() { log.SetOutput(previousLogOutput) })

	_, err = service.RotateMcpServerToken(ctx, &turingv1.RotateMcpServerTokenRequest{
		ServerId: server.Server.ID, BearerToken: sentinel,
	})
	if err == nil {
		t.Fatal("want a refusal")
	}
	assertStringSentinelFree(t, "rotate error", err.Error(), sentinel)

	current, getErr := repo.GetMCPServer(ctx, server.Server.ID)
	if getErr != nil {
		t.Fatal(getErr)
	}
	if len(current.SealedToken) != 0 {
		t.Fatal("a refused rotation must not replace the server's sealed token")
	}
	if notifier.calls != 0 {
		t.Fatalf("notify calls = %d, want 0: a refused rotation must not notify", notifier.calls)
	}

	var auditCount int
	if err := database.QueryRowContext(ctx, `SELECT COUNT(*) FROM audit_logs WHERE action IN ('mcp.server.token_rotated', 'mcp.server.token_cleared')`).Scan(&auditCount); err != nil {
		t.Fatal(err)
	}
	if auditCount != 0 {
		t.Fatalf("token rotation audit count = %d, want 0: a refusal before any mutation must never be audited", auditCount)
	}
	assertStringSentinelFree(t, "process log", logged.String(), sentinel)
}

// TestRegisterMcpServerTokenMetadataCollisionSentinelSweep proves a
// refused token-metadata-collision registration attempt leaves no trace
// anywhere a response, log, audit row, event, or repository row could
// carry it: the refusal happens before any repository mutation, so
// nothing is persisted, notified, or audited at all — the sentinel is
// swept across every surface anyway, the same way
// TestBearerTokenNeverLeaksAcrossRegisterAndRotate and
// TestImportUnsupportedHeaderKeyEqualToBearerTokenNeverLeaks
// (security_sentinel_test.go) do for their own findings, rather than
// assuming "refused before persistence" is proof enough on its own.
func TestRegisterMcpServerTokenMetadataCollisionSentinelSweep(t *testing.T) {
	const sentinel = "mcp-registry-token-metadata-sentinel-2a7c9f0e-do-not-leak"
	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if err := db.ApplyMigrations(context.Background(), database); err != nil {
		t.Fatal(err)
	}
	repo := repository.New(database)
	sealer, err := secretbox.New(bytes.Repeat([]byte{0x41}, secretbox.KeySize))
	if err != nil {
		t.Fatal(err)
	}
	service := New(repo, sealer, nil)
	auditService := audit.New(repo)
	service.SetAuditRecorder(auditService)
	ctx := context.Background()

	var logged bytes.Buffer
	previousLogOutput := log.Writer()
	log.SetOutput(&logged)
	t.Cleanup(func() { log.SetOutput(previousLogOutput) })

	_, err = service.RegisterMcpServer(ctx, &turingv1.RegisterMcpServerRequest{
		Name: sentinel, Url: "https://vendor.example/mcp",
		Tier:        turingv1.McpServerTier_MCP_SERVER_TIER_REMOTE_URL,
		BearerToken: sentinel,
	})
	if err == nil {
		t.Fatal("want a refusal")
	}
	assertStringSentinelFree(t, "register error", err.Error(), sentinel)

	if _, lookupErr := repo.GetMCPServerByName(ctx, sentinel); lookupErr != repository.ErrMCPServerNotFound {
		t.Fatalf("err = %v, want ErrMCPServerNotFound: no row from a refused registration", lookupErr)
	}

	var auditCount int
	if err := database.QueryRowContext(ctx, `SELECT COUNT(*) FROM audit_logs`).Scan(&auditCount); err != nil {
		t.Fatal(err)
	}
	if auditCount != 0 {
		t.Fatalf("audit count = %d, want 0: a refusal before any mutation must never be audited", auditCount)
	}
	var eventCount int
	if err := database.QueryRowContext(ctx, `SELECT COUNT(*) FROM events`).Scan(&eventCount); err != nil {
		t.Fatal(err)
	}
	if eventCount != 0 {
		t.Fatalf("event count = %d, want 0", eventCount)
	}
	assertStringSentinelFree(t, "process log", logged.String(), sentinel)
}
