package mcpregistry

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/db"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/secretbox"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// mcpTwoEntryDocument builds a minimal mcp.json document with two ordinary
// entries whose sorted-name processing order is fixed and predictable:
// "aaa-vendor" always commits before "zzz-vendor" is even attempted (see
// ImportJSON's own sorted-order comment).
func mcpTwoEntryDocument() []byte {
	return []byte(`{
		"mcpServers": {
			"aaa-vendor": {"url": "https://aaa-vendor.example/mcp"},
			"zzz-vendor": {"url": "https://zzz-vendor.example/mcp"}
		}
	}`)
}

// findUnsupportedReason returns the reason recorded for name in refused,
// and whether it was present at all.
func findUnsupportedReason(refused []*turingv1.UnsupportedMcpServer, name string) (string, bool) {
	for _, entry := range refused {
		if entry.GetName() == name {
			return entry.GetReason(), true
		}
	}
	return "", false
}

// TestReimportMcpJsonCancellationAfterFirstEntryStillSucceedsWithPartialReport
// is the "safe" path: the caller's context is canceled — via the
// test-only importEntryBarrier hook — immediately after "aaa-vendor" has
// already committed through its own independent transaction, but before
// "zzz-vendor" is even attempted. ImportJSON's own lookup for
// "zzz-vendor" then fails with a bare "context canceled" (not
// ErrMCPServerNotFound), which ImportJSON cannot attribute to a single
// entry and so returns as a fatal error — but, thanks to ImportJSON's own
// named-return contract, alongside the accumulated report naming
// "aaa-vendor" imported. ReimportConfiguredJSON's recordDocumentRefusal
// then persists the merged issues (a bounded "_document" entry) through a
// context detached from the now-canceled one, which succeeds — so the
// whole call surfaces as an ordinary successful partial report, never an
// error, with aaa-vendor's real commit intact, notified, and audited.
func TestReimportMcpJsonCancellationAfterFirstEntryStillSucceedsWithPartialReport(t *testing.T) {
	service, repo := newRegistryTestService(t)
	notifier := &countingRegistryChangeNotifier{}
	service.SetRegistryChangeNotifier(notifier)
	recorder := &recordingAuditRecorder{}
	service.SetAuditRecorder(recorder)

	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "mcp.json"), mcpTwoEntryDocument(), 0o600); err != nil {
		t.Fatal(err)
	}
	service.SetMCPConfigRoot(root)

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	var barrierFired int
	service.importEntryBarrier = func(name string) {
		barrierFired++
		if name == "aaa-vendor" {
			cancel()
		}
	}

	response, err := service.ReimportMcpJson(ctx, &turingv1.ReimportMcpJsonRequest{})
	if err != nil {
		t.Fatalf("a cancellation discovered only after the first entry already committed must still produce a successful partial report, not an error: %v", err)
	}
	if barrierFired != 1 {
		t.Fatalf("importEntryBarrier fired %d times, want exactly 1 (only aaa-vendor ever commits)", barrierFired)
	}
	if len(response.GetImported()) != 1 || response.GetImported()[0] != "aaa-vendor" {
		t.Fatalf("Imported = %v, want [aaa-vendor]: it committed before the cancellation was even discovered", response.GetImported())
	}
	if len(response.GetSkipped()) != 0 {
		t.Fatalf("Skipped = %v, want none", response.GetSkipped())
	}
	reason, present := findUnsupportedReason(response.GetUnsupported(), "_document")
	if !present {
		t.Fatalf("Refused = %+v, want a bounded _document entry describing the interrupted run", response.GetUnsupported())
	}
	if reason == "" || len(reason) > 512 {
		t.Fatalf("_document reason = %q, want a short, bounded, non-empty message", reason)
	}
	if bytesContainsAny(reason, "vendor-token", "sealed_token") {
		t.Fatalf("_document reason = %q, must never carry a token", reason)
	}

	// aaa-vendor really persisted, disabled — the create-only invariant
	// every other ImportJSON entry already gets.
	servers, err := repo.ListMCPServers(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	aaa := findRepositoryServer(t, servers, "aaa-vendor")
	if aaa.Enabled {
		t.Fatal("a freshly imported server must be disabled")
	}
	// zzz-vendor was never reached far enough to commit anything.
	for _, server := range servers {
		if server.Name == "zzz-vendor" {
			t.Fatalf("zzz-vendor must not exist: its own entry was never durably committed (%+v)", server)
		}
	}

	if notifier.calls != 1 {
		t.Fatalf("notify calls = %d, want 1: aaa-vendor really committed despite the later cancellation", notifier.calls)
	}
	if len(recorder.records) != 1 {
		t.Fatalf("audit records = %+v, want exactly one", recorder.records)
	}
	if recorder.records[0].payload["imported"] != 1 {
		t.Fatalf("audit payload = %+v, want imported=1", recorder.records[0].payload)
	}
	// ReimportMcpJson itself sees a nil error here (recordDocumentRefusal's
	// own detached-context persist succeeded despite the earlier
	// cancellation — see this test's own doc comment), so this audits as
	// an ordinary "completed" run, never "partial": that status is
	// reserved for a run ReimportMcpJson maps to Internal after some
	// entries already committed (see
	// TestReimportMcpJsonRepositoryFailureAfterFirstEntryStillAuditsBeforeInternal).
	if recorder.records[0].payload["status"] != "completed" {
		t.Fatalf("audit payload[status] = %v, want completed", recorder.records[0].payload["status"])
	}

	// The registry's own persisted issues reflect the same merged state
	// the response does — never silently stale from a previous run.
	issues, err := repo.ListMCPImportIssues(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	foundDocument := false
	for _, issue := range issues {
		if issue.Name == "_document" {
			foundDocument = true
		}
	}
	if !foundDocument {
		t.Fatalf("issues = %+v, want a persisted _document entry", issues)
	}
}

// TestReimportConfiguredJSONDirectlyReportsHonestUnsupportedCountAfterCancellation
// calls ReimportConfiguredJSON directly — exactly the way app startup
// does (see internal/app.New) to decide both whether to abort and what
// count to log — rather than through the ReimportMcpJson RPC. Startup's
// own "mcp.json import refused %d entries" log line is only as honest as
// len(report.Unsupported) here: this proves that count reflects the real,
// merged state (every already-committed entry's own report untouched,
// plus the one new "_document" entry) after the exact same
// cancel-after-first-entry interruption the RPC-level test above proves,
// not a stale or fabricated number.
func TestReimportConfiguredJSONDirectlyReportsHonestUnsupportedCountAfterCancellation(t *testing.T) {
	service, _ := newRegistryTestService(t)
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "mcp.json"), mcpTwoEntryDocument(), 0o600); err != nil {
		t.Fatal(err)
	}
	service.SetMCPConfigRoot(root)

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	service.importEntryBarrier = func(name string) {
		if name == "aaa-vendor" {
			cancel()
		}
	}

	report, err := service.ReimportConfiguredJSON(ctx)
	if err != nil {
		t.Fatalf("ReimportConfiguredJSON must succeed with a partial report, matching app startup's own expectation that only a genuine file/database failure aborts: %v", err)
	}
	if len(report.Imported) != 1 || report.Imported[0] != "aaa-vendor" {
		t.Fatalf("Imported = %v, want [aaa-vendor]", report.Imported)
	}
	// The exact count app.go's startup log would print.
	if len(report.Unsupported) != 1 {
		t.Fatalf("Unsupported count = %d, want exactly 1 (the _document entry) — this is the exact number app startup logs", len(report.Unsupported))
	}
	if _, present := report.Unsupported["_document"]; !present {
		t.Fatalf("Unsupported = %+v, want a _document entry", report.Unsupported)
	}
}

