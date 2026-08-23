package mcpregistry

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
)

// mcpThirdPartyBudgetTestDocument builds an mcp.json document with one
// comfortably in-budget entry ("aaa-vendor") and one entry
// ("zzz-over-budget") whose own tool, combined with aaa-vendor's
// already-committed contribution, pushes the *third-party-only* share of
// the registry-wide aggregate over repository.MaxThirdPartyMCPRegistryToolBytes
// (128 KiB = 131072 bytes) — well before either entry, alone or combined,
// comes anywhere near the much larger full aggregate
// (repository.MaxMCPRegistryToolBytes). ImportJSON processes entries in
// sorted-name order, so "aaa-vendor" (which sorts first) always commits
// before "zzz-over-budget" is even attempted. Deliberately not an
// exact-boundary test (the repository package's own
// TestReplaceServerToolsTxThirdPartyBudget* tests already cover that):
// 80000+60000 raw padding bytes, plus each tool's small fixed JSON
// overhead, comfortably exceeds the 131072-byte cap without needing
// fragile exact arithmetic, while each individually (and even
// zzz-over-budget alone) stays comfortably under it too.
func mcpThirdPartyBudgetTestDocument() []byte {
	firstPadding := strings.Repeat("x", 80_000)
	secondPadding := strings.Repeat("y", 60_000)
	return []byte(`{
		"mcpServers": {
			"aaa-vendor": {
				"url": "https://aaa-vendor.example/mcp",
				"tools": [{"name": "a", "inputSchema": {"type": "object", "d": "` + firstPadding + `"}}]
			},
			"zzz-over-budget": {
				"url": "https://zzz-over-budget.example/mcp",
				"tools": [{"name": "b", "inputSchema": {"type": "object", "d": "` + secondPadding + `"}}]
			}
		}
	}`)
}

