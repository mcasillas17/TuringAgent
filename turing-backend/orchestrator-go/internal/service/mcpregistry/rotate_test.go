package mcpregistry

import (
	"bytes"
	"context"
	"strings"
	"testing"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/secretbox"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"
)

func TestRotateMcpServerTokenRequiresServerID(t *testing.T) {
	service, _ := newRegistryTestService(t)
	_, err := service.RotateMcpServerToken(context.Background(), &turingv1.RotateMcpServerTokenRequest{
		BearerToken: "vendor-secret",
	})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("code = %v, want InvalidArgument when server_id is missing", status.Code(err))
	}
}

func TestRotateMcpServerTokenMissingServerIsNotFound(t *testing.T) {
	service, _ := newRegistryTestService(t)
	_, err := service.RotateMcpServerToken(context.Background(), &turingv1.RotateMcpServerTokenRequest{
		ServerId:    "mcp_missing",
		BearerToken: "vendor-secret",
	})
	if status.Code(err) != codes.NotFound {
		t.Fatalf("code = %v, want NotFound for a missing server", status.Code(err))
	}
}

func TestRotateMcpServerTokenRefusesBundledServer(t *testing.T) {
	service, repo := newRegistryTestService(t)
	bundled, err := repo.ImportMCPServer(context.Background(), repository.ImportedMCPServer{
		Name: "bundled-vendor", URL: "https://bundled.example/mcp", Tier: repository.MCPServerTierBundled,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.RotateMcpServerToken(context.Background(), &turingv1.RotateMcpServerTokenRequest{
		ServerId:    bundled.Server.ID,
		BearerToken: "vendor-secret",
	})
	if status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("code = %v, want FailedPrecondition for a bundled server", status.Code(err))
	}
}

func TestRotateMcpServerTokenMissingKeyFailsPrecondition(t *testing.T) {
	service, repo := newRegistryTestService(t)
	server, err := repo.RegisterMCPServer(context.Background(), repository.ImportedMCPServer{
		Name: "vendor", URL: "https://vendor.example/mcp", Tier: repository.MCPServerTierRemoteURL,
	})
	if err != nil {
		t.Fatal(err)
	}
	service.sealer = nil

	_, err = service.RotateMcpServerToken(context.Background(), &turingv1.RotateMcpServerTokenRequest{
		ServerId:    server.ID,
		BearerToken: "vendor-secret",
	})
	if status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("code = %v, want FailedPrecondition when a token is given without a key", status.Code(err))
	}
	if !strings.Contains(err.Error(), "TURING_INTEGRATION_KEY") {
		t.Fatalf("error = %v, want it to name the missing key", err)
	}
}