// mcpThreeEntryDocumentWithAnOrdinaryRefusal builds an mcp.json document
// with three entries whose sorted-name processing order is fixed: a
// "command"-shaped entry (ordinary, per-entry Unsupported refusal —
// stdio is unsupported) sorts and therefore processes first, then a
// second entry that commits normally, then a third that is never
// reached. Used to prove report.Unsupported — not merely
// report.Imported — survives a fatal error discovered partway through
// the same run: the first entry's own refusal is recorded well before
// the second entry's commit or the third entry's later interruption, so
// it is never part of what a fatal return path could plausibly still be
// "in the middle of" accumulating.
func mcpThreeEntryDocumentWithAnOrdinaryRefusal() []byte {
	return []byte(`{
		"mcpServers": {
			"aaa-refused": {"command": "npx", "args": ["x"]},
			"bbb-vendor": {"url": "https://bbb-vendor.example/mcp"},
			"zzz-vendor": {"url": "https://zzz-vendor.example/mcp"}
		}
	}`)
}

// TestReimportMcpJsonCancellationPreservesEarlierPerEntryRefusalTooNotJustImported
// closes the one coverage gap the cancellation-safe-partial-report test
// above leaves open: it only ever has a "_document" entry (recorded
// after the cancellation) alongside Imported, never proving that an
// *earlier*, ordinary per-entry Unsupported refusal — recorded well
// before anything was interrupted — also survives a later fatal error in
// the same run. ImportJSON's named-return/defer mechanism preserves
// report.Unsupported by construction (recordUnsupported already writes
// directly into the same map every fatal return path returns), but this
// proves it end to end rather than only by inspection: "aaa-refused" is
// refused first, "bbb-vendor" commits and triggers the cancellation via
// importEntryBarrier, and "zzz-vendor" is never reached — the final
// response must carry all three: aaa-refused (Unsupported, unchanged
// reason), bbb-vendor (Imported), and _document (the new, merged
// interruption note) — with nothing lost and nothing collapsed.
func TestReimportMcpJsonCancellationPreservesEarlierPerEntryRefusalTooNotJustImported(t *testing.T) {
	service, _ := newRegistryTestService(t)
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "mcp.json"), mcpThreeEntryDocumentWithAnOrdinaryRefusal(), 0o600); err != nil {
		t.Fatal(err)
	}
	service.SetMCPConfigRoot(root)

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	service.importEntryBarrier = func(name string) {
		if name == "bbb-vendor" {
			cancel()
		}
	}

	response, err := service.ReimportMcpJson(ctx, &turingv1.ReimportMcpJsonRequest{})
	if err != nil {
		t.Fatalf("a cancellation after the second entry committed must still produce a successful partial report: %v", err)
	}
	if len(response.GetImported()) != 1 || response.GetImported()[0] != "bbb-vendor" {
		t.Fatalf("Imported = %v, want [bbb-vendor]", response.GetImported())
	}
	refusedReason, refusedPresent := findUnsupportedReason(response.GetUnsupported(), "aaa-refused")
	if !refusedPresent {
		t.Fatalf("Refused = %+v, want aaa-refused still present: its own refusal was recorded well before the later cancellation, not lost by it", response.GetUnsupported())
	}
	const wantRefusedReason = "stdio/command MCP servers are unsupported; run the server in a container or use an HTTPS URL"
	if refusedReason != wantRefusedReason {
		t.Fatalf("aaa-refused reason = %q, want %q: its own real reason, not collapsed into the later _document note", refusedReason, wantRefusedReason)
	}
	if _, documentPresent := findUnsupportedReason(response.GetUnsupported(), "_document"); !documentPresent {
		t.Fatalf("Refused = %+v, want a _document entry describing the interrupted run too", response.GetUnsupported())
	}
	if len(response.GetUnsupported()) != 2 {
		t.Fatalf("Refused = %+v, want exactly two entries (aaa-refused and _document)", response.GetUnsupported())
	}
}