// TestImportJSONRefusesOverThirdPartyBudgetEntryWithoutLosingEarlierImports
// proves ImportJSON treats repository.ErrMCPThirdPartyToolBudgetExceeded
// exactly like every other per-entry repository disposition
// (ErrMCPServerRegistryFull, ErrMCPToolNameCollision,
// ErrMCPRegistryToolBudgetExceeded): recorded as an ordinary Unsupported
// refusal for the offending entry, never an error that aborts the rest of
// the document. Before ImportJSON's switch had a case for this error, it
// fell to the default branch and returned an error from ImportJSON
// itself — discarding the in-memory report entirely (including
// "aaa-vendor" below, which had *already* committed to the repository
// via its own independent transaction one loop iteration earlier) and
// collapsing mcp_import_issues down to a single opaque "_document" entry
// instead of the real, per-entry refusal.
func TestImportJSONRefusesOverThirdPartyBudgetEntryWithoutLosingEarlierImports(t *testing.T) {
	service, repo := newRegistryTestService(t)
	ctx := context.Background()

	report, err := service.ImportJSON(ctx, mcpThirdPartyBudgetTestDocument())
	if err != nil {
		t.Fatalf("ImportJSON returned an error instead of a per-entry refusal: %v", err)
	}
	if len(report.Imported) != 1 || report.Imported[0] != "aaa-vendor" {
		t.Fatalf("Imported = %v, want [aaa-vendor]: the earlier, in-budget entry must not be lost", report.Imported)
	}
	if len(report.Skipped) != 0 {
		t.Fatalf("Skipped = %v, want none", report.Skipped)
	}
	reason, refused := report.Unsupported["zzz-over-budget"]
	if !refused {
		t.Fatalf("Unsupported = %+v, want zzz-over-budget refused for exceeding the third-party tool budget", report.Unsupported)
	}
	if reason != mcpThirdPartyToolBudgetExceededMessage {
		t.Fatalf("reason = %q, want the fixed third-party budget-exceeded reason %q", reason, mcpThirdPartyToolBudgetExceededMessage)
	}
	// Never collapsed into a document-level summary.
	if _, collapsed := report.Unsupported["_document"]; collapsed {
		t.Fatalf("Unsupported = %+v, a per-entry budget refusal must never collapse into _document", report.Unsupported)
	}

	// aaa-vendor's row genuinely exists: its own transaction committed
	// independently of zzz-over-budget's later refusal.
	aaaServer, err := repo.GetMCPServerByName(ctx, "aaa-vendor")
	if err != nil {
		t.Fatalf("aaa-vendor was not persisted despite being reported Imported: %v", err)
	}
	aaaTools, err := repo.ListMCPServerTools(ctx, aaaServer.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(aaaTools) != 1 || aaaTools[0].Name != "a" || !aaaTools[0].Present {
		t.Fatalf("aaa-vendor tools = %+v, want its one tool present", aaaTools)
	}

	// zzz-over-budget must create no row at all — the repository's own
	// transaction already rolled back entirely (see
	// TestReplaceServerToolsTxThirdPartyBudgetOneByteOverCapAloneIsRefused
	// in the repository package for the boundary/rollback proof at that
	// layer).
	if _, err := repo.GetMCPServerByName(ctx, "zzz-over-budget"); err != repository.ErrMCPServerNotFound {
		t.Fatalf("err = %v, want ErrMCPServerNotFound: a budget-refused entry must create no row", err)
	}

	// mcp_import_issues persists the real, per-entry refusal — never a
	// "_document" collapse.
	issues, err := repo.ListMCPImportIssues(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(issues) != 1 || issues[0].Name != "zzz-over-budget" || issues[0].Reason != mcpThirdPartyToolBudgetExceededMessage {
		t.Fatalf("issues = %+v, want exactly one zzz-over-budget issue with the fixed reason", issues)
	}
}

// TestReimportMcpJsonNotifiesAndAuditsCorrectlyWhenALaterEntryExceedsThirdPartyBudget
// exercises the same scenario through the file-based ReimportMcpJson RPC,
// the caller ImportJSON's own per-entry report feeds: notify must still
// fire because aaa-vendor really was imported (a document-level abort
// must never suppress that), the RPC's Refused list must name
// zzz-over-budget with the fixed reason rather than a collapsed
// "_document" summary, and the audit record must carry the real counts
// (one imported, one refused) rather than whatever an aborted-document
// error path would have produced.
func TestReimportMcpJsonNotifiesAndAuditsCorrectlyWhenALaterEntryExceedsThirdPartyBudget(t *testing.T) {
	service, _ := newRegistryTestService(t)
	notifier := &countingRegistryChangeNotifier{}
	service.SetRegistryChangeNotifier(notifier)
	recorder := &recordingAuditRecorder{}
	service.SetAuditRecorder(recorder)
	ctx := context.Background()

	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "mcp.json"), mcpThirdPartyBudgetTestDocument(), 0o600); err != nil {
		t.Fatal(err)
	}
	service.SetMCPConfigRoot(root)

	response, err := service.ReimportMcpJson(ctx, &turingv1.ReimportMcpJsonRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if len(response.GetImported()) != 1 || response.GetImported()[0] != "aaa-vendor" {
		t.Fatalf("Imported = %v, want [aaa-vendor]", response.GetImported())
	}
	if len(response.GetUnsupported()) != 1 || response.GetUnsupported()[0].GetName() != "zzz-over-budget" ||
		response.GetUnsupported()[0].GetReason() != mcpThirdPartyToolBudgetExceededMessage {
		t.Fatalf("Refused = %+v, want exactly one zzz-over-budget refusal with the fixed reason", response.GetUnsupported())
	}
	if notifier.calls != 1 {
		t.Fatalf("notify calls = %d, want 1: aaa-vendor really was imported despite zzz-over-budget's refusal", notifier.calls)
	}

	if len(recorder.records) != 1 {
		t.Fatalf("audit records = %+v, want exactly one", recorder.records)
	}
	last := recorder.records[0]
	if last.action != "mcp.server.reimported" {
		t.Fatalf("audit action = %q, want mcp.server.reimported", last.action)
	}
	if last.payload["imported"] != 1 || last.payload["skipped"] != 0 || last.payload["refused"] != 1 {
		t.Fatalf("audit payload = %+v, want imported=1 skipped=0 refused=1", last.payload)
	}
}

// TestImportJSONRefusesOverFullAggregateBudgetEntryWithoutLosingEarlierImports
// is the full-aggregate counterpart: it anchors most of
// repository.MaxMCPRegistryToolBytes with a bundled/skills snapshot (via
// UpsertTools) *before* ImportJSON ever runs, so two small non-bundled
// entries — each, and even combined, comfortably within their own,
// separate MaxThirdPartyMCPRegistryToolBytes sub-budget — still trip the
// full, shared aggregate. This proves ImportJSON's own
// ErrMCPRegistryToolBudgetExceeded handling (distinct from the
// third-party-specific case just above) is exercised the same
// per-entry, non-aborting way even when the third-party sub-budget was
// never the real constraint.
func TestImportJSONRefusesOverFullAggregateBudgetEntryWithoutLosingEarlierImports(t *testing.T) {
	service, repo := newRegistryTestService(t)
	ctx := context.Background()

	const anchorName = "skills.anchor"
	const anchorPrefix = `{"type":"object","d":"`
	const anchorSuffix = `"}`
	anchorPad := repository.MaxMCPRegistryToolBytes - 1000 - len(anchorName) - len(anchorPrefix) - len(anchorSuffix)
	if err := repo.UpsertTools(ctx, []repository.DiscoveredTool{{
		ServerName: "skills", ToolName: anchorName, Policy: "safe",
		SchemaJSON: anchorPrefix + strings.Repeat("x", anchorPad) + anchorSuffix,
	}}); err != nil {
		t.Fatalf("bundled/skills anchor snapshot: %v", err)
	}

	firstPadding := strings.Repeat("x", 900)
	secondPadding := strings.Repeat("y", 101)
	document := []byte(`{
		"mcpServers": {
			"aaa-vendor": {
				"url": "https://aaa-vendor.example/mcp",
				"tools": [{"name": "a", "inputSchema": {"type": "object", "d": "` + firstPadding + `"}}]
			},
			"zzz-over-budget": {
				"url": "https://zzz-over-budget.example/mcp",
				"tools": [{"name": "b", "inputSchema": {"type": "object", "d": "` + secondPadding + `"}}]
			}
		}
	}`)

	report, err := service.ImportJSON(ctx, document)
	if err != nil {
		t.Fatalf("ImportJSON returned an error instead of a per-entry refusal: %v", err)
	}
	if len(report.Imported) != 1 || report.Imported[0] != "aaa-vendor" {
		t.Fatalf("Imported = %v, want [aaa-vendor]: the earlier, in-budget entry must not be lost", report.Imported)
	}
	reason, refused := report.Unsupported["zzz-over-budget"]
	if !refused {
		t.Fatalf("Unsupported = %+v, want zzz-over-budget refused for exceeding the full aggregate tool budget", report.Unsupported)
	}
	if reason != mcpRegistryToolBudgetExceededMessage {
		t.Fatalf("reason = %q, want the fixed full-aggregate budget-exceeded reason %q (not the third-party-specific one: both non-bundled entries stay nowhere near that narrower cap)", reason, mcpRegistryToolBudgetExceededMessage)
	}
	if _, err := repo.GetMCPServerByName(ctx, "zzz-over-budget"); err != repository.ErrMCPServerNotFound {
		t.Fatalf("err = %v, want ErrMCPServerNotFound: a budget-refused entry must create no row", err)
	}
}
