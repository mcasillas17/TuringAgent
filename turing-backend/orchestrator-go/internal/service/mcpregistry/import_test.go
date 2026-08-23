package mcpregistry

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/db"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/secretbox"
)

func TestImportingMcpJSONLeavesEverythingOff(t *testing.T) {
	service, repo := newRegistryTestService(t)
	report, err := service.ImportJSON(context.Background(), []byte(`{
		"mcpServers": {
			"vendor": {
				"url": "https://vendor.example/mcp",
				"headers": {"Authorization": "Bearer vendor-secret"},
				"tools": [{
					"name": "vendor.lookup",
					"inputSchema": {"type": "object"}
				}]
			}
		}
	}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Unsupported) != 0 {
		t.Fatalf("unsupported = %v, want none", report.Unsupported)
	}

	servers, err := repo.ListMCPServers(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	vendor := findRepositoryServer(t, servers, "vendor")
	if vendor.Enabled {
		t.Fatal("an imported server must arrive disabled")
	}
	if vendor.Tier != repository.MCPServerTierRemoteURL {
		t.Fatalf("tier = %q, want remote URL", vendor.Tier)
	}
	if bytes.Contains(vendor.SealedToken, []byte("vendor-secret")) {
		t.Fatal("the imported bearer was stored in plaintext")
	}

	tools, err := repo.ListMCPServerTools(context.Background(), vendor.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(tools) != 1 || tools[0].Policy != "approval_required" || tools[0].Enabled {
		t.Fatalf("imported tools = %+v, want disabled and approval_required", tools)
	}
}

func TestStdioEntriesAreReportedAsUnsupported(t *testing.T) {
	service, repo := newRegistryTestService(t)
	report, err := service.ImportJSON(context.Background(), []byte(`{
		"mcpServers": {
			"local": {"command": "npx", "args": ["x"]}
		}

	}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(report.Unsupported["local"], "stdio") {
		t.Fatalf("report = %q, want it to say why stdio is unsupported", report.Unsupported["local"])
	}
	servers, err := repo.ListMCPServers(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, server := range servers {
		if server.Name == "local" {
			t.Fatal("a stdio entry must not be registered")
		}
	}
}

// A reserved or pattern-invalid name must be refused for that reason alone,
// even when its raw JSON body would otherwise fail strict decoding (an
// unknown field here). This proves the name check runs before the entry is
// decoded, sharing the one validateMCPServerName implementation import and
// RegisterMcpServer both use — not a second, decode-order-dependent check
// that a future edit could silently diverge from mapMCPValidationError's
// reserved-name handling. Neither entry's own raw name appears anywhere in
// the report: both are recorded under a bounded, synthetic
// "_invalid_server_N" label with the one fixed
// invalidMCPEntryNameMessage reason (see TestImportInvalidEntryKeyEqualToBearerSentinelNeverLeaks
// for the full leak sweep), assigned in sorted-name order — "files" before
// "not a valid name!" — so this is deterministic regardless of which
// specific check ("reserved" vs. pattern-invalid) refused which entry.
func TestImportJSONRefusesReservedAndInvalidNamesBeforeDecodingTheEntryBody(t *testing.T) {
	service, repo := newRegistryTestService(t)
	report, err := service.ImportJSON(context.Background(), []byte(`{
		"mcpServers": {
			"files": {"unknown_field_that_fails_strict_decoding": true},
			"not a valid name!": {"unknown_field_that_fails_strict_decoding": true}
		}
	}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Unsupported) != 2 {
		t.Fatalf("Unsupported = %+v, want exactly two entries", report.Unsupported)
	}
	for _, label := range []string{"_invalid_server_1", "_invalid_server_2"} {
		reason, refused := report.Unsupported[label]
		if !refused {
			t.Fatalf("Unsupported = %+v, want %q present", report.Unsupported, label)
		}
		if reason != invalidMCPEntryNameMessage {
			t.Fatalf("%s reason = %q, want the fixed reason %q", label, reason, invalidMCPEntryNameMessage)
		}
	}
	if _, present := report.Unsupported["files"]; present {
		t.Fatal("the entry's own raw name \"files\" must never be used as the Unsupported key")
	}
	if _, present := report.Unsupported["not a valid name!"]; present {
		t.Fatal("the entry's own raw name \"not a valid name!\" must never be used as the Unsupported key")
	}

	servers, err := repo.ListMCPServers(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, server := range servers {
		// "files" itself is a pre-existing bundled row (seeded by
		// migration 0016, not by this import attempt) and is expected
		// to still be present — only a *new* row for either raw name
		// would indicate this refusal did not actually hold.
		if server.Name == "not a valid name!" {
			t.Fatal("an invalid name must never create a row")
		}
	}
}

// A reserved name must be refused case-insensitively through the file
// import path too — not just validateMCPServerName in isolation — so a
// mixed-case reserved name in mcp.json cannot register over (and shadow) a
// bundled server's namespace, and, since mcpServerNamePattern itself
// accepts mixed case, this would otherwise silently succeed.
func TestImportJSONRefusesReservedNamesCaseInsensitively(t *testing.T) {
	service, repo := newRegistryTestService(t)
	report, err := service.ImportJSON(context.Background(), []byte(`{
		"mcpServers": {
			"Files": {"url": "https://vendor.example/mcp"},
			"SYSTEM": {"url": "https://vendor.example/mcp"},
			"sKiLlS": {"url": "https://vendor.example/mcp"}
		}
	}`))
	if err != nil {
		t.Fatal(err)
	}
	// Every entry is refused under its own bounded, synthetic
	// "_invalid_server_N" label (never its own raw, case-variant name —
	// see TestImportJSONRefusesReservedAndInvalidNamesBeforeDecodingTheEntryBody),
	// one per entry, all sharing the one fixed invalidMCPEntryNameMessage.
	if len(report.Unsupported) != 3 {
		t.Fatalf("Unsupported = %+v, want exactly three entries", report.Unsupported)
	}
	for _, label := range []string{"_invalid_server_1", "_invalid_server_2", "_invalid_server_3"} {
		reason, refused := report.Unsupported[label]
		if !refused {
			t.Fatalf("Unsupported = %+v, want %q present", report.Unsupported, label)
		}
		if reason != invalidMCPEntryNameMessage {
			t.Fatalf("%s reason = %q, want the fixed reason %q", label, reason, invalidMCPEntryNameMessage)
		}
	}
	for _, name := range []string{"Files", "SYSTEM", "sKiLlS"} {
		if _, present := report.Unsupported[name]; present {
			t.Fatalf("the entry's own raw name %q must never be used as the Unsupported key", name)
		}
	}
	if len(report.Imported) != 0 {
		t.Fatalf("Imported = %v, want none: every entry names a reserved bundled namespace", report.Imported)
	}
	servers, err := repo.ListMCPServers(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, server := range servers {
		if server.Name == "Files" || server.Name == "SYSTEM" || server.Name == "sKiLlS" {
			t.Fatalf("a case-variant reserved name must never create a row: %+v", server)
		}
	}
}

// validateMCPServerName is the one shared reserved/pattern check. This
// fails if that sharing is ever undone — e.g. if ImportJSON grows its own
// separate name check that drifts from validateServerDefinition's.
func TestValidateMCPServerNameIsSharedByImportAndValidateServerDefinition(t *testing.T) {
	for _, name := range []string{
		"files", "system", "skills",
		// Reserved names must be refused case-insensitively: the
		// pattern itself accepts mixed-case names, so without a
		// case-insensitive reserved check, "Files"/"SYSTEM"/"sKiLlS"
		// would silently register over a bundled server's namespace
		// under a differently-cased name.
		"Files", "SYSTEM", "sKiLlS",
		"not a valid name!", "",
	} {
		nameErr := validateMCPServerName(name)
		if nameErr == nil {
			t.Fatalf("validateMCPServerName(%q) = nil, want an error", name)
		}
		_, defErr := validateServerDefinition(name, "https://vendor.example/mcp", nil, "")
		if defErr == nil {
			t.Fatalf("validateServerDefinition(%q, ...) = nil, want an error", name)
		}
		if defErr.Error() != nameErr.Error() {
			t.Fatalf("validateServerDefinition(%q) = %q, want the same reason as validateMCPServerName: %q",
				name, defErr.Error(), nameErr.Error())
		}
	}
}

// The "integrations" pseudo-server name is reserved at import, and the
// "github." tool namespace it owns cannot be claimed by a third-party
// server's discovery either — the second half is what keeps a registered
// vendor from shadowing a first-party integration tool after the fact.
func TestIntegrationsNameAndGitHubToolNamespaceAreReserved(t *testing.T) {
	service, repo := newRegistryTestService(t)
	report, err := service.ImportJSON(context.Background(), []byte(`{"mcpServers":{"integrations":{"url":"https://vendor.example/mcp"}}}`))
	if err != nil {
		t.Fatal(err)
	}
	if reason := report.Unsupported["_invalid_server_1"]; reason != invalidMCPEntryNameMessage {
		t.Fatalf("report = %+v, want _invalid_server_1 refused with the fixed reason %q", report.Unsupported, invalidMCPEntryNameMessage)
	}
	if _, present := report.Unsupported["integrations"]; present {
		t.Fatal("the entry's own raw name \"integrations\" must never be used as the Unsupported key")
	}
	server, err := repo.RegisterMCPServer(context.Background(), repository.ImportedMCPServer{Name: "vendor", URL: "https://vendor.example/mcp", Tier: repository.MCPServerTierRemoteURL})
	if err != nil {
		t.Fatal(err)
	}
	err = service.RecordDiscovery(context.Background(), server.Server.ID, []DiscoveredTool{{Name: "github.list_issues", SchemaJSON: `{}`}})
	if !errors.Is(err, repository.ErrMCPToolNameCollision) {
		t.Fatalf("collision error = %v", err)
	}
}

func newRegistryTestService(t *testing.T) (*Server, *repository.Repository) {
	t.Helper()
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
	return New(repo, sealer, nil), repo
}

func findRepositoryServer(t *testing.T, servers []repository.MCPServerRecord, name string) repository.MCPServerRecord {
	t.Helper()
	for _, server := range servers {
		if server.Name == name {
			return server
		}
	}
	t.Fatalf("server %q not found in %+v", name, servers)
	return repository.MCPServerRecord{}
}

// An unsupported header name or tool name in mcp.json is attacker
// controlled — an operator's own file, but one that could still be
// arbitrarily long or (via headers, which are a JSON object's keys) not
// otherwise length-limited by anything upstream. Every ImportJSON
// unsupported reason must go through the same boundedStatusMessage bound
// RecordDiscovery's own failures already used, both in the report handed
// back to the caller and in what gets persisted to mcp_import_issues.
func TestImportJSONBoundsLongAttackerControlledReasonsAndKeepsValidUTF8(t *testing.T) {
	service, repo := newRegistryTestService(t)
	longHeaderName := strings.Repeat("x-évil-header-", 60)
	longToolName := strings.Repeat("évil-tool-name-", 60)
	if len(longHeaderName) <= maxMCPStatusMessageBytes || len(longToolName) <= maxMCPStatusMessageBytes {
		t.Fatalf("test setup is broken: names must exceed maxMCPStatusMessageBytes (%d) on their own", maxMCPStatusMessageBytes)
	}

	document := fmt.Sprintf(`{
		"mcpServers": {
			"vendor-header": {
				"url": "https://vendor-header.example/mcp",
				"headers": {%q: "Bearer x"}
			},
			"vendor-tool": {
				"url": "https://vendor-tool.example/mcp",
				"tools": [{"name": %q, "inputSchema": {"type": "array"}}]
			}
		}
	}`, longHeaderName, longToolName)

	report, err := service.ImportJSON(context.Background(), []byte(document))
	if err != nil {
		t.Fatal(err)
	}

	headerReason, ok := report.Unsupported["vendor-header"]
	if !ok {
		t.Fatalf("report = %+v, want vendor-header refused", report.Unsupported)
	}
	assertBoundedUTF8(t, "header reason", headerReason)

	toolReason, ok := report.Unsupported["vendor-tool"]
	if !ok {
		t.Fatalf("report = %+v, want vendor-tool refused", report.Unsupported)
	}
	assertBoundedUTF8(t, "tool reason", toolReason)

	issues, err := repo.ListMCPImportIssues(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	found := map[string]bool{}
	for _, issue := range issues {
		assertBoundedUTF8(t, "persisted reason for "+issue.Name, issue.Reason)
		found[issue.Name] = true
	}
	if !found["vendor-header"] || !found["vendor-tool"] {
		t.Fatalf("issues = %+v, want both vendor-header and vendor-tool persisted", issues)
	}
}

// The mcp.json entry KEY itself is attacker-controlled — a JSON object's
// keys carry no length limit of their own — and, unlike the "reason"
// string recordUnsupported bounds for every other refusal, an invalid
// entry's own key is never used to record its refusal at all: it is
// recorded under a bounded, synthetic "_invalid_server_N" label instead
// (see invalidMCPEntryLabel), regardless of how long or malformed the
// original key was. This proves an arbitrarily long invalid key never
// reaches the in-memory report, the persisted mcp_import_issues row, or
// (transitively) the Flutter UI's unsupported-server list — not even
// truncated, since it is never used as source material for the label at
// all.
func TestImportJSONBoundsLongInvalidServerNameKeyAndKeepsValidUTF8(t *testing.T) {
	service, repo := newRegistryTestService(t)
	longInvalidName := strings.Repeat("x-évil-name-", 20) + "!" // trailing "!" also makes it pattern-invalid
	if len(longInvalidName) <= maxMCPUnsupportedNameBytes {
		t.Fatalf("test setup is broken: name must exceed maxMCPUnsupportedNameBytes (%d) on its own", maxMCPUnsupportedNameBytes)
	}

	document, err := json.Marshal(map[string]any{
		"mcpServers": map[string]any{
			longInvalidName: map[string]any{"url": "https://vendor.example/mcp"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	report, err := service.ImportJSON(context.Background(), document)
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Unsupported) != 1 {
		t.Fatalf("Unsupported = %+v, want exactly one entry", report.Unsupported)
	}
	reason, refused := report.Unsupported["_invalid_server_1"]
	if !refused {
		t.Fatalf("Unsupported = %+v, want _invalid_server_1 present", report.Unsupported)
	}
	if reason != invalidMCPEntryNameMessage {
		t.Fatalf("reason = %q, want the fixed reason %q", reason, invalidMCPEntryNameMessage)
	}
	for key := range report.Unsupported {
		if strings.Contains(key, longInvalidName) || strings.HasPrefix(longInvalidName, key) {
			t.Fatalf("key = %q, must not be derived from the entry's own long invalid name at all", key)
		}
	}

	issues, err := repo.ListMCPImportIssues(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(issues) != 1 {
		t.Fatalf("issues = %+v, want exactly one persisted", issues)
	}
	for _, issue := range issues {
		if issue.Name != "_invalid_server_1" {
			t.Fatalf("persisted issue name = %q, want the synthetic label _invalid_server_1", issue.Name)
		}
		if len(issue.Name) > maxMCPUnsupportedNameBytes {
			t.Fatalf("persisted issue name length = %d, want <= maxMCPUnsupportedNameBytes (%d)", len(issue.Name), maxMCPUnsupportedNameBytes)
		}
		if !utf8.ValidString(issue.Name) {
			t.Fatalf("persisted issue name = %q is not valid UTF-8", issue.Name)
		}
	}
}

// Recording an invalid or reserved entry's refusal under a synthetic,
// per-document-ordinal label (rather than anything derived from its own
// name) means two distinct invalid names never collapse into a single
// Unsupported entry, unlike a scheme that bounded and reused the name
// itself: each gets its own "_invalid_server_N" label, assigned in the
// same sorted-by-name order ImportJSON already processes entries in, so
// this is deterministic across repeated imports of the same document —
// not merely uncollided by chance.
func TestImportJSONTwoInvalidNamesGetDistinctDeterministicSyntheticLabels(t *testing.T) {
	service, _ := newRegistryTestService(t)
	prefix := strings.Repeat("x", maxMCPUnsupportedNameBytes)
	nameA := prefix + "-first-suffix-makes-this-invalid!"
	nameB := prefix + "-second-suffix-makes-this-invalid!"
	if nameA == nameB {
		t.Fatal("test setup is broken: the two names must differ only after the shared bounded prefix")
	}

	document, err := json.Marshal(map[string]any{
		"mcpServers": map[string]any{
			nameA: map[string]any{"url": "https://vendor-a.example/mcp"},
			nameB: map[string]any{"url": "https://vendor-b.example/mcp"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	report, err := service.ImportJSON(context.Background(), document)
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Unsupported) != 2 {
		t.Fatalf("Unsupported = %+v, want exactly two entries: distinct synthetic labels, no collision", report.Unsupported)
	}
	for _, label := range []string{"_invalid_server_1", "_invalid_server_2"} {
		reason, refused := report.Unsupported[label]
		if !refused {
			t.Fatalf("Unsupported = %+v, want %q present", report.Unsupported, label)
		}
		if reason != invalidMCPEntryNameMessage {
			t.Fatalf("%s reason = %q, want the fixed reason %q", label, reason, invalidMCPEntryNameMessage)
		}
	}
	if len(report.Imported) != 0 {
		t.Fatalf("Imported = %v, want none: both entries are independently invalid", report.Imported)
	}

	// Determinism: repeating the exact same import produces the exact
	// same labels — nameA (sorted first) is always _invalid_server_1,
	// nameB is always _invalid_server_2 — not an artifact of map
	// iteration order or any other incidental nondeterminism.
	again, err := service.ImportJSON(context.Background(), document)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(report.Unsupported, again.Unsupported) {
		t.Fatalf("Unsupported changed across repeated imports of the same document: first=%+v, second=%+v", report.Unsupported, again.Unsupported)
	}
}

func assertBoundedUTF8(t *testing.T, what, reason string) {
	t.Helper()
	if len(reason) > maxMCPStatusMessageBytes {
		t.Fatalf("%s length = %d, want <= %d", what, len(reason), maxMCPStatusMessageBytes)
	}
	if !utf8.ValidString(reason) {
		t.Fatalf("%s is not valid UTF-8: %q", what, reason)
	}
}

// validatedMCPServer carries a plaintext bearer Token until the caller
// seals it. Nothing in this package intentionally formats one with fmt,
// but %v/%+v/%#v are exactly what a stray log.Printf, an error wrap, or a
// test failure message reaches for by default — so String/GoString must
// redact Token themselves rather than relying on every call site to
// remember not to print the struct directly.
func TestValidatedMCPServerNeverPrintsTokenViaFmt(t *testing.T) {
	const sentinel = "validated-mcp-server-token-should-never-print-abc123"
	validated := validatedMCPServer{
		Name:  "vendor",
		URL:   "https://vendor.example/mcp",
		Token: sentinel,
		Tier:  repository.MCPServerTierRemoteURL,
	}
	for _, format := range []string{"%v", "%+v", "%#v", "%s"} {
		rendered := fmt.Sprintf(format, validated)
		if strings.Contains(rendered, sentinel) {
			t.Fatalf("fmt %q rendered the token: %s", format, rendered)
		}
	}
}
