package mcpregistry

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestReimportConfiguredJSONAbsentFileClearsIssuesAndReturnsEmptyReport(t *testing.T) {
	service, repo := newRegistryTestService(t)
	if err := repo.ReplaceMCPImportIssues(context.Background(), map[string]string{
		"stale": "a previous run's issue",
	}); err != nil {
		t.Fatal(err)
	}
	service.SetMCPConfigRoot(t.TempDir())

	report, err := service.ReimportConfiguredJSON(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Imported) != 0 || len(report.Skipped) != 0 || len(report.Unsupported) != 0 {
		t.Fatalf("report = %+v, want empty when mcp.json is absent", report)
	}
	issues, err := repo.ListMCPImportIssues(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(issues) != 0 {
		t.Fatalf("issues = %+v, want cleared when mcp.json is absent", issues)
	}
}

func TestReimportConfiguredJSONMalformedDocumentIsRecordedAsBoundedDocumentRefusal(t *testing.T) {
	service, repo := newRegistryTestService(t)
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "mcp.json"), []byte("not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	service.SetMCPConfigRoot(root)

	report, err := service.ReimportConfiguredJSON(context.Background())
	if err != nil {
		t.Fatalf("a malformed document must be reported, not returned as an error: %v", err)
	}
	if len(report.Imported) != 0 || len(report.Skipped) != 0 {
		t.Fatalf("report = %+v, want no imports or skips for a malformed document", report)
	}
	if _, present := report.Unsupported["_document"]; !present {
		t.Fatalf("Unsupported = %v, want a _document refusal", report.Unsupported)
	}
	issues, err := repo.ListMCPImportIssues(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, issue := range issues {
		if issue.Name == "_document" {
			found = true
		}
	}
	if !found {
		t.Fatalf("issues = %+v, want a recorded _document issue", issues)
	}
}

func TestReimportConfiguredJSONValidFileGoesThroughImportJSON(t *testing.T) {
	service, repo := newRegistryTestService(t)
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "mcp.json"), []byte(`{
		"mcpServers": {
			"vendor": {"url": "https://vendor.example/mcp"}
		}
	}`), 0o600); err != nil {
		t.Fatal(err)
	}
	service.SetMCPConfigRoot(root)

	report, err := service.ReimportConfiguredJSON(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Imported) != 1 || report.Imported[0] != "vendor" {
		t.Fatalf("Imported = %v, want [vendor]", report.Imported)
	}
	servers, err := repo.ListMCPServers(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	findRepositoryServer(t, servers, "vendor")
}

// A directory in place of mcp.json is a deterministic non-ENOENT read
// failure (unlike an absent file, which is the clean-slate case covered
// above). ReimportConfiguredJSON must return the one fixed message rather
// than the underlying *PathError, which would otherwise repeat the config
// root's filesystem path; the public RPC must map that to Internal without
// leaking the path either.
func TestReimportConfiguredJSONOtherReadFailureReturnsFixedMessageAndMapsToInternal(t *testing.T) {
	service, _ := newRegistryTestService(t)
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "mcp.json"), 0o700); err != nil {
		t.Fatal(err)
	}
	service.SetMCPConfigRoot(root)

	report, err := service.ReimportConfiguredJSON(context.Background())
	if err == nil {
		t.Fatal("a directory in place of mcp.json must be reported as a read failure, not silently succeed")
	}
	if err.Error() != "read mcp.json failed" {
		t.Fatalf("error = %q, want the fixed read-failure message", err.Error())
	}
	if strings.Contains(err.Error(), root) {
		t.Fatalf("error = %q, must not expose the config root path", err.Error())
	}
	if len(report.Imported) != 0 || len(report.Skipped) != 0 || len(report.Unsupported) != 0 {
		t.Fatalf("report = %+v, want empty on a read failure", report)
	}

	_, rpcErr := service.ReimportMcpJson(context.Background(), &turingv1.ReimportMcpJsonRequest{})
	if status.Code(rpcErr) != codes.Internal {
		t.Fatalf("code = %v, want Internal for an unreadable mcp.json", status.Code(rpcErr))
	}
	if strings.Contains(rpcErr.Error(), root) {
		t.Fatalf("rpc error = %q, must not expose the config root path", rpcErr.Error())
	}
	const wantRPCMessage = "reimport mcp.json failed"
	if !strings.Contains(rpcErr.Error(), wantRPCMessage) {
		t.Fatalf("rpc error = %q, want it to contain %q", rpcErr.Error(), wantRPCMessage)
	}
}

