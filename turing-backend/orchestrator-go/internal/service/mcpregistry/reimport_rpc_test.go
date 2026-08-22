package mcpregistry

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/db"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/secretbox"
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

// Every successful ReimportMcpJson RPC is audited — including a run that
// imports nothing (an absent mcp.json) and a run that is refused-only (a
// malformed document) — with counts alone: imported, skipped, refused.
// Never the names or reasons that would identify what happened, which is
// exactly the information ListMcpServers/ReimportMcpJson's own response
// already carries in full.
func TestReimportMcpJsonAuditsEveryRunIncludingEmptyAndMalformedOnly(t *testing.T) {
	service, repo := newRegistryTestService(t)
	recorder := &recordingAuditRecorder{}
	service.SetAuditRecorder(recorder)
	root := t.TempDir()
	service.SetMCPConfigRoot(root)

	// Run 1: absent mcp.json — an empty, clean-slate run.
	if _, err := service.ReimportMcpJson(context.Background(), &turingv1.ReimportMcpJsonRequest{}); err != nil {
		t.Fatal(err)
	}

	// Run 2: a malformed document — a refused-only run, nothing imported
	// or skipped.
	if err := os.WriteFile(filepath.Join(root, "mcp.json"), []byte("not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := service.ReimportMcpJson(context.Background(), &turingv1.ReimportMcpJsonRequest{}); err != nil {
		t.Fatal(err)
	}

	// Run 3: one of each disposition.
	if _, err := repo.RegisterMCPServer(context.Background(), repository.ImportedMCPServer{
		Name: "already-there", URL: "https://already-there.example/mcp", Tier: repository.MCPServerTierRemoteURL,
	}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "mcp.json"), []byte(`{
		"mcpServers": {
			"new-vendor": {"url": "https://new-vendor.example/mcp"},
			"already-there": {"url": "https://already-there.example/mcp"},
			"bad-vendor": {"command": "npx", "args": ["x"]}
		}
	}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := service.ReimportMcpJson(context.Background(), &turingv1.ReimportMcpJsonRequest{}); err != nil {
		t.Fatal(err)
	}

	if len(recorder.records) != 3 {
		t.Fatalf("records = %+v, want three audit rows, one per successful reimport run", recorder.records)
	}
	for i, record := range recorder.records {
		if record.action != "mcp.server.reimported" {
			t.Fatalf("records[%d].action = %q, want mcp.server.reimported", i, record.action)
		}
		if record.target != "mcp.json" {
			t.Fatalf("records[%d].target = %q, want mcp.json", i, record.target)
		}
		for key := range record.payload {
			if key != "imported" && key != "skipped" && key != "refused" {
				t.Fatalf("records[%d] payload has unexpected key %q: %+v", i, key, record.payload)
			}
		}
	}
	if recorder.records[0].payload["imported"] != 0 || recorder.records[0].payload["skipped"] != 0 || recorder.records[0].payload["refused"] != 0 {
		t.Fatalf("empty-run payload = %+v, want all zero counts", recorder.records[0].payload)
	}
	if recorder.records[1].payload["imported"] != 0 || recorder.records[1].payload["skipped"] != 0 || recorder.records[1].payload["refused"] != 1 {
		t.Fatalf("malformed-only run payload = %+v, want refused=1 and everything else zero", recorder.records[1].payload)
	}
	if recorder.records[2].payload["imported"] != 1 || recorder.records[2].payload["skipped"] != 1 || recorder.records[2].payload["refused"] != 1 {
		t.Fatalf("mixed run payload = %+v, want imported=1 skipped=1 refused=1", recorder.records[2].payload)
	}
}

// The notify and audit steps — and, now that Refused is built purely from
// this call's own in-memory report rather than a second repository read,
// the response mapping itself — must not be skippable by anything that
// could race in immediately after ReimportConfiguredJSON's mutation has
// already committed. This uses an audit recorder that, once it has
// captured the row, cancels the very context the RPC was called with —
// simulating a side effect of the audit write (or of the notifier)
// becoming visible to whatever runs next. Previously this deterministically
// broke the later ListMCPImportIssues re-read used to build the response,
// so the RPC returned Internal; now that read is gone, so the RPC must
// succeed regardless, and notify/audit/the response itself must all still
// reflect the committed import. Moving notify or audit to after the
// response is built (or reintroducing a second repository read for
// Refused) would reopen exactly this race.
func TestReimportMcpJsonAuditSurvivesALaterResponseMappingFailure(t *testing.T) {
	service, _ := newRegistryTestService(t)
	root := t.TempDir()
	service.SetMCPConfigRoot(root)
	if err := os.WriteFile(filepath.Join(root, "mcp.json"), []byte(`{
		"mcpServers": {
			"vendor": {"url": "https://vendor.example/mcp"}
		}
	}`), 0o600); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	recorder := &recordingAuditRecorder{afterRecord: cancel}
	service.SetAuditRecorder(recorder)
	notifier := &countingRegistryChangeNotifier{}
	service.SetRegistryChangeNotifier(notifier)

	response, err := service.ReimportMcpJson(ctx, &turingv1.ReimportMcpJsonRequest{})
	if err != nil {
		t.Fatalf("ReimportMcpJson must still succeed: the response is mapped entirely from the in-memory report, not a second read that could observe the now-cancelled context: %v", err)
	}
	if ctx.Err() == nil {
		t.Fatal("test setup failed: the audit recorder never cancelled the original request context")
	}
	if len(recorder.records) != 1 {
		t.Fatalf("records = %+v, want the reimport audited despite the later cancellation", recorder.records)
	}
	if recorder.records[0].action != "mcp.server.reimported" {
		t.Fatalf("action = %q, want mcp.server.reimported", recorder.records[0].action)
	}
	if notifier.calls != 1 {
		t.Fatalf("notify calls = %d, want 1: notify must fire for a committed import despite the later cancellation", notifier.calls)
	}
	if len(response.GetImported()) != 1 || response.GetImported()[0] != "vendor" {
		t.Fatalf("Imported = %v, want [vendor] despite the later cancellation", response.GetImported())
	}
}

// A bearer token given via mcp.json headers must never reach the
// ReimportMcpJson response, its audit row, or the process log — the same
// guarantee TestBearerTokenNeverLeaksAcrossRegisterAndRotate proves for
// RegisterMcpServer/RotateMcpServerToken, extended to the reimport path
// now that reimport is itself audited.
func TestReimportMcpJsonBearerSentinelRunStaysSentinelFree(t *testing.T) {
	service, _ := newRegistryTestService(t)
	recorder := &recordingAuditRecorder{}
	service.SetAuditRecorder(recorder)
	root := t.TempDir()
	service.SetMCPConfigRoot(root)

	const sentinel = "reimport-bearer-sentinel-9f3c7a1e-do-not-leak"
	if err := os.WriteFile(filepath.Join(root, "mcp.json"), []byte(fmt.Sprintf(`{
		"mcpServers": {
			"vendor": {
				"url": "https://vendor.example/mcp",
				"headers": {"Authorization": "Bearer %s"}
			}
		}
	}`, sentinel)), 0o600); err != nil {
		t.Fatal(err)
	}

	var logged bytes.Buffer
	previous := log.Writer()
	log.SetOutput(&logged)
	t.Cleanup(func() { log.SetOutput(previous) })

	response, err := service.ReimportMcpJson(context.Background(), &turingv1.ReimportMcpJsonRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if len(response.GetImported()) != 1 || response.GetImported()[0] != "vendor" {
		t.Fatalf("Imported = %v, want [vendor]", response.GetImported())
	}
	assertStringSentinelFree(t, "reimport response", fmt.Sprintf("%+v", response), sentinel)

	if len(recorder.records) != 1 {
		t.Fatalf("records = %+v, want one audit row", recorder.records)
	}
	for key, value := range recorder.records[0].payload {
		if key != "imported" && key != "skipped" && key != "refused" {
			t.Fatalf("payload has unexpected key %q=%v", key, value)
		}
		if s, ok := value.(string); ok {
			assertStringSentinelFree(t, "audit payload value", s, sentinel)
		}
	}
	assertStringSentinelFree(t, "process log", logged.String(), sentinel)
}

// TestReimportMcpJsonConcurrentOverlappingRunsCannotSwapRefusedResponses
// proves two overlapping ReimportMcpJson calls sharing one repository can
// never swap each other's Refused response: each call's response must
// reflect only its own report.Unsupported, never whatever the shared
// mcp_import_issues table looked like by the time the OTHER call finished
// writing to it. reimportBarrier forces call A to pause after its own
// ImportJSON has already persisted its issues ("bad-a") but before A's
// response is built, while call B (a second *Server sharing the same
// repository, with no barrier of its own) runs an entire overlapping
// reimport to completion first — persisting a different issue ("bad-b")
// into the same shared table and returning before A resumes. If the
// response were still built by re-reading that shared table (the bug this
// change removes), A's response would show bad-b's reason instead of its
// own.
func TestReimportMcpJsonConcurrentOverlappingRunsCannotSwapRefusedResponses(t *testing.T) {
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

	serviceA := New(repo, sealer, nil)
	rootA := t.TempDir()
	serviceA.SetMCPConfigRoot(rootA)
	if err := os.WriteFile(filepath.Join(rootA, "mcp.json"), []byte(`{
		"mcpServers": {
			"bad-a": {"command": "npx", "args": ["x"]}
		}
	}`), 0o600); err != nil {
		t.Fatal(err)
	}

	serviceB := New(repo, sealer, nil)
	rootB := t.TempDir()
	serviceB.SetMCPConfigRoot(rootB)
	if err := os.WriteFile(filepath.Join(rootB, "mcp.json"), []byte(`{
		"mcpServers": {
			"bad-b": {"command": "npx", "args": ["x"]}
		}
	}`), 0o600); err != nil {
		t.Fatal(err)
	}

	reached := make(chan struct{})
	release := make(chan struct{})
	var barrierFired int
	serviceA.reimportBarrier = func() {
		barrierFired++
		close(reached)
		<-release
	}

	type outcome struct {
		response *turingv1.ReimportMcpJsonResponse
		err      error
	}
	resultA := make(chan outcome, 1)
	go func() {
		response, err := serviceA.ReimportMcpJson(context.Background(), &turingv1.ReimportMcpJsonRequest{})
		resultA <- outcome{response: response, err: err}
	}()

	select {
	case <-reached:
		// call A has persisted "bad-a" and is now paused before building
		// its response.
	case <-time.After(5 * time.Second):
		t.Fatal("test setup failed: reimportBarrier was never invoked by ReimportMcpJson")
	}

	responseB, err := serviceB.ReimportMcpJson(context.Background(), &turingv1.ReimportMcpJsonRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if got := responseB.GetRefused(); len(got) != 1 || got[0].GetName() != "bad-b" {
		t.Fatalf("B's Refused = %v, want [bad-b]", got)
	}

	close(release)
	var final outcome
	select {
	case final = <-resultA:
	case <-time.After(5 * time.Second):
		t.Fatal("call A never returned after its barrier was released")
	}
	if final.err != nil {
		t.Fatal(final.err)
	}
	if barrierFired != 1 {
		t.Fatalf("barrier fired %d times, want exactly 1", barrierFired)
	}
	if got := final.response.GetRefused(); len(got) != 1 || got[0].GetName() != "bad-a" {
		t.Fatalf("A's Refused = %v, want [bad-a]: it must never reflect B's concurrent refusal", got)
	}
}