// TestReimportMcpJsonRepositoryFailureAfterFirstEntryStillAuditsBeforeInternal
// is the rare fallback path: not merely a canceled context but a genuine,
// unrelated repository failure (the database itself closed) strikes
// immediately after "aaa-vendor" has already committed — so even
// recordDocumentRefusal's own detached-context persistence attempt fails
// too (a closed database refuses every query regardless of which context
// carries it). ReimportMcpJson must still map this to Internal, but only
// after notifying and auditing the real, already-committed effect: the
// audit record must show imported=1, and the notifier must still fire,
// exactly as the "safe" path above does — the only difference is the
// final status returned to the caller.
//
// A fake, in-memory audit recorder is used here (not the real one): the
// same closed database that breaks recordDocumentRefusal's own
// detached-context write would equally break a *real* audit write
// against it, which would make "was audit actually called" and "did the
// database accept it" indistinguishable. auditMCPEvent's own swallow-
// and-log behavior for a failed real write is existing, unchanged
// behavior this test is not about; what this test is specifically about
// is that the call happens at all, with the right payload, before the
// Internal status is returned — which the fake recorder observes
// directly, independent of the database's own state.
func TestReimportMcpJsonRepositoryFailureAfterFirstEntryStillAuditsBeforeInternal(t *testing.T) {
	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if err := db.ApplyMigrations(context.Background(), database); err != nil {
		t.Fatal(err)
	}
	sealer, err := secretbox.New(bytes.Repeat([]byte{0x41}, secretbox.KeySize))
	if err != nil {
		t.Fatal(err)
	}
	repo := repository.New(database)
	service := New(repo, sealer, nil)
	notifier := &countingRegistryChangeNotifier{}
	service.SetRegistryChangeNotifier(notifier)
	recorder := &recordingAuditRecorder{}
	service.SetAuditRecorder(recorder)

	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "mcp.json"), mcpTwoEntryDocument(), 0o600); err != nil {
		t.Fatal(err)
	}
	service.SetMCPConfigRoot(root)

	var barrierFired int
	service.importEntryBarrier = func(name string) {
		barrierFired++
		if name == "aaa-vendor" {
			_ = database.Close()
		}
	}

	_, err = service.ReimportMcpJson(context.Background(), &turingv1.ReimportMcpJsonRequest{})
	if status.Code(err) != codes.Internal {
		t.Fatalf("code = %v, want Internal once even the detached post-commit issue write fails against a closed database", status.Code(err))
	}
	if barrierFired != 1 {
		t.Fatalf("importEntryBarrier fired %d times, want exactly 1", barrierFired)
	}
	if notifier.calls != 1 {
		t.Fatalf("notify calls = %d, want 1: aaa-vendor really committed before the database closed", notifier.calls)
	}
	if len(recorder.records) != 1 {
		t.Fatalf("audit records = %+v, want exactly one, recorded before the Internal status was returned", recorder.records)
	}
	if recorder.records[0].action != "mcp.server.reimported" {
		t.Fatalf("audit action = %q, want mcp.server.reimported", recorder.records[0].action)
	}
	if recorder.records[0].payload["imported"] != 1 {
		t.Fatalf("audit payload = %+v, want imported=1: aaa-vendor really committed before the database closed", recorder.records[0].payload)
	}
	// This run committed at least one import before the later, fatal
	// failure struck — the "partial" status, never "completed" (a run
	// with no later error) — distinguishes it from an ordinary
	// successful reimport for anything that reads this audit trail.
	if recorder.records[0].payload["status"] != "partial" {
		t.Fatalf("audit payload[status] = %v, want partial", recorder.records[0].payload["status"])
	}
}
