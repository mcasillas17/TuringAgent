package mcpregistry

import (
	"context"
	"strings"
	"testing"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// A bearer token of exactly maxMCPBearerTokenBytes must still be accepted
// by normalizeBearerToken — the boundary must not be off-by-one against a
// legitimate, maximally-long credential.
func TestNormalizeBearerTokenAcceptsExactCap(t *testing.T) {
	token := strings.Repeat("a", maxMCPBearerTokenBytes)
	normalized, err := normalizeBearerToken(token)
	if err != nil {
		t.Fatalf("a token of exactly maxMCPBearerTokenBytes must not be refused: %v", err)
	}
	if len(normalized) != maxMCPBearerTokenBytes {
		t.Fatalf("normalized length = %d, want exactly maxMCPBearerTokenBytes (%d)", len(normalized), maxMCPBearerTokenBytes)
	}
}

// One byte over the cap must be refused with a fixed, generic reason —
// never the token itself, its length, or any other identifying detail.
func TestNormalizeBearerTokenRejectsOneByteOverCap(t *testing.T) {
	token := strings.Repeat("a", maxMCPBearerTokenBytes+1)
	_, err := normalizeBearerToken(token)
	if err == nil {
		t.Fatal("want an error for a token one byte over maxMCPBearerTokenBytes")
	}
	if strings.Contains(err.Error(), token) {
		t.Fatalf("err = %v, must not echo the oversized token", err)
	}
}

// The byte cap must be computed in real UTF-8 bytes, not rune count: a
// multibyte token at exactly the byte cap must be accepted, and one byte
// over (still adding whole runes) must be refused — proving len() byte
// semantics, not a naive character count that would undercount a
// multi-byte token relative to its real stored size.
func TestNormalizeBearerTokenCapUsesByteLengthNotRuneCount(t *testing.T) {
	// "é" is 2 bytes in UTF-8 (U+00E9), so maxMCPBearerTokenBytes/2 copies
	// of it are exactly maxMCPBearerTokenBytes bytes but half as many
	// runes.
	if maxMCPBearerTokenBytes%2 != 0 {
		t.Fatal("test setup assumes an even maxMCPBearerTokenBytes")
	}
	atCap := strings.Repeat("é", maxMCPBearerTokenBytes/2)
	if len(atCap) != maxMCPBearerTokenBytes {
		t.Fatalf("test setup is broken: byte length = %d, want exactly maxMCPBearerTokenBytes (%d)", len(atCap), maxMCPBearerTokenBytes)
	}
	if _, err := normalizeBearerToken(atCap); err != nil {
		t.Fatalf("a multibyte token at exactly maxMCPBearerTokenBytes bytes must not be refused: %v", err)
	}

	overCap := atCap + "x"
	if len(overCap) != maxMCPBearerTokenBytes+1 {
		t.Fatalf("test setup is broken: byte length = %d, want exactly maxMCPBearerTokenBytes+1", len(overCap))
	}
	if _, err := normalizeBearerToken(overCap); err == nil {
		t.Fatal("want an error for a multibyte token one byte over maxMCPBearerTokenBytes")
	}
}

// The cap must be enforced identically for every path a bearer token can
// arrive through: an mcp.json Authorization header, RegisterMcpServer's
// bearer_token field, and RotateMcpServerToken's bearer_token field — all
// three funnel through the one shared normalizeBearerToken.
func TestBearerTokenLengthCapEnforcedAcrossImportRegisterAndRotate(t *testing.T) {
	oversized := strings.Repeat("a", maxMCPBearerTokenBytes+1)

	t.Run("import", func(t *testing.T) {
		service, repo := newRegistryTestService(t)
		ctx := context.Background()
		report, err := service.ImportJSON(ctx, []byte(`{
			"mcpServers": {
				"vendor": {
					"url": "https://vendor.example/mcp",
					"headers": {"Authorization": "Bearer `+oversized+`"}
				}
			}
		}`))
		if err != nil {
			t.Fatal(err)
		}
		reason, refused := report.Unsupported["vendor"]
		if !refused {
			t.Fatalf("Unsupported = %+v, want vendor refused for an oversized bearer token", report.Unsupported)
		}
		if strings.Contains(reason, oversized) {
			t.Fatalf("reason = %q, must not echo the oversized token", reason)
		}
		if _, err := repo.GetMCPServerByName(ctx, "vendor"); err != repository.ErrMCPServerNotFound {
			t.Fatalf("err = %v, want ErrMCPServerNotFound: an oversized token must create no row", err)
		}
	})

	t.Run("register", func(t *testing.T) {
		service, repo := newRegistryTestService(t)
		ctx := context.Background()
		_, err := service.RegisterMcpServer(ctx, &turingv1.RegisterMcpServerRequest{
			Name: "vendor", Url: "https://vendor.example/mcp",
			Tier:        turingv1.McpServerTier_MCP_SERVER_TIER_REMOTE_URL,
			BearerToken: oversized,
		})
		if status.Code(err) != codes.InvalidArgument {
			t.Fatalf("code = %v, want InvalidArgument for an oversized bearer token", status.Code(err))
		}
		if err != nil && strings.Contains(err.Error(), oversized) {
			t.Fatalf("err = %v, must not echo the oversized token", err)
		}
		if _, gerr := repo.GetMCPServerByName(ctx, "vendor"); gerr != repository.ErrMCPServerNotFound {
			t.Fatalf("err = %v, want ErrMCPServerNotFound: an oversized token must create no row", gerr)
		}
	})

	t.Run("rotate", func(t *testing.T) {
		service, repo := newRegistryTestService(t)
		ctx := context.Background()
		server, err := repo.RegisterMCPServer(ctx, repository.ImportedMCPServer{
			Name: "vendor", URL: "https://vendor.example/mcp", Tier: repository.MCPServerTierRemoteURL,
		})
		if err != nil {
			t.Fatal(err)
		}
		_, rerr := service.RotateMcpServerToken(ctx, &turingv1.RotateMcpServerTokenRequest{
			ServerId: server.Server.ID, BearerToken: oversized,
		})
		if status.Code(rerr) != codes.InvalidArgument {
			t.Fatalf("code = %v, want InvalidArgument for an oversized bearer token", status.Code(rerr))
		}
		if rerr != nil && strings.Contains(rerr.Error(), oversized) {
			t.Fatalf("err = %v, must not echo the oversized token", rerr)
		}
	})
}

// A bearer token of exactly the cap must still be accepted end to end
// through RegisterMcpServer — the boundary must not be off-by-one at the
// RPC layer either.
func TestRegisterMcpServerAcceptsBearerTokenAtExactCap(t *testing.T) {
	service, _ := newRegistryTestService(t)
	ctx := context.Background()
	atCap := strings.Repeat("a", maxMCPBearerTokenBytes)
	_, err := service.RegisterMcpServer(ctx, &turingv1.RegisterMcpServerRequest{
		Name: "vendor", Url: "https://vendor.example/mcp",
		Tier:        turingv1.McpServerTier_MCP_SERVER_TIER_REMOTE_URL,
		BearerToken: atCap,
	})
	if err != nil {
		t.Fatalf("a bearer token of exactly maxMCPBearerTokenBytes must not be refused: %v", err)
	}
}
