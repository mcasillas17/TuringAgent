package mcpregistry

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/prototext"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
)

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

func TestRegisterMcpServerRefusesReservedAndInvalidInput(t *testing.T) {
	service, _ := newRegistryTestService(t)
	for name, request := range map[string]*turingv1.RegisterMcpServerRequest{
		"reserved name":   {Name: "integrations", Url: "https://vendor.example/mcp"},
		"bundled name":    {Name: "files", Url: "https://vendor.example/mcp"},
		"invalid name":    {Name: "../escape", Url: "https://vendor.example/mcp"},
		"userinfo url":    {Name: "vendor", Url: "https://user:pass@vendor.example/mcp"},
		"relative url":    {Name: "vendor", Url: "/mcp"},
		"stdio-ish blank": {Name: "vendor", Url: ""},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := service.RegisterMcpServer(context.Background(), request); status.Code(err) != codes.InvalidArgument {
				t.Fatalf("err = %v, want InvalidArgument", err)
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
	if err := repo.DeleteMCPServer(context.Background(), vendor.ID); err != nil {
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
	service.SetMCPConfigPath(path)

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

func TestReimportWithoutMountedFileRefusesLegibly(t *testing.T) {
	service, _ := newRegistryTestService(t)
	if _, err := service.ReimportMcpJson(context.Background(), &turingv1.ReimportMcpJsonRequest{}); status.Code(err) != codes.FailedPrecondition ||
		!strings.Contains(err.Error(), "no mcp.json is mounted") {
		t.Fatalf("err = %v, want the no-mounted-path refusal", err)
	}
	service.SetMCPConfigPath(filepath.Join(t.TempDir(), "mcp.json"))
	_, err := service.ReimportMcpJson(context.Background(), &turingv1.ReimportMcpJsonRequest{})
	if status.Code(err) != codes.FailedPrecondition || !strings.Contains(err.Error(), "does not exist") {
		t.Fatalf("err = %v, want a legible missing-file refusal", err)
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
	service.SetMCPConfigPath(path)
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

func TestReimportMalformedDocumentReplacesStaleIssues(t *testing.T) {
	service, repo := newRegistryTestService(t)
	path := filepath.Join(t.TempDir(), "mcp.json")
	if err := os.WriteFile(path, []byte(`{"mcpServers": {"runner": {"command": "npx"}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	service.SetMCPConfigPath(path)
	if _, err := service.ReimportMcpJson(context.Background(), &turingv1.ReimportMcpJsonRequest{}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{not json`), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := service.ReimportMcpJson(context.Background(), &turingv1.ReimportMcpJsonRequest{})
	if status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("malformed re-import err = %v, want FailedPrecondition", err)
	}
	issues, err := repo.ListMCPImportIssues(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(issues) != 1 || issues[0].Name != "_document" {
		t.Fatalf("issues after malformed re-import = %+v, want only the _document decode issue replacing the stale runner row", issues)
	}
}
