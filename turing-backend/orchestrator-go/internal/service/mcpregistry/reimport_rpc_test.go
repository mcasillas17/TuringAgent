package mcpregistry

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
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
