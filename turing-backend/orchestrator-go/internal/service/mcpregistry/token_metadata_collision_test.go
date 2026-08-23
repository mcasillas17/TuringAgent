package mcpregistry

import (
	"bytes"
	"context"
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