func TestRotateMcpServerTokenEmptyClearsIt(t *testing.T) {
	service, repo := newRegistryTestService(t)
	sealer, err := secretbox.New(bytes.Repeat([]byte{0x41}, secretbox.KeySize))
	if err != nil {
		t.Fatal(err)
	}
	service.sealer = sealer
	server, err := repo.RegisterMCPServer(context.Background(), repository.ImportedMCPServer{
		Name: "vendor", URL: "https://vendor.example/mcp", Tier: repository.MCPServerTierRemoteURL,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.RotateMcpServerToken(context.Background(), &turingv1.RotateMcpServerTokenRequest{
		ServerId:    server.ID,
		BearerToken: "vendor-secret",
	}); err != nil {
		t.Fatal(err)
	}

	if _, err := service.RotateMcpServerToken(context.Background(), &turingv1.RotateMcpServerTokenRequest{
		ServerId:    server.ID,
		BearerToken: "",
	}); err != nil {
		t.Fatal(err)
	}

	updated, err := repo.GetMCPServer(context.Background(), server.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(updated.SealedToken) != 0 {
		t.Fatal("an empty bearer_token must clear the sealed token")
	}
}

func TestRotateMcpServerTokenSealsWithServerNameAsAAD(t *testing.T) {
	service, repo := newRegistryTestService(t)
	sealer, err := secretbox.New(bytes.Repeat([]byte{0x41}, secretbox.KeySize))
	if err != nil {
		t.Fatal(err)
	}
	service.sealer = sealer
	server, err := repo.RegisterMCPServer(context.Background(), repository.ImportedMCPServer{
		Name: "vendor", URL: "https://vendor.example/mcp", Tier: repository.MCPServerTierRemoteURL,
	})
	if err != nil {
		t.Fatal(err)
	}
	const token = "vendor-rotated-secret"
	if _, err := service.RotateMcpServerToken(context.Background(), &turingv1.RotateMcpServerTokenRequest{
		ServerId:    server.ID,
		BearerToken: token,
	}); err != nil {
		t.Fatal(err)
	}
	updated, err := repo.GetMCPServer(context.Background(), server.ID)
	if err != nil {
		t.Fatal(err)
	}
	opened, err := sealer.Open(updated.SealedToken, []byte(server.Name))
	if err != nil {
		t.Fatalf("token was not sealed with the server name as AAD: %v", err)
	}
	if string(opened) != token {
		t.Fatalf("stored token = %q, want %q", opened, token)
	}
}

func TestRotateMcpServerTokenRepeatedRotationsWork(t *testing.T) {
	service, repo := newRegistryTestService(t)
	sealer, err := secretbox.New(bytes.Repeat([]byte{0x41}, secretbox.KeySize))
	if err != nil {
		t.Fatal(err)
	}
	service.sealer = sealer
	server, err := repo.RegisterMCPServer(context.Background(), repository.ImportedMCPServer{
		Name: "vendor", URL: "https://vendor.example/mcp", Tier: repository.MCPServerTierRemoteURL,
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, token := range []string{"first-secret", "second-secret", "third-secret"} {
		if _, err := service.RotateMcpServerToken(context.Background(), &turingv1.RotateMcpServerTokenRequest{
			ServerId:    server.ID,
			BearerToken: token,
		}); err != nil {
			t.Fatalf("rotate to %q: %v", token, err)
		}
		updated, err := repo.GetMCPServer(context.Background(), server.ID)
		if err != nil {
			t.Fatal(err)
		}
		opened, err := sealer.Open(updated.SealedToken, []byte(server.Name))
		if err != nil {
			t.Fatal(err)
		}
		if string(opened) != token {
			t.Fatalf("stored token = %q, want %q", opened, token)
		}
	}
}

func TestRotateMcpServerTokenNotifiesRegistryChange(t *testing.T) {
	service, repo := newRegistryTestService(t)
	server, err := repo.RegisterMCPServer(context.Background(), repository.ImportedMCPServer{
		Name: "vendor", URL: "https://vendor.example/mcp", Tier: repository.MCPServerTierRemoteURL,
	})
	if err != nil {
		t.Fatal(err)
	}
	notifier := &countingRegistryChangeNotifier{}
	service.SetRegistryChangeNotifier(notifier)

	if _, err := service.RotateMcpServerToken(context.Background(), &turingv1.RotateMcpServerTokenRequest{
		ServerId: server.ID, BearerToken: "vendor-secret",
	}); err != nil {
		t.Fatal(err)
	}
	if notifier.calls != 1 {
		t.Fatalf("notify calls = %d, want 1 after a successful rotation", notifier.calls)
	}
}

func TestRotateMcpServerTokenResponseNeverIncludesTokenOrCiphertext(t *testing.T) {
	service, repo := newRegistryTestService(t)
	server, err := repo.RegisterMCPServer(context.Background(), repository.ImportedMCPServer{
		Name: "vendor", URL: "https://vendor.example/mcp", Tier: repository.MCPServerTierRemoteURL,
	})
	if err != nil {
		t.Fatal(err)
	}
	const token = "vendor-secret-should-never-be-returned"
	descriptor, err := service.RotateMcpServerToken(context.Background(), &turingv1.RotateMcpServerTokenRequest{
		ServerId:    server.ID,
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