// A reimport of a document that both repeats an already-imported server and
// introduces a new one must skip the former (create-only) and import the
// latter, alongside any refused entries, with each list independently
// sorted — the same mapping TestReimportMcpJsonRPCMapsReportFieldsAndSortsRefused
// exercises for Imported/Refused, extended to cover Skipped.
func TestReimportMcpJsonRPCMapsSkippedForAlreadyImportedServers(t *testing.T) {
	service, repo := newRegistryTestService(t)
	root := t.TempDir()
	for _, existing := range []string{"vendor", "another-vendor"} {
		if _, err := repo.RegisterMCPServer(context.Background(), repository.ImportedMCPServer{
			Name: existing, URL: "https://" + existing + ".example/mcp", Tier: repository.MCPServerTierRemoteURL,
		}); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "mcp.json"), []byte(`{
		"mcpServers": {
			"vendor": {"url": "https://vendor.example/mcp"},
			"another-vendor": {"url": "https://another-vendor.example/mcp"},
			"zz-new": {"url": "https://zz-new.example/mcp"},
			"aa-new": {"url": "https://aa-new.example/mcp"},
			"bad-vendor": {"command": "npx", "args": ["x"]}
		}
	}`), 0o600); err != nil {
		t.Fatal(err)
	}
	service.SetMCPConfigRoot(root)

	response, err := service.ReimportMcpJson(context.Background(), &turingv1.ReimportMcpJsonRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if got := response.GetImported(); len(got) != 2 || got[0] != "aa-new" || got[1] != "zz-new" {
		t.Fatalf("Imported = %v, want sorted [aa-new zz-new]", got)
	}
	if got := response.GetSkipped(); len(got) != 2 || got[0] != "another-vendor" || got[1] != "vendor" {
		t.Fatalf("Skipped = %v, want sorted [another-vendor vendor]", got)
	}
	if got := response.GetRefused(); len(got) != 1 || got[0].GetName() != "bad-vendor" {
		t.Fatalf("Refused = %v, want [bad-vendor]", got)
	}
}

func TestReimportMcpJsonRPCFailsPreconditionWhenRootIsUnconfigured(t *testing.T) {
	service, _ := newRegistryTestService(t)
	_, err := service.ReimportMcpJson(context.Background(), &turingv1.ReimportMcpJsonRequest{})
	if status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("code = %v, want FailedPrecondition when the config root is unconfigured", status.Code(err))
	}
}

func TestReimportMcpJsonRPCMapsReportFieldsAndSortsRefused(t *testing.T) {
	service, _ := newRegistryTestService(t)
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "mcp.json"), []byte(`{
		"mcpServers": {
			"vendor": {"url": "https://vendor.example/mcp"},
			"zz-bad": {"command": "npx", "args": ["x"]},
			"aa-bad": {"command": "npx", "args": ["x"]}
		}
	}`), 0o600); err != nil {
		t.Fatal(err)
	}
	service.SetMCPConfigRoot(root)

	response, err := service.ReimportMcpJson(context.Background(), &turingv1.ReimportMcpJsonRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if len(response.GetImported()) != 1 || response.GetImported()[0] != "vendor" {
		t.Fatalf("Imported = %v, want [vendor]", response.GetImported())
	}
	if len(response.GetRefused()) != 2 {
		t.Fatalf("Refused = %v, want 2 entries", response.GetRefused())
	}
	if response.GetRefused()[0].GetName() != "aa-bad" || response.GetRefused()[1].GetName() != "zz-bad" {
		t.Fatalf("Refused names = [%q, %q], want sorted [aa-bad, zz-bad]",
			response.GetRefused()[0].GetName(), response.GetRefused()[1].GetName())
	}
}

func TestReimportMcpJsonNotifiesOnlyWhenEntriesWereImported(t *testing.T) {
	service, _ := newRegistryTestService(t)
	notifier := &countingRegistryChangeNotifier{}
	service.SetRegistryChangeNotifier(notifier)

	root := t.TempDir()
	service.SetMCPConfigRoot(root)
	if _, err := service.ReimportMcpJson(context.Background(), &turingv1.ReimportMcpJsonRequest{}); err != nil {
		t.Fatal(err)
	}
	if notifier.calls != 0 {
		t.Fatalf("notify calls = %d, want 0 when nothing was imported (absent file)", notifier.calls)
	}

	if err := os.WriteFile(filepath.Join(root, "mcp.json"), []byte(`{
		"mcpServers": {
			"vendor": {"url": "https://vendor.example/mcp"}
		}
	}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := service.ReimportMcpJson(context.Background(), &turingv1.ReimportMcpJsonRequest{}); err != nil {
		t.Fatal(err)
	}
	if notifier.calls != 1 {
		t.Fatalf("notify calls = %d, want 1 after an import", notifier.calls)
	}
}

type countingRegistryChangeNotifier struct {
	calls int
}

func (n *countingRegistryChangeNotifier) NotifyMCPRegistryChanged(context.Context) error {
	n.calls++
	return nil
}
